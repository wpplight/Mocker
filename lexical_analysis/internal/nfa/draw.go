package nfa

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/goccy/go-graphviz"
)

// ─────────────────────────────────────────────
// ASCII 文本输出（零依赖，调试用）
// ─────────────────────────────────────────────

// ToTXT 返回 NFA 的可读文本表示。
func (n *NFA) ToTXT() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "NFA: %d states, %d accept(s)\n", n.NextID, len(n.Accepts))
	fmt.Fprintf(&sb, "Start: s%d\n", n.Start)
	fmt.Fprintf(&sb, "Accepts: ")
	for i, acc := range n.Accepts {
		if i > 0 {
			sb.WriteString(", ")
		}
		tag := n.AcceptTags[acc]
		fmt.Fprintf(&sb, "s%d(%s)", acc, tag)
	}
	sb.WriteString("\n\nTransitions:\n")

	for state := 0; state < n.NextID; state++ {
		fmt.Fprintf(&sb, "  s%d:\n", state)
		if eps, ok := n.Epsilon[state]; ok && len(eps) > 0 {
			fmt.Fprintf(&sb, "    ε → %v\n", eps)
		}
		for ch, tos := range n.Trans[state] {
			label := formatChar(ch)
			fmt.Fprintf(&sb, "    %s → %v\n", label, tos)
		}
	}
	return sb.String()
}

// ─────────────────────────────────────────────
// DOT 输出（Graphviz 通用格式）
// ─────────────────────────────────────────────

// ToDOT 返回 NFA 的 Graphviz DOT 文本。
// 关键优化：把连续字符合并成范围标签（如 a-z），避免 26 条平行边。
func (n *NFA) ToDOT() string {
	var sb strings.Builder
	sb.WriteString("digraph nfa {\n")
	sb.WriteString("  rankdir=LR;\n")
	sb.WriteString("  splines=true;\n")
	sb.WriteString("  graph [bgcolor=\"white\", pad=0.3, nodesep=0.5, ranksep=0.8];\n")
	sb.WriteString("  node [shape=circle, fontname=\"Helvetica\", fontsize=12, penwidth=1.2, fixedsize=false, margin=0.08];\n")
	sb.WriteString("  edge [fontname=\"Helvetica\", fontsize=10, penwidth=0.8, color=\"#333333\"];\n")
	sb.WriteString("  start [shape=point, width=0.12, label=\"\", penwidth=1.0];\n")
	fmt.Fprintf(&sb, "  start -> s%d;\n", n.Start)

	// 接受态集合
	acceptSet := make(map[int]bool)
	for _, acc := range n.Accepts {
		acceptSet[acc] = true
	}
	for acc := range acceptSet {
		tag := n.AcceptTags[acc]
		fmt.Fprintf(&sb, "  s%d [shape=doublecircle, label=\"s%d\\n(%s)\", penwidth=1.4];\n",
			acc, acc, tag)
	}

	// ε 边（虚线、灰色）
	for from, tos := range n.Epsilon {
		for _, to := range tos {
			fmt.Fprintf(&sb, "  s%d -> s%d [label=\"ε\", style=dashed, color=\"#888888\", penwidth=0.6];\n", from, to)
		}
	}

	// 字符边（合并连续字符为范围）
	for from, trans := range n.Trans {
		// 按目标状态分组字符
		for to, label := range n.mergedCharLabels(trans) {
			fmt.Fprintf(&sb, "  s%d -> s%d [label=%q];\n", from, to, label)
		}
	}

	sb.WriteString("}\n")
	return sb.String()
}

// mergedCharLabels 把字符类按目标状态分组，连续字符合并成范围。
// 返回：map[目标状态]label（如 {1: "a-z A-Z _"}）
func (n *NFA) mergedCharLabels(trans map[byte][]int) map[int]string {
	// key: 目标状态（取第一个，多个目标画多条边）
	// val: 该目标对应的字符集合
	targetChars := make(map[int][]byte)
	for ch, tos := range trans {
		if len(tos) == 0 {
			continue
		}
		// 简化处理：NFA 应该是 deterministic（同一字符单目标），多目标画多条
		for _, to := range tos {
			targetChars[to] = append(targetChars[to], ch)
		}
	}
	result := make(map[int]string)
	for to, chars := range targetChars {
		// 排序字符
		sort.Slice(chars, func(i, j int) bool { return chars[i] < chars[j] })
		// 合并连续字符为范围
		result[to] = formatCharSet(chars)
	}
	return result
}

// formatCharSet 把有序字符集合格式化为可读范围标签。
// 例如 [a,b,c,d,h,i,j] → "a-d h-j"，特殊字符单独显示。
func formatCharSet(chars []byte) string {
	if len(chars) == 0 {
		return ""
	}
	var parts []string
	i := 0
	for i < len(chars) {
		j := i
		// 找连续范围
		for j+1 < len(chars) && chars[j+1] == chars[j]+1 {
			j++
		}
		// chars[i..j] 是连续范围
		if j-i >= 2 {
			parts = append(parts, formatChar(chars[i])+"-"+formatChar(chars[j]))
		} else {
			for k := i; k <= j; k++ {
				parts = append(parts, formatChar(chars[k]))
			}
		}
		i = j + 1
	}
	return strings.Join(parts, " ")
}

// formatChar 把字符格式化为可读字符（处理不可打印字符）。
func formatChar(c byte) string {
	switch {
	case c >= 0x20 && c < 0x7f:
		return string(c)
	case c == '\t':
		return "TAB"
	case c == '\n':
		return "LF"
	case c == '\r':
		return "CR"
	case c == 0:
		return "\\0"
	default:
		return fmt.Sprintf("\\x%02x", c)
	}
}

// ─────────────────────────────────────────────
// PNG 输出：优先用系统 dot，goccy 作 fallback
// ─────────────────────────────────────────────

// ToPNG 把 NFA 渲染成 PNG。
// 优先调用系统 `dot` 命令（输出最完整），如果不可用则用 goccy/go-graphviz。
func (n *NFA) ToPNG() ([]byte, error) {
	// 1. 尝试系统 dot 命令
	if png, err := renderBySystemDot(n); err == nil {
		return png, nil
	}
	// 2. fallback: goccy
	return n.renderByGoccy()
}

// renderBySystemDot 调用系统的 `dot` 命令渲染。
func renderBySystemDot(n *NFA) ([]byte, error) {
	dotPath, err := writeTempDot(n)
	if err != nil {
		return nil, err
	}
	defer removeFile(dotPath)

	pngPath := dotPath + ".png"
	defer removeFile(pngPath)

	// 调用系统 dot
	cmd := exec.Command("dot", "-Tpng", dotPath, "-o", pngPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("dot: %w: %s", err, stderr.String())
	}

	// 读取 PNG
	return readFile(pngPath)
}

// renderByGoccy 用 goccy/go-graphviz 渲染（fallback）。
func (n *NFA) renderByGoccy() ([]byte, error) {
	ctx := context.Background()
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("graphviz.New: %w", err)
	}
	graph, err := g.Graph()
	if err != nil {
		return nil, fmt.Errorf("g.Graph: %w", err)
	}
	defer graph.Close()

	acceptSet := make(map[int]bool)
	for _, acc := range n.Accepts {
		acceptSet[acc] = true
	}
	nodeCache := make(map[int]*graphviz.Node)
	getNode := func(id int) (*graphviz.Node, error) {
		if cached, ok := nodeCache[id]; ok {
			return cached, nil
		}
		name := fmt.Sprintf("s%d", id)
		node, err := graph.CreateNodeByName(name)
		if err != nil {
			return nil, err
		}
		node.SetShape(graphviz.CircleShape)
		node.SetFontName("Helvetica")
		node.SetFontSize(12)
		node.SetPenWidth(1.2)
		if acceptSet[id] {
			node.SetShape(graphviz.DoubleCircleShape)
			tag := n.AcceptTags[id]
			node.SetLabel(fmt.Sprintf("s%d\n(%s)", id, tag))
			node.SetPenWidth(1.4)
		} else {
			node.SetLabel(name)
		}
		nodeCache[id] = node
		return node, nil
	}

	startNode, err := graph.CreateNodeByName("start")
	if err != nil {
		return nil, err
	}
	startNode.SetShape(graphviz.PointShape)
	startNode.SetLabel("")
	startNode.SetFixedSize(true)
	startNode.SetWidth(0.12)
	startNode.SetPenWidth(1.0)

	fromNode, err := getNode(n.Start)
	if err != nil {
		return nil, err
	}
	startEdge, err := graph.CreateEdgeByName("", startNode, fromNode)
	if err != nil {
		return nil, err
	}
	startEdge.SetPenWidth(1.0)
	startEdge.SetColor("#333333")

	for from, tos := range n.Epsilon {
		f, err := getNode(from)
		if err != nil {
			return nil, err
		}
		for _, to := range tos {
			t, err := getNode(to)
			if err != nil {
				return nil, err
			}
			edge, err := graph.CreateEdgeByName("", f, t)
			if err != nil {
				return nil, err
			}
			edge.SetLabel("ε")
			edge.SetStyle("dashed")
			edge.SetColor("#888888")
			edge.SetPenWidth(0.6)
		}
	}

	for from, trans := range n.Trans {
		f, err := getNode(from)
		if err != nil {
			return nil, err
		}
		for to, label := range n.mergedCharLabels(trans) {
			t, err := getNode(to)
			if err != nil {
				return nil, err
			}
			edge, err := graph.CreateEdgeByName("", f, t)
			if err != nil {
				return nil, err
			}
			edge.SetLabel(label)
			edge.SetColor("#333333")
			edge.SetPenWidth(0.8)
		}
	}

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.PNG, &buf); err != nil {
		return nil, fmt.Errorf("g.Render: %w", err)
	}
	return buf.Bytes(), nil
}

// ── 文件辅助 ──
func writeTempDot(n *NFA) (string, error) {
	f, err := os.CreateTemp("", "nfa-*.dot")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(n.ToDOT()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func removeFile(path string) {
	_ = os.Remove(path)
}
