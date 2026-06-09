package dfa

import (
	"slices"
	"strconv"
	"strings"
)

// Minimize 用迭代划分精化算法对 DFA 做最小化。
//
// 算法（Moore 变体）：
//  1. 初始划分：按 (accept_tag, is_accept) 切分
//  2. 迭代：对每个 group，检查是否能被"对某字符到达不同 group"分裂
//  3. 直到稳定
//  4. 重建 DFA：每个等价类 = 1 个新状态
//
// 两个状态等价 ⟺ (a) accept 标签相同（或都非接受）且
//
//	(b) 对每个字符，转移到等价目标。
//
// 返回新 DFA；不修改原 DFA。
func (d *DFA) Minimize() *DFA {
	if d.NumStates == 0 {
		return d
	}

	// ── 1. 初始划分：按 accept tag 切分 ──
	// groupOf[state] = groupId
	groupOf := make([]int, d.NumStates)
	// tagOf[group] = 该 group 的 accept tag（"" = 非接受）
	tagOf := make(map[int]string)
	// groupCount[groupId] = 该 group 状态数（用于输出统计）
	groupCount := make(map[int]int)

	nextG := 0
	// 先把非接受态全放 group 0
	for s := 0; s < d.NumStates; s++ {
		if _, isAcc := d.Accepts[s]; !isAcc {
			groupOf[s] = nextG
			groupCount[nextG]++
		}
	}
	if groupCount[nextG] > 0 {
		tagOf[nextG] = "" // 非接受
		nextG++
	}
	// 接受态按 tag 分
	for s, tag := range d.Accepts {
		// 找 tag 对应的 group
		var found int = -1
		for gid, t := range tagOf {
			if t == tag {
				found = gid
				break
			}
		}
		if found < 0 {
			found = nextG
			tagOf[found] = tag
			groupCount[found] = 0
			nextG++
		}
		groupOf[s] = found
		groupCount[found]++
	}

	// ── 2. 迭代精化 ──
	for {
		newGroup := make([]int, d.NumStates)
		// 收集 (signature) → newGroupId
		sigToId := make(map[string]int)
		nextId := 0

		for s := 0; s < d.NumStates; s++ {
			sig := stateSignature(d, s, groupOf)
			id, ok := sigToId[sig]
			if !ok {
				id = nextId
				sigToId[sig] = id
				nextId++
			}
			newGroup[s] = id
		}

		// 检查是否稳定
		stable := true
		for s := 0; s < d.NumStates; s++ {
			if groupOf[s] != newGroup[s] {
				stable = false
				break
			}
		}
		if stable {
			break
		}
		groupOf = newGroup
	}

	// ── 3. 重建 DFA：每个等价类 = 1 个新状态 ──
	groupIDToNewState := make(map[int]int)
	for s := 0; s < d.NumStates; s++ {
		g := groupOf[s]
		if _, ok := groupIDToNewState[g]; !ok {
			groupIDToNewState[g] = len(groupIDToNewState)
		}
	}

	newDFA := &DFA{
		Start:   -1,
		Accepts: make(map[int]string),
	}
	// 预分配 States 和 Trans（AddTransition 依赖 States 数组长度）
	numNew := len(groupIDToNewState)
	newDFA.States = make([]dfaState, numNew)
	newDFA.Trans = make([]map[byte][]int, numNew)
	for i := range newDFA.Trans {
		newDFA.Trans[i] = make(map[byte][]int)
	}
	newDFA.NumStates = numNew

	// 复制转移
	for oldState := 0; oldState < d.NumStates; oldState++ {
		newFrom := groupIDToNewState[groupOf[oldState]]
		trans := d.Trans[oldState]
		// 收集字符并排序
		var chars []byte
		for ch := range trans {
			chars = append(chars, ch)
		}
		slices.Sort(chars)
		for _, ch := range chars {
			tos := trans[ch]
			if len(tos) == 0 {
				continue
			}
			// DFA 应确定性：len(tos) == 1
			newTo := groupIDToNewState[groupOf[tos[0]]]
			if newTo >= len(newDFA.Trans) || newFrom >= len(newDFA.Trans) {
				continue // 防御
			}
			newDFA.Trans[newFrom][ch] = []int{newTo}
		}
	}
	// 复制接受态
	for oldState, tag := range d.Accepts {
		newState := groupIDToNewState[groupOf[oldState]]
		newDFA.Accepts[newState] = tag
	}
	// 起点
	newDFA.Start = groupIDToNewState[groupOf[d.Start]]

	return newDFA
}

// stateSignature 算状态的签名：
//
//	"<tag>|<c1>=<g1>,<c2>=<g2>,..."
//
// 两个状态等价 ⟺ 签名相同（同 tag + 同 group 转移）。
func stateSignature(d *DFA, state int, groupOf []int) string {
	tag, isAcc := d.Accepts[state]
	if !isAcc {
		tag = "·" // 非接受用中点
	}
	trans := d.Trans[state]
	// 收集字符并排序
	var chars []byte
	for ch := range trans {
		chars = append(chars, ch)
	}
	slices.Sort(chars)
	parts := make([]string, 0, len(chars)+1)
	parts = append(parts, tag)
	for _, ch := range chars {
		tos := trans[ch]
		if len(tos) == 0 {
			continue
		}
		parts = append(parts, string(rune(ch))+"="+strconv.Itoa(groupOf[tos[0]]))
	}
	return strings.Join(parts, "|")
}
