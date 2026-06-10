package parser

import (
	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseTypeRef 解析类型：str / num / bool / 用户类型 / *T / T[]
func (p *Parser) parseTypeRef() ast.TypeRef {
	pos := p.peek().Pos

	// 指针前缀：*T
	if p.match(mocker_lex.TypeOP_MUL) {
		p.consume(mocker_lex.TypeOP_MUL)
		elem := p.parseTypeRef()
		return &ast.TypePtr{PosBase: ast.PosBase{P: pos}, Elem: elem}
	}

	if !p.match(mocker_lex.TypeID, mocker_lex.TypeTYPE_STR, mocker_lex.TypeTYPE_NUM,
		mocker_lex.TypeTYPE_BOOL, mocker_lex.TypeTYPE_BYTE, mocker_lex.TypeTYPE_ANY) {
		p.errorf("expected type, got %s", p.peek().Type)
		return &ast.TypeName{PosBase: ast.PosBase{P: pos}, Name: "any"}
	}

	// 取类型名（TYPE_* 用 literal 字符串作为名字）
	t := p.consume(p.peek().Type)
	typeName := t.Value

	tr := ast.TypeRef(&ast.TypeName{PosBase: ast.PosBase{P: pos}, Name: typeName})

	// 可选 [] 后缀
	for p.match(mocker_lex.TypeSEP_LBRACKET) {
		p.consume(mocker_lex.TypeSEP_LBRACKET)
		p.consume(mocker_lex.TypeSEP_RBRACKET)
		tr = &ast.TypeArray{PosBase: ast.PosBase{P: pos}, Elem: tr}
	}
	return tr
}
