package dfa

import (
	"strings"
	"testing"

	"lexical_analysis/internal/nfa"
	"lexical_analysis/internal/regex"
)

// ─────────────────────────────────────────────
// 辅助：构造 NFA 列表
// ─────────────────────────────────────────────

func buildNFAs(t *testing.T, specs map[string]string) []*nfa.NFA {
	t.Helper()
	nfas := make([]*nfa.NFA, 0, len(specs))
	keys := make([]string, 0, len(specs))
	// 用 map 是为了测试方便；这里按插入顺序遍历（Go map 不保证顺序）
	// 实际生产时 CombinedNFA 接有序列表
	for k, v := range specs {
		ast := regex.MustParse(v)
		b := nfa.NewBuilder()
		b.Build(ast, k)
		nfas = append(nfas, b.NFA())
		keys = append(keys, k)
	}
	return nfas
}

// ─────────────────────────────────────────────
// ε-closure 测试
// ─────────────────────────────────────────────

func TestEpsilonClosure_Simple(t *testing.T) {
	// Concat(CharClass, Star(CharClass)) —— 类似 ID
	ast := regex.MustParse(`[a-z][a-z]*`)
	b := nfa.NewBuilder()
	b.Build(ast, "T")
	n := b.NFA()

	// Start 状态的 ε-closure 应该只有自己
	closure := epsilonClosure(n, n.Start)
	if len(closure) != 1 || closure[0] != n.Start {
		t.Errorf("Start ε-closure = %v, want [%d]", closure, n.Start)
	}
}

func TestEpsilonClosure_HasEpsilon(t *testing.T) {
	// Star(CharClass) —— Star 内部有 4 条 ε 边
	ast := regex.MustParse(`[a-z]*`)
	b := nfa.NewBuilder()
	b.Build(ast, "T")
	n := b.NFA()

	// 详细追踪：
	//   CharClass: 0, 1
	//   Star 新建: 2 (s, Start!), 3 (e, accept)
	//   Epsilon: 2→0 (enter), 2→3 (skip), 1→3 (exit), 1→0 (loop)
	// Start = 2（Star 外层 wrapper，**不是 0**）

	// Start = 2 的 ε-closure: 2→{0,3}，0/3 没 ε 出
	// 结果: {0, 2, 3}
	closure := epsilonClosure(n, n.Start)
	if !equalIntSlice(closure, []int{0, 2, 3}) {
		t.Errorf("Start(%d) ε-closure = %v, want [0, 2, 3]", n.Start, closure)
	}

	// s=0 没有 ε 边
	closure = epsilonClosure(n, 0)
	if len(closure) != 1 || closure[0] != 0 {
		t.Errorf("CharClass s=0 ε-closure = %v, want [0]", closure)
	}

	// s=1 有 ε→3 和 ε→0
	closure = epsilonClosure(n, 1)
	if !equalIntSlice(closure, []int{0, 1, 3}) {
		t.Errorf("s=1 ε-closure = %v, want [0, 1, 3]", closure)
	}
}

func TestEpsilonClosureSet(t *testing.T) {
	ast := regex.MustParse(`[a-z]*`)
	b := nfa.NewBuilder()
	b.Build(ast, "T")
	n := b.NFA()

	// 详细追踪：s=0 没 ε 边, s=1 有 ε→0, ε→3
	// ε-closure({0, 1}) = {0, 1, 3}
	closure := epsilonClosureSet(n, []int{0, 1})
	if !equalIntSlice(closure, []int{0, 1, 3}) {
		t.Errorf("ε-closureSet = %v, want [0, 1, 3]", closure)
	}
}

func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// ─────────────────────────────────────────────
// dfaState 标准化与等价性
// ─────────────────────────────────────────────

func TestCanonicalize(t *testing.T) {
	// 顺序不同的 TokenStartSet 列表，标准化后应该相等
	a := dfaState{
		{TokenIdx: 2, States: []int{3, 4}},
		{TokenIdx: 0, States: []int{1}},
		{TokenIdx: 1, States: []int{2}},
	}
	b := dfaState{
		{TokenIdx: 0, States: []int{1}},
		{TokenIdx: 1, States: []int{2}},
		{TokenIdx: 2, States: []int{3, 4}},
	}

	if !canonicalize(a).equals(canonicalize(b)) {
		t.Errorf("Canonical forms should be equal:\n  a=%v\n  b=%v", canonicalize(a), canonicalize(b))
	}
	if canonicalize(a).hash() != canonicalize(b).hash() {
		t.Errorf("Hashes should match: %s vs %s", canonicalize(a).hash(), canonicalize(b).hash())
	}
}

func TestEquals(t *testing.T) {
	a := dfaState{{TokenIdx: 0, States: []int{1, 2}}}
	b := dfaState{{TokenIdx: 0, States: []int{1, 2}}}
	c := dfaState{{TokenIdx: 0, States: []int{2, 1}}} // 内部顺序不同
	d := dfaState{{TokenIdx: 0, States: []int{1}}}    // 长度不同

	if !a.equals(b) {
		t.Error("a and b should be equal")
	}
	// 内部顺序不同：当前 equals 直接比较 slice，所以不相等（用户层面应先 canonicalize）
	if a.equals(c) {
		t.Error("a and c should NOT be equal (raw, not canonicalized)")
	}
	if a.equals(d) {
		t.Error("a and d should NOT be equal")
	}
}

// ─────────────────────────────────────────────
// DFA 基础操作
// ─────────────────────────────────────────────

func TestDFA_addCanonicalState(t *testing.T) {
	d := NewDFA()
	s1 := dfaState{{TokenIdx: 0, States: []int{1}}}
	id1 := d.addCanonicalState(s1)
	if id1 != 0 {
		t.Errorf("first add should return 0, got %d", id1)
	}

	// 重新加等价状态，应该返回相同 ID
	s2 := dfaState{{TokenIdx: 0, States: []int{1}}}
	id2 := d.addCanonicalState(s2)
	if id2 != id1 {
		t.Errorf("equivalent state should return same id, got %d", id2)
	}

	// 不同状态
	s3 := dfaState{{TokenIdx: 0, States: []int{2}}}
	id3 := d.addCanonicalState(s3)
	if id3 == id1 {
		t.Errorf("different state should return new id, but got same %d", id3)
	}
}

func TestDFA_AddTransition_Dedup(t *testing.T) {
	d := NewDFA()
	d.addCanonicalState(dfaState{{TokenIdx: 0, States: []int{0}}})
	d.AddTransition(0, 'a', 1)
	d.AddTransition(0, 'a', 1) // 重复，应该去重
	if len(d.GetTrans()[0]['a']) != 1 {
		t.Errorf("Transition should be deduplicated, got %v", d.GetTrans()[0]['a'])
	}
}

// ─────────────────────────────────────────────
// CombinedNFA + 子集构造
// ─────────────────────────────────────────────

func TestToDFA_Simple(t *testing.T) {
	// 两个简单 token: "a" 和 "b"
	ast1 := regex.MustParse("a")
	b1 := nfa.NewBuilder()
	b1.Build(ast1, "T_A")
	nfa1 := b1.NFA()

	ast2 := regex.MustParse("b")
	b2 := nfa.NewBuilder()
	b2.Build(ast2, "T_B")
	nfa2 := b2.NFA()

	c := NewCombinedNFA([]*nfa.NFA{nfa1, nfa2}, []string{"T_A", "T_B"})
	d := c.ToDFA()

	// 起点：每 token 的 start closure
	if d.GetStart() != 0 {
		t.Errorf("Start should be 0, got %d", d.GetStart())
	}
	// 状态数：起点 + 2 个接受态（T_A 和 T_B）= 3
	if d.GetNumStates() != 3 {
		t.Errorf("Expected 3 states (start + 2 accepts), got %d", d.GetNumStates())
	}

	// 起点应是接受态？不一定，'a' 和 'b' 都需要读一个字符才到接受
	// 但起点本身不在接受态
	if _, ok := d.AcceptTag(0); ok {
		t.Errorf("Start should not be accept (both tokens need at least 1 char)")
	}
}

func TestToDFA_SingleCharTokens(t *testing.T) {
	// ";" 和 ","
	ast1 := regex.MustParse(`;`)
	b1 := nfa.NewBuilder()
	b1.Build(ast1, "SEMI")
	nfa1 := b1.NFA()

	ast2 := regex.MustParse(`,`)
	b2 := nfa.NewBuilder()
	b2.Build(ast2, "COMMA")
	nfa2 := b2.NFA()

	c := NewCombinedNFA([]*nfa.NFA{nfa1, nfa2}, []string{"SEMI", "COMMA"})
	d := c.ToDFA()

	// 起点 → 读 ';' → accept(SEMI)
	// 起点 → 读 ',' → accept(COMMA)
	// 起点 → 读其他 → 死路
	// 状态：起点 + SEMI接受 + COMMA接受 = 3

	if d.GetNumStates() < 3 {
		t.Errorf("Expected at least 3 states, got %d", d.GetNumStates())
	}

	// 验证能接受 ";"
	state := d.GetStart()
	tos := d.GetTrans()[state][';']
	if len(tos) == 0 {
		t.Fatal("No transition on ';' from start")
	}
	tag, ok := d.AcceptTag(tos[0])
	if !ok || tag != "SEMI" {
		t.Errorf("';' should lead to SEMI accept, got %s ok=%v", tag, ok)
	}
}

func TestToDFA_Overlapping(t *testing.T) {
	// "if" 和 "id"——前两个字符重叠
	ast1 := regex.MustParse("if")
	b1 := nfa.NewBuilder()
	b1.Build(ast1, "KW_IF")
	nfa1 := b1.NFA()

	ast2 := regex.MustParse("id")
	b2 := nfa.NewBuilder()
	b2.Build(ast2, "ID")
	nfa2 := b2.NFA()

	c := NewCombinedNFA([]*nfa.NFA{nfa1, nfa2}, []string{"KW_IF", "ID"})
	d := c.ToDFA()

	// 起点 → 'i' → 状态 A（KW_IF 在 A=1, ID 在 A=0 后的状态）
	// A → 'f' → KW_IF accept
	// A → 'd' → ID accept
	state0 := d.GetStart()
	tosI := d.GetTrans()[state0]['i']
	if len(tosI) == 0 {
		t.Fatal("No 'i' transition from start")
	}
	stateA := tosI[0]

	tosF := d.GetTrans()[stateA]['f']
	if len(tosF) == 0 {
		t.Fatal("No 'f' transition after 'i'")
	}
	tag, ok := d.AcceptTag(tosF[0])
	if !ok || tag != "KW_IF" {
		t.Errorf("'if' should accept KW_IF, got %s ok=%v", tag, ok)
	}

	tosD := d.GetTrans()[stateA]['d']
	if len(tosD) == 0 {
		t.Fatal("No 'd' transition after 'i'")
	}
	tag, ok = d.AcceptTag(tosD[0])
	if !ok || tag != "ID" {
		t.Errorf("'id' should accept ID, got %s ok=%v", tag, ok)
	}
}

func TestToDFA_PriorityByOrder(t *testing.T) {
	// "if" (先定义) 和 "if" (后定义) 都是相同字符串
	// 接受时应该选先定义的
	ast1 := regex.MustParse("if")
	b1 := nfa.NewBuilder()
	b1.Build(ast1, "FIRST")
	nfa1 := b1.NFA()

	ast2 := regex.MustParse("if")
	b2 := nfa.NewBuilder()
	b2.Build(ast2, "SECOND")
	nfa2 := b2.NFA()

	c := NewCombinedNFA([]*nfa.NFA{nfa1, nfa2}, []string{"FIRST", "SECOND"})
	d := c.ToDFA()

	// 跑 "if" → 应选 FIRST
	state0 := d.GetStart()
	tosI := d.GetTrans()[state0]['i']
	if len(tosI) == 0 {
		t.Fatal("No 'i' transition")
	}
	tosF := d.GetTrans()[tosI[0]]['f']
	if len(tosF) == 0 {
		t.Fatal("No 'f' transition")
	}
	tag, _ := d.AcceptTag(tosF[0])
	if tag != "FIRST" {
		t.Errorf("Expected FIRST (先定义优先), got %s", tag)
	}
}

// ─────────────────────────────────────────────
// 真实 token 测试
// ─────────────────────────────────────────────

func TestToDFA_RealTokens(t *testing.T) {
	// 3 个 token: "if", "id", ";"
	specs := map[string]string{
		"KW_IF": "if",
		"ID":    "id",
		"SEMI":  `;`,
	}

	nfas := make([]*nfa.NFA, 0, len(specs))
	keys := make([]string, 0, len(specs))
	for k, v := range specs {
		ast := regex.MustParse(v)
		b := nfa.NewBuilder()
		b.Build(ast, k)
		nfas = append(nfas, b.NFA())
		keys = append(keys, k)
	}

	c := NewCombinedNFA(nfas, keys)
	d := c.ToDFA()

	if d.GetNumStates() < 4 {
		t.Errorf("Expected at least 4 states, got %d", d.GetNumStates())
	}

	// 模拟 "if" → 应得 KW_IF
	state := d.GetStart()
	for _, ch := range []byte("if") {
		tos := d.GetTrans()[state][ch]
		if len(tos) == 0 {
			t.Fatalf("No transition on %q", ch)
		}
		state = tos[0]
	}
	tag, ok := d.AcceptTag(state)
	if !ok || tag != "KW_IF" {
		t.Errorf("'if' should accept KW_IF, got %s ok=%v", tag, ok)
	}

	// 模拟 ";" → 应得 SEMI
	state = d.GetStart()
	tos := d.GetTrans()[state][';']
	if len(tos) == 0 {
		t.Fatal("No ';' transition")
	}
	tag, ok = d.AcceptTag(tos[0])
	if !ok || tag != "SEMI" {
		t.Errorf("';' should accept SEMI, got %s ok=%v", tag, ok)
	}
}

// ─────────────────────────────────────────────
// 输出测试
// ─────────────────────────────────────────────

func TestDFA_ToTXT(t *testing.T) {
	ast := regex.MustParse("a")
	b := nfa.NewBuilder()
	b.Build(ast, "T_A")
	nfa1 := b.NFA()

	c := NewCombinedNFA([]*nfa.NFA{nfa1}, []string{"T_A"})
	d := c.ToDFA()
	txt := d.ToTXT()
	if !strings.Contains(txt, "DFA:") {
		t.Error("ToTXT should contain 'DFA:'")
	}
	if !strings.Contains(txt, "T_A") {
		t.Error("ToTXT should contain accept tag 'T_A'")
	}
}

func TestDFA_ToDOT(t *testing.T) {
	ast := regex.MustParse("a")
	b := nfa.NewBuilder()
	b.Build(ast, "T_A")
	nfa1 := b.NFA()

	c := NewCombinedNFA([]*nfa.NFA{nfa1}, []string{"T_A"})
	d := c.ToDFA()
	dot := d.ToDOT()
	if !strings.Contains(dot, "digraph dfa") {
		t.Error("ToDOT should contain 'digraph dfa'")
	}
	if !strings.Contains(dot, "doublecircle") {
		t.Error("Accept state should use doublecircle")
	}
}

// ─────────────────────────────────────────────
// 最小化测试
// ─────────────────────────────────────────────

// tokenizeLongest 用 DFA 找最长合法前缀。
func tokenizeLongest(d *DFA, input string) ([]string, string) {
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
			return false
		}
	}
	return true
}

func TestMinimize_Simple(t *testing.T) {
	// 两个不同 token "a" 和 "b"
	nfas := []*nfa.NFA{}
	keys := []string{}
	for k, v := range map[string]string{"A": "a", "B": "b"} {
		ast := regex.MustParse(v)
		b := nfa.NewBuilder()
		b.Build(ast, k)
		nfas = append(nfas, b.NFA())
		keys = append(keys, k)
	}
	raw := NewCombinedNFA(nfas, keys).ToDFA()
	min := raw.Minimize()

	// 最小化后状态数应 ≤ 原始
	if min.GetNumStates() > raw.GetNumStates() {
		t.Errorf("Minimize increased states: %d → %d", raw.GetNumStates(), min.GetNumStates())
	}
	// 接受态应该都是唯一的
	if len(min.GetAccepts()) != 2 {
		t.Errorf("Expected 2 accepts, got %d", len(min.GetAccepts()))
	}
}

func TestMinimize_MergesEquivalent(t *testing.T) {
	// "a*" 和 "[ab]*" 共享一些状态？构造一个能产生重复 ID 接受态的 DFA
	// 用 "if" 和 "id" 关键字示例
	nfas := []*nfa.NFA{}
	keys := []string{}
	for k, v := range map[string]string{"KW": "if", "ID": `[a-z]+`} {
		ast := regex.MustParse(v)
		b := nfa.NewBuilder()
		b.Build(ast, k)
		nfas = append(nfas, b.NFA())
		keys = append(keys, k)
	}
	raw := NewCombinedNFA(nfas, keys).ToDFA()
	min := raw.Minimize()

	// 最小化前后 ID 接受态数目对比
	rawIDCount := 0
	for _, tag := range raw.GetAccepts() {
		if tag == "ID" {
			rawIDCount++
		}
	}
	minIDCount := 0
	for _, tag := range min.GetAccepts() {
		if tag == "ID" {
			minIDCount++
		}
	}
	if minIDCount > rawIDCount {
		t.Errorf("Minimize increased ID accepts: %d → %d", rawIDCount, minIDCount)
	}
	// 最小化后至少还有 1 个 ID 接受态
	if minIDCount < 1 {
		t.Errorf("Expected at least 1 ID accept, got %d", minIDCount)
	}
}

func TestMinimize_PreservesBehavior(t *testing.T) {
	// 构造一个 DFA，最小化后跑分词，验证 token 流不变
	nfas := []*nfa.NFA{}
	keys := []string{}
	for k, v := range map[string]string{
		"KW_IF": "if",
		"ID":    `[a-z]+`,
		"NUM":   `[0-9]+`,
		";":     `\;`,
	} {
		ast := regex.MustParse(v)
		b := nfa.NewBuilder()
		b.Build(ast, k)
		nfas = append(nfas, b.NFA())
		keys = append(keys, k)
	}
	raw := NewCombinedNFA(nfas, keys).ToDFA()
	min := raw.Minimize()

	tests := []struct {
		input    string
		expected []string
	}{
		{"if", []string{"KW_IF"}},
		{"ifoo", []string{"ID"}},
		{"123", []string{"NUM"}},
		{";", []string{";"}},
		{"if;", []string{"KW_IF", ";"}},
		{"ifoo;123", []string{"ID", ";", "NUM"}},
	}

	for _, tc := range tests {
		gotRaw, _ := tokenizeLongest(raw, tc.input)
		gotMin, _ := tokenizeLongest(min, tc.input)
		if !sliceEq(gotRaw, tc.expected) {
			t.Errorf("raw tokenize(%q) = %v, want %v", tc.input, gotRaw, tc.expected)
		}
		if !sliceEq(gotMin, tc.expected) {
			t.Errorf("min tokenize(%q) = %v, want %v", tc.input, gotMin, tc.expected)
		}
	}
}
