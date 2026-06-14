package ir

import (
	"testing"

	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// TestInferSubEdgeSrc_BasicSuccess 隐式 SubEdge 源推断 — 唯一类型匹配
func TestInferSubEdgeSrc_BasicSuccess(t *testing.T) {
	src := `
package main

world {
    >> str words
    new := words + " world!"
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

hello {
    h := "hello"
    world w;
    <add_str> w

    >> str out_str
}
`
	prog := parseAndLowerInferTest(t, src)
	hello := prog.FindNodeByName("hello")
	if hello == nil {
		t.Fatal("hello node not found")
	}
	if len(hello.SubEdges) != 1 {
		t.Fatalf("expected 1 SubEdge, got %d", len(hello.SubEdges))
	}
	se := hello.SubEdges[0]
	if se.SrcAttr != "h" {
		t.Errorf("expected SrcAttr='h' (inferred), got %q", se.SrcAttr)
	}
	if se.DstInstance != "w" {
		t.Errorf("expected DstInstance='w', got %q", se.DstInstance)
	}
	if se.EdgeName != "add_str" {
		t.Errorf("expected EdgeName='add_str', got %q", se.EdgeName)
	}
}

// TestInferSubEdgeSrc_MultipleCandidates 推断失败 — 多于 1 个匹配
func TestInferSubEdgeSrc_MultipleCandidates(t *testing.T) {
	src := `
package main

world {
    >> str words
    new := words + " world!"
    new >>
}

<add_str> {
    hello.h1 >> world.words
    world.new >> hello.out_str
}

hello {
    h1 := "hello"
    h2 := "world"
    world w;
    <add_str> w

    >> str out_str
}
`
	prog := parseAndLowerInferTest(t, src)
	hello := prog.FindNodeByName("hello")
	if hello == nil {
		t.Fatal("hello node not found")
	}
	se := hello.SubEdges[0]
	// 多候选 → 推断失败，SrcAttr 留 "__implicit__" 标记
	if se.SrcAttr != "__implicit__" {
		t.Errorf("expected SrcAttr='__implicit__' (failed), got %q", se.SrcAttr)
	}
}

// TestInferSubEdgeSrc_NoMatchingType 推断失败 — 没有任何变量匹配类型
func TestInferSubEdgeSrc_NoMatchingType(t *testing.T) {
	src := `
package main

world {
    >> num n
    doubled := n * 2
    doubled >>
}

<add_str> {
    hello.h >> world.n
    world.doubled >> hello.result
}

hello {
    h := "hello"
    world w;
    <add_str> w

    >> num result
}
`
	prog := parseAndLowerInferTest(t, src)
	hello := prog.FindNodeByName("hello")
	if hello == nil {
		t.Fatal("hello node not found")
	}
	se := hello.SubEdges[0]
	// 没有 num 类型的变量 → 推断失败
	if se.SrcAttr != "__implicit__" {
		t.Errorf("expected SrcAttr='__implicit__' (no match), got %q", se.SrcAttr)
	}
}

// TestInferSubEdgeSrc_ExplicitStillWorks 显式语法仍然有效（回归测试）
func TestInferSubEdgeSrc_ExplicitStillWorks(t *testing.T) {
	src := `
package main

world {
    >> str words
    new := words + " world!"
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

hello {
    h := "hello"
    world w;
    h <add_str> w

    >> str out_str
}
`
	prog := parseAndLowerInferTest(t, src)
	hello := prog.FindNodeByName("hello")
	if hello == nil {
		t.Fatal("hello node not found")
	}
	se := hello.SubEdges[0]
	// 显式语法：SrcAttr 必须是 h
	if se.SrcAttr != "h" {
		t.Errorf("expected SrcAttr='h' (explicit), got %q", se.SrcAttr)
	}
}

// parseAndLowerInferTest 测试辅助：解析 + Lower
func parseAndLowerInferTest(t *testing.T, src string) *IRProgram {
	t.Helper()
	file, errs := parser.Parse([]byte(src))
	if errs != nil {
		t.Fatalf("parse error: %v", errs)
	}
	// Wrap in map of files
	files := map[string]*ast.File{"test.ce": file}
	sem := semantic.CheckAll(files)
	if len(sem.Errors) > 0 {
		t.Fatalf("semantic errors: %v", sem.Errors)
	}
	return Lower(sem)
}
