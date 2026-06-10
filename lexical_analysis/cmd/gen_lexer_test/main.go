// 测试生成 lexer 正确性：先调 glex 生成代码，再 import 跑分词。
package main

import (
	"fmt"
	"lexical_analysis/out/mylexer"
	"os"
)

func main() {
	path := "examples/main.mocker"
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		os.Exit(1)
	}

	toks, err := mylexer.Tokenize(string(src))
	if err != nil {
		fmt.Printf("  ERROR: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("  %s\n", toks)

}
