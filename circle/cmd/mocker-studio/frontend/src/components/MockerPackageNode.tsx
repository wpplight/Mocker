import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { cn } from "../lib/utils";
import { Package, Box, Maximize2, ArrowRightLeft } from "lucide-react";
import { useEditorStore } from "../store/editor";

interface MockerPackageData {
  pkgName: string;
  nodeCount: number;
  boundaryCount: number;
  isMain: boolean;
  [key: string]: unknown;
}

// 颜色按包名 hash 到 hue —— 不同包用不同颜色但都偏冷色（不抢 lifecycle/dataflow 的绿/蓝）
function pkgColor(pkg: string): { border: string; header: string; badge: string } {
  let hash = 0;
  for (let i = 0; i < pkg.length; i++) hash = (hash * 31 + pkg.charCodeAt(i)) | 0;
  const hue = Math.abs(hash) % 360;
  return {
    border: `border-[oklch(0.55_0.12_${hue})]/50`,
    header: `bg-[oklch(0.55_0.12_${hue})]/15`,
    badge: `bg-[oklch(0.55_0.12_${hue})]/20 text-[oklch(0.65_0.14_${hue})]`,
  };
}

function MockerPackageNodeInner({ id, data, selected }: NodeProps) {
  const { pkgName, nodeCount = 0, boundaryCount = 0, isMain } = data as unknown as MockerPackageData;
  const togglePackageCollapse = useEditorStore((s) => s.togglePackageCollapse);
  const colors = pkgColor(pkgName || "");

  return (
    <div
      className={cn(
        "w-[200px] rounded-lg border-2 border-dashed bg-[var(--node-bg)]/60 shadow-md transition-all duration-200 group",
        colors.border,
        selected && "shadow-[0_0_0_2px_var(--primary)] ring-1 ring-[var(--primary)]/50"
      )}
    >
      {/* Top handles for lifecycle edges (in/out) */}
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
      {/* Left/right handles for dataflow routing through package */}
      <Handle
        type="target"
        position={Position.Left}
        id="port-in"
        className="!w-1.5 !h-1.5 !min-w-0 !min-h-0 !bg-[var(--port-in)] !border-[var(--port-in)]"
      />
      <Handle
        type="source"
        position={Position.Right}
        id="port-out"
        className="!w-1.5 !h-1.5 !min-w-0 !min-h-0 !bg-[var(--port-out)] !border-[var(--port-out)]"
      />

      <div
        className={cn(
          "flex items-center gap-2 px-3 py-2 rounded-t-[calc(var(--radius)-2px)]",
          colors.header
        )}
      >
        <Package className="w-4 h-4 opacity-70" />
        <span className="text-[10px] font-mono text-[var(--muted-foreground)] opacity-70">
          package
        </span>
        <span
          className={cn(
            "text-sm font-semibold tracking-tight truncate",
            isMain ? "text-[var(--primary)]" : "text-[var(--foreground)]"
          )}
          title={pkgName}
        >
          {pkgName || "(root)"}
        </span>
        {!isMain && (
          <button
            onClick={(e) => {
              e.stopPropagation();
              togglePackageCollapse(pkgName);
            }}
            className="ml-auto p-0.5 rounded hover:bg-[var(--accent)] text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
            title="展开包"
          >
            <Maximize2 className="w-3 h-3" />
          </button>
        )}
      </div>

      <div className="px-3 py-2 flex items-center gap-3 text-[10px] font-mono text-[var(--muted-foreground)] border-t border-[var(--border)]/30">
        <span className="inline-flex items-center gap-0.5">
          <Box className="w-2.5 h-2.5 opacity-70" />
          {nodeCount} 节点
        </span>
        {boundaryCount > 0 && (
          <span
            className={cn(
              "inline-flex items-center gap-0.5 px-1 rounded text-[9px]",
              colors.badge
            )}
            title="跨包节点（折叠时仍可被外部访问）"
          >
            <ArrowRightLeft className="w-2.5 h-2.5" />
            {boundaryCount}
          </span>
        )}
      </div>
    </div>
  );
}

export const MockerPackageNode = memo(MockerPackageNodeInner);
