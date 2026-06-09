package nfa

import (
	"sort"
	"strings"
	"testing"

	"lexical_analysis/internal/regex"
)

// ─────────────────────────────────────────────
// 基础叶子测试
// ─────────────────────────────────────────────

func TestBuild_Literal(t *testing.T) {
	ast := regex.MustParse("a")
	b := NewBuilder()
	s, e := b.Build(ast, "T")

	if s != 0 || e != 1 {
		t.Errorf("Start=%d Accept=%d, want 0, 1", s, e)
	}
	if b.NFA().Start != 0 {
		t.Errorf("NFA.Start=%d, want 0", b.NFA().Start)
	}
	if len(b.NFA().Accepts) != 1 || b.NFA().Accepts[0] != 1 {
		t.Errorf("Accepts=%v, want [1]", b.NFA().Accepts)
	}
	if b.NFA().AcceptTags[1] != "T" {
		t.Errorf("AcceptTags[1]=%q, want T", b.NFA().AcceptTags[1])
	}
	if tos := b.NFA().Trans[0]['a']; len(tos) != 1 || tos[0] != 1 {
		t.Errorf("Trans[0]['a']=%v, want [1]", tos)
	}
	if b.NFA().NextID != 2 {
		t.Errorf("NextID=%d, want 2", b.NFA().NextID)
	}
}

func TestBuild_CharClass(t *testing.T) {
	ast := regex.MustParse(`[a-z]`)
	b := NewBuilder()
	s, e := b.Build(ast, "CC")

	if s != 0 || e != 1 {
		t.Errorf("Start=%d Accept=%d, want 0, 1", s, e)
	}
	// 应该有 26 个字符边
	if len(b.NFA().Trans[0]) != 26 {
		t.Errorf("Trans[0] has %d chars, want 26", len(b.NFA().Trans[0]))
	}
	if b.NFA().Trans[0]['a'] == nil {
		t.Error("Trans[0]['a'] missing")
	}
	if b.NFA().Trans[0]['z'] == nil {
		t.Error("Trans[0]['z'] missing")
	}
}

// ─────────────────────────────────────────────
// 组合规则测试
// ─────────────────────────────────────────────

func TestBuild_Concat(t *testing.T) {
	// [a-zA-Z_][a-zA-Z0-9_]*  ← 类似 ID
	ast := regex.MustParse(`[a-zA-Z_][a-zA-Z0-9_]*`)
	b := NewBuilder()
	s, e := b.Build(ast, "ID")

	// Concat 不新建 s, e
	// left CharClass: 0, 1
	// right CharClass (inner of Star): 2, 3
	// Star 新建: 4, 5 (接受态)
	// Concat.Start = left.Start = 0
	// Concat.End = right.End (Star's end) = 5
	if s != 0 {
		t.Errorf("Start=%d, want 0 (Concat reuses left.Start)", s)
	}
	if e != 5 {
		t.Errorf("End=%d, want 5 (Star's end)", e)
	}
	if b.NFA().NextID != 6 {
		t.Errorf("NextID=%d, want 6", b.NFA().NextID)
	}
	if b.NFA().Accepts[0] != 5 {
		t.Errorf("Accepts[0]=%d, want 5", b.NFA().Accepts[0])
	}
}

func TestBuild_Star(t *testing.T) {
	// [a-z]*
	ast := regex.MustParse(`[a-z]*`)
	b := NewBuilder()
	s, e := b.Build(ast, "STAR")

	// CharClass: 0, 1
	// Star 新建: 2, 3
	// Star.Start = 2, Star.End = 3
	if s != 2 {
		t.Errorf("Start=%d, want 2 (Star creates new s)", s)
	}
	if e != 3 {
		t.Errorf("End=%d, want 3 (Star creates new e)", e)
	}
	if b.NFA().NextID != 4 {
		t.Errorf("NextID=%d, want 4", b.NFA().NextID)
	}

	// Star 的 4 条 ε 边
	// s=2 → is=0, s=2 → e=3, ie=1 → e=3, ie=1 → is=0
	if !containsAll(b.NFA().Epsilon[2], 0, 3) {
		t.Errorf("Epsilon[2] should contain 0 and 3, got %v", b.NFA().Epsilon[2])
	}
	if !containsAll(b.NFA().Epsilon[1], 3, 0) {
		t.Errorf("Epsilon[1] should contain 3 and 0, got %v", b.NFA().Epsilon[1])
	}
}

func TestBuild_Plus(t *testing.T) {
	// [a-z]+
	ast := regex.MustParse(`[a-z]+`)
	b := NewBuilder()
	s, e := b.Build(ast, "PLUS")

	// CharClass: 0, 1
	// Star wrapper: 2, 3
	// Plus.Start = 0 (= inner.Start)
	// Plus.End = 3 (= Star.End)
	if s != 0 {
		t.Errorf("Start=%d, want 0 (Plus reuses inner.Start)", s)
	}
	if e != 3 {
		t.Errorf("End=%d, want 3 (Plus.End = Star.End)", e)
	}
	if b.NFA().NextID != 4 {
		t.Errorf("NextID=%d, want 4", b.NFA().NextID)
	}
	// Plus 焊接边：inner.e=1 → Star.s=2
	if !contains(b.NFA().Epsilon[1], 2) {
		t.Errorf("Epsilon[1] should contain 2 (Plus weld), got %v", b.NFA().Epsilon[1])
	}
}

func TestBuild_Optional(t *testing.T) {
	// [a-z]?
	ast := regex.MustParse(`[a-z]?`)
	b := NewBuilder()
	s, e := b.Build(ast, "OPT")

	// CharClass: 0, 1
	// Optional 新建: 2, 3
	// Optional.Start = 2, Optional.End = 3
	if s != 2 {
		t.Errorf("Start=%d, want 2", s)
	}
	if e != 3 {
		t.Errorf("End=%d, want 3", e)
	}
	// Optional 的 3 条 ε 边
	// s=2 → is=0, s=2 → e=3, ie=1 → e=3
	if !containsAll(b.NFA().Epsilon[2], 0, 3) {
		t.Errorf("Epsilon[2] should contain 0 and 3, got %v", b.NFA().Epsilon[2])
	}
	if !contains(b.NFA().Epsilon[1], 3) {
		t.Errorf("Epsilon[1] should contain 3, got %v", b.NFA().Epsilon[1])
	}
}

func TestBuild_Group(t *testing.T) {
	// (a)
	ast := regex.MustParse(`(a)`)
	b := NewBuilder()
	s, e := b.Build(ast, "G")

	// Group 透传：start=0, end=1
	if s != 0 || e != 1 {
		t.Errorf("Start=%d End=%d, want 0, 1 (Group is transparent)", s, e)
	}
}

func TestBuild_Union(t *testing.T) {
	// a|b
	ast := regex.MustParse(`a|b`)
	b := NewBuilder()
	s, e := b.Build(ast, "U")

	// a: 0, 1
	// b: 2, 3
	// Union 新建: 4, 5
	// Union.Start = 4, Union.End = 5
	if s != 4 {
		t.Errorf("Start=%d, want 4", s)
	}
	if e != 5 {
		t.Errorf("End=%d, want 5", e)
	}
}

// ─────────────────────────────────────────────
// 真实 token 测试
// ─────────────────────────────────────────────

func TestBuild_RealToken_ID(t *testing.T) {
	ast := regex.MustParse(`[a-zA-Z_][a-zA-Z0-9_]*`)
	b := NewBuilder()
	s, e := b.Build(ast, "ID")

	// Start=0, End=5, 6 个状态
	if s != 0 {
		t.Errorf("Start=%d, want 0", s)
	}
	if e != 5 {
		t.Errorf("End=%d, want 5", e)
	}
	if b.NFA().NextID != 6 {
		t.Errorf("NextID=%d, want 6", b.NFA().NextID)
	}

	// 验证关键边
	if !contains(b.NFA().Epsilon[1], 4) {
		t.Error("Concat weld: state 1 should ε→ state 4")
	}
}

func TestBuild_RealToken_NUM(t *testing.T) {
	ast := regex.MustParse(`[+\-]?[0-9]+`)
	b := NewBuilder()
	s, e := b.Build(ast, "NUM")

	// 详细追踪：
	//   Optional 的 inner CharClass: 0, 1
	//   Optional 新建 wrapper: 2, 3
	//   Plus 的 inner CharClass: 4, 5
	//   Plus 新建 wrapper (via Star): 6, 7
	//   Concat welds: 3 → 6
	//   Concat.Start = Optional.Start = 2
	//   Concat.End   = Plus.End       = 7
	if s != 2 {
		t.Errorf("Start=%d, want 2 (Concat reuses Optional.Start)", s)
	}
	if e != 7 {
		t.Errorf("End=%d, want 7 (Concat reuses Plus.End)", e)
	}
	if b.NFA().NextID != 8 {
		t.Errorf("NextID=%d, want 8", b.NFA().NextID)
	}
	// 接受态只有 7（最后 fragment 的 e）
	if len(b.NFA().Accepts) != 1 || b.NFA().Accepts[0] != 7 {
		t.Errorf("Accepts=%v, want [7]", b.NFA().Accepts)
	}
}

func TestBuild_RealToken_OP_EQ(t *testing.T) {
	ast := regex.MustParse(`==`)
	b := NewBuilder()
	s, e := b.Build(ast, "OP_EQ")

	// == is two literals Concat: states 0,1,2,3
	// Concat.Start = 0, End = 3
	if s != 0 {
		t.Errorf("Start=%d, want 0", s)
	}
	if e != 3 {
		t.Errorf("End=%d, want 3", e)
	}
	if !contains(b.NFA().Trans[0]['='], 1) {
		t.Error("Trans[0]['='] should target state 1")
	}
	if !contains(b.NFA().Trans[2]['='], 3) {
		t.Error("Trans[2]['='] should target state 3")
	}
}

// ─────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────

func contains(slice []int, target int) bool {
	for _, v := range slice {
		if v == target {
			return true
		}
	}
	return false
}

func containsAll(slice []int, targets ...int) bool {
	for _, t := range targets {
		if !contains(slice, t) {
			return false
		}
	}
	return true
}

// ─────────────────────────────────────────────
// 输出测试
// ─────────────────────────────────────────────

func TestToTXT(t *testing.T) {
	ast := regex.MustParse(`a|b`)
	b := NewBuilder()
	b.Build(ast, "U")
	txt := b.NFA().ToTXT()
	if !strings.Contains(txt, "Start: s4") {
		t.Errorf("ToTXT should contain 'Start: s4', got:\n%s", txt)
	}
	if !strings.Contains(txt, "U") {
		t.Errorf("ToTXT should contain accept tag 'U', got:\n%s", txt)
	}
}

func TestToDOT(t *testing.T) {
	ast := regex.MustParse(`a|b`)
	b := NewBuilder()
	b.Build(ast, "U")
	dot := b.NFA().ToDOT()
	if !strings.Contains(dot, "digraph nfa") {
		t.Errorf("ToDOT should contain 'digraph nfa', got:\n%s", dot)
	}
	if !strings.Contains(dot, "start -> s4") {
		t.Errorf("ToDOT should contain 'start -> s4', got:\n%s", dot)
	}
	if !strings.Contains(dot, "ε") {
		t.Error("ToDOT should contain 'ε' edges")
	}
}

// 排序辅助（让 Epsilon 比较可重现）
func sortInts(s []int) []int {
	out := append([]int{}, s...)
	sort.Ints(out)
	return out
}
