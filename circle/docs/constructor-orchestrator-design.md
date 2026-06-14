# Mocker 设计：构造函数编排器模式（Constructor Orchestrator）

> 版本：M4.5（核心设计）
> 日期：2026-06-12
> 状态：实施中

---

## 目录

1. [核心思想](#核心思想)
2. [新语法 vs 旧语法](#新语法-vs-旧语法)
3. [节点 body 内的拓扑语法](#节点-body-内的拓扑语法)
4. [三个 .ce 源码完整分析](#三个-ce-源码完整分析)
5. [目标 emit 风格](#目标-emit-风格)
6. [编译器改造清单](#编译器改造清单)
7. [关键设计原则](#关键设计原则)
8. [实施步骤](#实施步骤)

---

## 核心思想

> **每个节点的 `NewXxx()` 是它自己子图的"编译器"**
> **整个调用图是一棵树，每个 NewXxx() 是树的节点，main() 只调 entry**

具体表现为：

1. **节点 body 是自包含的 sub-graph**：
   - sub-instance 声明（`typeName varName;`）
   - sub-edge（`src <edge> dst`）
   - port 声明（`>>portName`）
   - 内部 flow（`src >> dst.attr;`）

2. **构造时递归编排**：
   - `NewXxx()` 包含 init state + 创建子实例 + 调子实例方法

3. **main() 极简**：
   - 只调 `_ = New<entry>()`

4. **无 channel/goroutine**（除非是 fanout）：
   - 纯函数调用链
   - 编译产物 = 一棵递归构造树

---

## 新语法 vs 旧语法

### 旧（M4.4）

```ce
hello {
    h := "hello world!"
    h >>                      // 出度
}

stdio.Println {
    >> str msg                // 入度
}

<out> {                      // 顶层 edge
    hello.h >> stdio.Println.msg
}

stdio {                      // 拓扑块（已废弃）
    Println <write> io.write
}

main {                       // main 节点（包含完整拓扑）
    hello happy
    stdio.Println p
    happy <out> p
}
```

### 新（M4.5）

```ce
hello {                      // hello 节点 body 包含 sub-graph
    h := "hello"
    world w;                 // sub-instance 声明
    h <add_str> w            // sub-edge
    
    >> out_str               // port 声明（hello 的输出端口）
    stdio.Println p;         // 外部 sub-instance 引用
    out_str >> p.msg;        // 内部 flow
}

world {                      // 独立节点
    >> str words
    new := words + " world!"
    new >>
}

<add_str> {                 // 顶层 edge（跨实例）
    hello.h >> world.words
    world.new >> hello.out_str
}

main {                       // main 只声明 entry
    hello happy;
}
```

### 关键差异

| 维度 | 旧 (M4.4) | 新 (M4.5) |
|------|----------|----------|
| 拓扑位置 | 集中在 main 节点 | 分散在每个节点 body 内 |
| 节点 body 内容 | 单 block 序列 | 嵌套 sub-graph（sub-instance + sub-edge + port + flow） |
| 入口 | main body 有完整拓扑 | main body 只声明 entry |
| 递归性 | 单层 main 拓扑 | 递归：节点 body 嵌套 sub-graph |

---

## 节点 body 内的拓扑语法

### 4 种新 StructMember 形式

#### 1. SubInstanceDecl — sub-instance 声明

```ce
typeName varName;
```

- 例：`world w;`（声明一个 `world` 类型的实例 `w`）
- 例：`stdio.Println p;`（声明一个 `stdio.Println` 类型的实例 `p`）
- 用于节点 body 内**声明子实例**
- 与 main body 的 `InstanceDecl` 形式相同，但语义是"在父节点内部"

#### 2. SubEdgeDecl — sub-edge 连接

```ce
src <edge_name> dst
```

- 例：`h <add_str> w`（在 hello 内部，h 节点 → w 节点，via `<add_str>`）
- 用于节点 body 内**连接 sub-instance 之间的边**
- 与 main body 的 `EdgeConnDecl` 形式相同

#### 3. PortDecl — 端口声明（已有）

```ce
>> portName             // 无类型
>> str portName         // 带类型
```

- 例：`>> str msg`（stdio.Println 的输入端口）
- 例：`>> out_str`（hello 的输出端口，无类型）

#### 4. FlowDecl / FlowStmt — 内部 flow

```ce
src                                    // 裸出度（隐式 emit）
src >>                                 // 显式出度
src >> dst.attr;                       // 内部 flow（带 `;`）
```

- 例：`new >>`（world 内部出度）
- 例：`out_str >> p.msg;`（hello 内部 flow，p 是 sub-instance）

### 嵌套关系

```
hello (node) ─────────┐
  ├─ sub-instance: world w   ─→  world 节点（独立声明）
  ├─ sub-edge: h <add_str> w
  ├─ port: out_str
  └─ flow: out_str >> p.msg
```

每个 sub-instance 引用一个**外部定义的节点**（在同包或其他包里）。

---

## 三个 .ce 源码完整分析

### 1. main.ce

```ce
package main

import stdio

hello {
    h := "hello"
    world w;             // sub-instance: 类型 world, 名字 w
    h <add_str> w        // sub-edge: h → w, via <add_str>

    >> out_str           // port 声明（hello 的输出）
    stdio.Println p;     // sub-instance: 类型 stdio.Println, 名字 p
    out_str >> p.msg;    // flow: hello.out_str → p.msg
}

world {
    >> str words         // port 声明（world 的输入）
    new := words + " world!"
    new >>                // 出度
}

<add_str> {              // 顶层 edge
    hello.h >> world.words
    world.new >> hello.out_str
}

main {
    hello happy;         // 只声明 entry
}
```

**分析**：
- `hello` 节点 body 是 sub-graph
- `world` 节点是 simple sync block（输入 words，返回 new）
- `<add_str>` 是跨实例的 edge（含 body）
- `main` 只声明 entry

### 2. stdio.ce

```ce
package stdio

import io

@Println {
    >> str msg           // 输入端口 msg
    nl := "\n"
    num fid = 1
    data := msg + nl
    io.write out;        // sub-instance: io.write
    fid >> out.fid       // flow: Println.fid → out.fid
    data >> out.data     // flow: Println.data → out.data
}
```

**分析**：
- `Println` 节点 body 包含 sub-instance `io.write out`
- 内部 flow 把 fid/data 传给它

### 3. io.ce

```ce
package io

@write {
    >> num fid           // 输入端口
    >> str data          // 输入端口
    
    fid >> SYSCALL.fid   // 内部 flow 到保留字 SYSCALL
    data >> SYSCALL.data
}
```

**分析**：
- `write` 节点 body 直接 flow 到保留字 `SYSCALL`
- 没有 sub-instance（直接到保留字）

---

## 目标 emit 风格

### hello world（M4.5）

#### 输入
```ce
hello { h := "hello"; world w; h <add_str> w; >> out_str; stdio.Println p; out_str >> p.msg; }
world { >> str words; new := words + " world!"; new >> }
<add_str> { hello.h >> world.words; world.new >> hello.out_str }
main { hello happy; }
```

#### 输出
```go
package main

import (
    "syscall"
)

// ===== Node: world (package main) =====
type world struct {}

func Newworld() *world {
    world_instance := &world{}
    return world_instance
}

func (n *world) block0(words string) string {
    new := words + " world!"
    return new
}

// ===== Node: stdio.Println =====
type Println struct {}

func NewPrintln() *Println {
    return &Println{}
}

func (n *Println) block0(msg string) {
    // MVP: Println 直接 syscall.Write(1, msg)
    syscall.Write(1, []byte(msg))
}

// ===== Node: hello (package main) =====
type hello struct {
    h string
}

func Newhello() *hello {
    hello_instance := &hello{
        h: "hello",
    }
    // sub-instance: world w
    w := Newworld()
    // sub-edge: h <add_str> w → 调 w.block0(hello_instance.h)
    out_str := w.block0(hello_instance.h)
    // sub-instance: stdio.Println p
    p := NewPrintln()
    // flow: out_str >> p.msg → 调 p.block0(out_str)
    p.block0(out_str)
    return hello_instance
}

// ===== Node: io.write =====
type write struct {}

func Newwrite() *write {
    return &write{}
}

func (n *write) block0(fid int, data string) {
    // MVP: write 直接 syscall.Write
    syscall.Write(fid, []byte(data))
}

// ===== Node: stdio.Println (with sub-instance io.write) =====
// 实际合并到上面 Println 的 NewPrintln 里

func main() {
    _ = Newhello()  // main 只调 entry
}
```

### 关键 emit 模式

#### 模式 1：sync block → method

```go
func (n *world) block0(words string) string {
    new := words + " world!"
    return new
}
```

- 方法名 `blockN`（N 是 block 索引）
- 输入 → 参数
- 输出 → 返回值

#### 模式 2：constructor = orchestrator

```go
func Newhello() *hello {
    hello_instance := &hello{h: "hello"}
    
    // ① sub-instance 声明 → NewXxx()
    w := Newworld()
    
    // ② sub-edge → 调 w.block0(hello.h)
    out_str := w.block0(hello_instance.h)
    
    // ③ 另一个 sub-instance
    p := NewPrintln()
    
    // ④ 内部 flow → 调 p.block0(out_str)
    p.block0(out_str)
    
    return hello_instance
}
```

#### 模式 3：main = entry

```go
func main() {
    _ = Newhello()
}
```

- main 只调 entry 节点的 NewXxx()
- entry 节点通过 sub-graph 编排整个执行流

---

## 编译器改造清单

### AST 层（`internal/parser/ast/ast.go`）

需要给 `StructMember` 接口加新类型：
- `SubInstanceDecl`：`typeName varName`（在节点 body 内）
- `SubEdgeDecl`：`src <edge> dst`（在节点 body 内）

已有：
- `InstanceDecl`（用于 main body）
- `EdgeConnDecl`（用于 main body）

### Parser 层（`internal/parser/parse_decl.go`）

`parseStructMember` 需要识别：
- `IDENT IDENT`（peekN(1) 是 IDENT）→ **SubInstanceDecl**（节点 body 专用）
  - 注意：这会和 `hello <add_str>` 冲突——需要看 `peekN(2)` 是否是 `<` 来区分
  - 实际是：如果是 `<`，就是 sub-edge；否则是 sub-instance
- `IDENT < EDGE_NAME IDENT` → **SubEdgeDecl**

### Semantic 层（`internal/semantic/`）

- `CheckMainBody` 改为 `CheckNodeBody`，所有节点 body 都检查 sub-instance + sub-edge
- 校验 sub-instance type 存在
- 校验 sub-edge 在符号表能找到对应 edge body

### IR 层（`internal/ir/ir.go`）

`IRNode` 加字段：
- `SubInstances []*IRSubInstance`：`typeName + instanceName`
- `SubEdges []*IRSubEdge`：`srcAttr + edgeName + dstInstanceName + dstAttr`

### Codegen 层（`internal/codegen/emit.go`）

新的 `emitNode`：
1. struct（state 字段）
2. NewXxx() 构造器：
   - init state
   - 对每个 sub-edge：创建 dst instance，调 dst.block0(self.<output>)
3. block0() 方法（外部调用入口）
4. main() = `_ = New<entry>()`

---

## 关键设计原则

### 1. 嵌套递归

每个节点 body 可以包含**任意深度**的 sub-graph。理论上：
- `hello` 包含 `world` 和 `println`
- `world` 还可以包含 `trim`、`concat` 等
- 这是一棵调用树

### 2. main 是最外层入口

main body 极简（只声明 entry），不包含任何 edges 或 flows。所有编排由各节点的 NewXxx() 完成。

### 3. 命名规则

| 元素 | 命名 |
|------|------|
| struct | `<TypeName>` |
| 构造器 | `New<TypeName>()` |
| 实例变量 | `<lowercase_type_name>_instance`（如 `hello_instance`） |
| sync block 方法 | `block0`, `block1`, ... |
| 特殊 edge 方法 | `<src>_<edge>_<dst>`（user 的例子：`hello_addstr_world`） |

### 4. 字段 vs 实例变量

`hello_instance.h`（访问 state 字段）和 `hello_instance`（实例变量本身）不能冲突：
- struct 字段 `h`（来自 init stmt）
- 局部变量 `hello_instance`（构造器自创建）

### 5. 构造器模式统一

所有 NewXxx() 遵循同一模式：
```go
NewXxx() *X {
    self := &X{state init}    // 1. init state
    for each sub-instance {   // 2. 递归构造
        child := New<ChildType>()
        for each sub-edge {     // 3. 调子方法
            child.block0(self.<output>)
        }
    }
    return self
}
```

---

## 实施步骤

### 步骤 1：扩展 AST
加 `SubInstanceDecl` + `SubEdgeDecl` types

### 步骤 2：扩展 Parser
`parseStructMember` 加新形式识别

### 步骤 3：扩展 Semantic
校验 sub-instance 和 sub-edge

### 步骤 4：扩展 IR
`IRNode` 加 `SubInstances` + `SubEdges` 字段

### 步骤 5：重写 Codegen
新的 `emitNode` 实现构造函数编排器

### 步骤 6：测试验证
跑 main.ce + stdio + io

### 步骤 7：更新文档
所有 docs 同步新设计

---

## 关键参考

- `example/debug/000-example.go` — 目标 emit 风格（user 提供）
- `example/main.ce` — 新 main.ce 语法
- `example/stdio/stdio.ce` — 新 stdio 语法
- `example/io/io.ce` — 新 io 语法
- `internal/codegen/emit.go` — 当前 codegen（待改造）
- `internal/ir/ir.go` — IRNode 结构（待扩展）
- `internal/parser/ast/ast.go` — AST 定义（待扩展 StructMember）