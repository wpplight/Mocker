package dfa

import (
	"fmt"
	"sort"
	"strings"
)

// TokenStartSet 表示"DFA 状态下，某个 token 的 NFA 状态子集"。
type TokenStartSet struct {
	TokenIdx int   // 哪个 token（按 c.TokenNFAs 索引）
	States   []int // ε-closure 后的状态列表（已排序）
}

// dfaState 是已排序+去重的 TokenStartSet 列表，作为 DFA 状态。
// 按 TokenStartSet.TokenIdx 升序排列，便于等价比较。
type dfaState []TokenStartSet

// canonicalize 返回 dfaState 的标准化形式（按 TokenIdx 升序，内部 States 也排序）。
// 排序保证等价的状态集有完全相同的表示，从而 equals/hash 才能正确去重。
func canonicalize(sets []TokenStartSet) dfaState {
	out := make(dfaState, len(sets))
	copy(out, sets)
	sort.Slice(out, func(i, j int) bool {
		return out[i].TokenIdx < out[j].TokenIdx
	})
	for i := range out {
		sort.Ints(out[i].States)
	}
	return out
}

// hash 返回 dfaState 的字符串表示（用于去重 / 哈希）。
// 保证：等价的状态集有相同的 hash。
func (s dfaState) hash() string {
	parts := make([]string, len(s))
	for i, ts := range s {
		// 内部 States 也要排序（通常已是排序的，但保险）
		inner := append([]int{}, ts.States...)
		sort.Ints(inner)
		parts[i] = fmt.Sprintf("%d:%v", ts.TokenIdx, inner)
	}
	return strings.Join(parts, "|")
}

// equals 判断两个 dfaState 是否等价。
func (s dfaState) equals(o dfaState) bool {
	if len(s) != len(o) {
		return false
	}
	for i := range s {
		if s[i].TokenIdx != o[i].TokenIdx {
			return false
		}
		if !equalIntSlice(s[i].States, o[i].States) {
			return false
		}
	}
	return true
}

func equalIntSlice(a, b []int) bool {
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
