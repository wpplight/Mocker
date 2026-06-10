package parser

import (
	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseTopDecl IDENT 开头的顶层声明分发
//   - IDENT "{"           → StructDecl
//   - IDENT "<" EDGE_NAME ">" IDENT "{" → EdgeDecl
//   - IDENT "(" ... ")"   → FuncDecl
//   - IDENT "<" ... ">"   → FuncDecl (尖括号签名)
//   - "main" 是保留字，永远是 FuncDecl（无参数 + { body }）
func (p *Parser) parseTopDecl() ast.Decl {
	pos := p.peek().Pos
	name := p.consume(mocker_lex.TypeID).Value

	// main 是语言保留字，永远是 FuncDecl
	if name == "main" {
		return p.parseFuncDecl("main", false, false)
	}

	switch p.peek().Type {
	case mocker_lex.TypeSEP_LBRACE:
		return p.parseStructBody(pos, name, false, ast.StructKindPlain)

	case mocker_lex.TypeOP_LT:
		// 试识别 hello < EdgeName > IDENT {  模式
		if p.isEdgeDeclSig() {
			return p.parseEdgeDecl(pos, name)
		}
		// 否则是尖括号签名 FuncDecl
		return p.parseFuncDecl(name, true, false)

	case mocker_lex.TypeSEP_LPAREN:
		return p.parseFuncDecl(name, false, false)

	default:
		p.errorf("expected '{', '<' or '(' after top-level IDENT %q, got %s",
			name, p.peek().Type)
		return nil
	}
}

//	isEdgeDeclSig peek 是否匹配：< EdgeName > IDENT {
//	  EdgeName 可以是 EDGE_NAME（含 -）或普通 IDENT
//	  IDENT（dst）也支持 CALL（如 io.write）
func (p *Parser) isEdgeDeclSig() bool {
	if p.peek().Type != mocker_lex.TypeOP_LT {
		return false
	}
	n1 := p.peekN(1).Type
	if n1 != mocker_lex.TypeEDGE_NAME && n1 != mocker_lex.TypeID && n1 != mocker_lex.TypeCALL {
		return false
	}
	if p.peekN(2).Type != mocker_lex.TypeOP_GT {
		return false
	}
	// dst 支持 IDENT 和 CALL
	n3 := p.peekN(3).Type
	if n3 != mocker_lex.TypeID && n3 != mocker_lex.TypeCALL {
		return false
	}
	return p.peekN(4).Type == mocker_lex.TypeSEP_LBRACE
}

// parseStruct 处理 @name / name / <name> 开头
func (p *Parser) parseStruct() *ast.StructDecl {
	pos := p.peek().Pos
	exported := false

	if p.match(mocker_lex.TypeSEP_AT) {
		p.consume(mocker_lex.TypeSEP_AT)
		exported = true
	}

	var name string
	kind := ast.StructKindPlain

	switch p.peek().Type {
	case mocker_lex.TypeID, mocker_lex.TypeCALL:
		name = p.consume(p.peek().Type).Value
	case mocker_lex.TypeOP_LT:
		kind = ast.StructKindEdge
		p.consume(mocker_lex.TypeOP_LT)
		name = p.consume(mocker_lex.TypeEDGE_NAME).Value
		p.consume(mocker_lex.TypeOP_GT)
	default:
		p.errorf("expected struct name, got %s", p.peek().Type)
		return nil
	}

	return p.parseStructBody(pos, name, exported, kind)
}

// parseStructBody 解析 { Members }
func (p *Parser) parseStructBody(pos ast.Pos, name string, exported bool, kind ast.StructKind) *ast.StructDecl {
	p.consume(mocker_lex.TypeSEP_LBRACE)

	var members []ast.StructMember
	loopGuard := 0
	for !p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
		p.skipTrivial()
		if p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
			break
		}
		m := p.parseStructMember()
		if m != nil {
			members = append(members, m)
		}
		loopGuard++
		if loopGuard > 1000 {
			p.errorf("infinite loop in struct body at token %v", p.peek())
			break
		}
	}
	p.consume(mocker_lex.TypeSEP_RBRACE)

	// 启发式：含 PortDecl / FlowDecl 视为节点
	if kind == ast.StructKindPlain {
		for _, m := range members {
			switch m.(type) {
			case *ast.PortDecl, *ast.FlowDecl, *ast.VarDecl:
				kind = ast.StructKindNode
			}
			if kind != ast.StructKindPlain {
				break
			}
		}
	}

	return &ast.StructDecl{
		PosBase:  ast.PosBase{P: pos},
		Kind:     kind,
		Exported: exported,
		Name:     name,
		Members:  members,
	}
}

// parseStructMember 4 种合法形式：
//  0. >> str hey        → PortDecl
//  1. str Domain        → FieldDecl（typed）
//  2. h := "hi"         → VarDecl
//  3. h / h >> / h>>msg → FlowDecl
func (p *Parser) parseStructMember() ast.StructMember {
	pos := p.peek().Pos
	tok := p.peek()

	// 形式 0：>> 开头
	if tok.Type == mocker_lex.TypeOP_RRARROW {
		return p.parsePortDecl()
	}

	// 形式 1：typed field（IDENT 是类型关键字或已知类型）
	// 也支持 1.5：typed var decl   `str name := expr`  /  `str name = expr`
	if isTypeStart(tok.Type) && p.isTypedFieldStart() {
		typ := p.parseTypeRef()
		if p.match(mocker_lex.TypeID) {
			name := p.consume(mocker_lex.TypeID).Value
			// 1.5：name 后面是 := 或 = → 改成 VarDecl with explicit type
			// 此时当前 token 已经是 ASSIGN op（peek()），用 isAssignOp 直接看
			if isAssignOp(p.peek().Type) {
				p.consumeAssign()
				init := p.parseExpr()
				var flow *ast.FlowChain
				if p.match(mocker_lex.TypeOP_RRARROW) {
					flow = p.parseFlowChain()
				}
				return &ast.VarDecl{
					PosBase: ast.PosBase{P: pos},
					Name:    name,
					Init:    init,
					Flow:    flow,
				}
			}
			return &ast.FieldDecl{PosBase: ast.PosBase{P: pos}, Type: typ, Name: name}
		}
		// typed field 后没跟 IDENT，回退
		return nil
	}

	// 形式 2：IDENT := EXPR  /  IDENT = EXPR
	// := 和 = 在 parser 层等价（都是变量声明）
	if tok.Type == mocker_lex.TypeID && p.matchAssign() {
		return p.parseVarDeclInStruct()
	}

	// 形式 2.5：EXPR >>  表达式流出糖
	// 触发条件：IDENT 后跟二元操作符 (+ - * / .) 或成员访问，整体以 >> 收尾
	// 例：msg+nl >>   →  __ce_concat_0 := msg+nl  ;  __ce_concat_0 >>
	if tok.Type == mocker_lex.TypeID && isExprContinuation(p.peekN(1).Type) {
		return p.parseExprOutMember()
	}

	// 形式 3：IDENT（裸字段，可带 >> 链）
	if tok.Type == mocker_lex.TypeID {
		return p.parseFlowDecl()
	}

	p.errorf("unexpected token %s in struct body", tok.Type)
	p.pos++
	return nil
}

// skipTrivial 跳过 SEMI / 空行 等
func (p *Parser) skipTrivial() {
	for p.match(mocker_lex.TypeSEP_SEMI) {
		p.consume(mocker_lex.TypeSEP_SEMI)
	}
}
func isTypeStart(t mocker_lex.Type) bool {
	switch t {
	case mocker_lex.TypeTYPE_STR,
		mocker_lex.TypeTYPE_NUM,
		mocker_lex.TypeTYPE_BOOL,
		mocker_lex.TypeTYPE_BYTE,
		mocker_lex.TypeTYPE_ANY,
		mocker_lex.TypeID,     // 用户自定义类型
		mocker_lex.TypeOP_MUL: // 指针前缀 *
		return true
	}
	return false
}

// isTypedFieldStart peek 当前是 type，下一个是 IDENT
func (p *Parser) isTypedFieldStart() bool {
	if !isTypeStart(p.peek().Type) {
		return false
	}
	// 跳过可能的 *[] 修饰，看下一个是不是 IDENT
	next := p.peekN(1).Type
	return next == mocker_lex.TypeID
}

// parsePortDecl >> str hey
func (p *Parser) parsePortDecl() *ast.PortDecl {
	pos := p.consume(mocker_lex.TypeOP_RRARROW).Pos
	typ := p.parseTypeRef()
	if !p.match(mocker_lex.TypeID) {
		p.errorf("expected port name, got %s", p.peek().Type)
		return nil
	}
	name := p.consume(mocker_lex.TypeID).Value

	return &ast.PortDecl{
		PosBase: ast.PosBase{P: pos},
		Type:    typ,
		Name:    name,
		// Body: 暂留空，等 lexer 支持 INDENT/DEDENT 后激活
	}
}

// parseVarDeclInStruct 节点体里的 h := "hi"  /  h = "hi"  → VarDecl
// := 和 = 在 parser 层等价
func (p *Parser) parseVarDeclInStruct() *ast.VarDecl {
	pos := p.peek().Pos
	name := p.consume(mocker_lex.TypeID).Value
	p.consumeAssign()
	init := p.parseExpr()

	decl := &ast.VarDecl{
		PosBase: ast.PosBase{P: pos},
		Name:    name,
		Init:    init,
	}

	// 可选 >> 链
	if p.match(mocker_lex.TypeOP_RRARROW) {
		decl.Flow = p.parseFlowChain()
	}
	return decl
}

// parseFlowDecl 裸字段 / 导出字段：h / h>> / h>>msg
func (p *Parser) parseFlowDecl() *ast.FlowDecl {
	pos := p.peek().Pos
	name := p.consume(mocker_lex.TypeID).Value
	decl := &ast.FlowDecl{
		PosBase: ast.PosBase{P: pos},
		Head:    name,
	}
	if p.match(mocker_lex.TypeOP_RRARROW) {
		decl.Chain = p.parseFlowChain()
	}
	return decl
}

// isExprContinuation IDENT 后能接"表达式延续"的 token 吗？
// 用来在 struct body 里识别 EXPR >> 糖写法（msg+nl >>）
// 涵盖：二元操作符、成员访问、函数调用
func isExprContinuation(t mocker_lex.Type) bool {
	switch t {
	case mocker_lex.TypeOP_ADD, mocker_lex.TypeOP_SUB,
		mocker_lex.TypeOP_MUL, mocker_lex.TypeOP_DIV,
		mocker_lex.TypeSEP_DOT,
		mocker_lex.TypeSEP_LPAREN: // 函式调用也算
		return true
	}
	return false
}

// parseExprOutMember 解析 EXPR >> 糖写法
//
// 触发场景：msg+nl >>  /  msg+nl+other >>  /  obj.field >>  /  fn(x) >>
// 语义：自动创建一个合成变量 __ce_concat_N，把表达式赋给它，再标为出
//
//	编译期/IR 阶段会做"如果是两个 str 就 concat、否则按二元 op 处理"
//
// AST 展开（伪）：
//
//	msg+nl >>
//	  ↓
//	VarDecl{ Name: "__ce_concat_0", Init: BinaryExpr(msg, +, nl), Flow: nil }
//	（再后面是 >> chain，由 parseFlowChain 处理）
func (p *Parser) parseExprOutMember() ast.StructMember {
	pos := p.peek().Pos
	expr := p.parseExpr() // 解析 msg+nl，parseExpr 在 >> 处自然停止

	if !p.match(mocker_lex.TypeOP_RRARROW) {
		p.errorf("expected '>>' after expression in struct body, got %s", p.peek().Type)
		return nil
	}
	p.consume(mocker_lex.TypeOP_RRARROW)

	var flow *ast.FlowChain
	if p.match(mocker_lex.TypeOP_RRARROW) {
		flow = p.parseFlowChain()
	}

	// 生成合成变量
	name := p.nextSynthetic("concat")
	return &ast.VarDecl{
		PosBase: ast.PosBase{P: pos},
		Name:    name,
		Init:    expr,
		Flow:    flow,
	}
}

// parseEdgeDecl hello <out-no-co> say { ... }
// dst 现在接受 IDENT 或 CALL：
//
//	hello <out> say         → dst="say"
//	Println <write> io.write → dst="io.write"（CALL）
func (p *Parser) parseEdgeDecl(pos ast.Pos, src string) *ast.EdgeDecl {
	p.consume(mocker_lex.TypeOP_LT)
	// 边名：EDGE_NAME（含 - 的 out-no-co）或 IDENT（普通的 out）/ CALL（stdio.foo）
	var edge string
	switch p.peek().Type {
	case mocker_lex.TypeEDGE_NAME, mocker_lex.TypeID, mocker_lex.TypeCALL:
		edge = p.consume(p.peek().Type).Value
	default:
		p.errorf("expected edge name, got %s", p.peek().Type)
		// 防死循环
		if p.peek().Type != mocker_lex.TypeOP_GT {
			p.pos++
		}
	}
	p.consume(mocker_lex.TypeOP_GT)
	// dst：IDENT（say）/ CALL（io.write）
	var dst string
	switch p.peek().Type {
	case mocker_lex.TypeID, mocker_lex.TypeCALL:
		dst = p.consume(p.peek().Type).Value
	default:
		p.errorf("expected dst node (IDENT or CALL), got %s", p.peek().Type)
		// 防死循环
		if p.peek().Type != mocker_lex.TypeSEP_LBRACE {
			p.pos++
		}
	}
	p.consume(mocker_lex.TypeSEP_LBRACE)

	body := p.parseStmts()
	p.consume(mocker_lex.TypeSEP_RBRACE)

	return &ast.EdgeDecl{
		PosBase: ast.PosBase{P: pos},
		Src:     src,
		Edge:    edge,
		Dst:     dst,
		Body:    body,
	}
}
