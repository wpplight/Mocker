package dfa

import "lexical_analysis/internal/nfa"

// CombinedNFA 持多个 token NFA 引用（不复制数据），用于子集构造。
type CombinedNFA struct {
	TokenNFAs []*nfa.NFA     // 所有 token 的 NFA（按 DSL 顺序）
	TokenKeys []string       // TokenNFAs[i] 对应的 token key
	Order     map[string]int // tokenKey → Order 索引（先定义优先）
}

// NewCombinedNFA 构造一个 CombinedNFA。
// nfas 和 keys 必须等长，keys[i] 是 nfas[i] 的 token 名。
// 接受态冲突时，Order 小的（先定义的）token 胜出。
func NewCombinedNFA(nfas []*nfa.NFA, keys []string) *CombinedNFA {
	order := make(map[string]int, len(keys))
	for i, k := range keys {
		order[k] = i
	}
	return &CombinedNFA{
		TokenNFAs: nfas,
		TokenKeys: keys,
		Order:     order,
	}
}

// computeStartSet 算每 token 的 start ε-closure，组成"DFA 起点集合"。
// 这就是路径 D 的"预算"步骤——但后续 move 还要算 ε-closure。
func (c *CombinedNFA) computeStartSet() dfaState {
	sets := make(dfaState, len(c.TokenNFAs))
	for i, nfa := range c.TokenNFAs {
		sets[i] = TokenStartSet{
			TokenIdx: i,
			States:   epsilonClosure(nfa, nfa.Start),
		}
	}
	return canonicalize(sets)
}

// move 从 dfaState s 读字符 ch，返回新的 dfaState。
// 每个 token 独立 move + ε-closure，结果拼接。
func (c *CombinedNFA) move(s dfaState, ch byte) dfaState {
	var result dfaState
	for _, ts := range s {
		nfa := c.TokenNFAs[ts.TokenIdx]

		// 1. 从 ts.States 读 ch
		var moved []int
		for _, st := range ts.States {
			if tos, ok := nfa.Trans[st][ch]; ok {
				moved = append(moved, tos...)
			}
		}
		if len(moved) == 0 {
			continue
		}

		// 2. move 后的状态需再算 ε-closure（move 可能引入新 ε 边）
		//    注意：这只在 moved 这个小集合上算，不是全图
		closure := epsilonClosureSet(nfa, moved)

		result = append(result, TokenStartSet{
			TokenIdx: ts.TokenIdx,
			States:   closure,
		})
	}
	return canonicalize(result)
}

// determineAccept 解决接受态冲突：先定义优先。
// 返回 (tokenKey, true) 如果 s 包含接受态。
// 由于 BFS 按 token 顺序处理，先进入的 token（Order 小）会被先加入 dfa.Accepts，
// 所以这个函数只在 SetAccept 之前调用。
func (c *CombinedNFA) determineAccept(s dfaState) (string, bool) {
	for _, ts := range s {
		nfa := c.TokenNFAs[ts.TokenIdx]
		for _, st := range ts.States {
			if _, ok := nfa.AcceptTags[st]; ok {
				return c.TokenKeys[ts.TokenIdx], true
			}
		}
	}
	return "", false
}
