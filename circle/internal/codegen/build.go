// Package codegen build.go —— 把 emit 出来的 Go 源码编译成二进制
//
// 用法（CLI 接线版）：
//
//	srcCode := codegen.EmitGoFromIR(prog)
//	err := codegen.BuildWithOptions(srcCode, "./mymock", codegen.BuildOptions{
//	    Verbose: true,
//	})
//
// 流程：
//  1. 创建临时目录（默认 /tmp/circle-build-XXXXXX，可指定）
//  2. 写入 main.go
//  3. go mod init
//  4. go build -o outPath
//  5. 清理临时目录（KeepTemp=true 跳过清理；指定 TempDir 时默认保留）
//
// 设计：保留旧 Build(srcCode, outPath) 签名不变，新逻辑走 BuildWithOptions。
package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildOptions 控制编译行为
type BuildOptions struct {
	// KeepTemp 编译完成后不删除临时目录（debug 用，可看生成的 main.go）
	KeepTemp bool

	// TempDir 指定临时目录路径。空字符串 → 用 os.MkdirTemp 生成一个。
	// 非空时，无论 KeepTemp 是否为 true，都不会删除（用户指定的位置我们不动）。
	TempDir string

	// ModuleName go mod init 用的 module 名，默认 "circle"
	ModuleName string

	// Verbose 打印中间步骤（生成的 main.go 路径、go build 命令等）
	Verbose bool
}

// Build 把 Go 源码编译成二进制（兼容旧 API，等价于 BuildWithOptions(srcCode, outPath, BuildOptions{})）
func Build(srcCode, outPath string) error {
	return BuildWithOptions(srcCode, outPath, BuildOptions{})
}

// BuildWithOptions 把 Go 源码编译成二进制
//
// 参数：
//   - srcCode: emit 出来的 Go 源码
//   - outPath: 输出的二进制路径（如 ./mymock 或 /tmp/hello）
//   - opts:   编译选项
func BuildWithOptions(srcCode, outPath string, opts BuildOptions) error {
	if opts.ModuleName == "" {
		opts.ModuleName = "circle"
	}

	// 0. 关键修复：outPath 如果是相对路径，go build 会按 cmd.Dir=tmpDir 解析，
	//    结果写进 tmp 里。把 outPath 转成绝对路径，用调用方（当前进程）的 cwd。
	if !filepath.IsAbs(outPath) {
		abs, err := filepath.Abs(outPath)
		if err != nil {
			return fmt.Errorf("resolve outPath: %w", err)
		}
		outPath = abs
	}

	// 1. 创建临时目录
	var tmpDir string
	var err error
	if opts.TempDir != "" {
		tmpDir = opts.TempDir
		if err = os.MkdirAll(tmpDir, 0755); err != nil {
			return fmt.Errorf("create tmp dir: %w", err)
		}
	} else {
		tmpDir, err = os.MkdirTemp("", "circle-build-*")
		if err != nil {
			return fmt.Errorf("create tmp dir: %w", err)
		}
	}

	// 清理策略：用户没指定 TempDir 且 KeepTemp=false → 删；否则保留
	if opts.TempDir == "" && !opts.KeepTemp {
		defer os.RemoveAll(tmpDir)
	}

	// 2. 写 main.go
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(srcCode), 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}
	if opts.Verbose {
		fmt.Printf("→ generated main.go: %s (%d bytes)\n", mainPath, len(srcCode))
	}

	// 3. go mod init
	if err := runCmd(tmpDir, opts.Verbose, "go", "mod", "init", opts.ModuleName); err != nil {
		return fmt.Errorf("go mod init: %w", err)
	}

	// 4. go build -o outPath
	if err := runCmd(tmpDir, opts.Verbose, "go", "build", "-o", outPath, "."); err != nil {
		return fmt.Errorf("go build: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("→ binary built: %s\n", outPath)
	}
	return nil
}

// Run 编译并运行 binary，输出 stdout
//
// 返回 (stdout, exitCode, error)
func Run(srcCode string, args ...string) (string, int, error) {
	// 1. 编译到临时 binary
	tmpBin, err := os.CreateTemp("", "mymock-*")
	if err != nil {
		return "", 0, fmt.Errorf("create tmp bin: %w", err)
	}
	tmpBin.Close()
	defer os.Remove(tmpBin.Name())

	if err := Build(srcCode, tmpBin.Name()); err != nil {
		return "", 0, err
	}

	// 2. 运行
	cmd := exec.Command(tmpBin.Name(), args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return string(out), 1, err
		}
	}
	return string(out), exitCode, nil
}

// runCmd 在 dir 里跑 cmd，返回 error
//
// verbose=true 时：把子进程 stdout/stderr 实时接到当前终端
// verbose=false 时：捕获输出，错误时塞到 error message 里
func runCmd(dir string, verbose bool, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cmd failed: %s\noutput: %s", err, string(out))
	}
	return nil
}
