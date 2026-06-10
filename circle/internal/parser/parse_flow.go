package parser

import (
	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseConnection 解析图连接：
//
//	hello <out> stdio.Println
//	hello <out-no-co> say
//	<stdio.Println>  (单 EdgeRef/CallRef 也允许)
func (p *Parser) parseConnection() *ast.Connection {
	pos := p.peek().Pos
	conn := &ast.Connection{PosBase: ast.PosBase{P: pos}}

	for p.isConnectionHopStart() {
		hop := p.parseConnectionHop()
		if hop == nil {
			break
		}
		conn.Hops = append(conn.Hops, hop)
	}
	return conn
}

func (p *Parser) isConnectionHopStart() bool {
	switch p.peek().Type {
	case mocker_lex.TypeOP_LT,
		mocker_lex.TypeID,
		mocker_lex.TypeCALL,
		mocker_lex.TypeEDGE_NAME:
		return true
	}
	return false
}

func (p *Parser) parseConnectionHop() ast.ConnectionHop {
	pos := p.peek().Pos
	switch p.peek().Type {
	case mocker_lex.TypeOP_LT:
		p.consume(mocker_lex.TypeOP_LT)
		// 边名：EDGE_NAME（含 - 的 out-no-co）或 IDENT（普通的 out）/ CALL（stdio.foo）
		var name string
		switch p.peek().Type {
		case mocker_lex.TypeEDGE_NAME, mocker_lex.TypeID, mocker_lex.TypeCALL:
			name = p.consume(p.peek().Type).Value
		default:
			p.errorf("expected edge name, got %s", p.peek().Type)
			if p.peek().Type != mocker_lex.TypeOP_GT {
				p.pos++
			}
		}
		p.consume(mocker_lex.TypeOP_GT)
		return &ast.EdgeRef{PosBase: ast.PosBase{P: pos}, Name: name}

	case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
		// 解析 primary expr（ident / member / call）
		e := p.parsePrimary()
		switch v := e.(type) {
		case *ast.CallExpr:
			return &ast.CallRef{PosBase: ast.PosBase{P: pos}, Fn: v.Fn, Args: v.Args}
		case *ast.MemberExpr:
			// stdio.Println 作为 hop
			return &ast.CallRef{PosBase: ast.PosBase{P: pos}, Fn: v}
		case *ast.IdentExpr:
			return &ast.NodeRef{PosBase: ast.PosBase{P: pos}, Name: v.Name}
		}
	}
	return nil
}

// parseFlowStmt 解析单链数据流：hello >> out >> stdio.Println
//
// 关键约束：本函数只在「源后面紧跟 1 个 >>（不跟第二个 >>）」时被调用。
// 「源后面跟 >> >>」这种 fan-out 模式由 parseStmtDispatch 单独路由到 parseFlowFanout。
// 因此本函数在循环里看到第二个 >> 时要立刻停，让外层把控制权交回 parseStmts。
func (p *Parser) parseFlowStmt() *ast.FlowStmt {
	pos := p.peek().Pos
	first := p.parseFlowStep()
	steps := []*ast.FlowStep{first}

	loopGuard := 0
	for p.match(mocker_lex.TypeOP_RRARROW) {
		startPos := p.pos
		p.consume(mocker_lex.TypeOP_RRARROW)

		// 见到第二个 >> 立即停：那是 fan-out 边界，不归本函数管
		if p.match(mocker_lex.TypeOP_RRARROW) {
			break
		}
		if !p.isFlowTargetStart() {
			break
		}
		steps = append(steps, p.parseFlowStep())

		if p.pos == startPos {
			p.errorf("parseFlowStmt stuck, advancing")
			p.pos++
		}
		loopGuard++
		if loopGuard > 1000 {
			p.errorf("infinite loop in parseFlowStmt")
			break
		}
	}
	return &ast.FlowStmt{PosBase: ast.PosBase{P: pos}, Steps: steps}
}

// parseFlowCont 续行：>>say.hay  或  >>say.hay>>stdio.b
//
// 由 parseStmt 在看到「行首是 >>」（且 dispatcher 没把它识别为 fan-out）时调用，
// 语义是「接着上一条 FlowStmt 末尾的裸 >>，把 chain 续上」。
// 同样：见到连续两个 >> 就停。
func (p *Parser) parseFlowCont() *ast.FlowCont {
	pos := p.consume(mocker_lex.TypeOP_RRARROW).Pos
	steps := []*ast.FlowStep{p.parseFlowStep()}

	for p.match(mocker_lex.TypeOP_RRARROW) {
		p.consume(mocker_lex.TypeOP_RRARROW)
		// 见到第二个 >> 立即停：fan-out 边界
		if p.match(mocker_lex.TypeOP_RRARROW) {
			break
		}
		if !p.isFlowTargetStart() {
			break
		}
		steps = append(steps, p.parseFlowStep())
	}
	return &ast.FlowCont{PosBase: ast.PosBase{P: pos}, Steps: steps}
}

// parseFlowFanout 解析 fan-out 并发分支：
//
//	hello.h >>
//	  >>say.hay
//	  >>say.my>>stdio.b
//	  >>say.world
//
// 触发条件：parseStmtDispatch 看到 IDENT >> >>（3 token lookahead）
// 把第一个 FlowStep 的 Target 抽出来当 Src，剩下的每条 >> chain 各自成一个 FlowBranch。
func (p *Parser) parseFlowFanout() *ast.FlowFanout {
	pos := p.peek().Pos
	first := p.parseFlowStep()

	if first == nil || first.Target == nil {
		p.errorf("fan-out: missing source step")
		return &ast.FlowFanout{PosBase: ast.PosBase{P: pos}, Src: nil}
	}
	src := first.Target

	// 第一个 >> 必须存在（dispatcher 已保证）
	if !p.match(mocker_lex.TypeOP_RRARROW) {
		p.errorf("fan-out: expected '>>' after source")
		return &ast.FlowFanout{PosBase: ast.PosBase{P: pos}, Src: src}
	}
	p.consume(mocker_lex.TypeOP_RRARROW)

	// 第二个 >> 也必须存在（dispatcher 已保证）
	if !p.match(mocker_lex.TypeOP_RRARROW) {
		p.errorf("fan-out: expected '>>' to open branch, got %s", p.peek().Type)
		return &ast.FlowFanout{PosBase: ast.PosBase{P: pos}, Src: src}
	}

	var branches []*ast.FlowBranch
	loopGuard := 0
	for p.match(mocker_lex.TypeOP_RRARROW) {
		p.consume(mocker_lex.TypeOP_RRARROW)

		branch := p.parseFlowBranch()
		if branch != nil {
			branches = append(branches, branch)
		}

		loopGuard++
		if loopGuard > 1000 {
			p.errorf("infinite loop in parseFlowFanout")
			break
		}
	}
	return &ast.FlowFanout{PosBase: ast.PosBase{P: pos}, Src: src, Branches: branches}
}

// parseFlowBranch 解析 fan-out 的 1 条分支：>>say.hay  或  >>say.hay>>stdio.b
// 进入时已消费掉分支开头的 >>。
// 关键规则：
//   - 同行内的 >> 是 chain 延续（如 >>say.hay>>stdio.b）
//   - 跨行的 >> 是 fan-out 下一条分支的开头，本分支停下
func (p *Parser) parseFlowBranch() *ast.FlowBranch {
	pos := p.peek().Pos
	first := p.parseFlowStep()
	if first == nil {
		return nil
	}
	steps := []*ast.FlowStep{first}
	lastLine := first.Pos().Line

	for p.match(mocker_lex.TypeOP_RRARROW) {
		// 跨行：下一条 fan-out 分支的开头，停下交还给 parseFlowFanout
		if p.peek().Pos.Line != lastLine {
			break
		}
		p.consume(mocker_lex.TypeOP_RRARROW)

		// 见到第二个 >>：fan-out 下一条分支的开头，停下来交还给 parseFlowFanout
		if p.match(mocker_lex.TypeOP_RRARROW) {
			break
		}
		if !p.isFlowTargetStart() {
			break
		}
		steps = append(steps, p.parseFlowStep())
		lastLine = steps[len(steps)-1].Pos().Line
	}
	return &ast.FlowBranch{PosBase: ast.PosBase{P: pos}, Steps: steps}
}

// parseFlowChain 字段后的导出链
// 第一个 >> 必须存在；调用方要保证
// 支持三种形式：
//   - ">>"            （裸 >>，无 target）
//   - ">> msg"        （单 target，可选重命名）
//   - ">> msg >> tgt" （多 target 链）
func (p *Parser) parseFlowChain() *ast.FlowChain {
	chain := &ast.FlowChain{}
	loopGuard := 0
	for p.match(mocker_lex.TypeOP_RRARROW) {
		startPos := p.pos
		p.consume(mocker_lex.TypeOP_RRARROW)

		// 多行续行：跳过连续的 >>(没有 target 紧跟的情况)
		for p.match(mocker_lex.TypeOP_RRARROW) {
			p.consume(mocker_lex.TypeOP_RRARROW)
		}

		// 如果没有 target（如 "h >>" 在 struct body 末尾），跳出
		if !p.isFlowTargetStart() {
			break
		}

		step := p.parseFlowStep()
		// 可选重命名：>> msg（msg 必须是简单 IDENT，不能是 CALL）
		if p.match(mocker_lex.TypeOP_RRARROW) && p.peekN(1).Type == mocker_lex.TypeID {
			p.consume(mocker_lex.TypeOP_RRARROW)
			step.As = p.consume(mocker_lex.TypeID).Value
		}
		chain.Steps = append(chain.Steps, step)

		// 防死循环
		if p.pos == startPos {
			p.errorf("parseFlowChain stuck at token %v, advancing", p.peek())
			p.pos++
		}
		loopGuard++
		if loopGuard > 1000 {
			p.errorf("infinite loop in parseFlowChain")
			break
		}
	}
	return chain
}

// isFlowTargetStart peek 是否是合法 flow target 的起始
func (p *Parser) isFlowTargetStart() bool {
	switch p.peek().Type {
	case mocker_lex.TypeSTRING,
		mocker_lex.TypeID,
		mocker_lex.TypeCALL,
		mocker_lex.TypeEDGE_NAME:
		return true
	}
	return false
}

// parseFlowStep 一步：target + 可选 rename
//
// target 可以是：
//   - 字符串字面量  "hello"
//   - 单个标识符    foo
//   - 成员链        sysio.write  /  pkg.foo.bar
//   - 函式调用      sysio.write(fid)   ← 用于「data >> sysio.write(fid)」这种
//                                      内层节点带参数的形式
//   - 任意表达式    msg+nl  /  a*b  /  (a+b)*c   ← 走 FlowExpr（fallback）
func (p *Parser) parseFlowStep() *ast.FlowStep {
	pos := p.peek().Pos
	step := &ast.FlowStep{PosBase: ast.PosBase{P: pos}}

	switch p.peek().Type {
	case mocker_lex.TypeSTRING:
		step.Target = &ast.FlowLiteral{
			PosBase: ast.PosBase{P: pos},
			Value:   p.consume(mocker_lex.TypeSTRING).Value,
		}
	case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
		// 用 parseExpr 解析（包含二元 op），简单 ident / member / call 走 FlowIdent
		// 复杂表达式（BinaryExpr / UnaryExpr / 括号）走 FlowExpr fallback
		e := p.parseExpr()
		switch v := e.(type) {
		case *ast.IdentExpr:
			step.Target = &ast.FlowIdent{
				PosBase: ast.PosBase{P: pos},
				Chain:   []string{v.Name},
			}
		case *ast.MemberExpr:
			step.Target = &ast.FlowIdent{
				PosBase: ast.PosBase{P: pos},
				Chain:   flattenMember(v),
			}
		case *ast.CallExpr:
			// 函式调用作为 flow target：sysio.write(fid) / stdio.Println(x, y)
			step.Target = &ast.FlowIdent{
				PosBase: ast.PosBase{P: pos},
				Chain:   flattenMemberExprChain(v.Fn),
				Call:    v.Args,
			}
		default:
			// 复杂表达式（msg+nl、a*b、(a+b)*c 等）→ FlowExpr
			step.Target = &ast.FlowExpr{
				PosBase: ast.PosBase{P: pos},
				Expr:    e,
			}
		}
	default:
		p.errorf("expected flow target, got %s", p.peek().Type)
	}

	// 可选重命名：>> msg
	if p.match(mocker_lex.TypeOP_RRARROW) && p.peekN(1).Type == mocker_lex.TypeID {
		p.consume(mocker_lex.TypeOP_RRARROW)
		step.As = p.consume(mocker_lex.TypeID).Value
	}
	return step
}

// flattenMemberExprChain 把 MemberExpr / IdentExpr 链拍平成 []string
// 与 flattenMember 不同：保留顺序（sysio.write → ["sysio", "write"]）
func flattenMemberExprChain(e ast.Expr) []string {
	var out []string
	cur := e
	for {
		switch v := cur.(type) {
		case *ast.MemberExpr:
			out = append([]string{v.Name}, out...)
			cur = v.Obj
		case *ast.IdentExpr:
			out = append([]string{v.Name}, out...)
			return out
		default:
			return out
		}
	}
}

// flattenMember 把 MemberExpr 链拍平成字符串切片
//
//	a.b.c  →  ["a","b","c"]
func flattenMember(m *ast.MemberExpr) []string {
	var out []string
	cur := ast.Expr(m)
	for {
		switch v := cur.(type) {
		case *ast.MemberExpr:
			out = append([]string{v.Name}, out...)
			cur = v.Obj
		case *ast.IdentExpr:
			out = append([]string{v.Name}, out...)
			return out
		}
	}
}
