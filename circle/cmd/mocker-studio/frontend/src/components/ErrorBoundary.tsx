/**
 * React ErrorBoundary —— 把任何组件运行时错误显示在屏幕上，
 * 避免 wails v2 在 Linux 上 "React 挂载后黑屏" 时看不到原因。
 */
import { Component, type ReactNode, type ErrorInfo } from "react";

interface Props {
  children: ReactNode;
}

interface State {
  error: Error | null;
  info: ErrorInfo | null;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, info: null };

  static getDerivedStateFromError(error: Error): State {
    return { error, info: null };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("[ErrorBoundary]", error, info);
    this.setState({ error, info });
  }

  render() {
    if (this.state.error) {
      return (
        <div
          style={{
            position: "fixed",
            inset: 0,
            background: "#0a0a0a",
            color: "#ff6b6b",
            fontFamily: "ui-monospace, Menlo, monospace",
            padding: 24,
            overflow: "auto",
            zIndex: 99999,
          }}
        >
          <h1 style={{ color: "#ff4757", fontSize: 22, marginTop: 0 }}>
            ⚠️ Mocker Studio crashed
          </h1>
          <p style={{ color: "#ffa502" }}>
            把下面内容截图发给开发者，定位渲染失败原因：
          </p>
          <pre
            style={{
              background: "#1a1a1a",
              padding: 16,
              borderRadius: 6,
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
              fontSize: 13,
              lineHeight: 1.5,
            }}
          >
            {this.state.error.name}: {this.state.error.message}
            {"\n\n"}
            {this.state.error.stack}
            {this.state.info?.componentStack
              ? `\n\nComponent stack:${this.state.info.componentStack}`
              : ""}
          </pre>
        </div>
      );
    }
    return this.props.children;
  }
}
