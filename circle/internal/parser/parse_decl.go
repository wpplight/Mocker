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

	// Style 2 语法糖：<edge_name> { body } —— 编译器从 body 推导 src/dst
	if p.isEdgeDeclSugarSig() {
		return p.parseEdgeDeclSugar(pos)
	}

	name := p.consume(mocker_lex.TypeID).Value

	// main 是语言保留字：当 NodeDecl 解析（简化版的特殊节点）
	// main { ... } body 包含 InstanceDecl + EdgeConnDecl，由 IR 层 lowerMainTopology 处理
	if name == "main" {
		pos := p.peek().Pos // { 的位置
		_ = pos
		return p.parseStructBody(p.peek().Pos, "main", false, ast.StructKindNode)
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

// isEdgeDeclSugarSig 检查 Style 2 语法糖：<edge_name> { body }
//
// 形式：< IDENT/EDGE_NAME/CALL > { （无 src/dst）
func (p *Parser) isEdgeDeclSugarSig() bool {
	if p.peek().Type != mocker_lex.TypeOP_LT {
		return false
	}
	// < edge_name
	n1 := p.peekN(1).Type
	if n1 != mocker_lex.TypeEDGE_NAME && n1 != mocker_lex.TypeID && n1 != mocker_lex.TypeCALL {
		return false
	}
	// edge_name >
	if p.peekN(2).Type != mocker_lex.TypeOP_GT {
		return false
	}
	// > {
	if p.peekN(3).Type != mocker_lex.TypeSEP_LBRACE {
		return false
	}
	return true
}

// parseEdgeDeclSugar Style 2 语法糖：<edge_name> { body }
//
// 例：
//
//	<write> {
//	    Println.fid >> io.write.fid
//	    Println.data >> io.write.data
//	}
//
// Src 和 Dst 暂时为空，由 semantic 层的 InferEdgeEndpoints 从 body 推导
func (p *Parser) parseEdgeDeclSugar(pos ast.Pos) *ast.EdgeDecl {
	p.consume(mocker_lex.TypeOP_LT)

	var edge string
	switch p.peek().Type {
	case mocker_lex.TypeEDGE_NAME, mocker_lex.TypeID, mocker_lex.TypeCALL:
		edge = p.consume(p.peek().Type).Value
	default:
		p.errorf("expected edge name, got %s", p.peek().Type)
		return nil
	}
	p.consume(mocker_lex.TypeOP_GT)
	p.consume(mocker_lex.TypeSEP_LBRACE)

	body := p.parseStmts()
	p.consume(mocker_lex.TypeSEP_RBRACE)

	return &ast.EdgeDecl{
		PosBase: ast.PosBase{P: pos},
		Src:     "", // 由 InferEdgeEndpoints 推导
		Edge:    edge,
		Dst:     "", // 由 InferEdgeEndpoints 推导
		Body:    body,
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

// parseStructMember 5 种合法形式：
//  0. >> str hey        → PortDecl (入度)
//  1. str Domain        → FieldDecl（typed，无 init）
//     1.5 str name = expr  → VarDecl 显式类型（用 = 号，类型已写）
//  2. name := expr      → VarDecl 类型推断（用 := 号，类型从 expr 推）
//  3. h / h >> / h>>msg → FlowDecl（出度，必须有名字！）
//
// := 和 = 语义不同（用户拍板）：
//
//	:= → 类型推断（不能有显式类型）
//	=  → 显式类型（必须先写 type）
func (p *Parser) parseStructMember() ast.StructMember {
	pos := p.peek().Pos
	tok := p.peek()

	// 形式 0：>> 开头（入度声明）
	if tok.Type == mocker_lex.TypeOP_RRARROW {
		return p.parsePortDecl()
	}

	// 形式 4 + 5（main 节点专用）：InstanceDecl / EdgeConnDecl
	// 必须在 typed field 检查之前！否则 `hello happy` 会被当成 FieldDecl
	if tok.Type == mocker_lex.TypeID {
		// 子形式 5：`src <edge> dst` → EdgeConnDecl
		if p.peekN(1).Type == mocker_lex.TypeOP_LT {
			return p.parseEdgeConnDecl()
		}
		// 子形式 4：`typeName varName` → InstanceDecl（peekN(1) 是 IDENT 或 CALL）
		if p.peekN(1).Type == mocker_lex.TypeID {
			return p.parseInstanceDecl()
		}
	}
	// CALL 形式：`pkg.Node varName` → InstanceDecl（跨包）
	if tok.Type == mocker_lex.TypeCALL {
		if p.peekN(1).Type == mocker_lex.TypeID {
			return p.parseInstanceDecl()
		}
	}

	// 形式 1：typed field（IDENT 是类型关键字或已知类型）
	if isTypeStart(tok.Type) && p.isTypedFieldStart() {
		typ := p.parseTypeRef()
		if p.match(mocker_lex.TypeID) {
			name := p.consume(mocker_lex.TypeID).Value
			// 形式 1.5：name 后面是 = （不用 :=） → VarDecl 显式类型
			// 用户拍板 := = 显式类型 严格区分
			if p.peek().Type == mocker_lex.TypeOP_ASSIGN {
				p.consume(mocker_lex.TypeOP_ASSIGN)
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
			// 形式 1.6：type name >>  →  typed local + export（具名出度）
			// 例：Response response >>  →  类型 Response 的 response 出度
			if p.peek().Type == mocker_lex.TypeOP_RRARROW {
				p.consume(mocker_lex.TypeOP_RRARROW)
				flow := p.parseFlowChain()
				return &ast.VarDecl{
					PosBase: ast.PosBase{P: pos},
					Name:    name,
					Flow:    flow,
				}
			}
			return &ast.FieldDecl{PosBase: ast.PosBase{P: pos}, Type: typ, Name: name}
		}
		// typed field 后没跟 IDENT，回退
		return nil
	}

	// 形式 2：type-inferred VarDecl，必须用 := （不用 = ）
	// := 类型推断，类型从 expr 推论
	if tok.Type == mocker_lex.TypeID && p.peekN(1).Type == mocker_lex.TypeOP_DEFINE {
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
		// 子形式 5（main 节点专用）：EdgeConnDecl `src <edge> dst`
		// peekN(1) 必须是 < （OP_LT），且 peekN(3) 必须是 IDENT/CALL（dst）
		if p.peekN(1).Type == mocker_lex.TypeOP_LT {
			return p.parseEdgeConnDecl()
		}
		// 子形式 4（main 节点专用）：InstanceDecl `typeName varName`
		// peekN(1) 必须是 IDENT（var name）
		if p.peekN(1).Type == mocker_lex.TypeID {
			return p.parseInstanceDecl()
		}
		return p.parseFlowDecl()
	}

	// 形式 4 跨包：InstanceDecl `pkg.Node varName`
	// 注：理论上 CALL 形式应该在 IDENT 分支处理（CALL 也是以 IDENT 开头）
	// 上面 check `tok.Type == TypeCALL` 后下面也会到这里
	if tok.Type == mocker_lex.TypeCALL {
		// 形式 4 跨包：`stdio.Println p` → InstanceDecl
		if p.peekN(1).Type == mocker_lex.TypeID {
			return p.parseInstanceDecl()
		}
	}

	// 形式 4 + 5（main 节点专用）：InstanceDecl / EdgeConnDecl
	// 我们在 parseStructMember 末尾已经检查过，这里兜底（实际上不会到这里）

	p.errorf("unexpected token %s in struct body", tok.Type)
	p.pos++
	return nil
}

// parseInstanceDecl 解析 `typeName varName;`（main 节点专用）
//
// 例：`hello happy;`         → 用 @hello 类型声明 happy 实例
//
//	`stdio.Println p;`     → 用 stdio.Println 类型声明 p 实例
//
// ; 可选（用户友好）
func (p *Parser) parseInstanceDecl() ast.StructMember {
	pos := p.peek().Pos
	typeName := p.consume(p.peek().Type).Value // IDENT 或 CALL
	varName := p.consume(mocker_lex.TypeID).Value
	if p.match(mocker_lex.TypeSEP_SEMI) {
		p.consume(mocker_lex.TypeSEP_SEMI)
	}
	return &ast.InstanceDecl{
		PosBase: ast.PosBase{P: pos},
		Type:    typeName,
		Name:    varName,
	}
}

// parseEdgeConnDecl 解析 `src <edge> dst`（main 节点专用）
//
// 例：`happy <out> p`         → 在 happy 和 p 之间建 <out> 边
//
// edge name 可以是 EDGE_NAME（含 - 的 out-no-co）或 IDENT（普通的 out）
func (p *Parser) parseEdgeConnDecl() ast.StructMember {
	pos := p.peek().Pos
	srcName := p.consume(mocker_lex.TypeID).Value
	p.consume(mocker_lex.TypeOP_LT)
	// edge name: EDGE_NAME（含 -）或 IDENT（普通的 out）
	var edgeName string
	switch p.peek().Type {
	case mocker_lex.TypeEDGE_NAME, mocker_lex.TypeID:
		edgeName = p.consume(p.peek().Type).Value
	default:
		p.errorf("expected edge name, got %s", p.peek().Type)
		p.pos++
		return nil
	}
	p.consume(mocker_lex.TypeOP_GT)
	dstName := p.consume(mocker_lex.TypeID).Value
	return &ast.EdgeConnDecl{
		PosBase: ast.PosBase{P: pos},
		Src:     srcName,
		Edge:    edgeName,
		Dst:     dstName,
	}
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

// parseVarDeclInStruct 节点体里的 h := expr  → VarDecl（类型推断版）
// 必须用 := （不用 = ，= 是显式类型专用）
func (p *Parser) parseVarDeclInStruct() *ast.VarDecl {
	pos := p.peek().Pos
	name := p.consume(mocker_lex.TypeID).Value
	// 必须是 := （不是 = ）
	if p.peek().Type != mocker_lex.TypeOP_DEFINE {
		p.errorf("expected ':=' for type-inferred var decl, got %s (use 'type name = expr' for explicit type)", p.peek().Type)
	}
	p.consume(mocker_lex.TypeOP_DEFINE)
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
