import { useEffect, useRef } from "react";
import { useEditorStore } from "../store/editor";
import { ScrollArea } from "./ui/scroll-area";
import {
  Terminal,
  CheckCircle2,
  XCircle,
  Loader2,
  AlertTriangle,
  Trash2,
} from "lucide-react";

export function OutputPanel() {
  const {
    compileResult,
    isCompiling,
    runOutput,
    isRunning,
    diagnostics,
    clearRunOutput,
  } = useEditorStore();
  const scrollRef = useRef<HTMLDivElement>(null);

  // 自动滚动到底部
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [runOutput, compileResult?.output, compileResult?.error]);

  // M0.5: 任何状态下都显示 OutputPanel（流式输出 / 诊断 / 编译结果）
  const hasOutput =
    runOutput.length > 0 ||
    diagnostics.length > 0 ||
    compileResult !== null ||
    isCompiling ||
    isRunning;

  if (!hasOutput) return null;

  return (
    <div className="h-48 border-t border-[var(--border)] bg-[var(--card)]/40 flex flex-col shrink-0">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-1.5 border-b border-[var(--border)] bg-[var(--card)]/60">
        <Terminal className="w-3.5 h-3.5 text-[var(--muted-foreground)]" />
        <span className="text-xs font-medium text-[var(--muted-foreground)]">Output</span>
        <div className="flex-1" />
        {(isCompiling || isRunning) && (
          <div className="flex items-center gap-1.5">
            <Loader2 className="w-3 h-3 animate-spin text-[var(--primary)]" />
            <span className="text-[10px] text-[var(--primary)]">
              {isCompiling ? "Compiling..." : "Running..."}
            </span>
          </div>
        )}
        {!isCompiling && !isRunning && compileResult && (
          <div className="flex items-center gap-1.5">
            {compileResult.success ? (
              <>
                <CheckCircle2 className="w-3 h-3 text-emerald-500" />
                <span className="text-[10px] text-emerald-500">Success</span>
                {compileResult.exitCode !== 0 && (
                  <span className="text-[10px] text-[var(--muted-foreground)]">
                    (exit {compileResult.exitCode})
                  </span>
                )}
              </>
            ) : (
              <>
                <XCircle className="w-3 h-3 text-red-500" />
                <span className="text-[10px] text-red-500">Failed</span>
                {compileResult.exitCode !== 0 && (
                  <span className="text-[10px] text-[var(--muted-foreground)]">
                    (exit {compileResult.exitCode})
                  </span>
                )}
              </>
            )}
          </div>
        )}
        <button
          onClick={() => clearRunOutput()}
          className="ml-2 p-0.5 hover:bg-[var(--accent)]/50 rounded transition-colors"
          title="Clear output"
        >
          <Trash2 className="w-3 h-3 text-[var(--muted-foreground)]/50 hover:text-[var(--muted-foreground)]" />
        </button>
      </div>

      {/* Content */}
      <ScrollArea className="flex-1" ref={scrollRef}>
        <div className="p-3 text-xs font-mono leading-relaxed space-y-2">
          {/* M0.5: 诊断信息（编译/语义错误） */}
          {diagnostics.length > 0 && (
            <div className="space-y-1 pb-2 border-b border-[var(--border)]/50">
              {diagnostics.map((d, i) => (
                <div key={i} className="flex items-start gap-2">
                  <AlertTriangle className="w-3 h-3 text-amber-500 shrink-0 mt-0.5" />
                  <div className="flex-1">
                    {d.line > 0 && (
                      <span className="text-[var(--muted-foreground)]/60 mr-2">
                        {d.line}:{d.column}
                      </span>
                    )}
                    <span className="text-amber-300">{d.message}</span>
                    {d.hint && (
                      <div className="text-[var(--muted-foreground)]/70 text-[10px] mt-0.5">
                        hint: {d.hint}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* 流式输出 */}
          {runOutput.length > 0 && (
            <pre className="whitespace-pre-wrap text-[var(--foreground)]">
              {runOutput.join("")}
            </pre>
          )}

          {/* 编译结果（静态） */}
          {!isRunning && runOutput.length === 0 && compileResult?.success && compileResult.output && (
            <pre className="whitespace-pre-wrap text-[var(--foreground)]">
              {compileResult.output}
            </pre>
          )}
          {!isRunning && runOutput.length === 0 && compileResult && !compileResult.success && (
            <>
              {compileResult.error && (
                <pre className="whitespace-pre-wrap text-red-400">
                  {compileResult.error}
                </pre>
              )}
              {compileResult.output && (
                <pre className="whitespace-pre-wrap text-[var(--muted-foreground)]">
                  {compileResult.output}
                </pre>
              )}
            </>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
