# Mocker 编译器 — 执行文档

> 版本：M4.4（去掉 TopologyDecl，main 是特殊节点）
> 日期：2026-06-12
> 主题：简化的语法结构 + 从 .ce 到 Go 的完整流程

---

## 目录

1. [架构变化](#架构变化)
2. [数据流：从 .ce 到 Go](#数据流从-ce-到-go)
3. [核心数据结构](#核心数据结构)
4. [边分类算法](#边分类算法)
5. [直链 vs 散连：emit 策略](#直链-vs-散连emit-策略)
6. [关键代码位置](#关键代码位置)
7. [实例：hello world 的完整流程](#实例hello-world-的完整流程)

---

## 架构变化

### 旧架构（已废弃）
- 节点（`@Node { ... }`）+ 边（`<edge> { ... }`）+ **拓扑块**（`pkgName { ... }`）
- 拓扑块单独声明包内有哪些边连接，与 EdgeDecl body 分离
- main 也用 `main { ... }` 拓扑块作为入口

### 新架构（M4.4）
- **删除** TopologyDecl
- 只有两类符号：**节点 + 边**
- `main` 是**特殊节点**，body 里包含：
  - `typeName varName` — 实例声明
  - `src <edge> dst` — 实例间连边

### 为什么去掉拓扑块

```
原始：拓扑分析要依附于 TopologyDecl 节点
新：拓扑结构从节点的连接关系自动推断

旧：edge + topology block 二元结构（结构层 + 行为层）
新：edge 单层结构（行为就是结构）
```

---

## 数据流：从 .ce 到 Go

### 1. Parser（AST 构建）
- 输入：`.ce` 源文件
- 输出：`ast.File`（`ast.NodeDecl`, `ast.EdgeDecl`, `ast.StructDecl` 含 main 节点）

### 2. Semantic（语义分析）
- 输入：`ast.File`
- 输出：`semantic.WorkspaceResult`
- **关键**：对 main 节点跑 `CheckMainBody`（替换旧的 `CheckTopology`）
- 校验项：
  - InstanceDecl 的 type 存在（本地 + 跨包）
  - InstanceDecl 的 name 不重复
  - EdgeConnDecl 的 src/dst 已声明
  - EdgeConnDecl 的 edge 能在符号表里找到

### 3. IR Lower
- 输入：semantic.WorkspaceResult
- 输出：`ir.IRProgram`
- **关键**：`lowerMainTopology` 从 main 节点的 InstanceDecl + EdgeConnDecl 收集 IRTopology

### 4. BuildGraph（图分析）
- 输入：IRProgram
- 输出：IRGraph（节点 + 边 + StartNodes）
- 边的 Kind：Direct / Fanout / Fanin
- 用 `FindChains` 找出直链

### 5. Codegen
- 输入：IRProgram + IRGraph
- 输出：Go 源码
- 策略：
  - `isDirectChainTopology()` 用图分析决定模式
  - 直链：`emitSimpleNode()` + `emitDirectChainMain()`
  - 散连：`emitNode()` + `emitEdgeWirings()`

### 6. Go Build + Run
- `go build ./...`
- 直链模式：直接调用，无 goroutine
- 散连模式：goroutine + `time.Sleep(100ms)`

---

## 核心数据结构

### AST 简化（`internal/parser/ast/ast.go`）

```go
// 删除：TopologyDecl, TopologyVar
// 新增：InstanceDecl, EdgeConnDecl

// InstanceDecl 实例声明（main 节点专用）
type InstanceDecl struct {
    PosBase
    Type string // 节点类型（"hello" / "stdio.Println"）
    Name string // 实例名（"happy" / "p"）
}

// EdgeConnDecl 边连接（main 节点专用）
type EdgeConnDecl struct {
    PosBase
    Src  string // 源实例名
    Edge string // 边名
    Dst  string // 目标实例名
}
```

### main 节点 body 规则

```ce
main {
    hello happy              // InstanceDecl: Type="hello" Name="happy"
    stdio.Println p         // InstanceDecl: Type="stdio.Println" Name="p"
    happy <out> p           // EdgeConnDecl: Src="happy" Edge="out" Dst="p"
}
```

### 符号表 + 入口点

```go
type EntryPoint struct {
    File         *ast.File
    MainNode     *ast.StructDecl  // main 节点
    VarInstances map[string]string  // instance → type
    Edges        []EdgeConnDeclInfo
    AutoExec     []string  // indegree=0 的实例（启动时自动执行）
    // ...
}
```

---

## 边分类算法

### Direct vs Fanout vs Fanin

| 类型 | 触发条件 | emit 策略 |
|------|---------|----------|
| **Direct** | 1 src → 1 dst（无 fanout） | `dst.Run(src.Run())` 一行调用 |
| **Fanout** | 1 src → N dsts（branch ops） | goroutine + N channel |
| **Fanin** | N srcs → 1 dst | goroutine + merge |

### IRGraph 核心

```go
type IRGraph struct {
    Nodes      map[string]*GraphNode
    Edges      []*GraphEdge
    StartNodes []string
}

type GraphEdgeKind int

const (
    GraphEdgeDirect GraphEdgeKind = iota
    GraphEdgeFanout
    GraphEdgeFanin
)
```

### FindChains 算法

```
对每个 StartNode：
  chain = [start]
  current = start
  while true:
    next, isFanout = nextInChain(current)
    if isFanout: break  # 散连
    if next == "": break  # terminal
    chain.append(next)
    current = next
  chains.append(chain)
```

---

## 直链 vs 散连：emit 策略

### 直链 emit（`emitDirectChainMain`）

```go
func main() {
    happy := Newhello()
    p := NewPrintln()
    p.Run(happy.Run())  // 直接调用，无 channel
}
```

### 散连 emit（`emitEdgeWirings`）

```go
func main() {
    hello := Newhello()
    say := Newsay()
    
    go say.runBlock0()    // 3 个 USED block 都开 goroutine
    go say.runBlock1()
    go say.runBlock2()
    go hello.runAuto()    // auto-exec 也开 goroutine
    
    go func() {
        for {
            select {
            case v := <-hello.h_out:
                say.hey_in <- v  // 分支派发
            }
        }
    }()
    
    time.Sleep(100 * time.Millisecond)
}
```

---

## 关键代码位置

| 文件 | 内容 |
|------|------|
| `internal/parser/ast/ast.go` | AST 定义：删除 TopologyDecl，加 InstanceDecl/EdgeConnDecl |
| `internal/parser/parse_file.go` | parseFile：删 TopologyDecl 调度，所有 IDENT 走 parseTopDecl |
| `internal/parser/parse_decl.go` | parseTopDecl：`main` 当 StructDecl；parseStructMember 加 form 4/5 |
| `internal/semantic/topology.go` | CheckMainBody（替换 CheckTopology）；nodeExists 支持跨包全名 |
| `internal/semantic/entry.go` | FindEntryPoint 找 main 节点；AnnotateEntryPoint 填 sync/async |
| `internal/semantic/checker.go` | 删 TopologyDecl 循环；main 节点跳过 NodeBody 检查 |
| `internal/ir/lower.go` | lowerMainTopology：从 main 节点 body 收集 InstanceDecl + EdgeConnDecl |
| `internal/ir/ir.go` | AnalyzeTopology：edge.Dst strip pkg 后查 nodes map |
| `internal/ir/graph.go` | IRGraph + BuildGraph + FindChains |
| `internal/codegen/emit.go` | emitNodeStructs（用 graph）/ emitDirectChainMain / emitEdgeWirings |
| `cmd/circle/main.go` | circle run / circle -debug run 入口 |
| `example/main.ce` | hello world 示例 |
| `example/io/io.ce` | io 包（去掉 topology block） |
| `example/stdio/stdio.ce` | stdio 包（去掉 topology block） |

---

## 实例：hello world 的完整流程

### 输入：`main.ce`

```ce
package main

import stdio

hello {
    h := "hello world!"
    h >>
}

<out> {
    hello.h >> stdio.Println.msg
}

main {
    hello happy
    stdio.Println p
    happy <out> p
}
```

### Step 1: Parser → AST

- `hello { ... }` → `StructDecl{Kind: Node, Name: "hello"}`
- `<out> { ... }` → `EdgeDecl{Src: "hello", Dst: "stdio.Println", Edge: "out"}`（InferEdgeEndpoints 推导）
- `main { ... }` → `StructDecl{Kind: Node, Name: "main"}` 含 InstanceDecl × 2 + EdgeConnDecl × 1

### Step 2: Semantic → SymbolTable

- main 符号表：`hello`, `say`
- stdio 符号表：`Println`, `to_string`
- io 符号表：`write`
- edges（含 Style 2 推导后）：
  - `hello <out> stdio.Println`（从 `<out> { ... }` Style 2 推导）
  - `Println <write> io.write`
  - `write <syscall> SYSCALL`

### Step 3: CheckMainBody

```
varMap = { "happy": "hello", "p": "stdio.Println" }
edges:
  { happy, out, p } → 解析为 { hello, out, stdio.Println } ✓
```

### Step 4: IR Lower

```
main.IRTopology {
    Edges: [{hello, out, Println}]  // strip pkg
    VarInstances: {happy: hello, p: stdio.Println}
    AllNodes: [hello, Println]
}
```

### Step 5: BuildGraph

```
Nodes: hello (1 block, USED, auto-exec), Println (1 block, USED), write (1 block, USED)
Edges:
  hello --Direct--> Println (topo)
  Println --Direct--> write (topo)
  write --Direct--> SYSCALL (topo)
StartNodes: [hello]
Chains: [[hello, Println, write]]
```

### Step 6: isDirectChainTopology

```
chains = [[hello, Println, write]]
len(chains) == 1 && 所有节点 1 USED block → 直链模式
```

### Step 7: emitDirectChainMain

```go
func main() {
    happy := Newhello()
    p := NewPrintln()
    p.Run(happy.Run())
}
```

### Step 8: Go Build + Run

```
$ ./main
hello world!
```

---

## 修改总览（vs 旧版）

| 文件 | 改动 |
|------|------|
| `parser/ast/ast.go` | 删 `TopologyDecl`、`TopologyVar`；加 `InstanceDecl`、`EdgeConnDecl` |
| `parser/parse_file.go` | 删 parseTopologyDecl 调度，简化 dispatch 逻辑 |
| `parser/parse_decl.go` | `main` 走 `parseStructBody`；parseStructMember 加 form 4/5（InstanceDecl/EdgeConnDecl） |
| `parser/dump.go` | 删 TopologyDecl case；加 InstanceDecl/EdgeConnDecl case |
| `semantic/topology.go` | 重写：`CheckTopology` → `CheckMainBody`；nodeExists 支持跨包全名 |
| `semantic/entry.go` | 重写：FindEntryPoint 找 main StructDecl 而不是 TopologyDecl |
| `semantic/checker.go` | 删 TopologyDecl 循环；main 节点跳过 NodeBody 检查 |
| `semantic/symbol.go` | 删 TopologyDecl case |
| `ir/lower.go` | `lowerTopology` → `lowerMainTopology` |
| `ir/ir.go` | AnalyzeTopology：edge.Dst strip pkg 后查 nodes |
| `example/main.ce` | 保留 main { ... } 语法 |
| `example/stdio/stdio.ce` | 删 `stdio { Println <write> io.write }` 拓扑块 |
| `example/io/io.ce` | 删 `io { write <syscall> SYSCALL { ... } }` 拓扑块，保留为顶层 EdgeDecl |

---

## 关键设计原则

> **拓扑 = 代码 builder，不是胶水**
> - 简化：节点 + 边，无拓扑块
> - 直链用直接调用（`p.Run(happy.Run())`）
> - 散连用 channel + goroutine
> - dead-code 消除（say/to_string 不 emit）
> - 类型推断（从 init stmt 字面量推 string）
> - **main 是个特殊节点**，body 包含拓扑结构（实例 + 边）

---

## 参考

- `internal/parser/ast/ast.go` — AST 定义
- `internal/parser/parse_decl.go` — 节点/边/main 节点解析
- `internal/semantic/topology.go` — CheckMainBody
- `internal/semantic/entry.go` — 入口点分析
- `internal/ir/lower.go` — lowerMainTopology
- `internal/ir/graph.go` — IRGraph + BuildGraph + FindChains
- `internal/codegen/emit.go` — codegen + emit 策略
- `example/main.ce` — hello world 示例
- `example/io/io.ce` — io 包（无 topology）
- `example/stdio/stdio.ce` — stdio 包（无 topology）