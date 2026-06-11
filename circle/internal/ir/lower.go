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

	// 拓扑 (TopologyDecl)
	for _, decl := range file.Decls {
		if t, ok := decl.(*ast.TopologyDecl); ok {
			pkg.Topology = lowerTopology(t, sem)
		}
	}

	return pkg
}

// lowerStruct 把 StructDecl 降级为 IRNode
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

	// 走 members 提取 Inputs / Outputs / Init / Fields
	for _, m := range s.Members {
		switch mm := m.(type) {
		case *ast.PortDecl:
			// >> type name（输入口）
			node.Inputs = append(node.Inputs, IRInput{
				Name: mm.Name,
				Type: IRTypeOf(mm.Type),
				Pos:  mm.Pos(),
			})
		case *ast.FieldDecl:
			// 强类型字段（节点级状态）
			node.State = append(node.State, IRField{
				Name: mm.Name,
				Type: IRTypeOf(mm.Type),
				Pos:  mm.Pos(),
			})
		case *ast.VarDecl:
			// var 声明 → IRSimpleStmt
			node.Init = append(node.Init, &IRSimpleStmt{
				Kind: "vardecl",
				Text: fmt.Sprintf("%s := %s", mm.Name, exprToString(mm.Init)),
				Pos:  mm.Pos(),
			})
		case *ast.FlowDecl:
			// name >>（出度）
			node.Outputs = append(node.Outputs, IROutput{
				Name: mm.Head,
				Pos:  mm.Pos(),
			})
		}
	}

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

// lowerTopology 把 TopologyDecl 降级为 IRTopology
//
// TopologyDecl.Edges 是 []*EdgeDecl（用户写在 {} 里的边引用）
// IRTopology.Edges 是 []EdgeKey（IR 视角的三元组）
func lowerTopology(t *ast.TopologyDecl, sem *semantic.WorkspaceResult) *IRTopology {
	topo := &IRTopology{
		Pos: t.Pos(),
	}

	seen := map[EdgeKey]bool{}
	for _, e := range t.Edges {
		ek := EdgeKey{Src: e.Src, Name: e.Edge, Dst: e.Dst}
		if !seen[ek] {
			seen[ek] = true
			topo.Edges = append(topo.Edges, ek)
		}
		// 也收集所有节点
		addNodeToList(&topo.AllNodes, e.Src)
		addNodeToList(&topo.AllNodes, e.Dst)
	}

	return topo
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

// resolveFlowOps 把 Stmt slice 拍平到 IRFlowOp slice
func resolveFlowOps(stmts []ast.Stmt) []IRFlowOp {
	var ops []IRFlowOp
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case *ast.FlowStmt:
			// 一条 flow chain：a >> b >> c
			// 每对相邻 step 之间是一个 IRFlowOp
			for i := 0; i < len(s.Steps)-1; i++ {
				srcName, srcAttr := splitTarget(s.Steps[i].Target)
				dstName, dstAttr := splitTarget(s.Steps[i+1].Target)
				ops = append(ops, IRFlowOp{
					Op:      FlowOpSend,
					Src:     srcName,
					SrcAttr: srcAttr,
					Dst:     dstName,
					DstAttr: dstAttr,
				})
			}
		case *ast.FlowFanout:
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
		case *ast.FlowCont:
			// continuation
			for i := 0; i < len(s.Steps)-1; i++ {
				srcName, srcAttr := splitTarget(s.Steps[i].Target)
				dstName, dstAttr := splitTarget(s.Steps[i+1].Target)
				ops = append(ops, IRFlowOp{
					Op:      FlowOpSend,
					Src:     srcName,
					SrcAttr: srcAttr,
					Dst:     dstName,
					DstAttr: dstAttr,
				})
			}
		}
	}
	return ops
}

// splitTarget 把 FlowTarget 拆成 (nodeName, attrName)
//
//   - "Println.fid" → ("Println", "fid")
//   - "io.write.fid" → ("io.write", "fid")（跨包）
//   - "fid" → ("", "fid")（本节点自己）
//   - FlowExpr (msg+nl) → ("", "<expr>")
func splitTarget(t ast.FlowTarget) (string, string) {
	switch v := t.(type) {
	case *ast.FlowIdent:
		if len(v.Chain) == 0 {
			return "", ""
		}
		if len(v.Chain) == 1 {
			return "", v.Chain[0] // 本节点自己
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
