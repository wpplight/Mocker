package parser

import (
	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseFuncDecl 函数声明
//   name "(" params ")" "{" body "}"
//   name "<" params ">" "{" body "}"
//   name             "{" body "}"   （无参数：main 块用）
//   <name> (   - 导出 FuncDecl
func (p *Parser) parseFuncDecl(name string, angleSig, alreadyExported bool) *ast.FuncDecl {
	pos := p.peek().Pos

	var params []*ast.Param
	// 三种形式：
	//   "(" params ")"  - 圆括号
	//   "<" params ">"  - 尖括号
	//   "{"             - 无参数（直接 body，main 块用）
	switch {
	case p.match(mocker_lex.TypeSEP_LPAREN):
		p.consume(mocker_lex.TypeSEP_LPAREN)
		params = p.parseParamList(mocker_lex.TypeSEP_RPAREN)
		p.consume(mocker_lex.TypeSEP_RPAREN)
	case p.match(mocker_lex.TypeOP_LT):
		p.consume(mocker_lex.TypeOP_LT)
		params = p.parseParamList(mocker_lex.TypeOP_GT)
		p.consume(mocker_lex.TypeOP_GT)
	case p.match(mocker_lex.TypeSEP_LBRACE):
		// 无参数函数：直接 body
		// 这种情况通常用于 main 块：main { body }
		// 保留 angleSig 参数以防未来需要
		_ = angleSig
	default:
		p.errorf("expected '(', '<' or '{' after func name %q, got %s", name, p.peek().Type)
		// 防死循环
		if !p.match(mocker_lex.TypeSEP_LBRACE) {
			p.pos++
		}
	}

	p.consume(mocker_lex.TypeSEP_LBRACE)
	body := p.parseStmts()
	p.consume(mocker_lex.TypeSEP_RBRACE)

	return &ast.FuncDecl{
		PosBase:  ast.PosBase{P: pos},
		Name:     name,
		Exported: alreadyExported,
		Params:   params,
		Body:     body,
	}
}

// parseParamList 解析形参直到 end token
func (p *Parser) parseParamList(end mocker_lex.Type) []*ast.Param {
	var params []*ast.Param
	loopGuard := 0
	for !p.match(end, mocker_lex.TypeEOF) {
		startPos := p.pos
		typ := p.parseTypeRef()
		name := p.consume(mocker_lex.TypeID).Value
		params = append(params, &ast.Param{
			PosBase: ast.PosBase{P: p.peek().Pos},
			Type:    typ,
			Name:    name,
		})
		if p.match(mocker_lex.TypeSEP_COMMA) {
			p.consume(mocker_lex.TypeSEP_COMMA)
		}
		if p.pos == startPos {
			p.errorf("parseParamList stuck, advancing")
			p.pos++
		}
		loopGuard++
		if loopGuard > 1000 {
			p.errorf("infinite loop in parseParamList")
			break
		}
	}
	return params
}
