// Package semantic 主入口：Check(file) → 错误列表
package semantic

import (
	"strings"

	"circle/internal/parser/ast"
)

// CheckResult 单包语义分析结果（向后兼容旧 API）
type CheckResult struct {
	File       *ast.File
	Table      *SymbolTable
	EntryPoint *EntryPoint
	EdgeKinds  map[EdgeKey]EdgeKind
	Errors     []SemanticError
}

// WorkspaceResult 多包语义分析结果（workspace 模式）
type WorkspaceResult struct {
	Files      map[string]*ast.File
	Tables     map[string]*SymbolTable
	EntryPoint *EntryPoint
	EdgeKinds  map[EdgeKey]EdgeKind
	Errors     []SemanticError
}

// Check 单包语义分析（向后兼容）
//
// 流程：
//  1. 先对所有 EdgeDecl 跑 InferEdgeEndpoints（Style 2 语法糖：填 src/dst）
//  2. 建符号表（这时 edge 已有正确的 src/dst）
//  3. 跑 edge body 类型检查 + topology 检查
func Check(file *ast.File) *CheckResult {
	result := &CheckResult{
		File:      file,
		EdgeKinds: map[EdgeKey]EdgeKind{},
	}

	// 步骤 1：Style 2 语法糖——infer 边端点
	for _, decl := range file.Decls {
		if e, ok := decl.(*ast.EdgeDecl); ok {
			result.Errors = append(result.Errors, InferEdgeEndpoints(e)...)
		}
	}

	// 步骤 2：建符号表
	table, errs := ResolveFile(file)
	result.Table = table
	result.Errors = append(result.Errors, errs...)

	// 步骤 3：跑 edge body + topology
	for _, decl := range file.Decls {
		if e, ok := decl.(*ast.EdgeDecl); ok {
			result.Errors = append(result.Errors, CheckEdgeBody(e, table)...)
			key := EdgeKey{Src: e.Src, Edge: e.Edge, Dst: e.Dst}
			result.EdgeKinds[key] = ClassifyEdge(e)
		}
	}

	for _, decl := range file.Decls {
		if t, ok := decl.(*ast.TopologyDecl); ok {
			result.Errors = append(result.Errors, CheckTopology(t, table)...)
		}
	}

	// 步骤 3b：节点 body 类型推导 + 隐式初始化检查（解决 #3 + #5）
	for _, decl := range file.Decls {
		if s, ok := decl.(*ast.StructDecl); ok {
			if s.Kind == ast.StructKindNode {
				env := ResolveNodeBody(s.Name, s.Members)
				result.Errors = append(result.Errors, CheckNodeBody(s.Name, s.Members, env)...)
			}
		}
	}

	result.EntryPoint = FindEntryPoint(file)
	if result.EntryPoint != nil {
		AnnotateEntryPoint(result.EntryPoint, table)
	}

	return result
}

// CheckAll 多包语义分析（Task B）
//
// 流程：
//  1. 对所有文件的所有 EdgeDecl 跑 InferEdgeEndpoints
//  2. 建所有包的符号表
//  3. 跑每包的 edge body + topology 检查（跨包用 cross 版的 helper）
//  4. 找 main 包入口点
func CheckAll(files map[string]*ast.File) *WorkspaceResult {
	result := &WorkspaceResult{
		Files:     files,
		Tables:    map[string]*SymbolTable{},
		EdgeKinds: map[EdgeKey]EdgeKind{},
	}

	// 步骤 1：所有包的 edge 先 infer 端点
	for _, file := range files {
		for _, decl := range file.Decls {
			if e, ok := decl.(*ast.EdgeDecl); ok {
				result.Errors = append(result.Errors, InferEdgeEndpoints(e)...)
			}
		}
	}

	// 步骤 2：建符号表
	for pkgName, file := range files {
		table, errs := ResolveFile(file)
		result.Tables[pkgName] = table
		result.Errors = append(result.Errors, errs...)
	}

	// 步骤 3：每包的 edge body + topology 检查
	for pkgName, file := range files {
		localTable := result.Tables[pkgName]

		for _, decl := range file.Decls {
			if e, ok := decl.(*ast.EdgeDecl); ok {
				result.Errors = append(result.Errors, CheckEdgeBodyCross(e, localTable, result.Tables)...)
				key := EdgeKey{Src: e.Src, Edge: e.Edge, Dst: e.Dst}
				result.EdgeKinds[key] = ClassifyEdge(e)
			}
		}

		for _, decl := range file.Decls {
			if t, ok := decl.(*ast.TopologyDecl); ok {
				result.Errors = append(result.Errors, CheckTopologyCross(t, localTable, result.Tables)...)
			}
		}

		// 步骤 3b：节点 body 类型推导 + 隐式初始化检查（解决 #3 + #5）
		for _, decl := range file.Decls {
			if s, ok := decl.(*ast.StructDecl); ok {
				if s.Kind == ast.StructKindNode {
					env := ResolveNodeBody(s.Name, s.Members)
					result.Errors = append(result.Errors, CheckNodeBody(s.Name, s.Members, env)...)
				}
			}
		}
	}

	// 步骤 4：入口点
	if mainFile, ok := files["main"]; ok {
		result.EntryPoint = FindEntryPoint(mainFile)
		if result.EntryPoint != nil && result.Tables["main"] != nil {
			AnnotateEntryPoint(result.EntryPoint, result.Tables["main"])
		}
	}

	return result
}

// CheckEdgeBodyCross 跨包版 edge body 检查
//
// 策略：
//  1. 解析 edge 的 src / dst 是否存在（本包或跨包）
//  2. 如果是跨包引用，建一个"虚拟符号表"，把跨包节点 + port 映射成本地表项
//  3. 调 CheckEdgeBody 用虚拟表做剩下的类型检查
//
// 简化：MVP 只支持 "pkg.node" 形式的跨包引用。
func CheckEdgeBodyCross(edge *ast.EdgeDecl, localTable *SymbolTable, allTables map[string]*SymbolTable) []SemanticError {
	// 检查 src / dst 是否跨包
	_, srcCrossPkg := crossLookupNode(edge.Src, localTable, allTables)
	_, dstCrossPkg := crossLookupNode(edge.Dst, localTable, allTables)

	// 全是本包节点（没跨包）：直接走原 CheckEdgeBody
	if srcCrossPkg == "" && dstCrossPkg == "" {
		return CheckEdgeBody(edge, localTable)
	}

	// 跨包场景：构造一个 merged table，把跨包节点 + 它的 in/out 都塞进去
	merged := mergeTableForEdge(localTable, allTables, edge)
	return CheckEdgeBody(edge, merged)
}

// mergeTableForEdge 给 edge body 检查构造一个临时符号表
//
// 规则：
//   - 复制本地表所有节点
//   - 如果 edge.Src 是 "pkg.node"，把 pkg.node 加进 merged（按完整 qualified 名 "pkg.node"）
//   - 如果 edge.Dst 是 "pkg.node"，同上
//   - 节点名不剥前缀，让 CheckEdgeBody 能直接用 body 里的 "pkg.node.port" 查表
func mergeTableForEdge(localTable *SymbolTable, allTables map[string]*SymbolTable, edge *ast.EdgeDecl) *SymbolTable {
	merged := NewSymbolTable(localTable.PkgName)

	// 复制本地表所有节点
	for name, ns := range localTable.Nodes {
		merged.Nodes[name] = ns
	}
	// 复制本地表所有边
	for key, ed := range localTable.Edges {
		merged.Edges[key] = ed
	}

	// 把跨包的 src 节点 + 它的 in/out 加进 merged（按完整 qualified 名）
	srcNode, srcPkg := crossLookupNode(edge.Src, localTable, allTables)
	if srcNode != nil && srcPkg != "" {
		merged.Nodes[edge.Src] = srcNode
	}
	_ = srcPkg

	// dst 同理
	dstNode, dstPkg := crossLookupNode(edge.Dst, localTable, allTables)
	if dstNode != nil && dstPkg != "" {
		merged.Nodes[edge.Dst] = dstNode
	}
	_ = dstPkg

	return merged
}

// crossLookupNode 跨包查节点：
//  1. "node" → 本包查找
//  2. "pkg.node" → 跨包查找（pkg 表里查 node）
func crossLookupNode(name string, localTable *SymbolTable, allTables map[string]*SymbolTable) (node *NodeSymbol, pkgName string) {
	// 先本包
	if n := localTable.GetNode(name); n != nil {
		return n, ""
	}
	// 跨包：pkg.node 形式
	if idx := strings.LastIndex(name, "."); idx > 0 {
		pkg := name[:idx]
		nodeName := name[idx+1:]
		if n := LookupPackage(allTables, pkg, nodeName); n != nil {
			return n, pkg
		}
	}
	return nil, ""
}

// crossLookupExport 跨包查 in/out
func crossLookupExport(nodeName, attrName string, localTable *SymbolTable, allTables map[string]*SymbolTable) (in *InputSymbol, isOutput bool, found bool) {
	// 先本包
	if in, isOutput, found = localTable.GetExport(nodeName, attrName); found {
		return
	}
	// 跨包：pkg.node.attr 形式
	if idx := strings.LastIndex(nodeName, "."); idx > 0 {
		pkg := nodeName[:idx]
		n := nodeName[idx+1:]
		if in, isOutput, found = LookupPackageExport(allTables, pkg, n, attrName); found {
			return
		}
	}
	return nil, false, false
}
