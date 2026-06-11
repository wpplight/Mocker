# Mocker 编译器 Roadmap

> 从当前状态到"编译出来一个能跑的 Go 二进制"，**还差什么 / 怎么排期 / 关键决策**
>
> 配套文档：
> - [Mocker架构设计.md](../../docs/Mocker架构设计.md) — 整体里程碑（M0–M10）
> - [ast_design.md](./ast_design.md) — AST 节点设计
> - [parser.md](../../docs/parser.md) — Parser 实现细节
> - [../../docs/language.md](../../docs/language.md) — 语言规范

---

## 〇、TL;DR

| 阶段 | M 编号 | 状态 | 预计工作量 |
| --- | --- | --- | --- |
| Lexer | M0 | ✅ 完成 | — |
| Parser + AST | M2 | ✅ 完成 | — |
| 文档 + 示例 | M9 | ✅ 完成（含 SYSCALL/Style2/多包/拓扑）| — |
| CLI dump（debug 工具） | — | ✅ 完成 | — |
| **Semantic（骨架）** | M3 | ✅ MVP + 跨包 + Style2 + SYSCALL + 类型推导 + 隐式初始化 | ✅ 完成 |
| **IR** | M4 上半 | ✅ 数据结构 + Lower + dump + AnalyzeTopology | ✅ 完成 |
| **Codegen** | M4 下半 | ❌ 未开始 | 3-5 天 |
| **Runtime（Go 写）** | M5 | ❌ 未开始 | 2-3 天 |
| **CLI build pipeline** | M6 | ❌ 未开始 | 1-2 天 |
| **端到端验证** | M7 | ❌ 未开始 | 1-2 天 |
| **CI / release** | M10 | ❌ 未开始 | 1 天 |

**MVP 路径：2 周**（12 个工作日）。**生产质量：4 周**（20 个工作日 + bug 修复 + 文档）。

---

## 一、当前状态（✅ 已完成）

### 1.1 词法层 M0 — `mocker_lex/`

```
mocker_lex/
├── lexer.go      # 手写 Lexer
├── lexer_test.go # 单元测试
└── lex.go        # 兼容 shim
```

**支持**：
- 关键字：`package` / `import` / `enum` / `if` / `else` / `return` / `true` / `false`
- 类型关键字：`str` / `num` / `bool` / `byte` / `any`
- 操作符：`>>` / `<<` / `:=` / `==` / `!=` / `<=` / `>=` / `&&` / `\|\|` / `+` / `-` / `*` / `/` / `.`
- 特殊：`@`（export） / `#`（注释） / `'`（注释，类型 1）
- 复合 token：CALL（`a.b.c`）/ EDGE_NAME（`out-no-co`）

### 1.2 Parser M2 — `internal/parser/`

```
internal/parser/
├── ast/ast.go        # AST 节点定义
├── parser.go         # Parser 框架 + helper
├── parse_file.go     # 顶层调度 + TopologyDecl
├── parse_decl.go     # StructDecl / EdgeDecl / TopologyDecl / typed-var
├── parse_flow.go     # FlowStmt / FlowCont / FlowFanout / FlowBranch / FlowExpr
├── parse_stmt.go     # Connection / IfStmt / ReturnStmt
├── parse_func.go      # FuncDecl
├── parse_expr.go     # Pratt 表达式
├── parse_type.go     # TypeRef
└── dump.go           # AST → 文本
```

**支持**（[language.md](../../docs/language.md) §三详细）：
- 节点 `@name { ... }` / `name { ... }`
- 边 `src <edge_name> dst { ... }`（dst 支持 IDENT 和 CALL 如 `io.write`）
- 拓扑块 `<pkgname> { <EdgeRef>* }`
- 入口保留名 `main { ... }`
- 三种数据流：单链 / 续行 / **fan-out 并发**
- 三种语法糖：port-forward / `msg+nl` 拼接 / 单 `=` 变量声明
- 跨包调用 `sysio.write(fid, data)` 解析为 FlowIdent + Call

### 1.3 5 个 example 全 pass

```
main.ce                   errors: 0    ← 入口拓扑
sysio.ce                  errors: 0    ← 编译终点
io.ce                     errors: 0    ← 文件抽象
stdio/stdio.ce            errors: 0    ← 用户接口（含拓扑块 + 糖）
stdio/to_string.ce        errors: 0    ← 简单节点
```

### 1.4 工具链

- `cmd/circle/main.go` — `circle -i file.ce` 打印 AST
- `cmd/tokdump/main.go` — 打印 token 流（debug 用）
- `Makefile` — `make build` / `make run` / `make test`

---

## 二、还差什么（按 M3→M7 顺序）

### 2.1 🟡 Semantic 语义分析（M3）— **MVP 骨架完成**

**目标**：把"语法对的 AST"变成"语义对的 IR 准备态"。

**新建目录** `internal/semantic/`（已就位）：

| 文件 | 职责 | 状态 |
| --- | --- | --- |
| `types.go` | Type 枚举、SemanticError、TypeRef 解析 | ✅ |
| `symbol.go` | SymbolTable / NodeSymbol / PortSymbol / FlowSymbol / EdgeKey | ✅ |
| `resolver.go` | `ResolveFile`：建符号表 | ✅ |
| `edge.go` | `CheckEdgeBody`：点对点类型 + port 存在性 + 类型匹配 | ✅ |
| `edge.go` | `ClassifyEdge`：body 含 FlowFanout → async（spawn goroutine） | ✅ |
| `topology.go` | `CheckTopology`：每个 entry 三元组必须在 EdgeDecl 里 | ✅ |
| `entry.go` | `FindEntryPoint`：package main + main{} 拓扑；`AnnotateEntryPoint` 标 sync/async | ✅ |
| `checker.go` | 主入口 `Check(file)` 调度 4 个检查 | ✅ |

**已实现的检查**：

| 检查 | 例子 | 报错 |
| --- | --- | --- |
| 未定义符号 | `Prinln.msg`（stdio.ce 真实拼错）| `port "Prinln.msg" not found on src "Println"` |
| 类型不匹配 | `str msg >> num fid` | `type mismatch: str.msg (str) → num.fid (num)` |
| 拓扑条目无实现 | 拓扑列了 `Println <write> io.write`，top-level 没 | `topology entry has no matching edge declaration` |
| 边 body 引用不存在的 port | `say.hay`（main.ce 真实拼错，应是 `say.hey`）| `port "hay" not found on node "say"` |
| goroutine 决策 | `hello <out> say` body 含 `>>` `>>` 三分支 | async：codegen 时**每分支一个 goroutine** |
| 入口点 | `package main` 的 `main { ... }` | entry point 已识别，indegree=0 节点标为 **auto-exec** |

**MVP 阶段没做的**（待后续补 / 已补）：

- ✅ 跨包 import 实际加载（`import stdio` → 读 `stdio/stdio.ce`）— Task B 已补，workspace BFS
- ✅ 跨包类型检查（`stdio.Println` 是否真存在）— Task B 已补，stdio 引用 io.write 现在 OK
- ❌ 完整类型推导（只检查显式 `>> type name` 端口的字面量类型）— 验证：`>> num msg` 仍通过
- ⚠️ port body 校验 — 设计里没 port body 概念，**从 list 删除**
- ❌ 隐式初始化检查（`msg+nl` 拼接糖的语义验证）— 验证：`msg + 999`（str+num）仍通过

**关键设计点**（用户拍板）：

```
Goroutine 决策权在边：
   - 边 body 含 FlowFanout（>> >>）→ async，每条分支一个 goroutine
   - 边 body 只有 FlowStmt/FlowCont（普通 >>）→ sync，函数调用形式

入口约定：
   - package main 才是入口
   - main { ... } 拓扑块描述启动序列
   - topology 里 indegree=0 节点 → auto-exec（创建时自动执行）
```

**已知 parser 局限导致 semantic 误报**：

`example/main.ce` 里：
```
say {
    >> str hey
      hey >> stdio.Println
    >>str my
        my >> stdio.Println
    ...
}
```

第二行 `hey >> stdio.Println` 的 chain 把下一行的 `>>` 误吃，导致 `>> str my` 被错误地解析成 `FieldDecl str my`（不是 PortDecl）。结果是 `say` 节点的符号表里只有 `hey` 端口，缺 `my` / `world`。

修复方法：parser 需要在 `parseFlowChain` 里加**行号追踪**，知道 `>>` 是同一行续行还是新行起始。**MVP 暂接受这个误报**——等 parser 改完，semantic 也会变干净。

**已实现的入口点分析输出**（`circle -entry -i example/main.ce`）：

```
=== EntryPoint ===
EntryPoint: package main, 2 nodes, 1 edges, 1 auto-exec
  all nodes: [hello say]
  auto-exec: [hello]                    ← 无入度 → 启动时执行
  sync edges (0): []
  async edges (1, spawn goroutines): [hello <out> say]   ← 含 fan-out → async
```

这条信息会传给 IR 阶段：
- `hello` codegen 时开一个 goroutine（auto-exec）
- `hello <out> say` 边 codegen 时给 3 个分支各开一个 goroutine

**剩 1-2 天的跨包工作**（单文件 MVP 已可工作）：

1. `internal/semantic/loader.go` — 跨包 import 加载（读 `import sysio` 指向的 .ce 文件）
2. `CheckResult` 加 `imports map[string]*SymbolTable` 字段
3. `Check(file, loader)` 多加一个参数，让 checker 能跨包查符号

预计 1-2 天。

---

### 2.2 ❌ IR 中间表示（M4 上半）— 关键

**目标**：把 AST 降到"代码生成器友好"的形态。

**新建目录** `internal/ir/`：

| 文件 | 职责 |
| --- | --- |
| `ir.go` | IR 节点定义：`IRNode` / `IRGraph` / `IRNode` / `IREdge` / `IRPort` |
| `graph.go` | 把拓扑块 + EdgeDecl 转为运行时图（节点列表 + 边列表 + port 列表）|
| `lower.go` | `AST → IR` 的主入口 |
| `optimize.go` | 死代码消除（拓扑没引用的节点不 emit） / inline / 常量传播 |

**IR 核心模型**（建议）：

```go
// IRGraph 程序 = 图
type IRGraph struct {
    Nodes []*IRNode      // 所有节点（按拓扑入口顺序）
    Edges []*IREdge      // 所有边（拓扑块列出 + top-level 定义）
    Entrypoints []string // 入口节点（无入度的）
}

type IRNode struct {
    Name     string
    IsExported bool
    Ports    []*IRPort     // 入符号（数据从外部流入这里）
    Flows    []*IRFlow     // 出符号（数据从这里流到外部）
    AutoExec bool          // 无入度 → 创建时自动执行（goroutine）
}

type IREdge struct {
    Src     string
    Dst     string
    Body    []IRStmt      // 从 src.port 走到 dst.port 的具体步骤
}

type IRPort struct {
    Name string
    Type string
}
```

**核心决策**（需要你拍板）：

| 决策 | 选项 | 影响 |
| --- | --- | --- |
| **goroutine 粒度** | A) 每无入度节点一个 / B) 每 `>>` 一步一个 / C) 完全单线程 | 决定 codegen 风格 + 性能 |
| **port 触发并发** | A) 同步 / B) 异步（每个 data 一个 goroutine） | 决定运行时复杂度 |
| **拓扑 = 定义 vs 启动** | A) 仅定义（runtime 自己 topological sort）/ B) 启动序列（按顺序） | 决定 main 块的语义 |

**预计 2-3 天**。

---

### 2.3 ❌ Codegen 代码生成（M4 下半）— 关键

**目标**：把 IR emit 成 Go 源码，编译成可执行二进制。

**新建目录** `internal/codegen/`：

| 文件 | 职责 |
| --- | --- |
| `gen.go` | `IR → Go 源码` 主入口 |
| `emit.go` | 写文件、调用 `go build`、产物路径管理 |
| `syscall_table.go` | `sysio.*` → 平台 syscall 的映射（硬编码或表驱动）|
| `template/runtime.go.tmpl` | 节点 runtime 模板（goroutine + port 接收 + flow 触发）|
| `template/main.go.tmpl` | 程序入口模板（从 `main { ... }` 拓扑 emit）|

**sysio 边界 emit**（关键！自举友好）：

```go
// sysio.write(fid, data) → 直接 emit syscall 调用
// 必须在 codegen 里硬编码映射，不走 Go runtime
var SyscallTable = map[string]map[string]string{
    "write": {
        "linux":   "syscall.Write(int(fd), data)",
        "darwin":  "syscall.Write(int(fd), data)",
        "windows": "windows.WriteFile(...)",
    },
    "read":  { "linux": "syscall.Read(...)",  ... },
    "exit":  { "linux": "syscall.Exit(int(code))", ... },
    "fork":  { "linux": "runtime.GoFork(fn)",  ... },
}
```

**自举约束**（[language.md §11.3](../../docs/language.md)）：
- ❌ 不能用 `cgo` / `unsafe` / `reflect`（依赖 Go runtime，自举就断）
- ✅ 只能用 `syscall.RawSyscall` / 标准库
- ✅ 节点 / 边 runtime 可以用 `goroutine` / `channel`

**预计 3-5 天**。

---

### 2.4 ❌ Runtime 运行时库（M5）— 关键

**目标**：让 stdio / io / sysio 真正能跑起来（先用 Go 写，自举时再换成 .ce）。

**新建目录** `internal/runtime/`：

```
internal/runtime/
├── sysio/
│   ├── sysio.go              # sysio.write / read / exit 的 Go 实现
│   ├── syscall_linux.go      # Linux 平台
│   ├── syscall_darwin.go     # macOS 平台
│   └── syscall_windows.go    # Windows 平台
├── io/
│   └── io.go                 # io.write(fid, data) → sysio.write
└── stdio/
    └── stdio.go              # stdio.Println(msg) → io.write(1, msg+"\n")
```

**自举路径**（分阶段）：

| 阶段 | sysio | io | stdio | 可不可自举 |
| --- | --- | --- | --- | --- |
| **M5 bootstrap** | Go | Go | Go | ❌ |
| **M5 收尾** | Go（边界）| .ce | .ce | ❌（sysio 仍 Go） |
| **自举时** | Go（最终） | .ce | .ce | ❌（sysio 永远是 Go） |

**预计 2-3 天**。

---

### 2.5 ❌ CLI build pipeline（M6）

**目标**：在 `cmd/circle/main.go` 加 `build` 子命令。

**当前**：`circle -i file.ce` 只跑 parse → dump AST

**新加**：

```bash
circle build -i main.ce -o ./mymock      # 完整 pipeline → 二进制
circle build -i main.ce -o ./mymock -v   # verbose，打印每阶段
circle check -i main.ce                 # 只跑 parse + semantic
circle dump-ir  -i main.ce              # parse + semantic + IR + dump
circle dump-go -i main.ce               # parse + semantic + IR + codegen + dump
```

**`build` 内部流程**：

```
[1] parse .ce files              (parser)
[2] resolve imports             (semantic)
[3] check semantics             (semantic)
[4] lower to IR                 (ir)
[5] emit Go source              (codegen)
[6] write to temp dir           (emit)
[7] go build temp/...           (exec.Command)
[8] move binary to -o path     (emit)
[9] cleanup temp dir            (emit)
```

**预计 1-2 天**。

---

### 2.6 ❌ 端到端验证（M7）

**目标**：`./mymock` 能跑，打印 "hello world!"。

**E2E 测试**：

```bash
# 编译
$ circle build -i main.ce -o ./mymock
$ ls -la ./mymock
-rwxr-xr-x  1 user  user  8.4M  ./mymock    ← Linux/macOS 二进制

# 跑
$ ./mymock
hello world!    ← 来自 hello <out> say 的 fan-out 分支 hay
hello world!    ← 来自分支 my
hello world!    ← 来自分支 world
```

**测试套件**：

```
internal/e2e/
├── hello_world_test.go    # 最简单：单节点 → Println
├── fanout_test.go         # 验证并发分支都触发
├── cross_pkg_test.go      # main 引用 stdio，确认 import resolution
└── syscall_test.go        # sysio.write 真的写到 fd 1
```

**预计 1-2 天**。

---

## 三、关键设计决策（需要你拍板）

按重要性排：

### 决策 1：goroutine 粒度

| 选项 | 描述 | 优 | 劣 |
| --- | --- | --- | --- |
| **A** 每无入度节点 1 个 | `hello` 是入口节点 → 一个 goroutine；内部串行 | 简单、port 触发确定性 | 节点多时 goroutine 多 |
| **B** 每 `>>` 1 步 | `a >> b >> c` 拆 3 个 goroutine | 真正并行 | 调度复杂，调试难 |
| **C** 完全单线程 | 用 `select` 模拟 | 最简 codegen | 失去真正的并行 |

**推荐 A**——契合"无入度节点 = 启动源"的语义，codegen 也最自然。

### 决策 2：port 触发的并发

```ce
say {
    >> str hey     ← port 收到数据时执行 body
      hey >> stdio.Println
}
```

| 选项 | 描述 |
| --- | --- |
| **A** 同步 | port body 同步执行；一个 port 一次只能处理一个 data |
| **B** 异步 | port body 开新 goroutine；每个 data 一个独立 goroutine |

**推荐 B**——port 收到数据时开新 goroutine，和 fan-out 的"每分支独立执行"一致。

### 决策 3：sysio 平台映射

| 选项 | 描述 | 优 | 劣 |
| --- | --- | --- | --- |
| **A** 硬编码 | `if name == "write" { emit syscall.Write(...) }` | 简单 | 加平台要改 codegen |
| **B** 表驱动 | `map[name]map[platform]string` | 易扩展 | 表大、调试不直观 |
| **C** 配置文件 | JSON/YAML | 不重编译就能加 | 多一层间接 |

**推荐 B**——加平台只加表项，不动 codegen 逻辑。

### 决策 4：包加载

| 选项 | 描述 | 优 | 劣 |
| --- | --- | --- | --- |
| **A** 显式路径 | `circle build -i main.ce -I ./stdlib` | 简单，可控 | 用户要记 -I |
| **B** 环境变量 | `MOCKER_PATH=./stdlib` 自动找 | 少打参数 | 隐式 |
| **C** 锁定文件 | `Mocker.lock` 记录依赖 | 可重现 | 复杂 |

**推荐 A → B**——MVP 用 A，加环境变量支持是 5 分钟的事。

### 决策 5：错误恢复

| 选项 | 描述 | 适合 |
| --- | --- | --- |
| **A** fail-fast | 第一个错就停 | 开发 |
| **B** 全报告 | 扫完一遍报所有错 | CI / AI |
| **C** 两种都支持 | `-fail-fast` 标志切换 | 通用 |

**推荐 C**——开发用 A 快速定位，CI 用 B 一次看完。

---

## 四、推荐实施路径

### 4.1 MVP（2 周 / 12 工作日）

```
第 1 周：semantic 骨架 + IR 设计 + 跑通一个 hello world
┌──────────────────────────────────────────────────────────┐
│ Day 1-2: semantic/symbol.go + resolve.go 骨架            │
│          跑通 stdio.Println 跨包解析                        │
│                                                          │
│ Day 3-4: semantic/typeck.go + validate.go               │
│          报"Prinln 未定义" / "msg 类型不匹配"               │
│                                                          │
│ Day 5:   ir/ir.go + graph.go + lower.go 骨架              │
│          把拓扑块 + 边定义 → 运行时图                       │
└──────────────────────────────────────────────────────────┘
                              ↓
第 2 周：codegen + runtime + CLI build + E2E
┌──────────────────────────────────────────────────────────┐
│ Day 6-7: codegen/gen.go                                   │
│          emit Go 源码（runtime + main entry）              │
│                                                          │
│ Day 8-9: runtime/sysio + runtime/io + runtime/stdio      │
│          (Go 实现，syscall.Write 等)                      │
│                                                          │
│ Day 10:  CLI build pipeline (cmd/circle)                  │
│          parse → semantic → IR → codegen → go build        │
│                                                          │
│ Day 11-12: E2E 测试                                      │
│          hello world!、fan-out、跨包、syscall 4 个 case     │
└──────────────────────────────────────────────────────────┘
```

**第一个里程碑**：`./mymock` 能跑，打印 3 行 "hello world!"。

### 4.2 生产质量（再加 2 周）

```
第 3 周：打磨
  - 跨包错误信息更友好（"stdio.Println 在 stdio/stdio.ce 第 21 行"）
  - 死代码消除（拓扑没引用的节点不 emit）
  - 优化（inline、合并同类型相邻流）
  - 更多 example 覆盖（netio/、test cases）

第 4 周：CI + release
  - GitHub Actions（每次 push 跑测试 + 跨平台 build）
  - 预编译二进制发布到 GitHub Releases
  - 安装脚本 `curl -sSL ... | sh`
```

---

## 五、立即可做的下一步

按 ROI 排序：

| 顺序 | 任务 | 价值 | 工作量 |
| --- | --- | --- | --- |
| **1** | 拍板 §三 决策 1-5 | 决定 IR/codegen 方向，省得返工 | 1-2 小时 |
| **2** | 写 `internal/semantic/symbol.go` 骨架 | 后续 IR/codegen 都依赖它 | 1 天 |
| **3** | 写 `internal/semantic/resolve.go` 跨包解析 | 让 `stdio.Println` 能查到 Println 定义 | 1 天 |
| **4** | 写 `internal/semantic/typeck.go` 类型检查 | 抓出 `str >> num` 之类错误 | 1-2 天 |
| **5** | 写 `internal/ir/` 骨架 | semantic 完成后才能跑 IR | 2-3 天 |
| **6** | 写 `internal/codegen/syscall_table.go` | sysio 边界先定好 | 0.5 天 |
| **7** | 写 `internal/runtime/sysio/` | 没有它啥都跑不起来 | 1-2 天 |

**建议从决策 1-5 拍板开始**——这 5 个决策直接影响 IR/codegen 设计，先定下来省得后面返工。

---

## 六、相关链接

- [Mocker架构设计.md](../../docs/Mocker架构设计.md) — M0–M10 整体里程碑
- [language.md](../../docs/language.md) — 语言规范
- [ast_design.md](./ast_design.md) — AST 节点设计
- [parser.md](../../docs/parser.md) — Parser 实现指南
- [example/](../example/) — 5 个 .ce 示例
