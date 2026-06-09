package main

import (
	"fmt"

	"lexical_analysis/internal/dfa"
	"lexical_analysis/internal/dsl"
	"lexical_analysis/internal/nfa"
)

func main() {
	sf, err := dsl.ReadFile("/home/wpp/homework/complit-reason/lexical_analysis/examples/tokens.glex")
	if err != nil {
		panic(err)
	}
	keys := make([]string, 0, len(sf.Tokens))
	nfas := make([]*nfa.NFA, 0, len(sf.Tokens))
	for _, t1 := range sf.Tokens {
		keys = append(keys, t1.Key)
		nfas = append(nfas, t1.NFA)
	}
	combined := dfa.NewCombinedNFA(nfas, keys)
	raw := combined.ToDFA()
	fmt.Printf("Raw DFA: %d states, %d accepts\n\n", raw.GetNumStates(), len(raw.GetAccepts()))

	// 列出所有 accept 状态和它们的目标转移
	fmt.Println("=== Raw accepts and their outgoing chars ===")
	for state, tag := range raw.GetAccepts() {
		trans := raw.GetTrans()[state]
		var chars []byte
		for ch := range trans {
			chars = append(chars, ch)
		}
		// 简单描述目标
		destSummary := ""
		if len(chars) > 0 {
			destSummary = fmt.Sprintf("→ state %d (via %d chars)", trans[chars[0]][0], len(chars))
		} else {
			destSummary = "(no transitions)"
		}
		fmt.Printf("  s%d  %-12s %s\n", state, tag, destSummary)
	}

	min := raw.Minimize()
	fmt.Printf("\nMinimized DFA: %d states, %d accepts\n", min.GetNumStates(), len(min.GetAccepts()))

	// 调试：检查 DFA 是否真的是确定性的
	fmt.Println("\n=== Checking DFA determinism ===")
	for state := 0; state < raw.GetNumStates(); state++ {
		trans := raw.GetTrans()[state]
		for ch, tos := range trans {
			if len(tos) > 1 {
				fmt.Printf("  s%d char=%q → %v (NON-DETERMINISTIC!)\n", state, ch, tos)
			}
		}
	}
}
