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
			node := lowerStruct(pkgName, s, sem)
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
func lowerStruct(pkgName string, s *ast.StructDecl, sem *semantic.WorkspaceResult) *IRNode {
	if s.Kind != ast.StructKindNode && s.Kind != ast.StructKindPlain {
		return nil // 边结构 (StructKindEdge) 走 EdgeDecl 路径
	}

	node := &IRNode{
		Name:     s.Name,
		Pkg:      pkgName,
		Kind:     nodeKindFromStruct(s.Kind),
		Exported: s.Exported,
		Pos:      s.Pos(),
	}

	bb := NewBlockBuilder()

	// M4.5：第一遍扫描 — 收集 SubInstanceDecl / SubEdgeDecl（后续 VarDecl 需要）
	callerName := s.Name // 当前正在 lower 的节点名
	for _, m := range s.Members {
		switch mm := m.(type) {
		case *ast.SubInstanceDecl:
			node.SubInstances = append(node.SubInstances, &IRSubInstance{
				TypeName:     mm.Type,
				InstanceName: mm.Name,
			})
		case *ast.SubEdgeDecl:
			subEdge := &IRSubEdge{
				SrcAttr:     mm.Src,
				EdgeName:    mm.Edge,
				DstInstance: mm.Dst,
			}
			// 查顶层 EdgeDecl body 填 DstAttr + RetAttr
			fillSubEdgeAttrs(subEdge, callerName, sem)
			// 隐式 SubEdge：<add_str> w 形式，Src 留空 → 根据 edge body + AST 推断
			if subEdge.SrcAttr == "" {
				if inferred, ok := inferSubEdgeSrc(subEdge, callerName, s, sem); ok {
					subEdge.SrcAttr = inferred
				} else {
					// 推断失败：fallback 用 "__implicit__" 标记，
					// 让 codegen 在 emit 时报清晰错误
					subEdge.SrcAttr = "__implicit__"
				}
			}
			node.SubEdges = append(node.SubEdges, subEdge)
		}
	}

	// 第二遍扫描 — 处理其他 member（PortDecl / FlowDecl / FieldDecl / VarDecl）
	for _, m := range s.Members {
		switch mm := m.(type) {
		case *ast.PortDecl:
			// >> type name → 入度（有类型）
			// >> name → 出度（无类型，M4.5 的 output port，不加为 input）
			if mm.Type == nil {
				// 无类型：这是 output port，不加为 input
				// Output 由 FlowDecl 或 SubFlow 决定
				break
			}
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

			// M4.5：节点 body 内的内部 flow 到 sub-instance 的 input
			// 例：`out_str >> p.msg;` → IRSubFlow{SrcAttr: "out_str", DstInstance: "p", DstAttr: "msg"}
			if mm.Chain != nil {
				for _, step := range mm.Chain.Steps {
					if ident, isIdent := step.Target.(*ast.FlowIdent); isIdent {
						// 链可能是 ["p", "msg"]（len=2）或 ["p.msg"]（CALL 整体）
						// 两种情况都要处理
						dstInstance := ""
						dstAttr := ""
						if len(ident.Chain) == 2 {
							dstInstance = ident.Chain[0]
							dstAttr = ident.Chain[1]
						} else if len(ident.Chain) == 1 {
							parts := strings.SplitN(ident.Chain[0], ".", 2)
							if len(parts) == 2 {
								dstInstance = parts[0]
								dstAttr = parts[1]
							}
						}
						if dstInstance != "" && dstAttr != "" {
							node.SubFlows = append(node.SubFlows, &IRSubFlow{
								SrcAttr:     mm.Head,
								DstInstance: dstInstance,
								DstAttr:     dstAttr,
							})
						}
					}
				}
			}

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
			// M4.5：state field（被 SubEdge 引用时）= struct field
			//   如果当前 VarDecl 的 name 被任何 SubEdge 当作 SrcAttr，就 emit 为 state field
			//   否则就是 block-local（在 NewXxx 或 block0 里 local var）
			isStateField := false
			for _, se := range node.SubEdges {
				if se.SrcAttr == mm.Name {
					isStateField = true
					break
				}
			}
			if isStateField {
				// 状态字段
				node.Outputs = append(node.Outputs, IROutput{
					Name: mm.Name,
					Type: inferExprTypeSimple(exprToString(mm.Init)),
					Pos:  mm.Pos(),
				})
				node.State = append(node.State, IRField{
					Name: mm.Name,
					Type: inferExprTypeSimple(exprToString(mm.Init)),
					Pos:  mm.Pos(),
				})
			}

		case *ast.AssignStmt:
			// 赋值语句（a := b / a = b / a += b / a++）
			// 这里简化为存原文本，codegen 原样 emit
			text := assignStmtToText(mm)
			if text != "" {
				stmt := &IRSimpleStmt{Kind: "assign", Text: text, Pos: mm.Pos()}
				if !bb.AddStmt(stmt) {
					node.Init = append(node.Init, stmt)
				}
			}

		case *ast.IfStmt:
			// if 条件语句
			text := ifStmtToText(mm)
			if text != "" {
				stmt := &IRSimpleStmt{Kind: "if", Text: text, Pos: mm.Pos()}
				if !bb.AddStmt(stmt) {
					node.Init = append(node.Init, stmt)
				}
			}

		case *ast.ForStmt:
			// for 循环语句
			text := forStmtToText(mm)
			if text != "" {
				stmt := &IRSimpleStmt{Kind: "for", Text: text, Pos: mm.Pos()}
				if !bb.AddStmt(stmt) {
					node.Init = append(node.Init, stmt)
				}
			}

		case *ast.WhileStmt:
			// while 循环语句（会被转译为 for cond { body }）
			text := whileStmtToText(mm)
			if text != "" {
				stmt := &IRSimpleStmt{Kind: "while", Text: text, Pos: mm.Pos()}
				if !bb.AddStmt(stmt) {
					node.Init = append(node.Init, stmt)
				}
			}

		case *ast.SubInstanceDecl, *ast.SubEdgeDecl:
			// 已在第一遍扫描中处理，跳过
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
//   - EdgeConnDecl `happy <out> p` → Edges += {hello, out, Println}（按 type 解析 src/dst）
//   - 同时存 InstanceEdges（含 instance-level 信息，给 codegen 用）
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

	// 收集 EdgeConnDecl：解析 var → type，加到 Edges + InstanceEdges
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
		// 也存 instance-level 边（codegen 编排器用）
		topo.InstanceEdges = append(topo.InstanceEdges, InstanceEdge{
			SrcInstance: conn.Src,
			Edge:        conn.Edge,
			DstInstance: conn.Dst,
		})
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

// fillSubEdgeAttrs 从顶层 EdgeDecl body 推断 SubEdge 的 DstAttr / RetAttr
//
// 推断规则（M4.5）：
//   - 顶层 edge body 第一个 FlowStmt 的 last step 描述调用输入：callee.DstAttr
//   - 顶层 edge body 后续 FlowStmt（表示返回路径）中，last step 指向 caller 的步骤：
//     last step 的 attr 就是 RetAttr
//
// 例：caller=hello, sub_edge=h<add_str>w, top-level edge body:
//
//	FlowStmt[hello.h >> world.words]      → DstAttr = "words"
//	FlowStmt[world.new >> hello.out_str]  → RetAttr = "out_str"
func fillSubEdgeAttrs(subEdge *IRSubEdge, callerName string, sem *semantic.WorkspaceResult) {
	if sem == nil {
		return
	}
	// 在所有包的 EdgeDecl 里找（按 EdgeName + caller 匹配）
	for _, file := range sem.Files {
		for _, decl := range file.Decls {
			edge, ok := decl.(*ast.EdgeDecl)
			if !ok || edge.Edge != subEdge.EdgeName {
				continue
			}
			if edge.Src != callerName {
				continue
			}

			// 找第一个 FlowStmt（调用的输入路径）和第二个 FlowStmt（返回路径）
			var firstFlow, returnFlow *ast.FlowStmt
			for _, stmt := range edge.Body {
				fs, ok := stmt.(*ast.FlowStmt)
				if !ok {
					continue
				}
				if firstFlow == nil {
					firstFlow = fs
				} else if returnFlow == nil {
					returnFlow = fs
					break
				}
			}

			// DstAttr：第一个 FlowStmt 的 last step 的 attr（callee 的入 port）
			if firstFlow != nil && len(firstFlow.Steps) >= 2 {
				last := firstFlow.Steps[len(firstFlow.Steps)-1]
				if last.Target != nil {
					_, attr := splitFlowTargetAttr(last.Target)
					subEdge.DstAttr = attr
				}
			}

			// RetAttr：returnFlow 的 last step 的 attr（caller 上的返回 port）
			if returnFlow != nil && len(returnFlow.Steps) >= 2 {
				last := returnFlow.Steps[len(returnFlow.Steps)-1]
				if last.Target != nil {
					node, attr := splitFlowTargetAttr(last.Target)
					if node == callerName {
						subEdge.RetAttr = attr
					}
				}
			}
			return
		}
	}
}

// inferSubEdgeSrc 隐式 SubEdge 源推断（语法糖 <add_str> w）
//
// 推断规则：
//  1. 从 top-level edge body 的第一个 FlowStmt 拿到 callee 的入 port（如 world.words）
//  2. 查 callee node 拿到该入 port 的类型
//  3. 扫描 caller 节点的 AST（s.Members）里的"非 input"变量集（VarDecl + FieldDecl + FlowDecl）
//  4. 找类型匹配的唯一变量，作为 SrcAttr
//  5. 0 个或多个候选 → 推断失败（返回 false，调用方需要报错或 fallback）
//
// 设计意图（用户拍板）：
//   - edge body 是源头，声明这个 edge 需要 caller 提供什么类型的变量
//   - 推断路径是“唯一”匹配，杜绝多义性
//   - 错误信息在 codegen 阶段报，这里只负责推断或标记为 __implicit__
//
// 例：
//
//	edge:  hello.h >> world.words     （callee 入 port = world.words: str）
//	caller (AST):
//	  VarDecl  h := "hello"            (str)
//	  PortDecl >> str out_str          (input，跳过)
//	→ h 匹配，返回 "h"
//
// 为什么要从 AST 扫，不从 callerNode：
//   - 调用时机在第一遍扫描中，callerNode.State / Init / Outputs 还未填
//   - AST 是权威源，里面有完整的类型注解（FieldDecl.Type / PortDecl.Type）
//   - 推断完成后，State 决定逻辑才能正确把该变量设为 state field
func inferSubEdgeSrc(subEdge *IRSubEdge, callerName string, s *ast.StructDecl, sem *semantic.WorkspaceResult) (string, bool) {
	if sem == nil || subEdge == nil || s == nil {
		return "", false
	}

	// 1. 查 top-level edge body
	var firstFlow *ast.FlowStmt
	for _, file := range sem.Files {
		for _, decl := range file.Decls {
			edge, ok := decl.(*ast.EdgeDecl)
			if !ok || edge.Edge != subEdge.EdgeName {
				continue
			}
			// 选 caller 匹配的那个 edge（M4.5：edge 有 owner）
			if edge.Src != callerName {
				continue
			}
			for _, stmt := range edge.Body {
				if fs, ok := stmt.(*ast.FlowStmt); ok {
					firstFlow = fs
					break
				}
			}
			break
		}
		if firstFlow != nil {
			break
		}
	}
	if firstFlow == nil || len(firstFlow.Steps) < 2 {
		return "", false
	}

	// 2. 拿 callee 的入 port 名 + 类型
	last := firstFlow.Steps[len(firstFlow.Steps)-1]
	calleeNodeName, calleePort := splitFlowTargetAttr(last.Target)
	if calleeNodeName == "" || calleePort == "" {
		return "", false
	}
	// 查 callee node 的入 port 类型
	// 优先从 callerNode 所在 pkg 查（不同 pkg 用 pkg.NodeName 形式）
	portType := lookupCalleeInputType(calleeNodeName, calleePort, callerName, sem)
	if portType.Kind == TypeUnknown || portType.Kind == TypeAny {
		return "", false
	}

	// 3. 扫描 caller 的 AST members，收集所有"非 input"变量及其类型
	//    然后按 portType 过滤：只保留类型匹配的
	var candidates []candidateSrc
	for _, m := range s.Members {
		switch mm := m.(type) {
		case *ast.VarDecl:
			// VarDecl：变量声明（包含 init 表达式可推断类型）
			t := inferExprTypeSimple(exprToString(mm.Init))
			addCandidateIfMatch(&candidates, mm.Name, t, portType, "vardecl")
		case *ast.FieldDecl:
			// FieldDecl：具类型字段
			t := IRTypeOf(mm.Type)
			addCandidateIfMatch(&candidates, mm.Name, t, portType, "fielddecl")
		case *ast.FlowDecl:
			// FlowDecl：`h >>` 形式的出度（h 本身是个变量、后面会流出）
			// 取 init expr 的类型
			if mm.Head != "" {
				t := inferExprTypeSimple(mm.Head)
				if t.Kind == TypeUnknown {
					t = IRType{Kind: TypeStr} // fallback：出度默认 str
				}
				addCandidateIfMatch(&candidates, mm.Head, t, portType, "flowdecl")
			}
			// 跳过 PortDecl / SubInstanceDecl / SubEdgeDecl
		}
	}

	// 4. 唯一匹配
	if len(candidates) == 1 {
		return candidates[0].name, true
	}
	// 多个候选 → 推断失败，要求显式写源
	return "", false
}

// addCandidateIfMatch 添加候选到列表（按 portType 过滤）
//
// 过滤规则：
//   - 跳过未知类型（TypeUnknown）
//   - 跳过与 portType 不匹配的变量
//   - 匹配规则：IRType 相等（Kind + Name）
func addCandidateIfMatch(candidates *[]candidateSrc, name string, t, portType IRType, kind string) {
	if t.Kind == TypeUnknown {
		return
	}
	// 类型匹配：Kind 相同，Name 也相同（用户自定义类型时）
	if t.Kind != portType.Kind || t.Name != portType.Name {
		return
	}
	*candidates = append(*candidates, candidateSrc{name: name, kind: kind, typ: t})
}

// lookupCalleeInputType 查 callee 节点的入 port 类型
//
// calleeNodeName 可能是 "world" 或 "stdio.Println"（跨包）
func lookupCalleeInputType(calleeNodeName, inputName, callerName string, sem *semantic.WorkspaceResult) IRType {
	// 通过 SymbolTable 查 NodeSymbol
	for _, table := range sem.Tables {
		ns := table.GetNode(calleeNodeName)
		if ns == nil {
			continue
		}
		for _, in := range ns.Inputs {
			if in.Name == inputName {
				// 转换 semantic.Type → IRType
				return semTypeToIRType(in.Type)
			}
		}
	}
	// 跨包短名 fallback：尝试所有 pkg 的 Nodes
	for _, table := range sem.Tables {
		// 跨包查找：pkgName.NodeName 形式
		if idx := strings.LastIndex(calleeNodeName, "."); idx > 0 {
			pkg := calleeNodeName[:idx]
			node := calleeNodeName[idx+1:]
			if pkg == table.PkgName {
				if ns := table.GetNode(node); ns != nil {
					for _, in := range ns.Inputs {
						if in.Name == inputName {
							return semTypeToIRType(in.Type)
						}
					}
				}
			}
		}
	}
	return IRType{Kind: TypeUnknown}
}

// semTypeToIRType 把 semantic.Type 转 IRType
func semTypeToIRType(t semantic.Type) IRType {
	switch t {
	case semantic.TypeStr:
		return IRType{Kind: TypeStr}
	case semantic.TypeNum:
		return IRType{Kind: TypeNum}
	case semantic.TypeBool:
		return IRType{Kind: TypeBool}
	case semantic.TypeByte:
		return IRType{Kind: TypeByte}
	case semantic.TypeAny:
		return IRType{Kind: TypeAny}
	default:
		return IRType{Kind: TypeUnknown}
	}
}

// candidateSrc 推断出的源候选项
type candidateSrc struct {
	name string
	kind string // "vardecl" / "fielddecl" / "flowdecl"
	typ  IRType // 变量的类型（用于诊断信息）
}

// splitFlowTargetAttr 从 FlowTarget 提取 (nodeName, attrName)
//
// 复用 semantic/edge.go 的 extractPortName 思路，但本文件不依赖 semantic 包的内部 helper。
// 例："hello.h" → ("hello", "h"); "h" → ("", "h"); "stdio.Println.msg" → ("stdio.Println", "msg")
func splitFlowTargetAttr(t ast.FlowTarget) (string, string) {
	ident, isIdent := t.(*ast.FlowIdent)
	if !isIdent || len(ident.Chain) == 0 {
		return "", ""
	}
	if len(ident.Chain) >= 2 {
		// 多段链：最后一段是 attr，前面拼起来是 node
		node := ident.Chain[0]
		for i := 1; i < len(ident.Chain)-1; i++ {
			node += "." + ident.Chain[i]
		}
		return node, ident.Chain[len(ident.Chain)-1]
	}
	// 单段：可能是 "node.attr" 或纯 attr
	name := ident.Chain[0]
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[:idx], name[idx+1:]
	}
	return "", name
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

// assignStmtToText 把 AssignStmt 转成可读文本（codegen 用）
//
//  1. 复合赋值：a += b  → "a += b"（Go 原生支持）
//  2. i++  /  i--   → "i++" / "i--"（Go 原生支持）
//  3. 多变量赋值：a, b := expr  → "a, b := expr"
func assignStmtToText(s *ast.AssignStmt) string {
	if s == nil {
		return ""
	}
	// 复合赋值（a += b / a -= b / a *= b / a /= b）
	// Compound 是词法 token 的 Value（如 "+=" / "-="），但 ++/-- 是 i++/i-- 形式（Compound="+"，Rhs=nil）
	if s.Compound != "" {
		if s.Rhs == nil {
			// i++  /  i--  形式：Compound = "+" / "-"
			return fmt.Sprintf("%s%s", s.Lhs[0], s.Compound+s.Compound)
		}
		// Compound = "+=" / "-=" / "*=" / "/=" 形式
		return fmt.Sprintf("%s %s %s", s.Lhs[0], s.Compound, exprToString(s.Rhs))
	}
	// 普通多变量赋值：a, b := expr
	if len(s.Lhs) > 1 {
		return fmt.Sprintf("%s := %s", strings.Join(s.Lhs, ", "), exprToString(s.Rhs))
	}
	// 普通单变量赋值：a := b  /  a = b
	return fmt.Sprintf("%s := %s", s.Lhs[0], exprToString(s.Rhs))
}

// ifStmtToText 把 IfStmt 转成可读文本（codegen 用）
func ifStmtToText(s *ast.IfStmt) string {
	if s == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("if ")
	sb.WriteString(exprToString(s.Cond))
	sb.WriteString(" {\n")
	for _, st := range s.Body.Stmts {
		sb.WriteString("\t")
		sb.WriteString(stmtToText(st))
		sb.WriteString("\n")
	}
	sb.WriteString("}")
	if s.Else != nil {
		sb.WriteString(" else ")
		switch e := s.Else.(type) {
		case *ast.IfStmt:
			sb.WriteString(strings.TrimSuffix(ifStmtToText(e), "\n"))
		case *ast.BlockStmt:
			sb.WriteString("{\n")
			for _, st := range e.Stmts {
				sb.WriteString("\t")
				sb.WriteString(stmtToText(st))
				sb.WriteString("\n")
			}
			sb.WriteString("}")
		}
	}
	return sb.String()
}

// forStmtToText 把 ForStmt 转成可读文本（codegen 用）
//
//  1. C 风格 for(init; cond; post) { body }
//  2. Go while：for cond { body }    （init=nil, post=nil）
//  3. 无限循环：for { body }            （init=nil, cond=nil, post=nil）
func forStmtToText(s *ast.ForStmt) string {
	if s == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("for ")
	// for { } 形式
	if s.Init == nil && s.Cond == nil && s.Post == nil {
		sb.WriteString("{\n")
	} else {
		// init
		if s.Init != nil {
			sb.WriteString(stmtToText(s.Init))
		}
		sb.WriteString("; ")
		// cond（去掉 exprToString 加的括号）
		if s.Cond != nil {
			sb.WriteString(exprToStringNoParen(s.Cond))
		}
		sb.WriteString("; ")
		// post
		if s.Post != nil {
			sb.WriteString(stmtToText(s.Post))
		}
		sb.WriteString(" {\n")
	}
	for _, st := range s.Body.Stmts {
		sb.WriteString("\t")
		sb.WriteString(stmtToText(st))
		sb.WriteString("\n")
	}
	sb.WriteString("}")
	return sb.String()
}

// exprToStringNoParen 类似 exprToString，但 binary expr 不加括号（用于 for cond）
func exprToStringNoParen(e ast.Expr) string {
	if e == nil {
		return ""
	}
	switch v := e.(type) {
	case *ast.LiteralExpr:
		switch v.Kind {
		case ast.LitString:
			return fmt.Sprintf("%q", v.Value)
		}
		return v.Value
	case *ast.IdentExpr:
		return v.Name
	case *ast.BinaryExpr:
		return fmt.Sprintf("%s %s %s", exprToStringNoParen(v.L), v.Op, exprToStringNoParen(v.R))
	case *ast.UnaryExpr:
		return fmt.Sprintf("%s%s", v.Op, exprToStringNoParen(v.X))
	}
	return "?"
}

// whileStmtToText 把 WhileStmt 转成可读文本（转译为 for cond { body }）
func whileStmtToText(s *ast.WhileStmt) string {
	if s == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("for ")
	sb.WriteString(exprToString(s.Cond))
	sb.WriteString(" {\n")
	for _, st := range s.Body.Stmts {
		sb.WriteString("\t")
		sb.WriteString(stmtToText(st))
		sb.WriteString("\n")
	}
	sb.WriteString("}")
	return sb.String()
}

// stmtToText 把任意 stmt 转成文本（codegen 用）
func stmtToText(s ast.Stmt) string {
	if s == nil {
		return ""
	}
	switch v := s.(type) {
	case *ast.VarDecl:
		return fmt.Sprintf("%s := %s", v.Name, exprToString(v.Init))
	case *ast.AssignStmt:
		return assignStmtToText(v)
	case *ast.IfStmt:
		return ifStmtToText(v)
	case *ast.ForStmt:
		return forStmtToText(v)
	case *ast.WhileStmt:
		return whileStmtToText(v)
	case *ast.BlockStmt:
		var sb strings.Builder
		sb.WriteString("{\n")
		for _, st := range v.Stmts {
			sb.WriteString("\t")
			sb.WriteString(stmtToText(st))
			sb.WriteString("\n")
		}
		sb.WriteString("}")
		return sb.String()
	}
	return "?"
}
