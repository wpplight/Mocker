package parser

import (
	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseExpr Pratt 算法入口
// 优先级（高 → 低）：
//   7  .  (成员访问)
//   6  *  /
//   5  +  -
//   4  <  >  <=  >=
//   3  == !=
//   2  &&
//   1  ||
func (p *Parser) parseExpr() ast.Expr {
	return p.parseBinary(1)
}

func (p *Parser) parseBinary(minPrec int) ast.Expr {
	lhs := p.parseUnary()

	for p.isOp() && opPrec(p.peek().Type) >= minPrec {
		op := p.peek()
		p.pos++
		prec := opPrec(op.Type)
		rhs := p.parseBinary(prec + 1)
		lhs = &ast.BinaryExpr{
			PosBase: ast.PosBase{P: lhs.Pos()},
			Op:      op.Value,
			L:       lhs,
			R:       rhs,
		}
	}
	return lhs
}

func (p *Parser) isOp() bool {
	return opPrec(p.peek().Type) > 0
}

func (p *Parser) parseUnary() ast.Expr {
	if p.match(mocker_lex.TypeOP_SUB, mocker_lex.TypeOP_NOT) {
		op := p.consume(p.peek().Type)
		x := p.parseUnary()
		return &ast.UnaryExpr{
			PosBase: ast.PosBase{P: op.Pos},
			Op:      op.Value,
			X:       x,
		}
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() ast.Expr {
	pos := p.peek().Pos
	switch p.peek().Type {
	case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
		return p.parseIdentOrMemberOrCall()
	case mocker_lex.TypeSTRING:
		return &ast.LiteralExpr{
			PosBase: ast.PosBase{P: pos},
			Kind:    ast.LitString,
			Value:   p.consume(mocker_lex.TypeSTRING).Value,
		}
	case mocker_lex.TypeNUM:
		return &ast.LiteralExpr{
			PosBase: ast.PosBase{P: pos},
			Kind:    ast.LitNumber,
			Value:   p.consume(mocker_lex.TypeNUM).Value,
		}
	case mocker_lex.TypeKW_TRUE:
		p.consume(mocker_lex.TypeKW_TRUE)
		return &ast.LiteralExpr{PosBase: ast.PosBase{P: pos}, Kind: ast.LitBool, Value: "true"}
	case mocker_lex.TypeKW_FALSE:
		p.consume(mocker_lex.TypeKW_FALSE)
		return &ast.LiteralExpr{PosBase: ast.PosBase{P: pos}, Kind: ast.LitBool, Value: "false"}
	case mocker_lex.TypeSEP_LPAREN:
		p.consume(mocker_lex.TypeSEP_LPAREN)
		e := p.parseExpr()
		p.consume(mocker_lex.TypeSEP_RPAREN)
		return e
	default:
		p.errorf("unexpected token in expression: %s", p.peek().Type)
		p.pos++ // 强制前进防死循环（如 "a + + b" 之类连续 op）
		return &ast.LiteralExpr{PosBase: ast.PosBase{P: pos}}
	}
}

func (p *Parser) parseIdentOrMemberOrCall() ast.Expr {
	pos := p.peek().Pos
	first := p.consume(p.peek().Type).Value
	e := ast.Expr(&ast.IdentExpr{PosBase: ast.PosBase{P: pos}, Name: first})

	// 成员访问链
	for p.match(mocker_lex.TypeSEP_DOT) {
		p.consume(mocker_lex.TypeSEP_DOT)
		name := p.consume(mocker_lex.TypeID).Value
		e = &ast.MemberExpr{
			PosBase: ast.PosBase{P: pos},
			Obj:     e,
			Name:    name,
		}
	}

	// 可选调用
	if p.match(mocker_lex.TypeSEP_LPAREN) {
		p.consume(mocker_lex.TypeSEP_LPAREN)
		var args []ast.Expr
		for !p.match(mocker_lex.TypeSEP_RPAREN, mocker_lex.TypeEOF) {
			args = append(args, p.parseExpr())
			if p.match(mocker_lex.TypeSEP_COMMA) {
				p.consume(mocker_lex.TypeSEP_COMMA)
			}
		}
		p.consume(mocker_lex.TypeSEP_RPAREN)
		e = &ast.CallExpr{
			PosBase: ast.PosBase{P: pos},
			Fn:      e,
			Args:    args,
		}
	}
	return e
}

func opPrec(t mocker_lex.Type) int {
	switch t {
	case mocker_lex.TypeOP_OR:
		return 1
	case mocker_lex.TypeOP_AND:
		return 2
	case mocker_lex.TypeOP_EQ, mocker_lex.TypeOP_NE:
		return 3
	case mocker_lex.TypeOP_LT, mocker_lex.TypeOP_GT,
		mocker_lex.TypeOP_LE, mocker_lex.TypeOP_GE:
		return 4
	case mocker_lex.TypeOP_ADD, mocker_lex.TypeOP_SUB:
		return 5
	case mocker_lex.TypeOP_MUL, mocker_lex.TypeOP_DIV:
		return 6
	case mocker_lex.TypeSEP_DOT:
		return 7
	}
	return 0
}
