import { useEditorStore } from "../store/editor";
import { cn } from "../lib/utils";
import { ScrollArea } from "./ui/scroll-area";
import { Separator } from "./ui/separator";
import { X, ArrowDown, Package } from "lucide-react";
import { Button } from "./ui/button";
import type { Edge as RFEdge } from "@xyflow/react";

export function PropertiesPanel() {
  const {
    propertiesOpen,
    selectedNodeDetail,
    selectedEdgeId,
    flowEdges,
    setPropertiesOpen,
    setSelectedNodeId,
    setSelectedEdgeId,
  } = useEditorStore();

  if (!propertiesOpen) return null;

  // 边详情模式
  if (selectedEdgeId && !selectedNodeDetail) {
    const edge = flowEdges.find((e) => e.id === selectedEdgeId);
    if (!edge) return null;
    return (
      <EdgeDetailView
        edge={edge}
        onClose={() => {
          setSelectedEdgeId(null);
          setPropertiesOpen(false);
        }}
      />
    );
  }

  // 节点详情模式
  if (!selectedNodeDetail) return null;
  const detail = selectedNodeDetail;

  return (
    <div className="w-72 h-full border-l border-[var(--border)] bg-[var(--card)]/40 flex flex-col shrink-0">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-[var(--border)]">
        <div className="flex items-center gap-2">
          {detail.exported && (
            <span className="text-[10px] font-mono text-[var(--primary)]">@</span>
          )}
          <span className="text-sm font-semibold text-[var(--foreground)]">{detail.name}</span>
          <span
            className={cn(
              "text-[10px] px-1.5 py-0.5 rounded-full font-medium",
              detail.kind === "node"
                ? "bg-[var(--primary)]/20 text-[var(--primary)]"
                : detail.kind === "struct"
                ? "bg-[var(--port-out)]/20 text-[var(--port-out)]"
                : "bg-[var(--edge-color)]/20 text-[var(--edge-color)]"
            )}
          >
            {detail.kind}
          </span>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="h-6 w-6"
          onClick={() => {
            setSelectedNodeId(null);
            setPropertiesOpen(false);
          }}
        >
          <X className="w-3.5 h-3.5" />
        </Button>
      </div>

      <ScrollArea className="flex-1">
        <div className="p-3 space-y-4">
          {/* Ports In */}
          {detail.members.filter((m) => m.kind === "port_in").length > 0 && (
            <PropertySection title="Input Ports" color="text-[var(--port-in)]">
              {detail.members
                .filter((m) => m.kind === "port_in")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--port-in)] text-[10px] font-mono">IN</span>
                    <span className="text-xs font-mono text-[var(--muted-foreground)]">
                      {m.type}
                    </span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Variables */}
          {detail.members.filter((m) => m.kind === "var").length > 0 && (
            <PropertySection title="Variables" color="text-[var(--primary)]">
              {detail.members
                .filter((m) => m.kind === "var")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--primary)] text-[10px] font-mono">:=</span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                    {m.value && (
                      <span className="text-xs font-mono text-[var(--muted-foreground)] truncate">
                        = {m.value}
                      </span>
                    )}
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Fields */}
          {detail.members.filter((m) => m.kind === "field").length > 0 && (
            <PropertySection title="Fields" color="text-[var(--port-out)]">
              {detail.members
                .filter((m) => m.kind === "field")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--port-out)] text-[10px] font-mono">T</span>
                    <span className="text-xs font-mono text-[var(--muted-foreground)]">
                      {m.type}
                    </span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Sub-instances */}
          {detail.members.filter((m) => m.kind === "sub_instance").length > 0 && (
            <PropertySection title="Sub-instances" color="text-[var(--edge-color)]">
              {detail.members
                .filter((m) => m.kind === "sub_instance")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--edge-color)] text-[10px] font-mono">i</span>
                    <span className="text-xs font-mono text-[var(--muted-foreground)]">
                      {m.type}
                    </span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Sub-edges */}
          {detail.members.filter((m) => m.kind === "sub_edge").length > 0 && (
            <PropertySection title="Sub-edges" color="text-[var(--edge-color)]">
              {detail.members
                .filter((m) => m.kind === "sub_edge")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--edge-color)] text-[10px] font-mono">~</span>
                    <span className="text-xs font-mono text-[var(--foreground)]">{m.name}</span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Flows */}
          {detail.members.filter((m) => m.kind === "flow").length > 0 && (
            <PropertySection title="Output Flows" color="text-[var(--port-out)]">
              {detail.members
                .filter((m) => m.kind === "flow")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--port-out)] text-[10px] font-mono">OUT</span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Flow targets */}
          {detail.members.filter((m) => m.kind === "flow_target").length > 0 && (
            <PropertySection title="Data Flows" color="text-[var(--port-out)]">
              {detail.members
                .filter((m) => m.kind === "flow_target")
                .map((m) => (
                  <PropertyRow key={`${m.name}-${m.value}`}>
                    <span className="text-[var(--port-out)] text-[10px] font-mono">→</span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                    <span className="text-[var(--port-out)]">→</span>
                    <span className="text-xs font-mono text-[var(--muted-foreground)] truncate">
                      {m.value}
                    </span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Control flow */}
          {detail.members.filter((m) => m.kind === "control").length > 0 && (
            <PropertySection title="Control Flow" color="text-[var(--primary)]">
              {detail.members
                .filter((m) => m.kind === "control")
                .map((m) => (
                  <PropertyRow key={`${m.name}-${m.value}`}>
                    <span className="text-[var(--primary)] text-[10px] font-mono font-semibold">
                      {m.name}
                    </span>
                    <span className="text-xs font-mono text-[var(--muted-foreground)] truncate">
                      ({m.value})
                    </span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Edge connections */}
          {detail.members.filter((m) => m.kind === "edge_conn").length > 0 && (
            <PropertySection title="Edge Connections" color="text-[var(--edge-color)]">
              {detail.members
                .filter((m) => m.kind === "edge_conn")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--edge-color)] text-[10px] font-mono">⚡</span>
                    <span className="text-xs font-mono text-[var(--foreground)]">{m.name}</span>
                  </PropertyRow>
                ))}
            </PropertySection>
          )}

          {/* Assigns */}
          {detail.members.filter((m) => m.kind === "assign").length > 0 && (
            <PropertySection title="Assignments" color="text-[var(--muted-foreground)]">
              {detail.members
                .filter((m) => m.kind === "assign")
                .map((m) => (
                  <PropertyRow key={m.name}>
                    <span className="text-[var(--muted-foreground)] text-[10px] font-mono">=</span>
                    <span className="text-xs font-mono font-medium text-[var(--foreground)]">
                      {m.name}
                    </span>
                    {m.value && (
                      <span className="text-xs font-mono text-[var(--muted-foreground)] truncate">
                        = {m.value}
                      </span>
                    )}
                  </PropertyRow>
                ))}
            </PropertySection>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}

function PropertySection({
  title,
  color,
  children,
}: {
  title: string;
  color: string;
  children: React.ReactNode;
}) {
  return (
    <div className="space-y-1.5">
      <div className={cn("text-[10px] font-medium uppercase tracking-wider", color, "opacity-60")}>
        {title}
      </div>
      <div className="space-y-0.5">{children}</div>
      <Separator className="mt-2 opacity-30" />
    </div>
  );
}

function PropertyRow({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2 pl-2 py-0.5 rounded hover:bg-[var(--accent)]/30 transition-colors">
      {children}
    </div>
  );
}

// 边详情视图 —— 点边查流动
function EdgeDetailView({ edge, onClose }: { edge: RFEdge; onClose: () => void }) {
  const { flowNodes } = useEditorStore();
  const data = (edge.data as Record<string, unknown>) ?? {};
  const kind = (data.kind as string) ?? "flow";
  const edgeName = (data.edgeName as string) ?? "";
  const isRedirected = Boolean(data._redirected);

  const srcNode = flowNodes.find((n) => n.id === edge.source);
  const dstNode = flowNodes.find((n) => n.id === edge.target);
  const srcName = srcNode
    ? ((srcNode.data as Record<string, unknown> | undefined)?.qualified as string) || srcNode.id
    : edge.source;
  const dstName = dstNode
    ? ((dstNode.data as Record<string, unknown> | undefined)?.qualified as string) || dstNode.id
    : edge.target;

  const kindMeta = {
    lifecycle: { label: "生命周期边", color: "#22c55e", desc: "父→子 创建关系；节点变量归这条边所有。" },
    dataflow: { label: "数据流边", color: "#3b82f6", desc: "运行时数据：父节点 attr → 子节点 input。" },
    topology: { label: "拓扑边", color: "#60a5fa", desc: "main 包内声明的 <edge> 边。" },
    flow: { label: "块调用边", color: "#3b82f6", desc: "block.Flow：节点内部输出 → 外部目标。" },
  }[kind as string] ?? { label: "边", color: "var(--muted-foreground)", desc: "" };

  const srcAttr = (edge.sourceHandle ?? "").toString();
  const dstAttr = (edge.targetHandle ?? "").toString();

  return (
    <div className="w-72 h-full border-l border-[var(--border)] bg-[var(--card)]/40 flex flex-col shrink-0">
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2 border-b border-[var(--border)]">
        <div className="flex items-center gap-2">
          <span
            className="inline-block w-2.5 h-2.5 rounded-full"
            style={{ background: kindMeta.color }}
          />
          <span className="text-sm font-semibold text-[var(--foreground)]">
            {edgeName || "(未命名边)"}
          </span>
          <span
            className="text-[10px] px-1.5 py-0.5 rounded-full font-medium"
            style={{
              background: `${kindMeta.color}22`,
              color: kindMeta.color,
            }}
          >
            {kindMeta.label}
          </span>
        </div>
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onClose}>
          <X className="w-3.5 h-3.5" />
        </Button>
      </div>

      <ScrollArea className="flex-1">
        <div className="p-3 space-y-4">
          {/* 边定义描述 */}
          <div className="text-[10px] text-[var(--muted-foreground)] leading-relaxed">
            {kindMeta.desc}
          </div>

          {/* 流动方向 */}
          <div className="space-y-2">
            <div className="text-[10px] font-medium uppercase tracking-wider text-[var(--muted-foreground)] opacity-60">
              数据流动
            </div>
            <div className="rounded-md border border-[var(--border)] bg-[var(--background)]/40 px-2 py-1.5 space-y-1.5 font-mono text-[11px]">
              <div className="flex items-center gap-2">
                <span className="text-[10px] text-[var(--muted-foreground)] opacity-60">FROM</span>
                <span className="text-[var(--foreground)] font-semibold">{srcName}</span>
                {srcAttr && (
                  <span className="text-[10px] px-1 py-0.5 rounded bg-[var(--primary)]/10 text-[var(--primary)]">
                    .{srcAttr}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2 pl-3 text-[var(--primary)]">
                <ArrowDown className="w-3 h-3" />
                <span className="text-[9px]">
                  {kind === "lifecycle" ? "creates" : kind === "dataflow" ? "dataflow" : kind}
                </span>
              </div>
              <div className="flex items-center gap-2">
                <span className="text-[10px] text-[var(--muted-foreground)] opacity-60">TO</span>
                <span className="text-[var(--foreground)] font-semibold">{dstName}</span>
                {dstAttr && (
                  <span className="text-[10px] px-1 py-0.5 rounded bg-[var(--port-in)]/10 text-[var(--port-in)]">
                    .{dstAttr}
                  </span>
                )}
              </div>
            </div>
          </div>

          {/* 跨包提示 */}
          {isRedirected && (
            <div className="rounded-md border border-dashed border-[var(--primary)]/40 bg-[var(--primary)]/5 px-2 py-1.5 text-[10px] text-[var(--primary)] flex items-center gap-1.5">
              <Package className="w-3 h-3" />
              <span>该边指向已折叠的包 —— 点击包容器可展开</span>
            </div>
          )}

          {/* 原始 ID（debug 用） */}
          <div className="space-y-1.5">
            <div className="text-[10px] font-medium uppercase tracking-wider text-[var(--muted-foreground)] opacity-60">
              Edge ID
            </div>
            <div className="text-[10px] font-mono text-[var(--muted-foreground)] break-all">
              {edge.id}
            </div>
          </div>

          {/* 跨包标识 */}
          {Boolean(data.crossPackage) && (
            <div className="rounded-md border border-[var(--primary)]/30 bg-[var(--primary)]/10 px-2 py-1.5 text-[10px] flex items-center gap-1.5">
              <span className="text-[var(--primary)]">⇄</span>
              <span>跨包边</span>
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
