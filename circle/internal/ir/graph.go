package ir

import (
	"fmt"
	"strings"
)

// IRGraph 程序的图结构（IR 层构建，codegen 用）
//
// 设计目的：
//   - 把 AST + semantic 解析后的 topology 抽象成一个明确的图结构
//   - codegen 用这个图决定如何生成代码（直连 → 函数调用；散连 → goroutine + channel）
//   - 这是 Mocker 编译器的核心 IR：从图结构派生出所有 emit 策略
//
// 节点：每个 IRNode（按 type name）
// 边：每个 topology entry（main + 各包内部）+ block.Flow entry
//
// 边的 Kind（GraphEdgeKind）：
//   - GraphEdgeDirect: 1 src → 1 dst（直链，可 emit 为函数调用）
//   - GraphEdgeFanout: 1 src → N dsts（扇出，需 goroutine + 多 channel 派发）
//   - GraphEdgeFanin:  N srcs → 1 dst（汇合，需 channel + merge）
//
// 例子（hello world 直链）：
//
//	hello (auto-exec) ──GraphEdgeDirect──> Println (sync)
//	  ↓ returns string                       ↓ receives string
//	  "hello world!"                          syscall.Write(1, msg)
//
// 例子（say 3-fanout）：
//
//	hello ──GraphEdgeFanout──> say.block0 ──GraphEdgeDirect──> stdio.Println
//	                  ──> say.block1 ──GraphEdgeDirect──> stdio.Println
//	                  ──> say.block2 ──GraphEdgeDirect──> stdio.Println
type IRGraph struct {
	Nodes map[string]*GraphNode // node name (strip pkg) → GraphNode
	Edges []*GraphEdge          // 所有边

	// StartNodes: auto-exec 启动节点（程序入口）
	StartNodes []string
}

// GraphNode 图节点
type GraphNode struct {
	Name       string     // node name（strip pkg）
	IRNode     *IRNode    // 对应的 IR node
	UsedBlocks []*IRBlock // USED blocks（只有 1 个 → SimpleNode；多个 → ComplexNode）
	IsAutoExec bool       // 有 USED 的 auto-exec block
	IsTerminal bool       // 没有出度

	// 度数统计（从边推出来）
	FanInCount  int // 入度（被多少边指过来）
	FanOutCount int // 出度（自己指出去多少）
}

// GraphEdge 图边
type GraphEdge struct {
	Src          string         // source node name（strip pkg）
	SrcAttr      string         // source output attribute（如 "hey"）
	Dst          string         // destination node name（strip pkg；或 "SYSCALL" 等保留字）
	DstAttr      string         // destination input attribute（如 "msg"）
	Kind         GraphEdgeKind  // Direct / Fanout / Fanin
	Branch       int            // for Fanout, branch index (0-based)
	FromTopology bool           // from topology block（true）或 block.Flow（false）
}

// GraphEdgeKind 边种类（区别于 EdgeKind，那是 edge 运行时类型 Sync/Async）
type GraphEdgeKind int

const (
	// GraphEdgeDirect 直链：1 src → 1 dst
	// emit 策略：src.Run() → dst.Run(value)
	GraphEdgeDirect GraphEdgeKind = iota

	// GraphEdgeFanout 扇出：1 src → N dsts
	// emit 策略：goroutine + N 个 channel，每个 channel 一份
	GraphEdgeFanout

	// GraphEdgeFanin 汇合：N srcs → 1 dst
	// emit 策略：dst 等 N 个 channel 输入都到齐才执行
	GraphEdgeFanin
)

// String GraphEdgeKind → string（debug 用）
func (k GraphEdgeKind) String() string {
	switch k {
	case GraphEdgeDirect:
		return "Direct"
	case GraphEdgeFanout:
		return "Fanout"
	case GraphEdgeFanin:
		return "Fanin"
	}
	return "Unknown"
}

// BuildGraph 从 IRProgram 构建 IRGraph
//
// 步骤：
//  1. 添加所有 IRNode 到 Nodes
//  2. 添加所有 topology 边（main + 各包）
//  3. 添加所有 block.Flow 边（节点内部的 flow）
//  4. 计算每个节点的 FanIn/FanOut 度数
//  5. 找出 StartNodes（auto-exec 节点）
func BuildGraph(prog *IRProgram) *IRGraph {
	g := &IRGraph{
		Nodes: map[string]*GraphNode{},
	}

	// 1. 添加所有节点
	for _, pkg := range prog.Packages {
		for name, n := range pkg.Nodes {
			g.Nodes[name] = &GraphNode{
				Name:       name,
				IRNode:     n,
				UsedBlocks: collectUsedBlocks(n),
				IsAutoExec: hasAutoExecBlock(n),
			}
		}
	}

	// 2. 添加 topology 边
	for _, pkg := range prog.Packages {
		if pkg.Topology == nil {
			continue
		}
		for _, ek := range pkg.Topology.Edges {
			edge := classifyTopologyEdge(ek, pkg)
			if edge != nil {
				g.Edges = append(g.Edges, edge)
			}
		}
	}

	// 3. 添加 block.Flow 边（节点内部 flow 到外部节点）
	for _, pkg := range prog.Packages {
		for _, n := range pkg.Nodes {
			for _, blk := range n.Blocks {
				if !blk.IsUsed {
					continue
				}
				for _, op := range blk.Flow {
					g.Edges = append(g.Edges, &GraphEdge{
						Src:          n.Name,
						SrcAttr:      op.SrcAttr,
						Dst:          stripPkg(op.Dst),
						DstAttr:      op.DstAttr,
						Kind:         classifyFlowOpKind(op),
						Branch:       op.Branch,
						FromTopology: false,
					})
				}
			}
		}
	}

	// 4. 计算度数
	fanIn := map[string]int{}
	fanOut := map[string]int{}
	for _, e := range g.Edges {
		if e.Dst != "SYSCALL" && e.Dst != "" {
			fanIn[e.Dst]++
		}
		if e.Src != "" {
			fanOut[e.Src]++
		}
	}
	for name, n := range g.Nodes {
		n.FanInCount = fanIn[name]
		n.FanOutCount = fanOut[name]
		n.IsTerminal = (n.FanOutCount == 0)
	}

	// 5. 找出 StartNodes（auto-exec + FanIn=0）
	for name, n := range g.Nodes {
		if n.IsAutoExec && n.FanInCount == 0 {
			g.StartNodes = append(g.StartNodes, name)
		}
	}

	return g
}

// FindChains 在图上找出所有直链（每条链从 auto-exec 节点出发，序列式连接到 terminal）
//
// 规则：
//   - 起点：auto-exec 节点（StartNodes）
//   - 后续：跟随 GraphEdgeDirect 边（每个节点 FanIn=1, FanOut=1）
//   - 遇到 GraphEdgeFanout / GraphEdgeFanin → 链断
//   - 遇到 FanOutCount > 1 → 链断（多出口）
//   - 遇到 FanInCount > 1 → 链断（多入口）
//
// 返回：每条链是节点名列表（从 auto-exec 到 terminal）
func (g *IRGraph) FindChains() [][]string {
	chains := [][]string{}
	visited := map[string]bool{}

	for _, start := range g.StartNodes {
		if visited[start] {
			continue
		}
		chain := []string{start}
		visited[start] = true
		current := start

		for {
			next, isFanout := g.nextInChain(current, visited)
			if isFanout {
				break // 散连 → 链断
			}
			if next == "" {
				break // terminal → 链结束
			}
			chain = append(chain, next)
			visited[next] = true
			current = next
		}
		chains = append(chains, chain)
	}

	return chains
}

// nextInChain 返回 current 节点的下一跳
//
//   - 返回 ("", false) → terminal（没有出度）
//   - 返回 ("", true)  → 散连（fanout edge 或多出口）
//   - 返回 (name, false) → 直链下一跳
func (g *IRGraph) nextInChain(current string, visited map[string]bool) (string, bool) {
	n := g.Nodes[current]
	if n == nil {
		return "", false
	}
	if n.FanOutCount > 1 {
		return "", true // 多出口 → 散连
	}
	if n.FanOutCount == 0 {
		return "", false // terminal
	}

	// 找 outgoing edge
	for _, e := range g.Edges {
		if e.Src != current {
			continue
		}
		if e.Dst == "SYSCALL" || e.Dst == "" {
			continue
		}
		if e.Kind != GraphEdgeDirect {
			return "", true // Fanout/Fanin edge
		}
		if visited[e.Dst] {
			return "", false // cycle
		}
		return e.Dst, false
	}
	return "", false
}

// Dump 把图结构 dump 成 string（debug 输出用）
func (g *IRGraph) Dump() string {
	var sb strings.Builder
	sb.WriteString("=== IRGraph ===\n")
	sb.WriteString(fmt.Sprintf("Nodes: %d\n", len(g.Nodes)))
	sb.WriteString(fmt.Sprintf("Edges: %d\n", len(g.Edges)))
	sb.WriteString(fmt.Sprintf("StartNodes: %v\n", g.StartNodes))
	sb.WriteString("\n--- Nodes ---\n")
	for _, name := range sortedKeys(g.Nodes) {
		n := g.Nodes[name]
		auto := ""
		if n.IsAutoExec {
			auto = " [AUTO]"
		}
		if n.IsTerminal {
			auto += " [TERM]"
		}
		blocks := len(n.UsedBlocks)
		sb.WriteString(fmt.Sprintf("  %s (blocks=%d, in=%d, out=%d)%s\n",
			name, blocks, n.FanInCount, n.FanOutCount, auto))
	}
	sb.WriteString("\n--- Edges ---\n")
	for _, e := range g.Edges {
		fromTopo := ""
		if e.FromTopology {
			fromTopo = " [topo]"
		}
		branch := ""
		if e.Kind == GraphEdgeFanout {
			branch = fmt.Sprintf(" [branch=%d]", e.Branch)
		}
		sb.WriteString(fmt.Sprintf("  %s --%s--> %s  (%s.%s → %s.%s)%s%s\n",
			e.Src, e.Kind, e.Dst,
			e.Src, e.SrcAttr,
			e.Dst, e.DstAttr,
			fromTopo, branch))
	}
	sb.WriteString("\n--- Chains ---\n")
	chains := g.FindChains()
	for i, c := range chains {
		sb.WriteString(fmt.Sprintf("  chain[%d]: %v\n", i, c))
	}
	return sb.String()
}

// sortedKeys 返回 map keys 的排序列表（dump 稳定输出用）
func sortedKeys(m map[string]*GraphNode) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// collectUsedBlocks 收集 USED blocks
func collectUsedBlocks(n *IRNode) []*IRBlock {
	out := []*IRBlock{}
	for i := range n.Blocks {
		if n.Blocks[i].IsUsed {
			out = append(out, &n.Blocks[i])
		}
	}
	return out
}

// hasAutoExecBlock 节点是否有 USED 的 auto-exec block
func hasAutoExecBlock(n *IRNode) bool {
	for _, b := range n.Blocks {
		if b.IsUsed && b.IsAutoExec {
			return true
		}
	}
	return false
}

// classifyFlowOpKind 把 IRFlowOp 转成 GraphEdgeKind
//
//   - FlowOpBranchSend → GraphEdgeFanout
//   - 其他（FlowOpSend 等） → GraphEdgeDirect
func classifyFlowOpKind(op IRFlowOp) GraphEdgeKind {
	switch op.Op {
	case FlowOpBranchSend:
		return GraphEdgeFanout
	default:
		return GraphEdgeDirect
	}
}

// classifyTopologyEdge 把 topology edge 转成 GraphEdge
//
// 判定 kind：扫描 edge.Flow，找 BranchSend → GraphEdgeFanout；否则 GraphEdgeDirect
func classifyTopologyEdge(ek EdgeKey, pkg *IRPackage) *GraphEdge {
	src := stripPkg(ek.Src)
	dst := stripPkg(ek.Dst)

	// 查 pkg.Edges 拿 body
	edge := pkg.Edges[ek]
	kind := GraphEdgeDirect
	if edge != nil {
		for _, op := range edge.Flow {
			if op.Op == FlowOpBranchSend {
				kind = GraphEdgeFanout
				break
			}
		}
	}

	// 保留字节点（SYSCALL 等）当成 terminal node
	return &GraphEdge{
		Src:          src,
		Dst:          dst,
		Kind:         kind,
		FromTopology: true,
	}
}

// stripPkg 去掉跨包前缀（"io.write" → "write"）
func stripPkg(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}