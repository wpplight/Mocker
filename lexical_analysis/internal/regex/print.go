package regex

import (
	"fmt"
	"strings"
)

// Print 把 AST 树以树形格式输出，模仿 `tree` 命令的样式。
// 便于人工查看 parser 是否正确解析了正则。
func Print(r Regex) string {
	var sb strings.Builder
	if r == nil {
		return "<nil>"
	}
	printNode(&sb, r, "", true)
	return sb.String()
}

func printNode(sb *strings.Builder, r Regex, prefix string, isLast bool) {
	sb.WriteString(prefix)
	if isLast {
		sb.WriteString("└── ")
	} else {
		sb.WriteString("├── ")
	}
	sb.WriteString(nodeLabel(r))
	sb.WriteString("\n")

	childPrefix := prefix
	if isLast {
		childPrefix += "    "
	} else {
		childPrefix += "│   "
	}
	children := nodeChildren(r)
	for i, c := range children {
		printNode(sb, c, childPrefix, i == len(children)-1)
	}
}

func nodeLabel(r Regex) string {
	switch n := r.(type) {
	case *Literal:
		return fmt.Sprintf("Literal %q", string(n.Ch))
	case *CharClass:
		body := formatCharClassChars(n.Chars)
		if n.Negate {
			return "CharClass [^" + body + "]"
		}
		return "CharClass [" + body + "]"
	case *Dot:
		return "Dot"
	case *Concat:
		return "Concat"
	case *Union:
		return "Union"
	case *Star:
		return "Star"
	case *Plus:
		return "Plus"
	case *Optional:
		return "Optional"
	case *Repeat:
		switch {
		case n.Max == -1:
			return fmt.Sprintf("Repeat {%d,}", n.Min)
		case n.Min == n.Max:
			return fmt.Sprintf("Repeat {%d}", n.Min)
		default:
			return fmt.Sprintf("Repeat {%d,%d}", n.Min, n.Max)
		}
	case *Group:
		return "Group"
	case *Empty:
		return "Empty"
	}
	return "Unknown"
}

func nodeChildren(r Regex) []Regex {
	switch n := r.(type) {
	case *Literal, *CharClass, *Dot, *Empty:
		return nil
	case *Concat:
		return []Regex{n.Left, n.Right}
	case *Union:
		return []Regex{n.Left, n.Right}
	case *Star:
		return []Regex{n.Inner}
	case *Plus:
		return []Regex{n.Inner}
	case *Optional:
		return []Regex{n.Inner}
	case *Repeat:
		return []Regex{n.Inner}
	case *Group:
		return []Regex{n.Inner}
	}
	return nil
}

// formatCharClassChars 把字符集合格式化为可读字符串：
//   - 连续范围用 a-z
//   - 单字符用 a
//   - 非可打印字符用 \xHH
func formatCharClassChars(chars []byte) string {
	if len(chars) == 0 {
		return ""
	}
	var sb strings.Builder
	i := 0
	for i < len(chars) {
		// 找连续范围
		j := i
		for j+1 < len(chars) && chars[j+1] == chars[j]+1 {
			j++
		}
		if j-i >= 2 {
			// 范围（至少 3 个字符才算，否则按单字符处理更清晰）
			sb.WriteString(formatChar(chars[i]))
			sb.WriteString("-")
			sb.WriteString(formatChar(chars[j]))
		} else {
			for k := i; k <= j; k++ {
				if k > i {
					sb.WriteString(" ")
				}
				sb.WriteString(formatChar(chars[k]))
			}
		}
		i = j + 1
	}
	return sb.String()
}

func formatChar(c byte) string {
	switch {
	case c >= 'a' && c <= 'z',
		c >= 'A' && c <= 'Z',
		c >= '0' && c <= '9':
		return string(c)
	case c == ' ':
		return "SP"
	case c == '\t':
		return "TAB"
	case c == '\n':
		return "LF"
	case c == '\r':
		return "CR"
	case c == 0:
		return "\\0"
	case c < 0x20 || c == 0x7f:
		return fmt.Sprintf("\\x%02x", c)
	default:
		return string(c)
	}
}
