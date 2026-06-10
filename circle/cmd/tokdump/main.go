package main

import (
"fmt"
"os"
"circle/mocker_lex"
)

func main() {
src, _ := os.ReadFile(os.Args[1])
toks, err := mocker_lex.Tokenize(string(src))
if err != nil {
fmt.Println("lex err:", err)
return
}
for i, t := range toks {
fmt.Printf("%d: %s %q\n", i, t.Type, t.Value)
}
}
