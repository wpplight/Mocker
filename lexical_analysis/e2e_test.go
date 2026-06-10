// Package lexical_analysis_test 是端到端测试：
// 走完整 DSL → NFA → DFA → 分词 链路，验证 examples/tokens.glex
// 在所有优先级场景下都输出正确的 token 流。
package lexical_analysis_test

import (
	"fmt"
	"testing"

	"lexical_analysis/internal/dfa"
	"lexical_analysis/internal/dsl"
	"lexical_analysis/internal/nfa"
)

// TestE2E_RealLexer_Tokenize 跑 examples/tokens.glex 真实词法。
// 验证：关键字 > ID、REAL > NUM、多字符 OP > 单字符 OP。
func TestE2E_RealLexer_Tokenize(t *testing.T) {
	sf, err := dsl.ReadFile("examples/tokens.glex")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	keys := make([]string, 0, len(sf.Tokens))
	nfas := make([]*nfa.NFA, 0, len(sf.Tokens))
	for _, t1 := range sf.Tokens {
		keys = append(keys, t1.Key)
		nfas = append(nfas, t1.NFA)
	}
	combined := dfa.NewCombinedNFA(nfas, keys)
	d := combined.ToDFA()

	tests := []struct {
		input    string
		expected []string
	}{
		// ── 关键字（优先于 ID）──
		{"if", []string{"KW_IF"}},
		{"else", []string{"KW_ELSE"}},
		{"for", []string{"KW_FOR"}},
		{"while", []string{"KW_WHILE"}},
		{"int", []string{"KW_INT"}},
		// ── 标识符（含关键字前缀必须正确退化为 ID）──
		{"ifoo", []string{"ID"}},
		{"intvar", []string{"ID"}},
		{"x", []string{"ID"}},
		{"x123_y", []string{"ID"}},
		// ── 数字 ──
		{"123", []string{"NUM"}},
		{"+123", []string{"NUM"}},
		{"-123", []string{"NUM"}},
		{"123.456", []string{"REAL"}}, // REAL 优先于 NUM
		{"+1.5", []string{"REAL"}},
		// ── 运算符（多字符优先）──
		{"=", []string{"OP_ASSIGN"}},
		{"==", []string{"OP_EQ"}}, // OP_EQ 优先于 OP_ASSIGN
		{"<", []string{"OP_LT"}},
		{"<=", []string{"OP_LE"}}, // OP_LE 优先于 OP_LT
		{">", []string{"OP_GT"}},
		{">=", []string{"OP_GE"}}, // OP_GE 优先于 OP_GT
		{"+", []string{"OP_ADD"}},
		{"-", []string{"OP_SUB"}},
		{"*", []string{"OP_MUL"}},
		{"/", []string{"OP_DIV"}},
		// ── 定界符 ──
		{";", []string{"SEP_SEMI"}},
		{",", []string{"SEP_COMMA"}},
		{"(", []string{"SEP_LPAREN"}},
		{")", []string{"SEP_RPAREN"}},
		{"{", []string{"SEP_LBRACE"}},
		{"}", []string{"SEP_RBRACE"}},
	}

	for _, tc := range tests {
		got, _ := tokenizeLongest(d, tc.input)
		if !sliceEq(got, tc.expected) {
			t.Errorf("tokenize(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

// tokenizeLongest 用 DFA 找最长合法前缀，最简单的分词器实现。
func tokenizeLongest(d *dfa.DFA, input string) ([]string, string) {
	out := []string{}
	trace := ""
	i := 0
	for i < len(input) {
		state := d.GetStart()
		lastAccept := -1
		lastTag := ""
		j := i
		for ; j < len(input); j++ {
			ch := input[j]
			tos := d.GetTrans()[state][ch]
			if len(tos) == 0 {
				break
			}
			state = tos[0]
			if tag, ok := d.AcceptTag(state); ok {
				lastAccept = j
				lastTag = tag
			}
		}
		if lastAccept < i {
			return out, "err"
		}
		out = append(out, lastTag)
		trace += lastTag + " "
		i = lastAccept + 1
	}
	return out, trace
}

func sliceEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			fmt.Println("sss")
			return false
		}
	}
	return true
}
