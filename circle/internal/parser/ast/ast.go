// Package ast 定义 Mocker DSL 的抽象语法树（AST）节点。
//
// AST 是从 Token 流到后续阶段（semantic / IR / codegen）的中央数据结构。
// 每个节点都实现 Node 接口（带 Pos()），并通过空方法 marker() 防止类型混淆。
package ast

import "fmt"

// Pos 源码位置（行/列/字节偏移）。
// 当前由 parser 在 tokenize 之后用 wrapper 注入，line/col/offset 都从 0 开始。
type Pos struct {
	Line   int
	Col    int
	Offset int
}

// String 把 Pos 渲染成 "line:col" 字符串（错误信息用）
func (p Pos) String() string {
	return fmt.Sprintf("%d:%d", p.Line, p.Col)
}

// ──── 通用接口 ────

// Node 所有 AST 节点的根接口。
type Node interface {
	Pos() Pos
	nodeMarker()
}

// Decl 声明节点（出现在顶层或函数体内）。
type Decl interface {
	Node
	declMarker()
}

// Stmt 语句节点。
type Stmt interface {
	Node
	stmtMarker()
}

// Expr 表达式节点。
type Expr interface {
	Node
	exprMarker()
}

// TypeRef 类型引用。
type TypeRef interface {
	Node
	typeMarker()
}

// StructMember struct / node / edge body 里的成员。
type StructMember interface {
	Node
	structMemberMarker()
}

// ConnectionHop 图连接中的一跳。
type ConnectionHop interface {
	Node
	hopMarker()
}

// FlowTarget 数据流里的一个目标。
type FlowTarget interface {
	Node
	flowTargetMarker()
}

// ──── 基础节点 ────

// PosBase 简化 Pos() 实现：所有节点的 Pos 字段都是 Pos 类型
// 注意：导出，供 parser 包使用
type PosBase struct{ P Pos }

func (p PosBase) Pos() Pos { return p.P }

// ──── 文件 ────

// File 整个 .mocker 文件
type File struct {
	PosBase
	Pkg     *PackageDecl
	Imports []*ImportDecl
	Decls   []Decl
}

// PackageDecl package main
type PackageDecl struct {
	PosBase
	Name string
}

// ImportDecl import stdio
type ImportDecl struct {
	PosBase
	Path string
}

// ──── 顶层声明 ────

// EnumDecl enum Method { Post, Get, Delete }
type EnumDecl struct {
	PosBase
	Name   string
	Values []string
}

// StructKind 区分结构体 / 节点 / 边
type StructKind int

const (
	StructKindPlain StructKind = iota
	StructKindNode
	StructKindEdge
)

func (k StructKind) String() string {
	switch k {
	case StructKindPlain:
		return "struct"
	case StructKindNode:
		return "node"
	case StructKindEdge:
		return "edge"
	}
	return "?"
}

// StructDecl 表示三种之一：
//  1. 普通结构体：  @cookie { str Domain }
//  2. 节点：        hello { h := "hi" }
//  3. 边（单边）：  <out> { h>>msg>> }
type StructDecl struct {
	PosBase
	Kind     StructKind
	Exported bool
	Name     string
	Members  []StructMember
}

// EdgeDecl 边声明（带源/汇的完整形式）：hello <out> say { ... }
type EdgeDecl struct {
	PosBase
	Src  string
	Edge string
	Dst  string
	Body []Stmt
}

// ──── main 节点专用成员（实例声明 + 边连接） ────
//
// 设计：去掉了原来的 TopologyDecl，让 `main` 是个特殊节点。
// main 节点 body 里可以包含：
//   - InstanceDecl：声明一个节点实例（`hello happy`）
//   - EdgeConnDecl：在实例之间连边（`happy <out> p`）
//
// 编译器从 main 节点 body 自动分析拓扑。
//
// InstanceDecl 实例声明：`typeName varName`
//
// 例：`hello happy`           → 用 @hello 类型声明 happy 实例
//
//	`stdio.Println p`       → 用 stdio.Println 类型声明 p 实例
//
// EdgeConnDecl 边连接：`srcInstance <edgeName> dstInstance`
//
// 例：`happy <out> p`          → 在 happy 和 p 之间建 <out> 边
type InstanceDecl struct {
	PosBase
	Type string // 节点类型（"hello" / "stdio.Println"）
	Name string // 实例名（"happy" / "p"）
}

// EdgeConnDecl 边连接（main 节点专用）
//
// 例：`happy <out> p`          → srcInstance="happy", Edge="out", DstInstance="p"
type EdgeConnDecl struct {
	PosBase
	Src  string // 源实例名
	Edge string // 边名
	Dst  string // 目标实例名
}

// ──── M4.5：节点 body 内的 sub-graph 成员 ────
//
// 设计：节点 body 内的 sub-instance + sub-edge 让每个节点"自带 sub-graph"，
// 构造时递归编排（NewXxx() 创建子实例 + 调子方法）。
//
// 与 InstanceDecl/EdgeConnDecl 的区别：
//   - InstanceDecl/EdgeConnDecl：原 main body 专用
//   - SubInstanceDecl/SubEdgeDecl：节点 body 内的 sub-graph

// SubInstanceDecl 节点 body 内的 sub-instance 声明
//
// 例：`world w;`（在 hello body 内声明 world 实例）
//
//	`stdio.Println p;`（在 hello body 内声明 Println 实例）
type SubInstanceDecl struct {
	PosBase
	Type string // 节点类型（"world" / "stdio.Println"）
	Name string // 实例名（"w" / "p"）
}

// SubEdgeDecl 节点 body 内的 sub-edge 连接
//
// 例：`h <add_str> w`（在 hello body 内，h → w，via <add_str>）
type SubEdgeDecl struct {
	PosBase
	Src  string // 源实例名（节点 body 内的 sub-instance）
	Edge string // 边名
	Dst  string // 目标实例名
}

// FuncDecl 函数：main{...} / Post(str router){...} / <Post(str router)>{...}
type FuncDecl struct {
	PosBase
	Name     string
	Exported bool
	Params   []*Param
	Body     []Stmt
}

// Param 函数 / 边形参
type Param struct {
	PosBase
	Type TypeRef
	Name string
}

// ──── StructMember 的 4 种实现 ────

// FieldDecl 强类型字段：str Domain
type FieldDecl struct {
	PosBase
	Type TypeRef
	Name string
}

// VarDecl 节点体里的变量声明：h := "hi"
type VarDecl struct {
	PosBase
	Name string
	Init Expr
	Flow *FlowChain
}

// FlowDecl 裸字段 / 导出字段：h  /  h >>  /  h>>msg>>
type FlowDecl struct {
	PosBase
	Head  string
	Chain *FlowChain
}

// PortDecl 端口声明：>> str hey
type PortDecl struct {
	PosBase
	Type TypeRef
	Name string
	Body []Stmt
}

// ──── 类型 ────

// TypeName str / num / bool / 用户自定义
type TypeName struct {
	PosBase
	Name string
}

// TypeArray cookie[]
type TypeArray struct {
	PosBase
	Elem TypeRef
}

// TypePtr *nio.context
type TypePtr struct {
	PosBase
	Elem TypeRef
}

// ──── 语句 ────

// BlockStmt { ... }
type BlockStmt struct {
	PosBase
	Stmts []Stmt
}

// IfStmt if cond { ... } else { ... }
type IfStmt struct {
	PosBase
	Cond Expr
	Body *BlockStmt
	Else Stmt // *BlockStmt 或 *IfStmt
}

// ForStmt for(init; cond; post) { ... }
//
// 变体：
//   - C 风格：for(init; cond; post) { body }
//   - Go while：for cond { body }           （Init=nil, Post=nil）
//   - 无限循环：for { body }                 （Init=nil, Cond=nil, Post=nil）
//
// 括号是必须的（避免与 node body 内的 ">>" 冲突）
type ForStmt struct {
	PosBase
	Init Stmt // *ast.VarDecl / *ast.AssignStmt / nil
	Cond Expr // nil 表示条件不写
	Post Stmt // *ast.AssignStmt / nil
	Body *BlockStmt
}

// WhileStmt while(cond) { ... }
//
// Mocker 专用语法（Go 里 while 不存在）—— codegen 时转成 for cond { body }
type WhileStmt struct {
	PosBase
	Cond Expr
	Body *BlockStmt
}

// ReturnStmt return v
type ReturnStmt struct {
	PosBase
	Value Expr
}

// AssignStmt a, b := expr
//
// 变体：复合赋值（a += b）
//   - Compound = "+" / "-" / "*" / "/"
//   - CompoundVar = 源变量名（a）
//   - Rhs = 加法/减法等的右半部分
//
// 例：a += b  →  AssignStmt{Lhs:[a], Compound:"+", CompoundVar:"a", Rhs:b}
//
//	codegen 会原样 emit 成 "a += b"（Go 原生支持）
type AssignStmt struct {
	PosBase
	Lhs         []string
	Rhs         Expr
	Compound    string // "" 表示普通赋值；"+" / "-" / "*" / "/" 表示复合赋值
	CompoundVar string // 复合赋值的源变量名（暂未用，保留）
}

// Connection 图连接：hello <out> stdio.Println
type Connection struct {
	PosBase
	Hops []ConnectionHop
}

// ──── ConnectionHop 三种实现 ────

// NodeRef 节点引用：hello
type NodeRef struct {
	PosBase
	Name string
}

// EdgeRef 边引用：<out>
type EdgeRef struct {
	PosBase
	Name string
}

// CallRef 调用引用：stdio.Println(x)
type CallRef struct {
	PosBase
	Fn   Expr
	Args []Expr
}

// ──── 数据流 ────

// FlowStmt 完整数据流（单链）：hello >> out >> stdio.Println
// 只在「1 个 chain 一直串下去」时用，fan-out 走 FlowFanout。
type FlowStmt struct {
	PosBase
	Steps []*FlowStep
}

// FlowCont 续行：>>say.hay
// 上一条 FlowStmt 末尾是裸 >>（没接 target），下一行用 >> 续接 chain。
type FlowCont struct {
	PosBase
	Steps []*FlowStep
}

// FlowFanout fan-out 并发分支（核心并发语法）：
//
//	hello.h >>
//	  >>say.hay
//	  >>say.my
//	  >>say.world
//
// 语义：1 个 Src 触发 N 个并发 Branch，每个 Branch 是一条独立可执行单元（独立 goroutine）。
// 触发条件：第一个 FlowStep 后面跟 >> >>（两个连续 RRARROW）。
type FlowFanout struct {
	PosBase
	Src      FlowTarget    // 源 target（如 hello.h）
	Branches []*FlowBranch // N 个并发分支
}

// FlowBranch fan-out 的 1 条分支（1 个独立可执行单元）
// 1 步或多步：>>say.hay  或  >>say.hay>>stdio.b
type FlowBranch struct {
	PosBase
	Steps []*FlowStep
}

// FlowChain 字段后的导出链：>> / >> msg / >> msg >> target
type FlowChain struct {
	Steps []*FlowStep
}

// FlowStep 数据流的一步
type FlowStep struct {
	PosBase
	Target FlowTarget
	As     string
}

// ──── FlowTarget 两种实现 ────

// FlowIdent 标识符 / 成员访问：a / a.b / stdio.Println
type FlowIdent struct {
	PosBase
	Chain []string
	Call  []Expr
}

// FlowLiteral 字符串字面量
type FlowLiteral struct {
	PosBase
	Value string
}

// FlowExpr 任意表达式（用于 msg+nl 拼接糖、二元 op 等）
// 简单 ident / member / call 仍走 FlowIdent；遇到 BinaryExpr / UnaryExpr
// 等复杂表达式就 fallback 到 FlowExpr
type FlowExpr struct {
	PosBase
	Expr Expr
}

// ──── 表达式 ────

// IdentExpr x
type IdentExpr struct {
	PosBase
	Name string
}

// LiteralExpr "hello" / 42 / true
type LiteralExpr struct {
	PosBase
	Kind  LiteralKind
	Value string
}

// LiteralKind 字面量类型
type LiteralKind int

const (
	LitString LiteralKind = iota
	LitNumber
	LitBool
)

func (k LiteralKind) String() string {
	switch k {
	case LitString:
		return "string"
	case LitNumber:
		return "number"
	case LitBool:
		return "bool"
	}
	return "?"
}

// MemberExpr a.b / stdio.Println
type MemberExpr struct {
	PosBase
	Obj  Expr
	Name string
}

// CallExpr foo(a, b)
type CallExpr struct {
	PosBase
	Fn   Expr
	Args []Expr
}

// BinaryExpr a + b
type BinaryExpr struct {
	PosBase
	Op string
	L  Expr
	R  Expr
}

// UnaryExpr -x / !flag
type UnaryExpr struct {
	PosBase
	Op string
	X  Expr
}

// ExprStmtWrap 表达式语句（true / false / 函数调用等独立成 stmt）
type ExprStmtWrap struct {
	PosBase
	E Expr
}

// ──── marker 方法（防止类型混淆） ────

func (*File) nodeMarker()        {}
func (*File) declMarker()        {}
func (*PackageDecl) nodeMarker() {}
func (*PackageDecl) declMarker() {}
func (*ImportDecl) nodeMarker()  {}
func (*ImportDecl) declMarker()  {}

func (*EnumDecl) nodeMarker()   {}
func (*EnumDecl) declMarker()   {}
func (*StructDecl) nodeMarker() {}
func (*StructDecl) declMarker() {}
func (*EdgeDecl) nodeMarker()   {}
func (*EdgeDecl) declMarker()   {}

func (*FuncDecl) nodeMarker() {}
func (*FuncDecl) declMarker() {}

func (*FieldDecl) nodeMarker()               {}
func (*FieldDecl) structMemberMarker()       {}
func (*VarDecl) nodeMarker()                 {}
func (*VarDecl) structMemberMarker()         {}
func (*VarDecl) stmtMarker()                 {}
func (*FlowDecl) nodeMarker()                {}
func (*FlowDecl) structMemberMarker()        {}
func (*PortDecl) nodeMarker()                {}
func (*PortDecl) structMemberMarker()        {}
func (*InstanceDecl) nodeMarker()            {}
func (*InstanceDecl) structMemberMarker()    {}
func (*EdgeConnDecl) nodeMarker()            {}
func (*EdgeConnDecl) structMemberMarker()    {}
func (*SubInstanceDecl) nodeMarker()         {}
func (*SubInstanceDecl) structMemberMarker() {}
func (*SubEdgeDecl) nodeMarker()             {}
func (*SubEdgeDecl) structMemberMarker()     {}

func (*TypeName) nodeMarker()  {}
func (*TypeName) typeMarker()  {}
func (*TypeArray) nodeMarker() {}
func (*TypeArray) typeMarker() {}
func (*TypePtr) nodeMarker()   {}
func (*TypePtr) typeMarker()   {}

func (*BlockStmt) nodeMarker()          {}
func (*BlockStmt) stmtMarker()          {}
func (*BlockStmt) structMemberMarker()  {}
func (*IfStmt) nodeMarker()             {}
func (*IfStmt) stmtMarker()             {}
func (*IfStmt) structMemberMarker()     {}
func (*ForStmt) nodeMarker()            {}
func (*ForStmt) stmtMarker()            {}
func (*ForStmt) structMemberMarker()    {}
func (*WhileStmt) nodeMarker()          {}
func (*WhileStmt) stmtMarker()          {}
func (*WhileStmt) structMemberMarker()  {}
func (*ReturnStmt) nodeMarker()         {}
func (*ReturnStmt) stmtMarker()         {}
func (*ReturnStmt) structMemberMarker() {}
func (*AssignStmt) nodeMarker()         {}
func (*AssignStmt) stmtMarker()         {}
func (*AssignStmt) structMemberMarker() {}
func (*Connection) nodeMarker()         {}
func (*Connection) stmtMarker()         {}

func (*NodeRef) nodeMarker() {}
func (*NodeRef) hopMarker()  {}
func (*EdgeRef) nodeMarker() {}
func (*EdgeRef) hopMarker()  {}
func (*CallRef) nodeMarker() {}
func (*CallRef) hopMarker()  {}

func (*FlowStmt) nodeMarker()   {}
func (*FlowStmt) stmtMarker()   {}
func (*FlowCont) nodeMarker()   {}
func (*FlowCont) stmtMarker()   {}
func (*FlowFanout) nodeMarker() {}
func (*FlowFanout) stmtMarker() {}
func (*FlowBranch) nodeMarker() {}
func (*FlowBranch) stmtMarker() {}

func (*FlowIdent) nodeMarker()         {}
func (*FlowIdent) flowTargetMarker()   {}
func (*FlowLiteral) nodeMarker()       {}
func (*FlowLiteral) flowTargetMarker() {}
func (*FlowExpr) nodeMarker()          {}
func (*FlowExpr) flowTargetMarker()    {}

func (*IdentExpr) nodeMarker()    {}
func (*IdentExpr) exprMarker()    {}
func (*LiteralExpr) nodeMarker()  {}
func (*LiteralExpr) exprMarker()  {}
func (*MemberExpr) nodeMarker()   {}
func (*MemberExpr) exprMarker()   {}
func (*CallExpr) nodeMarker()     {}
func (*CallExpr) exprMarker()     {}
func (*BinaryExpr) nodeMarker()   {}
func (*BinaryExpr) exprMarker()   {}
func (*UnaryExpr) nodeMarker()    {}
func (*UnaryExpr) exprMarker()    {}
func (*ExprStmtWrap) nodeMarker() {}
func (*ExprStmtWrap) stmtMarker() {}

// Dummy reference to avoid "imported and not used" if all marker methods get tree-shook
var _ = (*Param)(nil)
