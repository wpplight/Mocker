// Package ast 定义 Mocker DSL 的抽象语法树（AST）节点。
//
// AST 是从 Token 流到后续阶段（semantic / IR / codegen）的中央数据结构。
// 每个节点都实现 Node 接口（带 Pos()），并通过空方法 marker() 防止类型混淆。
package ast

// Pos 源码位置（行/列/字节偏移）。
// 当前由 parser 在 tokenize 之后用 wrapper 注入，line/col/offset 都从 0 开始。
type Pos struct {
	Line   int
	Col    int
	Offset int
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

// TopologyDecl 拓扑块：<PkgName> { <EdgeRef>* }
// 块名必须 == 当前 package 名（编译器用此约束验证）
// 复用 EdgeDecl 形态，body 留空（结构层）—— 真正的走线在 top-level EdgeDecl 里
type TopologyDecl struct {
	PosBase
	Name  string      // 块名（== 包名）
	Edges []*EdgeDecl // 边引用列表（无 body）
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

// ReturnStmt return v
type ReturnStmt struct {
	PosBase
	Value Expr
}

// AssignStmt a, b := expr
type AssignStmt struct {
	PosBase
	Lhs []string
	Rhs Expr
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

func (*EnumDecl) nodeMarker()     {}
func (*EnumDecl) declMarker()     {}
func (*StructDecl) nodeMarker()   {}
func (*StructDecl) declMarker()   {}
func (*EdgeDecl) nodeMarker()     {}
func (*EdgeDecl) declMarker()     {}
func (*TopologyDecl) nodeMarker() {}
func (*TopologyDecl) declMarker() {}
func (*FuncDecl) nodeMarker()     {}
func (*FuncDecl) declMarker()     {}

func (*FieldDecl) nodeMarker()         {}
func (*FieldDecl) structMemberMarker() {}
func (*VarDecl) nodeMarker()           {}
func (*VarDecl) structMemberMarker()   {}
func (*VarDecl) stmtMarker()           {}
func (*FlowDecl) nodeMarker()          {}
func (*FlowDecl) structMemberMarker()  {}
func (*PortDecl) nodeMarker()          {}
func (*PortDecl) structMemberMarker()  {}

func (*TypeName) nodeMarker()  {}
func (*TypeName) typeMarker()  {}
func (*TypeArray) nodeMarker() {}
func (*TypeArray) typeMarker() {}
func (*TypePtr) nodeMarker()   {}
func (*TypePtr) typeMarker()   {}

func (*BlockStmt) nodeMarker()  {}
func (*BlockStmt) stmtMarker()  {}
func (*IfStmt) nodeMarker()     {}
func (*IfStmt) stmtMarker()     {}
func (*ReturnStmt) nodeMarker() {}
func (*ReturnStmt) stmtMarker() {}
func (*AssignStmt) nodeMarker() {}
func (*AssignStmt) stmtMarker() {}
func (*Connection) nodeMarker() {}
func (*Connection) stmtMarker() {}

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
