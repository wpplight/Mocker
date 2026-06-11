// Package ir 实现 Mocker 的中间表示层（M4）
//
// IR = AST 拍平后的可线性遍历形态
//   - 每条信息 codegen 一次拿全（不用深遍历 AST）
//   - 包含拓扑裁减（pruning）信息
//   - 包含运行时语义（goroutine 策略、channel/函数调用）
//
// 设计（按用户拍板）：
//   - 节点 = struct-like，可有入度前的初始化
//   - 边决定 goroutine 策略（sync = 函数调用，async = channel）
//   - 拓扑块裁减：emit 时只生成用到的 block
//   - async edge 可选 ack channel（sync backpressure）
package ir

import (
	"circle/internal/parser/ast"
)

// ──── 顶层 IR ────

// IRProgram 整个编译产物
//
// 一个 program = 多个包 + main 包的拓扑
type IRProgram struct {
	PkgName  string                // 通常是 "main"
	Packages map[string]*IRPackage // pkg_name → 包
	Topology *IRTopology           // 入口拓扑（main 包专属）
}

// IRPackage 单个包的 IR
//
// 每个包都可以有自己的 Topology（不只 main 包）：
//   - main 包的 Topology = 启动序列（程序从这里开始跑）
//   - 其他包的 Topology = 内部路由（描述包内数据怎么流动）
//
// 例：stdio 包有
//
//	stdio {
//	    Println <write> io.write     // 内部拓扑：Println 收到的数据路由给 io.write
//	}
//
// 这个拓扑告诉编译器：stdio 包内部，Println 收到的数据应该路由给 io.write。
type IRPackage struct {
	Name     string              // 包名
	Nodes    map[string]*IRNode  // 节点名 → 节点
	Edges    map[EdgeKey]*IREdge // 边 key → 边
	Topology *IRTopology         // 该包的拓扑（可能为 nil；main 一定有）
	IsMain   bool                // 是否是入口包（PackageName == "main"）
}

// EdgeKey 边的三元组 key
type EdgeKey struct {
	Src  string
	Name string
	Dst  string
}

// ──── 节点 IR ────

// IRNode 一个节点（@name）的完整 IR 视图
//
// 设计：
//   - Inputs / Outputs 是入度 / 出度（暴露给外部的接口）
//   - Init 是入度前执行的初始化（用户可在 @node 顶部声明）
//   - Blocks 是按入度切分的执行单元（codegen 时按 Topology 用到的部分裁减）
//   - State 是节点的持久字段（goroutine 之间共享）
//   - AutoExec 标记无入度的 block（启动时直接跑）
type IRNode struct {
	Name     string
	Kind     NodeKind // NodeKindNode / NodeKindEdge / NodeKindPlain
	Exported bool     // @ 前缀（包外可见）

	// 接口面
	Inputs  []IRInput
	Outputs []IROutput

	// 内部结构
	Init   []IRStmt  // 入度到达前的初始化（变量定义、表达式等）
	Blocks []IRBlock // 按入度切分的 block（一个入度 → 一个 block，多入度 → 多 block）

	// 状态（节点级别持久字段）
	State []IRField

	// codegen 用（由 topology 分析填入）
	AutoExec     bool     // 是否至少有一个 block 是 auto-exec（无入度）
	UsedBlocks   []int    // topology 引用到的 block 索引（用于裁减）
	ReferencedBy []string // 被哪些边引用（优化用）

	// 源位置（debug / 错误信息用）
	Pos ast.Pos
}

// IRInput 一个入度（>> type name）
type IRInput struct {
	Name string
	Type IRType
	Pos  ast.Pos
}

// IROutput 一个出度（name >>）
//
// 出度没有显式类型（值由 block 计算得到）
type IROutput struct {
	Name string
	Type IRType // 推断得到（TypeUnknown 表示未知）
	Pos  ast.Pos
}

// IRField 一个节点字段（持久状态）
type IRField struct {
	Name string
	Type IRType
	Pos  ast.Pos
}

// IRBlock 一个执行 block（按入度切分）
//
// 设计（按用户拍板）：
//   - 每个 block 有 a 个入度（Inputs）和 b 个出度（Outputs）
//   - block 体（Stmts）在 codegen 时按 topology 用到的部分裁减
//   - 一个 block 是 auto-exec ⟺ Inputs 为空
type IRBlock struct {
	Inputs     []string // 触发本 block 的入度名
	Outputs    []string // 本 block 出口的出度名
	Stmts      []IRStmt // block 体（按用户写的顺序）
	IsAutoExec bool     // 无入度（启动即跑）
	Pos        ast.Pos
}

// NodeKind 节点种类
type NodeKind int

const (
	NodeKindPlain NodeKind = iota
	NodeKindNode
	NodeKindEdge
)

func (k NodeKind) String() string {
	switch k {
	case NodeKindPlain:
		return "struct"
	case NodeKindNode:
		return "node"
	case NodeKindEdge:
		return "edge"
	}
	return "?"
}

// ──── 边 IR ────

// IREdge 一条边的 IR 视图
//
// 设计（按用户拍板）：
//   - sync edge = 函数调用（emit 成 func call）
//   - async edge = goroutine spawn + channel send
//   - async edge 可选 ack channel（codegen 根据 HasAck 决定是否生成 ack）
type IREdge struct {
	Src  string
	Name string
	Dst  string

	// 同步 vs 异步（由 semantic 决定）
	Kind EdgeKind

	// 异步时的分支数（fanout 数）
	Branches int

	// 是否启用 ack channel（async + 用户在 edge body 中定义了 ack）
	HasAck bool

	// body 已展开的 flow 操作（IR 阶段把 AST FlowStmt/FlowFanout 拍平）
	Flow []IRFlowOp

	// 源位置
	Pos ast.Pos
}

// EdgeKind 边的运行时形态
type EdgeKind int

const (
	EdgeSync  EdgeKind = iota // 同步，函数调用
	EdgeAsync                 // 异步，goroutine + channel
)

func (k EdgeKind) String() string {
	switch k {
	case EdgeSync:
		return "sync"
	case EdgeAsync:
		return "async"
	}
	return "?"
}

// IRFlowOp 一条 flow 操作（已展开的 edge body）
//
// 形式：
//   - 单链: 多个 SendOp 连成一串
//   - fan-out: 一个 SendOp + 多个 BranchSendOp（不同 dst）
//   - 函数调用: CallOp（sync edge 用）
type IRFlowOp struct {
	Op      FlowOpKind
	Src     string // 源节点名
	SrcAttr string // 源节点的属性（in/out 名）
	Dst     string // 目标节点名
	DstAttr string // 目标节点的属性（in 名）
	IsAck   bool   // 是否是 ack channel 的 send（用于 backpressure）
	Branch  int    // fan-out 分支号（0 = 主路径，1+ = 分支）
}

// FlowOpKind flow 操作类型
type FlowOpKind int

const (
	FlowOpSend       FlowOpKind = iota // 异步：channel send
	FlowOpCall                         // 同步：函数调用
	FlowOpBranchSend                   // 异步 fan-out：分支 channel send
)

func (k FlowOpKind) String() string {
	switch k {
	case FlowOpSend:
		return "send"
	case FlowOpCall:
		return "call"
	case FlowOpBranchSend:
		return "branch_send"
	}
	return "?"
}

// ──── 拓扑 IR ────

// IRTopology main 包的拓扑块 IR
//
// 设计：
//   - Edges: 启动时建哪些连线（按 (src, name, dst) 三元组）
//   - AutoExecNodes: 无入度的节点（创建即跑）
//   - 这两条信息决定 codegen 怎么生成 main()
type IRTopology struct {
	Edges         []EdgeKey
	AutoExecNodes []string
	AllNodes      []string // 拓扑里出现过的所有节点（去重）
	Pos           ast.Pos
}

// ──── 语句 IR ────

// IRStmt block 体内的语句
//
// MVP 用 AST 直接拍平过来（codegen 时再细分）
// 后续可改成自己的 Stmt 节点，更利于优化
type IRStmt interface{ irStmtMarker() }

// IRSimpleStmt 简单语句（赋值、变量声明等）
type IRSimpleStmt struct {
	Kind string // "assign" / "vardecl" / "fielddecl"
	Text string // 原始代码（codegen 时直接拼接）
	Pos  ast.Pos
}

func (*IRSimpleStmt) irStmtMarker() {}

// IRFlowStmt flow 语句（>> 链）
type IRFlowStmt struct {
	Ops []IRFlowOp
	Pos ast.Pos
}

func (*IRFlowStmt) irStmtMarker() {}

// IRExprStmt 表达式语句（裸表达式）
type IRExprStmt struct {
	Text string // 原始表达式
	Pos  ast.Pos
}

func (*IRExprStmt) irStmtMarker() {}

// ──── 类型系统 ────

// IRType 简化的类型表示（sum type）
//
// MVP：基础类型 + 用户自定义（opaque，按名字引用）
type IRType struct {
	Kind TypeKind
	Name string // 用户自定义类型时填这个
}

// TypeKind 类型种类
type TypeKind int

const (
	TypeUnknown TypeKind = iota
	TypeStr
	TypeNum
	TypeBool
	TypeByte
	TypeAny
)

func (k TypeKind) String() string {
	switch k {
	case TypeStr:
		return "str"
	case TypeNum:
		return "num"
	case TypeBool:
		return "bool"
	case TypeByte:
		return "byte"
	case TypeAny:
		return "any"
	default:
		return "?"
	}
}

// 便捷构造器
func Str() IRType             { return IRType{Kind: TypeStr} }
func Num() IRType             { return IRType{Kind: TypeNum} }
func Bool() IRType            { return IRType{Kind: TypeBool} }
func Byte() IRType            { return IRType{Kind: TypeByte} }
func Any() IRType             { return IRType{Kind: TypeAny} }
func User(name string) IRType { return IRType{Kind: TypeStr, Name: name} } // MVP：用户类型暂当 str

// IRTypeOf 把 AST TypeRef 转成 IRType
func IRTypeOf(ref ast.TypeRef) IRType {
	if ref == nil {
		return IRType{Kind: TypeUnknown}
	}
	switch t := ref.(type) {
	case *ast.TypeName:
		switch t.Name {
		case "str":
			return Str()
		case "num":
			return Num()
		case "bool":
			return Bool()
		case "byte":
			return Byte()
		case "any":
			return Any()
		default:
			return IRType{Kind: TypeStr, Name: t.Name} // 用户自定义
		}
	case *ast.TypeArray:
		return Any() // 数组 MVP 用 any
	case *ast.TypePtr:
		return Any() // 指针 MVP 用 any
	}
	return IRType{Kind: TypeUnknown}
}

// ──── helpers ────

// NewIRProgram 构造空 program
func NewIRProgram() *IRProgram {
	return &IRProgram{
		Packages: map[string]*IRPackage{},
	}
}

// NewIRPackage 构造空包
func NewIRPackage(name string) *IRPackage {
	return &IRPackage{
		Name:   name,
		Nodes:  map[string]*IRNode{},
		Edges:  map[EdgeKey]*IREdge{},
		IsMain: name == "main",
	}
}

// AddPackage 加一个包到 program
func (p *IRProgram) AddPackage(pkg *IRPackage) {
	p.Packages[pkg.Name] = pkg
}

// SetTopology 设置该包的拓扑（同时如果是 main 包，也写到 program.Topology 快捷访问）
func (p *IRProgram) SetTopology(pkgName string, topo *IRTopology) {
	pkg, ok := p.Packages[pkgName]
	if !ok {
		return
	}
	pkg.Topology = topo
	if pkg.IsMain {
		p.Topology = topo
	}
}

// GetTopology 取包的拓扑
func (p *IRProgram) GetTopology(pkgName string) *IRTopology {
	if pkgName == "main" && p.Topology != nil {
		return p.Topology
	}
	pkg, ok := p.Packages[pkgName]
	if !ok {
		return nil
	}
	return pkg.Topology
}

// AllTopologies 返回所有包的拓扑（用于遍历分析）
func (p *IRProgram) AllTopologies() map[string]*IRTopology {
	out := map[string]*IRTopology{}
	for name, pkg := range p.Packages {
		if pkg.Topology != nil {
			out[name] = pkg.Topology
		}
	}
	return out
}

// FindNode 跨包查节点
func (p *IRProgram) FindNode(pkgName, nodeName string) *IRNode {
	pkg, ok := p.Packages[pkgName]
	if !ok {
		return nil
	}
	return pkg.Nodes[nodeName]
}

// ──── 拓扑分析（pruning / auto-exec 计算）────

// AnalyzeTopology 分析所有包的 topology，填 UsedBlocks 和 AutoExec
//
// 每个包都有自己的 topology（不只 main 包），所以要遍历所有包。
// 规则（按用户拍板）：
//   - 每个 edge 引用 src 的某个 out + dst 的某个 in
//   - 这些 (out/in) 标记为"被引用"
//   - 节点按入度切 block：哪个 in 被引用，那个 block 就被用到
//   - 如果一个节点的所有 in 都没被引用，但 out 被引用 → 节点本身要被引用（用于计算）
//   - auto-exec：节点的某个 block 没有 in（默认触发）
func (p *IRProgram) AnalyzeTopology() {
	// 1. 收集每个节点的"被引用 in/out"（遍历所有包的 topology）
	referencedIns := map[string]map[string]bool{}  // nodeName → set of ins
	referencedOuts := map[string]map[string]bool{} // nodeName → set of outs

	for _, topo := range p.AllTopologies() {
		for _, ek := range topo.Edges {
			edge := findEdgeInProgram(p, ek)
			if edge == nil {
				continue
			}
			if referencedOuts[edge.Src] == nil {
				referencedOuts[edge.Src] = map[string]bool{}
			}
			for _, op := range edge.Flow {
				if op.Op == FlowOpSend || op.Op == FlowOpBranchSend || op.Op == FlowOpCall {
					referencedOuts[edge.Src][op.SrcAttr] = true
					if referencedIns[edge.Dst] == nil {
						referencedIns[edge.Dst] = map[string]bool{}
					}
					referencedIns[edge.Dst][op.DstAttr] = true
				}
			}
		}
	}

	// 2. 给每个节点算 UsedBlocks
	for _, pkg := range p.Packages {
		for _, node := range pkg.Nodes {
			ins := referencedIns[node.Name]
			if ins == nil {
				ins = map[string]bool{}
			}
			for i, blk := range node.Blocks {
				used := false
				for _, in := range blk.Inputs {
					if ins[in] {
						used = true
						break
					}
				}
				if used || blk.IsAutoExec {
					node.UsedBlocks = append(node.UsedBlocks, i)
				}
			}
			node.AutoExec = false
			for _, idx := range node.UsedBlocks {
				if node.Blocks[idx].IsAutoExec {
					node.AutoExec = true
					break
				}
			}
		}
	}

	// 3. 填 AutoExecNodes（每个包独立算）
	for pkgName, topo := range p.AllTopologies() {
		autoExec := map[string]bool{}
		for _, ek := range topo.Edges {
			autoExec[ek.Src] = false
		}
		for _, name := range topo.AllNodes {
			if !autoExec[name] {
				autoExec[name] = true
			}
		}
		for n, isAuto := range autoExec {
			if isAuto {
				topo.AutoExecNodes = append(topo.AutoExecNodes, n)
			}
		}
		_ = pkgName
	}
}

// findEdgeInProgram 跨包查 edge
func findEdgeInProgram(p *IRProgram, key EdgeKey) *IREdge {
	for _, pkg := range p.Packages {
		if e, ok := pkg.Edges[key]; ok {
			return e
		}
	}
	return nil
}
