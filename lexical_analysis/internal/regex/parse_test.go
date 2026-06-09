package regex

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────
// 基础节点测试
// ─────────────────────────────────────────────────────────────

func TestParseLiteral(t *testing.T) {
	r := MustParse("a")
	lit, ok := r.(*Literal)
	if !ok {
		t.Fatalf("expected *Literal, got %T", r)
	}
	if lit.Ch != 'a' {
		t.Errorf("expected 'a', got %q", lit.Ch)
	}
}

func TestParseLiteralEscapes(t *testing.T) {
	tests := []struct {
		in   string
		want byte
	}{
		{`\+`, '+'},
		{`\*`, '*'},
		{`\(`, '('},
		{`\)`, ')'},
		{`\[`, '['},
		{`\]`, ']'},
		{`\{`, '{'},
		{`\}`, '}'},
		{`\\`, '\\'},
		{`\.`, '.'},
		{`\-`, '-'},
		{`\|`, '|'},
		{`\?`, '?'},
		{`\^`, '^'},
		{`\$`, '$'},
		{`\n`, '\n'},
		{`\t`, '\t'},
		{`\r`, '\r'},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r, err := Parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			lit, ok := r.(*Literal)
			if !ok {
				t.Fatalf("expected *Literal, got %T (%v)", r, Print(r))
			}
			if lit.Ch != tt.want {
				t.Errorf("got %q, want %q", lit.Ch, tt.want)
			}
		})
	}
}

func TestParseDot(t *testing.T) {
	r := MustParse(".")
	if _, ok := r.(*Dot); !ok {
		t.Errorf("expected *Dot, got %T", r)
	}
}

// ─────────────────────────────────────────────────────────────
// 字符类测试
// ─────────────────────────────────────────────────────────────

func TestParseCharClassSimple(t *testing.T) {
	r := MustParse("[abc]")
	cc, ok := r.(*CharClass)
	if !ok {
		t.Fatalf("expected *CharClass, got %T", r)
	}
	if cc.Negate {
		t.Error("expected Negate=false")
	}
	if string(cc.Chars) != "abc" {
		t.Errorf("got %q, want \"abc\"", cc.Chars)
	}
}

func TestParseCharClassRange(t *testing.T) {
	r := MustParse("[a-z]")
	cc := r.(*CharClass)
	if string(cc.Chars) != "abcdefghijklmnopqrstuvwxyz" {
		t.Errorf("unexpected chars: %q", cc.Chars)
	}
}

func TestParseCharClassMultiRange(t *testing.T) {
	r := MustParse("[a-zA-Z_]")
	cc := r.(*CharClass)
	expected := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
	if string(cc.Chars) != expected {
		t.Errorf("unexpected chars: %q", cc.Chars)
	}
}

func TestParseCharClassNegate(t *testing.T) {
	r := MustParse("[^abc]")
	cc := r.(*CharClass)
	if !cc.Negate {
		t.Error("expected Negate=true")
	}
}

func TestParseShorthandEscapes(t *testing.T) {
	tests := []struct {
		in      string
		negate  bool
		minChar byte
		maxChar byte
	}{
		{`\d`, false, '0', '9'},
		{`\D`, true, '0', '9'},
		{`\w`, false, 'a', 'z'}, // 包含 a-z
		{`\W`, true, 'a', 'z'},
		{`\s`, false, ' ', ' '},
		{`\S`, true, ' ', ' '},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r, err := Parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			cc, ok := r.(*CharClass)
			if !ok {
				t.Fatalf("expected *CharClass, got %T", r)
			}
			if cc.Negate != tt.negate {
				t.Errorf("Negate: got %v, want %v", cc.Negate, tt.negate)
			}
			// 简单校验：包含 minChar 且包含 maxChar（如果有）
			foundMin, foundMax := false, false
			for _, c := range cc.Chars {
				if c == tt.minChar {
					foundMin = true
				}
				if c == tt.maxChar {
					foundMax = true
				}
			}
			if !foundMin {
				t.Errorf("min char %q not in class", tt.minChar)
			}
			if tt.maxChar != tt.minChar && !foundMax {
				t.Errorf("max char %q not in class", tt.maxChar)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 量词测试
// ─────────────────────────────────────────────────────────────

func TestParseStar(t *testing.T) {
	r := MustParse("a*")
	s, ok := r.(*Star)
	if !ok {
		t.Fatalf("expected *Star, got %T", r)
	}
	if _, ok := s.Inner.(*Literal); !ok {
		t.Errorf("Inner should be *Literal, got %T", s.Inner)
	}
}

func TestParsePlus(t *testing.T) {
	r := MustParse("a+")
	if _, ok := r.(*Plus); !ok {
		t.Errorf("expected *Plus, got %T", r)
	}
}

func TestParseOptional(t *testing.T) {
	r := MustParse("a?")
	if _, ok := r.(*Optional); !ok {
		t.Errorf("expected *Optional, got %T", r)
	}
}

func TestParseRepeatExact(t *testing.T) {
	r := MustParse("a{3}")
	rep, ok := r.(*Repeat)
	if !ok {
		t.Fatalf("expected *Repeat, got %T", r)
	}
	if rep.Min != 3 || rep.Max != 3 {
		t.Errorf("got {%d,%d}, want {3,3}", rep.Min, rep.Max)
	}
}

func TestParseRepeatRange(t *testing.T) {
	r := MustParse("a{2,5}")
	rep := r.(*Repeat)
	if rep.Min != 2 || rep.Max != 5 {
		t.Errorf("got {%d,%d}, want {2,5}", rep.Min, rep.Max)
	}
}

func TestParseRepeatUnbounded(t *testing.T) {
	r := MustParse("a{2,}")
	rep := r.(*Repeat)
	if rep.Min != 2 || rep.Max != -1 {
		t.Errorf("got {%d,%d}, want {2,-1}", rep.Min, rep.Max)
	}
}

// ─────────────────────────────────────────────────────────────
// 连接 / 选择 / 分组
// ─────────────────────────────────────────────────────────────

func TestParseConcat(t *testing.T) {
	r := MustParse("ab")
	c, ok := r.(*Concat)
	if !ok {
		t.Fatalf("expected *Concat, got %T", r)
	}
	if l, ok := c.Left.(*Literal); !ok || l.Ch != 'a' {
		t.Errorf("Left should be Literal 'a'")
	}
	if l, ok := c.Right.(*Literal); !ok || l.Ch != 'b' {
		t.Errorf("Right should be Literal 'b'")
	}
}

func TestParseUnion(t *testing.T) {
	r := MustParse("a|b")
	u, ok := r.(*Union)
	if !ok {
		t.Fatalf("expected *Union, got %T", r)
	}
	if l, ok := u.Left.(*Literal); !ok || l.Ch != 'a' {
		t.Errorf("Left should be Literal 'a'")
	}
}

func TestParseGroup(t *testing.T) {
	r := MustParse("(ab)")
	g, ok := r.(*Group)
	if !ok {
		t.Fatalf("expected *Group, got %T", r)
	}
	if _, ok := g.Inner.(*Concat); !ok {
		t.Errorf("Inner should be *Concat, got %T", g.Inner)
	}
}

func TestParseEmpty(t *testing.T) {
	// "" 应该是 Empty
	r, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.(*Empty); !ok {
		t.Errorf("expected *Empty, got %T", r)
	}
}

// ─────────────────────────────────────────────────────────────
// 优先级测试
// ─────────────────────────────────────────────────────────────

func TestPrecedence_UnionLowest(t *testing.T) {
	// "ab|c"  →  (ab)|c
	r := MustParse("ab|c")
	u, ok := r.(*Union)
	if !ok {
		t.Fatalf("expected *Union, got %T", r)
	}
	if _, ok := u.Left.(*Concat); !ok {
		t.Errorf("Left should be Concat (ab)")
	}
	if l, ok := u.Right.(*Literal); !ok || l.Ch != 'c' {
		t.Errorf("Right should be Literal 'c'")
	}
}

func TestPrecedence_QuantifierHigherThanConcat(t *testing.T) {
	// "ab*"  →  Concat(a, Star(b))
	r := MustParse("ab*")
	c, ok := r.(*Concat)
	if !ok {
		t.Fatalf("expected *Concat, got %T", r)
	}
	if _, ok := c.Left.(*Literal); !ok {
		t.Errorf("Left should be Literal 'a'")
	}
	if _, ok := c.Right.(*Star); !ok {
		t.Errorf("Right should be *Star, got %T", c.Right)
	}
}

func TestPrecedence_GroupBreaksQuantifier(t *testing.T) {
	// "(ab)*"  →  Star(Group(Concat(a, b)))
	r := MustParse("(ab)*")
	s, ok := r.(*Star)
	if !ok {
		t.Fatalf("expected *Star, got %T", r)
	}
	g, ok := s.Inner.(*Group)
	if !ok {
		t.Fatalf("expected *Group, got %T", s.Inner)
	}
	if _, ok := g.Inner.(*Concat); !ok {
		t.Errorf("Group inner should be Concat")
	}
}

// ─────────────────────────────────────────────────────────────
// 真实 token 正则的端到端测试
// ─────────────────────────────────────────────────────────────

func TestParseTokenSpec_ID(t *testing.T) {
	// [a-zA-Z_][a-zA-Z0-9_]*
	r := MustParse(`[a-zA-Z_][a-zA-Z0-9_]*`)
	c, ok := r.(*Concat)
	if !ok {
		t.Fatalf("expected Concat, got %T", r)
	}
	if _, ok := c.Left.(*CharClass); !ok {
		t.Errorf("Left should be CharClass")
	}
	if _, ok := c.Right.(*Star); !ok {
		t.Errorf("Right should be Star")
	}
}

func TestParseTokenSpec_NUM(t *testing.T) {
	// [+\-]?[0-9]+
	r := MustParse(`[+\-]?[0-9]+`)
	c, ok := r.(*Concat)
	if !ok {
		t.Fatalf("expected Concat, got %T", r)
	}
	if _, ok := c.Left.(*Optional); !ok {
		t.Errorf("Left should be Optional, got %T", c.Left)
	}
	if _, ok := c.Right.(*Plus); !ok {
		t.Errorf("Right should be Plus, got %T", c.Right)
	}
}

func TestParseTokenSpec_REAL(t *testing.T) {
	// [+\-]?[0-9]+\.[0-9]+
	r := MustParse(`[+\-]?[0-9]+\.[0-9]+`)
	// 不强制具体结构，只验证不报错
	if r == nil {
		t.Fatal("got nil")
	}
}

func TestParseTokenSpec_OP_EQ(t *testing.T) {
	// ==
	r := MustParse("==")
	c, ok := r.(*Concat)
	if !ok {
		t.Fatalf("expected Concat, got %T", r)
	}
	if l, ok := c.Left.(*Literal); !ok || l.Ch != '=' {
		t.Errorf("Left should be Literal '='")
	}
	if l, ok := c.Right.(*Literal); !ok || l.Ch != '=' {
		t.Errorf("Right should be Literal '='")
	}
}

func TestParseTokenSpec_SEP_LPAREN(t *testing.T) {
	// \(
	r := MustParse(`\(`)
	lit, ok := r.(*Literal)
	if !ok {
		t.Fatalf("expected Literal, got %T", r)
	}
	if lit.Ch != '(' {
		t.Errorf("expected '(', got %q", lit.Ch)
	}
}

// ─────────────────────────────────────────────────────────────
// 错误测试
// ─────────────────────────────────────────────────────────────

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"unterminated_class", "[abc"},
		{"unclosed_group", "(ab"},
		{"invalid_range_z_to_a", "[z-a]"},
		{"incomplete_repeat", "a{"},
		{"negative_min_in_repeat", "a{-1}"},
		{"max_less_than_min", "a{5,2}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.in)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.in)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// Print 函数测试（确保树形打印不崩）
// ─────────────────────────────────────────────────────────────

func TestPrint_Complex(t *testing.T) {
	r := MustParse(`[a-zA-Z_][a-zA-Z0-9_]*`)
	out := Print(r)
	if !strings.Contains(out, "Concat") {
		t.Errorf("expected output to contain 'Concat', got:\n%s", out)
	}
	if !strings.Contains(out, "Star") {
		t.Errorf("expected output to contain 'Star'")
	}
	if !strings.Contains(out, "CharClass") {
		t.Errorf("expected output to contain 'CharClass'")
	}
}

func TestPrint_NestedUnion(t *testing.T) {
	r := MustParse(`(if|else)`)
	out := Print(r)
	if !strings.Contains(out, "Group") {
		t.Errorf("expected 'Group' in output:\n%s", out)
	}
	if !strings.Contains(out, "Union") {
		t.Errorf("expected 'Union' in output")
	}
}
