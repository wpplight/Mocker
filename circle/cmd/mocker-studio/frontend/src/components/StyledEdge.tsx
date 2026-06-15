import {
  BaseEdge,
  EdgeLabelRenderer,
  getSmoothStepPath,
  type EdgeProps,
} from "@xyflow/react";

type EdgeKind = "lifecycle" | "dataflow" | "topology" | "flow";

const kindStyles: Record<EdgeKind, { color: string; width: number; dash: string; animated: boolean }> = {
  // 节点变量生命周期（创建 / 归属）—— 绿色，醒目，动画
  lifecycle: {
    color: "#22c55e",   // 绿色
    width: 3,
    dash: "6 3",
    animated: true,
  },
  // 数据流（runtime data：父节点 attr → 子节点 input）—— 蓝色，实线
  dataflow: {
    color: "#3b82f6",   // 蓝色
    width: 2,
    dash: "none",
    animated: false,
  },
  // 边声明（main 包里的 <edge> 边）—— 浅蓝，虚线
  topology: {
    color: "#60a5fa",   // 浅蓝
    width: 1.5,
    dash: "8 4",
    animated: false,
  },
  // 块调用（block.Flow 节点内部流到外部）—— 蓝色，实线
  flow: {
    color: "#3b82f6",   // 蓝色
    width: 2,
    dash: "none",
    animated: false,
  },
};

export function StyledEdge({
  id,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
  data,
  label,
  selected,
  markerEnd,
}: EdgeProps) {
  const kind = ((data as Record<string, unknown>)?.kind as EdgeKind) ?? "flow";
  const edgeName = (data as Record<string, unknown>)?.edgeName as string | undefined;
  const displayLabel = edgeName || (typeof label === "string" ? label : undefined);

  const style = kindStyles[kind] ?? kindStyles.flow;

  const [edgePath, labelX, labelY] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 12,
  });

  // 标签配色：按 kind 区分（绿色 / 蓝色 / 浅蓝）
  // 选中态：className 上的 !text-[primary] 覆盖 inline style
  const labelStyle: React.CSSProperties | undefined = (() => {
    if (selected) return undefined;
    if (kind === "lifecycle") {
      return { color: "#22c55e", borderColor: "rgba(34,197,94,0.4)" };
    }
    if (kind === "topology") {
      return { color: "#60a5fa", borderColor: "rgba(96,165,250,0.4)" };
    }
    // dataflow / flow
    return { color: "#3b82f6", borderColor: "rgba(59,130,246,0.4)" };
  })();

  return (
    <>
      <BaseEdge
        id={id}
        path={edgePath}
        markerEnd={markerEnd}
        style={{
          stroke: selected ? "var(--primary)" : style.color,
          strokeWidth: selected ? style.width + 1 : style.width,
          strokeDasharray: style.dash,
          ...(style.animated
            ? { animation: "dashdraw 0.6s linear infinite" }
            : {}),
          transition: "stroke 0.15s, stroke-width 0.15s",
        }}
      />
      {displayLabel && (
        <EdgeLabelRenderer>
          <div
            style={{
              position: "absolute",
              transform: `translate(-50%, -50%) translate(${labelX}px,${labelY}px)`,
              pointerEvents: "all",
            }}
            className="edge-label-pill"
          >
            <span
              className={`
                text-[10px] font-medium font-mono px-2 py-0.5 rounded-full
                bg-[var(--card)] text-[var(--muted-foreground)]
                border border-[var(--border)] shadow-sm
                whitespace-nowrap
                transition-colors duration-150
                hover:text-[var(--foreground)] hover:border-[var(--primary)]/40
                ${selected ? "!text-[var(--primary)] !border-[var(--primary)]/50" : ""}
              `}
              style={labelStyle}
            >
              {kind === "lifecycle" ? "✦ " : ""}{displayLabel}
            </span>
          </div>
        </EdgeLabelRenderer>
      )}
    </>
  );
}
