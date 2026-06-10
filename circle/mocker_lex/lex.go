// Package mocker_lex 兼容 shim：旧 glex 接口已经合并到 lexer.go。
//
// 保留这个文件是为了让旧 import 不报错。新代码应直接用 lexer.go 里的定义。
package mocker_lex

// EOFToken 返回一个 EOF token（兼容旧 API）
func EOFToken() Token {
	return Token{Type: TypeEOF, Kind: KindEOF, Value: ""}
}
