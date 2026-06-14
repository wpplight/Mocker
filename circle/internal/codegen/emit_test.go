// Package codegen 测试
//
// 目标：验证 M4.5 self-contained sync 编排能产生和 gen_example.go 一致的输出
// （即 hello 的 NewXxx() 内部编排 SubEdge + SubFlow，而不是把编排放进 block0）
package codegen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"circle/internal/ir"
	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// TestEmitMainHello 验证 hello world (.ce) → Go 输出
//
// 加载 example/ workspace（main + stdio + io + ...）跑完整流水线
// 期望：
//   - hello.Newhello() 内部编排 SubEdge `h <add_str> w` 和 SubFlow `out_str >> p.msg`
//   - hello **没有** block0 方法（self-contained sync → 编排全在 NewXxx）
//   - world 有 caller-specific 方法 add_str_hello(words string) string
//   - Println / write 有 block0 方法
//   - main() 只调 `_ = Newhello()`
func TestEmitMainHello(t *testing.T) {
	// 找到 example 目录（cwd = .../internal/codegen → .../example）
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	exampleRoot := filepath.Join(wd, "..", "..", "..", "example")
	// 检查是否存在，不存在则跳过（CI 环境可能没有 example）
	if _, err := os.Stat(exampleRoot); err != nil {
		t.Skipf("example dir not found at %s: %v", exampleRoot, err)
	}

	// 扫 workspace
	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: exampleRoot})
	for _, e := range scanErrs {
		t.Logf("scan warn: %s", e)
	}
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		t.Fatalf("no main: %v", err)
	}
	files, bfsErrs := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	for _, e := range bfsErrs {
		t.Logf("bfs warn: %s", e)
	}
	wresult := semantic.CheckAll(files)
	for _, e := range wresult.Errors {
		t.Logf("semantic error: %s", e)
	}
	if len(wresult.Errors) > 0 {
		t.Fatalf("semantic errors: %d", len(wresult.Errors))
	}

	prog := ir.Lower(wresult)
	srcCode := EmitGoFromIR(prog)

	// 验证 hello.Newhello() 内部有 SubEdge 编排
	if !strings.Contains(srcCode, "w := Newworld()") {
		t.Errorf("expected NewXxx to call Newworld, got:\n%s", srcCode)
	}
	if !strings.Contains(srcCode, "out_str := w.add_str_hello(hello_instance.h)") {
		t.Errorf("expected SubEdge call add_str_hello, got:\n%s", srcCode)
	}
	if !strings.Contains(srcCode, "p := NewPrintln()") {
		t.Errorf("expected SubFlow to create p, got:\n%s", srcCode)
	}
	if !strings.Contains(srcCode, "p.block0(out_str)") {
		t.Errorf("expected SubFlow to call p.block0, got:\n%s", srcCode)
	}

	// 验证 hello **没有** block0 方法（self-contained）
	if strings.Contains(srcCode, "func (n *hello) block0") {
		t.Errorf("expected hello NOT to have block0 method (self-contained), got:\n%s", srcCode)
	}

	// 验证 world 有 caller-specific 方法 add_str_hello
	if !strings.Contains(srcCode, "func(n *world) add_str_hello(words string) string") {
		t.Errorf("expected world.add_str_hello method, got:\n%s", srcCode)
	}
	// world 没有 block0
	if strings.Contains(srcCode, "func (n *world) block0") {
		t.Errorf("expected world NOT to have block0, got:\n%s", srcCode)
	}

	// 验证 Println / write 有 block0
	if !strings.Contains(srcCode, "func (n *Println) block0(msg string)") {
		t.Errorf("expected Println.block0, got:\n%s", srcCode)
	}
	if !strings.Contains(srcCode, "func (n *write) block0(fid int, data string)") {
		t.Errorf("expected write.block0, got:\n%s", srcCode)
	}

	// 验证 main() 只调 Newhello()
	if !strings.Contains(srcCode, "_ = Newhello()") {
		t.Errorf("expected main() to call Newhello(), got:\n%s", srcCode)
	}
}

// TestInferEdgeMultiFlowStmt 验证 multi-FlowStmt edge body 推断
//
// 边 `<add_str>` body 有 2 个 FlowStmt（输入路径 + 返回路径）
// 应推断为 hello <add_str> world，**不**报 "多个 src" 错误
func TestInferEdgeMultiFlowStmt(t *testing.T) {
	src := []byte(`package main

hello {
    h := "hi"
    h >>
    >> str out_str
}

world {
    >> str words
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}
`)
	file := mustParse(t, src)
	sem := semantic.Check(file)
	if len(sem.Errors) > 0 {
		t.Fatalf("semantic errors: %v", sem.Errors)
	}
	// 找到 <add_str> 边的 EdgeDecl
	for _, decl := range file.Decls {
		if e, ok := decl.(*ast.EdgeDecl); ok && e.Edge == "add_str" {
			if e.Src != "hello" || e.Dst != "world" {
				t.Errorf("expected edge src=hello dst=world, got src=%s dst=%s", e.Src, e.Dst)
			}
			return
		}
	}
	t.Fatal("no <add_str> edge found")
}

func mustParse(t *testing.T, src []byte) *ast.File {
	t.Helper()
	file, errs := parser.Parse(src)
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	return file
}

// TestEmitMainHelloSnapshot 验证生成代码与 example/gen_example.go 结构一致
//
// 这是一个 snapshot test，防止 codegen 在后续重构中偏离期望结构。
// 检查不依赖节点输出顺序（map iteration 在 Go 里不固定），只检查关键
// 片段存在性。
func TestEmitMainHelloSnapshot(t *testing.T) {
	// 找到 example 目录（cwd = .../internal/codegen → .../example）
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	exampleRoot := filepath.Join(wd, "..", "..", "..", "example")
	if _, err := os.Stat(exampleRoot); err != nil {
		t.Skipf("example dir not found at %s: %v", exampleRoot, err)
	}

	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: exampleRoot})
	for _, e := range scanErrs {
		t.Logf("scan warn: %s", e)
	}
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		t.Fatalf("no main: %v", err)
	}
	files, bfsErrs := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	for _, e := range bfsErrs {
		t.Logf("bfs warn: %s", e)
	}
	wresult := semantic.CheckAll(files)
	for _, e := range wresult.Errors {
		t.Logf("semantic error: %s", e)
	}
	if len(wresult.Errors) > 0 {
		t.Fatalf("semantic errors: %d", len(wresult.Errors))
	}

	prog := ir.Lower(wresult)
	srcCode := EmitGoFromIR(prog)

	// Snapshot 验证：以下片段必须存在（不依赖输出顺序）
	// 注意：world 节点使用 for 循环 + 复合赋值 + 数学表达式
	expected := []string{
		"type world struct {",
		"func Newworld() *world {",
		"func(n *world) add_str_hello(words string) string {",
		"new := words",
		"t := (((1 + (2 * 1)) - 8) + ((2 + 2) * 2))",
		"for i := 0; i < t; i++ {",
		"new += \"world!\"",
		"return new",
		"type hello struct {",
		"h string",
		"func Newhello() *hello {",
		"hello_instance := &hello{",
		"h: \"hello\",",
		"w := Newworld()",
		"out_str := w.add_str_hello(hello_instance.h)",
		"p := NewPrintln()",
		"p.block0(out_str)",
		"return hello_instance",
		"type Println struct {",
		"func (n *Println) block0(msg string)  {",
		"out := Newwrite()",
		"out.block0(fid, data)",
		"type write struct {",
		"func (n *write) block0(fid int, data string)  {",
		"syscall.Write(fid, []byte(data))",
		"func main() {",
		"_ = Newhello()",
	}

	for _, want := range expected {
		if !strings.Contains(srcCode, want) {
			t.Errorf("expected substring %q not found in:\n%s", want, srcCode)
		}
	}
}
