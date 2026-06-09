package nfa

import "lexical_analysis/internal/regex"

// ─────────────────────────────────────────────
// 叶子处理
// ─────────────────────────────────────────────

// processLeaf 处理叶子节点：Literal / CharClass / Dot / Empty。
// 直接创建 s, e 状态、加边。
//
// **关键**：叶子节点的 e 状态**不**标记 AcceptTags。
// 因为叶子节点可能是 Concat/Plus/Star 等复合结构的中转点，
// 只有最外层 fragment 的最终 e 才是真正的接受态（由 Build 入口统一标记）。
// 这里若把每个叶子 e 都打上 AcceptTags，会让 determineAccept 在
// 任何"经过叶子 e"的位置都误判为接受——这正是合并 DFA 时 KW 错误
// 抢占 ID 的根因。
func (b *Builder) processLeaf(r regex.Regex, tokenKey string) (s, e int) {
	s, e = b.nfa.NewState(), b.nfa.NewState()
	switch a := r.(type) {
	case *regex.Literal:
		b.nfa.AddTrans(s, a.Ch, e)
	case *regex.CharClass:
		for _, ch := range a.Chars {
			b.nfa.AddTrans(s, ch, e)
		}
	case *regex.Dot:
		for c := 0; c < 256; c++ {
			b.nfa.AddTrans(s, byte(c), e)
		}
	case *regex.Empty:
		b.nfa.AddEpsilon(s, e)
	}
	return s, e
}

// ─────────────────────────────────────────────
// Thompson combine 规则（Builder 的方法）
// ─────────────────────────────────────────────

// concatRule: pop right, pop left, 加 1 条 ε
// Concat.Start = left.Start（不新建 s）
// Concat.End   = right.End （不新建 e）
func (b *Builder) concatRule() (s, e int) {
	rs, re := b.popFrag()
	ls, le := b.popFrag()
	b.nfa.AddEpsilon(le, rs)
	return ls, re
}

// unionRule: pop right, pop left, 新建 s,e, 加 4 条 ε
// Union.Start = 新建 s
// Union.End   = 新建 e
func (b *Builder) unionRule() (s, e int) {
	rs, re := b.popFrag()
	ls, le := b.popFrag()
	s, e = b.nfa.NewState(), b.nfa.NewState()
	b.nfa.AddEpsilon(s, ls) // 进入 left
	b.nfa.AddEpsilon(s, rs) // 进入 right
	b.nfa.AddEpsilon(le, e) // 退出 left
	b.nfa.AddEpsilon(re, e) // 退出 right
	return s, e
}

// starRule: pop inner, 新建 s,e, 加 4 条 ε（含自环）
// Star.Start = 新建 s
// Star.End   = 新建 e
func (b *Builder) starRule() (s, e int) {
	is, ie := b.popFrag()
	s, e = b.nfa.NewState(), b.nfa.NewState()
	b.nfa.AddEpsilon(s, is)  // 进入
	b.nfa.AddEpsilon(s, e)   // 跳过（0 次）
	b.nfa.AddEpsilon(ie, e)  // 退出
	b.nfa.AddEpsilon(ie, is) // 自环
	return s, e
}

// plusRule: pop inner, 复制给 StarRule, 焊接
// Plus.Start = inner.Start（= Star 之 inner 的入口）
// Plus.End   = Star.End
//
// 关键：Plus = Concat(inner, Star(inner))，两份 inner 用同一个 fragment。
// 所以复制一份 (s, e) 即可——不是真的复制状态。
func (b *Builder) plusRule() (s, e int) {
	is, ie := b.popFrag()
	b.pushFrag(is, ie) // 复制一份给 StarRule 用
	ss, se := b.starRule()
	b.nfa.AddEpsilon(ie, ss) // 焊接：inner.e → Star.s
	return is, se
}

// optionalRule: pop inner, 新建 s,e, 加 3 条 ε
// Optional.Start = 新建 s
// Optional.End   = 新建 e
func (b *Builder) optionalRule() (s, e int) {
	is, ie := b.popFrag()
	s, e = b.nfa.NewState(), b.nfa.NewState()
	b.nfa.AddEpsilon(s, is) // 进入
	b.nfa.AddEpsilon(s, e)  // 跳过（0 次）
	b.nfa.AddEpsilon(ie, e) // 退出
	return s, e
}

// groupRule: pop inner, 透传
// Group.Start = inner.Start
// Group.End   = inner.End
func (b *Builder) groupRule() (s, e int) {
	return b.popFrag()
}

// ─────────────────────────────────────────────
// Repeat 规则
// ─────────────────────────────────────────────

// repeatRule 分发到具体 Repeat 形式。
func (b *Builder) repeatRule(a *regex.Repeat) (s, e int) {
	if a.Min == a.Max {
		return b.repeatExact(a.Inner, a.Min)
	}
	if a.Max == -1 {
		return b.repeatAtLeast(a.Inner, a.Min)
	}
	return b.repeatRange(a.Inner, a.Min, a.Max)
}

// repeatExact {n}: Concat(inner, inner, ..., inner) n 次
// 用 buildSub 构造 n 个独立的 inner 片段，再 Concat。
func (b *Builder) repeatExact(inner regex.Regex, n int) (s, e int) {
	if n == 0 {
		s, e = b.nfa.NewState(), b.nfa.NewState()
		b.nfa.AddEpsilon(s, e)
		return s, e
	}
	// 第一个
	s, e = b.buildSub(inner)
	for i := 1; i < n; i++ {
		ns, ne := b.buildSub(inner)
		b.nfa.AddEpsilon(e, ns)
		e = ne
	}
	return s, e
}

// repeatAtLeast {n,}: Concat(inner^n, Star(inner))
func (b *Builder) repeatAtLeast(inner regex.Regex, n int) (s, e int) {
	if n == 0 {
		return b.starOfFragment(inner)
	}
	// 先串 n 份
	s, e = b.repeatExact(inner, n)
	// 再接 Star(inner)：用 buildSub 拿到 Star 片段
	starS, starE := b.starOfFragment(inner)
	b.nfa.AddEpsilon(e, starS)
	return s, starE
}

// repeatRange {n,m}: Union(A^n, A^(n+1), ..., A^m)
func (b *Builder) repeatRange(inner regex.Regex, n, m int) (s, e int) {
	// 先构造 n 份
	s, e = b.repeatExact(inner, n)
	// 再 union (n+1..m) 份
	for k := n + 1; k <= m; k++ {
		ks, ke := b.repeatExact(inner, k)
		// Union(s, e) 和 (ks, ke)
		ns, ne := b.nfa.NewState(), b.nfa.NewState()
		b.nfa.AddEpsilon(ns, s)
		b.nfa.AddEpsilon(ns, ks)
		b.nfa.AddEpsilon(e, ne)
		b.nfa.AddEpsilon(ke, ne)
		s, e = ns, ne
	}
	return s, e
}

// starOfFragment 构造 Star(inner) 片段（递归辅助）
func (b *Builder) starOfFragment(inner regex.Regex) (s, e int) {
	is, ie := b.buildSub(inner)
	ns, ne := b.nfa.NewState(), b.nfa.NewState()
	b.nfa.AddEpsilon(ns, is)
	b.nfa.AddEpsilon(ns, ne)
	b.nfa.AddEpsilon(ie, ne)
	b.nfa.AddEpsilon(ie, is)
	return ns, ne
}

// ─────────────────────────────────────────────
// buildSub: 递归辅助，构造一个独立的子片段
// （Repeat 需要多份独立 inner，所以用递归而不是 fragment 复制）
// ─────────────────────────────────────────────

func (b *Builder) buildSub(r regex.Regex) (s, e int) {
	switch a := r.(type) {
	case *regex.Literal, *regex.CharClass, *regex.Dot, *regex.Empty:
		return b.processLeaf(r, "")
	case *regex.Concat:
		ls, le := b.buildSub(a.Left)
		rs, re := b.buildSub(a.Right)
		b.nfa.AddEpsilon(le, rs)
		return ls, re
	case *regex.Union:
		rs, re := b.buildSub(a.Right)
		ls, le := b.buildSub(a.Left)
		ns, ne := b.nfa.NewState(), b.nfa.NewState()
		b.nfa.AddEpsilon(ns, ls)
		b.nfa.AddEpsilon(ns, rs)
		b.nfa.AddEpsilon(le, ne)
		b.nfa.AddEpsilon(re, ne)
		return ns, ne
	case *regex.Star:
		is, ie := b.buildSub(a.Inner)
		ns, ne := b.nfa.NewState(), b.nfa.NewState()
		b.nfa.AddEpsilon(ns, is)
		b.nfa.AddEpsilon(ns, ne)
		b.nfa.AddEpsilon(ie, ne)
		b.nfa.AddEpsilon(ie, is)
		return ns, ne
	case *regex.Plus:
		is, ie := b.buildSub(a.Inner)
		b.pushFrag(is, ie)
		ss, se := b.starRule()
		b.nfa.AddEpsilon(ie, ss)
		return is, se
	case *regex.Optional:
		is, ie := b.buildSub(a.Inner)
		ns, ne := b.nfa.NewState(), b.nfa.NewState()
		b.nfa.AddEpsilon(ns, is)
		b.nfa.AddEpsilon(ns, ne)
		b.nfa.AddEpsilon(ie, ne)
		return ns, ne
	case *regex.Group:
		return b.buildSub(a.Inner)
	case *regex.Repeat:
		return b.repeatRule(a)
	}
	panic("nfa.buildSub: unknown AST node type")
}
