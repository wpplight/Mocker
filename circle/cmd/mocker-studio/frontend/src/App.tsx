import { useEffect, useCallback, useRef, useState } from "react";
import { ReactFlowProvider } from "@xyflow/react";
import { useEditorStore } from "./store/editor";
import * as svc from "./lib/service";
import { Toolbar } from "./components/Toolbar";
import { Sidebar } from "./components/Sidebar";
import { GraphEditor } from "./components/GraphEditor";
import { CodeEditor } from "./components/CodeEditor";
import { PropertiesPanel } from "./components/PropertiesPanel";
import { OutputPanel } from "./components/OutputPanel";


function App() {
  const { viewMode, code, currentFile, setParsed, setWorkspace, ingestReparse, setDiagnostics } = useEditorStore();
  const parseTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const workspaceLoadedRef = useRef(false);

  // M0.5: 启动加载 workspace
  useEffect(() => {
    const loadWorkspace = async () => {
      try {
        const wsPath = await svc.GetWorkspace();
        if (!wsPath || wsPath === ".") return;
        const info = await svc.OpenWorkspace(wsPath);
        setWorkspace(info);
        if (info.errors) {
          setDiagnostics(info.errors);
        }
        if (info.parsed) {
          setParsed(info.parsed);
        }
        workspaceLoadedRef.current = true;
      } catch (err) {
        console.error("OpenWorkspace failed:", err);
      }
    };
    loadWorkspace();
  }, [setWorkspace, setParsed, setDiagnostics]);

  // Parse code on change with debounce —— 走 ReparseWorkspace 统一入口
  //
  // 不再用 ParseSource（单文件 AST 视图），改为 ReparseWorkspace 触发完整
  // workspace pipeline，GraphEditor 始终是"从 main 触发的多包图"。
  const parseCode = useCallback(
    async (code: string) => {
      if (!workspaceLoadedRef.current) return;
      try {
        // 拿到当前打开的 file path（默认 mainFile）
        const filePath = currentFile ?? "main.ce";
        const info = await svc.ReparseWorkspace(filePath, code);
        // WorkspaceInfo 跟 OpenWorkspace 同 schema，但 ingestReparse 不覆盖 code
        // （避免 store → useEffect → reparse 死循环）
        ingestReparse(info);
        if (info.errors) {
          setDiagnostics(info.errors);
        }
        if (info.parsed) {
          setParsed(info.parsed);
        }
      } catch (err) {
        console.error("Reparse error:", err);
      }
    },
    [currentFile, ingestReparse, setParsed, setDiagnostics]
  );

  useEffect(() => {
    if (parseTimerRef.current) {
      clearTimeout(parseTimerRef.current);
    }
    parseTimerRef.current = setTimeout(() => {
      parseCode(code);
    }, 300);

    return () => {
      if (parseTimerRef.current) {
        clearTimeout(parseTimerRef.current);
      }
    };
  }, [code, parseCode]);

  // Keyboard shortcuts
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "b") {
        e.preventDefault();
        // Trigger compile
        const btn = document.querySelector("[data-compile-btn]") as HTMLButtonElement;
        btn?.click();
      } else if ((e.ctrlKey || e.metaKey) && e.key === "s") {
        e.preventDefault();
        // 触发保存（M0.5：调 Service.SaveFile）
        const btn = document.querySelector("[data-save-btn]") as HTMLButtonElement;
        btn?.click();
      }
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, []);

  return (
    <ReactFlowProvider>
      <div className="h-screen w-screen flex flex-col overflow-hidden">
        <Toolbar />
        <div className="flex-1 flex overflow-hidden">
          <Sidebar />
          <div className="flex-1 flex flex-col overflow-hidden">
            <div className="flex-1 flex overflow-hidden">
              {/* Graph view */}
              {(viewMode === "graph" || viewMode === "split") && (
                <GraphPane viewMode={viewMode} />
              )}

              {/* Code view */}
              {(viewMode === "code" || viewMode === "split") && (
                <CodePane viewMode={viewMode} />
              )}
            </div>
            <OutputPanel />
          </div>
          <PropertiesPanel />
        </div>
      </div>
    </ReactFlowProvider>
  );
}

function GraphPane({ viewMode }: { viewMode: string }) {
  const [splitRatio, setSplitRatio] = useState(50);
  const isDraggingRef = useRef(false);
  const containerRef = useRef<HTMLDivElement>(null);

  const handleMouseDown = useCallback((e: React.MouseEvent) => {
    e.preventDefault();
    isDraggingRef.current = true;

    const onMouseMove = (ev: MouseEvent) => {
      if (!isDraggingRef.current || !containerRef.current) return;
      const rect = containerRef.current.getBoundingClientRect();
      const pct = ((ev.clientX - rect.left) / rect.width) * 100;
      setSplitRatio(Math.min(Math.max(pct, 20), 80));
    };

    const onMouseUp = () => {
      isDraggingRef.current = false;
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    };

    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  }, []);

  if (viewMode !== "split") {
    return (
      <div className="w-full overflow-hidden">
        <GraphEditor />
      </div>
    );
  }

  return (
    <>
      <div
        ref={containerRef}
        className="overflow-hidden shrink-0"
        style={{ width: `${splitRatio}%` }}
      >
        <GraphEditor />
      </div>
      {/* Drag resize handle */}
      <div
        onMouseDown={handleMouseDown}
        className="w-1 shrink-0 bg-[var(--border)] cursor-col-resize hover:bg-[var(--primary)] active:bg-[var(--primary)] transition-colors relative group"
      >
        <div className="absolute inset-y-0 -left-1 -right-1" />
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-0.5 h-6 bg-[var(--muted-foreground)]/20 rounded-full opacity-0 group-hover:opacity-100 transition-opacity" />
      </div>
    </>
  );
}

function CodePane({ viewMode }: { viewMode: string }) {
  if (viewMode !== "split") {
    return (
      <div className="w-full overflow-hidden">
        <CodeEditor />
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-hidden min-w-0">
      <CodeEditor />
    </div>
  );
}

export default App;
