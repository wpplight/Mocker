package ide

import (
	"fmt"

	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// ParseSource 解析一段 .ce 源码，返回 ParsedFile JSON
//
// 流程：
//  1. parser.Parse → AST
//  2. semantic.Check → 符号表 + 类型检查
//  3. 把 AST + 符号表拍扁成 ParsedFile
//
// 单文件模式（不依赖 workspace），用于编辑器里临时改代码。
func (s *Service) ParseSource(src string) (*ParsedFile, error) {
	file, parseErrs := parser.Parse([]byte(src))
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("parse errors: %d", len(parseErrs))
	}

	// semantic check
	checkResult := semantic.Check(file)

	// emit
	pf := &ParsedFile{
		PackageName: "",
		Imports:     []string{},
		Nodes:       []NodeDetail{},
		Edges:       []EdgeDetail{},
		Enums:       []EnumDetail{},
		Graph: GraphData{
			Nodes: []FlowNode{},
			Edges: []FlowEdge{},
		},
	}

	if file.Pkg != nil {
		pf.PackageName = file.Pkg.Name
	}
	for _, imp := range file.Imports {
		pf.Imports = append(pf.Imports, imp.Path)
	}

	// enums
	for _, decl := range file.Decls {
		if e, ok := decl.(*ast.EnumDecl); ok {
			pf.Enums = append(pf.Enums, EnumDetail{
				Name:   e.Name,
				Values: e.Values,
			})
		}
	}

	// nodes + graph
	buildGraphFromFile(pf, file, checkResult.Table)

	// errors
	for _, e := range checkResult.Errors {
		pf.Errors = append(pf.Errors, errorToDiagnostic(e))
	}
	for _, pe := range parseErrs {
		pf.Errors = append(pf.Errors, Diagnostic{
			Line:    pe.Pos.Line,
			Column:  pe.Pos.Col,
			Message: pe.Msg,
		})
	}

	return pf, nil
}

// ParseWorkspace 解析整个 workspace
//
// 流程：
//  1. semantic.ScanWorkspace 扫目录
//  2. semantic.LoadWorkspaceBFS BFS 加载
//  3. semantic.CheckAll 多包语义检查
//  4. 把所有包的信息合并成一个 ParsedFile JSON
//
// 用于"打开项目"操作。
func (s *Service) ParseWorkspace() (*ParsedFile, error) {
	workspace := s.workspaceDir
	if workspace == "" {
		workspace = "."
	}

	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: workspace})
	if len(scanErrs) > 0 {
		// 不直接返回 error —— 把诊断放进 ParsedFile.Errors
	}

	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		return nil, fmt.Errorf("find main package: %w", err)
	}

	files, bfsErrs := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	if len(bfsErrs) > 0 {
		// 同上
	}

	wresult := semantic.CheckAll(files)

	// 合并所有包到一个 ParsedFile
	pf := &ParsedFile{
		PackageName: mainInfo.Name,
		Imports:     []string{},
		Nodes:       []NodeDetail{},
		Edges:       []EdgeDetail{},
		Enums:       []EnumDetail{},
		Graph: GraphData{
			Nodes: []FlowNode{},
			Edges: []FlowEdge{},
		},
	}

	for pkgName, file := range files {
		_ = pkgName // 暂取 main 包为主，其他包 nodes 也合并进来（M1 细化）
		if file.Pkg == nil {
			continue
		}

		// 第一遍只把 main 包作为"主视图"，其他包节点也合并进来
		table := wresult.Tables[file.Pkg.Name]
		if table == nil {
			continue
		}

		// imports
		for _, imp := range file.Imports {
			pf.Imports = append(pf.Imports, imp.Path)
		}

		// enums
		for _, decl := range file.Decls {
			if e, ok := decl.(*ast.EnumDecl); ok {
				pf.Enums = append(pf.Enums, EnumDetail{
					Name:   e.Name,
					Values: e.Values,
				})
			}
		}

		// nodes + graph
		buildGraphFromFile(pf, file, table)
	}

	// errors
	for _, e := range wresult.Errors {
		pf.Errors = append(pf.Errors, errorToDiagnostic(e))
	}
	for _, e := range scanErrs {
		pf.Errors = append(pf.Errors, errorToDiagnostic(e))
	}
	for _, e := range bfsErrs {
		pf.Errors = append(pf.Errors, errorToDiagnostic(e))
	}

	return pf, nil
}

// ──── helpers ────

// buildGraphFromFile 把单个文件填进 ParsedFile（nodes + graph）
func buildGraphFromFile(pf *ParsedFile, file *ast.File, table *semantic.SymbolTable) {
	// 1. 节点（StructDecl 且 Name != "main"）
	for _, decl := range file.Decls {
		s, ok := decl.(*ast.StructDecl)
		if !ok {
			continue
		}
		if s.Name == "main" {
			// main 节点的 InstanceDecl 在 M1 改造时单独处理
			// MVP：把 main 节点的 InstanceDecl 当作"节点"展示
			for _, m := range s.Members {
				if inst, ok := m.(*ast.InstanceDecl); ok {
					pf.Graph.Nodes = append(pf.Graph.Nodes, FlowNode{
						ID:       "node-" + inst.Name,
						Type:     "mockerNode",
						Name:     inst.Name,
						Exported: false,
						Position: autoLayoutPosition(len(pf.Graph.Nodes)),
						Data: map[string]any{
							"label":    inst.Name,
							"kind":     "instance",
							"type":     inst.Type,
							"exported": false,
							"members":  []NodeMember{},
						},
					})
					pf.Nodes = append(pf.Nodes, NodeDetail{
						Name:     inst.Name,
						Exported: false,
						Kind:     "instance",
						Members:  []NodeMember{},
					})
				}
				if conn, ok := m.(*ast.EdgeConnDecl); ok {
					pf.Graph.Edges = append(pf.Graph.Edges, FlowEdge{
						ID:       fmt.Sprintf("edge-%s-%s-%s", conn.Src, conn.Edge, conn.Dst),
						Source:   "node-" + conn.Src,
						Target:   "node-" + conn.Dst,
						EdgeName: conn.Edge,
						Animated: true,
					})
					pf.Edges = append(pf.Edges, EdgeDetail{
						Src:  conn.Src,
						Edge: conn.Edge,
						Dst:  conn.Dst,
						Body: []string{},
					})
				}
			}
			continue
		}

		// 普通节点
		pf.Graph.Nodes = append(pf.Graph.Nodes, FlowNode{
			ID:       "node-" + s.Name,
			Type:     "mockerNode",
			Name:     s.Name,
			Exported: s.Exported,
			Position: autoLayoutPosition(len(pf.Graph.Nodes)),
			Data: map[string]any{
				"label":    s.Name,
				"kind":     "node",
				"exported": s.Exported,
				"members":  emitStructMembers(s.Members),
			},
		})

		// NodeDetail
		var nd NodeDetail
		if table != nil {
			if sym := table.GetNode(s.Name); sym != nil {
				nd = emitNodeDetail(sym, file)
			}
		}
		if nd.Name == "" {
			nd = NodeDetail{
				Name:     s.Name,
				Exported: s.Exported,
				Kind:     "node",
				Members:  emitStructMembers(s.Members),
			}
		}
		pf.Nodes = append(pf.Nodes, nd)
	}

	// 2. 边（EdgeDecl）
	for _, decl := range file.Decls {
		e, ok := decl.(*ast.EdgeDecl)
		if !ok {
			continue
		}
		pf.Edges = append(pf.Edges, emitEdgeDetail(e))

		// 渲染到图（如果 src/dst 都存在）
		if e.Src != "" && e.Dst != "" {
			pf.Graph.Edges = append(pf.Graph.Edges, FlowEdge{
				ID:       fmt.Sprintf("edge-%s-%s-%s", e.Src, e.Edge, e.Dst),
				Source:   "node-" + e.Src,
				Target:   "node-" + e.Dst,
				EdgeName: e.Edge,
				Animated: true,
			})
		}
	}
}

// autoLayoutPosition 简易自动布局（横排 + 换行）
func autoLayoutPosition(idx int) map[string]float64 {
	const colWidth = 320
	const rowHeight = 280
	const maxCols = 4

	col := idx % maxCols
	row := idx / maxCols
	return map[string]float64{
		"x": float64(50 + col*colWidth),
		"y": float64(50 + row*rowHeight),
	}
}

// errorToDiagnostic 把 SemanticError 转 Diagnostic
func errorToDiagnostic(e semantic.SemanticError) Diagnostic {
	return Diagnostic{
		Line:    e.Pos.Line,
		Column:  e.Pos.Col,
		Message: e.Msg,
		Hint:    e.Hint,
	}
}