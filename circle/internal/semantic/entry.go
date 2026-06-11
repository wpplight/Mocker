package semantic

import (
	"fmt"

	"circle/internal/parser/ast"
)

// ──── 入口点分析 ────
//
// 设计（用户拍板）：
//   - package main 才是入口（库包无入口）
//   - 入口的启动序列 = `main { ... }` 拓扑块
//   - 拓扑块里所有"无入度节点"创建时**自动执行**（auto-exec）
//     （前面定下的："有些节点一开始没有入度，就不会等待边调用，
//      而是在节点创建的时候执行，比如 hello 节点"）
//   - 有入度的节点等数据流入再触发
//
// 这条信息传给后续 IR 阶段：
//   - auto-exec 节点 codegen 时开一个 goroutine（启动即跑）
//   - 有入度节点 codegen 时由 port 触发时开 goroutine
//   - sync edge 同步调用，async edge (含 fan-out) 每分支开 goroutine

// EntryPoint 入口点信息
type EntryPoint struct {
	File        *ast.File         // package main 那个文件
	Topology    *ast.TopologyDecl // main { ... } 拓扑块
	AutoExec    []string          // 无入度节点名（启动时自动执行）
	AllNodes    []string          // 拓扑里出现过的所有节点
	AsyncEdges  []EdgeKey         // 异步边（body 含 fan-out）
	SyncEdges   []EdgeKey         // 同步边
}

// FindEntryPoint 找一个 AST 文件里的入口点
//
// 返回 nil 表示该文件不是入口包（没 package main 或没 main 拓扑）
func FindEntryPoint(file *ast.File) *EntryPoint {
	if file == nil || file.Pkg == nil {
		return nil
	}
	if file.Pkg.Name != "main" {
		return nil
	}

	// 找 `main { ... }` 拓扑块
	var mainTopo *ast.TopologyDecl
	for _, decl := range file.Decls {
		if t, ok := decl.(*ast.TopologyDecl); ok {
			mainTopo = t
			break
		}
	}
	if mainTopo == nil {
		return nil
	}

	ep := &EntryPoint{
		File:     file,
		Topology: mainTopo,
	}

	// 收集所有节点
	indeg := map[string]int{} // 顺便算 indegree
	for _, e := range mainTopo.Edges {
		ep.AllNodes = appendUnique(ep.AllNodes, e.Src)
		ep.AllNodes = appendUnique(ep.AllNodes, e.Dst)
		indeg[e.Dst]++

		// 分类 sync / async
		impl := EdgeKey{Src: e.Src, Edge: e.Edge, Dst: e.Dst}
		// 我们手头没有 EdgeDecl（topo entry 没有 body），
		// 但 MVP 阶段我们从 ResolveFile 拿到的符号表里能找到
		// 这里先按名称标记，到 IR 阶段再做精确判断
		// TODO: 把 table 也传进来
		ep.SyncEdges = append(ep.SyncEdges, impl) // 默认 sync
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
	for _, e := range ep.Topology.Edges {
		key := EdgeKey{Src: e.Src, Edge: e.Edge, Dst: e.Dst}
		impl := table.GetEdge(e.Src, e.Edge, e.Dst)
		if impl == nil {
			// 之前应该已经报错过，这里跳过
			continue
		}
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
		ep.File.Pkg.Name, len(ep.AllNodes), len(ep.Topology.Edges), len(ep.AutoExec))
	s += fmt.Sprintf("  all nodes: %v\n", ep.AllNodes)
	s += fmt.Sprintf("  auto-exec: %v\n", ep.AutoExec)
	s += fmt.Sprintf("  sync edges (%d): %v\n", len(ep.SyncEdges), ep.SyncEdges)
	s += fmt.Sprintf("  async edges (%d, spawn goroutines): %v\n", len(ep.AsyncEdges), ep.AsyncEdges)
	return s
}
