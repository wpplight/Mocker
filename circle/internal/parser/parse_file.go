package parser

import (
	"fmt"

	"circle/internal/parser/ast"
	"circle/mocker_lex"
)

// parseFile 顶层调度器
//
//	package <name>
//	import <name>*
//	<TopDecl>*
//
// ════════════════════════════════════════════════════════════
//
//	两类符号 + 一个特殊 main 节点
//
// ════════════════════════════════════════════════════════════
//
//  1. 节点（Node）  @name { body }  或  name { body }
//     @ 前缀 = export 标记
//     body 可以包含：
//     - PortDecl（>> str hey）：输入端口
//     - VarDecl（h := "hi"）：带 init 的局部变量
//     - FlowDecl（h / h>> / h>>msg）：出度
//     - InstanceDecl / EdgeConnDecl（main 专用）
//
//  2. 边（Edge）  src <edge_name> dst { body }
//     运行时数据流实现（含 body，写具体走线）
//     或：<edge_name> { body }（Style 2 语法糖，src/dst 由 compiler 推断）
//
//  3. main 节点（特殊）
//     main 是个普通 NodeDecl（Name="main"，Kind=Node）
//     body 里可以包含：
//     - InstanceDecl：`typeName varName` —— 声明实例
//     - EdgeConnDecl：`src <edge> dst` —— 在实例之间连边
//     编译器从 main body 自动分析拓扑（不再需要 TopologyDecl）
//
// parser 调度：
//   - 看到 IDENT/CALL → 走 parseTopDecl
//   - 看到 @ → 走 parseStruct（@ 是 export 标记）
//   - 看到 < → 看是不是 Style 2 语法糖
//   - main 是 IDENT 名字，普通走 parseTopDecl 即可
func (p *Parser) parseFile() *ast.File {
	file := &ast.File{PosBase: ast.PosBase{P: p.peek().Pos}}

	// ① package 声明
	// 记下当前包名，给后面判断 main 节点用
	if p.match(mocker_lex.TypeSYS_PACK) {
		file.Pkg = p.parsePackage()
		p.currentPkg = file.Pkg.Name
	} else {
		p.errorf("expected 'package' at top of file")
	}

	// ② 顶层声明循环
	loopGuard := 0
	for !p.match(mocker_lex.TypeEOF) {
		startPos := p.pos
		var d ast.Decl
		switch p.peek().Type {
		case mocker_lex.TypeSYS_IMPORT:
			d = p.parseImport()
		case mocker_lex.TypeKW_ENUM:
			d = p.parseEnum()
		case mocker_lex.TypeSEP_AT:
			d = p.parseStruct() // @name 或 @<name>  ←  @ 是 export 标记
		case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
			// 简化：不再有特殊 topology 调度，全部走 parseTopDecl
			// main { ... } 通过 parseTopDecl 解析为 StructDecl{Name="main", Kind=Node}
			d = p.parseTopDecl()
		case mocker_lex.TypeOP_LT:
			// Style 2 语法糖：<edge_name> { body } —— 编译器从 body 推导 src/dst
			if p.isEdgeDeclSugarSig() {
				d = p.parseEdgeDeclSugar(p.peek().Pos)
			} else {
				d = p.parseStruct() // <out> 单边形式
			}
		default:
			p.errorf("unexpected token %s at top level", p.peek().Type)
			p.pos++
			continue
		}
		if d != nil {
			file.Decls = append(file.Decls, d)
		}
		// 防死循环：如果 parser 没消耗 token 就 break
		if p.pos == startPos {
			p.errorf("parser stuck at token %v (no progress), advancing", p.peek())
			p.pos++
		}
		loopGuard++
		if loopGuard > 10000 {
			p.errors = append(p.errors, ParseError{
				Pos: p.peek().Pos,
				Msg: fmt.Sprintf("infinite loop in parseFile (token=%v, pos=%d, decls=%d)",
					p.peek(), p.pos, len(file.Decls)),
			})
			break
		}
	}
	return file
}

func (p *Parser) parsePackage() *ast.PackageDecl {
	pos := p.consume(mocker_lex.TypeSYS_PACK).Pos
	name := p.consume(mocker_lex.TypeID).Value
	return &ast.PackageDecl{PosBase: ast.PosBase{P: pos}, Name: name}
}

// parseMainNode —— parse main 节点 body（已废弃：现在 main 走 parseStructBody）
//
// 保留为空函数，避免下游代码引用。
func (p *Parser) parseMainNode() {
}

func (p *Parser) parseImport() *ast.ImportDecl {
	pos := p.consume(mocker_lex.TypeSYS_IMPORT).Pos
	path := p.consume(mocker_lex.TypeID).Value
	return &ast.ImportDecl{PosBase: ast.PosBase{P: pos}, Path: path}
}

func (p *Parser) parseEnum() *ast.EnumDecl {
	pos := p.consume(mocker_lex.TypeKW_ENUM).Pos
	name := p.consume(mocker_lex.TypeID).Value
	p.consume(mocker_lex.TypeSEP_LBRACE)

	var values []string
	for !p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
		v := p.consume(mocker_lex.TypeID).Value
		values = append(values, v)
		if p.match(mocker_lex.TypeSEP_COMMA) {
			p.consume(mocker_lex.TypeSEP_COMMA)
		}
	}
	p.consume(mocker_lex.TypeSEP_RBRACE)

	return &ast.EnumDecl{PosBase: ast.PosBase{P: pos}, Name: name, Values: values}
}

// posBase 在 ast 包内定义；为避免循环引用，这里做别名
// （已通过 ast.PosBase 直接引用）
