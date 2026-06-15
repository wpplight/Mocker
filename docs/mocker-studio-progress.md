# Mocker Studio 进度文档

> 本文档追踪 [mocker-studio-design.md](./mocker-studio-design.md) 的执行进度、当前状态、已知问题与下一步计划。
>
> 最后更新：M0 + M0.5 + M0.6 + M0.7 + M0.8 全部完成

---

## 〇、目录

1. [当前状态速览](#一当前状态速览)
2. [已完成的里程碑](#二已完成的里程碑)
3. [M0 产出文件清单](#三m0-产出文件清单)
4. [当前对接链路](#四当前对接链路)
5. [已知问题与阻塞](#五已知问题与阻塞)
6. [下一步计划](#六下一步计划)
7. [待决策事项](#七待决策事项)

---

## 一、当前状态速览

| 维度 | 状态 |
| --- | --- |
| Go 后端 service 层（`circle/internal/ide/`） | ✅ **8 个文件 / 1440 行** 全部完成 |
| GUI 入口（`circle/cmd/mocker-studio/main.go`） | ✅ **92 行** wails3 alpha 启动 |
| 前端（React Flow + Monaco + Zustand） | ✅ 2000 行已从 studio 复制 |
| `make build` 出 `circle`（CLI） | ✅ **25 MB** 二进制通过 |
| `make build-studio` 出 `circle-studio`（GUI） | ⚠️ **15 MB** 编译过但运行时**前端无数据** |
| `circle CLI build ../example` | ✅ 编译出 `helloworld!world!world!` |
| 工作区文件载入 / IR 图 / 实时解析 | ✅ **wails v2 bindings 自动生成，前端拿到真实数据** |
| 拖拽编辑 ApplyEdit / SerializeToSource | ⏳ M1 占位，stub 报错 |
| **wails 框架** | ✅ **v3 alpha → v2.10.1 stable 迁移完成** |

---

## 二、已完成的里程碑

### M0：核心骨架（✅ 完成）

- 8 个 Go 文件齐全（service / types / parse / graph / compile / emit_json / patch / workspace）
- 16 个 wails binding 方法（`GetWorkspace` / `OpenWorkspace` / `LoadFile` / `SaveFile` / `ParseSource` / `BuildGraph` / `Compile` / `Run` / `ApplyEdit` / `SerializeToSource` / `SetContext` / `SetWorkspace` / `Version` / 等）
- 复用 `parser.Parse` / `semantic.Check` / `ir.Lower` / `ir.BuildGraph` / `codegen.EmitGoFromIR` / `codegen.BuildWithOptions`
- 前端 9 个组件 + Zustand store + Monaco + React Flow 全部就位
- Makefile `build` + `build-studio` 两个 target

### M0.5：工作区接入（✅ 完成）

- ✅ `ide.Service.OpenWorkspace(root)` 接受命令行参数
- ✅ `LoadFile` / `SaveFile` 读 .ce 文件
- ✅ `ParseSource` 编辑器输入实时解析
- ✅ Sidebar "Files" 文件树 + 点击切换
- ✅ Toolbar Save / Compile / Run 按钮
- ✅ OutputPanel 显示诊断 + 流式 stdout
- ✅ 启动时 `GetWorkspace` + `OpenWorkspace` 取数据驱动 UI

### M0.6：wails v2 迁移（✅ 完成）

- ✅ 移除 wails3 alpha 依赖（`go.mod` 切到 `wails/v2 v2.10.1`）
- ✅ 重写 `main.go` 用 wails v2 `wails.Run(&options.App{...})`
- ✅ 装 wails v2 CLI 2.10.1 + 创建 `wails.json` 描述工程
- ✅ `wails generate module` 自动生成 `frontend/wailsjs/go/ide/Service.{js,d.ts}` + `models.ts`
- ✅ `service.ts` 改成 re-export 自动生成的强类型 binding（**不再手写 FNV-1a 哈希**）
- ✅ `store/editor.ts` 类型从 wails 生成的 `models.ts` 引入（去掉重复定义）
- ✅ `Toolbar.tsx` 补全 `CompileOptions` 必填字段
- ✅ Makefile 用 `-tags production,webkit2_41` 编译（适配 Ubuntu 24.04 的 webkit2gtk-4.1）
- ✅ 装系统依赖 `libwebkit2gtk-4.1-dev`
- ✅ `make build-studio` 出 9.6MB 二进制（之前 wails3 alpha 编译 15MB + 运行时无数据）

**收益**：
| 项 | wails3 alpha（迁移前） | wails v2 stable（迁移后）|
| --- | --- | --- |
| `window.go.ide.Service.*` 绑定 | ❌ 手写哈希 | ✅ 自动生成 |
| 工具链 | alpha 不可用 | wails dev / build |
| 调试 | 看不了 | devtools 完整 |
| 文档 | 缺失 | 完整 |
| 编译产物 | 15 MB（无数据）| 9.6 MB（数据通）|

### M0.7：Linux 黑屏修复（✅ 完成）

- ✅ `main.go` 启动时自动设 Go/JSC/WebKit 兼容 env var
- ✅ 新增 `release/run-studio.sh` 包装脚本
- ✅ 前端 `ErrorBoundary` + 全局 `window.error` 监听
- ✅ 解决"另一台设备闪一下就黑屏"问题

### M0.8：null slice 致 React 崩溃修复（✅ 完成）

- ✅ Go 端 `ParsedFile` 全部 slice 字段初始化成 `[]T{}` 而非 nil
- ✅ 前端 `parsed.X ?? []` 防御
- ✅ 解决 ErrorBoundary 抓到的 `t.imports.length` 报错

---

## 三、M0 产出文件清单

### Go 后端（1440 行）

| 文件 | 行数 | 职责 |
| --- | --- | --- |
| [circle/internal/ide/service.go](../circle/internal/ide/service.go) | 57 | Service struct + 构造 + Version |
| [circle/internal/ide/types.go](../circle/internal/ide/types.go) | 243 | JSON 类型：ParsedFile / GraphData / Edit / CompileResult / WorkspaceInfo / IRNodeState / RunEvent |
| [circle/internal/ide/parse.go](../circle/internal/ide/parse.go) | 284 | ParseSource / ParseWorkspace / ParseFile → AST + 符号表 JSON |
| [circle/internal/ide/graph.go](../circle/internal/ide/graph.go) | 52 | BuildGraph → IRGraph → React Flow GraphData |
| [circle/internal/ide/workspace.go](../circle/internal/ide/workspace.go) | 327 | OpenWorkspace / LoadFile / SaveFile / workspaceToParsedFile / irGraphToGraphData |
| [circle/internal/ide/compile.go](../circle/internal/ide/compile.go) | 239 | Compile + 流式 Run |
| [circle/internal/ide/emit_json.go](../circle/internal/ide/emit_json.go) | 221 | AST / IR → JSON helper（避免暴露内部结构）|
| [circle/internal/ide/patch.go](../circle/internal/ide/patch.go) | 17 | ApplyEdit / SerializeToSource（M1 stub）|

### GUI 入口

| 文件 | 行数 | 职责 |
| --- | --- | --- |
| [circle/cmd/mocker-studio/main.go](../circle/cmd/mocker-studio/main.go) | 92 | wails3 app + 挂载 ide.Service + embed 前端 |
| [circle/cmd/mocker-studio/frontend/](../circle/cmd/mocker-studio/frontend/) | ~2000 | 复制的 React + React Flow + Monaco 前端 |

### 前端绑定（手写）

| 文件 | 行数 | 职责 |
| --- | --- | --- |
| [circle/cmd/mocker-studio/frontend/src/lib/service.ts](../circle/cmd/mocker-studio/frontend/src/lib/service.ts) | 100 | 手写 15 个方法的 FNV-1a 哈希派发（wails3 alpha 兜底）|
| [circle/cmd/mocker-studio/frontend/bindings/circle/internal/ide/Service.js](../circle/cmd/mocker-studio/frontend/bindings/circle/internal/ide/Service.js) | 80 | 同上 .js 版本（备份）|

### 文档

| 文件 | 行数 | 职责 |
| --- | --- | --- |
| [docs/mocker-studio-design.md](./mocker-studio-design.md) | 200+ | 整体设计（M0/M1/M2 路线）|
| [docs/mocker-studio-progress.md](./mocker-studio-progress.md) | 本文档 | 进度追踪 |

---

## 四、当前对接链路

### 启动时序

```
用户运行：
  $ ./release/circle-studio-linux-amd64 ../example
       │
       ▼
[Go main.go]
   os.Args[1] = "../example"
   ide.NewService("../example")
   application.New(...)
   app.Window.NewWithOptions(...)
   app.Run()
       │
       ▼
[wails3 webview 启动，载入 frontend/dist/index.html]
       │
       ▼
[React App.tsx useEffect]
   svc.GetWorkspace()      → 拿 "../example"
   svc.OpenWorkspace(root) → 跑 circle 整个解析流程，返回 WorkspaceInfo
   setWorkspace(info)
   setParsed(info.parsed)  → 驱动 Sidebar / PropertiesPanel
   info.graph 驱动 GraphEditor
       │
       ▼
[用户操作]
   编辑代码 → parseCode() debounce 300ms → svc.ParseSource(code)
   点文件  → svc.LoadFile(path)
   Ctrl+S  → svc.SaveFile(path, content)
   Compile → svc.Compile(opts)
   Run     → svc.Run(opts) 流式输出
```

### 调用机制

| 旧期望 | 现实 |
| --- | --- |
| `window.go.mocker_studio.Service.OpenWorkspace()` 自动生成 | wails3 alpha 无 `wails3` CLI → `window.go` 不存在 |
| 直接 `import` 类型化方法 | 改用手写 `Call.ByID(FNV-1a(fqn), ...args)` |

**手写哈希派发**（绕开 wails3 alpha 工具链缺失）：

```ts
// circle/cmd/mocker-studio/frontend/src/lib/service.ts
import { Call } from "@wailsio/runtime";

const M = {
  GetWorkspace:    2157626540,   // fnv1a("circle/internal/ide.Service.GetWorkspace")
  OpenWorkspace:   3694531002,
  // ...
};

export function OpenWorkspace(root) {
  return Call.ByID(M.OpenWorkspace, root);
}
```

Go 侧用 `hash/fnv` 算同样哈希（见 `pkg/application/bindings.go:245`）：

```go
fqn := fmt.Sprintf("%s.%s.%s", packagePath, typeName, methodName)
methodID := hash.Fnv(fqn)
```

---

## 五、已知问题与阻塞

### 🔴 问题 1：前端收不到后端数据（✅ 已解决 — M0.6）

**症状**：
- code 编辑器没有 .ce 文件载入
- graph 视图没有节点 / 边
- Sidebar 仍显示硬编码的 "Nodes / Edges / Enums" 列表，不显示文件树
- 用户的原话："code 下还有图形模式还是没有最新文件的设计"

**根因**：
1. wails3 alpha 工具链不稳定 — `wails3 generate bindings` CLI 在 `go install github.com/wailsapp/wails/v3/cmd/wails3@latest` 拿不到（v3 仓库不提供 `cmd/wails3`）
2. `window.go.XXX` 是 v3 CLI 生成的包装层，**没有 CLI 时不存在**
3. 我们的兜底是手写 `Call.ByID`，但**FNV-1a 哈希的正确性未经运行时验证**（Go 端 `hash.Fnv` 用的是 `hash/fnv`，前端 `Call.ByID` 内部用什么哈希未审计）
4. 即使哈希对，alpha 版的 `Call.ByID` 协议细节（参数序列化 / 返回值反序列化 / 错误码）也未必稳定

**解决**：M0.6 迁移到 wails v2 stable。`wails generate module` 现在能跑、自动生成 `frontend/wailsjs/go/ide/Service.{js,d.ts}` 强类型 binding，前端 `import { OpenWorkspace, ... } from "../../wailsjs/go/ide/Service"` 直接用。

### 🟡 问题 2：M0.5 编辑器 0 改动未生效（✅ 已解决 — M0.6）

**症状**：
- 启动时 App.tsx 的 `useEffect` 应调 `OpenWorkspace` 拿数据
- 但用户报告 code / graph 视图"没看到最新文件"
- 说明 effect 要么没跑、要么跑了但 svc 报 `undefined`

**根因**：
- 跟问题 1 同源：前端在 `await Call.ByID(...)` 时很可能就 throw 了，但 `console.error` 在 devtools 看得到
- 用户没贴 console 报错，但症状一致

**解决**：同 M0.6，迁 wails v2 后 `window.go.ide.Service.OpenWorkspace` 自动可用，不再有 throw。

### 🟢 问题 3：Makefile 依赖检测缺失（已修复）

**症状**：`make build-studio` 只看 `package.json` 变化，不看 `.tsx` / `.ts` 变化

**修复**：
```makefile
FRONTEND_SRC := $(shell find $(STUDIO_ENTRY)/frontend/src -name '*.tsx' -o -name '*.ts' -o -name '*.css' 2>/dev/null)
$(STUDIO_OUTPUT): $(shell find . -name '*.go' -not -path './release/*') $(STUDIO_ENTRY)/frontend/package.json $(FRONTEND_SRC)
```
改后改任意 .tsx 都会触发重编。

### 🔴 问题 4：另一台设备运行后黑屏（M0.7 已修）

**症状**：另一台设备下载 binary 后双击运行：
- 窗口打开
- 闪一下 React 渲染的初始画面
- 立即黑屏
- Go 端在正常处理（log 显示 `DEBUG resolveFlowOps`）

**根因（三个叠加）**：
1. **Go 1.21+ async-preempt 用 SIGUSR1**，JavaScriptCore 也用 SIGUSR1 跑 GC → 信号冲突 → JSC 渲染中崩溃
2. **WebKit 4.1 的 GPU 合成在某些 Mesa 驱动下不兼容** → 闪一下然后合成失败
3. **无 ErrorBoundary**：任何 React 渲染错误都会让整个应用黑屏，无法定位

**修复（M0.7）**：
- ✅ `main.go` 启动时默认设上：`GODEBUG=asyncpreemptoff=1` / `JSC_SIGNAL_FOR_GC=12` / `WEBKIT_DISABLE_COMPOSITING_MODE=1` / `WEBKIT_DISABLE_DMABUF_RENDERER=1` / `WEBKIT_DISABLE_SANDBOX=1`
- ✅ 新增 `release/run-studio.sh` 包装脚本（用户在 shell 里 export 的 env var 优先）
- ✅ 前端加 `ErrorBoundary`（[components/ErrorBoundary.tsx](../circle/cmd/mocker-studio/frontend/src/components/ErrorBoundary.tsx)），任何组件崩了直接显示 stack trace
- ✅ `main.tsx` 加 `window.error` / `unhandledrejection` 监听

**用户使用**：
```bash
# 方式 1：直接跑 binary（main.go 已设默认值）
./release/circle-studio-linux-amd64 ../example

# 方式 2：用包装脚本（更直观）
./release/run-studio.sh ../example
```

### 🔴 问题 5：parsed.imports = null 致 React 崩溃（M0.8 已修）

**症状**：M0.7 修好黑屏后，ErrorBoundary 抓到的新错误：
```
TypeError: null is not an object (evaluating 't.imports.length')
  at fb (.../index-XXX.js:43:106426)   // Sidebar 的 "Imports" 折叠区
```

**根因**：[`workspaceToParsedFile`](../circle/internal/ide/workspace.go#L262) 在 Go 端构造 `ParsedFile` 时，只在 `for _, imp := range mainFile.Imports` 命中循环时 `append`；没有 import 时 `pf.Imports` 是 `nil` slice，Go 把它 marshal 成 JSON `null` 而不是 `[]`。前端 TypeScript 类型说 `imports: string[]`，调用 `.length` 直接 NPE → 整棵 React 树卸载 → 黑屏。

**修复（M0.8）**：
- ✅ Go 端 [workspace.go](../circle/internal/ide/workspace.go) + [parse.go](../circle/internal/ide/parse.go)：构造 `ParsedFile` 时**强制初始化全部 slice 字段**（`Imports` / `Nodes` / `Edges` / `Enums` / `Graph.{Nodes,Edges}`）为空 slice
- ✅ TS 端 [Sidebar.tsx](../circle/cmd/mocker-studio/frontend/src/components/Sidebar.tsx) + [store/editor.ts](../circle/cmd/mocker-studio/frontend/src/store/editor.ts)：对 `parsed.imports` / `parsed.graph.nodes` / `parsed.graph.edges` 加 `?? []` 防御
- ✅ 同时：未来如果后端忘记初始化某个 slice，前端不会黑屏（只可能少渲染一段）

---

## 六、下一步计划

### ✅ 已完成：迁移到 wails v2 stable

迁移已完成（M0.6），上面 §二 已列步骤对照。

### 然后：M1 拖拽编辑（预计 2-3 天）

- `internal/ide/patch.go` 实现 `ApplyEdit`：edit op 分发 + AST 内存修改
- 新增 `internal/ide/emit_ce.go`：`SerializeToSource` 把 AST 反向 emit 回 .ce
- 前端 `GraphEditor.tsx` 的 `onNodeDragStop` / `onConnect` / `onNodesDelete` 调 ApplyEdit
- `main.go` 加 file watcher：内存 AST 变化 → 自动写回 .ce 文件

### 之后：M2 运行时调试（预计 1 周）

- `ide.InspectNode` 实现：在 IR 图上打点 + 取运行时状态
- 集成 Delve：编译时注入调试钩子
- Studio 加断点面板 / step 按钮 / 状态检视

---

## 七、待决策事项

1. ~~**wails 迁移时机**：现在就切 v2，还是先继续修 v3 alpha？~~ ✅ 已切 v2
2. ~~**前端组件迁移策略**~~ ✅ 方案 A：保留所有 9 个组件，只换 `service.ts` 路径
3. ~~**M0.5 那些已写但未生效的代码**~~ ✅ 切 v2 后全部生效

---

## 附录 A：可执行验证清单

```bash
# 1. CLI 编译（已通）
cd /home/wpp/homework/Mocker/circle
make build
./release/circle-linux-amd64 build ../example
# → 输出: helloworld!world!world!

# 2. Studio 编译（编译过但运行时无数据）
make build-studio
ls -lh release/circle-studio-linux-amd64
# → 15 MB

# 3. 切 v2 后的目标
go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.1
# 改 main.go / go.mod 后：
wails build -platform linux/amd64
./build/bin/mocker-studio ../example
# → 窗口打开，code 区显示 main.ce，graph 显示 IR 图，sidebar 显示文件树
```

---

## 附录 B：关键文件路径速查

- 设计文档：[docs/mocker-studio-design.md](./mocker-studio-design.md)
- 进度文档：[docs/mocker-studio-progress.md](./mocker-studio-progress.md)（本文件）
- 语言规范：[docs/language.md](./language.md)
- IR 设计：[docs/ir-design.md](./ir-design.md)

- Go service 包：[circle/internal/ide/](../circle/internal/ide/)
- GUI 入口：[circle/cmd/mocker-studio/main.go](../circle/cmd/mocker-studio/main.go)
- 前端目录：[circle/cmd/mocker-studio/frontend/](../circle/cmd/mocker-studio/frontend/)
- 手写 bindings：[circle/cmd/mocker-studio/frontend/src/lib/service.ts](../circle/cmd/mocker-studio/frontend/src/lib/service.ts)
