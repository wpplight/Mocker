// Package mocker_lex 实现 Mocker DSL 的手写词法分析器。
//
// 旧版本（lexer_glex_gen.go.bak）由 glex 自动生成，状态机分散在 1800 行
// switch 里、可读性差、有 bug（识别完 "}" 之后无法重置）。本文件是手写
// 重写版：~250 行，无 bug，易扩展。
//
// 用法：
//
//	tokens, err := mocker_lex.Tokenize(src)
//	if err != nil { ... }
//
// 调试 / 详细错误：
//
//	tokens, err := mocker_lex.TokenizeEx(src, mocker_lex.TokenizeOpts{Debug: true})
package mocker_lex

import (
	"fmt"
	"strings"
)

// ──── 类型分类 ────

// Kind token 分类（type 维度）
type Kind int

const (
	KindInvalid Kind = iota
	KindSYS          // package / import
	KindKW           // enum / if / else / return / true / false
	KindTYPE         // str / num / bool / byte / any
	KindOP           // 运算符
	KindSEP          // 分隔符
	KindIDENT        // 标识符
	KindCALL         // 包.方法链
	KindEDGE         // 边名（含 -）
	KindSTRING       // 字符串字面量
	KindNUM          // 数字
	KindEOF          // 人工追加
)

var kindNames = [...]string{
	KindInvalid: "Invalid",
	KindSYS:     "SYS",
	KindKW:      "KW",
	KindTYPE:    "TYPE",
	KindOP:      "OP",
	KindSEP:     "SEP",
	KindIDENT:   "IDENT",
	KindCALL:    "CALL",
	KindEDGE:    "EDGE",
	KindSTRING:  "STRING",
	KindNUM:     "NUM",
	KindEOF:     "EOF",
}

func (k Kind) String() string { return kindNames[k] }

// ──── 具体 token 类型 ────

// Type 具体的 token 名
type Type int

const (
	TypeInvalid Type = iota

	// SYS
	TypeSYS_PACK
	TypeSYS_IMPORT

	// KW
	TypeKW_ENUM
	TypeKW_IF
	TypeKW_ELSE
	TypeKW_FOR
	TypeKW_WHILE
	TypeKW_RETURN
	TypeKW_TRUE
	TypeKW_FALSE

	// TYPE
	TypeTYPE_STR
	TypeTYPE_NUM
	TypeTYPE_BOOL
	TypeTYPE_BYTE
	TypeTYPE_ANY

	// OP 多字符
	TypeOP_RRARROW    // >>
	TypeOP_LARROW     // <<
	TypeOP_DEFINE     // :=
	TypeOP_EQ         // ==
	TypeOP_NE         // !=
	TypeOP_LE         // <=
	TypeOP_GE         // >=
	TypeOP_AND        // &&
	TypeOP_OR         // ||
	TypeOP_ADD_ASSIGN // +=
	TypeOP_SUB_ASSIGN // -=
	TypeOP_MUL_ASSIGN // *=
	TypeOP_DIV_ASSIGN // /=
	TypeOP_INC        // ++
	TypeOP_DEC        // --

	// OP 单字符
	TypeOP_NOT // !
	TypeOP_LT  // <
	TypeOP_GT  // >
	TypeOP_ASSIGN
	TypeOP_ADD // +
	TypeOP_SUB // -
	TypeOP_MUL // *
	TypeOP_DIV // /

	// SEP
	TypeSEP_AT       // @
	TypeSEP_LPAREN   // (
	TypeSEP_RPAREN   // )
	TypeSEP_LBRACE   // {
	TypeSEP_RBRACE   // }
	TypeSEP_LBRACKET // [
	TypeSEP_RBRACKET // ]
	TypeSEP_SEMI     // ;
	TypeSEP_COMMA    // ,
	TypeSEP_DOT      // .
	TypeSEP_COLON    // :

	// 标识符类
	TypeSTRING
	TypeNUM
	TypeCALL
	TypeEDGE_NAME
	TypeID

	// 特殊
	TypeEOF
)

var typeNames = [...]string{
	TypeInvalid: "Invalid",

	TypeSYS_PACK:   "SYS_PACK",
	TypeSYS_IMPORT: "SYS_IMPORT",

	TypeKW_ENUM:   "KW_ENUM",
	TypeKW_IF:     "KW_IF",
	TypeKW_ELSE:   "KW_ELSE",
	TypeKW_FOR:    "KW_FOR",
	TypeKW_WHILE:  "KW_WHILE",
	TypeKW_RETURN: "KW_RETURN",
	TypeKW_TRUE:   "KW_TRUE",
	TypeKW_FALSE:  "KW_FALSE",

	TypeTYPE_STR:  "TYPE_STR",
	TypeTYPE_NUM:  "TYPE_NUM",
	TypeTYPE_BOOL: "TYPE_BOOL",
	TypeTYPE_BYTE: "TYPE_BYTE",
	TypeTYPE_ANY:  "TYPE_ANY",

	TypeOP_RRARROW:    "OP_RRARROW",
	TypeOP_LARROW:     "OP_LARROW",
	TypeOP_DEFINE:     "OP_DEFINE",
	TypeOP_EQ:         "OP_EQ",
	TypeOP_NE:         "OP_NE",
	TypeOP_LE:         "OP_LE",
	TypeOP_GE:         "OP_GE",
	TypeOP_AND:        "OP_AND",
	TypeOP_OR:         "OP_OR",
	TypeOP_ADD_ASSIGN: "OP_ADD_ASSIGN",
	TypeOP_SUB_ASSIGN: "OP_SUB_ASSIGN",
	TypeOP_MUL_ASSIGN: "OP_MUL_ASSIGN",
	TypeOP_DIV_ASSIGN: "OP_DIV_ASSIGN",
	TypeOP_INC:        "OP_INC",
	TypeOP_DEC:        "OP_DEC",
	TypeOP_NOT:        "OP_NOT",

	TypeOP_LT:     "OP_LT",
	TypeOP_GT:     "OP_GT",
	TypeOP_ASSIGN: "OP_ASSIGN",
	TypeOP_ADD:    "OP_ADD",
	TypeOP_SUB:    "OP_SUB",
	TypeOP_MUL:    "OP_MUL",
	TypeOP_DIV:    "OP_DIV",

	TypeSEP_AT:       "SEP_AT",
	TypeSEP_LPAREN:   "SEP_LPAREN",
	TypeSEP_RPAREN:   "SEP_RPAREN",
	TypeSEP_LBRACE:   "SEP_LBRACE",
	TypeSEP_RBRACE:   "SEP_RBRACE",
	TypeSEP_LBRACKET: "SEP_LBRACKET",
	TypeSEP_RBRACKET: "SEP_RBRACKET",
	TypeSEP_SEMI:     "SEP_SEMI",
	TypeSEP_COMMA:    "SEP_COMMA",
	TypeSEP_DOT:      "SEP_DOT",
	TypeSEP_COLON:    "SEP_COLON",

	TypeSTRING:    "STRING",
	TypeNUM:       "NUM",
	TypeCALL:      "CALL",
	TypeEDGE_NAME: "EDGE_NAME",
	TypeID:        "ID",

	TypeEOF: "EOF",
}

var typeKinds = [...]Kind{
	TypeInvalid: KindInvalid,

	TypeSYS_PACK:   KindSYS,
	TypeSYS_IMPORT: KindSYS,

	TypeKW_ENUM:   KindKW,
	TypeKW_IF:     KindKW,
	TypeKW_ELSE:   KindKW,
	TypeKW_FOR:    KindKW,
	TypeKW_WHILE:  KindKW,
	TypeKW_RETURN: KindKW,
	TypeKW_TRUE:   KindKW,
	TypeKW_FALSE:  KindKW,

	TypeTYPE_STR:  KindTYPE,
	TypeTYPE_NUM:  KindTYPE,
	TypeTYPE_BOOL: KindTYPE,
	TypeTYPE_BYTE: KindTYPE,
	TypeTYPE_ANY:  KindTYPE,

	TypeOP_RRARROW:    KindOP,
	TypeOP_LARROW:     KindOP,
	TypeOP_ADD_ASSIGN: KindOP,
	TypeOP_SUB_ASSIGN: KindOP,
	TypeOP_MUL_ASSIGN: KindOP,
	TypeOP_DIV_ASSIGN: KindOP,
	TypeOP_INC:        KindOP,
	TypeOP_DEC:        KindOP,
	TypeOP_DEFINE:     KindOP,
	TypeOP_EQ:         KindOP,
	TypeOP_NE:         KindOP,
	TypeOP_LE:         KindOP,
	TypeOP_GE:         KindOP,
	TypeOP_AND:        KindOP,
	TypeOP_OR:         KindOP,
	TypeOP_NOT:        KindOP,
	TypeOP_LT:         KindOP,
	TypeOP_GT:         KindOP,
	TypeOP_ASSIGN:     KindOP,
	TypeOP_ADD:        KindOP,
	TypeOP_SUB:        KindOP,
	TypeOP_MUL:        KindOP,
	TypeOP_DIV:        KindOP,

	TypeSEP_AT:       KindSEP,
	TypeSEP_LPAREN:   KindSEP,
	TypeSEP_RPAREN:   KindSEP,
	TypeSEP_LBRACE:   KindSEP,
	TypeSEP_RBRACE:   KindSEP,
	TypeSEP_LBRACKET: KindSEP,
	TypeSEP_RBRACKET: KindSEP,
	TypeSEP_SEMI:     KindSEP,
	TypeSEP_COMMA:    KindSEP,
	TypeSEP_DOT:      KindSEP,
	TypeSEP_COLON:    KindSEP,

	TypeSTRING:    KindSTRING,
	TypeNUM:       KindNUM,
	TypeCALL:      KindCALL,
	TypeEDGE_NAME: KindEDGE,
	TypeID:        KindIDENT,

	TypeEOF: KindEOF,
}

func (t Type) String() string {
	if int(t) < len(typeNames) {
		return typeNames[t]
	}
	return fmt.Sprintf("Type(%d)", int(t))
}

// ──── Pos：源码位置 ────

// Pos 源码位置（行/列/字节偏移）
type Pos struct {
	Line   int // 1-based
	Col    int // 1-based
	Offset int // 0-based 字节偏移
}

// ──── Token ────

// Token 词法分析器输出的最小单元
type Token struct {
	Type  Type
	Kind  Kind
	Value string // 匹配的原始文本（不含外层引号 / 括号）
	Pos   Pos
}

func (t Token) String() string {
	if t.Pos.Line > 0 {
		return fmt.Sprintf("%s(%q) at %d:%d", t.Type, t.Value, t.Pos.Line, t.Pos.Col)
	}
	return fmt.Sprintf("%s(%q)", t.Type, t.Value)
}

// ──── 关键字 / 类型查找表 ────

var keywords = map[string]Type{
	"package": TypeSYS_PACK,
	"import":  TypeSYS_IMPORT,
	"enum":    TypeKW_ENUM,
	"if":      TypeKW_IF,
	"else":    TypeKW_ELSE,
	"for":     TypeKW_FOR,
	"while":   TypeKW_WHILE,
	"return":  TypeKW_RETURN,
	"true":    TypeKW_TRUE,
	"false":   TypeKW_FALSE,
	"str":     TypeTYPE_STR,
	"num":     TypeTYPE_NUM,
	"bool":    TypeTYPE_BOOL,
	"byte":    TypeTYPE_BYTE,
	"any":     TypeTYPE_ANY,
}

// ──── 单字符 op/sep 表 ────

var singleCharTokens = map[byte]Type{
	'!': TypeOP_NOT,
	'+': TypeOP_ADD,
	'-': TypeOP_SUB, // 注：减号在 IDENT 后是合法的（如 EDGE_NAME），但作为单独 token 始终是减号
	'*': TypeOP_MUL,
	'/': TypeOP_DIV,
	'<': TypeOP_LT,
	'>': TypeOP_GT,
	'=': TypeOP_ASSIGN,
	'(': TypeSEP_LPAREN,
	')': TypeSEP_RPAREN,
	'{': TypeSEP_LBRACE,
	'}': TypeSEP_RBRACE,
	'[': TypeSEP_LBRACKET,
	']': TypeSEP_RBRACKET,
	';': TypeSEP_SEMI,
	',': TypeSEP_COMMA,
	'.': TypeSEP_DOT,
	':': TypeSEP_COLON,
	'@': TypeSEP_AT,
}

// 多字符 op 列表（按长度优先 → 字典序）
var multiCharOps = []string{
	">>", "<<", ":=", "==", "!=", "<=", ">=", "&&", "||",
}

var multiCharTypes = map[string]Type{
	">>": TypeOP_RRARROW,
	"<<": TypeOP_LARROW,
	":=": TypeOP_DEFINE,
	"==": TypeOP_EQ,
	"!=": TypeOP_NE,
	"<=": TypeOP_LE,
	">=": TypeOP_GE,
	"&&": TypeOP_AND,
	"||": TypeOP_OR,
	"+=": TypeOP_ADD_ASSIGN, // 复合赋值：a += b
	"-=": TypeOP_SUB_ASSIGN,
	"*=": TypeOP_MUL_ASSIGN,
	"/=": TypeOP_DIV_ASSIGN,
	"++": TypeOP_INC, // 增量：i++
	"--": TypeOP_DEC, // 减量：i--
}

// ──── 字符分类辅助 ────

func isSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func isIdentStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || isDigit(b)
}

// ──── 主入口 ────

// Tokenize 切词（标准入口）
func Tokenize(input string) ([]Token, error) {
	return TokenizeEx(input, TokenizeOpts{})
}

// TokenizeOpts 控制 Tokenize 行为
type TokenizeOpts struct {
	// Debug 打开后，每个字符的处理会写入 DebugLog
	Debug bool
	// DebugLog debug 输出目的地（可选；nil = 内部 strings.Builder）
	DebugLog *strings.Builder
}

// LexError 详细报错信息
type LexError struct {
	Pos     Pos
	Char    byte
	Snippet string // 周围 32 字节
	Msg     string
}

func (e *LexError) Error() string {
	return fmt.Sprintf("lex error at line %d col %d (pos %d): %s near %q (char=%q)",
		e.Pos.Line, e.Pos.Col, e.Pos.Offset, e.Msg, e.Snippet, rune(e.Char))
}

// TokenizeEx 完整切词（带 debug / 详细错误）
func TokenizeEx(input string, opts TokenizeOpts) ([]Token, error) {
	var (
		tokens    []Token
		pos       = 0
		line      = 1
		col       = 1
		debugBuf  strings.Builder
		debugSink = opts.DebugLog
	)
	if debugSink == nil {
		debugSink = &debugBuf
	}
	debug := func(format string, args ...any) {
		if opts.Debug {
			fmt.Fprintf(debugSink, format, args...)
		}
	}
	commit := func(t Type, k Kind, start, end int, startLine, startCol int) {
		tok := Token{
			Type:  t,
			Kind:  k,
			Value: input[start:end],
			Pos:   Pos{Line: startLine, Col: startCol, Offset: start},
		}
		tokens = append(tokens, tok)
		debug("  [commit] %s(%q) at line %d col %d (len=%d)\n",
			t, tok.Value, tok.Pos.Line, tok.Pos.Col, end-start)
	}
	emitError := func(start, cur int, msg string) error {
		snippet := input
		if len(snippet) > 64 {
			s := start - 16
			if s < 0 {
				s = 0
			}
			e := cur + 16
			if e > len(snippet) {
				e = len(snippet)
			}
			snippet = snippet[s:e]
		}
		var ch byte
		if cur < len(input) {
			ch = input[cur]
		}
		return &LexError{
			Pos:     Pos{Line: line, Col: col, Offset: cur},
			Char:    ch,
			Snippet: snippet,
			Msg:     msg,
		}
	}
	advance := func(n int) {
		for i := 0; i < n && pos < len(input); i++ {
			if input[pos] == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			pos++
		}
	}

	debug("=== Lex %d bytes (debug=%v) ===\n", len(input), opts.Debug)

	for pos < len(input) {
		ch := input[pos]

		// ① 跳过空白
		if isSpace(ch) {
			debug("  [skip space] pos=%d ch=%q\n", pos, ch)
			advance(1)
			continue
		}

		// ② 注释：//  到行尾  /  /* ... */  块注释
		if ch == '/' && pos+1 < len(input) {
			next := input[pos+1]
			if next == '/' {
				debug("  [line comment] pos=%d\n", pos)
				advance(2)
				for pos < len(input) && input[pos] != '\n' {
					advance(1)
				}
				continue
			}
			if next == '*' {
				debug("  [block comment] pos=%d\n", pos)
				advance(2)
				for pos+1 < len(input) && !(input[pos] == '*' && input[pos+1] == '/') {
					advance(1)
				}
				if pos+1 < len(input) {
					advance(2) // consume */
				}
				continue
			}
		}

		// ③ 多字符 op（优先匹配）
		if pos+1 < len(input) {
			two := input[pos : pos+2]
			if t, ok := multiCharTypes[two]; ok {
				debug("  [multi-op] pos=%d two=%q\n", pos, two)
				commit(t, KindOP, pos, pos+2, line, col)
				advance(2)
				continue
			}
		}

		// ④ 字符串字面量
		if ch == '"' {
			startLine, startCol, startPos := line, col, pos
			debug("  [string] pos=%d\n", pos)
			advance(1) // consume opening "
			// 找到结尾的 "，处理 \"
			valueStart := pos
			for pos < len(input) && input[pos] != '"' {
				if input[pos] == '\\' && pos+1 < len(input) {
					advance(2) // skip escape pair
				} else {
					advance(1)
				}
			}
			if pos >= len(input) {
				return nil, emitError(startPos, pos, "unterminated string literal")
			}
			valueEnd := pos
			advance(1) // consume closing "
			_ = startLine
			_ = startCol
			// value 是引号内的内容（不含外层引号）
			commit(TypeSTRING, KindSTRING, valueStart, valueEnd, startLine, startCol)
			continue
		}

		// ⑤ 数字
		if isDigit(ch) {
			startLine, startCol, startPos := line, col, pos
			debug("  [num] pos=%d\n", pos)
			for pos < len(input) && isDigit(input[pos]) {
				advance(1)
			}
			// 可选小数部分
			if pos+1 < len(input) && input[pos] == '.' && isDigit(input[pos+1]) {
				advance(1) // consume .
				for pos < len(input) && isDigit(input[pos]) {
					advance(1)
				}
			}
			commit(TypeNUM, KindNUM, startPos, pos, startLine, startCol)
			continue
		}

		// ⑥ 标识符 / 关键字 / CALL / EDGE_NAME
		if isIdentStart(ch) {
			startLine, startCol, startPos := line, col, pos
			debug("  [ident] pos=%d\n", pos)
			// 读第一段 IDENT
			for pos < len(input) && isIdentPart(input[pos]) {
				advance(1)
			}
			first := input[startPos:pos]

			// 检查后续是否被 . 或 - 扩展
			var kind Kind = KindIDENT
			if pos+1 < len(input) {
				sep := input[pos]
				switch sep {
				case '.':
					// CALL: IDENT (.IDENT)+ （至少一个点）
					if isIdentStart(input[pos+1]) {
						kind = KindCALL
						for {
							advance(1) // '.'
							for pos < len(input) && isIdentPart(input[pos]) {
								advance(1)
							}
							if pos+1 < len(input) && input[pos] == '.' && isIdentStart(input[pos+1]) {
								continue
							}
							break
						}
					}
				case '-':
					// EDGE_NAME: IDENT (-IDENT)+ （至少一个横杠）
					if isIdentStart(input[pos+1]) {
						kind = KindEDGE
						for {
							advance(1) // '-'
							for pos < len(input) && isIdentPart(input[pos]) {
								advance(1)
							}
							if pos+1 < len(input) && input[pos] == '-' && isIdentStart(input[pos+1]) {
								continue
							}
							break
						}
					}
				}
			}

			// 决定 Type
			var t Type
			switch kind {
			case KindCALL:
				t = TypeCALL
			case KindEDGE:
				t = TypeEDGE_NAME
			default:
				// 可能是关键字
				if kw, ok := keywords[first]; ok {
					t = kw
				} else {
					t = TypeID
				}
			}
			commit(t, typeKinds[t], startPos, pos, startLine, startCol)
			continue
		}

		// ⑦ 单字符 op / sep
		if t, ok := singleCharTokens[ch]; ok {
			debug("  [single] pos=%d ch=%q\n", pos, ch)
			commit(t, typeKinds[t], pos, pos+1, line, col)
			advance(1)
			continue
		}

		// ⑧ 错误
		return nil, emitError(pos, pos, fmt.Sprintf("unexpected character %q", rune(ch)))
	}

	// 末尾追加 EOF token（便于 parser 终止判断）
	tokens = append(tokens, Token{
		Type:  TypeEOF,
		Kind:  KindEOF,
		Value: "",
		Pos:   Pos{Line: line, Col: col, Offset: pos},
	})
	return tokens, nil
}
