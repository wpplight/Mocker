package dsl

import (
	"strings"
	"testing"

	"lexical_analysis/internal/regex"
)

// ─────────────────────────────────────────────────────────────
// parseKey 单元测试
// ─────────────────────────────────────────────────────────────

func TestParseKey_NoUnderscore(t *testing.T) {
	typ, name, err := parseKey("ID")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "ID" || name != "ID" {
		t.Errorf("got (%q,%q), want (ID,ID)", typ, name)
	}
}

func TestParseKey_WithUnderscore(t *testing.T) {
	typ, name, err := parseKey("OP_ADD")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "OP" || name != "ADD" {
		t.Errorf("got (%q,%q), want (OP,ADD)", typ, name)
	}
}

func TestParseKey_MultiUnderscore(t *testing.T) {
	// "OP_LE_EQ" 应被 SplitN 切成 ["OP", "LE_EQ"]
	typ, name, err := parseKey("OP_LE_EQ")
	if err != nil {
		t.Fatal(err)
	}
	if typ != "OP" || name != "LE_EQ" {
		t.Errorf("got (%q,%q), want (OP,LE_EQ)", typ, name)
	}
}

func TestParseKey_Errors(t *testing.T) {
	tests := []string{
		"_FOO", // type 为空
		"FOO_", // name 为空
	}
	for _, key := range tests {
		t.Run(key, func(t *testing.T) {
			_, _, err := parseKey(key)
			if err == nil {
				t.Errorf("expected error for %q", key)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// ReadString 端到端测试
// ─────────────────────────────────────────────────────────────

const sampleDSL = `
package: mylexer
tokens:
  ID:    '[a-zA-Z_][a-zA-Z0-9_]*'
  NUM:   '[+\-]?[0-9]+'
  REAL:  '[+\-]?[0-9]+\.[0-9]+'
  KW_IF: 'if'
  KW_ELSE: 'else'
  OP_ADD: '\+'
  OP_SUB: '-'
  OP_MUL: '\*'
  OP_DIV: '/'
  OP_ASSIGN: '='
  OP_EQ: '=='
  OP_LT: '<'
  OP_GT: '>'
  OP_LE: '<='
  OP_GE: '>='
  SEP_LPAREN: '\('
  SEP_RPAREN: '\)'
  SEP_LBRACE: '\{'
  SEP_RBRACE: '\}'
  SEP_SEMI: ';'
  SEP_COMMA: ','
`

func TestReadString_Sample(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatalf("ReadString failed: %v", err)
	}

	if sf.Package != "mylexer" {
		t.Errorf("Package: got %q, want %q", sf.Package, "mylexer")
	}
	if sf.TotalTokens != 21 {
		t.Errorf("TotalTokens: got %d, want 21", sf.TotalTokens)
	}

	// 检查 Types（应去重 + 排序）
	wantTypes := []string{"ID", "KW", "NUM", "OP", "REAL", "SEP"}
	if len(sf.Types) != len(wantTypes) {
		t.Fatalf("Types count: got %d, want %d", len(sf.Types), len(wantTypes))
	}
	for i, t1 := range wantTypes {
		if sf.Types[i] != t1 {
			t.Errorf("Types[%d]: got %q, want %q", i, sf.Types[i], t1)
		}
	}
}

func TestReadString_GlobalStatistics(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatal(err)
	}

	// 检查每个 type 的数量
	wantCounts := map[string]int{
		"ID":   1,
		"NUM":  1,
		"REAL": 1,
		"KW":   2,  // IF, ELSE
		"OP":   10, // ADD, SUB, MUL, DIV, ASSIGN, EQ, LT, GT, LE, GE
		"SEP":  6,  // LPAREN, RPAREN, LBRACE, RBRACE, SEMI, COMMA
	}
	for typ, want := range wantCounts {
		got := sf.CountOfType(typ)
		if got != want {
			t.Errorf("CountOfType(%q): got %d, want %d", typ, got, want)
		}
	}
}

func TestReadString_TokensOfType(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatal(err)
	}
	ops := sf.TokensOfType("OP")
	if len(ops) != 10 {
		t.Errorf("TokensOfType(OP): got %d, want 10", len(ops))
	}
	// 检查 OP_ADD 存在
	found := false
	for _, op := range ops {
		if op.Key == "OP_ADD" {
			found = true
			if op.Type != "OP" || op.Name != "ADD" {
				t.Errorf("OP_ADD: got type=%q name=%q, want OP/ADD", op.Type, op.Name)
			}
			if op.IsSingle() {
				t.Error("OP_ADD.IsSingle() should be false")
			}
			break
		}
	}
	if !found {
		t.Error("OP_ADD not found in OP tokens")
	}
}

func TestReadString_SingleInstance(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatal(err)
	}
	for _, t1 := range sf.Tokens {
		switch t1.Key {
		case "ID", "NUM", "REAL":
			if !t1.IsSingle() {
				t.Errorf("%s: IsSingle() should be true", t1.Key)
			}
		case "OP_ADD", "KW_IF", "SEP_SEMI":
			if t1.IsSingle() {
				t.Errorf("%s: IsSingle() should be false", t1.Key)
			}
		}
	}
}

func TestReadString_ASTPopulated(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatal(err)
	}
	for _, t1 := range sf.Tokens {
		if t1.AST == nil {
			t.Errorf("%s: AST is nil", t1.Key)
			continue
		}
		// ID 的 AST 应该是 Concat
		if t1.Key == "ID" {
			if _, ok := t1.AST.(*regex.Concat); !ok {
				t.Errorf("ID.AST should be *Concat, got %T", t1.AST)
			}
		}
		// OP_ADD 的 AST 应该是 Literal('+')
		if t1.Key == "OP_ADD" {
			lit, ok := t1.AST.(*regex.Literal)
			if !ok {
				t.Errorf("OP_ADD.AST should be *Literal, got %T", t1.AST)
			} else if lit.Ch != '+' {
				t.Errorf("OP_ADD.AST.Ch: got %q, want '+'", lit.Ch)
			}
		}
	}
}

func TestReadString_OrderPreserved(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatal(err)
	}
	// Order 字段应该是 0, 1, 2, ...（按 DSL 迭代顺序）
	for i, t1 := range sf.Tokens {
		if t1.Order != i {
			t.Errorf("token[%d].Order: got %d, want %d", i, t1.Order, i)
		}
	}
	// 所有 token 都能被找到
	seen := make(map[string]bool)
	for _, t1 := range sf.Tokens {
		seen[t1.Key] = true
	}
	for _, expected := range []string{"ID", "NUM", "REAL", "KW_IF", "OP_ADD", "OP_EQ", "SEP_SEMI"} {
		if !seen[expected] {
			t.Errorf("expected token %q in list", expected)
		}
	}
}

func TestReadString_Stats(t *testing.T) {
	sf, err := ReadString(sampleDSL)
	if err != nil {
		t.Fatal(err)
	}
	stats := sf.Stats()
	// 简单校验：包含关键信息
	if !strings.Contains(stats, "Package:") {
		t.Error("Stats should contain 'Package:'")
	}
	if !strings.Contains(stats, "OP:") {
		t.Error("Stats should contain 'OP:' type")
	}
	if !strings.Contains(stats, "OP_ADD") {
		t.Error("Stats should list OP_ADD token")
	}
}

// ─────────────────────────────────────────────────────────────
// 错误测试
// ─────────────────────────────────────────────────────────────

func TestReadString_Errors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{
			"missing_package",
			`
tokens:
  ID: 'abc'
`,
		},
		{
			"empty_tokens",
			`
package: x
tokens: {}
`,
		},
		{
			"empty_regex",
			`
package: x
tokens:
  FOO: ''
`,
		},
		{
			"duplicate_key",
			`
package: x
tokens:
  ID: 'a'
  ID: 'b'
`,
		},
		{
			"invalid_regex",
			`
package: x
tokens:
  BAD: '[abc'
`,
		},
		{
			"empty_type_in_key",
			`
package: x
tokens:
  _FOO: 'a'
`,
		},
		{
			"empty_name_in_key",
			`
package: x
tokens:
  FOO_: 'a'
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadString(tt.in)
			if err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────
// ReadFile 集成测试（用 examples/tokens.glex）
// ─────────────────────────────────────────────────────────────

func TestReadFile_Example(t *testing.T) {
	// 跳到 lexical_analysis 根目录
	sf, err := ReadFile("../../examples/tokens.glex")
	if err != nil {
		t.Skipf("examples/tokens.glex not found (skipping): %v", err)
		return
	}
	if sf.TotalTokens < 20 {
		t.Errorf("expected at least 20 tokens, got %d", sf.TotalTokens)
	}
}
