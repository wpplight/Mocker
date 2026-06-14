package parser

import (
	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseStmts 解析语句列表直到 }
func (p *Parser) parseStmts() []ast.Stmt {
	var stmts []ast.Stmt
	loopGuard := 0
	for !p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
		p.skipTrivial()
		if p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
			break
		}
		s := p.parseStmt()
		if s != nil {
			stmts = append(stmts, s)
		}
		loopGuard++
		if loopGuard > 1000 {
			p.errorf("infinite loop in parseStmts at token %v", p.peek())
			break
		}
	}
	return stmts
}

// parseStmt 语句分发
func (p *Parser) parseStmt() ast.Stmt {
	tok := p.peek()
	switch tok.Type {
	case mocker_lex.TypeKW_IF:
		return p.parseIf()
	case mocker_lex.TypeKW_FOR:
		return p.parseFor()
	case mocker_lex.TypeKW_WHILE:
		return p.parseWhile()
	case mocker_lex.TypeKW_RETURN:
		return p.parseReturn()
	case mocker_lex.TypeKW_TRUE, mocker_lex.TypeKW_FALSE:
		// 表达式语句（true / false 单独成 stmt）
		e := p.parseExpr()
		return &ast.ExprStmtWrap{E: e}
	case mocker_lex.TypeOP_RRARROW:
		return p.parseFlowCont()
	case mocker_lex.TypeOP_LT:
		return p.parseConnection()
	case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
		return p.parseStmtDispatch()
	default:
		p.errorf("unexpected statement starting with %s (%q)", tok.Type, tok.Value)
		p.pos++
		return nil
	}
}

// parseStmtDispatch IDENT 开头的语句
//
//	IDENT ":=" EXPR       → VarDecl
//	IDENT "," IDENT ":="  → AssignStmt
//	IDENT ">>" ">>" ...   → FlowFanout  (并发分支，源后面跟两个 >>)
//	IDENT ">>" ...        → FlowStmt    (单链)
//	IDENT <next IDENT>    → Connection  (IDENT 是 node 起点)
func (p *Parser) parseStmtDispatch() ast.Stmt {
	// 试复合赋值（a += b / a -= b / a *= b / a /= b）
	if p.peekN(1).Type == mocker_lex.TypeOP_ADD_ASSIGN ||
		p.peekN(1).Type == mocker_lex.TypeOP_SUB_ASSIGN ||
		p.peekN(1).Type == mocker_lex.TypeOP_MUL_ASSIGN ||
		p.peekN(1).Type == mocker_lex.TypeOP_DIV_ASSIGN {
		return p.parseVarDeclOrAssign()
	}

	// 试 Connection 起始
	if p.isConnectionStart() {
		return p.parseConnection()
	}

	// 试 VarDecl / AssignStmt
	if p.peekN(1).Type == mocker_lex.TypeOP_DEFINE {
		return p.parseVarDeclOrAssign()
	}

	// 试 FlowFanout：IDENT >> >> ...
	// 3-token lookahead：只有源后面紧跟 2 个连续的 >> 才是 fan-out
	if p.peekN(1).Type == mocker_lex.TypeOP_RRARROW &&
		p.peekN(2).Type == mocker_lex.TypeOP_RRARROW {
		return p.parseFlowFanout()
	}

	// 试 FlowStmt：IDENT >> ...
	if p.peekN(1).Type == mocker_lex.TypeOP_RRARROW {
		return p.parseFlowStmt()
	}

	// 默认：表达式语句
	e := p.parseExpr()
	return &ast.ExprStmtWrap{E: e}
}

// isConnectionStart IDENT 后能起一个 Connection？
//   - ">>" : FlowStmt
//   - ":=" : VarDecl
//   - ","  : AssignStmt
//   - "{"  : FuncBody?  (不可)
//   - 后续 IDENT/ID/CALL/EDGE_NAME/LT：作为 Connection 的下一 hop
func (p *Parser) isConnectionStart() bool {
	// 简化：IDENT 后面只要不是 := , >> 就当作 Connection 起点
	// 因为 Connection = IDENT (<IDENT> | EDGE_REF | CALL)*
	switch p.peekN(1).Type {
	case mocker_lex.TypeOP_DEFINE, mocker_lex.TypeOP_ASSIGN, mocker_lex.TypeSEP_COMMA, mocker_lex.TypeOP_RRARROW,
		mocker_lex.TypeOP_ADD_ASSIGN, mocker_lex.TypeOP_SUB_ASSIGN,
		mocker_lex.TypeOP_MUL_ASSIGN, mocker_lex.TypeOP_DIV_ASSIGN,
		mocker_lex.TypeOP_INC, mocker_lex.TypeOP_DEC,
		mocker_lex.TypeSEP_LBRACE, mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeSEP_SEMI,
		mocker_lex.TypeEOF:
		return false
	}
	return true
}

// parseVarDeclOrAssign x := y  /  x = y  /  a, b := y  /  a, b = y
//
//	复合赋值：x += y / x -= y / x *= y / x /= y  都会解糖成 x = x <op> y
func (p *Parser) parseVarDeclOrAssign() ast.Stmt {
	pos := p.peek().Pos
	first := p.consume(mocker_lex.TypeID).Value

	// 复合赋值 (x += y / x -= y / x *= y / x /= y)
	if p.matchCompoundAssign() {
		op := p.consume(p.peek().Type).Value
		rhs := p.parseExpr()
		// 存到 AssignStmt 里，codegen 上面负责 emit 成 Go 的 “first op= rhs"
		return &ast.AssignStmt{
			PosBase:     ast.PosBase{P: pos},
			Lhs:         []string{first},
			Rhs:         rhs,
			Compound:    op, // "+" / "-" / "*" / "/"
			CompoundVar: first,
		}
	}

	if p.match(mocker_lex.TypeSEP_COMMA) {
		// AssignStmt
		var lhs []string
		lhs = append(lhs, first)
		for p.match(mocker_lex.TypeSEP_COMMA) {
			p.consume(mocker_lex.TypeSEP_COMMA)
			lhs = append(lhs, p.consume(mocker_lex.TypeID).Value)
		}
		p.consumeAssign()
		rhs := p.parseExpr()
		return &ast.AssignStmt{PosBase: ast.PosBase{P: pos}, Lhs: lhs, Rhs: rhs}
	}

	// VarDecl
	p.consumeAssign()
	init := p.parseExpr()
	return &ast.VarDecl{PosBase: ast.PosBase{P: pos}, Name: first, Init: init}
}

// matchCompoundAssign 下一 token 是复合赋值？
func (p *Parser) matchCompoundAssign() bool {
	switch p.peek().Type {
	case mocker_lex.TypeOP_ADD_ASSIGN,
		mocker_lex.TypeOP_SUB_ASSIGN,
		mocker_lex.TypeOP_MUL_ASSIGN,
		mocker_lex.TypeOP_DIV_ASSIGN:
		return true
	}
	return false
}

// parseIf if(cond) { ... } else { ... }  或  if cond { ... } else { ... }
//
// 支持两种语法（括号可选）：
//   - C 风格：if (cond) { ... } else { ... }
//   - Go 风格：if cond { ... } else { ... }
func (p *Parser) parseIf() ast.Stmt {
	pos := p.consume(mocker_lex.TypeKW_IF).Pos
	// 可选括号
	if p.match(mocker_lex.TypeSEP_LPAREN) {
		p.consume(mocker_lex.TypeSEP_LPAREN)
	}
	cond := p.parseExpr()
	if p.match(mocker_lex.TypeSEP_RPAREN) {
		p.consume(mocker_lex.TypeSEP_RPAREN)
	}
	p.consume(mocker_lex.TypeSEP_LBRACE)
	body := &ast.BlockStmt{PosBase: ast.PosBase{P: pos}, Stmts: p.parseStmts()}
	p.consume(mocker_lex.TypeSEP_RBRACE)

	ifc := &ast.IfStmt{PosBase: ast.PosBase{P: pos}, Cond: cond, Body: body}
	if p.match(mocker_lex.TypeKW_ELSE) {
		p.consume(mocker_lex.TypeKW_ELSE)
		if p.match(mocker_lex.TypeKW_IF) {
			ifc.Else = p.parseIf()
		} else {
			p.consume(mocker_lex.TypeSEP_LBRACE)
			els := &ast.BlockStmt{PosBase: ast.PosBase{P: pos}, Stmts: p.parseStmts()}
			p.consume(mocker_lex.TypeSEP_RBRACE)
			ifc.Else = els
		}
	}
	return ifc
}

// parseFor for(init; cond; post) { ... }
//
// 变体：
//   - C 风格：for(init; cond; post) { body }
//   - Go while：for (cond) { body }   （init=nil, post=nil）
//   - 无限循环：for { body }            （init=nil, cond=nil, post=nil）
//
// 括号是必须的（避免与 >> 和 node body 冲突）
func (p *Parser) parseFor() ast.Stmt {
	pos := p.consume(mocker_lex.TypeKW_FOR).Pos
	p.consume(mocker_lex.TypeSEP_LPAREN) // for 必须要 (

	var init, post ast.Stmt
	var cond ast.Expr

	// 先 peek 一下，看是不是 Go-while 形式（cond 在 init 位置）
	// 判别：当前是 IDENT/CALL/NUM/STRING/LPAREN，下一个不是 := = ++ -- 也不是 ; 也不是 ) 也不是 (
	// 则认为是 cond
	if p.isForCondOnly() {
		cond = p.parseExpr()
	} else {
		// 1. init（可能是空 —— for(; cond; post)）
		if !p.match(mocker_lex.TypeSEP_SEMI) {
			// init 是 VarDecl 或 AssignStmt
			init = p.parseForInit()
		}
		p.consume(mocker_lex.TypeSEP_SEMI)

		// 2. cond（可能是空 —— for(init; ; post)）
		if !p.match(mocker_lex.TypeSEP_SEMI) {
			cond = p.parseExpr()
		}
		p.consume(mocker_lex.TypeSEP_SEMI)

		// 3. post（可能是空 —— for(init; cond; )）
		if !p.match(mocker_lex.TypeSEP_RPAREN) {
			// post 是 AssignStmt
			post = p.parseForPost()
		}
	}
	p.consume(mocker_lex.TypeSEP_RPAREN)

	p.consume(mocker_lex.TypeSEP_LBRACE)
	body := &ast.BlockStmt{PosBase: ast.PosBase{P: pos}, Stmts: p.parseStmts()}
	p.consume(mocker_lex.TypeSEP_RBRACE)

	return &ast.ForStmt{
		PosBase: ast.PosBase{P: pos},
		Init:    init,
		Cond:    cond,
		Post:    post,
		Body:    body,
	}
}

// isForCondOnly 判断 for 头部是否是“cond 在 init 位”的 Go-while 形式
//
// 返 true 的情况：peekN(0) 是“表达式”起始，peekN(1) 不是 := = ++ -- 也不是 ; 也不是 )
func (p *Parser) isForCondOnly() bool {
	first := p.peek()
	if first.Type == mocker_lex.TypeSEP_SEMI {
		return false // 空 init，走默认路径
	}
	// 表达式起始
	switch first.Type {
	case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeNUM,
		mocker_lex.TypeSTRING, mocker_lex.TypeKW_TRUE, mocker_lex.TypeKW_FALSE,
		mocker_lex.TypeSEP_LPAREN, mocker_lex.TypeOP_SUB, mocker_lex.TypeOP_NOT:
		// 看第二个 token
	default:
		return false
	}
	second := p.peekN(1)
	// 如果下一个是 := = ++ -- ; ) (  则不是 Go-while
	switch second.Type {
	case mocker_lex.TypeOP_DEFINE, mocker_lex.TypeOP_ASSIGN,
		mocker_lex.TypeOP_INC, mocker_lex.TypeOP_DEC,
		mocker_lex.TypeSEP_SEMI, mocker_lex.TypeSEP_RPAREN, mocker_lex.TypeSEP_LPAREN:
		return false
	}
	return true
}

// parseForInit 解析 for 的 init 部分：varDecl / assignStmt
func (p *Parser) parseForInit() ast.Stmt {
	// init 必是 VarDecl 或 AssignStmt（不能是 FlowStmt 等）
	pos := p.peek().Pos
	first := p.consume(mocker_lex.TypeID).Value

	if p.match(mocker_lex.TypeSEP_COMMA) {
		// AssignStmt: a, b := expr
		var lhs []string
		lhs = append(lhs, first)
		for p.match(mocker_lex.TypeSEP_COMMA) {
			p.consume(mocker_lex.TypeSEP_COMMA)
			lhs = append(lhs, p.consume(mocker_lex.TypeID).Value)
		}
		p.consumeAssign()
		rhs := p.parseExpr()
		return &ast.AssignStmt{PosBase: ast.PosBase{P: pos}, Lhs: lhs, Rhs: rhs}
	}

	// VarDecl: name := expr  /  name = expr
	p.consumeAssign()
	init := p.parseExpr()
	return &ast.VarDecl{PosBase: ast.PosBase{P: pos}, Name: first, Init: init}
}

// parseForPost 解析 for 的 post 部分：simple assign (i++ / i += 1 / i = i + 1)
func (p *Parser) parseForPost() ast.Stmt {
	// post 是简单赋值或增量
	pos := p.peek().Pos
	first := p.consume(mocker_lex.TypeID).Value

	// 变体 1：i++  /  i--
	if p.match(mocker_lex.TypeOP_INC) {
		p.consume(mocker_lex.TypeOP_INC)
		return &ast.AssignStmt{
			PosBase:     ast.PosBase{P: pos},
			Lhs:         []string{first},
			Compound:    "+",
			CompoundVar: first,
		}
	}
	if p.match(mocker_lex.TypeOP_DEC) {
		p.consume(mocker_lex.TypeOP_DEC)
		return &ast.AssignStmt{
			PosBase:     ast.PosBase{P: pos},
			Lhs:         []string{first},
			Compound:    "-",
			CompoundVar: first,
		}
	}

	// 变体 2：i += 1 / i -= 1 / i *= 1 / i /= 1
	if p.matchCompoundAssign() {
		op := p.consume(p.peek().Type).Value
		rhs := p.parseExpr()
		return &ast.AssignStmt{
			PosBase:     ast.PosBase{P: pos},
			Lhs:         []string{first},
			Rhs:         rhs,
			Compound:    op,
			CompoundVar: first,
		}
	}

	// 变体 3：i = i + 1（普通赋值）
	p.consumeAssign()
	rhs := p.parseExpr()
	return &ast.AssignStmt{PosBase: ast.PosBase{P: pos}, Lhs: []string{first}, Rhs: rhs}
}

// parseWhile while(cond) { ... }
//
// Mocker 专用语法（Go 里没有 while）—— codegen 转成 for cond { body }
func (p *Parser) parseWhile() ast.Stmt {
	pos := p.consume(mocker_lex.TypeKW_WHILE).Pos
	p.consume(mocker_lex.TypeSEP_LPAREN)
	cond := p.parseExpr()
	p.consume(mocker_lex.TypeSEP_RPAREN)
	p.consume(mocker_lex.TypeSEP_LBRACE)
	body := &ast.BlockStmt{PosBase: ast.PosBase{P: pos}, Stmts: p.parseStmts()}
	p.consume(mocker_lex.TypeSEP_RBRACE)
	return &ast.WhileStmt{PosBase: ast.PosBase{P: pos}, Cond: cond, Body: body}
}

// parseReturn return v
func (p *Parser) parseReturn() ast.Stmt {
	pos := p.consume(mocker_lex.TypeKW_RETURN).Pos
	if p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeSEP_SEMI, mocker_lex.TypeEOF) {
		return &ast.ReturnStmt{PosBase: ast.PosBase{P: pos}}
	}
	v := p.parseExpr()
	return &ast.ReturnStmt{PosBase: ast.PosBase{P: pos}, Value: v}
}
