// Package ide 提供给 Mocker Studio（wails v2 GUI）调用的 service 层。
//
// 设计原则：
//   - Service 直接 import circle/internal/*（parser / semantic / ir / codegen）
//   - 通过 JSON struct（types.go）与前端通信，对齐 studio/.../store/editor.ts 的 ParsedFile
//   - 不做 marshal 内部 AST（emit_json.go 提供有限的、面向前端的展示字段）
package ide

// ──── 解析结果（对齐 studio frontend/src/store/editor.ts 的 ParsedFile）────

// ParsedFile 整个 .ce 文件解析后的结果
type ParsedFile struct {
	PackageName string       `json:"packageName"`
	Imports     []string     `json:"imports"`
	Graph       GraphData    `json:"graph"`
	Nodes       []NodeDetail `json:"nodes"`
	Edges       []EdgeDetail `json:"edges"`
	Enums       []EnumDetail `json:"enums"`

	// Errors 解析 + 语义检查的错误（前端高亮用）
	Errors []Diagnostic `json:"errors,omitempty"`
}

// ──── 图数据（驱动 React Flow）────

// GraphData React Flow 吃的图数据
//
// M1：在原有 Nodes / Edges 之上加 Packages 分组，让前端能按包折叠/展开。
type GraphData struct {
	Nodes    []FlowNode    `json:"nodes"`
	Edges    []FlowEdge    `json:"edges"`
	Packages []PackageInfo `json:"packages"`
}

// PackageInfo 包的折叠元信息
//
// 一次 OpenWorkspace / BuildGraph 输出所有相关包（main + 它 import 的包）。
// 前端用它做：
//   - Sidebar 列出所有包 + 提供折叠按钮
//   - 跨包边的归一化（collapsed 时整包节点收成一个 "package" 节点）
//   - 节点位置（按包分列，column = pkg index）
type PackageInfo struct {
	Name             string   `json:"name"`             // "stdio"
	IsMain           bool     `json:"isMain"`           // 是否是入口包
	NodeIds          []string `json:"nodeIds"`          // 该包所有 node id（"node-Println" 等）
	BoundaryNodeIds  []string `json:"boundaryNodeIds"`  // 该包有跨包边的 node（折叠时仍要显示）
	DefaultCollapsed bool     `json:"defaultCollapsed"` // 推荐默认状态：非 main 包默认折叠
}

// FlowNode React Flow 的节点
type FlowNode struct {
	ID            string             `json:"id"`
	Type          string             `json:"type"`          // "mockerNode" / "mockerEnum" / "mockerPackage"
	Name          string             `json:"name"`          // 短名，如 "Println"
	QualifiedName string             `json:"qualifiedName"` // "stdio.Println"
	Pkg           string             `json:"pkg"`           // "stdio" / "main" / ""
	Exported      bool               `json:"exported"`
	IsBoundary    bool               `json:"isBoundary"`    // 有跨包边（折叠包时仍显示）
	CollapseState string             `json:"collapseState"` // "expanded" | "collapsed" | "package"（受包折叠控制）
	Position      map[string]float64 `json:"position"`      // {x, y}
	Data          map[string]any     `json:"data"`
}

// FlowEdge React Flow 的边
type FlowEdge struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Target       string  `json:"target"`
	SourceHandle *string `json:"sourceHandle,omitempty"`
	TargetHandle *string `json:"targetHandle,omitempty"`
	EdgeName     string  `json:"edgeName"`
	Animated     bool    `json:"animated"`

	// M1：跨包边信息，让前端能区分同包内边 / 跨包边
	SrcPkg       string `json:"srcPkg"`
	DstPkg       string `json:"dstPkg"`
	CrossPackage bool   `json:"crossPackage"`

	// M1.x：边语义，区分三种关系，前端按 kind 着色 + 选 handle 位置
	//   "lifecycle"  父节点 → 子节点（创建 / 归属）              → 绿色 + 顶部 handle
	//   "dataflow"   父节点.attr → 子节点.input（运行时数据流） → 青色 + 侧 handle
	//   "topology"   topology 边（main 的 Edges）              → 灰色 + 侧 handle
	//   "flow"       block.Flow 边（节点内部 flow 到外部）     → 灰色 + 侧 handle
	Kind string `json:"kind"`

	// Data React Flow 的 data 字段（前端 StyledEdge 走 data.kind / data.edgeName）
	//
	// 把它跟顶层 Kind / EdgeName 保持同步，避免前端额外再剥一次。
	Data map[string]any `json:"data,omitempty"`
}

// ──── 节点 / 边 / 枚举详情（PropertiesPanel 用）────

// NodeDetail 节点详情（属性面板 + 拖拽编辑用）
type NodeDetail struct {
	Name     string       `json:"name"`
	Exported bool         `json:"exported"`
	Kind     string       `json:"kind"` // "node" / "edge" / "struct"
	Members  []NodeMember `json:"members"`
}

// NodeMember 节点体内的成员
type NodeMember struct {
	Kind  string `json:"kind"` // "port_in" / "var" / "field" / "flow" / "flow_target" / "sub_instance" / "sub_edge" / "edge_conn" / "control" / "assign"
	Name  string `json:"name"`
	Type  string `json:"type,omitempty"`
	Value string `json:"value,omitempty"`
}

// EdgeDetail 边详情
type EdgeDetail struct {
	Src  string   `json:"src"`
	Edge string   `json:"edge"`
	Dst  string   `json:"dst"`
	Body []string `json:"body"`
}

// EnumDetail 枚举详情
type EnumDetail struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// ──── 诊断信息（错误提示）────

// Diagnostic 错误 / 警告（前端高亮用）
type Diagnostic struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// ──── 编辑（M1 拖拽编辑用）────

// Edit 单个编辑操作
//
// Op 决定 Payload 的 schema：
//   - "add_node"        → AddNodePayload
//   - "remove_node"     → RemoveNodePayload
//   - "move_node"       → MoveNodePayload
//   - "add_edge"        → AddEdgePayload
//   - "remove_edge"     → RemoveEdgePayload
//   - "patch_node_body" → PatchNodeBodyPayload
type Edit struct {
	Op      string `json:"op"`
	Payload any    `json:"payload"`
}

// AddNodePayload 加节点
type AddNodePayload struct {
	Name     string `json:"name"`
	Exported bool   `json:"exported"`
	Type     string `json:"type"` // "node" / "enum"
	Position [2]int `json:"position"`
	Body     string `json:"body"` // 节点 body 源码
}

// RemoveNodePayload 删节点
type RemoveNodePayload struct {
	Name string `json:"name"`
}

// MoveNodePayload 移动节点（仅 position 元数据）
type MoveNodePayload struct {
	Name     string `json:"name"`
	Position [2]int `json:"position"`
}

// AddEdgePayload 加边
type AddEdgePayload struct {
	Src  string `json:"src"`
	Edge string `json:"edge"`
	Dst  string `json:"dst"`
	Body string `json:"body"` // 边 body 源码
}

// RemoveEdgePayload 删边
type RemoveEdgePayload struct {
	Src  string `json:"src"`
	Edge string `json:"edge"`
	Dst  string `json:"dst"`
}

// PatchNodeBodyPayload 修改节点 body
type PatchNodeBodyPayload struct {
	Name    string `json:"name"`
	NewBody string `json:"newBody"`
}

// ──── 编译运行 ────

// CompileOptions 编译选项
type CompileOptions struct {
	OutputPath string `json:"outputPath"`
	EmitGo     string `json:"emitGo"`
	KeepTmp    bool   `json:"keepTmp"`
	Run        bool   `json:"run"`
	RunArgs    string `json:"runArgs"`
	Source     string `json:"source"`    // 直接传源码（不走 workspace）
	Workspace  string `json:"workspace"` // 或指定 workspace 目录
}

// CompileResult 编译结果
type CompileResult struct {
	Success     bool   `json:"success"`
	Output      string `json:"output"`
	Error       string `json:"error,omitempty"`
	ExitCode    int    `json:"exitCode"`
	GeneratedGo string `json:"generatedGo,omitempty"`
}

// ──── 版本信息 ────

// VersionInfo 版本信息
type VersionInfo struct {
	App       string `json:"app"`
	Build     string `json:"build"`
	CircleDir string `json:"circleDir"`
	GoVersion string `json:"goVersion"`
}

// ──── 工作区（M0.5 真实载入）────

// WorkspaceInfo 整个工作区的快照
//
// OpenWorkspace 返回这个结构，前端据此：
//  1. 在 Sidebar 显示文件树
//  2. 把 MainSource 灌进 CodeEditor
//  3. 用 GraphData 驱动 React Flow
type WorkspaceInfo struct {
	Root       string       `json:"root"`       // 工作区根路径（绝对路径）
	PkgName    string       `json:"pkgName"`    // 主包名（通常 "main"）
	MainFile   string       `json:"mainFile"`   // main.ce 相对路径
	MainSource string       `json:"mainSource"` // main.ce 源码
	Files      []FileInfo   `json:"files"`      // 所有 .ce 文件
	Graph      GraphData    `json:"graph"`      // IR 图（驱动 React Flow）
	Parsed     *ParsedFile  `json:"parsed"`     // 解析后的 AST 摘要（驱动 PropertiesPanel）
	Errors     []Diagnostic `json:"errors"`
}

// FileInfo 单个 .ce 文件信息
type FileInfo struct {
	Path    string `json:"path"`    // 相对 workspace 根
	AbsPath string `json:"absPath"` // 绝对路径
	Pkg     string `json:"pkg"`     // 包名
	Size    int64  `json:"size"`
}

// ──── 运行时状态（M2 动态分析）────

// IRNodeState 一个 IR 节点的运行时快照
//
// 由 codegen 在节点里加 debug emit 时回传：
//   - 入度当前值（channel 上一次收到的）
//   - 出度当前值（最近一次产出）
//   - Block 当前执行状态（idle / running / blocked）
type IRNodeState struct {
	NodeName string         `json:"nodeName"`
	Inputs   map[string]any `json:"inputs,omitempty"`  // input name → value
	Outputs  map[string]any `json:"outputs,omitempty"` // output name → value
	Blocks   []BlockState   `json:"blocks"`
}

// BlockState block 运行状态
type BlockState struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "idle" / "running" / "blocked"
	Trigger string `json:"trigger,omitempty"`
}

// RunEvent 运行时事件（流式回传）
//
// Run() 的 channel 不只发 stdout，也发这些结构化事件，让前端能区分
// "程序输出" 和 "运行时状态变化"。
type RunEvent struct {
	Kind  string `json:"kind"`            // "stdout" / "stderr" / "node_started" / "node_finished" / "edge_send" / "exit"
	Node  string `json:"node,omitempty"`  // 哪个节点（仅 kind=node_* / edge_send）
	Text  string `json:"text,omitempty"`  // stdout/stderr 文本
	State any    `json:"state,omitempty"` // IRNodeState（仅 kind=node_state）
	Exit  int    `json:"exit,omitempty"`  // 退出码（仅 kind=exit）
	Time  int64  `json:"time"`            // unix nano
}
