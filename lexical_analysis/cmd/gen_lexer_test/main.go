// 测试生成 lexer 正确性：先调 glex 生成代码，再 import 跑分词。
package main

import (
	"fmt"
	"lexical_analysis/out/mylexer"
	"os"
)

func main() {
	tests := []string{
		"if x = 10;",
		"while(x<100){x=x+1;}",
		"123.456 + 789",
		"ifoo intvar x123_y",
		"== <= >=",
	}

	fail := 0
	for _, src := range tests {
		fmt.Printf("--- input: %q\n", src)
		toks, err := mylexer.Tokenize(src)
		if err != nil {
			fmt.Printf("  ERROR: %v\n", err)
			fail++
			continue
		}
		for _, t := range toks {
			fmt.Printf("  %s\n", t)
		}
	}
	if fail > 0 {
		os.Exit(1)
	}
}
