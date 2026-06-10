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
//   IDENT ":=" EXPR       → VarDecl
//   IDENT "," IDENT ":="  → AssignStmt
//   IDENT ">>" ">>" ...   → FlowFanout  (并发分支，源后面跟两个 >>)
//   IDENT ">>" ...        → FlowStmt    (单链)
//   IDENT <next IDENT>    → Connection  (IDENT 是 node 起点)
func (p *Parser) parseStmtDispatch() ast.Stmt {
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
		mocker_lex.TypeSEP_LBRACE, mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeSEP_SEMI,
		mocker_lex.TypeEOF:
		return false
	}
	return true
}

// parseVarDeclOrAssign x := y  /  x = y  /  a, b := y  /  a, b = y
// := 和 = 在 parser 层等价
func (p *Parser) parseVarDeclOrAssign() ast.Stmt {
	pos := p.peek().Pos
	first := p.consume(mocker_lex.TypeID).Value

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

// parseIf if cond { ... } else { ... }
func (p *Parser) parseIf() ast.Stmt {
	pos := p.consume(mocker_lex.TypeKW_IF).Pos
	cond := p.parseExpr()
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

// parseReturn return v
func (p *Parser) parseReturn() ast.Stmt {
	pos := p.consume(mocker_lex.TypeKW_RETURN).Pos
	if p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeSEP_SEMI, mocker_lex.TypeEOF) {
		return &ast.ReturnStmt{PosBase: ast.PosBase{P: pos}}
	}
	v := p.parseExpr()
	return &ast.ReturnStmt{PosBase: ast.PosBase{P: pos}, Value: v}
}
