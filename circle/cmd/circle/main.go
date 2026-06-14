// Command circle 是 Mocker DSL 编译器入口。
//
// 用法：
//
//	circle build             编译当前 workspace，生成中间代码
//	circle -debug build      编译并输出调试中间文件到 ./debug/
//	circle -h                帮助
package main

import (
	"flag"
	"fmt"
	"os"
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
	debug := flag.Bool("debug", false, "输出调试中间文件到 ./debug/")
	debugDirArg := flag.String("debug-dir", "debug", "调试输出目录")
	showHelp := flag.Bool("h", false, "显示帮助")
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
			}
		}()
	}

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "build":
		runBuild()
	default:
		fmt.Fprintf(os.Stderr, "未知命令: %s\n\n", flag.Arg(0))
		usage()
		os.Exit(1)
	}
}

// runBuild 编译 workspace：扫描 → 语义检查 → IR Lower → 生成 Go 源码
func runBuild() {
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

	// 7. 输出到 stdout
	fmt.Println(srcCode)

	// 8. debug 输出（AST/semantic/IR/d2 图 + 生成的 Go 源码）
	if debugOn {
		dumpDebug(files, wresult, prog, srcCode)
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
  circle build            编译当前 workspace，生成 Go 中间代码
  circle -debug build     编译并输出调试中间文件到 ./debug/
  circle -h               显示本帮助
`))
}
