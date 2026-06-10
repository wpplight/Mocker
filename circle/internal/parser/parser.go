// Package parser 实现 Mocker DSL 的语法分析器：Token 流 → AST。
//
// 用法：
//
//	file, errs := parser.Parse(src)
//	if len(errs) > 0 { ... }
package parser

import (
	"fmt"

	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// Parser 递归下降语法分析器
type Parser struct {
	tokens           []tok // 带 Pos 的 token
	pos              int   // 当前位置
	errors           []ParseError
	syntheticCounter int    // 语法糖生成的合成变量计数器（msg+nl 之类）
	currentPkg       string // 当前包名（用于拓扑块识别：块名 == 包名）
}

// nextSynthetic 生成下一个合成变量名
// 用法：msg+nl >> 自动展开为
//
//	__ce_concat_0 := msg+nl
//	__ce_concat_0 >>
func (p *Parser) nextSynthetic(prefix string) string {
	n := p.syntheticCounter
	p.syntheticCounter++
	return fmt.Sprintf("__ce_%s_%d", prefix, n)
}

// tok 是带 Pos 的 token 包装（lexer.Token 没有 Pos，我们重新计数）
type tok struct {
	Type  mocker_lex.Type
	Value string
	Pos   ast.Pos
}

// ParseError 解析错误（聚合，不中断）
type ParseError struct {
	Pos ast.Pos
	Msg string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("parse error at %s: %s", e.Pos, e.Msg)
}

// Parse 公开入口：把 .mocker 源码解析成 AST
func Parse(src []byte) (*ast.File, []ParseError) {
	rawTokens, err := mocker_lex.Tokenize(string(src))
	if err != nil {
		return nil, []ParseError{{Pos: ast.Pos{}, Msg: err.Error()}}
	}

	// 直接用 lexer 给的 Pos（行/列/字节偏移都是真实值）
	// 之前用 token 序号当 Line 是错的，会让 fan-out 的"同行 vs 跨行"判断失效
	tokens := make([]tok, len(rawTokens))
	for i, t := range rawTokens {
		tokens[i] = tok{
			Type:  t.Type,
			Value: t.Value,
			Pos:   ast.Pos{Line: t.Pos.Line, Col: t.Pos.Col, Offset: t.Pos.Offset},
		}
	}

	p := &Parser{tokens: tokens}
	file := p.parseFile()
	return file, p.errors
}

// ──── 辅助函数 ────

func (p *Parser) peek() tok {
	if p.pos >= len(p.tokens) {
		return tok{Type: mocker_lex.TypeEOF, Pos: ast.Pos{}}
	}
	return p.tokens[p.pos]
}

func (p *Parser) peekN(n int) tok {
	idx := p.pos + n
	if idx >= len(p.tokens) {
		return tok{Type: mocker_lex.TypeEOF, Pos: ast.Pos{}}
	}
	return p.tokens[idx]
}

// isAssignOp 返回 true 表示当前 token 是"声明赋值"操作符
//
//	Mocker 里 := 和 = 都作变量声明（区别只是风格）：
//	  :=  → 显式声明意图
//	  =   → 沿用 C 风格（"初始化"语义）
//	两者在 parser 层完全等价，semantic 阶段可以再细分。
func isAssignOp(t mocker_lex.Type) bool {
	return t == mocker_lex.TypeOP_DEFINE || t == mocker_lex.TypeOP_ASSIGN
}

// matchAssign 1-token lookahead：判断下一 token 是不是声明赋值操作符
//
//	:=  或  =
//
// 用法：用在 `IDENT <ASSIGN> EXPR` 这种需要 lookahead 1 的场景
//
//	e.g. parseStructMember 里看 "IDENT 后面" 是不是 := / =
//	     parseStmtDispatch 里看 IDENT 后面 是不是 := / =
func (p *Parser) matchAssign() bool {
	return isAssignOp(p.peekN(1).Type)
}

// consumeAssign 消费当前 token（必须是 := 或 =）
// 调用时机：IDENT 已经被消费后，下一 token 就是 ASSIGN op
func (p *Parser) consumeAssign() tok {
	if !isAssignOp(p.peek().Type) {
		t := p.peek()
		p.errorf("expected ':=' or '=', got %s", t.Type)
		return t
	}
	return p.consume(p.peek().Type)
}

func (p *Parser) consume(kind mocker_lex.Type) tok {
	t := p.peek()
	if t.Type != kind {
		p.errorAt(t.Pos, "expected %s, got %s (value=%q)", kind, t.Type, t.Value)
		return t
	}
	if t.Type != mocker_lex.TypeEOF {
		p.pos++
	}
	return t
}

// match 不消费，只判断
func (p *Parser) match(kinds ...mocker_lex.Type) bool {
	cur := p.peek().Type
	for _, k := range kinds {
		if cur == k {
			return true
		}
	}
	return false
}

// expectAt 在某位置报错
func (p *Parser) errorAt(pos ast.Pos, format string, args ...any) {
	p.errors = append(p.errors, ParseError{
		Pos: pos,
		Msg: fmt.Sprintf(format, args...),
	})
}

// errorf 当前 token 位置报错
func (p *Parser) errorf(format string, args ...any) {
	p.errorAt(p.peek().Pos, format, args...)
}

// atPos 把 ast.Pos 构造器集中
func atPos(p ast.Pos) ast.Pos { return p }
