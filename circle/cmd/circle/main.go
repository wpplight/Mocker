// Command circle 是 Mocker DSL 编译器入口。
//
// 用法：
//
//	circle -ast -i <file.ce>     解析 + 语义 + 打印 AST
//	circle -i <file.ce>          同上（默认）
//	circle -i -                  从 stdin 读
//	circle -entry -i <file.ce>   额外打印入口点分析（auto-exec / sync/async edges）
//	circle -debug                把 AST/semantic/IR 输出到 ./debug/ 文件夹
//	circle run                   编译 workspace + 跑 hello world（Task A MVP）
//	circle ir                    dump IR（M4.1）
//	circle -h                    帮助
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"circle/internal/circledebug"
	"circle/internal/codegen"
	"circle/internal/ir"
	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// debugDir -debug 输出目录
var debugDir = "debug"

// debugOn 是否启用 debug
var debugOn = false

// dbg 全局 dumper（每个子命令自己 Add 内容，最后 Flush）
var dbg *circledebug.Dumper

// isRunCommand 检查第一个非 flag 参数是否是 "run"
// 用法：circle run
func isRunCommand() bool {
	return flag.NArg() >= 1 && flag.Arg(0) == "run"
}

// isIRCommand 检查是否是 ir 子命令
func isIRCommand() bool {
	return flag.NArg() >= 1 && flag.Arg(0) == "ir"
}

// runIR 跑 IR Lower（M4.1）—— 把 workspace AST+semantic → IR 并 dump
func runIR() {
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

	// 4. 跑 semantic
	wresult := semantic.CheckAll(files)
	for _, e := range wresult.Errors {
		fmt.Fprintf(os.Stderr, "%s\n", e)
	}

	// 5. Lower → IR
	prog := ir.Lower(wresult)

	// 6. dump
	fmt.Println(ir.DumpProgram(prog))

	// 7. debug 输出
	if debugOn {
		dumpWorkspaceDebug(files, wresult, prog)
	}
}

// runHello 跑 hello world MVP（Task A）
//
// 用法：circle run
// - 工作区扫到 main → 用 MVP codegen emit Go 源码 → 编译 → 运行
// - 当前只支持 example/main.ce 这个 hello world（硬编码模板）
// - 升级到 M4.2 后会用通用 IR → go/ast
func runHello() {
	srcCode := codegen.EmitHelloWorldGo()

	// Sanity check
	if errs := codegen.ValidationCheck(srcCode); len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "emit validation failed: %v\n", errs)
		os.Exit(1)
	}

	// debug 输出（run 模式：emit 出的 Go 源码也写一份）
	if debugOn {
		dbg.Add("00-emit-go.txt", srcCode)
	}

	fmt.Println("=== emit Go 源码 ===")
	fmt.Println(srcCode)
	fmt.Println("=== 编译 + 跑 ===")

	out, code, err := codegen.Run(srcCode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build/run failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("exit code: %d\n", code)
	fmt.Printf("output:\n%s", out)

	if debugOn {
		dbg.Add("99-run-output.txt", out)
	}
}

// dumpWorkspaceDebug 把 workspace 模式的 AST + semantic + IR 写到 dbg
func dumpWorkspaceDebug(files map[string]*ast.File, wresult *semantic.WorkspaceResult, prog *ir.IRProgram) {
	// AST
	var astBuf strings.Builder
	for name, file := range files {
		astBuf.WriteString(fmt.Sprintf("--- package %s ---\n", name))
		astBuf.WriteString(parser.Dump(file))
		astBuf.WriteString("\n")
	}
	dbg.Add("01-ast.txt", astBuf.String())

	// Semantic：符号表 + 错误 + 入口点
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
}

// dumpSingleFileDebug 把单文件模式的 AST + semantic 写到 dbg
func dumpSingleFileDebug(file *ast.File, sem *semantic.CheckResult) {
	dbg.Add("01-ast.txt", parser.Dump(file))

	var semBuf strings.Builder
	semBuf.WriteString("--- symbol table ---\n")
	semBuf.WriteString(sem.Table.String())
	semBuf.WriteString("\n--- errors ---\n")
	for _, e := range sem.Errors {
		semBuf.WriteString(e.Error())
		semBuf.WriteString("\n")
	}
	if sem.EntryPoint != nil {
		semBuf.WriteString("--- entry point ---\n")
		semBuf.WriteString(semantic.FormatEntryPoint(sem.EntryPoint))
	}
	dbg.Add("02-semantic.txt", semBuf.String())
}

func main() {
	var (
		showAST     = flag.Bool("ast", true, "解析并打印 AST（默认开启）")
		input       = flag.String("i", "", "输入文件路径；- 表示从 stdin 读")
		showEntry   = flag.Bool("entry", false, "额外打印入口点分析（auto-exec / sync/async 边）")
		showHelp    = flag.Bool("h", false, "显示帮助")
		showErrors  = flag.Bool("errs", true, "打印语义错误（默认开启）")
		showTable   = flag.Bool("table", false, "额外打印符号表（debug 用）")
		debug       = flag.Bool("debug", false, "把 AST/semantic/IR 写到 ./debug/ 文件夹")
		debugDirArg = flag.String("debug-dir", "debug", "debug 输出目录（-debug 打开时生效）")
	)
	flag.Usage = usage
	flag.Parse()

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
			} else {
				fmt.Fprintf(os.Stderr, "[debug] wrote files to %s/\n", debugDir)
			}
		}()
	}

	// 主调度：
	//   -i <file>  → 单文件模式（向后兼容）
	//   (没-i)     → workspace 模式：扫描 cwd，找 main，BFS 加载所有可达包
	//   run        → 编译并跑 hello world（Task A MVP）
	//   ir         → Lower + dump IR（M4.1）
	if isRunCommand() {
		runHello()
		return
	}
	if isIRCommand() {
		runIR()
		return
	}
	if *input == "" && flag.NArg() >= 1 {
		*input = flag.Arg(0)
	}

	if *input != "" {
		runSingleFile(*input, *showAST, *showEntry, *showTable, *showErrors)
	} else {
		runWorkspace(".", *showAST, *showEntry, *showTable, *showErrors)
	}
}

// runSingleFile 单文件模式（向后兼容）
func runSingleFile(path string, showAST, showEntry, showTable, showErrors bool) {
	src, err := readSource(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}

	file, parseErrs := parser.Parse(src)
	for _, e := range parseErrs {
		fmt.Fprintf(os.Stderr, "%s\n", e)
	}
	if file == nil {
		os.Exit(1)
	}

	sem := semantic.Check(file)
	if showErrors {
		for _, e := range sem.Errors {
			fmt.Fprintf(os.Stderr, "%s\n", e)
		}
	}

	if showEntry && sem.EntryPoint != nil {
		fmt.Println("=== EntryPoint ===")
		fmt.Println(semantic.FormatEntryPoint(sem.EntryPoint))
	}

	if showTable {
		fmt.Println("=== SymbolTable ===")
		fmt.Println(sem.Table)
	}

	if showAST {
		fmt.Println("=== AST ===")
		fmt.Println(parser.Dump(file))
	}

	if debugOn {
		dumpSingleFileDebug(file, sem)
	}

	totalErrs := len(parseErrs) + len(sem.Errors)
	if totalErrs > 0 {
		os.Exit(2)
	}
}

// runWorkspace workspace 模式（M3 Task B）
//
// 1. 扫描 cwd 找所有 .ce 文件 + 提取 package 名
// 2. 找 main 包
// 3. 从 main BFS 加载所有可达包
// 4. 跑多包语义检查（每包建符号表 + 跨包查找）
// 5. 打印所有错误 + AST
func runWorkspace(root string, showAST, showEntry, showTable, showErrors bool) {
	// ① 扫描
	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: root})
	if showErrors {
		for _, e := range scanErrs {
			fmt.Fprintf(os.Stderr, "%s\n", e)
		}
	}

	// ② 找 main
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "workspace error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("workspace: root=%s, main=%s (%s)\n", root, mainInfo.Name, mainInfo.Folder)
	fmt.Printf("  discovered %d packages:", len(pkgMap))
	for name, info := range pkgMap {
		fmt.Printf(" %s(%d files)", name, len(info.Files))
	}
	fmt.Println()

	// ③ BFS 加载
	files, bfsErrs := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	if showErrors {
		for _, e := range bfsErrs {
			fmt.Fprintf(os.Stderr, "%s\n", e)
		}
	}
	fmt.Printf("  loaded %d packages via BFS\n", len(files))

	// ④ 多包语义检查
	wresult := semantic.CheckAll(files)
	if showErrors {
		for _, e := range wresult.Errors {
			fmt.Fprintf(os.Stderr, "%s\n", e)
		}
	}

	// ⑤ dump
	if showEntry && wresult.EntryPoint != nil {
		fmt.Println("=== EntryPoint ===")
		fmt.Println(semantic.FormatEntryPoint(wresult.EntryPoint))
	}

	if showTable {
		fmt.Println("=== SymbolTables ===")
		for name, table := range wresult.Tables {
			fmt.Printf("--- package %s ---\n", name)
			fmt.Println(table)
		}
	}

	if showAST {
		fmt.Println("=== ASTs ===")
		for name, file := range files {
			fmt.Printf("--- package %s ---\n", name)
			fmt.Println(parser.Dump(file))
		}
	}

	// debug 输出（-debug 时写 ./debug/）
	if debugOn {
		// workspace 模式加 IR（降级一遍）
		prog := ir.Lower(wresult)
		dumpWorkspaceDebug(files, wresult, prog)
	}

	totalErrs := len(scanErrs) + len(bfsErrs) + len(wresult.Errors)
	if totalErrs > 0 {
		os.Exit(2)
	}
}

func readSource(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
circle — Mocker DSL 编译器

用法:
  circle -ast -i <file.ce>       解析 + 语义 + 打印 AST
  circle -i <file.ce>            同上（默认）
  circle -entry -i <file.ce>     额外打印入口点分析
  circle -i -                    从 stdin 读
  circle -h                      显示本帮助

选项:
  -ast    解析并打印 AST（默认 true）
  -entry  额外打印入口点分析（auto-exec / sync-async 边）
  -errs   打印语义错误（默认 true）
  -i      输入文件路径（必填）；"-" 表示 stdin
  -h      显示帮助

阶段:
  ① 解析       parser.Parse        → AST
  ② 语义       semantic.Check      → 错误 + 符号表 + 入口点
  ③ (TODO M4)  IR + codegen       → Go 源码
  ④ (TODO M6)  go build           → 二进制

示例:
  circle -ast -i example/main.ce
  circle -entry -i example/main.ce
  cat example/sysio.ce | circle -i -
`))
}
