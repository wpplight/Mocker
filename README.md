# Mocker — 节点即函数的 DSL 编译器

> 把"节点+边"画成图，让编译器**自动生成构造函数编排代码**。

Mocker 是一种面向 **API 编排 / 工作流 / 数据管道** 的小语言。开发者用类似图的语法声明**节点**（带 sub-graph 的状态机）和**边**（数据流），编译器把整张图编译成一个 Go 二进制；运行时按拓扑序调用每个节点的构造函数，并把上游产出的数据按声明连线喂给下游。

```
hello {                           <add_str> {
    h := "hello"                      hello.h  >> world.words
    world w                           world.new >> hello.out_str
    <add_str> w                  }
    >>str out_str             main { hello happy; }
    stdio.Println p;
    out_str >> p.msg;        }
```

↑ 这三段是 `../example/main.ce`。编译后 `hello.h → world.words → world.new → hello.out_str → p.msg` 的数据流被**自动串起来**，不需要手写 `setup`/`wire`。

---

## ✨ 主要特性

- **节点即函数**：每个节点是一个带状态 + 端口的实体，body 内部可以挂 **sub-instance**（实例化其它节点）和 **sub-edge**（局部边），编译器自动 emit 构造函数。
- **拓扑驱动**：跨节点数据流写 `<add_str> w` 一次声明，编译器推出**正向 / 反向 / 跨包**三向连线，运行时**严格拓扑序**调用，**无环**自动检测。
- **sub-graph 嵌套**：节点 body 内部可以嵌子图，无需把所有逻辑摊平到顶层。
- **隐式 SubEdge 推断**：`world w` 后面跟 `<add_str> w` → 编译器补全 `w` 的端口。
- **多包工作区**：BFS 加载 `import` 链路，自动跨包解析（stdio / io / netio / 业务包）。
- **调试中间文件**：`circle build -debug` 把 AST / 符号表 / IR / d2 图 / 生成 Go 一并落到 `./debug/`。
- **可视化 IDE**：`Mocker Studio`（基于 wails v2 + React + React Flow），双击图节点跳到源码、点边查定义/流动、跨包节点默认折叠。

---

## 📦 仓库结构

```
.
├── cmd/
│   ├── circle/          # CLI 编译器入口（circle build …）
│   ├── mocker-studio/   # wails v2 GUI（前端在 frontend/）
│   ├── test_graph/      # 跑一次 OpenWorkspace/ReparseWorkspace 打 GraphData，验证 IDE 序列化
│   └── tokdump/         # 词法 token dump 工具
├── internal/
│   ├── parser/          # Lexer + Pratt Parser + AST
│   ├── semantic/        # 跨包符号表 + 类型检查
│   ├── ir/              # 中间表示 + Lower + 图分析
│   ├── codegen/         # Go 源码 emit + 子进程 go build
│   ├── d2gen/           # d2 图（调试 + SVG 导出）
│   ├── ide/             # Mocker Studio 用的 Workspace API（OpenWorkspace / ReparseWorkspace / LocateNode …）
│   └── circledebug/     # debug dumper（写 ./debug/00-…06-…）
├── docs/                # 设计文档（roadmap / AST / 编排器 / 语言规范 / IR）
├── example/             # 最小可运行示例（main.ce / stdio / io / netio / cookie / debug）
├── mocker_lex/          # 老 lexer（仅保留给单测）
└── Makefile             # 编译 / 测试 / 跑示例
```

---

## 🚀 快速开始

### 编译 CLI

```bash
make build           # → release/circle-linux-amd64
make build-all       # 跨平台（linux / darwin / windows）
make install         # 装到 /usr/local/bin/circle
```

### 跑一个最小示例

```bash
cd example
../release/circle-linux-amd64 build -o ./hello
./hello
```

### 输出调试中间文件

```bash
../release/circle-linux-amd64 build -debug            # 写到 ./debug/
../release/circle-linux-amd64 -debug build            # 同样可以（flag 顺序无关）
../release/circle-linux-amd64 build -debug -debug-dir /tmp/mocker-debug
```

落盘：

| 文件 | 内容 |
| --- | --- |
| `00-emit-go.go` | 生成的 Go 源码（同步 `-emit-go` 行为） |
| `01-ast.txt` | 每个包的 AST dump |
| `02-semantic.txt` | 符号表 + 错误 + 入口点 |
| `03-ir.txt` | IR Program dump |
| `04-graph.txt` | IR Graph dump |
| `05-graph.d2` | d2 图源 |
| `06-graph.svg` | d2 渲染的 SVG（需要 `d2` CLI） |

### 跑测试

```bash
make test            # 所有单测
make test-lexer      # 只测 lexer
make test-parser     # 只测 parser
```

### 启动 Mocker Studio（可视化 IDE）

需要：Node.js 18+、GTK3 / WebKit2GTK（Linux）、`wails` v2 CLI。

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1

make build-studio            # → release/circle-studio-linux-amd64
./release/circle-studio-linux-amd64 ../example    # 打开 example 工作区

# 或者 dev 模式（热重载）
make dev-studio
```

Studio 主要交互：

- **左栏**：文件树 + 节点 + 边 + enum
- **中栏**：图（绿色 = 节点变量生命周期，蓝色 = 数据流/块调用，浅蓝 = 拓扑边）
- **右栏**：属性面板（点节点看 IR + 源码位置，点边看流动方向）
- **底部**：诊断 / 输出
- **双击图节点** → 在 split 编辑器中跳到对应 struct 定义
- **双击包容器** → 展开/折叠该包

---

## 🔧 `circle build` 标志

| 标志 | 说明 |
| --- | --- |
| `-o <path>` | 输出二进制路径（默认 `./mymock`） |
| `-keep-tmp` | 保留临时目录（看生成的 `main.go`） |
| `-temp-dir <dir>` | 指定临时目录路径（默认 `os.MkdirTemp`） |
| `-emit-go <file>` | 额外把生成的 Go 源码写到该文件 |
| `-run` | 编译后直接运行二进制 |
| `-run-args "<args>"` | 运行时参数 |
| `-debug` | 输出 AST / 符号表 / IR / d2 到 `./debug/` |
| `-debug-dir <dir>` | 调试输出目录（默认 `./debug`） |

`-debug` / `-debug-dir` / `-h` 顺序无关：`circle -debug build`、`circle build -debug` 等价。

---

## 🧬 编译器流水线

```
.ce source
  │
  ▼
┌──────────────┐
│  Lexer       │  tokens（IDENT / STRING / NUM / >> / := …）
└──────┬───────┘
       ▼
┌──────────────┐
│  Pratt       │  AST（StructDecl / EdgeDecl / Stmt …）
│  Parser      │
└──────┬───────┘
       ▼
┌──────────────┐
│  Semantic    │  跨包符号表 + 类型推导 + 隐式初始化
│  (BFS)       │  → wresult.Errors / Tables
└──────┬───────┘
       ▼
┌──────────────┐
│  IR Lower    │  IRProgram：节点 + body + 端口 + sub-instance
└──────┬───────┘
       ▼
┌──────────────┐
│  IR Graph    │  AnalyzeTopology + 跨包边
│  BuildGraph  │
└──────┬───────┘
       ▼
┌──────────────┐
│  Codegen     │  → 生成的 main.go
│  (Go emit)   │
└──────┬───────┘
       ▼
  go build → 可执行二进制
```

整条流水线在 [`internal/`](./internal) 里按 stage 拆开，每个 stage 都暴露 dump 函数，可单独调用。

---

## 📐 语言速览

完整规范见 [`docs/ast_design.md`](./docs/ast_design.md) / [`docs/execution.md`](./docs/execution.md)。下面是最常用的语法：

```ce
package main

import stdio

// 节点 hello
hello {
    h := "hello"            // 局部状态（var）
    world w                 // sub-instance：实例化 world
    <add_str> w             // sub-edge：world 走 add_str 边
    >>str out_str           // 输出端口 out_str
    stdio.Println p;        // sub-instance 跨包节点
    out_str >> p.msg;       // flow：本节点 out_str → p.msg
}

// 节点 world
world {
    >> str words            // 输入端口 words
    new := words
    new >>
}

// 顶层 edge：h 流向 words
<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

// 入口（必须叫 main）
main {
    hello happy;
}
```

---

## 🛠️ Mocker Studio 后端 API（`internal/ide`）

| 方法 | 用途 |
| --- | --- |
| `OpenWorkspace(root)` | 扫工作区 + BFS 加载 + 语义 + IR + GraphData |
| `ReparseWorkspace(path, src)` | 编辑器改代码后写盘 + 重新出图（保持与启动一致） |
| `ParseFile(path)` | 解析单文件（编辑器反馈） |
| `ParseSource(src)` | 解析裸源码（无 workspace） |
| `LoadFile(path)` / `SaveFile(path, content)` | 读/写文件 |
| `LocateNode(qualifiedName)` | 找 struct 在哪个文件、哪一行（双击跳源用） |
| `Compile(opts)` | 编译 workspace |
| `Run(opts)` | 编译并跑 |
| `InspectNode(name)` | 节点 IR 状态（M2 动态分析） |
| `ApplyEdit(edit)` / `SerializeToSource` | 拖拽编辑 |

返回的 `GraphData` 给前端的结构：

```go
type GraphData struct {
    Nodes    []FlowNode         // 含 Data.members（vars / fields / sub-instance / flow …）
    Edges    []FlowEdge         // 含 Data.kind = "lifecycle" | "dataflow" | "flow" | "topology"
    Packages []PackageInfo      // 含 DefaultCollapsed、BoundaryNodeIds
}
```

---

## 🧪 验证脚本

```bash
go run ./cmd/test_graph
# 跑 OpenWorkspace + ReparseWorkspace + LocateNode，打印 GraphData
# （验证 IDE 拿到的数据格式和 CLI 内部一致）
```

---

## 📚 配套文档

- [`docs/roadmap.md`](./docs/roadmap.md) — M0 → M2 完成度 + 后续排期
- [`docs/ast_design.md`](./docs/ast_design.md) — AST 节点设计
- [`docs/execution.md`](./docs/execution.md) — 构造函数编排器
- [`docs/constructor-orchestrator-design.md`](./docs/constructor-orchestrator-design.md) — 编排器设计

---

## 🤝 贡献

- 所有单测通过 + `go vet ./...` 无警告后再开 PR
- 新增 AST / IR 节点时同步更新 `docs/`
- Mocker Studio 前端改动先 `npx tsc --noEmit` 后再 `wails generate module`
