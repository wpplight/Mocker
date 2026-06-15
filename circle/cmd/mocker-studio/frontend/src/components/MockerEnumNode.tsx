import { memo } from "react";
import { Handle, Position, type NodeProps } from "@xyflow/react";
import { cn } from "../lib/utils";

interface MockerEnumData {
  label: string;
  values: string[];
  [key: string]: unknown;
}

function MockerEnumNodeInner({ data, selected }: NodeProps) {
  const { label, values = [] } = data as unknown as MockerEnumData;

  return (
    <div
      className={cn(
        "min-w-[160px] max-w-[240px] rounded-lg border-2 border-[var(--muted-foreground)]/30 bg-[var(--node-bg)] shadow-lg transition-shadow",
        selected && "shadow-[0_0_0_2px_var(--primary)] ring-1 ring-[var(--primary)]/50"
      )}
    >
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-2 rounded-t-[calc(var(--radius)-2px)] bg-[var(--muted-foreground)]/10">
        <span className="text-[10px] font-mono text-[var(--muted-foreground)] opacity-50">enum</span>
        <span className="text-sm font-semibold text-[var(--foreground)] tracking-tight">
          {label}
        </span>
      </div>

      {/* Values */}
      <div className="px-3 py-1.5 border-t border-[var(--border)]/30">
        {values.map((v) => (
          <div key={v} className="flex items-center gap-1.5 py-0.5">
            <span className="text-[10px] text-[var(--muted-foreground)] font-mono opacity-40">
              {"{ }"}
            </span>
            <span className="text-xs text-[var(--foreground)] font-mono">{v}</span>
          </div>
        ))}
      </div>

      <Handle type="target" position={Position.Left} className="!hidden" />
      <Handle type="source" position={Position.Right} className="!hidden" />
    </div>
  );
}

export const MockerEnumNode = memo(MockerEnumNodeInner);
