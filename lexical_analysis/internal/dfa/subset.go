package dfa

// ToDFA 是路径 D 的子集构造主入口。
// 1. 预算每 token 的 ε-closure(start) 作为 DFA 起点
// 2. BFS 探索所有可达的 dfaState
// 3. 对每个 dfaState × char，跑 move() 得到下一个 dfaState
// 4. 去重 + 标准化
// 5. 处理接受态（先定义优先）
func (c *CombinedNFA) ToDFA() *DFA {
	dfa := NewDFA()

	// 1. 起点
	startSets := c.computeStartSet()
	s0 := dfa.addCanonicalState(startSets)
	dfa.Start = s0
	if tag, ok := c.determineAccept(dfa.GetStateSet(s0)); ok {
		dfa.SetAccept(s0, tag)
	}

	// 2. BFS 探索
	worklist := []int{s0}
	visited := map[string]int{dfa.GetStateSet(s0).hash(): s0}

	for len(worklist) > 0 {
		s := worklist[0]
		worklist = worklist[1:]

		// 3. 遍历所有字符（ASCII 子集 0..255）
		for ch := 0; ch < 256; ch++ {
			nextSets := c.move(dfa.GetStateSet(s), byte(ch))
			if len(nextSets) == 0 {
				continue
			}

			// 4. 去重 + 加入 DFA
			h := nextSets.hash()
			sNext, exists := visited[h]
			if !exists {
				sNext = dfa.addCanonicalState(nextSets)
				visited[h] = sNext
				if tag, ok := c.determineAccept(dfa.GetStateSet(sNext)); ok {
					dfa.SetAccept(sNext, tag)
				}
				worklist = append(worklist, sNext)
			}

			// 5. 添加转移
			dfa.AddTransition(s, byte(ch), sNext)
		}
	}

	return dfa
}
