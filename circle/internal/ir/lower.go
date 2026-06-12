// Package ir Lower pass：AST + Semantic → IR
//
// 流程：
//  1. 遍历 WorkspaceResult 的所有文件
//  2. 每个 StructDecl（@name / @node / @edge）→ IRNode
//  3. 每个 EdgeDecl → IREdge
//  4. 每个 TopologyDecl → IRTopology
//  5. ResolveFlowOps：把 FlowStmt/FlowCont/FlowFanout 拍平到 IRFlowOp
//  6. 调用 AnalyzeTopology 填 UsedBlocks + AutoExec
//
// 设计原则（按用户拍板）：
//   - 每包都自己的 IRPackage（含自己的 Topology）
//   - 节点 IR 包含 Init + Blocks
//   - 边的 Kind (sync/async) 从 semantic.EdgeKinds 取
package ir

import (
	"fmt"
	"strings"

	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// Lower 入口：把 workspace semantic 结果降级为 IR
func Lower(sem *semantic.WorkspaceResult) *IRProgram {
	prog := &IRProgram{
		PkgName:  "main",
		Packages: map[string]*IRPackage{},
	}

	// 步骤 1：每包建 IRPackage
	for pkgName, file := range sem.Files {
		irPkg := lowerPackage(pkgName, file, sem)
		prog.Packages[pkgName] = irPkg
	}

	// 步骤 2：跑拓扑分析（填 UsedBlocks + AutoExec + AllNodes）
	prog.AnalyzeTopology()

	return prog
}

// lowerPackage 单个包降级
func lowerPackage(pkgName string, file *ast.File, sem *semantic.WorkspaceResult) *IRPackage {
	pkg := NewIRPackage(pkgName)

	// 节点 (@name)
	for _, decl := range file.Decls {
		if s, ok := decl.(*ast.StructDecl); ok {
			node := lowerStruct(s)
			if node != nil {
				pkg.Nodes[node.Name] = node
			}
		}
	}

	// 边 (EdgeDecl)
	for _, decl := range file.Decls {
		if e, ok := decl.(*ast.EdgeDecl); ok {
			edge := lowerEdge(e, sem)
			pkg.Edges[EdgeKey{Src: edge.Src, Name: edge.Name, Dst: edge.Dst}] = edge
		}
	}

	// 拓扑 (从 main 节点 body 收集：InstanceDecl + EdgeConnDecl)
	for _, decl := range file.Decls {
		if s, ok := decl.(*ast.StructDecl); ok && s.Name == "main" {
			pkg.Topology = lowerMainTopology(s, sem, pkg)
			// 同时把 main 当成一个特殊节点（不 emit 结构，只用来收集拓扑）
			// 不需要 lowerStruct（main 不是普通节点）
			break
		}
	}

	return pkg
}

// lowerStruct 把 StructDecl 降级为 IRNode
//
// Block 范围构造委托给 BlockBuilder（详见 block_builder.go）。
// 这里只负责：调用 builder + 收集 node-level 字段（Inputs/Outputs/State/Init）
func lowerStruct(s *ast.StructDecl) *IRNode {
	if s.Kind != ast.StructKindNode && s.Kind != ast.StructKindPlain {
		return nil // 边结构 (StructKindEdge) 走 EdgeDecl 路径
	}

	node := &IRNode{
		Name:     s.Name,
		Kind:     nodeKindFromStruct(s.Kind),
		Exported: s.Exported,
		Pos:      s.Pos(),
	}

	bb := NewBlockBuilder()

	for _, m := range s.Members {
		switch mm := m.(type) {
		case *ast.PortDecl:
			// >> type name：交给 builder
			bb.AddInput(mm.Name, mm.Pos())
			node.Inputs = append(node.Inputs, IRInput{
				Name: mm.Name,
				Type: IRTypeOf(mm.Type),
				Pos:  mm.Pos(),
			})

		case *ast.FlowDecl:
			// name >> [chain]
			var chain []*ast.FlowStep
			if mm.Chain != nil {
				chain = mm.Chain.Steps
			}
			bb.AddOutput(mm.Head, chain, mm.Pos())
			// 推断 output 类型：
			//   1. passthrough → 用 input 的类型
			//   2. 有 init stmt → 看 init expr 的字面量类型
			//   3. fallback → TypeAny (interface{})
			outType := IRType{Kind: TypeAny}
			for _, in := range node.Inputs {
				if in.Name == mm.Head {
					outType = in.Type
					break
				}
			}
			if outType.Kind == TypeAny {
				// 看 init stmt
				for _, s := range node.Init {
					if simple, ok := s.(*IRSimpleStmt); ok {
						parts := strings.SplitN(simple.Text, " := ", 2)
						if len(parts) == 2 && strings.TrimSpace(parts[0]) == mm.Head {
							// 推断 expr 类型（简化：只看字面量）
							outType = inferExprTypeSimple(strings.TrimSpace(parts[1]))
							break
						}
					}
				}
			}
			node.Outputs = append(node.Outputs, IROutput{
				Name: mm.Head,
				Type: outType,
				Pos:  mm.Pos(),
			})

		case *ast.FieldDecl:
			// 节点级状态字段
			node.State = append(node.State, IRField{
				Name: mm.Name,
				Type: IRTypeOf(mm.Type),
				Pos:  mm.Pos(),
			})

		case *ast.VarDecl:
			// var 声明
			stmt := &IRSimpleStmt{
				Kind: "vardecl",
				Text: fmt.Sprintf("%s := %s", mm.Name, exprToString(mm.Init)),
				Pos:  mm.Pos(),
			}
			if !bb.AddStmt(stmt) {
				// 没 current block → 放 Init
				node.Init = append(node.Init, stmt)
			}
		}
	}

	node.Blocks = bb.Blocks()
	return node
}

// nodeKindFromStruct 把 StructKind 转 NodeKind
func nodeKindFromStruct(k ast.StructKind) NodeKind {
	switch k {
	case ast.StructKindNode:
		return NodeKindNode
	case ast.StructKindPlain:
		return NodeKindPlain
	}
	return NodeKindPlain
}

// lowerEdge 把 EdgeDecl 降级为 IREdge
func lowerEdge(e *ast.EdgeDecl, sem *semantic.WorkspaceResult) *IREdge {
	edge := &IREdge{
		Src:  e.Src,
		Name: e.Edge,
		Dst:  e.Dst,
		Pos:  e.Pos(),
	}

	// 从 semantic 取边 kind
	key := semantic.EdgeKey{Src: e.Src, Edge: e.Edge, Dst: e.Dst}
	if k, ok := sem.EdgeKinds[key]; ok {
		edge.Kind = edgeKindFromSemantic(k)
	}

	// 把 body 的 FlowStmt/FlowCont/FlowFanout 拍平到 IRFlowOp
	edge.Flow = resolveFlowOps(e.Body)

	return edge
}

func edgeKindFromSemantic(k semantic.EdgeKind) EdgeKind {
	switch k {
	case semantic.EdgeSync:
		return EdgeSync
	case semantic.EdgeAsync:
		return EdgeAsync
	}
	return EdgeSync
}

// lowerMainTopology 从 main 节点 body 收集 InstanceDecl + EdgeConnDecl，降级为 IRTopology
//
// main 节点 body：
//   - InstanceDecl `hello happy` → VarInstances[happy] = hello
//   - EdgeConnDecl `happy <out> p` → Edges += {happy, out, p}（按 type 解析 src/dst）
//
// 简化版：之前是从 TopologyDecl 解析，现在改为从 main 节点的 body 解析。
func lowerMainTopology(mainNode *ast.StructDecl, sem *semantic.WorkspaceResult, pkg *IRPackage) *IRTopology {
	topo := &IRTopology{
		Pos:          mainNode.Pos(),
		VarInstances: map[string]string{},
	}

	if mainNode == nil {
		return topo
	}

	// 收集 InstanceDecl：instance name → type name
	for _, m := range mainNode.Members {
		inst, ok := m.(*ast.InstanceDecl)
		if !ok {
			continue
		}
		topo.VarInstances[inst.Name] = inst.Type
		// 加 type 到 AllNodes（strip pkg prefix，方便 codegen 查表）
		typeName := inst.Type
		if idx := strings.LastIndex(inst.Type, "."); idx >= 0 {
			typeName = inst.Type[idx+1:]
		}
		addNodeToList(&topo.AllNodes, typeName)
	}

	// 收集 EdgeConnDecl：解析 var → type，加到 Edges
	for _, m := range mainNode.Members {
		conn, ok := m.(*ast.EdgeConnDecl)
		if !ok {
			continue
		}
		// 解析 src/dst：var name → type
		srcType, srcOk := topo.VarInstances[conn.Src]
		dstType, dstOk := topo.VarInstances[conn.Dst]
		if !srcOk || !dstOk {
			continue // semantic 已报错，跳过
		}
		ek := EdgeKey{Src: srcType, Name: conn.Edge, Dst: dstType}
		// 去重
		dup := false
		for _, e := range topo.Edges {
			if e == ek {
				dup = true
				break
			}
		}
		if !dup {
			topo.Edges = append(topo.Edges, ek)
		}
		addNodeToList(&topo.AllNodes, srcType)
		addNodeToList(&topo.AllNodes, dstType)
	}

	return topo
}

// lowerTopology —— 已废弃：旧 TopologyDecl 已被 main 节点取代。
// 保留空 stub 避免 import cycle 影响其他文件编译。
//
// Deprecated: 用 lowerMainTopology 从 main 节点 body 收集拓扑。
func lowerTopology(t *ast.StructDecl, sem *semantic.WorkspaceResult, pkg *IRPackage) *IRTopology {
	return lowerMainTopology(t, sem, pkg)
}

// addNodeToList 把 name 加到 list（去重）
func addNodeToList(list *[]string, name string) {
	for _, n := range *list {
		if n == name {
			return
		}
	}
	*list = append(*list, name)
}

// inferExprTypeSimple 推断 expr literal 的类型（MVP：只识别 string/number/bool）
func inferExprTypeSimple(expr string) IRType {
	expr = strings.TrimSpace(expr)
	if len(expr) == 0 {
		return IRType{Kind: TypeAny}
	}
	if strings.HasPrefix(expr, `"`) && strings.HasSuffix(expr, `"`) {
		return IRType{Kind: TypeStr}
	}
	if isNumberLiteral(expr) {
		return IRType{Kind: TypeNum}
	}
	if expr == "true" || expr == "false" {
		return IRType{Kind: TypeBool}
	}
	return IRType{Kind: TypeAny}
}

// isNumberLiteral 判断是不是数字字面量（简化）
func isNumberLiteral(s string) bool {
	hasDot := false
	for i, c := range s {
		if c == '.' {
			if hasDot {
				return false
			}
			hasDot = true
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
		_ = i
	}
	return len(s) > 0
}

// countSteps 数一个 Stmt 里的 steps 数量
func countSteps(s ast.Stmt) int {
	switch v := s.(type) {
	case *ast.FlowStmt:
		return len(v.Steps)
	case *ast.FlowCont:
		return len(v.Steps)
	case *ast.FlowFanout:
		n := 0
		for _, b := range v.Branches {
			n += len(b.Steps)
		}
		return n
	}
	return 0
}

// resolveFlowOps 把 Stmt slice 拍平到 IRFlowOp slice
//
// 关键：FlowStmt + 后续 FlowCont 是一条 chain（跨多行）
// 例：`hello.h >>` + `>>say.hey` + `>>say.my` + `>>say.world`
//
//	→ FlowStmt{S=[hello.h]} + FlowCont{S=[say.hey]} + FlowCont{S=[say.my]} + FlowCont{S=[world]}
//	→ 合并成 [hello.h, say.hey, say.my, say.world] → 3 个 IRFlowOp
func resolveFlowOps(stmts []ast.Stmt) []IRFlowOp {
	fmt.Printf("DEBUG resolveFlowOps: %d stmts\n", len(stmts))
	for i, s := range stmts {
		fmt.Printf("DEBUG   [%d] %T steps=%d\n", i, s, countSteps(s))
	}
	var ops []IRFlowOp
	var allSteps []*ast.FlowStep // 累积 FlowStmt + FlowCont 的 steps

	flushChain := func() {
		// 把 allSteps 两两配对成 IRFlowOp
		for i := 0; i < len(allSteps)-1; i++ {
			srcName, srcAttr := splitTarget(allSteps[i].Target)
			dstName, dstAttr := splitTarget(allSteps[i+1].Target)
			ops = append(ops, IRFlowOp{
				Op:      FlowOpSend,
				Src:     srcName,
				SrcAttr: srcAttr,
				Dst:     dstName,
				DstAttr: dstAttr,
			})
		}
		allSteps = nil
	}

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.FlowStmt:
			// 一条 flow chain：a >> b >> c
			// 累积所有 steps（FlowStmt 和后续 FlowCont 是一起的）
			allSteps = append(allSteps, s.Steps...)
		case *ast.FlowCont:
			// continuation：累积到当前 chain
			allSteps = append(allSteps, s.Steps...)
		case *ast.FlowFanout:
			// fanout 先 flush 当前 chain
			flushChain()
			// 1 src → N branches
			srcName, srcAttr := splitTarget(s.Src)
			for bi, branch := range s.Branches {
				// 每个 branch 有 Steps，最后一个 step 是分支目标
				if len(branch.Steps) == 0 {
					continue
				}
				last := branch.Steps[len(branch.Steps)-1]
				dstName, dstAttr := splitTarget(last.Target)
				ops = append(ops, IRFlowOp{
					Op:      FlowOpBranchSend,
					Src:     srcName,
					SrcAttr: srcAttr,
					Dst:     dstName,
					DstAttr: dstAttr,
					Branch:  bi,
				})
			}
		}
	}
	// flush 最后的 chain
	flushChain()

	fmt.Printf("DEBUG resolveFlowOps done: %d ops\n", len(ops))
	return ops
}

// resolveFlowOpsFromSteps 把 head + chain 一起拍平到 IRFlowOp slice
//
// 用于 FlowDecl：head 是 name >> 的 name（即 src），steps 是 >>之后的链
//   - 第 0 步：head → steps[0]
//   - 第 i 步：steps[i-1] → steps[i]
func resolveFlowOpsFromSteps(head string, steps []*ast.FlowStep) []IRFlowOp {
	var ops []IRFlowOp
	if len(steps) == 0 {
		return ops
	}
	srcName, srcAttr := "", head
	dstName, dstAttr := splitTarget(steps[0].Target)
	ops = append(ops, IRFlowOp{
		Op:      FlowOpSend,
		Src:     srcName,
		SrcAttr: srcAttr,
		Dst:     dstName,
		DstAttr: dstAttr,
	})
	for i := 0; i < len(steps)-1; i++ {
		srcName, srcAttr = splitTarget(steps[i].Target)
		dstName, dstAttr = splitTarget(steps[i+1].Target)
		ops = append(ops, IRFlowOp{
			Op:      FlowOpSend,
			Src:     srcName,
			SrcAttr: srcAttr,
			Dst:     dstName,
			DstAttr: dstAttr,
		})
	}
	return ops
}

// splitTarget 把 FlowTarget 拆成 (nodeName, attrName)
//
//   - "Println.fid" → ("Println", "fid")
//   - "io.write.fid" → ("io.write", "fid")（跨包）
//   - "fid" → ("", "fid")（本节点自己）
//   - FlowExpr (msg+nl) → ("", "<expr>")
//
// 注意：chain 里的字符串可能带点也可能不带：
//   - "say.hey" 整字符串 → 拆成 ("say", "hey")
//   - ["say", "hey"] 多段  → ("say", "hey")
func splitTarget(t ast.FlowTarget) (string, string) {
	switch v := t.(type) {
	case *ast.FlowIdent:
		if len(v.Chain) == 0 {
			return "", ""
		}
		if len(v.Chain) == 1 {
			name := v.Chain[0]
			// 单字符串内含点 → 按最后一个点拆（node + attr）
			if idx := strings.LastIndex(name, "."); idx >= 0 {
				return name[:idx], name[idx+1:]
			}
			return "", name // 本节点自己
		}
		// 多段：node.attr 或 pkg.node.attr
		// 最后一段是 attr，前面拼起来是 node
		attr := v.Chain[len(v.Chain)-1]
		node := v.Chain[0]
		for i := 1; i < len(v.Chain)-1; i++ {
			node += "." + v.Chain[i]
		}
		return node, attr
	case *ast.FlowExpr:
		return "", "<expr:" + exprToString(v.Expr) + ">"
	}
	return "", "?"
}

// exprToString 简化的 expression → string（debug 用）
func exprToString(e ast.Expr) string {
	if e == nil {
		return ""
	}
	switch v := e.(type) {
	case *ast.LiteralExpr:
		// String 字面量要保留引号（parser 会 strip 引号存到 Value）
		switch v.Kind {
		case ast.LitString:
			return fmt.Sprintf("%q", v.Value)
		case ast.LitNumber, ast.LitBool:
			return v.Value
		}
		return v.Value
	case *ast.IdentExpr:
		return v.Name
	case *ast.BinaryExpr:
		return fmt.Sprintf("(%s %s %s)", exprToString(v.L), v.Op, exprToString(v.R))
	case *ast.UnaryExpr:
		return fmt.Sprintf("(%s%s)", v.Op, exprToString(v.X))
	case *ast.MemberExpr:
		return fmt.Sprintf("%s.%s", exprToString(v.Obj), v.Name)
	case *ast.CallExpr:
		return fmt.Sprintf("call(%s)", exprToString(v.Fn))
	}
	return "?"
}
