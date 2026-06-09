package nfa

import "lexical_analysis/internal/regex"

// travEntry 遍历栈条目：节点 + 是否已访问过子节点。
type travEntry struct {
	node    regex.Regex
	visited bool
}

// fragment Thompson 构造的子片段（s=start, e=end）。
type fragment struct {
	s, e int
}

// Builder 是 NFA 的构造器。
// 装构造时辅助状态：fragment 栈 + 遍历栈。
// 构造完通过 NFA() 取出纯数据。
type Builder struct {
	nfa       *NFA
	fragStack []fragment
	travStack []travEntry
}

// NewBuilder 新建一个空 Builder。
func NewBuilder() *Builder {
	return &Builder{
		nfa:       New(),
		fragStack: make([]fragment, 0, 16),
		travStack: make([]travEntry, 0, 16),
	}
}

// NFA 返回构造完成的 NFA（纯数据）。
func (b *Builder) NFA() *NFA { return b.nfa }

// ── fragment 栈操作 ──
func (b *Builder) pushFrag(s, e int) {
	b.fragStack = append(b.fragStack, fragment{s, e})
}

func (b *Builder) popFrag() (s, e int) {
	f := b.fragStack[len(b.fragStack)-1]
	b.fragStack = b.fragStack[:len(b.fragStack)-1]
	return f.s, f.e
}
