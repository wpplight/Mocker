# Mocker 编译器 — 执行文档

> 版本：M4.5（构造函数编排器模式 + 节点 body 内 sub-graph）
> 日期：2026-06-12
> 状态：实施中

---

## 目录

1. [架构概览](#架构概览)
2. [核心设计：构造函数编排器](#核心设计构造函数编排器)
3. [数据流：从 .ce 到 Go](#数据流从-ce-到-go)
4. [新语法概要](#新语法概要)
5. [节点 body 内的 sub-graph](#节点-body-内的-sub-graph)
6. [直链 vs 散连：emit 策略](#直链-vs-散连emit-策略)
7. [关键代码位置](#关键代码位置)
8. [实例：hello world 的完整流程](#实例hello-world-的完整流程)
9. [架构演进历史](#架构演进历史)
10. [关键参考](#关键参考)

---

## 架构概览

Mocker 编译器把 `.ce` 源文件编译成可执行的 Go 代码。

**核心设计模式**：**构造函数编排器（Constructor Orchestrator）**
- 每个节点的 `NewXxx()` 是它自己子图的"编译器"
- 节点 body 描述 sub-graph（sub-instance + sub-edge + port + flow）
- 构造时递归创建子实例 + 调子实例方法
- main() 只调 entry 节点的 NewXxx()
- 整个调用图是一棵树

**流水线**：
```
.ce 源文件
   ↓ parser
AST (StructDecl 含 sub-graph + port + flow)
   ↓ semantic
SymbolTable + 类型检查
   ↓ IR (lower)
IRProgram (IRNode 含 SubInstances + SubEdges)
   ↓ BuildGraph
IRGraph (图分析)
   ↓ codegen
Go source (构造器编排器模式)
   ↓ go build + go run
可执行程序
```

---

## 核心设计：构造函数编排器

### 设计原则

```
┌─────────────────────────────────────────────────────┐
│  节点 A 的 NewXxx()                                │
│  ┌───────────────────────────────────────────┐   │
│  │ 1. init self state                       │   │
│  │ 2. 创建 sub-instances:                  │   │
│  │      b := New<typeB>()                  │   │
│  │      c := New<typeC>()                  │   │
│  │ 3. 调 sub-instance 方法:                │   │
│  │      b.block0(self.<output>)            │   │
│  │      c.block0(b.<output>)               │   │
│  │ 4. return self                          │   │
│  └───────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

### 例子（hello world）

```go
type hello struct {
    h string
}

func Newhello() *hello {
    hello_instance := &hello{
        h: "hello",
    }
    // sub-instance: world w
    w := Newworld()
    // sub-edge: h <add_str> w
    out_str := w.block0(hello_instance.h)
    // sub-instance: stdio.Println p
    p := NewPrintln()
    // flow: out_str >> p.msg
    p.block0(out_str)
    return hello_instance
}

func main() {
    _ = Newhello()  // main 只调 entry
}
```

### 优势

1. **可读性**：emit 出来的 Go 代码就是构造树
2. **无并发**：纯函数调用链（除非 fanout）
3. **递归性**：节点 body 可以嵌套 sub-graph
4. **一致性**：所有 NewXxx() 遵循同一模式

---

## 数据流：从 .ce 到 Go

### 1. Parser（AST 构建）
- 输入：`.ce` 源文件
- 输出：`ast.File`（`ast.StructDecl` 含 4 种新 StructMember）
- **新**：识别 sub-instance (`typeName varName;`)、sub-edge (`src <edge> dst`)、flow (`src >> dst.attr;`)

### 2. Semantic（语义分析）
- 输入：ast.File
- 输出：semantic.WorkspaceResult
- **新**：`CheckNodeBody` 校验所有节点的 sub-instance + sub-edge
- main 节点也走 `CheckNodeBody`，但只识别 `InstanceDecl` 作为 entry

### 3. IR Lower
- 输入：semantic.WorkspaceResult
- 输出：ir.IRProgram
- **新**：`lowerSubGraph` 处理每个节点的 SubInstances + SubEdges
- IRNode 加 `SubInstances` + `SubEdges` 字段

### 4. BuildGraph（图分析）
- 输入：IRProgram
- 输出：IRGraph
- 用于决定 emit 模式（直链 vs 散连）
- main 节点不进入 graph（不是 node）

### 5. Codegen
- 输入：IRProgram + IRGraph
- 输出：Go 源码
- 策略：
  - 对每个 IRNode emit：
    - struct（state 字段）
    - NewXxx() 构造器（init + orchestration）
    - blockN() 方法（sync block 入口）
  - main() = `_ = New<entry>()`

### 6. Go Build + Run
- `go build ./...`
- 编译产物是构造树，运行时直接调 New<entry>()

---

## 新语法概要

### 节点 body 内的 4 种形式

```ce
@Node {
    // 1. 端口声明
    >> portName                 // 无类型
    >> str portName             // 带类型

    // 2. 状态变量
    name := expr                // init
    num name = expr             // 显式类型

    // 3. sub-instance 声明
    TypeName varName;           // 节点 body 内嵌实例

    // 4. sub-edge 连接
    src <edge_name> dst         // sub-instance 间连边

    // 5. 内部 flow
    src >> dst.attr;            // 内部 flow 到 sub-instance 的输入
}
```

### main body（极简）

```ce
main {
    TypeName varName;            // 只声明 entry instance
}
```

### 顶层 edge（仍存在）

```ce
<edge_name> {
    src.attr >> dst.attr
}
```

---

## 节点 body 内的 sub-graph

### 完整例子

```ce
hello {
    h := "hello"

    // sub-instance
    world w
    stdio.Println p

    // sub-edge
    h <add_str> w

    // port decl
    >> out_str

    // 内部 flow
    out_str >> p.msg;
}
```

### 对应的 IR 节点结构

```go
type IRNode struct {
    Name       string
    State      []IRState
    Init       []IRStmt
    Inputs     []IRInput
    Outputs     []IROutput

    // M4.5 新增：sub-graph
    SubInstances []*IRSubInstance  // {TypeName, InstanceName}
    SubEdges     []*IRSubEdge      // {SrcAttr, EdgeName, DstInstance, DstAttr}
}
```

### 对应的 emit

```go
type hello struct {
    h string
}

func Newhello() *hello {
    hello_instance := &hello{h: "hello"}

    // sub-instance: world w
    w := Newworld()
    // sub-edge: h <add_str> w
    out_str := w.block0(hello_instance.h)
    // sub-instance: stdio.Println p
    p := NewPrintln()
    // flow: out_str >> p.msg
    p.block0(out_str)

    return hello_instance
}
```

---

## 直链 vs 散连：emit 策略

### 直链（auto-exec → sync）

适合**无 fanout** 的简单调用链：

```go
func main() {
    _ = Newhello()  // 构造器内部递归调下游
}
```

### 散连（fanout）

适合**1 → N 分支**的场景，需要 goroutine：

```go
func main() {
    _ = Newhello()  // 构造器内可能用 goroutine
}
```

（具体实现取决于 sub-edge 数量）

---

## 关键代码位置

| 文件 | 内容 |
|------|------|
| `internal/parser/ast/ast.go` | AST：加 `SubInstanceDecl`、`SubEdgeDecl` |
| `internal/parser/parse_decl.go` | `parseStructMember` 加新形式识别 |
| `internal/parser/dump.go` | 加新 case |
| `internal/semantic/topology.go` | `CheckNodeBody` 校验 sub-instance + sub-edge |
| `internal/semantic/entry.go` | `FindEntryPoint` 找 entry |
| `internal/semantic/checker.go` | 调 CheckNodeBody |
| `internal/ir/ir.go` | IRNode 加 `SubInstances` + `SubEdges` |
| `internal/ir/lower.go` | `lowerSubGraph` 处理节点 body 内的 sub-graph |
| `internal/ir/graph.go` | 图分析（用于决定 emit 模式）|
| `internal/codegen/emit.go` | 构造函数编排器 emit |
| `cmd/circle/main.go` | `circle run` 入口 |
| `example/main.ce` | hello world 示例（新语法） |
| `example/stdio/stdio.ce` | stdio 包 |
| `example/io/io.ce` | io 包 |
| `docs/constructor-orchestrator-design.md` | 完整设计文档 |

---

## 实例：hello world 的完整流程

### 输入：`main.ce`

```ce
package main

import stdio

hello {
    h := "hello"
    world w;
    h <add_str> w

    >> out_str
    stdio.Println p;
    out_str >> p.msg;
}

world {
    >> str words
    new := words + " world!"
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

main {
    hello happy;
}
```

### Step 1: Parser → AST

- `hello { ... }` → StructDecl{Name: "hello", Members: [...]}
  - `h := "hello"` → VarDecl
  - `world w;` → **SubInstanceDecl{Type: "world", Name: "w"}**
  - `h <add_str> w` → **SubEdgeDecl{Src: "h", Edge: "add_str", Dst: "w"}**
  - `>> out_str` → PortDecl
  - `stdio.Println p;` → SubInstanceDecl
  - `out_str >> p.msg;` → **FlowDecl**（内部 flow）

### Step 2: Semantic → SymbolTable

校验 sub-instance types 存在、sub-edge 在符号表能找到。

### Step 3: IR Lower

`IRNode.SubInstances = [{TypeName: "world", InstanceName: "w"}, {TypeName: "stdio.Println", InstanceName: "p"}]`

`IRNode.SubEdges = [{SrcAttr: "h", Edge: "add_str", DstInstance: "w"}, {SrcAttr: "out_str", DstInstance: "p", DstAttr: "msg"}]`

### Step 4: Codegen

emit 出 Go 代码（构造器编排器风格）。

### Step 5: Go Build + Run

```
$ ./main
hello world!
```

---

## 架构演进历史

| 版本 | 日期 | 主要变化 |
|------|------|---------|
| M0 | — | Lexer 完成 |
| M2 | — | Parser + AST 完成 |
| M3 | — | Semantic + 多包 workspace |
| M4.0 | — | IR Lower + 直链 codegen |
| M4.1 | — | 图分析（IRGraph）|
| M4.2 | — | dead-code 消除 |
| M4.3 | — | 构造函数编排器 emit（run() 直接调） |
| M4.4 | — | 删 TopologyDecl，main 节点化 |
| **M4.5** | **2026-06-12** | **节点 body 内的 sub-graph（当前版本）** |

### M4.4 → M4.5 的关键变化

| 维度 | M4.4 | M4.5 |
|------|------|------|
| 拓扑位置 | 集中在 main 节点 body | 分散在每个节点 body |
| 节点 body | 单 block 序列 | sub-graph（sub-instance + sub-edge + port + flow） |
| main body | 完整拓扑（多 edge） | 极简（只声明 entry） |
| 入口检测 | main body 拓扑 | main body 唯一 entry 节点 |

---

## 关键参考

- [constructor-orchestrator-design.md](./constructor-orchestrator-design.md) — 完整设计文档
- [ast_design.md](./ast_design.md) — AST 设计
- [../../docs/ir-design.md](../../docs/ir-design.md) — IR 设计
- [../../docs/parser.md](../../docs/parser.md) — Parser 实现
- [../../docs/language.md](../../docs/language.md) — 语言规范
- [roadmap.md](./roadmap.md) — Roadmap
- `example/debug/000-example.go` — 目标 emit 风格（user 提供）