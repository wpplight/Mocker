// Command circle 是 Mocker DSL 编译器入口。
//
// 用法：
//
//	circle build -o ./mymock                    编译并产出二进制到 ./mymock
//	circle build -o ./mymock -keep-tmp          保留临时目录（可看生成的 main.go）
//	circle build -o ./mymock -emit-go ./gen.go  同时把生成的 Go 源码存到 ./gen.go
//	circle build -o ./mymock -run               编译后直接运行
//	circle build -debug                         编译并输出 AST/semantic/IR 等调试中间文件
//	circle -h                                   帮助
//
// flag 顺序无关：`circle -debug build -o ./mymock` 和 `circle build -debug -o ./mymock` 等价
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"circle/internal/circledebug"
	"circle/internal/codegen"
	"circle/internal/d2gen"
	"circle/internal/ir"
	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

var (
	debugOn  = false
	debugDir = "debug"
	dbg      *circledebug.Dumper
)

func main() {
	// 顶层 flag（顺序无关：用户既可以 `circle -debug build` 也可以 `circle build -debug`）
	fs := flag.NewFlagSet("circle", flag.ExitOnError)
	debug := fs.Bool("debug", false, "输出调试中间文件到 ./debug/")
	debugDirArg := fs.String("debug-dir", "debug", "调试输出目录")
	showHelp := fs.Bool("h", false, "显示帮助")
	fs.Usage = usage

	// M1.x：把 args 里所有顶层 flag 从原始 os.Args 里"抽"出来，subcommand 只看剩余的
	// 原因：Go 原生 flag.Parse() 碰到第一个非 flag 就停，`circle build -debug`
	// 不会先解析 -debug，直接交给 build 子命令就报 "flag provided but not defined"
	argv := reorderArgs(os.Args[1:])

	if err := fs.Parse(argv); err != nil {
		os.Exit(1)
	}

	if *showHelp {
		usage()
		return
	}

	// 初始化 debug
	debugOn = *debug
	debugDir = *debugDirArg
	if debugOn {
		dbg = circledebug.New(debugDir)
		defer func() {
			if err := dbg.Flush(); err != nil {
				fmt.Fprintf(os.Stderr, "debug flush error: %v\n", err)
			}
		}()
	}

	args := fs.Args()
	if len(args) < 1 {
		usage()
		os.Exit(1)
	}

	switch args[0] {
	case "build":
		runBuild(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", args[0])
		usage()
		os.Exit(1)
	}
}

// reorderArgs 把所有顶层 flag（-debug / -debug-dir / -h / --help）抽到 args 前面，
// 其它原序保留。
//
// 这样：
//   - `circle build -debug`         → `circle -debug build`
//   - `circle -debug build -keep-tmp` → `circle -debug build -keep-tmp`
//
// 简化了 flag 解析：所有顶层 flag 都被外层 fs 消费，subcommand 只看到非顶层的 flag。
func reorderArgs(argv []string) []string {
	// 顶层 flag 集合（用前缀匹配：-debug / -debug-dir / -h / -help）
	known := map[string]bool{
		"-debug":      true,
		"-debug-dir":  true,
		"-h":          true,
		"-help":       true,
		"--help":      true,
	}
	var top []string
	var rest []string
	i := 0
	for i < len(argv) {
		a := argv[i]
		if known[a] {
			top = append(top, a)
			// 带值的 flag
			if a == "-debug-dir" && i+1 < len(argv) {
				i++
				top = append(top, argv[i])
			}
			i++
			continue
		}
		rest = append(rest, a)
		i++
	}
	return append(top, rest...)
}

// runBuild 编译 workspace：扫描 → 语义检查 → IR Lower → 生成 Go 源码 → go build
func runBuild(argv []string) {
	// build 子命令专属 flag（在子命令里再解析一次）
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	outPath := fs.String("o", "./mymock", "输出二进制路径")
	keepTmp := fs.Bool("keep-tmp", false, "编译完成后保留临时目录（debug 用）")
	tempDir := fs.String("temp-dir", "", "指定临时目录路径（默认 os.MkdirTemp 生成）")
	emitGo := fs.String("emit-go", "", "额外把生成的 Go 源码写到该路径")
	doRun := fs.Bool("run", false, "编译后直接运行二进制")
	runArgs := fs.String("run-args", "", "传给运行时的参数（空格分隔）")
	if err := fs.Parse(argv); err != nil {
		os.Exit(1)
	}

	// 1. 扫 workspace
	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: "."})
	for _, e := range scanErrs {
		fmt.Fprintf(os.Stderr, "%s\n", e)
	}

	// 2. 找 main
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace error: %v\n", err)
		os.Exit(1)
	}

	// 3. BFS 加载
	files, bfsErrs := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	for _, e := range bfsErrs {
		fmt.Fprintf(os.Stderr, "%s\n", e)
	}

	// 4. 语义检查
	wresult := semantic.CheckAll(files)
	for _, e := range wresult.Errors {
		fmt.Fprintf(os.Stderr, "%s\n", e)
	}
	if len(wresult.Errors) > 0 {
		os.Exit(1)
	}

	// 5. IR Lower
	prog := ir.Lower(wresult)

	// 6. 生成 Go 源码
	srcCode := codegen.EmitGoFromIR(prog)

	// 7. 可选：把 Go 源码写到文件
	if *emitGo != "" {
		if err := os.WriteFile(*emitGo, []byte(srcCode), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "emit-go failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("→ generated Go: %s (%d bytes)\n", *emitGo, len(srcCode))
	}

	// 8. debug 输出（AST/semantic/IR/d2 图 + 生成的 Go 源码）
	if debugOn {
		dumpDebug(files, wresult, prog, srcCode)
	}

	// 9. 编译：写到 tmp 目录，调 go build
	opts := codegen.BuildOptions{
		KeepTemp: *keepTmp,
		TempDir:  *tempDir,
		Verbose:  true,
	}
	if err := codegen.BuildWithOptions(srcCode, *outPath, opts); err != nil {
		fmt.Fprintf(os.Stderr, "compile failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ built: %s\n", *outPath)

	// 10. 可选：直接跑
	if *doRun {
		args := []string{}
		if *runArgs != "" {
			args = strings.Fields(*runArgs)
		}
		cmd := exec.Command(*outPath, args...)
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				os.Exit(exitErr.ExitCode())
			}
			fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
			os.Exit(1)
		}
	}
}

// dumpDebug 把 AST/semantic/IR/图/生成代码写到 debug dumper
func dumpDebug(files map[string]*ast.File, wresult *semantic.WorkspaceResult, prog *ir.IRProgram, srcCode string) {
	dbg.Add("00-emit-go.go", srcCode)

	// AST
	var astBuf strings.Builder
	for name, file := range files {
		astBuf.WriteString(fmt.Sprintf("--- package %s ---\n", name))
		astBuf.WriteString(parser.Dump(file))
		astBuf.WriteString("\n")
	}
	dbg.Add("01-ast.txt", astBuf.String())

	// Semantic
	var semBuf strings.Builder
	for name, table := range wresult.Tables {
		semBuf.WriteString(fmt.Sprintf("--- package %s symbol table ---\n", name))
		semBuf.WriteString(table.String())
		semBuf.WriteString("\n")
	}
	semBuf.WriteString("--- errors ---\n")
	for _, e := range wresult.Errors {
		semBuf.WriteString(e.Error())
		semBuf.WriteString("\n")
	}
	if wresult.EntryPoint != nil {
		semBuf.WriteString("--- entry point ---\n")
		semBuf.WriteString(semantic.FormatEntryPoint(wresult.EntryPoint))
	}
	dbg.Add("02-semantic.txt", semBuf.String())

	// IR
	dbg.Add("03-ir.txt", ir.DumpProgram(prog))

	// Graph
	g := ir.BuildGraph(prog)
	dbg.Add("04-graph.txt", g.Dump())

	// d2 图
	opts := &d2gen.Options{Direction: "down"}
	dbg.Add("05-graph.d2", d2gen.Generate(prog, opts))
	if svgData, err := d2gen.RenderSVG(prog, opts); err == nil {
		dbg.Add("06-graph.svg", string(svgData))
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
circle — Mocker DSL 编译器

用法:
  circle build [flags]            编译当前 workspace，生成可执行二进制
  circle -h                       显示本帮助

flags（顺序无关，可放 build 前或后）:
  -debug            输出 AST/semantic/IR/d2 等调试中间文件
  -debug-dir <dir>  调试输出目录（默认 ./debug）

build flags:
  -o <path>         输出二进制路径（默认 ./mymock）
  -keep-tmp         编译完成后保留临时目录（debug 用，可看生成的 main.go）
  -temp-dir <dir>   指定临时目录路径（默认 os.MkdirTemp 生成）
  -emit-go <file>   额外把生成的 Go 源码写到该文件
  -run              编译后直接运行二进制
  -run-args "<args>" 运行时参数（空格分隔）

示例:
  circle build -o ./hello                        # 编译到 ./hello
  circle build -o ./hello -keep-tmp             # 保留 tmp，看生成的 Go
  circle build -o ./hello -emit-go ./gen.go     # 同时存一份 Go 源码
  circle build -o ./hello -run                  # 编译并跑
  circle build -o ./hello -run -run-args "a b"  # 跑时传参
  circle build -debug                           # 输出调试中间文件
  circle -debug build                           # 同样可以（flag 顺序无关）
`))
}
