// Package codegen build.go —— 把 emit 出来的 Go 源码编译成二进制
//
// 用法：
//   tmp := EmitHelloWorldGo()
//   err := Build(tmp, "/tmp/mymock")
//
// MVP：直接调 go build 即可。后续可以加 go/types 校验。
package codegen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Build 把 Go 源码编译成二进制
//
// 参数：
//   - srcCode: emit 出来的 Go 源码
//   - outPath: 输出的二进制路径（如 /tmp/mymock）
//
// 流程：
//  1. 创建临时目录，写入 main.go
//  2. go mod init
//  3. go build -o outPath
//  4. 清理临时目录
func Build(srcCode, outPath string) error {
	// 1. 创建临时目录
	tmpDir, err := os.MkdirTemp("", "circle-build-*")
	if err != nil {
		return fmt.Errorf("create tmp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// 2. 写 main.go
	mainPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainPath, []byte(srcCode), 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// 3. go mod init + go build
	if err := runCmd(tmpDir, "go", "mod", "init", "circle"); err != nil {
		return fmt.Errorf("go mod init: %w", err)
	}

	if err := runCmd(tmpDir, "go", "build", "-o", outPath, "."); err != nil {
		return fmt.Errorf("go build: %w", err)
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
func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cmd failed: %s\noutput: %s", err, string(out))
	}
	return nil
}