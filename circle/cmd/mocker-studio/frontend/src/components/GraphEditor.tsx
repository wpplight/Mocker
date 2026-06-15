import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  BackgroundVariant,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Node,
  type Edge,
  type OnNodesChange,
  type OnConnect,
  addEdge,
  Panel,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { MockerNode } from "./MockerNode";
import { MockerEnumNode } from "./MockerEnumNode";
import { MockerPackageNode } from "./MockerPackageNode";
import { StyledEdge } from "./StyledEdge";
import { useEditorStore } from "../store/editor";
import { LoadFile, LocateNode } from "../lib/service";
import { Layers } from "lucide-react";

const nodeTypes = {
  mockerNode: MockerNode,
  mockerEnum: MockerEnumNode,
  mockerPackage: MockerPackageNode,
};

const edgeTypes = {
  styledEdge: StyledEdge,
};

export function GraphEditor() {
  const {
    flowNodes,
    flowEdges,
    setSelectedNodeId,
    setSelectedEdgeId,
    packages,
    collapsedPackages,
    collapsedNodes,
    expandAllPackages,
    collapseAllPackages,
  } = useEditorStore();

  const [nodes, setNodes, onNodesChange] = useNodesState(flowNodes);
  const [edges, setEdges, onEdgesChange] = useEdgesState(flowEdges);

  // M1: 把折叠的包收成"包容器"节点；其他原样保留
  //
  // 包折叠状态：collapsedPackages[pkg] === true → 整包节点（boundary 也算）全部隐藏，
  // 替换成一个 mockerPackage 容器节点；
  // 展开 → 全部原样显示。
  //
  // 包容器的位置：放在包内第一个节点 y 不变，但 x 偏移到一个独立的"包列"
  // —— 包列紧贴 main 的最右一列右边，避免与原节点视觉重叠
  const visibleNodes = useMemo(() => {
    if (packages.length === 0) return flowNodes;

    // 算 main 包的 x 范围
    let mainMaxX = 0;
    for (const n of flowNodes) {
      const npkg = (n.data as Record<string, unknown> | undefined)?.pkg as string;
      if (npkg === "" || npkg === "main") {
        if (n.position.x > mainMaxX) mainMaxX = n.position.x;
      }
    }
    const pkgColumnBaseX = mainMaxX + 400; // 包列起点

    // 算出每个非 main 包在"包列"内的偏移
    const pkgColIndex: Record<string, number> = {};
    let nonMainIdx = 0;
    for (const pkg of packages) {
      if (pkg.isMain) continue;
      pkgColIndex[pkg.name] = nonMainIdx++;
    }

    const out: Node[] = [];

    for (const pkg of packages) {
      const isCollapsed = Boolean(collapsedPackages[pkg.name]);
      const pkgNodes = flowNodes.filter((n) => {
        const npkg = (n.data as Record<string, unknown> | undefined)?.pkg as string;
        return npkg === pkg.name;
      });
      if (!isCollapsed) {
        // 展开：原样输出
        for (const n of pkgNodes) out.push(n);
        continue;
      }
      // 折叠：整包节点全部隐藏，只留一个包容器
      // 取包内第一个节点 y 作为参考（保持视觉上是该包的位置）
      const baseY = pkgNodes.length > 0 ? pkgNodes[0].position.y : 60;
      // x 放到包列：pkgColumnBaseX + 索引 * 240
      const colIdx = pkgColIndex[pkg.name] ?? 0;
      out.push({
        id: `package-${pkg.name}`,
        type: "mockerPackage",
        position: { x: pkgColumnBaseX + colIdx * 240, y: baseY },
        data: {
          pkgName: pkg.name,
          nodeCount: pkgNodes.length,
          boundaryCount: pkg.boundaryNodeIds?.length ?? 0,
          isMain: pkg.isMain,
        },
      });
    }

    return out;
  }, [flowNodes, packages, collapsedPackages]);

  // 把所有节点 id（flowNodes）的真实 id 收集起来用于"折叠包内"边隐藏
  const flowNodeIds = useMemo(() => new Set(flowNodes.map((n) => n.id)), [flowNodes]);

  // M1: 边重路由
  //   - source/target 都在折叠包里 → 隐藏
  //   - source/target 一端折叠 → 改连到对应包容器
  //     - lifecycle 边 → 走容器顶部 handle（lifecycle-out/in）
  //     - dataflow / topology / flow 边 → 走容器左/右侧 handle（port-in/out）
  //   - 端点都可见 → 保持
  const visibleEdges = useMemo(() => {
    const out: Edge[] = [];
    for (const e of flowEdges) {
      const srcInFlow = flowNodeIds.has(e.source);
      const dstInFlow = flowNodeIds.has(e.target);
      const srcInVisible = visibleNodes.some((n) => n.id === e.source);
      const dstInVisible = visibleNodes.some((n) => n.id === e.target);
      if (srcInVisible && dstInVisible) {
        out.push(e);
        continue;
      }
      // 找出折叠掉的端点
      if (!srcInVisible && !dstInVisible) {
        // 两端都不可见（同一折叠包内）→ 隐藏
        continue;
      }
      // 一端可见一端折叠 → 重路由到包容器
      // handle 选择跟原边 kind 一致：lifecycle 用顶部，其它用左右
      const ed = (e.data as Record<string, unknown>) ?? {};
      const ek = (ed.kind as string) ?? "flow";
      const isLifecycle = ek === "lifecycle";
      const srcHandleId = isLifecycle ? "lifecycle-out" : "port-out";
      const dstHandleId = isLifecycle ? "lifecycle-in" : "port-in";
      let newEdge: Edge = { ...e };
      if (!srcInVisible && srcInFlow) {
        const origNode = flowNodes.find((n) => n.id === e.source);
        if (origNode) {
          const opkg = (origNode.data as Record<string, unknown> | undefined)?.pkg as string;
          if (opkg) {
            newEdge.source = `package-${opkg}`;
            newEdge.sourceHandle = srcHandleId;
            newEdge.data = { ...(e.data ?? {}), _redirected: true };
          }
        }
      }
      if (!dstInVisible && dstInFlow) {
        const origNode = flowNodes.find((n) => n.id === e.target);
        if (origNode) {
          const opkg = (origNode.data as Record<string, unknown> | undefined)?.pkg as string;
          if (opkg) {
            newEdge.target = `package-${opkg}`;
            newEdge.targetHandle = dstHandleId;
            newEdge.data = { ...(e.data ?? {}), _redirected: true };
          }
        }
      }
      out.push(newEdge);
    }
    return out;
  }, [flowEdges, flowNodes, visibleNodes, flowNodeIds]);

  // Sync from store to local state — 把 _collapsed 折叠标记烘焙进节点
  useEffect(() => {
    setNodes(
      visibleNodes.map((n) => ({
        ...n,
        data: {
          ...(n.data as Record<string, unknown>),
          _collapsed: n.type === "mockerNode" ? Boolean(collapsedNodes[n.id]) : undefined,
        },
      }))
    );
  }, [visibleNodes, collapsedNodes, setNodes]);

  useEffect(() => {
    setEdges(
      visibleEdges.map((e) => ({
        ...e,
        type: "styledEdge",
      }))
    );
  }, [visibleEdges, setEdges]);

  const onConnect: OnConnect = useCallback(
    (connection) => {
      const newEdge: Edge = {
        ...connection,
        id: `e-${connection.source}-${connection.target}-${Date.now()}`,
        type: "styledEdge",
        animated: true,
      };
      setEdges((eds) => addEdge(newEdge, eds));
    },
    [setEdges]
  );

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      setSelectedNodeId(node.id);
      setSelectedEdgeId(null);
    },
    [setSelectedNodeId, setSelectedEdgeId]
  );

  // M1.x: 双击图节点 → 跳源
  //   - 单击只选中（不抢编辑器焦点 / 不切文件）
  //   - 双击：调 LocateNode(qualified) → 切文件 + 跳光标
  const onNodeDoubleClick = useCallback(
    async (_: React.MouseEvent, node: Node) => {
      // 包容器双击不跳（无法定位到 struct）
      if (node.type === "mockerPackage") {
        const pkgName = (node.data as { pkg?: string })?.pkg;
        if (pkgName) {
          // 双击包容器 = 展开该包
          const { togglePackageCollapse } = useEditorStore.getState();
          togglePackageCollapse(pkgName);
        }
        return;
      }
      // enum 节点没源码位置
      if (node.type === "mockerEnum") {
        return;
      }
      const qualified =
        (node.data as { qualified?: string })?.qualified ?? node.id;
      try {
        const loc = await LocateNode(qualified);
        if (loc.path) {
          // 切到目标文件（如果不同），再设光标
          const state = useEditorStore.getState();
          if (state.currentFile !== loc.path) {
            const src = await LoadFile(loc.path);
            state.setCurrentFile(loc.path, src);
          }
          state.setCursorLocation(loc.path, loc.line, loc.col);
        }
      } catch (e) {
        console.warn("LocateNode failed for", qualified, e);
      }
    },
    []
  );

  const onEdgeClick = useCallback(
    (_: React.MouseEvent, edge: Edge) => {
      setSelectedEdgeId(edge.id);
      setSelectedNodeId(null);
    },
    [setSelectedEdgeId, setSelectedNodeId]
  );

  const onPaneClick = useCallback(() => {
    setSelectedNodeId(null);
    setSelectedEdgeId(null);
  }, [setSelectedNodeId, setSelectedEdgeId]);

  const nodeColor = useCallback((node: Node) => {
    if (node.type === "mockerEnum") return "oklch(0.50 0.05 260)";
    const data = node.data as Record<string, unknown> | undefined;
    const pkg = (data?.pkg as string) ?? "";
    // 跨包节点用 hue 区分
    if (pkg === "stdio") return "oklch(0.60 0.14 180)";
    if (pkg === "io") return "oklch(0.55 0.15 320)";
    if (pkg === "netio") return "oklch(0.55 0.15 30)";
    if (pkg === "cookie") return "oklch(0.55 0.15 100)";
    const kind = (data?.kind as string) ?? "";
    if (kind === "struct") return "oklch(0.55 0.15 150)";
    if (kind === "edge") return "oklch(0.55 0.12 260)";
    return "oklch(0.65 0.18 260)";
  }, []);

  const defaultViewport = useMemo(() => ({ x: 50, y: 50, zoom: 0.85 }), []);

  return (
    <div className="w-full h-full relative">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange as OnNodesChange}
        onEdgesChange={onEdgesChange}
        onConnect={onConnect}
        onNodeClick={onNodeClick}
        onNodeDoubleClick={onNodeDoubleClick}
        onEdgeClick={onEdgeClick}
        onPaneClick={onPaneClick}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        defaultViewport={defaultViewport}
        fitView
        fitViewOptions={{ padding: 0.2 }}
        proOptions={{ hideAttribution: true }}
        className="bg-[var(--background)]"
        snapToGrid
        snapGrid={[16, 16]}
        minZoom={0.1}
        maxZoom={3}
      >
        <Background
          variant={BackgroundVariant.Dots}
          gap={24}
          size={1.5}
          color="oklch(0.30 0.015 260)"
        />
        <Controls
          showInteractive={false}
          position="bottom-left"
          className="!rounded-lg !overflow-hidden !border !border-[var(--border)]"
        />
        <MiniMap
          nodeColor={nodeColor}
          maskColor="oklch(0.145 0.012 260 / 0.8)"
          position="bottom-right"
          className="!rounded-lg"
          pannable
          zoomable
        />

        {/* M1: 包折叠工具栏 */}
        {packages.length > 0 && (
          <Panel position="top-left" className="flex flex-col gap-1.5">
            <div className="flex items-center gap-1 bg-[var(--card)]/80 backdrop-blur-sm border border-[var(--border)] rounded-md px-2 py-1">
              <Layers className="w-3.5 h-3.5 text-[var(--primary)]" />
              <span className="text-[11px] font-mono text-[var(--muted-foreground)]">
                Packages ({packages.length})
              </span>
              <button
                onClick={expandAllPackages}
                className="ml-1 text-[10px] px-1.5 py-0.5 rounded hover:bg-[var(--accent)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
                title="展开所有包"
              >
                展开
              </button>
              <button
                onClick={collapseAllPackages}
                className="text-[10px] px-1.5 py-0.5 rounded hover:bg-[var(--accent)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
                title="折叠所有包"
              >
                折叠
              </button>
            </div>
          </Panel>
        )}

        {/* Status bar */}
        <Panel position="top-right" className="flex items-center gap-1">
          <span className="text-[10px] text-[var(--muted-foreground)] font-mono mr-2">
            {nodes.length}/{flowNodes.length} nodes · {edges.length}/{flowEdges.length} edges
            {packages.length > 0 && ` · ${packages.length} packages`}
          </span>
        </Panel>

        {/* Zoom level indicator */}
        <ZoomIndicator />
      </ReactFlow>

      {nodes.length === 0 && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none">
          <div className="text-center space-y-2">
            <div className="text-4xl text-[var(--muted-foreground)]/20 font-mono">{"{ }"}</div>
            <p className="text-sm text-[var(--muted-foreground)]/40">
              Write code in the editor to see the graph
            </p>
          </div>
        </div>
      )}
    </div>
  );
}

function ZoomIndicator() {
  const { getZoom } = useReactFlow();
  const [zoom, setZoom] = useState(85);

  useEffect(() => {
    const interval = setInterval(() => {
      setZoom(Math.round(getZoom() * 100));
    }, 200);
    return () => clearInterval(interval);
  }, [getZoom]);

  return (
    <Panel
      position="bottom-left"
      className="!mb-12 !ml-2"
    >
      <div className="text-[9px] font-mono text-[var(--muted-foreground)]/50 bg-[var(--card)]/60 backdrop-blur-sm px-1.5 py-0.5 rounded border border-[var(--border)]/50">
        {zoom}%
      </div>
    </Panel>
  );
}
