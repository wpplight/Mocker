// Package dfa 提供 NFA → DFA 合并与子集构造算法。
//
// 路径 D：每 token 算一次 ε-closure(start) 作为 DFA 起点，
// 后续 move 操作里 ε-closure 仍在每 token 自己的小集合上算（不是全图）。
package dfa

import "lexical_analysis/internal/nfa"

// epsilonClosure 算单个 NFA 状态的 ε-closure（BFS 走 ε 边）。
// 返回所有能通过 ε 边（任意步数）到达的状态，已排序。
func epsilonClosure(n *nfa.NFA, start int) []int {
	visited := map[int]bool{start: true}
	queue := []int{start}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, to := range n.Epsilon[s] {
			if !visited[to] {
				visited[to] = true
				queue = append(queue, to)
			}
		}
	}
	return sortedKeys(visited)
}

// epsilonClosureSet 算一组 NFA 状态的 ε-closure。
// 用于 move 操作后的状态集。
func epsilonClosureSet(n *nfa.NFA, starts []int) []int {
	visited := make(map[int]bool)
	queue := []int{}
	for _, s := range starts {
		if !visited[s] {
			visited[s] = true
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, to := range n.Epsilon[s] {
			if !visited[to] {
				visited[to] = true
				queue = append(queue, to)
			}
		}
	}
	return sortedKeys(visited)
}

// sortedKeys 返回 map 的 key 列表（已排序）。
func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// 简单插入排序（小集合足够快）
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
