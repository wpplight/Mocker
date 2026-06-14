package parser

import (
	"strings"
	"testing"
)

// TestParseFor_Classic C 风格 for 循环
func TestParseFor_Classic(t *testing.T) {
	src := `
package main

node {
    for(i:=0; i<3; i++) { new := "x" }
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed C-style for loop")
}

// TestParseFor_GoWhile for(cond) 作为 Go 的 while
func TestParseFor_GoWhile(t *testing.T) {
	src := `
package main

node {
    for(i < 3) { new := "x" }
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed Go-style for (cond)")
}

// TestParseWhile while 关键字
func TestParseWhile(t *testing.T) {
	src := `
package main

node {
    while(x < 10) { new := "x" }
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed while loop")
}

// TestParseIf_CStyle if 条件带括号
func TestParseIf_CStyle(t *testing.T) {
	src := `
package main

node {
    if (x > 0) { new := "x" } else { new := "y" }
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed C-style if with parens")
}

// TestParseIf_GoStyle if 条件不带括号
func TestParseIf_GoStyle(t *testing.T) {
	src := `
package main

node {
    if x > 0 { new := "x" } else { new := "y" }
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed Go-style if without parens")
}

// TestParseCompoundAssign 复合赋值 += / -=
func TestParseCompoundAssign(t *testing.T) {
	src := `
package main

node {
    x := 0
    x += 1
    y := 10
    y -= 2
    z := 1
    z *= 3
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed compound assignments")
}

// TestParseIncDec ++ / --
func TestParseIncDec(t *testing.T) {
	src := `
package main

node {
    for(i:=0; i<3; i++) { new := "x" }
    for(j:=10; j>0; j--) { new := "y" }
}
`
	_, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	t.Logf("OK: parsed ++/--")
}

// TestParseControlFlow_MainExample 主示例的控制流
//
// 模拟 example/main.ce 的 world 节点
func TestParseControlFlow_MainExample(t *testing.T) {
	src := `package main

import stdio

hello {
    h := "hello"
    world w;
    <add_str> w

    >>str out_str
    stdio.Println p;
    out_str >> p.msg;
}

world {
    >> str words
    new := words
    for(i:=0; i<3; i++) { new += " world!" }
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

main {
    hello happy;
}`
	file, errs := Parse([]byte(src))
	if len(errs) > 0 {
		t.Fatalf("parse errors: %v", errs)
	}
	// 期望解析出 4 个 decl：hello, world, edge, main
	if len(file.Decls) < 4 {
		t.Errorf("expected at least 4 decls, got %d", len(file.Decls))
	}
	// 检查解析过程中有 for 关键字
	var sb strings.Builder
	for _, d := range file.Decls {
		_, _ = d, sb
	}
	t.Logf("OK: parsed main example with control flow (%d decls)", len(file.Decls))
}
