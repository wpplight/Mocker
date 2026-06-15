# IR 设计文档（M4）

> Mocker 中间表示（Intermediate Representation）的完整设计。
> 位于 AST（语法层）和 codegen（目标代码层）之间。

---

## 〇、保留字（SYSCALL / EXIT / ALLOC）

保留字是**编译器内置的原始节点**，**不在 IR 里建模**：

- IR 不为 SYSCALL 创建 IREdge 或 IRNode（避免污染 IR 类型）
- codegen 在 emit 阶段识别：edge 的 dst 是 SYSCALL 时，插入对应的 syscall 调用
- 语义层有 `IsReservedNode(name)` helper 识别这些关键字

**IR 中表示 SYSCALL 边的方式**：
- `IREdge{Src: "write", Edge: "syscall", Dst: "SYSCALL"}`（普通的 edge AST）
- IR Lower 时检查 Dst 是否是保留字，打标记 `IREdge.HasReservedTarget = true`
- codegen emit 时看到标记 → emit syscall 而非普通 channel wiring

## 〇.一、Style 2 语法糖在 IR 里的体现

`<edge_name> { body }` 在 parser 层处理，**不影响 IR 结构**：

- parser 解析出 `EdgeDecl{Src: "", Edge: "write", Dst: "", Body: [...]}`
- semantic 跑 `InferEdgeEndpoints(edge)` 填上 src/dst
- IR Lower 时 edge 已经有正确的 src/dst，跟 Style 1 没区别

**IR 完全无感**：所有 edge 在 IR 阶段都是 Style 1 形式。

---

## 〇.二、IR Lower（M4.1 已完成）

`Lower(sem *WorkspaceResult) *IRProgram` 把语义层降级为 IR。

### 流程
1. 遍历所有包，每包建 `IRPackage`
2. 每包里每个 `StructDecl`（@name / 普通 node）→ `IRNode`
3. 每包里每个 `EdgeDecl` → `IREdge`（含 kind 从 semantic.EdgeKinds 取）
4. 每包里名为 `main` 的 `StructDecl` 节点 body → `IRTopology`（从 `InstanceDecl` + `EdgeConnDecl` 收集 VarInstances / InstanceEdges / StartNodes）
5. 调 `prog.AnalyzeTopology()` 填 `UsedBlocks` + `AutoExec`

### 节点降级（`lowerStruct`）
- `PortDecl` → `IRInput`
- `FieldDecl` → `IRField`（节点级状态）
- `VarDecl` → `IRSimpleStmt`（追加到 `Init`）
- `FlowDecl` → `IROutput`

### 边降级（`lowerEdge`）
- 边 kind 从 `semantic.EdgeKinds[EdgeKey]` 取
- body 用 `resolveFlowOps` 拍平：
  - `FlowStmt`（a >> b >> c）：每对相邻 step → `IRFlowOp{Op: FlowOpSend}`
  - `FlowFanout`（1 src → N branches）：每分支最后一步 → `IRFlowOp{Op: FlowOpBranchSend}`
  - `FlowCont`：同 FlowStmt
- 每个 `IRFlowOp` 有 `Src/SrcAttr/Dst/DstAttr`（target 拆 node 名 + attr 名）

### 拓扑降级（`lowerMainTopology`）
- 从名为 `main` 的 `StructDecl` 节点 body 收集：
  - `InstanceDecl`（`hello happy;`）→ `IRTopology.VarInstances[name] = type`
  - `EdgeConnDecl`（`happy <out> p`）→ `IRTopology.InstanceEdges[]`
- 去重 + 收集 `AllNodes`（去重）
- 后续 `AnalyzeTopology` 计算 `StartNodes`（含 auto-exec block 的节点）

### 验证
`circle ir` 子命令 dump 实际 IR（5 个包全部拍平，nodes/edges/topology 完整）。

### 对应实现
- [`internal/ir/lower.go`](file:///home/wpp/homework/Mocker/circle/internal/ir/lower.go)
- [`internal/ir/dump.go`](file:///home/wpp/homework/Mocker/circle/internal/ir/dump.go)

---

## 一、IR 是什么

Mocker AST 是嵌套的（File → Decls → Stmts → Exprs），codegen 要深遍历 7-8 层才能拿全一个 node 的信息（名字 + ins + outs + body + 类型）。

**IR = 把 AST 拍平**：每条信息 codegen 一次拿全。

```
AST 形式（嵌套）
File
 └── StructDecl "@Println"
      ├── PortDecl ">> str msg"
      ├── VarDecl "nl := ..."
      └── FlowDecl "msg >> stdio.Println"

IR 形式（扁平）
IRNode {
 Name:     "Println",
 Inputs:   [{Name:"msg", Type:TypeStr}],
 Outputs:  [{Name:"msg"}, ...],
 Init:     [...],
 Blocks:   [...],
 AutoExec: false,
}
```

---

## 二、核心数据模型

### 2.1 顶层

```go
type IRProgram struct {
    PkgName  string                  // 通常 "main"
    Packages map[string]*IRPackage   // pkg_name → 包
    Topology *IRTopology             // 入口拓扑（= main 包的 Topology 快捷访问）
}

type IRPackage struct {
    Name     string
    Nodes    map[string]*IRNode
    Edges    map[EdgeKey]*IREdge
    Topology *IRTopology           // 只有 main 包有（从入口节点 body 推导）
    IsMain   bool                  // 是否是入口包
}

type EdgeKey struct {
    Src, Name, Dst string
}
```

**只有 main 包有 Topology**（从 `main` 节点 body 推导）：
- **main 包的 Topology** = 入口节点 `main` body 里的 `InstanceDecl` + `EdgeConnDecl`
- **其他包没有 Topology**（包内节点之间的连线由各节点 body 内的 sub-instance + sub-edge + flow 表达）

例：main 节点 body 收集到的 entry：

```ce
main {
    hello happy;                // InstanceDecl → VarInstances["happy"] = "hello"
    happy <out> p               // EdgeConnDecl → InstanceEdges[]
}
```

构造函数编排器模式：每个节点的 `NewXxx()` 递归构造 sub-instance + 调用 sub-edge 方法（详见 [constructor-orchestrator-design.md](../circle/docs/constructor-orchestrator-design.md)）。

### 2.2 节点

```go
type IRNode struct {
    Name     string
    Kind     NodeKind     // node / edge / struct
    Exported bool         // @ 前缀

    // 接口面
    Inputs  []IRInput     // 入度（>> type name）
    Outputs []IROutput    // 出度（name >>）

    // 内部结构
    Init   []IRStmt      // 入度到达前的初始化
    Blocks []IRBlock     // 按入度切分的 block 们

    // 持久状态（可选）
    State []IRField

    // M4.5 新增：节点 body 内的 sub-graph（构造函数编排器）
    //
    // 节点 body 可以声明 sub-instance + sub-edge，构造时递归构造
    //   SubInstances: 节点 body 内嵌的子实例（world w, stdio.Println p）
    //   SubEdges:     sub-instance 之间的边（h <add_str> w, out_str >> p.msg）
    //   SubFlows:     内部 flow（srcAttr → DstInstance.DstAttr）
    SubInstances []*IRSubInstance  // {TypeName, InstanceName}
    SubEdges     []*IRSubEdge      // {SrcAttr, EdgeName, DstInstance, DstAttr}
    SubFlows      []*IRSubFlow      // {SrcAttr, DstInstance, DstAttr}

    // codegen 用（拓扑分析后填入）
    AutoExec     bool     // 至少一个 block 是 auto-exec
    UsedBlocks   []int    // 拓扑用到的 block 索引
    ReferencedBy []string // 被哪些边引用
}

// IRSubInstance（M4.5 新增）— 节点 body 内的子实例
type IRSubInstance struct {
    TypeName     string // 节点类型名（"world" / "stdio.Println"）
    InstanceName string // 实例名（"w" / "p"）
}

// IRSubEdge（M4.5 新增）— sub-instance 之间的 sub-edge 连接
type IRSubEdge struct {
    SrcAttr      string // 源属性名（"h" / "out_str"）
    EdgeName     string // 边名（"add_str" 等）
    DstInstance  string // 目标实例名
    DstAttr      string // 目标属性名（"" 表示"整个 output"）
}

// IRSubFlow（M4.5 新增）— 节点 body 内的内部 flow
type IRSubFlow struct {
    SrcAttr     string // 源属性名
    DstInstance string // 目标 sub-instance 名
    DstAttr     string // 目标 input 端口名
}

type IRInput struct {
    Name string
    Type IRType
    Pos  ast.Pos
}

type IROutput struct {
    Name string
    Type IRType
    Pos  ast.Pos
}
```

**关键设计**：
- `Inputs` / `Outputs` 是暴露的接口
- `Init` 是入度到达前要执行的初始化（用户可在节点顶部声明）
- `Blocks` 按入度切分（每个 block 有 a 个入度 + b 个出度）
- `State` 是节点级持久字段（多个 goroutine 之间共享）

### 2.3 Block

```go
type IRBlock struct {
    Inputs     []string   // 触发本 block 的入度名
    Outputs    []string   // 本 block 出口的出度名
    Stmts      []IRStmt   // block 体（用户写的顺序）
    IsAutoExec bool       // 无入度（启动即跑）
    Pos        ast.Pos
}
```

**block 模型**（用户拍板）：
- 每个节点 = 1 个内含 n 个 block 的图
- 每个 block = a 个入度 + b 个出度 + body
- a=0 → auto-exec（创建即跑）
- blocks 在源码里不必连续；M4.5 之后**所有 block 都 emit**（构造函数编排器按需调 method）

### 2.4 边

```go
type IREdge struct {
    Src, Name, Dst string

    Kind     EdgeKind     // sync / async
    Branches int          // async 时的分支数（fanout）
    HasAck   bool         // 是否启用 ack channel（async backpressure）

    Flow []IRFlowOp       // 已展开的 flow 操作
    Pos  ast.Pos
}

type EdgeKind int

const (
    EdgeSync  EdgeKind = iota  // 同步，函数调用
    EdgeAsync                  // 异步，goroutine + channel
)
```

**goroutine 决策权在边**（用户拍板）：
- body 含 `FlowFanout` → async → 每分支一个 goroutine
- body 只有 `FlowStmt` → sync → 函数调用

### 2.5 FlowOp（边 body 已展开）

```go
type IRFlowOp struct {
    Op      FlowOpKind  // send / call / branch_send
    Src     string      // 源节点名
    SrcAttr string      // 源属性（in/out 名）
    Dst     string      // 目标节点名
    DstAttr string      // 目标属性（in 名）
    IsAck   bool        // 是否 ack channel 的 send
    Branch  int         // fan-out 分支号
}

type FlowOpKind int

const (
    FlowOpSend FlowOpKind = iota  // 异步：channel send
    FlowOpCall                    // 同步：函数调用
    FlowOpBranchSend              // 异步 fan-out：分支 channel send
)
```

### 2.6 入口拓扑（仅 main 包）

```go
type IRTopology struct {
    Edges         []EdgeKey      // 顶层边列表（跨包用，本语义下基本不用）
    StartNodes    []string       // 程序启动时要主动跑的节点（main 包专属，含 auto-exec 的节点）
    AllNodes      []string       // main body 里出现过的所有 instance
    VarInstances  map[string]string  // instance name → type name（`hello happy;` → "happy": "hello"）
    InstanceEdges []InstanceEdge // main body 里的 EdgeConnDecl（`happy <out> p` → InstanceEdge{Src:"happy", Edge:"out", Dst:"p"}）
    Pos           ast.Pos
}

// InstanceEdge main body 里的边（instance-level）
type InstanceEdge struct {
    SrcInstance string  // 源实例名（如 "happy"）
    Edge        string  // 边名（如 "out"）
    DstInstance string  // 目标实例名（如 "p"）
}
```

> M4.5 起 `IRTopology` 只承载 **main 包入口节点的 InstanceDecl + EdgeConnDecl**；包内节点之间的连线由各节点 body 的 sub-graph 表达。

---

## 三、用户拍板的运行时映射

| 概念 | IR 表达 | 运行时 emit 形态 |
| --- | --- | --- |
| 节点 | `IRNode` | `struct N{ ch_in; ch_out; ... }` + `func (n *N) run()` |
| sync edge | `IREdge{Kind:Sync}` | 函数调用：`dst.in_f(src.out)` |
| async edge | `IREdge{Kind:Async}` | goroutine + `dst.in <- src.out` |
| async + ack | `IREdge{Kind:Async, HasAck:true}` | 加 ack channel，goroutine 等收到 ack 再继续 |
| auto-exec block | `IRBlock{IsAutoExec:true}` | 启动时直接 `go n.runBlock(i)` |
| 拓扑块（取消） | ~~`IRTopology`~~ | 已删除（M4.5）。main 包入口节点 body 由 `IRTopology` 收集 InstanceEdges + VarInstances + StartNodes；构造函数编排器按 sub-edge 递归构造 |
| 入度前初始化 | `IRNode.Init` | `func (n *N) init() { ... }`，在节点创建时调一次 |
| 状态字段 | `IRNode.State` | `n.field` 持久化在 struct 里 |

---

## 四、传输机制决策

| 边类型 | 传输 | 理由 |
| --- | --- | --- |
| sync edge | **函数调用** `dst.method(src.value)` | 调用关系清晰，串行，符合"main 串行化" |
| async edge | **channel** `dst.ch_in <- src.out` | 并发天然解耦 |
| async + ack | **双向 channel** `dst.ch_in <- src.out ; <- dst.ch_ack` | backpressure，可选 |

---

## 五、节点 → Go 结构（emit 时长这样）

按用户拍板：
- node 是"对象"，可有入度前的初始化
- 不同 node 复杂度不同

**简单 node**（无 init，无 state）：
```go
func Hello_Run(h_hey_ch chan string) {
    msg := "hello world!"
    h_hey_ch <- msg
}
```

**复杂 node**（有 init，有 state）：
```go
type Say struct {
    hey_ch  chan string
    my_ch   chan string
    world_ch chan string

    state_nl string   // 持久字段
}

func NewSay() *Say {
    return &Say{
        hey_ch: make(chan string),
        my_ch:  make(chan string),
        world_ch: make(chan string),
        state_nl: "\n",
    }
}

func (s *Say) Run() {
    // auto-exec block
    s.initState()

    // 多个 block 用 select 多路监听
    for {
        select {
        case v := <-s.hey_ch:
            go s.handleHey(v)  // async: 开 goroutine
        case v := <-s.my_ch:
            go s.handleMy(v)
        case v := <-s.world_ch:
            go s.handleWorld(v)
        }
    }
}
```

---

## 六、拓扑裁减（Pruning）

**关键优化**（用户拍板）：每个 node 按拓扑块用到哪些 in/out，**只生成用到的 block 的代码**。

> **M4.5 之后**：构造函数编排器模式下，**所有 block 默认 emit**，pruning 不再决定生死。此节保留作为历史参考。

### 6.1 流程

1. **收集引用**：遍历所有 edge.Flow，记录每个 (node, attr) 是否被引用
2. **算 UsedBlocks**：
   - block 被引用 ⟺ 它的某个 `Inputs[]` 在引用集合里
   - auto-exec block 默认被引用（启动即跑）
3. **算 AutoExec**：
   - 节点至少有一个 block 是 IsAutoExec 且被引用 → 节点是 AutoExec
4. **填 AutoExecNodes**：拓扑边集合里没出现过的节点 = 无入度 = auto-exec

### 6.2 例子（M4.5 之前）

```
@Println {           // 3 个 block:
    >> str msg       // block0: auto-exec (入度 msg → 出度 msg)
    fid              // block1: 入度 msg → 出度 stdio.Println(fid)  ← 只有这个被用到
    msg              // block2: ...
    ...
}

stdio {
    Println <write> io.write   // 只引用了 msg → 没用 stdio.Println
}
```

> **注意**：M4.5 起已取消 `stdio { ... }` 拓扑块。连线由各节点 body 内的 sub-instance + sub-edge 表达，构造函数编排器模式不需要按"拓扑引用关系"裁剪 block，**所有 block 默认 emit**（构造函数会按需调 method）。pruning 分析的细节保留在 `AnalyzeTopology` 里做兜底，但不再决定生死。

---

## 七、IRStmt 设计

```go
type IRStmt interface{ irStmtMarker() }

type IRSimpleStmt struct {       // 简单语句
    Kind string                   // "assign" / "vardecl" / "fielddecl"
    Text string                   // 原始代码
    Pos  ast.Pos
}

type IRFlowStmt struct {         // flow 语句（已展开的 >> 链）
    Ops []IRFlowOp
    Pos ast.Pos
}

type IRExprStmt struct {         // 裸表达式
    Text string
    Pos  ast.Pos
}
```

**MVP**：保留 AST 原始文本 + 类型标签。codegen 时按 Kind 选择不同 emit 模板。

---

## 八、IRType 系统

```go
type IRType struct {
    Kind TypeKind
    Name string  // 用户自定义类型时填
}

type TypeKind int

const (
    TypeUnknown TypeKind = iota
    TypeStr
    TypeNum
    TypeBool
    TypeByte
    TypeAny
)
```

**MVP**：基础类型 + 用户自定义（按名字引用，emit 时直接用 Go 类型名）。

---

## 九、关键 API

```go
// 1. 构造
program := ir.NewIRProgram()
pkg := ir.NewIRPackage("stdio")
program.AddPackage(pkg)

// 2. 从 AST + semantic 降级
irNode := ir.LowerNode(astNode, semTable)
pkg.Nodes["Println"] = irNode

// 3. 拓扑分析（填 UsedBlocks / AutoExec）
program.AnalyzeTopology()

// 4. 跑 codegen（M4.2）
goAST := codegen.Lower(program)
codegen.Emit(goAST, "main.go")
```

---

## 十、待办（你说拍板）

- [ ] `IRStmt` 是否要细化成自己的一套（当前是简单 wrapper）？
- [ ] `IRField` 节点级持久状态是否需要（MVP 可暂留空）？
- [ ] `HasAck` 的判断规则（用户怎么在 edge body 里"定义来回双通道"）？
- [ ] codegen 阶段 `text` 字段怎么 emit（直接拼 vs parse 后 emit）？

---

## 十一、文件位置

| 文件 | 作用 |
| --- | --- |
| `circle/internal/ir/ir.go` | IR 核心数据结构 + 拓扑分析 |
| `circle/internal/ir/lower.go` | (M4.1) AST + semantic → IR 降级器 |
| `circle/internal/codegen/lower.go` | (M4.2) IR → go/ast |
| `circle/internal/codegen/emit.go` | (M4.3) go/ast → Go 源码 + go build |

---

## 十二、示例：从 main.ce 到 IR

> 这是 M4.5 之前的示例（用"拓扑块"组织图）。M4.5 起已改为**节点 body 内 sub-graph + 构造函数编排器**模式，main body 只声明 entry instance。详见 §3.0.4 和 [constructor-orchestrator-design.md](../circle/docs/constructor-orchestrator-design.md)。

```
Mocker 源码：
hello {
    h := "hello world!"
    h >>
}
hello <out> say {            // 顶层边
    hello.h >>
    >>say.hey
    >>say.my
    >>say.world
}
say {
    >> str hey
      hey >> stdio.Println   // 糖
    >> str my
        my >> stdio.Println
    >> str world
        world >> stdio.Println
}
main {                       // 入口节点（M4.5 起只声明 entry instance）
    hello happy;
}
```

↓ IR（简化）：

```go
// pkg main
IRNode{
    Name: "hello",
    Outputs: [{Name:"h"}],
    Init: [IRSimpleStmt{Text: `h := "hello world!"`}],
    Blocks: [
        {IsAutoExec: true, Outputs:["h"], Stmts:[
            IRSimpleStmt{Text: `h >>`},
        ]},
    ],
    UsedBlocks: [0],   // 拓扑用到了 hello.h
    AutoExec: true,    // auto-exec block 被引用
}

IRNode{
    Name: "say",
    Inputs: [{Name:"hey", Type:Str}, {Name:"my", Type:Str}, {Name:"world", Type:Str}],
    Blocks: [
        {Inputs:["hey"], Stmts:[IRFlowStmt{Ops:[SendOp{...}]}]},
        {Inputs:["my"], Stmts:[IRFlowStmt{Ops:[SendOp{...}]}]},
        {Inputs:["world"], Stmts:[IRFlowStmt{Ops:[SendOp{...}]}]},
    ],
    UsedBlocks: [0, 1, 2],   // 拓扑 fan-out 用到 3 个
}

IREdge{
    Src: "hello", Name: "out", Dst: "say",
    Kind: EdgeAsync,      // 含 FlowFanout → async
    Branches: 3,            // 3 个分支
    Flow: [
        {Op: FlowOpBranchSend, Src:"hello", SrcAttr:"h", Dst:"say", DstAttr:"hey", Branch: 0},
        {Op: FlowOpBranchSend, Src:"hello", SrcAttr:"h", Dst:"say", DstAttr:"my", Branch: 1},
        {Op: FlowOpBranchSend, Src:"hello", SrcAttr:"h", Dst:"say", DstAttr:"world", Branch: 2},
    ],
}

IRTopology{                          // M4.5 起：从 main 节点 body 推导
    Edges: [{Src:"hello", Name:"out", Dst:"say"}],   // 顶层边
    StartNodes: ["hello"],   // 有 auto-exec 的节点
    AllNodes: ["hello", "say"],
    VarInstances: {"happy": "hello"},   // main { hello happy; }
    InstanceEdges: [],                  // main body 里没显式写 EdgeConnDecl
}
```

> M4.5 起构造函数编排器模式：`main()` 直接调 `Newhello()`，hello 内部递归构造 sub-instance + 调 sub-edge 方法，不再由 codegen 在 main 里手写 wiring。

↓ codegen emit：

```go
type Hello struct{}
type Say struct {
    hey_ch   chan string
    my_ch    chan string
    world_ch chan string
}

func Hello_Run(h_h_ch chan string) {
    h := "hello world!"
    h_h_ch <- h
}

func (s *Say) Run() {
    for {
        select {
        case v := <-s.hey_ch:
            go func(v string) { stdio.Println(v) }(v)
        case v := <-s.my_ch:
            go func(v string) { stdio.Println(v) }(v)
        case v := <-s.world_ch:
            go func(v string) { stdio.Println(v) }(v)
        }
    }
}

func main() {
    h := &Hello{h_h_ch: make(chan string)}
    s := &Say{hey_ch: make(chan string), my_ch: make(chan string), world_ch: make(chan string)}
    
    // 拓扑连线
    go Hello_Run(h.h_h_ch)
    go s.Run()
    
    // fan-out: 同一份 h 流入 3 个 port
    go func() {
        for {
            select {
            case v := <-h.h_h_ch:
                s.hey_ch <- v
                s.my_ch <- v
                s.world_ch <- v
            }
        }
    }()
    
    select {} // 永不退出
}
```

---

## 十三、决策回顾（你确认过的）

| 项 | 决策 |
| --- | --- |
| A. 整体 IR 结构 | 拆 `IRPackage[]` + 顶层 `IRTopology` |
| A. main 识别 | `PkgName == "main"` 的包，**入口节点 `main` body** 收集成 `IRTopology.VarInstances` / `InstanceEdges` |
| A. 拓扑嵌入 | 顶层 `IRProgram.Topology`（main 包专属，从 `main` 节点 body 推导）|
| A. **取消"包级拓扑块"**（M4.5） | 包内节点连线由各节点 body 的 sub-instance + sub-edge 承担；构造函数编排器模式 |
| B. Goroutine 策略 | 边决定；sync = 函数调用，async = goroutine + channel |
| B. Auto-exec | 节点某个 block 无入度且被拓扑引用 → AutoExec |
| B. Node 形态 | struct-like，可有 init 和 state；节点 body 内可声明 sub-instance + sub-edge + flow |
| B. 拓扑裁减 | emit 时按拓扑用到的 block 裁减（M4.5 之后：构造函数按需调 method，pruning 仅做兜底） |
| C. Edge 字段 | src/name/dst/kind/branches/hasAck/flow |
| D. Topology 字段 | InstanceEdges + VarInstances + StartNodes + AllNodes |
| E. Type | 基础类型 + 用户自定义（按名字）|
| F. 传输 | sync = 函数调用，async = channel（可选 ack）|
| G. Node → Go | struct + chan 字段 + run()，sync edge 函数调用，async goroutine spawn；M4.5 起构造函数编排器直接 emit 嵌套构造 |

---

**这份 doc 是 IR 设计的完整蓝图。审核后告诉我哪里要改、改完我就开始 M4.1 写 lower.go（AST + semantic → IR）。**