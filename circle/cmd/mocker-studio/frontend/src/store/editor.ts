import { create } from "zustand";
import type { Node, Edge } from "@xyflow/react";
import { ide } from "../../wailsjs/go/models";

export type NodeMember = ide.NodeMember;
export type NodeDetail = ide.NodeDetail;
export type EdgeDetail = ide.EdgeDetail;
export type EnumDetail = ide.EnumDetail;
export type ParsedFile = ide.ParsedFile;
export type CompileResult = ide.CompileResult;
export type CompileOptions = ide.CompileOptions;
export type Diagnostic = ide.Diagnostic;
export type FileInfo = ide.FileInfo;
export type WorkspaceInfo = ide.WorkspaceInfo;
export type IRNodeState = ide.IRNodeState;
export type VersionInfo = ide.VersionInfo;
export type PackageInfo = ide.PackageInfo;
export type GraphData = ide.GraphData;

// M0.5: 工作区文件树（类型从 wailsjs 引入，见上）

// M0.5: 诊断信息（类型从 wailsjs 引入，见上）

// M0.5: Workspace 信息（类型从 wailsjs 引入，见上）

type ViewMode = "graph" | "code" | "split";

interface EditorState {
  // Code state
  code: string;
  setCode: (code: string) => void;

  // Parsed state
  parsed: ParsedFile | null;
  setParsed: (parsed: ParsedFile | null) => void;

  // React Flow state
  flowNodes: Node[];
  flowEdges: Edge[];
  setFlowNodes: (nodes: Node[]) => void;
  setFlowEdges: (edges: Edge[]) => void;

  // Selection
  selectedNodeId: string | null;
  setSelectedNodeId: (id: string | null) => void;
  selectedNodeDetail: NodeDetail | null;

  // M1.x: 选中的边（点边查流动）
  selectedEdgeId: string | null;
  setSelectedEdgeId: (id: string | null) => void;

  // View mode
  viewMode: ViewMode;
  setViewMode: (mode: ViewMode) => void;

  // Compile result
  compileResult: CompileResult | null;
  setCompileResult: (result: CompileResult | null) => void;
  isCompiling: boolean;
  setIsCompiling: (v: boolean) => void;

  // Sidebar state
  sidebarCollapsed: boolean;
  toggleSidebar: () => void;

  // Properties panel state
  propertiesOpen: boolean;
  setPropertiesOpen: (v: boolean) => void;

  // Dirty flag
  isDirty: boolean;
  setIsDirty: (v: boolean) => void;

  // M0.5: 工作区
  workspaceRoot: string | null;
  workspaceFiles: FileInfo[];
  currentFile: string | null;     // 当前编辑的相对路径
  setWorkspace: (info: WorkspaceInfo) => void;
  ingestReparse: (info: WorkspaceInfo) => void; // ReparseWorkspace 回流：不覆盖 code
  setCurrentFile: (path: string, content: string) => void;
  closeWorkspace: () => void;

  // M1.x: 双击图节点 → 跳源
  //   cursorLocation.path 为 null 时，编辑器不动
  //   path 变化时 CodeEditor 加载新文件 + 设光标
  cursorLocation: { path: string; line: number; col: number; nonce: number } | null;
  setCursorLocation: (path: string, line: number, col: number) => void;
  clearCursorLocation: () => void;

  // M0.5: 诊断信息（编译错误实时反馈）
  diagnostics: Diagnostic[];
  setDiagnostics: (diags: Diagnostic[]) => void;

  // M0.5: 流式运行时输出
  runOutput: string[];
  appendRunOutput: (chunk: string) => void;
  clearRunOutput: () => void;
  isRunning: boolean;
  setIsRunning: (v: boolean) => void;

  // M1: 跨包折叠状态
  packages: PackageInfo[];
  setPackages: (pkgs: PackageInfo[]) => void;
  collapsedPackages: Record<string, boolean>; // pkgName → 是否折叠
  togglePackageCollapse: (pkgName: string) => void;
  expandAllPackages: () => void;
  collapseAllPackages: () => void;
  collapsedNodes: Record<string, boolean>; // nodeId → 是否折叠（节点级）
  toggleNodeCollapse: (nodeId: string) => void;
}

const DEFAULT_CODE = `package main

import stdio

hello {
    h := "hello"
    world w
    <add_str> w

    >>str out_str
    stdio.Println p;
    out_str >> p.msg;
}

world {
    >> str words
    new:=words
    t:= 1+2*1-8+(2+2)*2
    for(i:=0;i<t;i++) { new+="world!" }
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new>>hello.out_str
}

main {
    hello happy;
}`;

export const useEditorStore = create<EditorState>((set, get) => ({
  code: DEFAULT_CODE,
  setCode: (code) => set({ code, isDirty: true }),

  parsed: null,
  setParsed: (parsed) => {
    if (!parsed) {
      set({ parsed: null, flowNodes: [], flowEdges: [] });
      return;
    }

    const flowNodes: Node[] = (parsed.graph?.nodes ?? []).map((n) => ({
      id: n.id,
      type: n.type,
      // wails 生成的类型把 position 拍成 Record<string, number>，react-flow 要 {x, y}
      position: { x: Number(n.position?.x ?? 0), y: Number(n.position?.y ?? 0) },
      data: {
        ...(n.data as Record<string, unknown>),
        pkg: n.pkg,
        qualified: n.qualifiedName,
        isBoundary: n.isBoundary,
        collapseState: n.collapseState,
      },
    }));

    const flowEdges: Edge[] = (parsed.graph?.edges ?? []).map((e) => {
      // lifecycle 边用顶部 handle，dataflow/flow 用侧边 handle（来自服务端）
      const isLifecycle = e.kind === "lifecycle";
      return {
        id: e.id,
        source: e.source,
        target: e.target,
        sourceHandle: isLifecycle ? "lifecycle-out" : e.sourceHandle,
        targetHandle: isLifecycle ? "lifecycle-in" : e.targetHandle,
        data: {
          edgeName: e.edgeName,
          srcPkg: e.srcPkg,
          dstPkg: e.dstPkg,
          crossPackage: e.crossPackage,
          kind: e.kind,
        },
        animated: e.animated,
        label: e.edgeName,
        type: "smoothstep",
      };
    });

    // M1: 同步 packages + 初始化折叠状态
    const packages = parsed.graph?.packages ?? [];
    const collapsedPackages: Record<string, boolean> = {};
    for (const p of packages) {
      collapsedPackages[p.name] = p.defaultCollapsed;
    }
    // 默认折叠所有 non-main 节点
    const collapsedNodes: Record<string, boolean> = {};
    for (const n of parsed.graph?.nodes ?? []) {
      if (n.pkg && n.pkg !== "" && n.pkg !== "main") {
        collapsedNodes[n.id] = true;
      }
    }

    set({ parsed, flowNodes, flowEdges, packages, collapsedPackages, collapsedNodes });
  },

  flowNodes: [],
  flowEdges: [],
  setFlowNodes: (flowNodes) => set({ flowNodes }),
  setFlowEdges: (flowEdges) => set({ flowEdges }),

  selectedNodeId: null,
  setSelectedNodeId: (id) => {
    const state = get();
    let detail: NodeDetail | null = null;
    if (id && state.parsed) {
      const nodeName = id.replace("node-", "").replace("enum-", "").replace("package-", "");
      detail = state.parsed.nodes.find((n) => n.name === nodeName) || null;
    }
    set({ selectedNodeId: id, selectedNodeDetail: detail, propertiesOpen: id !== null });
  },
  selectedNodeDetail: null,

  selectedEdgeId: null,
  setSelectedEdgeId: (id) =>
    set({ selectedEdgeId: id, propertiesOpen: id !== null }),

  viewMode: "split",
  setViewMode: (viewMode) => set({ viewMode }),

  compileResult: null,
  setCompileResult: (compileResult) => set({ compileResult }),
  isCompiling: false,
  setIsCompiling: (isCompiling) => set({ isCompiling }),

  sidebarCollapsed: false,
  toggleSidebar: () => set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),

  propertiesOpen: false,
  setPropertiesOpen: (propertiesOpen) => set({ propertiesOpen }),

  isDirty: false,
  setIsDirty: (isDirty) => set({ isDirty }),

  // M0.5: 工作区
  workspaceRoot: null,
  workspaceFiles: [],
  currentFile: null,
  setWorkspace: (info) =>
    set({
      workspaceRoot: info.root,
      workspaceFiles: info.files,
      currentFile: info.mainFile,
      code: info.mainSource,
      isDirty: false,
    }),
  // ingestReparse —— 把 ReparseWorkspace 返回的 WorkspaceInfo 喂进 store
  // 但**不**覆盖 code / currentFile（保持编辑器内未保存草稿不变）
  ingestReparse: (info) =>
    set((s) => ({
      workspaceRoot: info.root || s.workspaceRoot,
      workspaceFiles: info.files?.length ? info.files : s.workspaceFiles,
      // code / currentFile 保持不变
    })),
  setCurrentFile: (path, content) =>
    set({ currentFile: path, code: content, isDirty: false }),
  closeWorkspace: () =>
    set({
      workspaceRoot: null,
      workspaceFiles: [],
      currentFile: null,
      code: DEFAULT_CODE,
      parsed: null,
      flowNodes: [],
      flowEdges: [],
      diagnostics: [],
      isDirty: false,
      cursorLocation: null,
    }),

  // M1.x: 跳源
  cursorLocation: null,
  setCursorLocation: (path, line, col) =>
    set((s) => ({
      // nonce 递增：同一文件内跳不同行也能触发 effect
      cursorLocation: { path, line, col, nonce: (s.cursorLocation?.nonce ?? 0) + 1 },
    })),
  clearCursorLocation: () => set({ cursorLocation: null }),

  // M0.5: 诊断
  diagnostics: [],
  setDiagnostics: (diagnostics) => set({ diagnostics }),

  // M0.5: 流式输出
  runOutput: [],
  appendRunOutput: (chunk) =>
    set((s) => ({ runOutput: [...s.runOutput, chunk] })),
  clearRunOutput: () => set({ runOutput: [] }),
  isRunning: false,
  setIsRunning: (isRunning) => set({ isRunning }),

  // M1: 跨包折叠
  packages: [],
  setPackages: (packages) => {
    // 按 PackageInfo.DefaultCollapsed 初始化折叠状态
    const collapsed: Record<string, boolean> = {};
    for (const p of packages) {
      collapsed[p.name] = p.defaultCollapsed;
    }
    set({ packages, collapsedPackages: collapsed });
  },
  collapsedPackages: {},
  togglePackageCollapse: (pkgName) =>
    set((s) => ({
      collapsedPackages: {
        ...s.collapsedPackages,
        [pkgName]: !s.collapsedPackages[pkgName],
      },
    })),
  expandAllPackages: () =>
    set((s) => {
      const next: Record<string, boolean> = {};
      for (const k of Object.keys(s.collapsedPackages)) next[k] = false;
      return { collapsedPackages: next };
    }),
  collapseAllPackages: () =>
    set((s) => {
      const next: Record<string, boolean> = {};
      for (const k of Object.keys(s.collapsedPackages)) next[k] = true;
      return { collapsedPackages: next };
    }),
  collapsedNodes: {},
  toggleNodeCollapse: (nodeId) =>
    set((s) => ({
      collapsedNodes: {
        ...s.collapsedNodes,
        [nodeId]: !s.collapsedNodes[nodeId],
      },
    })),
}));
