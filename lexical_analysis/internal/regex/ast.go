// Package regex 提供正则表达式的解析与 AST 表示。
//
// AST 是 glex 流水线中 M2（正则解析）的输出、M3（Thompson 构造 NFA）的输入。
// 节点类型覆盖常见完整的正则语法：字面字符、字符类、连接、选择、量词、分组、转义。
package regex

// Regex 是所有正则 AST 节点的接口。
// 通过 type assertion 可以还原具体类型。
type Regex interface {
	// regexNode 是私有方法，强制外部只能用具体类型（*Literal, *Concat 等）。
	regexNode()
}

// Literal 字面字符。Ch 是字符的字节值。
// 转义序列在 parser 阶段已展开，所以 '\d' 在 AST 里就是 *CharClass 而不是 *Literal。
type Literal struct {
	Ch byte
}

func (*Literal) regexNode() {}

// CharClass 字符类。Chars 是已展开范围的字符集合（去重，未排序）。
// Negate 为 true 时表示取反（如 [^abc]）。
type CharClass struct {
	Chars  []byte
	Negate bool
}

func (*CharClass) regexNode() {}

// Dot 任意单字符（除换行外，简化起见我们匹配任意 byte）。
type Dot struct{}

func (*Dot) regexNode() {}

// Concat 连接：A B
type Concat struct {
	Left, Right Regex
}

func (*Concat) regexNode() {}

// Union 选择：A | B（左结合）
type Union struct {
	Left, Right Regex
}

func (*Union) regexNode() {}

// Star 0 次或多次：A*
type Star struct {
	Inner Regex
}

func (*Star) regexNode() {}

// Plus 1 次或多次：A+
type Plus struct {
	Inner Regex
}

func (*Plus) regexNode() {}

// Optional 0 次或 1 次：A?
type Optional struct {
	Inner Regex
}

func (*Optional) regexNode() {}

// Repeat 限定次数：
//   - {n}     → Min=n, Max=n
//   - {n,}    → Min=n, Max=-1（-1 表示无穷）
//   - {n,m}   → Min=n, Max=m
type Repeat struct {
	Min   int
	Max   int // -1 表示无上界
	Inner Regex
}

func (*Repeat) regexNode() {}

// Group 分组：(A)，语法糖，等价于 A 自身但改变优先级。
type Group struct {
	Inner Regex
}

func (*Group) regexNode() {}

// Empty 空正则：匹配空串。出现在 () 内部或 a| 中作为空操作数。
type Empty struct{}

func (*Empty) regexNode() {}
