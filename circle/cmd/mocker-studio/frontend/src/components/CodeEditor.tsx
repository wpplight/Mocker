import { useRef, useCallback, useEffect } from "react";
import Editor, { type OnMount, type BeforeMount } from "@monaco-editor/react";
import { useEditorStore } from "../store/editor";
import { registerMockerLanguage, LANGUAGE_ID } from "../lib/mocker-lang";

export function CodeEditor() {
  const { code, setCode, cursorLocation } = useEditorStore();
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null);

  const handleBeforeMount: BeforeMount = useCallback((monaco) => {
    registerMockerLanguage(monaco);
  }, []);

  const handleMount: OnMount = useCallback((editor) => {
    editorRef.current = editor;
    editor.focus();
  }, []);

  // M1.x: 监听 cursorLocation 变化 → 跳到指定行/列
  //   - cursorLocation.path 切换 = 已通过 setCurrentFile 切文件，这里只跳光标
  //   - nonce 递增 = 同一文件多次跳也触发
  useEffect(() => {
    const editor = editorRef.current;
    if (!editor || !cursorLocation) return;
    const { line, col, nonce } = cursorLocation;
    // 让 code / model 切完再 reveal（用 microtask 推到下一个 tick）
    queueMicrotask(() => {
      const ed = editorRef.current;
      if (!ed) return;
      const pos = { lineNumber: Math.max(1, line), column: Math.max(1, col) };
      ed.setPosition(pos);
      ed.revealPositionInCenter(pos);
      ed.focus();
    });
    // 只用 nonce 触发 effect
    void nonce;
  }, [cursorLocation?.path, cursorLocation?.line, cursorLocation?.col, cursorLocation?.nonce, cursorLocation]);

  const handleChange = useCallback(
    (value: string | undefined) => {
      if (value !== undefined) {
        setCode(value);
      }
    },
    [setCode]
  );

  return (
    <div className="w-full h-full">
      <Editor
        height="100%"
        defaultLanguage={LANGUAGE_ID}
        value={code}
        onChange={handleChange}
        beforeMount={handleBeforeMount}
        onMount={handleMount}
        theme="mocker-dark"
        options={{
          fontSize: 13,
          fontFamily: "'JetBrains Mono', 'Fira Code', 'Cascadia Code', Consolas, monospace",
          fontLigatures: true,
          lineNumbers: "on",
          minimap: { enabled: false },
          scrollBeyondLastLine: false,
          wordWrap: "on",
          tabSize: 4,
          insertSpaces: true,
          renderWhitespace: "selection",
          bracketPairColorization: { enabled: true },
          guides: {
            bracketPairs: true,
            indentation: true,
          },
          smoothScrolling: true,
          cursorBlinking: "smooth",
          cursorSmoothCaretAnimation: "on",
          padding: { top: 12, bottom: 12 },
          overviewRulerBorder: false,
          hideCursorInOverviewRuler: true,
          scrollbar: {
            verticalScrollbarSize: 6,
            horizontalScrollbarSize: 6,
            useShadows: false,
          },
          lineHeight: 20,
          letterSpacing: 0.3,
        }}
      />
    </div>
  );
}
