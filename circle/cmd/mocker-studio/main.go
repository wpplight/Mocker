// Command mocker-studio 是 Mocker 的可视化 GUI 入口。
//
// 用 wails v2 封装 React Flow + Monaco 前端，service 直接 import circle/internal/ide
// （无进程间 JSON 序列化，拖拽编辑零延迟）。
//
// 编译：
//   make build-studio
//
// 用法：
//   ./release/circle-studio-linux-amd64 [workspace_dir]
//   ./release/run-studio.sh ../example   # 带黑屏 workaround 的包装脚本
//
// 端口 / 窗口由 wails v2 默认配置。

package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"circle/internal/ide"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 0. 强制设上 wails v2 + Linux 上必须的 env var（解决 "一闪而过就黑屏"）
	//
	// 根因：Go 1.21+ 用 SIGUSR1 做 async-preempt，JavaScriptCore 也用 SIGUSR1 跑 GC，
	//       两者冲突会让 JSC 渲染过程随机崩溃，表现为 React 挂载后瞬间黑屏。
	// 解法：让 JSC 改用 SIGUSR2（信号 12），或彻底关掉 Go 异步抢占。
	//       同时关掉 WebKit 的 GPU 合成（兼容性最好的一组设置）。
	//
	// 用户在 shell 里提前 export 的值优先；这里只设默认。
	if os.Getenv("GODEBUG") == "" {
		_ = os.Setenv("GODEBUG", "asyncpreemptoff=1")
	}
	if os.Getenv("JSC_SIGNAL_FOR_GC") == "" {
		_ = os.Setenv("JSC_SIGNAL_FOR_GC", "12")
	}
	if os.Getenv("WEBKIT_DISABLE_COMPOSITING_MODE") == "" {
		_ = os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	}
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	if os.Getenv("WEBKIT_DISABLE_SANDBOX") == "" {
		_ = os.Setenv("WEBKIT_DISABLE_SANDBOX", "1")
	}

	// 1. 解析可选的 workspace 参数
	//   ./circle-studio                       # 默认当前目录
	//   ./circle-studio ../example            # 打开 example 工作区
	//   ./circle-studio -h                    # 帮助
	workspace := "."
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help":
			fmt.Println("Usage: circle-studio [workspace_dir]")
			fmt.Println("  workspace_dir  Mocker workspace root (default: current dir)")
			fmt.Println("                 Must contain a `package main` with main.ce")
			return
		default:
			workspace = os.Args[1]
		}
	}

	// 2. 转绝对路径
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}

	log.Printf("Mocker Studio starting with workspace: %s", workspace)
	log.Printf("Black-screen workarounds: GODEBUG=%s JSC_SIGNAL_FOR_GC=%s WEBKIT_DISABLE_COMPOSITING_MODE=%s",
		os.Getenv("GODEBUG"),
		os.Getenv("JSC_SIGNAL_FOR_GC"),
		os.Getenv("WEBKIT_DISABLE_COMPOSITING_MODE"),
	)

	// 3. 构造 ide service
	svc := ide.NewService(workspace)

	// 4. 启动 wails v2 app
	err := wails.Run(&options.App{
		Title:     "Mocker Studio",
		Width:     1440,
		Height:    900,
		MinWidth:  1024,
		MinHeight: 700,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 10, G: 10, B: 10, A: 1},
		OnStartup: func(ctx context.Context) {
			svc.SetContext(ctx)
		},
		Bind: []interface{}{
			svc,
		},
	})

	if err != nil {
		log.Fatal(err)
	}
}
