# Mocker Studio 接入设计（circle + wails3）

> 把 [studio/mocker-studio](../studio/mocker-studio) 的可视化前端**直接接入**到 circle 编译器内部：
> - GUI 入口变成 `circle/cmd/mocker-studio/`
> - 后端 service 直接 `import "circle/internal/..."`（无进程间 JSON 序列化）
> - `make build` 出 `circle`（CLI）+ `make build-studio` 出 `circle-studio`（GUI）

---

## 〇、目录

1. [为什么放在 circle 里](#一为什么放在-circle-里)
2. [最终目录结构](#二最终目录结构)
3. [Service 接口设计](#三service-接口设计)
4. [前端集成](#四前端集成)
5. [Makefile / go.mod 改动](#五makefile--gomod-改动)
6. [执行计划（M0 → M1 → M2）](#六执行计划m0--m1--m2)
7. [工作量与产出](#七工作量与产出)

---

## 一、为什么放在 circle 里

| 方案 | 进程模型 | 拖拽编辑延迟 | 维护成本 |
| --- | --- | --- | --- |
| studio 子进程 exec circle | 每次按键都 fork 一次 | 几百 ms | 高（双向 JSON 同步）|
| **circle/cmd/mocker-studio 直接 import circle/internal** | 单进程，函数调用 | **零开销** | 低（同源代码）|

放 `circle/cmd/mocker-studio/` 下，可以直接 `import "circle/internal/ir"` / `"circle/internal/parser"` / `"circle/internal/semantic"`，**不用 marshal JSON 跨进程**。

---

## 二、最终目录结构

```
circle/
├── Makefile                       # 改：加 build-studio target
├── go.mod                         # 改：加 wails/v3 依赖（+ build tag 隔离）
├── internal/
│   ├── ide/                       # 🆕 暴露给前端的 service 层
│   │   ├── service.go             # Service struct + NewService
│   │   ├── types.go               # JSON-serializable 结构（ParsedFile / GraphData / Edit / CompileResult）
│   │   ├── parse.go               # ParseSource(src) → AST + 符号表 JSON
│   │   ├── graph.go               # BuildGraph() → IRGraph JSON（驱动 React Flow）
│   │   ├── patch.go               # ApplyEdit(edit) → 修改后的 GraphData（M1 拖拽编辑核心）
│   │   ├── compile.go             # Compile + Run + 流式 stdout
│   │   └── emit_json.go           # AST / IR → JSON 的辅助
│   ├── parser/                    # 现有，不动
│   ├── semantic/                  # 现有
│   ├── ir/                        # 现有（提供 IRGraph，天然适合驱动 React Flow）
│   └── codegen/                   # 现有（Compile + Run 复用）
└── cmd/
    ├── circle/main.go             # 现有 CLI 编译器，不动
    ├── tokdump/main.go            # 现有
    └── mocker-studio/             # 🆕 wails3 GUI 入口
        ├── main.go                # application.New + 注册 ide.Service
        └── frontend/              # 从 studio/mocker-studio/frontend 拷过来，删 parseCodeFrontend
```

---

## 三、Service 接口设计

```go
// circle/internal/ide/service.go
package ide

type Service struct {
    workspaceDir string  // 当前工作区
}

func New(workspaceDir string) *Service

// ──── 解析 ────
// ParseSource 解析单段 .ce 源码 → 完整 ParsedFile JSON
func (s *Service) ParseSource(src string) (*ParsedFile, error)

// ParseWorkspace 解析整个 workspace → 完整 ParsedFile JSON
func (s *Service) ParseWorkspace() (*ParsedFile, error)

// ──── 图构建（驱动 React Flow）──
// BuildGraph 走完整 pipeline：scan → semantic → IR Lower → BuildGraph → GraphData JSON
func (s *Service) BuildGraph() (*GraphData, error)

// ──── 拖拽编辑（M1）──
type Edit struct {
    Op       string // "add_node" | "remove_node" | "move_node" | "add_edge" | "remove_edge" | "patch_node_body"
    Payload  any    // 各 op 不同负载
}
func (s *Service) ApplyEdit(edit Edit) (*GraphData, error)

// ──── 编译运行 ───
type CompileOptions struct {
    OutputPath string
    EmitGo     string
    KeepTmp    bool
    Run        bool
    RunArgs    string
}
type CompileResult struct {
    Success     bool
    Output      string
    Error       string
    ExitCode    int
    GeneratedGo string  // emit-go 的产物
}
func (s *Service) Compile(opts CompileOptions) (*CompileResult, error)
func (s *Service) Run(opts CompileOptions) (<-chan string, error)  // 流式 stdout

// ──── 编辑 → 代码（M1）──
func (s *Service) SerializeToSource() (string, error)
```

### 3.1 JSON 类型要点（对齐 studio store）

`ParsedFile` 字段直接对齐 [studio/mocker-studio/frontend/src/store/editor.ts](file:///home/wpp/homework/Mocker/studio/mocker-studio/frontend/src/store/editor.ts) 里的 `ParsedFile` 类型，保证前端**零改动**对接：

```go
type ParsedFile struct {
    PackageName string
    Imports     []string
    Graph       GraphData  // nodes + edges（React Flow 吃这个）
    Nodes       []NodeDetail
    Edges       []EdgeDetail
    Enums       []EnumDetail
}

type GraphData struct {
    Nodes []FlowNode  // { id, type, name, exported, position, data }
    Edges []FlowEdge  // { id, source, target, sourceHandle, targetHandle, edgeName, animated }
}

type FlowNode struct {
    ID       string                 `json:"id"`
    Type     string                 `json:"type"`     // "mockerNode" | "mockerEnum"
    Name     string                 `json:"name"`
    Exported bool                   `json:"exported"`
    Position map[string]float64     `json:"position"` // {x, y}
    Data     map[string]any         `json:"data"`
}

type FlowEdge struct {
    ID           string  `json:"id"`
    Source       string  `json:"source"`
    Target       string  `json:"target"`
    SourceHandle *string `json:"sourceHandle,omitempty"`
    TargetHandle *string `json:"targetHandle,omitempty"`
    EdgeName     string  `json:"edgeName"`
    Animated     bool    `json:"animated"`
}
```

### 3.2 `BuildGraph` 数据流

```
ParseWorkspace (扫 .ce)
    ↓
semantic.CheckAll → WorkspaceResult
    ↓
ir.Lower → IRProgram
    ↓
ir.BuildGraph(prog) → IRGraph { Nodes, Edges, StartNodes }
    ↓
ide.translateGraphData(IRGraph) → GraphData JSON（nodes / edges 直接对应 React Flow）
```

---

## 四、前端集成

### 4.1 直接复用 studio 的组件

| 文件 | 复用方式 |
| --- | --- |
| [studio/frontend/src/components/GraphEditor.tsx](../studio/mocker-studio/frontend/src/components/GraphEditor.tsx) | 整文件拷，零改动 |
| [studio/frontend/src/components/MockerNode.tsx](../studio/mocker-studio/frontend/src/components/MockerNode.tsx) | 整文件拷 |
| [studio/frontend/src/components/StyledEdge.tsx](../studio/mocker-studio/frontend/src/components/StyledEdge.tsx) | 整文件拷 |
| [studio/frontend/src/components/Sidebar.tsx](../studio/mocker-studio/frontend/src/components/Sidebar.tsx) | 整文件拷 |
| [studio/frontend/src/components/Toolbar.tsx](../studio/mocker-studio/frontend/src/components/Toolbar.tsx) | 整文件拷 |
| [studio/frontend/src/components/PropertiesPanel.tsx](../studio/mocker-studio/frontend/src/components/PropertiesPanel.tsx) | 整文件拷 |
| [studio/frontend/src/components/OutputPanel.tsx](../studio/mocker-studio/frontend/src/components/OutputPanel.tsx) | 整文件拷 |
| [studio/frontend/src/components/CodeEditor.tsx](../studio/mocker-studio/frontend/src/components/CodeEditor.tsx) | 整文件拷 |
| [studio/frontend/src/lib/mocker-lang.ts](../studio/mocker-studio/frontend/src/lib/mocker-lang.ts) | 整文件拷（小补 enum / for / while / if 高亮） |
| [studio/frontend/src/store/editor.ts](../studio/mocker-studio/frontend/src/store/editor.ts) | 整文件拷（**`ParsedFile` 类型严格对齐 `internal/ide/types.go`**） |

### 4.2 `App.tsx` 必须改

[studio/frontend/src/App.tsx#L169-L540](../studio/mocker-studio/frontend/src/App.tsx#L169-L540) 的 `parseCodeFrontend` 函数（约 400 行正则解析器）**整段删除**，全部走：

```typescript
// 替换 parseCodeFrontend
const result = await window.go.circle.Service.ParseSource(code);
setParsed(result);
```

> Wails3 的 JS 绑定命名约定：`mocker-studio/Service` → `window.go.mocker_studio.Service`。
> 我们把 cmd 包名改成 `mocker-studio`，main 包用 `application.NewService(&ide.Service{})` 注册。

### 4.3 前端目录结构

```
circle/cmd/mocker-studio/
├── main.go
└── frontend/
    ├── package.json               # name 改成 "mocker-studio-frontend"
    ├── tsconfig.json
    ├── vite.config.ts             # base path 调整
    ├── index.html
    ├── public/
    └── src/
        ├── App.tsx                # 删 parseCodeFrontend，改 window.go.mocker_studio.Service
        ├── main.tsx
        ├── styles.css
        ├── vite-env.d.ts
        ├── components/            # 整目录拷
        ├── lib/                   # 整目录拷
        └── store/                 # 整目录拷
```

---

## 五、Makefile / go.mod 改动

### 5.1 `circle/go.mod`

从 [studio/go.mod](../studio/mocker-studio/go.mod) 抄 wails/v3 那一坨 + indirect deps：

```go
require github.com/wailsapp/wails/v3 v3.0.0-alpha.98

require (
    // ... 一堆 indirect，抄过来
)
```

> **隔离技巧**：wails3 依赖只在 `cmd/mocker-studio/*.go` 用，加 build tag：
> ```go
> //go:build wails3
> ```
> 开发者本地 `go build ./...` 不拉 wails 那一坨；`make build-studio` 加 `-tags wails3`。

### 5.2 `circle/Makefile`

加 3 行：

```makefile
.PHONY: build-studio
build-studio:
	@echo "→ building frontend..."
	@cd cmd/mocker-studio/frontend && npm install && npm run build
	@echo "→ building circle-studio..."
	@go build -tags wails3 -ldflags='$(LDFLAGS)' -o $(RELEASE_DIR)/circle-studio-$(GOOS)-$(GOARCH) ./cmd/mocker-studio
	@echo "✓ circle-studio built"

build-all: build build-studio
```

---

## 六、执行计划（M0 → M1 → M2）

### M0（骨架）—— 跑通"打开 .ce → 前端显示图 + 编译运行"

1. `internal/ide/{types,parse,graph,compile}.go` —— service 接口
2. `internal/ide/service.go` —— Service 注册
3. `cmd/mocker-studio/main.go` —— wails3 app
4. `circle/go.mod` —— 加 wails3 依赖
5. `circle/Makefile` —— build-studio target
6. 前端从 studio 拷过来 + 删 parseCodeFrontend
7. **验收**：`make build-studio` 出 `release/circle-studio-*`，打开 .ce 能看到图，点 Compile 能跑

### M1（拖拽编辑）

1. `internal/ide/patch.go` —— ApplyEdit + Edit 类型
2. 前端 `GraphEditor.tsx` 的 onNodeDragStop / onConnect / onNodesDelete 调 ApplyEdit
3. `internal/ide/emit_json.go` 扩展 —— AST → .ce 文本（`SerializeToSource`）
4. **验收**：拖节点、改连线，前端实时同步，反向写回 .ce 文件

### M2（运行时调试）

1. `internal/ide/compile.go` 扩展 —— 加 channel 流式 stdout
2. 前端 `OutputPanel.tsx` 显示实时输出
3. 加断点 / step / inspect IR 状态（M2+）

---

## 七、工作量与产出

| 阶段 | Go 代码 | 前端代码 | 复制粘贴 | 估计时间 |
| --- | --- | --- | --- | --- |
| **M0** | 600-800 行新 | 删 400 行正则 + 改 20 行 | 2000 行前端拷过来 | 1-2 天 |
| **M1** | 400-600 行新 | 100 行 | 0 | 1-2 天 |
| **M2** | 200-400 行新 | 100 行 | 0 | 1 天 |

### 7.1 复用现成代码清单

| 现有代码 | 复用方式 |
| --- | --- |
| [circle/cmd/circle/main.go#L75-L164](../circle/cmd/circle/main.go) 的 `runBuild` | 抽公共 `ide.Build(srcCode, opts)`，CLI 和 GUI 都调 |
| [circle/internal/ir/graph.go](../circle/internal/ir/graph.go) 的 `IRGraph` | `BuildGraph()` 直接转 React Flow 数据 |
| [studio/mocker-studio/main.go](../studio/mocker-studio/main.go) | 模板拷贝，改 1 行 service 名 |
| [studio/mocker-studio/frontend/src/components/*](../studio/mocker-studio/frontend/src/components/) | 全部直接拷 |
| [studio/mocker-studio/frontend/src/store/editor.ts](../studio/mocker-studio/frontend/src/store/editor.ts) | 直接拷，`ParsedFile` 对齐 |

---

## 八、命名约定

| 项目 | 值 |
| --- | --- |
| Go module | `circle`（现有） |
| Studio Go 包 | `mocker-studio`（在 `circle/cmd/mocker-studio/`） |
| Wails 服务名 | `Service`（注入名 `mocker_studio.Service`） |
| 前端包名 | `mocker-studio-frontend` |
| 二进制名 | `circle`（CLI）+ `circle-studio`（GUI） |
| Build tag | `wails3`（隔离 wails 依赖） |

> JS 端访问路径：`window.go.mocker_studio.Service.ParseSource(...)`。
> 这是 wails3 的默认约定：包名 `mocker-studio` → `mocker_studio`，加上注册的 service 类型名。

---

## 九、目录与文件汇总（落地清单）

### 🆕 新建文件

- `circle/internal/ide/service.go`
- `circle/internal/ide/types.go`
- `circle/internal/ide/parse.go`
- `circle/internal/ide/graph.go`
- `circle/internal/ide/compile.go`
- `circle/internal/ide/emit_json.go`
- `circle/internal/ide/patch.go`（M1）
- `circle/cmd/mocker-studio/main.go`
- `circle/cmd/mocker-studio/frontend/*`（从 studio 拷贝）

### ✏️ 修改文件

- `circle/go.mod`（加 wails3 依赖）
- `circle/Makefile`（加 build-studio target）
- `circle/cmd/mocker-studio/frontend/src/App.tsx`（删 parseCodeFrontend，改调 wails service）
- `circle/cmd/mocker-studio/frontend/package.json`（改 name）
- `circle/cmd/mocker-studio/frontend/vite.config.ts`（调 base path）
- `circle/cmd/circle/main.go`（抽出 `ide.Build` 公共函数，CLI 调用之）