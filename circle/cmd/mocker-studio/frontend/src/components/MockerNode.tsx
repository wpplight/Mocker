import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { cn } from "../lib/utils";
import { Box, ArrowRightLeft, Variable, Database, Equal, ArrowRight, GitBranch, Minimize2, Maximize2, Package as PackageIcon } from "lucide-react";
import { useEditorStore } from "../store/editor";

interface MemberData {
  kind: string;
  name: string;
  type?: string;
  value?: string;
}

interface MockerNodeData {
  label: string;
  qualified?: string;
  kind: string;
  exported: boolean;
  pkg?: string;
  isBoundary?: boolean;
  _collapsed?: boolean;
  members: MemberData[];
  [key: string]: unknown;
}

const kindColors: Record<string, { border: string; header: string; badge: string; glow: string }> = {
  node: {
    border: "border-[var(--primary)]/40",
    header: "bg-[var(--primary)]/15",
    badge: "bg-[var(--primary)]/20 text-[var(--primary)]",
    glow: "shadow-[0_0_12px_-2px_oklch(0.65_0.18_260_/_0.15)]",
  },
  struct: {
    border: "border-[var(--port-out)]/40",
    header: "bg-[var(--port-out)]/15",
    badge: "bg-[var(--port-out)]/20 text-[var(--port-out)]",
    glow: "shadow-[0_0_12px_-2px_oklch(0.65_0.18_150_/_0.15)]",
  },
  edge: {
    border: "border-[var(--edge-color)]/40",
    header: "bg-[var(--edge-color)]/15",
    badge: "bg-[var(--edge-color)]/20 text-[var(--edge-color)]",
    glow: "shadow-[0_0_12px_-2px_oklch(0.55_0.12_260_/_0.15)]",
  },
};

const kindLabels: Record<string, string> = {
  node: "Node",
  struct: "Struct",
  edge: "Edge",
};

type MemberGroup = {
  kind: string;
  members: MemberData[];
};

function groupMembers(members: MemberData[]): MemberGroup[] {
  const groups: MemberGroup[] = [];
  let currentKind = "";
  for (const m of members) {
    if (m.kind !== currentKind) {
      groups.push({ kind: m.kind, members: [m] });
      currentKind = m.kind;
    } else {
      groups[groups.length - 1].members.push(m);
    }
  }
  return groups;
}

function MockerNodeInner({ id, data, selected }: NodeProps) {
  const nodeData = data as unknown as MockerNodeData;
  const { label, qualified, kind, exported, members = [], pkg, isBoundary, _collapsed } = nodeData;
  const colors = kindColors[kind] || kindColors.node;
  const toggleNodeCollapse = useEditorStore((s) => s.toggleNodeCollapse);

  // M1.x: dataflow 端口识别
  //
  // 任何"能被数据流边连出"的成员都需要 port-out-X handle：
  //   - port_in     → 不连出，只连入（保持左侧 handle 即可）
  //   - flow        → 显式 flow 声明（>>str out_str），连出
  //   - var         → 变量可作为数据流源（h := "hello"，hello.h → world.words）
  //   - field       → struct 字段可作为数据流源
  //
  // 只看"连出"侧：连入侧只有 port_in 一种，已经在 Left 侧 handle 了。
  const portsIn = members.filter((m) => m.kind === "port_in");
  const portsOut = members.filter(
    (m) => m.kind === "flow" || m.kind === "var" || m.kind === "field" || m.kind === "port_out"
  );
  const vars = members.filter((m) => m.kind === "var");
  const fields = members.filter((m) => m.kind === "field");
  const subInstances = members.filter((m) => m.kind === "sub_instance");
  const subEdges = members.filter((m) => m.kind === "sub_edge");
  const assigns = members.filter((m) => m.kind === "assign");
  const flowTargets = members.filter((m) => m.kind === "flow_target");
  const controls = members.filter((m) => m.kind === "control");
  const edgeConns = members.filter((m) => m.kind === "edge_conn");

  const allDisplayMembers = [...vars, ...fields, ...controls, ...subInstances, ...subEdges, ...edgeConns, ...flowTargets, ...assigns];
  const groupedMembers = groupMembers(allDisplayMembers);

  // M1: 折叠态 —— 只显 pkg + name + IO 计数
  if (_collapsed) {
    return (
      <div
        className={cn(
          "w-auto min-w-[160px] max-w-[220px] rounded-md border bg-[var(--node-bg)]/90 shadow-md transition-all duration-200 group",
          colors.border,
          selected && "shadow-[0_0_0_2px_var(--primary)] ring-1 ring-[var(--primary)]/50"
        )}
      >
        {/* Top handles for lifecycle edges (source & target) */}
        <Handle
          type="source"
          position={Position.Top}
          id="lifecycle-out"
          className="!w-2 !h-2 !min-w-0 !min-h-0 !bg-green-500 !border-green-400"
          style={{ top: -4 }}
        />
        <Handle
          type="target"
          position={Position.Top}
          id="lifecycle-in"
          className="!w-2 !h-2 !min-w-0 !min-h-0 !bg-green-500 !border-green-400"
          style={{ top: -4 }}
        />
        <div
          className={cn(
            "flex items-center gap-1.5 px-2 py-1.5 rounded-[calc(var(--radius)-2px)]",
            colors.header
          )}
        >
          {pkg && pkg !== "" && pkg !== "main" && (
            <span className="inline-flex items-center gap-0.5 text-[9px] font-mono text-[var(--primary)] opacity-80 bg-[var(--primary)]/10 px-1 py-0.5 rounded">
              <PackageIcon className="w-2.5 h-2.5" />
              {pkg}
            </span>
          )}
          {exported && <span className="text-[9px] font-mono text-[var(--primary)] opacity-70">@</span>}
          <span className="text-xs font-semibold text-[var(--foreground)] truncate">
            {label}
          </span>
          <button
            onClick={(e) => {
              e.stopPropagation();
              toggleNodeCollapse(id);
            }}
            className="ml-auto p-0.5 rounded hover:bg-[var(--accent)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            title="展开节点"
          >
            <Maximize2 className="w-3 h-3" />
          </button>
        </div>
        <div className="px-2 py-1 flex items-center gap-2 text-[9px] font-mono text-[var(--muted-foreground)] border-t border-[var(--border)]/30">
          {/* Left handles for dataflow in */}
          {portsIn.map((m) => (
            <Handle
              key={`in-${m.name}`}
              type="target"
              position={Position.Left}
              id={`port-in-${m.name}`}
              className="!relative !left-0 !top-0 !translate-x-0 !translate-y-0 !w-1.5 !h-1.5 !min-w-0 !min-h-0 !bg-[var(--port-in)] !border-[var(--port-in)]"
            />
          ))}
          <span className="inline-flex items-center gap-0.5">
            <ArrowRight className="w-2.5 h-2.5 text-[var(--port-in)]" />
            {portsIn.length} in
          </span>
          <span className="inline-flex items-center gap-0.5">
            {portsOut.length} out
            <ArrowRight className="w-2.5 h-2.5 text-[var(--port-out)]" />
          </span>
          {/* Right handles for dataflow out */}
          {portsOut.map((m) => (
            <Handle
              key={`out-${m.name}`}
              type="source"
              position={Position.Right}
              id={`port-out-${m.name}`}
              className="!relative !right-0 !top-0 !translate-x-0 !translate-y-0 !w-1.5 !h-1.5 !min-w-0 !min-h-0 !bg-[var(--port-out)] !border-[var(--port-out)]"
            />
          ))}
          {isBoundary && (
            <span className="ml-auto text-[var(--primary)] opacity-70" title="跨包节点（包折叠时仍显示）">
              ⇄
            </span>
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      className={cn(
        "w-auto min-w-[200px] max-w-[320px] rounded-lg border-2 bg-[var(--node-bg)] shadow-lg transition-all duration-200",
        colors.border,
        colors.glow,
        selected && "shadow-[0_0_0_2px_var(--primary)] ring-1 ring-[var(--primary)]/50"
      )}
    >
      {/* Top handles for lifecycle edges */}
      <Handle
        type="source"
        position={Position.Top}
        id="lifecycle-out"
        className="!w-2 !h-2 !min-w-0 !min-h-0 !bg-green-500 !border-green-400"
        style={{ top: -4 }}
      />
      <Handle
        type="target"
        position={Position.Top}
        id="lifecycle-in"
        className="!w-2 !h-2 !min-w-0 !min-h-0 !bg-green-500 !border-green-400"
        style={{ top: -4 }}
      />
      {/* Header with glow */}
      <div
        className={cn(
          "flex items-center gap-2 px-3 py-2 rounded-t-[calc(var(--radius)-2px)]",
          colors.header,
          "shadow-[inset_0_1px_0_0_rgba(255,255,255,0.05)]"
        )}
      >
        {pkg && pkg !== "" && pkg !== "main" && (
          <span className="inline-flex items-center gap-0.5 text-[9px] font-mono text-[var(--primary)] opacity-80 bg-[var(--primary)]/10 px-1 py-0.5 rounded shrink-0">
            <PackageIcon className="w-2.5 h-2.5" />
            {pkg}
          </span>
        )}
        {exported && (
          <span className="text-[10px] font-mono text-[var(--primary)] opacity-70">@</span>
        )}
        <span
          className="text-sm font-semibold text-[var(--foreground)] tracking-tight truncate"
          title={qualified || label}
        >
          {label}
        </span>
        <span
          className={cn(
            "ml-auto text-[10px] font-medium px-1.5 py-0.5 rounded-full",
            colors.badge
          )}
        >
          {kindLabels[kind] || kind}
        </span>
        <button
          onClick={(e) => {
            e.stopPropagation();
            toggleNodeCollapse(id);
          }}
          className="p-0.5 rounded hover:bg-[var(--accent)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
          title="折叠节点（隐藏内部细节）"
        >
          <Minimize2 className="w-3 h-3" />
        </button>
      </div>

      {/* Ports In */}
      {portsIn.length > 0 && (
        <div className="px-3 py-1.5 border-t border-[var(--border)]/50">
          {portsIn.map((m) => (
            <div key={m.name} className="relative flex items-center gap-1.5 py-0.5">
              <Handle
                type="target"
                position={Position.Left}
                id={`port-in-${m.name}`}
                className="!relative !left-0 !top-0 !translate-x-0 !translate-y-0 !w-2 !h-2 !min-w-0 !min-h-0 !bg-[var(--port-in)] !border-[var(--port-in)] group-hover:!w-3 group-hover:!h-3 transition-all duration-150"
              />
              <span className="text-[10px] text-[var(--port-in)] font-mono opacity-60 flex items-center gap-0.5">
                <ArrowRight className="w-2.5 h-2.5" />
                IN
              </span>
              <span className="text-xs text-[var(--muted-foreground)] font-mono">{m.type}</span>
              <span className="text-xs text-[var(--foreground)] font-medium">{m.name}</span>
            </div>
          ))}
        </div>
      )}

      {/* Body members grouped by kind */}
      {groupedMembers.length > 0 && (
        <div className="border-t border-[var(--border)]/30">
          {groupedMembers.map((group, gi) => (
            <div key={`${group.kind}-${gi}`}>
              {gi > 0 && (
                <div className="mx-3 border-t border-[var(--border)]/20" />
              )}
              <div className="px-3 py-1.5 space-y-0.5">
                {group.members.map((m) => (
                  <MemberRow key={m.name} member={m} />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Ports Out —— dataflow 源（var / field / flow / port_out 都能从这里连出） */}
      {portsOut.length > 0 && (
        <div className="px-3 py-1.5 border-t border-[var(--border)]/50">
          {portsOut.map((m) => (
            <div key={m.name} className="relative flex items-center justify-end gap-1.5 py-0.5">
              <span className="text-xs text-[var(--foreground)] font-medium">{m.name}</span>
              <span className="text-[10px] text-[var(--port-out)] font-mono opacity-60 flex items-center gap-0.5">
                {m.kind === "var" ? (
                  <>
                    <Variable className="w-2.5 h-2.5" />
                    VAR
                    <ArrowRight className="w-2.5 h-2.5" />
                  </>
                ) : m.kind === "field" ? (
                  <>
                    <Database className="w-2.5 h-2.5" />
                    FLD
                    <ArrowRight className="w-2.5 h-2.5" />
                  </>
                ) : m.value ? (
                  `= ${m.value}`
                ) : (
                  <>
                    OUT
                    <ArrowRight className="w-2.5 h-2.5" />
                  </>
                )}
              </span>
              <Handle
                type="source"
                position={Position.Right}
                id={`port-out-${m.name}`}
                className="!relative !right-0 !top-0 !translate-x-0 !translate-y-0 !w-2 !h-2 !min-w-0 !min-h-0 !bg-[var(--port-out)] !border-[var(--port-out)] group-hover:!w-3 group-hover:!h-3 transition-all duration-150"
              />
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function MemberRow({ member }: { member: MemberData }) {
  const m = member;
  return (
    <div className="flex items-center gap-1.5 py-0.5">
      <MemberBadge kind={m.kind} />
      <span className="text-xs text-[var(--foreground)] font-mono truncate flex items-center gap-1">
        {m.kind === "sub_instance" && (
          <>
            <span className="text-[var(--primary)] font-medium">{m.type}</span>
            <span>{m.name}</span>
          </>
        )}
        {m.kind === "sub_edge" && <SubEdgeName name={m.name} />}
        {m.kind === "flow" && (
          <>
            <span>{m.name}</span>
            <ArrowRight className="w-3 h-3 text-[var(--port-out)] shrink-0" />
            <span className="text-[10px] text-[var(--port-out)] opacity-60">{">>"}</span>
          </>
        )}
        {m.kind === "flow_target" && (
          <>
            <span>{m.name}</span>
            <ArrowRight className="w-3 h-3 text-[var(--port-out)] shrink-0" />
            <span className="text-[var(--port-out)]">{m.value}</span>
          </>
        )}
        {m.kind === "edge_conn" && <SubEdgeName name={m.name} />}
        {m.kind === "control" && (
          <>
            <span className="text-[var(--primary)] font-medium">{m.name}</span>
            <span className="text-[var(--muted-foreground)]">({m.value})</span>
          </>
        )}
        {m.kind !== "sub_instance" && m.kind !== "sub_edge" && m.kind !== "flow" && m.kind !== "flow_target" && m.kind !== "edge_conn" && m.kind !== "control" && (
          <>
            {m.type && <span className="text-[var(--muted-foreground)]">{m.type} </span>}
            <span>{m.name}</span>
          </>
        )}
        {m.value && (
          <span className="text-[var(--muted-foreground)]"> = {m.value}</span>
        )}
      </span>
    </div>
  );
}

function SubEdgeName({ name }: { name: string }) {
  // Parse "src <edge> dst" and highlight the <> brackets
  const match = name.match(/^(.+?)\s*<(.+?)>\s*(.+)$/);
  if (match) {
    return (
      <>
        <span>{match[1]}</span>
        <span className="text-[var(--edge-color)]">{"<"}</span>
        <span className="text-[var(--edge-color)] font-medium">{match[2]}</span>
        <span className="text-[var(--edge-color)]">{">"}</span>
        <span>{match[3]}</span>
      </>
    );
  }
  return <span>{name}</span>;
}

function MemberBadge({ kind }: { kind: string }) {
  const config: Record<string, { icon: React.ReactNode; cls: string }> = {
    var: { icon: <Variable className="w-2.5 h-2.5" />, cls: "text-[var(--primary)]/70" },
    field: { icon: <Database className="w-2.5 h-2.5" />, cls: "text-[var(--port-out)]/70" },
    sub_instance: { icon: <Box className="w-2.5 h-2.5" />, cls: "text-[var(--edge-color)]/70" },
    sub_edge: { icon: <ArrowRightLeft className="w-2.5 h-2.5" />, cls: "text-[var(--edge-color)]/70" },
    assign: { icon: <Equal className="w-2.5 h-2.5" />, cls: "text-[var(--muted-foreground)]/50" },
    instance: { icon: <Box className="w-2.5 h-2.5" />, cls: "text-[var(--edge-color)]/70" },
    edge_conn: { icon: <ArrowRightLeft className="w-2.5 h-2.5" />, cls: "text-[var(--edge-color)]/70" },
    flow_target: { icon: <ArrowRight className="w-2.5 h-2.5" />, cls: "text-[var(--port-out)]/70" },
    control: { icon: <GitBranch className="w-2.5 h-2.5" />, cls: "text-[var(--primary)]/70" },
  };
  const c = config[kind] || { icon: <span className="text-[9px]">?</span>, cls: "text-[var(--muted-foreground)]" };
  return (
    <span className={cn("w-3.5 flex items-center justify-center shrink-0", c.cls)}>
      {c.icon}
    </span>
  );
}

export const MockerNode = memo(MockerNodeInner);
