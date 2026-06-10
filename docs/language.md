# Mocker (`.ce`) 语言规范

> **Mocker** = 非过程化的图形化 DSL → 单一 Go 二进制
>
> 文件后缀：`.ce`（取自 **C**ompilable **E**xecutable 的缩写，呼应自举目标）
> 设计目标：**严谨、可自举（self-hosting）**
>
> 配套文档：
> - [Mocker架构设计.md](./Mocker架构设计.md) — 整体架构 / 流水线
> - [parser.md](./parser.md) — Parser 实现细节
> - [circle/docs/ast_design.md](../circle/docs/ast_design.md) — AST 节点设计

---

## 〇、目录

1. [设计哲学](#一设计哲学)
2. [文件 / 包 / 模块](#二文件--包--模块)
3. [三大第一公民：节点 / 边 / 拓扑块](#三三大第一公民节点--边--拓扑块)
   - 3.5 [入口保留名 `main`](#35-入口保留名main既是包名又是拓扑块) ⭐
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

1. **非过程化**：节点只声明接口，边只描述连线，拓扑块只声明图骨架 —— **没有"先做 A 再做 B"的过程语义**。
2. **图形化（Graph-oriented）**：程序的本质是有向图（节点 + 边），运行时是数据沿图流动。
3. **自举优先**：所有语义都必须在 `.ce` 自己内部能完整表达，包括 stdio / io / sysio 三个 runtime 层。

### 1.2 三层职责严格分离

| 层 | 角色 | 谁写 |
| --- | --- | --- |
| **节点（Node）** | 纯接口声明（端口、出入符号） | stdio / io / sysio 包作者 |
| **边（Edge）** | 边内的点对点数据流实现（含 body） | 同上 |
| **拓扑块（Topology）** | 包的图结构索引，**无 body** | 同上 |

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

## 三、三大第一公民：节点 / 边 / 拓扑块

> 这是 Mocker 与一般命令式语言最大的区别 —— **图是第一公民**。

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

#### 3.1.3 例子

```ce
@Println {
    >> str msg            // 入符号：要打印的字符串
    fid  := 1             // 出符号：fd，默认 1（stdout）
    nl   := "\n"          // 局部常量
    msg+nl >>             // 出符号：编译期拼接 msg + 换行
    fid >>                // 出符号：fd
}
```

> 节点体**不含**控制流（`if` / `for` / `while`），过程性逻辑一律下沉到边体或 runtime。

### 3.2 边（Edge）

#### 3.2.1 语法

```ce
<src> <<edge_name>> <dst> {
    <body>
}
```

- `src` / `dst`：节点名（IDENT / CALL）
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

### 3.3 拓扑块（Topology Block）

#### 3.3.1 语法

```ce
<PkgName> {        ← 块名必须 == 当前 package 名
    <EdgeRef>*
}
```

`EdgeRef` = `src <edge_name> dst`（**复用 edge 语法，但无 body**）。

#### 3.3.2 例子

```ce
package stdio

@Println { ... }

Println <write> io.write {        // ← 边定义（结构 + 行为）
    Println.fid  >> io.write.fid
    Println.data >> io.write.data
}

stdio {                           // ← 拓扑块（仅结构）
    Println <write> io.write      //   复用 edge 语法，无 body
}
```

#### 3.3.3 拓扑块与边定义的关系

```
   拓扑块（结构层）                 top-level 边（行为层）
   ┌─────────────────┐           ┌──────────────────────┐
   │ stdio {         │   匹配    │ Println <write>      │
   │   Println       │ ←──────→  │   io.write { ... }   │
   │   <write>       │  (src,    │                      │
   │   io.write      │  edge,    │ body 写点对点走线    │
   │ }               │  dst)     │                      │
   └─────────────────┘           └──────────────────────┘
```

编译器按 `(src, edge_name, dst)` 三元组在 top-level 找对应边定义。

#### 3.3.4 拓扑块与 export 完全无关

| 维度 | 拓扑块 | @ 前缀 |
| --- | --- | --- |
| 决定 | 哪些边在包内"连起来" | 哪些节点对包外可见 |
| 服务 | 编译器分析（数据流图、路径追溯、死代码消除） | 模块系统（可见性） |
| 范围 | 包内 | 跨包 |

> 两者**完全正交**，互不干扰。

#### 3.3.5 编译器用法

1. **数据流分析**：沿拓扑条目建有向图
2. **路径追溯**：外部数据灌入 → Println → io.write → sysio.write → syscall
3. **死代码消除**：拓扑里没列的边 → 不强制 emit
4. **跨包递归**：拓扑条目里 dst 是 `io.write` / `sysio.write` 时，递归读那个包的拓扑块 + 边 body

### 3.4 三个构造 vs 三个抽象层

|  | 节点 | 边 | 拓扑块 |
| --- | --- | --- | --- |
| **定义在哪** | top-level | top-level | 块名 == 包名 |
| **有 body** | 是 | 是 | **否** |
| **含数据流** | 否（声明） | 是 | 否（结构） |
| **含端口声明** | 是 | 否 | 否 |
| **export 标记** | `@` 前缀 | N/A | N/A |

### 3.5 入口保留名：`main` 既是包名又是拓扑块

> **核心约定**：`main` 是 Mocker 的**保留名**（reserved name），由 dispatcher 强制处理。

#### 3.5.1 双层语义

| 出现位置 | 含义 |
| --- | --- |
| `package main` | **入口包**——这个文件是可执行程序的入口点（约定，等价于 Go 的 `package main`） |
| `main { ... }` | **入口拓扑**——程序启动时建立的初始图结构 |

**两件事统一在 `main` 这一个名字上**：包是 `main`，入口拓扑块也叫 `main`。

#### 3.5.2 dispatcher 行为

```
看到 IDENT == "main" 且后跟 {     →   永远走 parseTopologyDecl
                                    不会变成函数、struct、edge
```

所以 `package main` 的文件里写 `main { ... }` **不会被当成函数**，也不会有"函数名和包名冲突"的问题。

#### 3.5.3 例子

```ce
package main
import stdio

hello {                            // 节点（无入度，创建时自动执行）
    h := "hello world!"
    h >>
}

hello <out> say {                 // top-level 边定义（含 body，走线细节）
    hello.h >>
    >>say.hay
    >>say.my
    >>say.world
}

say {                             // 节点（3 个入度 port）
    >> str hey
      hey >> stdio.Println         // port-forward 糖
    >> str my
        my >> stdio.Println
    >> str world
        world >> stdio.Println
}

// ═══ 入口拓扑（main 保留名）═══
// 块体里描述程序启动时建立的连线。
// 编译器从这个块直接 emit 启动代码，**不再需要单独写 main 函数**。
main {
    hello <out> say                // 启动时建 hello→say 的 <out> 边
}
```

#### 3.5.4 编译器/运行时从这个块 emit 什么

```
1. 创建所有无入度节点（auto-exec）：
      hello 创建 → h 字段被赋值 → h 沿出符号流出

2. 沿 main 拓扑块建边：
      hello.h 沿 <out> 边流入 say

3. 触发有入度节点的 port：
      say.hey / say.my / say.world 各自激活 → 调 stdio.Println

4. 整体流程：
      hello(auto) → h → <out> → say → port → stdio.Println → ... → syscall
```

**整个启动是 declarative 的**——没有 imperative 步骤，全靠图论自动驱动。

#### 3.5.5 跟 Go 的对比

| | Go | Mocker |
| --- | --- | --- |
| 入口包名 | `package main` | `package main` |
| 入口函数 | `func main()` | `main { ... }` **拓扑块** |
| 入口体内容 | imperative 语句 | 声明性图结构（连线 + 拓扑） |
| 隐式 | 无 | hello 这种无入度节点创建时**自动执行** |

**哲学差异**：Go 的 main 是"程序从这里开始执行"，Mocker 的 main 是"程序从这张图开始存在"——你描述的是"哪些节点和边构成程序"，而不是"按顺序做什么"。

#### 3.5.6 块体里写什么

`main { ... }` 块体接受**和拓扑块同一种语法**——每行一条 `src <edge_name> dst` 形式的边引用，分号 `;` 可选：

```ce
main {
    hello <out> say
    hello <debug> say       // 同一对节点可以有多条边
    say <to_stdout> stdio.Println
}
```

**禁止的语法**（这些都不是启动时要建的边）：
- 节点声明（`name { ... }`）——节点要么从其他文件 import，要么通过包内拓扑建
- 边定义（带 body）——边 body 是"运行时的走线"，不是启动结构
- 函数声明——入口是拓扑，不是函数

#### 3.5.7 自举意义

`main` 约定的最大价值在**自举**：

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
| `main` | 入口保留名 | 走 TopologyDecl |

未来可能会加更多 reserved name（比如 `init` 用于"在 main 之前执行的初始化"），但当前只要 `main` 一个。

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

**关键点**：这是 **fixed compilation**（固定编译）—— 编译器直接 emit 转发代码，**不经过 EdgeDecl 的运行时边机制**（不开 goroutine、不走拓扑）。

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
| 是否进拓扑块 | 否（不进图） | 进拓扑块 |
| 能否做跨包链路分析 | 否 | 是 |

**所以糖写法适合**：调单参 utility（如 `Println` / `to_string` / 各种 transform），"数据来了就转一手走人"的场景。
**不适合**：多参数调用、需要进拓扑块做分析的场景。

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
Println <write> io.write {      // 显式边
    Println.fid  >> io.write.fid
    Println.data >> io.write.data
}

stdio {                         // 拓扑块列这条边
    Println <write> io.write
}
```

因为 `io.write` 有 2 个入端口，**必须显式边 + 拓扑**，不能用糖。

#### 3.5.8 糖写法使用 checklist

写新代码时，**先问自己 3 件事**：
1. 目标函数/节点是不是**只接 1 个参数**？
2. 数据源是不是**本节点已有的端口或变量**？
3. 类型是不是**对得上**？

3 个都是 YES → 用糖（`port >> target`）。
任何一个 NO → 用显式边（`port <edge> target { body }` + 拓扑块）。

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

### 6.3 与拓扑块的边界

| 概念 | 决定 | 实现机制 |
| --- | --- | --- |
| 节点是否包外可见 | `@` 前缀 | 编译期检查 |
| 边是否在包内"连起来" | 拓扑块条目 | 编译器分析 |

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
│   stdio { Println <write> io.write }                    │
└────────────────────┬─────────────────────────────────────┘
                     ▼
┌──────────────────────────────────────────────────────────┐
│ io 层（文件描述符抽象）                                  │
│   @write { >>num fid  >>byte data }                     │
│   （包内拓扑：io.write → sysio.write）                  │
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
| 控制流 | `if` / `else` / `return` |
| 字面量 | `true` / `false` |
| 类型 | `str` / `num` / `bool` / `byte` / `any` |
| 特殊 | `main`（保留作入口） |

### 9.2 操作符

| 类别 | 操作符 |
| --- | --- |
| 数据流 | `>>`（流到）/ `<<`（反流） |
| 边名 | `<` ... `>`（边名括号） |
| 赋值 | `:=`（声明） / `=`（暂未用） |
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
| `StructDecl` | `name { ... }` / `@name { ... }` | 节点（@ 前缀）也是 StructDecl |
| `EdgeDecl` | `src <edge> dst { ... }` | |
| `FuncDecl` | `main { ... }` | |
| `TopologyDecl` *（草案）* | `pkgname { edge-refs }` | 复用 EdgeDecl，只 body 为空 |

### 10.2 语句 Stmt

| 节点 | 例子 |
| --- | --- |
| `IfStmt` | `if cond { ... } else { ... }` |
| `ReturnStmt` | `return v` |
| `Connection` | `hello <out> say` |
| `FlowStmt` | `a >> b >> c`（单链） |
| `FlowCont` | `>>say.hay`（续行） |
| `FlowFanout` | `src >> + 多个 >>branches`（并发扇出） |
| `VarDecl` / `AssignStmt` | `h := "..."` / `a, b := expr` |
| `ExprStmtWrap` | 表达式当语句 |

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
| 图形化 vs 过程化 | 图形化 | 节点 / 边 / 拓扑块分立，无"先做 A 再做 B" |
| 出口标记 | `@` 前缀 | 不加新关键字，复用现有符号 |
| 拓扑块识别 | 块名 == 包名 | 不加新关键字，纯靠命名约定 |
| 拓扑块语法 | 复用 edge 语法 | 不引入新语法元素，零学习成本 |
| 三层架构 | sysio / io / stdio | 机制 / 抽象 / 语义清晰分离 |
| sysio 边界 | 编译器硬编码 emit | 单一可信源，简化跨平台 |
| fan-out 触发 | 连续 `>>` `>>` | 现有 token 就能表达，不加关键字 |
| AST 表达 fan-out | 专用 `FlowFanout` 节点 | 语义不混在 FlowStmt 里 |
| 数据流方向 | 左 → 右（`a >> b`） | 符合自然阅读顺序 |
| **port-forward sugar** | `port >> target`（单参）→ 编译期展开为隐式边 | 调用方写 1 行，编译器直接 emit 转发代码（不进拓扑块） |

---

## 十三、待办 / 未决

| 项 | 状态 | 备注 |
| --- | --- | --- |
| `TopologyDecl` AST 节点 | 草案，注释中 | 等用户拍板最后两个细节（跨包条目 + 不一致时报错 vs 警告） |
| `FlowBranch` AST 节点 | 草案，注释中 | fan-out 的分支节点 |
| 端口体（`>>str msg { ... }`） | 暂未支持 | 需要 lexer 支持 INDENT/DEDENT |
| `parseTopologyDecl` 真函数 | 未实现 | AST 节点落地后一并写 |
| 字符串字面量 `'...'` | 不支持 | lexer 只识别 `"..."` |
| 注释（`/* */` 块注释） | 已识别 | parser 会跳过 |
| **port-forward sugar 编译器识别** | 文档已写（§3.5），编译器侧未实现 | 目前是约定，靠同名 struct member 隐式连接；要真"fixed compilation"得在 IR 阶段加识别 |

---

> **下一步**：把 `TopologyDecl` / `FlowBranch` AST 节点和 `parseTopologyDecl` 函数一起实现，让 [example/stdio/stdio.ce](file:///home/wpp/homework/Mocker/example/stdio/stdio.ce) 末尾的 `stdio { Println <write> io.write }` 能 dump 出来。
