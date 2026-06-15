# Mocker (`.ce`) 语言规范

> **Mocker** = 非过程化的图形化 DSL → 单一 Go 二进制
>
> 文件后缀：`.ce`（取自 **C**ompilable **E**xecutable 的缩写，呼应自举目标）
> 设计目标：**严谨、可自举（self-hosting）**
>
> 配套文档：
> - [circle/docs/ast_design.md](../circle/docs/ast_design.md) — AST 节点设计
> - [circle/docs/execution.md](../circle/docs/execution.md) — 执行文档（构造函数编排器）
> - [ir-design.md](./ir-design.md) — IR 设计文档

---

## 〇、目录

1. [设计哲学](#一设计哲学)
2. [文件 / 包 / 模块](#二文件--包--模块)
3. [两大第一公民：节点 / 边](#三大第一公民节点--边)
   - 3.5 [入口保留名 `main`](#35-入口保留名main既是包名又是入口节点) ⭐
   - 3.6 [语法糖：节点体内的端口直转发](#36-语法糖节点体内的端口直转发) ⭐
4. [数据流：单链 / 续行 / 并发扇出](#四数据流单链--续行--并发扇出)
5. [函数 / 枚举 / 类型 / 表达式](#五函数--枚举--类型--表达式)
6. [模块可见性：export 与 import](#六模块可见性export-与-import)
7. [三层系统架构：sysio / io / stdio](#七三层系统架构sysio--io--stdio)
8. [编译终点：sysio 边界](#八编译终点sysio-边界)
9. [关键字 / 操作符总表](#九关键字--操作符总表)
10. [AST 节点概览](#十ast-节点概览)
11. [自举（self-hosting）设计](#十一自举self-hosting-设计)
12. [设计决策记录](#十二设计决策记录)

---

## 一、设计哲学

### 1.1 三句话

1. **非过程化**：节点只声明接口，边只描述点对点数据流，节点 body 内的 sub-graph 表达"包内怎么连" —— **没有"先做 A 再做 B"的过程语义**。
2. **图形化（Graph-oriented）**：程序的本质是有向图（节点 + 边），运行时是数据沿图流动。
3. **自举优先**：所有语义都必须在 `.ce` 自己内部能完整表达，包括 stdio / io / sysio 三个 runtime 层。

### 1.2 两类构造 + 三层职责严格分离

| 构造 | 角色 | 谁写 |
| --- | --- | --- |
| **节点（Node）** | 接口声明 + 自带 sub-graph（含 body 内的 sub-instance / sub-edge / 控制流） | stdio / io / sysio 包作者 |
| **边（Edge）** | 节点之间的点对点数据流实现（含 body） | 同上 |

| 层 | 角色 |
| --- | --- |
| **节点（Node）** | 纯接口声明 + body 内 sub-graph |
| **边（Edge）** | 节点间的点对点数据流 |

| 运行时层 | 包 | 说明 |
| --- | --- | --- |
| `stdio` | 用户接口 | 节点 / 边齐全 |
| `io` | 文件描述符抽象 | 节点 / 边齐全 |
| `sysio` | 编译终点 | 节点骨架（body 由编译器接管） |

> **不能混**：节点体里写"过程性"控制流（if / for）、边体里写"声明性"东西 —— 各自有各自的位置。

### 1.3 编译终点固定

`sysio.*` 节点是**唯一的内核交互点**，编译器硬编码"遇到 sysio 节点 → emit 真实 syscall"。

这保证：
- `sysio` 之上所有代码都是 `.ce` 写的（可自举）
- `sysio` 之下是平台特定 syscall（不可自举，但只是薄薄一层）

---

## 二、文件 / 包 / 模块

### 2.1 文件后缀：`.ce`

所有源文件以 `.ce` 结尾。例：

```
example/
├── main.ce              # 业务入口
├── sysio.ce             # 编译终点层
├── io.ce                # 文件描述符抽象层
└── stdio/
    └── stdio.ce         # 用户接口层
```

### 2.2 包声明

```ce
package <name>
```

`name` 是标识符。包名应与目录名一致（约定）。

### 2.3 导入

```ce
import <pkgname>
```

按包名导入，无路径前缀、无版本号、无 `as` 别名。

---

## 三、两大第一公民：节点 / 边

> 这是 Mocker 与一般命令式语言最大的区别 —— **图是第一公民**，但只有**两种**第一公民：**节点（Node）** 和 **边（Edge）**。
>
> 历史上曾经设想的"拓扑块"（`stdio { ... }`、`io { ... }`）已经在 M4.5 简化掉，**整套语言里没有第三个第一公民**：
> - **节点** = 自带 sub-graph 的对象（body 里可以内嵌 sub-instance + sub-edge）
> - **边** = 在节点之间搬运数据的连接
> - **包内的"图怎么连"通过节点的 sub-graph 表达**，不需要单独的拓扑块
> - **main 包** 用一个特殊的入口节点（也叫 `main`）来声明 entry 实例，其他什么都不用写

### 3.0 Block 模型（底层图结构）

按用户拍板：Mocker 是**纯图语言**——没有"port"这个概念，只有**入度（in）**和**出度（out）**：

```
节点 = 1 个内含 n 个 block 的图
    ↓
每个 block 有 a 个入度（>> type name）和 b 个出度（name >>）
    ↓
block 里生成的变量生命周期 == block 生命周期
    ↓
a=0 的 block = auto-exec（创建 node 时跑一次）
    ↓
"node.x" 外部访问 = 某个 block 的入度或出度
```

**关键术语**（按用户拍板，语义层全部对齐）：
- **入度（In）** = 数据流进 block 的入口（`>> type name`），有声明类型
- **出度（Out）** = block 计算出往外送的值（`name >>`），无显式类型
- **没有"port"这个概念** —— 只用图论里的入度/出度

#### 3.0.1 保留字（内置原始节点）

按用户拍板保留**几个关键字**作为编译器内置原始节点 —— **不是 .ce 文件里的节点**，编译器自带 emit 模板：

| 关键字 | 含义 | 编译器 emit |
| --- | --- | --- |
| `SYSCALL` | 系统调用边界 | 根据边的 body + 边名生成对应 syscall（如 `<syscall>` → `syscall.Write(fid, data)`）|
| `EXIT` | 进程退出 | `os.Exit(code)`（规划中）|
| `ALLOC` | 内存分配 | `make([]byte, size)`（规划中）|

**使用方式**：在顶层 `EdgeDecl` 的 dst 上写 `SYSCALL` 即可（保留节点天然就是边的一端）：

```ce
@write {
    >> num fid
    >> str data
}

io.write <syscall> SYSCALL {
    io.write.fid >> SYSCALL.fid
    io.write.data >> SYSCALL.data
}
```

编译器看到 `<syscall>` 边对接 `SYSCALL` 时，直接 emit `syscall.Write(fid, []byte(data))`，不需要任何 .ce 文件定义 SYSCALL。

**语义层识别**：`semantic.IsReservedNode("SYSCALL")` 返回 true，跨包查找时优先匹配。

#### 3.0.2 Block 的多位置 + 优化设计

**多个 block 可以散布**（用户拍板）：
- 一个节点可以包含 **n 个 block**
- 每个 block 有 a 个入度和 b 个出度
- **blocks 在源码里不必连续** —— 出度和入度可以在不同位置
- 编译器按需优化：某个 block 如果没被任何边引用 → **不 emit**

#### 3.0.3 边定义的两种形式（Style 1 + Style 2）

**Style 1（标准形式）**：`src <edge_name> dst { body }`

```ce
Println <write> io.write {
    Println.fid >> io.write.fid
    Println.data >> io.write.data
}
```

- **图结构清晰**：一眼看到 src/dst
- **适合 top-level edge + 跨包引用**

**Style 2（语法糖，仅紧凑场景）**：`<edge_name> { body }`

```ce
<write> {
    Println.fid >> io.write.fid
    Println.data >> io.write.data
}
```

- **省略 src/dst**，编译器从 body 推导
- **限制**：body 必须有唯一 src 和唯一 dst（fan-out / 多源不支持）
- **推导失败时报错**：提示用 Style 1 显式指明
- **适用场景**：单 src + 单 dst 的内部边，代码更紧凑

| 情况 | Style 2 处理 |
| --- | --- |
| 单 src + 单 dst（不同 attr）| ✅ 推导 |
| 多 src（多前缀不同）| ❌ 报错：用 Style 1 |
| 多 dst（fanout）| ❌ 报错：用 Style 1 |
| 单 ident（`fid>>fid`）| ❌ 报错：src/dst 不可推导，用 Style 1 |

**编译器判定**：`InferEdgeEndpoints` 在解析后扫描 body 收集前缀集合，校验唯一性后填回 edge.Src / edge.Dst。

#### 3.0.4 sub-graph：节点 body 自带"内部连线"（取代拓扑块的角色）

**M4.5 起，节点 body 自带 sub-graph**，承担了原先"拓扑块"的职责。这是 Mocker 语言的**核心机制**——通过在节点 body 里直接声明子实例、子边和内部 flow，表达"包内数据怎么路由"，不需要单独的拓扑块。

**三种 sub-graph 成员**（节点 body 内）：

| 成员 | 语法 | 含义 |
| --- | --- | --- |
| SubInstanceDecl | `world w;` | 声明一个子实例 |
| SubEdgeDecl | `h <add_str> w` 或 `<add_str> w` | 在子实例之间连边（显式/隐式源） |
| FlowDecl（内部） | `out_str >> p.msg;` | 把本节点的字段/出度喂给子实例的入端口 |

**完整示例**（来自 `example/main.ce`，展示 hello 节点如何自包含一个子图）：

```ce
hello {
    h := "hello"             // 局部变量（auto-exec block 的输出）
    world w                  // SubInstanceDecl：声明子实例 w（类型 world）
    <add_str> w              // SubEdgeDecl（隐式源）：编译器推断 h → w

    >>str out_str            // 入端口：接收 world 计算结果
    stdio.Println p          // SubInstanceDecl：跨包子实例
    out_str >> p.msg         // FlowDecl（内部）：out_str → p.msg
}
```

> **对比旧拓扑块（已删除）**：以前 `stdio { Println <write> io.write }` 是单独一行拓扑声明；现在每个节点在自己 body 里直接写出"我依赖哪些子实例、怎么连"。这保证了每个节点是**自描述的**——不需要外部拓扑块来"组装"。

这些 sub-graph 成员由 IR 阶段的 `IRSubInstance` / `IRSubEdge` / `IRSubFlow` 承担，最终在 codegen 里通过"构造函数编排器"模式递归构造子实例、调用子方法。

详见：[constructor-orchestrator-design.md](file:///home/wpp/homework/Mocker/circle/docs/constructor-orchestrator-design.md) 和 [execution.md](file:///home/wpp/homework/Mocker/circle/docs/execution.md)。

#### 3.0.5 多文件同包共享变量

**同一个文件夹内的多个 .ce 文件可以使用同一个 package 名**：

```
stdio/
├── stdio.ce       // package stdio, @Println
└── to_string.ce   // package stdio, @to_string
```

- 这些文件 **共享一个 SymbolTable**
- 节点 / 边 / 子实例都共享
- **跨文件夹同名 = 错误**（避免歧义）

#### 3.0.6 `:=` vs `=` 严格区分

| 符号 | 语义 | 用法 | 类型来源 |
| --- | --- | --- | --- |
| `:=` | **类型推断**（Go 风格）| `name := expr` | 从 expr 推论 |
| `=`  | **显式类型** | `type name = expr` | 用户写明 |

**错误组合（parser 拒绝）**：
- `type name := expr` ← 混搭，禁止
- `name = expr` ← 无显式类型却用 `=`，禁止

**例**：
```ce
msg := "hello"        // 推断 msg 是 str
num fid = 1            // 显式 num
data := msg + "\n"     // 推断 data 是 str（msg + str 字面量）
```

#### 3.0.7 类型推导 + 隐式初始化检查

Mocker 节点体里的表达式经过**类型推导**和**隐式初始化检查**：

**支持的类型推导**（`InferExprType`）：

| 表达式 | 推导规则 |
| --- | --- |
| `"hello"` | `str` |
| `42` | `num` |
| `true`/`false` | `bool` |
| `x` | 查 env（local var / port） |
| `a.b` | 查 `a.b` 的类型（节点字段） |
| `str + str` | `str` |
| `num + num` | `num` |
| `str + num` | **类型错误** |
| `num -/*/% num` | `num` |
| `x op y`（op 为 `== != < >`）| `bool` |
| `&& \|\|` | `bool` |
| `-x` | 需要 `num` |
| `!x` | 需要 `bool` |
| `func()` | `any`（MVP）|

**拼接糖 msg+nl 验证**：

```ce
nl := "\n"
data := msg + nl       // ✅ str + str = str
data := msg + 999      // ❌ "cannot use str + num"
```

**隐式初始化检查**（`CheckNodeBody`）：

每个 ident 引用都必须在使用前声明：

```ce
@Println {
    >> str msg
    unknown := xxx + msg   // ❌ "use of undeclared name xxx"
    unknown >>
}
```

支持的成员类型：
- `VarDecl`（`name := expr` / `type name = expr`）
- `FlowDecl`（`name >>`）
- `FieldDecl`（`type name`）
- `PortDecl`（`>> type name`）
- `SubInstanceDecl` / `SubEdgeDecl`（节点 body 内 sub-graph）

**检查时机**：在 `CheckAll` / `Check` 的步骤 3b（节点 body 检查阶段）跑，先 `ResolveNodeBody` 收集所有 decl，再 `CheckNodeBody` 走 expression。

**对应实现**：[`internal/semantic/infer.go`](file:///home/wpp/homework/Mocker/circle/internal/semantic/infer.go)

### 3.1 节点（Node）

#### 3.1.1 语法

```ce
@<Name> {        ← @ 前缀 = export
    <body>
}
```

或私有节点：

```ce
<Name> {
    <body>
}
```

#### 3.1.2 节点体（body）允许的内容

| 形式 | 含义 | 例子 |
| --- | --- | --- |
| `>> <type> <name>` | 声明入端口 | `>> str msg` |
| `<name> := <expr>` | 局部变量声明 | `nl := "\n"` |
| `<name> := <default>` | 带默认值的出符号 | `fid := 1` |
| `<name> >>` | 标为出符号 | `fid >>` |
| `<expr> >>` | 表达式流出（编译期算） | `msg + nl >>` |
| `<type> <name>` | 类型字段 | `str name` |
| `<Type> <name>;` | **sub-instance** 声明 | `world w;` / `stdio.Println p;` |
| `<src> <edge> <dst>` | **sub-edge** 声明 | `h <add_str> w` |
| `<edge> <dst>` | 隐式 sub-edge（推断源） | `<add_str> w` |
| `<name> >> <target>` | 内部 flow（喂给 sub-instance 的入端口） | `out_str >> p.msg` |
| `for(...) { ... }` | for 循环 | `for(i:=0; i<3; i++) { ... }` |
| `while(...) { ... }` | while 循环 | `while(x > 0) { ... }` |
| `if ... { ... }` | 条件语句 | `if x > 0 { ... } else { ... }` |
| `<name> += <expr>` | 复合赋值 | `new += " world!"` |
| `<name>++` / `<name>--` | 自增自减 | `i++` |

> 注：上面带 **sub-instance / sub-edge / 内部 flow** 的 3 行是 M4.5 新增；其他行（端口、变量、字段、控制流）在所有节点 body 里通用。

#### 3.1.3 例子（带 sub-graph）

```ce
@Println {
    >> str msg            // 入符号：要打印的字符串
    fid  := 1             // 局部变量：fd，默认 1（stdout）
    nl   := "\n"          // 局部常量
    msg+nl >>             // 出符号：编译期拼接 msg + 换行
    fid >>                // 出符号：fd
}

hello {
    h := "hello"
    world w;                  // sub-instance
    <add_str> w               // sub-edge（隐式源 h）

    >>str out_str
    stdio.Println p;          // sub-instance（跨包）
    out_str >> p.msg;         // 内部 flow：hello.out_str 喂给 p.msg
}
```

> 节点体支持控制流（`if` / `for` / `while` / 复合赋值 / 数学表达式），这些在 codegen 阶段直接转译为 Go 代码。

### 3.2 边（Edge）

#### 3.2.1 语法

```ce
<src> <<edge_name>> <dst> {
    <body>
}
```

- `src` / `dst`：节点名（IDENT / CALL，可带 pkg 前缀如 `io.write`）
- `edge_name`：边名（IDENT / EDGE_NAME / CALL，可含 `-`）
- `body`：边内的点对点数据流

#### 3.2.2 边体（body）允许的内容

| 形式 | 含义 |
| --- | --- |
| `<src.port> >> <dst.port>` | 把源的某端口连到汇的某端口 |

例：

```ce
Println <write> io.write {
    Println.fid  >> io.write.fid     // 源节点端口 → 目标节点端口
    Println.data >> io.write.data
}
```

> **边体 ≠ 节点体**：边体只描述"这条边内部数据怎么走"，不引入新节点、不定义新端口。
>
> 边体 ≠ 节点体里的 sub-edge body：sub-edge 复用了顶层 EdgeDecl（按 `(edge_name)` 查表），所以两边 body 写法一致。

### 3.3 入口节点 `main`（不是拓扑块）

**M4.5 起取消了"拓扑块"概念**。原先 `stdio { ... }` / `io { ... }` 这种"块名 == 包名"的特殊结构体已不存在——编译器从每个包里的 `main` 节点 body（仅 main 包）自动推导 entry instance，**包的内部图则通过各节点自己的 sub-graph 表达**。

`main` 节点是**特殊的入口节点**，body 里只放 **InstanceDecl**（声明 entry 实例）和 **EdgeConnDecl**（在实例之间连边）：

```ce
package main
import stdio

hello {
    // ... 节点 body，可含 sub-instance / sub-edge / flow / 控制流
}

main {
    hello happy;          // InstanceDecl：声明一个 entry 实例（@hello 类型，实例名 happy）
    // EdgeConnDecl（可选）：happy <out> p 等
}
```

`main` 节点 body 允许的形式：

| 形式 | AST 节点 | 含义 |
| --- | --- | --- |
| `<TypeName> <varName>;` | `InstanceDecl` | 声明一个 entry 实例 |
| `<src> <edge> <dst>` | `EdgeConnDecl` | 在实例之间连边 |

`main` body 不允许：
- ❌ 端口声明 / 变量声明 / 控制流 / sub-instance —— 它只是一个"实例清单 + 连线清单"
- ❌ 普通函数体语义

**编译器对 `main` body 的处理**：
1. `InstanceDecl` → 收集成 `IRTopology.VarInstances`（`name → type` 映射）
2. `EdgeConnDecl` → 收集成 `IRTopology.InstanceEdges`（`srcInst <edge> dstInst`）
3. `IRTopology.StartNodes` 从拓扑里挑"有 auto-exec block 的节点"

详见 [3.5](#35-入口保留名main既是包名又是入口节点) 和 [constructor-orchestrator-design.md](file:///home/wpp/homework/Mocker/circle/docs/constructor-orchestrator-design.md)。

### 3.4 节点 vs 边 速查

|  | 节点 | 边 |
| --- | --- | --- |
| **定义在哪** | top-level（`@name { ... }`）| top-level（`src <edge> dst { ... }`）|
| **有 body** | 是 | 是 |
| **含数据流** | 否（声明 / 内部 sub-graph）| 是 |
| **含端口声明** | 是 | 否 |
| **含控制流** | 是（`if`/`for`/`while`）| 否 |
| **含 sub-graph** | 是（sub-instance + sub-edge + flow）| 否 |
| **export 标记** | `@` 前缀 | N/A |
| **跨包引用** | `@pkg.Node`（CALL token）| `pkg.node` 作为 dst |
| **保留节点** | `SYSCALL` / `EXIT` / `ALLOC`（语义层识别）| — |

### 3.5 入口保留名：`main` 既是包名又是入口节点

> **核心约定**：`main` 是 Mocker 的**保留名**（reserved name），由 dispatcher 强制处理。

#### 3.5.1 双层语义

| 出现位置 | 含义 |
| --- | --- |
| `package main` | **入口包**——这个文件是可执行程序的入口点（约定，等价于 Go 的 `package main`） |
| `main { ... }` | **入口节点**——程序启动时 entry instance 的清单 + 连线 |

**两件事统一在 `main` 这一个名字上**：包是 `main`，入口节点也叫 `main`。

#### 3.5.2 dispatcher 行为

```
看到 IDENT == "main" 且后跟 {     →   永远走 parseStructBody(Kind=Node, Name="main")
                                    不会变成函数、struct、edge
```

所以 `package main` 的文件里写 `main { ... }` **不会被当成函数**，也不会有"函数名和包名冲突"的问题。

#### 3.5.3 例子（M4.5 构造函数编排器模式）

```ce
package main
import stdio

hello {
    h := "hello"
    world w;               // sub-instance 声明
    <add_str> w            // 隐式 sub-edge（推断源 h）

    >>str out_str
    stdio.Println p;       // sub-instance 声明
    out_str >> p.msg;      // 内部 flow
}

world {
    >> str words
    new := words
    for(i:=0; i<3; i++) { new += " world!" }
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

// ═══ 入口（main 保留名）═══
main {
    hello happy;           // 只声明 entry instance
}
```

构造时，`main()` 调用 `_ = Newhello()`，hello 的构造函数递归创建 world 和 Println 并调用它们的方法。

#### 3.5.4 编译器/运行时从这个节点 emit 什么

```
1. 创建所有 entry instance：
      main { hello happy; }  →  codegen 生成 Newhello() + _ = Newhello()

2. hello 的 NewXxx() 递归：
      hello body 内有 world w / <add_str> w / stdio.Println p / out_str >> p.msg
      → Newhello() 内部递归 Newworld() / NewPrintln() + 调用边方法

3. 触发 auto-exec 节点：
      hello.auto_exec block 跑 → h 字段被赋值 → h 沿 <add_str> 边流出

4. 整体流程：
      hello(auto) → h → <add_str> → world → new → hello.out_str → p.msg → syscall
```

**整个启动是 declarative 的**——没有 imperative 步骤，全靠图论自动驱动。

#### 3.5.5 跟 Go 的对比

| | Go | Mocker |
| --- | --- | --- |
| 入口包名 | `package main` | `package main` |
| 入口函数 | `func main()` | `main { ... }` **入口节点** |
| 入口体内容 | imperative 语句 | 声明性图结构（instance + edge 连接） |
| 隐式 | 无 | hello 这种无入度节点创建时**自动执行** |

**哲学差异**：Go 的 main 是"程序从这里开始执行"，Mocker 的 main 是"程序从这张图开始存在"——你描述的是"哪些节点和边构成程序"，而不是"按顺序做什么"。

#### 3.5.6 入口节点 body 里写什么

`main { ... }` 节点 body 只声明 **entry instance** + 必要的 **edge 连接**，分号 `;` 可选：

```ce
main {
    hello happy;            // InstanceDecl：声明 entry instance
    happy <out> p           // EdgeConnDecl（可选）：instance 之间连边
}
```

**所有跨实例的边连接、子实例创建，都下放到每个节点自己的 body 中**（构造函数编排器模式）：

```ce
hello {
    h := "hello"
    world w;                // hello 内部：声明 world 实例
    h <add_str> w           // hello 内部：h → w 边
    >> out_str
    stdio.Println p;        // hello 内部：声明 Println 实例
    out_str >> p.msg;       // hello 内部：out_str → p.msg flow
}
```

#### 3.5.7 自举意义

```
compiler/main.ce        ← Mocker 编译器自己的入口
    package main
    @lex { ... }        ← 编译器自己也是 .ce 写的
    @parse { ... }
    @codegen { ... }
    main {
        @lex <pipeline> @parse
        @parse <pipeline> @codegen
    }
```

编译器能编译自己，而且入口本身也是声明性的——Mocker 编译器是自描述的。

#### 3.5.8 reserved name 总表

| 名字 | 角色 | dispatcher 处理 |
| --- | --- | --- |
| `main` | 入口保留名 | 走 `parseStructBody(Kind=Node, Name="main")` |

未来可能会加更多 reserved name（比如 `init` 用于"在 main 之前执行的初始化"），但当前只要 `main` 一个。

#### 3.5.9 M4.5 简化：main 节点极简化（只声明 entry）

> **M4.5 更新**：去掉 main 节点作为"完整入口拓扑"的概念。

**M4.4 之前**（main body 充当完整拓扑块）：
```ce
main {
    hello happy
    stdio.Println p
    happy <out> p
}
```

**M4.5 新设计**（main body 只声明 entry instance，连线下放到各节点 body）：
```ce
main {
    hello happy;   // 只声明 entry instance
}
```

所有跨实例的边连接、各实例的创建顺序，都下放到**每个节点自己的 body**：

```ce
hello {
    h := "hello"
    world w;             // hello 内部：声明 world 实例
    h <add_str> w        // hello 内部：h → w 边
    >> out_str
    stdio.Println p;     // hello 内部：声明 Println 实例
    out_str >> p.msg;    // hello 内部：out_str → p.msg flow
}
```

**原理**：每个节点的 `NewXxx()` 是它自己子图的"编译器"。构造时递归创建子实例 + 调子实例方法。

`main()` 只调 entry：
```go
func main() {
    _ = Newhello()  // hello 的 NewXxx() 内部递归构造 world 和 Println
}
```

详见 [constructor-orchestrator-design.md](file:///home/wpp/homework/Mocker/circle/docs/constructor-orchestrator-design.md) 和 [execution.md](file:///home/wpp/homework/Mocker/circle/docs/execution.md)。

### 3.6 语法糖：节点体内的端口直转发

> 名字记一下：**port-forward sugar**（端口直转发糖）

#### 3.5.1 形式

在节点体内，紧跟一个入端口声明之后，可以直接写一行 `port_name >> target_call`：

```ce
say {
    >> str hey              // 入端口声明
      hey >> stdio.Println  // ← 语法糖：hey 端口的数据 → stdio.Println
}
```

#### 3.5.2 语义

等价于（编译器**直接**展开，不需要写显式边）：

```ce
say {
    >> str hey
    hey <anon> stdio.Println {     // 隐式边
        hey >> stdio.Println       // body 写点对点转发
    }
}
```

**关键点**：这是 **fixed compilation**（固定编译）—— 编译器直接 emit 转发代码，**不经过 EdgeDecl 的运行时边机制**（不开 goroutine、不走 IRTopology 分析路径）。

#### 3.5.3 适用条件（必须全部满足）

1. **LHS 是本节点已有的入端口名**（或局部变量名）
2. **目标节点恰好 1 个入端口**（参数数量 = 1）
3. 端口类型与目标入端口类型一致

这 3 个条件保证连线**无歧义**，编译器可以自动接上。

#### 3.5.4 反例：什么时候不能用

```ce
// io.write 有 2 个入端口（fid + data）→ 不能用糖
say {
    >> str msg
    >> num fid
    //    msg >> io.write(1)    // 歧义：msg 接到 io.write 的哪个端口？fid 还是 data？
    // 必须显式边：
    msg <to_io> io.write {
        msg >> io.write.data
        fid >> io.write.fid
    }
}
```

#### 3.5.5 编译器识别规则

| 节点体里看到 | 编译器动作 |
| --- | --- |
| `>> type name` | 入端口声明 |
| `name >>` | 出符号标 |
| `name >> target`（target 单入端口） | **fixed forward**（语法糖，直接 emit） |
| `name >> target`（target 多入端口） | 报错，要求显式边 |
| `name >>`（带 ChainOp，跨行写法） | 当 FlowStmt 处理（链） |

#### 3.5.6 优势 / 取舍

| 维度 | 糖写法 | 显式边写法 |
| --- | --- | --- |
| 代码量 | 1 行 | 3-5 行 |
| 灵活性 | 只支持"无歧义转发" | 任意点对点 |
| 运行时开销 | 零（编译期展开） | 走边运行时 |
| 是否进 IRTopology | 否（不进图） | 进 IRTopology（作为 InstanceEdge） |
| 能否做跨包链路分析 | 否 | 是 |

**所以糖写法适合**：调单参 utility（如 `Println` / `to_string` / 各种 transform），"数据来了就转一手走人"的场景。
**不适合**：多参数调用、需要 IRTopology 做分析的跨节点连线场景。

#### 3.5.7 当前 stdio 调用写法归类

```ce
// stdio.Println 只有一个入端口 msg → 糖写法
say {
    >> str hey
      hey >> stdio.Println       // 糖：直接 emit 转发到 stdio.Println

    >> str my
        my >> stdio.Println      // 糖：同上

    >> str world
        world >> stdio.Println   // 糖：同上
}
```

这 3 行都用的是 **port-forward sugar**，编译器识别后直接生成 `stdio.Println(msg)` 调用。

而 stdio 内部**仍然是显式边**：

```ce
// stdio 内部
Println <write> io.write {      // 显式边（顶层 EdgeDecl）
    Println.fid  >> io.write.fid
    Println.data >> io.write.data
}
```

因为 `io.write` 有 2 个入端口，**必须显式边**，不能用糖。`stdio.Println <write> io.write` 这条边通过编译器跨包查找（`Println` 是 stdio 包、`io.write` 是 io 包）拼起来，运行时由 `hello` 节点构造函数递归调用。

#### 3.5.8 糖写法使用 checklist

写新代码时，**先问自己 3 件事**：
1. 目标函数/节点是不是**只接 1 个参数**？
2. 数据源是不是**本节点已有的端口或变量**？
3. 类型是不是**对得上**？

3 个都是 YES → 用糖（`port >> target`）。
任何一个 NO → 用显式边（顶层 `src <edge> dst { body }` + 节点 body 内的 sub-edge `src <edge> dst`）。

---

## 四、数据流：单链 / 续行 / 并发扇出

> 数据流是 Mocker 表达"运行时怎么算"的核心机制。

### 4.1 操作符 `>>`

`>>` 表示"数据从左流到右"（左是源，右是汇）。

### 4.2 单链（Single Chain）

```ce
hello.h >> say.hey          // 1 步
hello.h >> say.hey >> stdio.Println   // 2 步链
```

### 4.3 续行（Continuation）

跨行的 `>>` 链：

```ce
hello.h >>                  // 行尾 >> 接续
    say.hey >>
        stdio.Println
```

编译器把这 3 行合并为一条 FlowStmt。

### 4.4 并发扇出（Fan-out / Coroutine）

#### 4.4.1 触发条件

**连续两个 `>>`**（`>>` 紧跟 `>>`）触发 fan-out：

```ce
hello.h >>                  // 第 1 个 >>：源就位
    >>say.hay               // 第 2 个 >>：触发 fan-out，开始新分支
    >>say.my                // 新分支
    >>say.world             // 新分支
```

#### 4.4.2 语义

- 1 个源（`hello.h`）
- N 个**并发**分支（每条分支是一个独立可执行单元 / goroutine）
- 每条分支可继续 chain（同行内 `>>`）

```ce
hello <out> say {
    hello.h >>
    >>say.hay>>stdio.Println    // 分支 1：hay → Println
    >>say.my                    // 分支 2
    >>say.world                 // 分支 3
}
```

#### 4.4.3 AST 表达

用专门的 `FlowFanout` 节点（不混在 `FlowStmt` 里）：

```
FlowFanout src=hello.h (concurrent branches: 3)
  Branch[0] >> say.hay >> stdio.Println
  Branch[1] >> say.my
  Branch[2] >> say.world
```

### 4.5 跨行 vs 并发的区分

| 模式 | token 序列 | 含义 |
| --- | --- | --- |
| `a >> b` | `a >> b` | 链 |
| `a >>` + `b` | `a >> b`（同行） | 链（跨行写法） |
| `a >>` + `>>b` | `a >> >> b`（2 个 `>>`） | **fan-out** |
| `a >>` + `b`（下一行） | `a >> b`（同行写法） | 链（合并跨行） |

> **关键**：`>>` 紧跟 `>>`（2 个连续 `>>`）才触发 fan-out；同行内的 `>> b` 永远只是链。

---

## 五、函数 / 枚举 / 类型 / 表达式

### 5.1 函数

```ce
main {                              // 入口（保留字，永远是 FuncDecl）
    hello <out> say                 // Connection：节点 → 边 → 节点
}

Post(str router) {                  // 带参函数
    Method method := Post
    ...
}
```

### 5.2 枚举

```ce
enum Method {
    Post, Get, Delete,
}
```

### 5.3 类型

内置类型：

| 关键字 | 含义 |
| --- | --- |
| `str` | 字符串 |
| `num` | 数字 |
| `bool` | 布尔 |
| `byte` | 字节 |
| `any` | 任意类型 |

复合类型：

```ce
*ctx                    // 指针
cookie[]                // 数组
```

### 5.4 表达式

运算符优先级（高 → 低）：

| 优先级 | 运算符 |
| --- | --- |
| 7 | `.`（成员访问） |
| 6 | `*` / `/` |
| 5 | `+` / `-` |
| 4 | `<` / `>` / `<=` / `>=` |
| 3 | `==` / `!=` |
| 2 | `&&` |
| 1 | `\|\|` |

赋值：`name := expr`

### 5.5 控制流

Mocker 支持 C/Go 风格的控制流语句，**只能出现在节点体内部**。

#### 5.5.1 for 循环

```ce
// C 风格 for（init; cond; post）
for(i:=0; i<3; i++) { new += " world!" }

// Go 风格 while（cond only）
for(i < 10) { process() }

// 带复合赋值的 body
for(i:=0; i<t; i++) { result += step }
```

#### 5.5.2 while 循环

```ce
while(x > 0) { x -= 1 }
```

codegen 阶段转译为 Go 的 `for x > 0 { ... }`。

#### 5.5.3 if / else if / else

```ce
// Go 风格（推荐）
if x > 0 { ... } else if x == 0 { ... } else { ... }

// C 风格（括号可选）
if (x > 0) { ... } else { ... }
```

#### 5.5.4 复合赋值与自增自减

```ce
x += 1      // 等价于 x = x + 1
y -= 2      // 等价于 x = x - 2
z *= 3
w /= 4
i++         // 仅限 for post 位置
j--
```

#### 5.5.5 数学表达式

支持完整的算术表达式（Pratt 算法），含括号和运算符优先级：

```ce
t := 1+2*1-8+(2+2)*2   // → 3
```

---

## 六、模块可见性：export 与 import

### 6.1 export 标记：唯一靠 `@` 前缀

```ce
@Println { ... }      // exported：包外可调 stdio.Println
Println { ... }       // private：仅本包内可见
```

### 6.2 import

```ce
import stdio

@main {
    msg >> stdio.Println       // 用 stdio 的 exported 节点
}
```

### 6.3 与 sub-graph 的边界

| 概念 | 决定 | 实现机制 |
| --- | --- | --- |
| 节点是否包外可见 | `@` 前缀 | 编译期检查 |
| 包内节点之间怎么连 | 节点 body 内的 sub-instance + sub-edge + flow | 构造函数编排器模式 |

> 注：**已经没有"包级拓扑块"概念**。`stdio`、`io` 这些包没有 `{ Println <write> io.write }` 这样的块结构——包内节点之间的连线由每个节点 body 自己声明的 sub-graph 表达。

---

## 七、三层系统架构：sysio / io / stdio

> Mocker 的标准库分三层，每层职责严格分离。

### 7.1 调用链

```
┌──────────────────────────────────────────────────────────┐
│ 用户代码                                                 │
│   msg >> stdio.Println                                  │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│ stdio 层（用户接口）                                     │
│   @Println {                                            │
│       >> str msg                                        │
│       msg + "\n" >>                                     │
│   }                                                     │
│   Println <write> io.write { ... }                      │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│ io 层（文件描述符抽象）                                  │
│   @write { >>num fid  >>byte data }                     │
│   io.write <syscall> SYSCALL { ... }                    │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│ sysio 层（编译终点 / 内核交互）                          │
│   @write { >>num fid  >>byte data }                     │
│   编译器遇到 sysio.write → emit syscall.Write(fid,data)│
└──────────────────────────────────────────────────────────┘
```

### 7.2 各层职责

| 层 | 内容 | 依赖 |
| --- | --- | --- |
| `sysio` | 节点骨架，**body 由编译器接管** | 无（最底层） |
| `io` | 文件描述符级 read/write，引用 sysio | sysio |
| `stdio` | `Println` 等用户接口，引用 io | io |

### 7.3 层次不变量

- `sysio` **绝不** import 任何包（它是叶子）
- `io` **只能** import `sysio`
- `stdio` **只能** import `io`（不能直接 import `sysio`）
- 用户代码 import `stdio`（不能直接 import 底层）

---

## 八、编译终点：sysio 边界

### 8.1 概念

`sysio.*` 节点是**编译器硬编码的边界**：

- 节点体留空（无需写实现）
- 编译器在 IR 阶段识别 `sysio.write(fid, data)` 调用
- 编译期替换为对应平台的 syscall：
  - Linux/macOS: `syscall.Write(fid, data)`
  - Windows: `WriteFile(handle, data)`

### 8.2 sysio 节点清单（待扩展）

| 节点 | 签名 | emit |
| --- | --- | --- |
| `sysio.write` | `>>num fid  >>byte data` | `syscall.Write(fid, data)` |
| `sysio.read` | `>>num fid  >>byte buf` | `syscall.Read(fid, buf)` |
| `sysio.exit` | `>>num code` | `syscall.Exit(code)` |
| `sysio.fork` | `>>any fn` | `runtime.GoFork(fn)`（自举映射到 goroutine） |

### 8.3 自举影响

sysio 必须能用 syscall / RawSyscall 实现，**不能用 cgo / unsafe / reflect**（这些依赖 Go 自身的 runtime，自举就断了）。

---

## 九、关键字 / 操作符总表

### 9.1 关键字（26 个）

| 类别 | 关键字 |
| --- | --- |
| 包 | `package` / `import` |
| 声明 | `enum` |
| 控制流 | `if` / `else` / `for` / `while` / `return` |
| 字面量 | `true` / `false` |
| 类型 | `str` / `num` / `bool` / `byte` / `any` |
| 特殊 | `main`（保留作入口） |

### 9.2 操作符

| 类别 | 操作符 |
| --- | --- |
| 数据流 | `>>`（流到）/ `<<`（反流） |
| 边名 | `<` ... `>`（边名括号） |
| 赋值 | `:=`（声明） / `=`（暂未用） / `+=` / `-=` / `*=` / `/=` |
| 自增自减 | `++` / `--`（仅 for post 位置） |
| 比较 | `==` / `!=` / `<` / `>` / `<=` / `>=` |
| 逻辑 | `&&` / `\|\|` / `!` |
| 算术 | `+` / `-` / `*` / `/` |
| 成员 | `.` |
| 分隔 | `(` `)` `[` `]` `{` `}` `,` `;` `:` |
| 标识 | `@`（export） / `#`（注释） |

### 9.3 符号与词法分类

| 符号 | 类别 |
| --- | --- |
| `>>` / `<<` | 多字符 OP |
| `:=` / `==` / `!=` / `<=` / `>=` / `&&` / `\|\|` | 多字符 OP |
| `<` / `>` | 边界符（边名专用），也作比较 OP |
| `.` | 成员访问 |
| `@` | export 前缀 |
| `_` | 通配 / 忽略 |

---

## 十、AST 节点概览

> 详细定义见 [circle/docs/ast_design.md](../circle/docs/ast_design.md)，这里只列大纲。

### 10.1 顶层 Decl

| 节点 | 例子 | 备注 |
| --- | --- | --- |
| `File` | 整个文件 | 装 Pkg / Imports / Decls |
| `PackageDecl` | `package main` | |
| `ImportDecl` | `import stdio` | |
| `EnumDecl` | `enum Method { ... }` | |
| `StructDecl` | `name { ... }` / `@name { ... }` / `main { ... }` | 节点（@ 前缀）也是 StructDecl；`main` 是 `StructKindNode` |
| `EdgeDecl` | `src <edge> dst { ... }` | |

### 10.2 StructMember（节点体内的成员）

| 节点 | 例子 |
| --- | --- |
| `PortDecl` | `>> str msg`（入端口）/ `name >>`（出端口） |
| `VarDecl` | `h := "hello"` |
| `FieldDecl` | `str name`（类型字段） |
| `AssignStmt` | `a = expr` / `a += expr` / `i++` |
| `FlowDecl` | `out_str >> p.msg`（内部 flow） |
| `SubInstanceDecl` | `world w;`（子实例声明） |
| `SubEdgeDecl` | `h <add_str> w` 或 `<add_str> w`（隐式推断） |
| `InstanceDecl` | `hello happy`（main 节点内的 entry 实例） |
| `ForStmt` | `for(i:=0; i<3; i++) { ... }` |
| `WhileStmt` | `while(cond) { ... }` |
| `IfStmt` | `if cond { ... } else if ... { ... } else { ... }` |

### 10.3 FlowStep 内部

| 节点 | 含义 |
| --- | --- |
| `FlowStep` | 数据流的一步：target + 可选 rename |
| `FlowIdent` | 标识符链（含 `sysio.write(fid)` 形式的 CallExpr） |
| `FlowLiteral` | 字符串字面量 |
| `FlowBranch` *（草案）* | fan-out 的一条分支 |

### 10.4 Expr

`IdentExpr` / `LiteralExpr` / `MemberExpr` / `CallExpr` / `BinaryExpr` / `UnaryExpr`

### 10.5 TypeRef

`TypeName` / `TypeArray` / `TypePtr`

---

## 十一、自举（self-hosting）设计

### 11.1 自举目标

Mocker 编译器（M1 → M10 阶段）最终能用 Mocker 自己写出来，即：
- lexer / parser / semantic / IR / codegen 全部用 `.ce` 写
- 编译产物仍是 Go 二进制（先 bootstrap 到 Go，再自举）

### 11.2 自举层次

```
┌─────────────────────────────────────────────────────┐
│ Mocker 编译器自身（最终用 .ce 写）                   │
│   - parser.ce                                       │
│   - semantic.ce                                     │
│   - ir.ce                                           │
│   - codegen.ce                                      │
└────────────────────┬────────────────────────────────┘
                     │ 编译期
                     ▼
┌─────────────────────────────────────────────────────┐
│ 标准库（也是 .ce 写）                                │
│   - stdio.ce（用户接口）                             │
│   - io.ce（文件抽象）                                │
│   - sysio.ce（编译终点）                             │
└────────────────────┬────────────────────────────────┘
                     │ 编译期
                     ▼
┌─────────────────────────────────────────────────────┐
│ 平台层（不可自举，Go 写）                            │
│   - sysio.write → syscall.Write                    │
│   - sysio.read  → syscall.Read                     │
└─────────────────────────────────────────────────────┘
```

### 11.3 自举约束

- `sysio` 只能用 `syscall` / `RawSyscall`，**禁用** `cgo` / `unsafe` / `reflect`
- `io` / `stdio` 只能用 `sysio` 提供的原语
- 编译器不能假设任何"Go 高级特性"，所有 runtime 都得在 `.ce` 里能表达

---

## 十二、设计决策记录

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 文件后缀 | `.ce`（非 `.mocker`） | 反映自举目标，更严谨 |
| 第一公民数量 | **只有节点 + 边两种**（M4.5 取消拓扑块） | 减少概念；包内连线由节点 body 内的 sub-graph 承担 |
| 图形化 vs 过程化 | 图形化 | 节点 / 边分立，无"先做 A 再做 B" |
| 出口标记 | `@` 前缀 | 不加新关键字，复用现有符号 |
| main 节点 | 只声明 entry instance | 所有编排下放到各节点 body（构造函数编排器模式） |
| 三层架构 | sysio / io / stdio | 机制 / 抽象 / 语义清晰分离 |
| sysio 边界 | 编译器硬编码 emit | 单一可信源，简化跨平台 |
| 控制流 | 透传 Go 语法 | for/while/if 直接转译为 Go 代码，无需 IR 解析 |
| 隐式 SubEdge | 类型唯一匹配推断 | `<add_str> w` 省略源变量，编译器自动推断 |
| 数学表达式 | Pratt 算法 | 正确处理运算符优先级和结合性 |
| CLI | `circle build` | 简化为单命令 + `-debug` 标志 |

---

## 十三、待办 / 未决

| 项 | 状态 | 备注 |
| --- | --- | --- |
| `FlowBranch` AST 节点 | 草案 | fan-out 的分支节点 |
| 多 SubEdge 同一目标时的排序和合并 | 待优化 | — |
| 循环依赖检测 | 待优化 | 防止无限递归 |
| CI / release | 未开始 | — |

> **已完成**：构造函数编排器模式、隐式 SubEdge 推断、控制流（for/while/if）、复合赋值、数学表达式、双向边 d2 图、简化的 CLI（`circle build`）。
