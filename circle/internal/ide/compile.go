package ide

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"circle/internal/codegen"
	"circle/internal/ir"
	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// Compile 编译当前 workspace 或 source，返回结果
//
// 流程（复用 cmd/circle/main.go 的逻辑）：
//  1. 决定 source：opts.Source 非空 → 单文件；否则走 opts.Workspace
//  2. 扫 workspace / 解析 source
//  3. semantic check + IR Lower
//  4. codegen.EmitGoFromIR
//  5. codegen.BuildWithOptions → 出二进制
//  6. （可选）执行二进制
func (s *Service) Compile(opts CompileOptions) (*CompileResult, error) {
	srcCode, err := s.buildSource(opts)
	if err != nil {
		return &CompileResult{
			Success: false,
			Error:   fmt.Sprintf("prepare source: %v", err),
		}, nil
	}

	result := &CompileResult{
		GeneratedGo: srcCode,
	}

	// 1. 可选：把 Go 源码写到文件
	if opts.EmitGo != "" {
		if err := os.WriteFile(opts.EmitGo, []byte(srcCode), 0644); err != nil {
			result.Error = fmt.Sprintf("emit-go failed: %v", err)
			return result, nil
		}
	}

	// 2. 编译
	outPath := opts.OutputPath
	if outPath == "" {
		outPath = "./mymock"
	}

	buildOpts := codegen.BuildOptions{
		KeepTemp: opts.KeepTmp,
		Verbose:  false, // GUI 模式不打印到 stdout
	}
	if err := codegen.BuildWithOptions(srcCode, outPath, buildOpts); err != nil {
		result.Error = fmt.Sprintf("compile failed: %v", err)
		return result, nil
	}
	result.Success = true
	result.Output = fmt.Sprintf("built: %s", outPath)

	// 3. 可选：运行
	if opts.Run {
		args := []string{}
		if opts.RunArgs != "" {
			args = strings.Fields(opts.RunArgs)
		}
		cmd := exec.Command(outPath, args...)
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		result.Output = fmt.Sprintf("built: %s\n\noutput:\n%s", outPath, string(out))
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result.ExitCode = exitErr.ExitCode()
			} else {
				result.Error = err.Error()
			}
			result.Success = false
		}
	}

	return result, nil
}

// Run 编译并流式执行，把 stdout/stderr 实时推给 channel
//
// channel 在子进程退出后自动关闭（无论成功失败）。
func (s *Service) Run(opts CompileOptions) (<-chan string, error) {
	ch := make(chan string, 64)

	go func() {
		defer close(ch)

		// 编译
		srcCode, err := s.buildSource(opts)
		if err != nil {
			ch <- fmt.Sprintf("[error] prepare source: %v\n", err)
			return
		}

		// 用临时二进制
		tmpBin, err := os.CreateTemp("", "mymock-*")
		if err != nil {
			ch <- fmt.Sprintf("[error] create tmp bin: %v\n", err)
			return
		}
		tmpBin.Close()
		defer os.Remove(tmpBin.Name())

		if err := codegen.BuildWithOptions(srcCode, tmpBin.Name(), codegen.BuildOptions{}); err != nil {
			ch <- fmt.Sprintf("[error] build: %v\n", err)
			return
		}

		// 运行
		args := []string{}
		if opts.RunArgs != "" {
			args = strings.Fields(opts.RunArgs)
		}
		cmd := exec.Command(tmpBin.Name(), args...)
		cmd.Env = os.Environ()

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			ch <- fmt.Sprintf("[error] stdout pipe: %v\n", err)
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			ch <- fmt.Sprintf("[error] stderr pipe: %v\n", err)
			return
		}

		if err := cmd.Start(); err != nil {
			ch <- fmt.Sprintf("[error] start: %v\n", err)
			return
		}

		// 流式读取
		done := make(chan struct{}, 2)
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					ch <- string(buf[:n])
				}
				if err != nil {
					done <- struct{}{}
					return
				}
			}
		}()
		go func() {
			buf := make([]byte, 1024)
			for {
				n, err := stderr.Read(buf)
				if n > 0 {
					ch <- string(buf[:n])
				}
				if err != nil {
					done <- struct{}{}
					return
				}
			}
		}()

		<-done
		<-done
		cmd.Wait()
	}()

	return ch, nil
}

// ──── helpers ────

// buildSource 从 opts 拿到 .ce 源码 + 编译成 Go 源码
func (s *Service) buildSource(opts CompileOptions) (string, error) {
	workspace := opts.Workspace
	if workspace == "" {
		workspace = s.workspaceDir
	}
	if workspace == "" {
		workspace = "."
	}

	if opts.Source != "" {
		// 单文件模式
		file, parseErrs := parser.Parse([]byte(opts.Source))
		if len(parseErrs) > 0 {
			return "", fmt.Errorf("parse errors: %s", parseErrs[0].Error())
		}
		checkResult := semantic.Check(file)
		if len(checkResult.Errors) > 0 {
			return "", fmt.Errorf("semantic errors: %s", checkResult.Errors[0].Error())
		}

		// 把 CheckResult 包成 WorkspaceResult 给 ir.Lower
		pkgName := ""
		if file.Pkg != nil {
			pkgName = file.Pkg.Name
		}
		wresult := &semantic.WorkspaceResult{
			Files: map[string]*ast.File{pkgName: file},
			Tables: map[string]*semantic.SymbolTable{
				pkgName: checkResult.Table,
			},
			EntryPoint: checkResult.EntryPoint,
			EdgeKinds:  checkResult.EdgeKinds,
			Errors:     checkResult.Errors,
		}
		prog := ir.Lower(wresult)
		return codegen.EmitGoFromIR(prog), nil
	}

	// workspace 模式（复用 cmd/circle/main.go 的逻辑）
	pkgMap, _ := semantic.ScanWorkspace(semantic.ScanOptions{Root: workspace})
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		return "", fmt.Errorf("find main: %w", err)
	}
	files, _ := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	wresult := semantic.CheckAll(files)
	if len(wresult.Errors) > 0 {
		return "", fmt.Errorf("semantic errors: %s", wresult.Errors[0].Error())
	}
	prog := ir.Lower(wresult)

	// 切到 main 包目录（codegen 要在那里临时目录写文件）
	absMain, _ := filepath.Abs(mainInfo.Folder)
	if absMain != "" {
		// 不改 process cwd，只是用作 hint
		_ = absMain
	}

	return codegen.EmitGoFromIR(prog), nil
}