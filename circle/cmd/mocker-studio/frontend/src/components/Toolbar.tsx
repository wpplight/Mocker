import { useCallback } from "react";
import { useEditorStore } from "../store/editor";
import * as svc from "../lib/service";
import { Button } from "./ui/button";
import { Separator } from "./ui/separator";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
  TooltipProvider,
} from "./ui/tooltip";
import {
  Play,
  Code2,
  GitBranch,
  Columns2,
  PanelLeftClose,
  PanelLeft,
  Loader2,
  CheckCircle2,
  XCircle,
  Cpu,
  Save,
} from "lucide-react";
import { cn } from "../lib/utils";

export function Toolbar() {
  const {
    viewMode,
    setViewMode,
    sidebarCollapsed,
    toggleSidebar,
    compileResult,
    isDirty,
    currentFile: _currentFile,
    workspaceRoot,
  } = useEditorStore();

  return (
    <TooltipProvider delayDuration={300}>
      <div className="h-10 flex items-center px-3 gap-1 border-b border-[var(--border)] bg-[var(--card)]/60 backdrop-blur-sm select-none shrink-0">
        {/* Left: sidebar toggle + brand */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              onClick={toggleSidebar}
              className="h-7 w-7"
            >
              {sidebarCollapsed ? (
                <PanelLeft className="w-3.5 h-3.5" />
              ) : (
                <PanelLeftClose className="w-3.5 h-3.5" />
              )}
            </Button>
          </TooltipTrigger>
          <TooltipContent side="bottom">
            {sidebarCollapsed ? "Show sidebar" : "Hide sidebar"}
          </TooltipContent>
        </Tooltip>

        <div className="flex items-center gap-1.5 ml-1">
          <Cpu className="w-4 h-4 text-[var(--primary)]" />
          <span className="text-sm font-semibold text-[var(--foreground)] tracking-tight">
            Mocker Studio
          </span>
          {workspaceRoot && (
            <span className="text-[10px] font-mono text-[var(--muted-foreground)]/60 ml-1 max-w-[180px] truncate">
              {workspaceRoot}
            </span>
          )}
          {isDirty && (
            <span className="w-1.5 h-1.5 rounded-full bg-[var(--primary)]/60" />
          )}
        </div>

        <Separator orientation="vertical" className="h-5 mx-2" />

        {/* Center: view mode */}
        <div className="flex items-center gap-0.5 bg-[var(--muted)] rounded-[var(--radius)] p-0.5">
          <ViewModeButton
            active={viewMode === "graph"}
            onClick={() => setViewMode("graph")}
            icon={<GitBranch className="w-3.5 h-3.5" />}
            label="Graph"
          />
          <ViewModeButton
            active={viewMode === "split"}
            onClick={() => setViewMode("split")}
            icon={<Columns2 className="w-3.5 h-3.5" />}
            label="Split"
          />
          <ViewModeButton
            active={viewMode === "code"}
            onClick={() => setViewMode("code")}
            icon={<Code2 className="w-3.5 h-3.5" />}
            label="Code"
          />
        </div>

        <div className="flex-1" />

        {/* Right: compile status + separator + run */}
        {compileResult && (
          <div className="flex items-center gap-1.5 mr-2">
            {compileResult.success ? (
              <CheckCircle2 className="w-3.5 h-3.5 text-emerald-500" />
            ) : (
              <XCircle className="w-3.5 h-3.5 text-red-500" />
            )}
            <span className="text-[10px] font-mono text-[var(--muted-foreground)] max-w-[200px] truncate">
              {compileResult.success
                ? compileResult.output || "OK"
                : compileResult.error}
            </span>
          </div>
        )}

        <Separator orientation="vertical" className="h-5 mx-1.5" />

        {/* Save button (M0.5) */}
        <SaveButton />

        <CompileButton />
      </div>
    </TooltipProvider>
  );
}

function ViewModeButton({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <button
      onClick={onClick}
      className={cn(
        "inline-flex items-center gap-1.5 px-2.5 py-1 rounded-[calc(var(--radius)-2px)] text-xs font-medium transition-all duration-200 cursor-pointer",
        active
          ? "bg-[var(--background)] text-[var(--foreground)] shadow-sm"
          : "text-[var(--muted-foreground)] hover:text-[var(--foreground)]"
      )}
    >
      {icon}
      <span className="hidden sm:inline">{label}</span>
    </button>
  );
}

// M0.5: 保存按钮
function SaveButton() {
  const { currentFile, code, setIsDirty } = useEditorStore();

  const handleSave = useCallback(async () => {
    if (!currentFile) {
      console.warn("No file selected to save");
      return;
    }
    try {
      await svc.SaveFile(currentFile, code);
      setIsDirty(false);
    } catch (err) {
      console.error("Save failed:", err);
    }
  }, [currentFile, code, setIsDirty]);

  if (!currentFile) return null;

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          data-save-btn
          size="icon"
          variant="ghost"
          onClick={handleSave}
          className="h-7 w-7"
        >
          <Save className="w-3.5 h-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="bottom">Save (Ctrl+S) — {currentFile}</TooltipContent>
    </Tooltip>
  );
}

function CompileButton() {
  const {
    workspaceRoot,
    setCompileResult,
    setIsCompiling,
    isCompiling,
    clearRunOutput,
    setIsRunning,
    appendRunOutput,
    setDiagnostics,
  } = useEditorStore();

  // M0.5: 优先编译 workspace；如果没 workspace 就退化为编译当前 source
  const handleBuildRun = useCallback(async () => {
    setIsCompiling(true);
    setCompileResult(null);
    clearRunOutput();

    try {
      const opts: import("../store/editor").CompileOptions = workspaceRoot
        ? {
            workspace: workspaceRoot,
            source: "",
            outputPath: "/tmp/mocker-build",
            emitGo: "",
            keepTmp: false,
            run: true,
            runArgs: "",
          }
        : {
            workspace: "",
            source: "",
            outputPath: "/tmp/mocker-build",
            emitGo: "",
            keepTmp: false,
            run: true,
            runArgs: "",
          };

      const result = await svc.Compile(opts);

      setCompileResult({
        success: result.success,
        output: result.output,
        error: result.error,
        exitCode: result.exitCode,
      });

      // 把编译错误的诊断也填进 store（M0.5 实时反馈）
      // 注意：当前 svc.Compile 不返回结构化 diagnostics，错误在 result.error 里
      // M1 会改 svc.Compile 加 diagnostics 字段
      if (result.error) {
        setDiagnostics([
          {
            line: 0,
            column: 0,
            message: result.error,
          },
        ]);
      }
    } catch (err) {
      setCompileResult({
        success: false,
        output: "",
        error: String(err),
        exitCode: 1,
      });
    } finally {
      setIsCompiling(false);
    }
  }, [workspaceRoot, setCompileResult, setIsCompiling, clearRunOutput, setDiagnostics]);

  // M0.5: 流式运行（调 Service.Run，把 stdout 增量推到 store）
  const handleRunStream = useCallback(async () => {
    setIsRunning(true);
    clearRunOutput();

    try {
      const opts: import("../store/editor").CompileOptions = workspaceRoot
        ? {
            workspace: workspaceRoot,
            source: "",
            outputPath: "/tmp/mocker-build",
            emitGo: "",
            keepTmp: false,
            run: true,
            runArgs: "",
          }
        : {
            workspace: "",
            source: "",
            outputPath: "/tmp/mocker-build",
            emitGo: "",
            keepTmp: false,
            run: true,
            runArgs: "",
          };

      const stream = await svc.Run(opts);
      for await (const chunk of stream) {
        appendRunOutput(chunk);
      }
    } catch (err) {
      appendRunOutput(`[error] ${String(err)}\n`);
    } finally {
      setIsRunning(false);
    }
  }, [workspaceRoot, clearRunOutput, appendRunOutput, setIsRunning]);

  return (
    <div className="flex items-center gap-1">
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            data-compile-btn
            size="sm"
            onClick={handleBuildRun}
            disabled={isCompiling}
            className="h-7 gap-1.5"
          >
            {isCompiling ? (
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Play className="w-3.5 h-3.5" />
            )}
            <span className="text-xs">Build & Run</span>
            <kbd className="text-[9px] font-mono opacity-50 ml-0.5 border border-[var(--foreground)]/10 rounded px-0.5 py-px">
              Ctrl+B
            </kbd>
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">Compile and run (Ctrl+B)</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            size="icon"
            variant="ghost"
            onClick={handleRunStream}
            className="h-7 w-7"
            title="Stream run output"
          >
            <Code2 className="w-3.5 h-3.5" />
          </Button>
        </TooltipTrigger>
        <TooltipContent side="bottom">Stream run (incremental stdout)</TooltipContent>
      </Tooltip>
    </div>
  );
}
