package dfa

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// ─────────────────────────────────────────────
// ASCII 文本输出
// ─────────────────────────────────────────────

// ToTXT 返回 DFA 的可读文本表示。
func (d *DFA) ToTXT() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "DFA: %d states\n", d.NumStates)
	fmt.Fprintf(&sb, "Start: s%d\n", d.Start)
	fmt.Fprintf(&sb, "Accepts:\n")
	for state, tag := range d.Accepts {
		fmt.Fprintf(&sb, "  s%d → %s\n", state, tag)
	}
	sb.WriteString("\nTransitions:\n")
	for state := 0; state < d.NumStates; state++ {
		if d.Trans[state] == nil {
			continue
		}
		fmt.Fprintf(&sb, "  s%d:\n", state)
		// 按字符排序
		var chars []byte
		for ch := range d.Trans[state] {
			chars = append(chars, ch)
		}
		sort.Slice(chars, func(i, j int) bool { return chars[i] < chars[j] })
		for _, ch := range chars {
			tos := d.Trans[state][ch]
			fmt.Fprintf(&sb, "    %s → %v\n", formatCharDFA(ch), tos)
		}
	}
	return sb.String()
}

// ─────────────────────────────────────────────
// DOT 输出
// ─────────────────────────────────────────────

// ToDOT 返回 DFA 的 Graphviz DOT 文本。
// 针对大 DFA 做了优化：
//  1. 连续字符合并成区间（"0-9, A-Z, _" 而非 "0|1|...|Z|_"），边数大幅减少
//  2. concentrate=true：合并 source/target 相同的平行边
//  3. size 上限 40x40 英寸、dpi=100：防止巨大 PNG
//  4. splines=ortho：大图用直角边比曲线边更清晰且快
func (d *DFA) ToDOT() string {
	var sb strings.Builder
	sb.WriteString("digraph dfa {\n")
	sb.WriteString("  rankdir=LR;\n")
	// 大图用 ortho（正交线），小图（< 20 状态）用 spline（曲线）更好看
	if d.NumStates > 20 {
		sb.WriteString("  splines=ortho;\n")
	} else {
		sb.WriteString("  splines=spline;\n")
	}
	// 全局参数
	sb.WriteString("  graph [bgcolor=\"white\", pad=0.5,\n")
	sb.WriteString("    nodesep=0.3, ranksep=0.6,\n")
	sb.WriteString("    dpi=100, ratio=compress, concentrate=true,\n")
	sb.WriteString("    overlap=false, esep=0.1,\n")
	sb.WriteString("    size=\"40,40!\", ratio=fill];\n") // 强制不超过 40x40 英寸
	sb.WriteString("  node [shape=circle, fontname=\"Helvetica\", fontsize=11, penwidth=1.2, margin=0.06, fixedsize=false];\n")
	sb.WriteString("  edge [fontname=\"Helvetica\", fontsize=9, penwidth=0.7, color=\"#333333\", arrowsize=0.7];\n")
	sb.WriteString("  start [shape=point, width=0.12, label=\"\", penwidth=1.0];\n")
	fmt.Fprintf(&sb, "  start -> s%d;\n", d.Start)

	// 接受态：双圈
	for state, tag := range d.Accepts {
		fmt.Fprintf(&sb, "  s%d [shape=doublecircle, label=\"s%d\\n(%s)\", penwidth=1.4];\n",
			state, state, tag)
	}

	// 转移：按 (state, target) 分组，字符合并成区间
	for state := 0; state < d.NumStates; state++ {
		trans := d.Trans[state]
		if len(trans) == 0 {
			continue
		}
		// 按目标状态分组
		byTarget := make(map[int][]byte)
		for ch, tos := range trans {
			for _, to := range tos {
				byTarget[to] = append(byTarget[to], ch)
			}
		}
		// 排序目标
		var targets []int
		for to := range byTarget {
			targets = append(targets, to)
		}
		sort.Ints(targets)
		for _, to := range targets {
			chars := byTarget[to]
			label := charRangesLabel(chars)
			fmt.Fprintf(&sb, "  s%d -> s%d [label=%q];\n", state, to, label)
		}
	}

	sb.WriteString("}\n")
	return sb.String()
}

// charRangesLabel 把字节列表合并成 "0-9, A-Z, _, a, c" 这样的紧凑标签。
func charRangesLabel(chars []byte) string {
	if len(chars) == 0 {
		return ""
	}
	// 排序 + 去重
	seen := make(map[byte]bool, len(chars))
	uniq := make([]byte, 0, len(chars))
	for _, c := range chars {
		if !seen[c] {
			seen[c] = true
			uniq = append(uniq, c)
		}
	}
	sort.Slice(uniq, func(i, j int) bool { return uniq[i] < uniq[j] })

	var parts []string
	i := 0
	for i < len(uniq) {
		start := uniq[i]
		j := i
		// 找连续区间（允许 gap=1）
		for j+1 < len(uniq) && uniq[j+1] == uniq[j]+1 {
			j++
		}
		if start == uniq[j] {
			parts = append(parts, formatCharDFA(start))
		} else {
			parts = append(parts, formatCharDFA(start)+"-"+formatCharDFA(uniq[j]))
		}
		i = j + 1
	}
	return strings.Join(parts, ", ")
}

func sortedKeysOfTrans(m map[byte][]int) []byte {
	out := make([]byte, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// formatCharDFA 格式化 DFA 边的字符标签。
func formatCharDFA(c byte) string {
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
// PNG 输出（用系统 dot 渲染）
// ─────────────────────────────────────────────

// ToPNG 用系统 `dot` 命令渲染 DFA 为 PNG。
func (d *DFA) ToPNG() ([]byte, error) {
	dotPath, err := writeTempDotDFA(d)
	if err != nil {
		return nil, err
	}
	defer os.Remove(dotPath)

	pngPath := dotPath + ".png"
	defer os.Remove(pngPath)

	cmd := exec.Command("dot", "-Tpng", dotPath, "-o", pngPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("dot: %w: %s", err, stderr.String())
	}
	return os.ReadFile(pngPath)
}

func writeTempDotDFA(d *DFA) (string, error) {
	f, err := os.CreateTemp("", "dfa-*.dot")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(d.ToDOT()); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
