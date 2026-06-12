package semantic

import (
	"fmt"

	"circle/internal/parser/ast"
)

// ──── 入口点分析 ────
//
// 设计（用户拍板）：
//   - package main 才是入口（库包无入口）
//   - 入口的启动序列 = `main { ... }` 节点 body 里的 InstanceDecl + EdgeConnDecl
//   - main 节点 body 里的所有"无入度实例"创建时**自动执行**（auto-exec）
//     （"有些节点一开始没有入度，就不会等待边调用，
//      而是在节点创建的时候执行，比如 hello 节点"）
//   - 有入度的节点等数据流入再触发
//
// 这条信息传给后续 IR 阶段：
//   - auto-exec 节点 codegen 时开一个 goroutine（启动即跑）
//   - 有入度节点 codegen 时由 port 触发时开 goroutine
//   - sync edge 同步调用，async edge (含 fan-out) 每分支开 goroutine

// EntryPoint 入口点信息
type EntryPoint struct {
	File        *ast.File     // package main 那个文件
	MainNode    *ast.StructDecl // main { ... } 节点
	VarInstances map[string]string // instance name → type name（从 InstanceDecl 收集）
	Edges       []EdgeConnDeclInfo // main 节点 body 里的 EdgeConnDecl
	AutoExec    []string      // 无入度实例名（启动时自动执行）
	AllNodes    []string      // main 里出现过的所有实例
	AsyncEdges  []EdgeKey     // 异步边（body 含 fan-out）
	SyncEdges   []EdgeKey     // 同步边
}

// EdgeConnDeclInfo 边的运行时表示
//
//   - Src / Dst：实例名（不是 type 名）
//   - SrcType / DstType：从 VarInstances 解析出的 type 名
//   - Body：从 top-level EdgeDecl 找来的（带 body 才有）
type EdgeConnDeclInfo struct {
	Src      string // instance name
	Edge     string
	Dst      string // instance name
	SrcType  string // type name
	DstType  string // type name
	HasBody  bool   // 是否找到 top-level EdgeDecl（含 body）
}

// FindEntryPoint 找一个 AST 文件里的入口点
//
// 返回 nil 表示该文件不是入口包（没 package main 或没 main 节点）
func FindEntryPoint(file *ast.File) *EntryPoint {
	if file == nil || file.Pkg == nil {
		return nil
	}
	if file.Pkg.Name != "main" {
		return nil
	}

	// 找 `main { ... }` 节点
	var mainNode *ast.StructDecl
	for _, decl := range file.Decls {
		if s, ok := decl.(*ast.StructDecl); ok && s.Name == "main" {
			mainNode = s
			break
		}
	}
	if mainNode == nil {
		return nil
	}

	ep := &EntryPoint{
		File:         file,
		MainNode:     mainNode,
		VarInstances: map[string]string{},
	}

	// 收集 InstanceDecl + EdgeConnDecl
	indeg := map[string]int{}
	for _, m := range mainNode.Members {
		switch v := m.(type) {
		case *ast.InstanceDecl:
			ep.VarInstances[v.Name] = v.Type
			ep.AllNodes = appendUnique(ep.AllNodes, v.Name)
		case *ast.EdgeConnDecl:
			info := EdgeConnDeclInfo{
				Src:  v.Src,
				Edge: v.Edge,
				Dst:  v.Dst,
			}
			if t, ok := ep.VarInstances[v.Src]; ok {
				info.SrcType = t
			}
			if t, ok := ep.VarInstances[v.Dst]; ok {
				info.DstType = t
			}
			ep.Edges = append(ep.Edges, info)
			ep.AllNodes = appendUnique(ep.AllNodes, v.Src)
			ep.AllNodes = appendUnique(ep.AllNodes, v.Dst)
			indeg[v.Dst]++

			key := EdgeKey{Src: info.SrcType, Edge: v.Edge, Dst: info.DstType}
			ep.SyncEdges = append(ep.SyncEdges, key) // 默认 sync，后面 AnnotateEntryPoint 修正
		}
	}

	// indegree=0 → auto-exec
	for _, n := range ep.AllNodes {
		if indeg[n] == 0 {
			ep.AutoExec = append(ep.AutoExec, n)
		}
	}

	return ep
}

// AnnotateEntryPoint 把 EdgeKind（sync/async）填上
//
// 需要 ResolveFile 的符号表（才能拿到 EdgeDecl body 判 async）
func AnnotateEntryPoint(ep *EntryPoint, table *SymbolTable) {
	if ep == nil {
		return
	}
	ep.AsyncEdges = nil
	ep.SyncEdges = nil
	for _, e := range ep.Edges {
		key := EdgeKey{Src: e.SrcType, Edge: e.Edge, Dst: e.DstType}
		impl := table.GetEdge(e.SrcType, e.Edge, e.DstType)
		if impl == nil {
			// 没找到 body（可能是 cross-pkg 边），跳过
			continue
		}
		e.HasBody = true
		if ClassifyEdge(impl) == EdgeAsync {
			ep.AsyncEdges = append(ep.AsyncEdges, key)
		} else {
			ep.SyncEdges = append(ep.SyncEdges, key)
		}
	}
}

// ──── Helpers ────

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// FormatEntryPoint 打印入口点信息（调试 / 验证用）
func FormatEntryPoint(ep *EntryPoint) string {
	if ep == nil {
		return "no entry point"
	}
	s := fmt.Sprintf("EntryPoint: package %s, %d nodes, %d edges, %d auto-exec\n",
		ep.File.Pkg.Name, len(ep.AllNodes), len(ep.Edges), len(ep.AutoExec))
	s += fmt.Sprintf("  var instances: %v\n", ep.VarInstances)
	s += fmt.Sprintf("  all instances: %v\n", ep.AllNodes)
	s += fmt.Sprintf("  auto-exec: %v\n", ep.AutoExec)
	s += fmt.Sprintf("  sync edges (%d): %v\n", len(ep.SyncEdges), ep.SyncEdges)
	s += fmt.Sprintf("  async edges (%d, spawn goroutines): %v\n", len(ep.AsyncEdges), ep.AsyncEdges)
	return s
}