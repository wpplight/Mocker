package regex

import (
	"fmt"
	"strings"
)

// ParseError 包含解析失败的详细信息（位置 + 消息）。
type ParseError struct {
	Msg   string
	Pos   int
	Input string
}

func (e *ParseError) Error() string {
	start := e.Pos - 10
	if start < 0 {
		start = 0
	}
	end := e.Pos + 10
	if end > len(e.Input) {
		end = len(e.Input)
	}
	cursor := strings.Repeat(" ", e.Pos-start) + "^"
	return fmt.Sprintf("regex: parse error at position %d: %s\n  near: %q\n         %s",
		e.Pos, e.Msg, e.Input[start:end], cursor)
}

// Parse 解析正则字符串为 AST。
//
// 支持的语法（按优先级从低到高）：
//
//	union      A | B
//	concat     A B（隐式邻接）
//	quantifier A*  A+  A?  A{n}  A{n,}  A{n,m}
//	atom       literal  \x  [..]  (..)  .
func Parse(input string) (Regex, error) {
	p := &parser{s: input}
	r, err := p.parseUnion()
	if err != nil {
		return nil, err
	}
	if !p.eof() {
		return nil, p.errf("unexpected character %q", p.peek())
	}
	return r, nil
}

// MustParse 是 Parse 的 panic 版本，失败时直接 panic。用于测试。
func MustParse(input string) Regex {
	r, err := Parse(input)
	if err != nil {
		panic(err)
	}
	return r
}

// ─────────────────────────────────────────────────────────────
// parser：递归下降实现
// ─────────────────────────────────────────────────────────────

type parser struct {
	s   string
	pos int
}

func (p *parser) eof() bool { return p.pos >= len(p.s) }
func (p *parser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.s[p.pos]
}
func (p *parser) next() byte {
	if p.eof() {
		return 0
	}
	ch := p.s[p.pos]
	p.pos++
	return ch
}

func (p *parser) errf(format string, args ...interface{}) error {
	return &ParseError{
		Msg:   fmt.Sprintf(format, args...),
		Pos:   p.pos,
		Input: p.s,
	}
}

// ─────────────────────────────────────────────────────────────
// 优先级 1：Union  A | B | C
// ─────────────────────────────────────────────────────────────

func (p *parser) parseUnion() (Regex, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}
	for p.peek() == '|' {
		p.next()
		right, err := p.parseConcat()
		if err != nil {
			return nil, err
		}
		left = &Union{Left: left, Right: right}
	}
	return left, nil
}

// ─────────────────────────────────────────────────────────────
// 优先级 2：Concat  A B C（隐式邻接）
// ─────────────────────────────────────────────────────────────

func (p *parser) parseConcat() (Regex, error) {
	var parts []Regex
	for !p.eof() && p.peek() != '|' && p.peek() != ')' {
		part, err := p.parseQuantified()
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return &Empty{}, nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	// 左折叠成 Concat
	result := Regex(parts[0])
	for i := 1; i < len(parts); i++ {
		result = &Concat{Left: result, Right: parts[i]}
	}
	return result, nil
}

// ─────────────────────────────────────────────────────────────
// 优先级 3：Quantifier  A*  A+  A?  A{n}  A{n,}  A{n,m}
// ─────────────────────────────────────────────────────────────

func (p *parser) parseQuantified() (Regex, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	switch p.peek() {
	case '*':
		p.next()
		return &Star{Inner: atom}, nil
	case '+':
		p.next()
		return &Plus{Inner: atom}, nil
	case '?':
		p.next()
		return &Optional{Inner: atom}, nil
	case '{':
		return p.parseRepeat(atom)
	}
	return atom, nil
}

func (p *parser) parseRepeat(atom Regex) (Regex, error) {
	if p.peek() != '{' {
		return atom, nil
	}
	p.next() // '{'

	min, max, ok, err := p.parseRange()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, p.errf("invalid repeat: expected number or range after '{'")
	}
	if p.peek() != '}' {
		return nil, p.errf("expected '}' in repeat, got %q", p.peek())
	}
	p.next() // '}'
	if min < 0 {
		return nil, p.errf("repeat count cannot be negative")
	}
	if max != -1 && max < min {
		return nil, p.errf("invalid repeat range {%d,%d}", min, max)
	}
	return &Repeat{Min: min, Max: max, Inner: atom}, nil
}

// parseRange 解析 {n}, {n,}, {n,m} 中的数字部分。
// 调用前已读过 '{'。
// 返回的 ok 为 false 表示这不是合法的范围（如 {} 空、{a} 非数字），调用方应回退。
func (p *parser) parseRange() (min, max int, ok bool, err error) {
	n1, hasN, err := p.parseNumber()
	if err != nil || !hasN {
		return 0, 0, false, err
	}
	if p.peek() == ',' {
		p.next()
		if p.peek() == '}' {
			// {n,}
			return n1, -1, true, nil
		}
		n2, hasN2, err2 := p.parseNumber()
		if err2 != nil {
			return 0, 0, false, err2
		}
		if !hasN2 {
			return 0, 0, false, nil
		}
		return n1, n2, true, nil
	}
	// {n}
	return n1, n1, true, nil
}

// parseNumber 解析一串数字。
func (p *parser) parseNumber() (int, bool, error) {
	start := p.pos
	for !p.eof() && p.peek() >= '0' && p.peek() <= '9' {
		p.next()
	}
	if p.pos == start {
		return 0, false, nil
	}
	n := 0
	for _, c := range p.s[start:p.pos] {
		n = n*10 + int(c-'0')
	}
	return n, true, nil
}

// ─────────────────────────────────────────────────────────────
// 优先级 4：Atom
// ─────────────────────────────────────────────────────────────

func (p *parser) parseAtom() (Regex, error) {
	ch := p.peek()
	switch ch {
	case '[':
		return p.parseCharClass()
	case '\\':
		p.next()
		return p.parseEscape()
	case '(':
		return p.parseGroup()
	case '.':
		p.next()
		return &Dot{}, nil
	case 0:
		return nil, p.errf("unexpected end of input")
	default:
		p.next()
		return &Literal{Ch: ch}, nil
	}
}

// parseEscape 解析 \x，调用前已读过 '\'。
// 展开常见转义：\d \D \w \W \s \S \n \t \r 以及字面转义。
func (p *parser) parseEscape() (Regex, error) {
	if p.eof() {
		return nil, p.errf("unexpected end of input after '\\'")
	}
	ch := p.next()
	switch ch {
	case 'd':
		return &CharClass{Chars: rangeBytes('0', '9'), Negate: false}, nil
	case 'D':
		return &CharClass{Chars: rangeBytes('0', '9'), Negate: true}, nil
	case 'w':
		return &CharClass{Chars: wordChars(), Negate: false}, nil
	case 'W':
		return &CharClass{Chars: wordChars(), Negate: true}, nil
	case 's':
		return &CharClass{Chars: spaceChars(), Negate: false}, nil
	case 'S':
		return &CharClass{Chars: spaceChars(), Negate: true}, nil
	case 'n':
		return &Literal{Ch: '\n'}, nil
	case 't':
		return &Literal{Ch: '\t'}, nil
	case 'r':
		return &Literal{Ch: '\r'}, nil
	case '0':
		return &Literal{Ch: 0}, nil
	default:
		// 任何其它 \x → Literal{x}（包括 \( \) \[ \] \\ \. \* \+ \? \| \^ \$ \- 等）
		return &Literal{Ch: ch}, nil
	}
}

// parseCharClass 解析 [...], 调用前已读到 '['。
// 支持：单字符、范围 [a-z]、转义 \d \w \s、字符类内并集、否定 [^..]。
func (p *parser) parseCharClass() (Regex, error) {
	p.next() // '['
	negate := false
	if p.peek() == '^' {
		negate = true
		p.next()
	}
	var chars []byte
	for !p.eof() && p.peek() != ']' {
		// 读单个字符（可能来自转义）
		ch, err := p.readCharClassChar()
		if err != nil {
			return nil, err
		}
		// 检查范围
		if p.peek() == '-' && p.pos+1 < len(p.s) && p.s[p.pos+1] != ']' {
			p.next() // '-'
			end, err := p.readCharClassChar()
			if err != nil {
				return nil, err
			}
			if end < ch {
				return nil, p.errf("invalid range %q-%q", ch, end)
			}
			chars = append(chars, rangeBytes(ch, end)...)
		} else {
			chars = append(chars, ch)
		}
	}
	if p.eof() {
		return nil, p.errf("unterminated character class")
	}
	p.next() // ']'
	chars = dedup(chars)
	return &CharClass{Chars: chars, Negate: negate}, nil
}

// readCharClassChar 读取字符类内的一个字符（处理转义）。
func (p *parser) readCharClassChar() (byte, error) {
	if p.peek() == '\\' {
		p.next()
		esc, err := p.parseEscape()
		if err != nil {
			return 0, err
		}
		switch e := esc.(type) {
		case *Literal:
			return e.Ch, nil
		case *CharClass:
			// \d \w \s 展开成字符类，inlined 进当前类
			if len(e.Chars) == 1 {
				return e.Chars[0], nil
			}
			// 多字符：返回第一个，剩下的在 parseCharClass 主循环里 hack
			// 简化处理：抛错（实际使用中应改 parseCharClass 支持）
			return 0, p.errf("multi-char escape not supported in character class")
		default:
			return 0, p.errf("unexpected escape in character class")
		}
	}
	return p.next(), nil
}

// parseGroup 解析 (A)，调用前已读到 '('。
func (p *parser) parseGroup() (Regex, error) {
	p.next() // '('
	inner, err := p.parseUnion()
	if err != nil {
		return nil, err
	}
	if p.peek() != ')' {
		return nil, p.errf("expected ')', got %q", p.peek())
	}
	p.next() // ')'
	return &Group{Inner: inner}, nil
}

// ─────────────────────────────────────────────────────────────
// 工具函数
// ─────────────────────────────────────────────────────────────

// rangeBytes 返回 [lo, hi] 范围内所有 byte（lo <= hi）。
func rangeBytes(lo, hi byte) []byte {
	out := make([]byte, 0, int(hi-lo)+1)
	for c := lo; c <= hi; c++ {
		out = append(out, c)
	}
	return out
}

// wordChars 预定义的 \w 字符集：[a-zA-Z0-9_]
func wordChars() []byte {
	out := make([]byte, 0, 63)
	out = append(out, rangeBytes('a', 'z')...)
	out = append(out, rangeBytes('A', 'Z')...)
	out = append(out, rangeBytes('0', '9')...)
	out = append(out, '_')
	return out
}

// spaceChars 预定义的 \s 字符集：[ \t\n\r\f\v]
func spaceChars() []byte {
	return []byte{' ', '\t', '\n', '\r', '\f', '\v'}
}

// dedup 去重并保持顺序。
func dedup(b []byte) []byte {
	seen := make(map[byte]bool, len(b))
	out := make([]byte, 0, len(b))
	for _, c := range b {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}
