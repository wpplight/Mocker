package nfa

import "lexical_analysis/internal/regex"

// isLeaf 判断节点是否是叶子。
func isLeaf(r regex.Regex) bool {
	switch r.(type) {
	case *regex.Literal, *regex.CharClass, *regex.Dot, *regex.Empty:
		return true
	}
	return false
}

// Build 是 NFA 构造的主入口（Builder 的方法）。
//
// 遍历栈只管"我走到哪了"，fragment 栈只管"我手上有哪些片段"。
// 两者职责彻底分离。
func (b *Builder) Build(root regex.Regex, tokenKey string) (s, e int) {
	b.travStack = append(b.travStack, travEntry{node: root})

	for len(b.travStack) > 0 {
		// 弹栈
		top := b.travStack[len(b.travStack)-1]
		b.travStack = b.travStack[:len(b.travStack)-1]

		if top.visited {
			// ── combine 阶段 ──
			switch a := top.node.(type) {
			case *regex.Concat:
				ns, ne := b.concatRule()
				b.pushFrag(ns, ne)
			case *regex.Union:
				ns, ne := b.unionRule()
				b.pushFrag(ns, ne)
			case *regex.Star:
				ns, ne := b.starRule()
				b.pushFrag(ns, ne)
			case *regex.Plus:
				ns, ne := b.plusRule()
				b.pushFrag(ns, ne)
			case *regex.Optional:
				ns, ne := b.optionalRule()
				b.pushFrag(ns, ne)
			case *regex.Repeat:
				ns, ne := b.repeatRule(a)
				b.pushFrag(ns, ne)
			case *regex.Group:
				ns, ne := b.groupRule()
				b.pushFrag(ns, ne)
			}
			continue
		}

		// ── 首次访问 ──
		if isLeaf(top.node) {
			s, e := b.processLeaf(top.node, tokenKey)
			b.pushFrag(s, e)
			continue
		}

		// 内部节点：自己二次入栈 + 子节点入栈
		b.travStack = append(b.travStack, travEntry{node: top.node, visited: true})
		b.pushChildren(top.node)
	}

	// 弹最后一个 fragment，就是整棵 AST 的 (s, e)
	s, e = b.popFrag()
	b.nfa.Start = s
	// 最终的 e 才是 NFA 的真正接受态
	b.nfa.Accepts = append(b.nfa.Accepts, e)
	// 接受态标签：如果是 Union/Star 等新建的 e，AcceptTags[e] 还是空，补上
	if b.nfa.AcceptTags[e] == "" {
		b.nfa.AcceptTags[e] = tokenKey
	}
	return s, e
}

// pushChildren 把节点的子节点入栈（右先左后，让左子先 pop 完）。
func (b *Builder) pushChildren(r regex.Regex) {
	if right := getRight(r); right != nil {
		b.travStack = append(b.travStack, travEntry{node: right})
	}
	if left := getLeft(r); left != nil {
		b.travStack = append(b.travStack, travEntry{node: left})
	}
	if inner := getInner(r); inner != nil {
		b.travStack = append(b.travStack, travEntry{node: inner})
	}
}

// 辅助：访问 AST 节点的 Left/Right/Inner 字段。
func getLeft(r regex.Regex) regex.Regex {
	switch a := r.(type) {
	case *regex.Concat:
		return a.Left
	case *regex.Union:
		return a.Left
	}
	return nil
}

func getRight(r regex.Regex) regex.Regex {
	switch a := r.(type) {
	case *regex.Concat:
		return a.Right
	case *regex.Union:
		return a.Right
	}
	return nil
}

func getInner(r regex.Regex) regex.Regex {
	switch a := r.(type) {
	case *regex.Star:
		return a.Inner
	case *regex.Plus:
		return a.Inner
	case *regex.Optional:
		return a.Inner
	case *regex.Repeat:
		return a.Inner
	case *regex.Group:
		return a.Inner
	}
	return nil
}
