package mocker_lex

import (
	"strings"
	"testing"
)

// helper: 取所有 token 的 Type 名字（用于 assert）
func tokTypes(src string) ([]string, error) {
	toks, err := Tokenize(src)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Type.String()
	}
	return out, nil
}

func TestTokenizeEmpty(t *testing.T) {
	toks, err := Tokenize("")
	if err != nil {
		t.Fatal(err)
	}
	// 期望只有 EOF
	if len(toks) != 1 || toks[0].Type != TypeEOF {
		t.Errorf("expected only EOF, got %d tokens: %+v", len(toks), toks)
	}
}

func TestTokenizeKeywords(t *testing.T) {
	types, err := tokTypes("package import enum if else return true false")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"SYS_PACK", "SYS_IMPORT", "KW_ENUM", "KW_IF", "KW_ELSE",
		"KW_RETURN", "KW_TRUE", "KW_FALSE", "EOF",
	}
	assertEqual(t, types, want)
}

func TestTokenizeTypes(t *testing.T) {
	types, err := tokTypes("str num bool byte any")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"TYPE_STR", "TYPE_NUM", "TYPE_BOOL", "TYPE_BYTE", "TYPE_ANY", "EOF"}
	assertEqual(t, types, want)
}

func TestTokenizeMultiCharOps(t *testing.T) {
	types, err := tokTypes(">> << := == != <= >= && ||")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"OP_RRARROW", "OP_LARROW", "OP_DEFINE", "OP_EQ", "OP_NE",
		"OP_LE", "OP_GE", "OP_AND", "OP_OR", "EOF",
	}
	assertEqual(t, types, want)
}

func TestTokenizeSingleCharOps(t *testing.T) {
	types, err := tokTypes("! + - * / < > = ( ) { } [ ] ; , . : @")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"OP_NOT", "OP_ADD", "OP_SUB", "OP_MUL", "OP_DIV",
		"OP_LT", "OP_GT", "OP_ASSIGN", "SEP_LPAREN", "SEP_RPAREN",
		"SEP_LBRACE", "SEP_RBRACE", "SEP_LBRACKET", "SEP_RBRACKET",
		"SEP_SEMI", "SEP_COMMA", "SEP_DOT", "SEP_COLON", "SEP_AT", "EOF",
	}
	assertEqual(t, types, want)
}

func TestTokenizeIdent(t *testing.T) {
	toks, err := Tokenize("foo bar_baz _x123")
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 4 { // 3 idents + EOF
		t.Fatalf("expected 4 tokens, got %d", len(toks))
	}
	for i, want := range []string{"foo", "bar_baz", "_x123", ""} {
		if toks[i].Value != want {
			t.Errorf("token[%d].Value = %q, want %q", i, toks[i].Value, want)
		}
	}
}

func TestTokenizeCall(t *testing.T) {
	// a.b  /  a.b.c  /  a.b.c.d 都应该是 CALL
	for _, src := range []string{"a.b", "a.b.c", "stdio.Println", "a.b.c.d"} {
		toks, err := Tokenize(src)
		if err != nil {
			t.Fatal(err)
		}
		if toks[0].Type != TypeCALL {
			t.Errorf("%q: expected CALL, got %s", src, toks[0].Type)
		}
		if toks[0].Value != src {
			t.Errorf("%q: value mismatch, got %q", src, toks[0].Value)
		}
	}
}

func TestTokenizeEdgeName(t *testing.T) {
	// out-no-co / send-data 应该是 EDGE_NAME
	for _, src := range []string{"out-no", "out-no-co", "send-data", "a-b-c-d-e"} {
		toks, err := Tokenize(src)
		if err != nil {
			t.Fatal(err)
		}
		if toks[0].Type != TypeEDGE_NAME {
			t.Errorf("%q: expected EDGE_NAME, got %s", src, toks[0].Type)
		}
		if toks[0].Value != src {
			t.Errorf("%q: value mismatch, got %q", src, toks[0].Value)
		}
	}
}

func TestTokenizeString(t *testing.T) {
	toks, err := Tokenize(`"hello" "with \" escape" ""`)
	if err != nil {
		t.Fatal(err)
	}
	if len(toks) != 4 {
		t.Fatalf("expected 4 tokens, got %d: %+v", len(toks), toks)
	}
	if toks[0].Type != TypeSTRING || toks[0].Value != "hello" {
		t.Errorf("tok0: %+v", toks[0])
	}
	if toks[1].Type != TypeSTRING || toks[1].Value != `with \" escape` {
		t.Errorf("tok1: %+v", toks[1])
	}
	if toks[2].Type != TypeSTRING || toks[2].Value != "" {
		t.Errorf("tok2: %+v", toks[2])
	}
}

func TestTokenizeUnterminatedString(t *testing.T) {
	_, err := Tokenize(`"abc`)
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
	if le, ok := err.(*LexError); !ok || !strings.Contains(le.Msg, "unterminated") {
		t.Errorf("expected LexError with 'unterminated', got %v", err)
	}
}

func TestTokenizeNumber(t *testing.T) {
	for _, src := range []string{"0", "42", "3.14", "0.5", "100.200", "100"} {
		toks, err := Tokenize(src)
		if err != nil {
			t.Fatal(err)
		}
		if toks[0].Type != TypeNUM {
			t.Errorf("%q: expected NUM, got %s", src, toks[0].Type)
		}
		if toks[0].Value != src {
			t.Errorf("%q: value = %q", src, toks[0].Value)
		}
	}
}

func TestTokenizeComments(t *testing.T) {
	types, err := tokTypes(`// line comment
/* block
   comment */
package`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"SYS_PACK", "EOF"}
	assertEqual(t, types, want)
}

func TestTokenizePos(t *testing.T) {
	src := "package\n  main"
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Pos.Line != 1 || toks[0].Pos.Col != 1 {
		t.Errorf("package: pos = %+v, want (1,1)", toks[0].Pos)
	}
	if toks[1].Pos.Line != 2 || toks[1].Pos.Col != 3 {
		t.Errorf("main: pos = %+v, want (2,3)", toks[1].Pos)
	}
}

func TestTokenizeMainMocker(t *testing.T) {
	// 真实例子：hello { ... }  main { hello <out-no-co> say }
	src := `package main
hello{
    h := "hi"
    h >>
}
main{
    hello <out-no-co> say
}`
	toks, err := Tokenize(src)
	if err != nil {
		t.Fatal(err)
	}
	// 关键 token 抽几个验证
	checks := map[int]Type{
		0:  TypeSYS_PACK, // package
		1:  TypeID,       // main
		2:  TypeID,       // hello
		3:  TypeSEP_LBRACE,
		4:  TypeID, // h
		5:  TypeOP_DEFINE,
		6:  TypeSTRING, // "hi"
		7:  TypeID,     // h
		8:  TypeOP_RRARROW,
		9:  TypeSEP_RBRACE,
		10: TypeID, // main
		11: TypeSEP_LBRACE,
		12: TypeID, // hello
		13: TypeOP_LT,
		14: TypeEDGE_NAME, // out-no-co
		15: TypeOP_GT,
		16: TypeID, // say
		17: TypeSEP_RBRACE,
		18: TypeEOF,
	}
	if len(toks) != len(checks) {
		t.Fatalf("expected %d tokens, got %d: %+v", len(checks), len(toks), toks)
	}
	for i, want := range checks {
		if toks[i].Type != want {
			t.Errorf("token[%d]: got %s, want %s (value=%q)", i, toks[i].Type, want, toks[i].Value)
		}
	}
}

func TestTokenizeDetailedError(t *testing.T) {
	_, err := Tokenize("abc @def") // @ 在 struct body 里合法，但 @def 是两个独立 token
	if err != nil {
		// 这里不应该出错，是 valid token 流
	}
	// 现在测一个真正会错的
	_, err = Tokenize("hello #world")
	if err == nil {
		t.Fatal("expected error for #")
	}
	le, ok := err.(*LexError)
	if !ok {
		t.Fatalf("expected *LexError, got %T: %v", err, err)
	}
	if le.Char != '#' {
		t.Errorf("Char = %q, want '#'", le.Char)
	}
	if le.Snippet == "" {
		t.Error("Snippet should not be empty")
	}
}

func TestTokenizeDebug(t *testing.T) {
	var log strings.Builder
	_, err := TokenizeEx("a + b", TokenizeOpts{Debug: true, DebugLog: &log})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "[commit]") {
		t.Errorf("expected debug log to contain [commit], got:\n%s", log.String())
	}
	if !strings.Contains(log.String(), "[single]") {
		t.Errorf("expected debug log to contain [single], got:\n%s", log.String())
	}
}

func TestTypeKindConsistency(t *testing.T) {
	// 确保 typeNames / typeKinds 数组长度一致，避免运行时越界
	if len(typeNames) != len(typeKinds) {
		t.Errorf("typeNames len=%d, typeKinds len=%d, must be equal",
			len(typeNames), len(typeKinds))
	}
	// 抽查 kind 类别
	checkPairs := map[Type]Kind{
		TypeSYS_PACK:   KindSYS,
		TypeKW_IF:      KindKW,
		TypeTYPE_STR:   KindTYPE,
		TypeOP_ADD:     KindOP,
		TypeSEP_LPAREN: KindSEP,
		TypeID:         KindIDENT,
		TypeCALL:       KindCALL,
		TypeEDGE_NAME:  KindEDGE,
		TypeSTRING:     KindSTRING,
		TypeNUM:        KindNUM,
		TypeEOF:        KindEOF,
	}
	for tt, want := range checkPairs {
		if typeKinds[tt] != want {
			t.Errorf("typeKinds[%s] = %s, want %s", tt, typeKinds[tt], want)
		}
	}
}

// ──── helpers ────

func assertEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("length mismatch: got %d, want %d\n  got:  %v\n  want: %v",
			len(got), len(want), got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
