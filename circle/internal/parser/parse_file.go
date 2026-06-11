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
//	[<TopologyBlock>]     ← 可选：包名块（== 描述包内数据流图）
//
// ════════════════════════════════════════════════════════════
//
//	三类符号 + 一个拓扑块（已确认设计）
//
// ════════════════════════════════════════════════════════════
//
//  1. 节点（Node）  @name { body }  或  name { body }
//     @ 前缀 = export 标记
//     带 @ 的节点 → 包外可见（其他包 import 后能 stdio.Println 调）
//     不带 @ 的节点 → 内部私有，仅本包可见
//
//  2. 边（Edge）  src <edge_name> dst { body }
//     运行时数据流实现（含 body，写具体走线）
//
//  3. 拓扑块（Topology）  <PkgName> { <EdgeRef>* }
//     块名必须 == 当前 package 名
//     内容是"包内有哪些边"的索引 —— **复用 edge 语法**（带 <edge_name>），但无 body
//     编译器把它和 top-level 的 EdgeDecl 按 (src, edge_name, dst) 三元组匹配：
//     拓扑条目 = 图结构层（哪些边存在）
//     EdgeDecl body = 行为层（边内点对点数据怎么走）
//     编译器用它做：
//     - 数据流分析（dataflow analysis）—— 沿拓扑条目建图
//     - 编译优化（死代码消除、inline、常量传播）
//     - 跨包数据路径追溯（外部灌入数据后流向哪）
//     和 export 完全无关！export 由 @ 前缀决定。
//
// 例子（example/stdio/stdio.ce 末尾）：
//
//	package stdio
//
//	@Println { ... }              // exported：包外可用 stdio.Println
//
//	Println <write> io.write {    // 【top-level 边定义】含 body，写"边内点对点怎么流"
//	    Println.fid >> io.write.fid
//	    Println.data >> io.write.data
//	}
//
//	stdio {                       // 【拓扑块】stdio 内部图结构索引，无 body
//	    Println <write> io.write  // 复用 edge 语法，列引用（src/edge_name/dst 三元组）
//	}
//
// 编译器配合：
//  1. 从拓扑块得到 (src, edge_name, dst) 三元组清单  →  图结构
//  2. 拿 (src, edge_name, dst) 去 top-level 找 EdgeDecl → 拿到 body 走线
//  3. 沿拓扑块追溯数据流：
//     外部灌入 → stdio.Println → io.write → sysio.write → syscall
//  4. sysio 编译终点的实际触发路径由"拓扑链"和"body 走线"共同决定
//
// parser 调度：
//   - 看到 IDENT/CALL/EDGE_NAME 时，先比对是不是当前包名
//   - 是 → 走 parseTopologyDecl，body 里解析"edge 引用"（无 body，但带 <edge_name>）
//   - 否 → 走原来 parseTopDecl（可能是 StructDecl / EdgeDecl / FuncDecl）
func (p *Parser) parseFile() *ast.File {
	file := &ast.File{PosBase: ast.PosBase{P: p.peek().Pos}}

	// ① package 声明
	// 记下当前包名，给后面拓扑块识别用
	if p.match(mocker_lex.TypeSYS_PACK) {
		file.Pkg = p.parsePackage()
		p.currentPkg = file.Pkg.Name // ★ 关键：把包名存到 parser
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
			// ★ 【拓扑块判定】两层检查：
			//   1) "main" 是保留名 → 永远是入口拓扑块（即使包名不是 main）
			//   2) 否则，如果当前 IDENT 等于当前包名 → 也是拓扑块
			//   两种情况都走 parseTopologyDecl
			if p.peek().Type == mocker_lex.TypeID {
				isMain := p.peek().Value == "main"
				isPkgName := p.currentPkg != "" && p.peek().Value == p.currentPkg
				if (isMain || isPkgName) && p.peekN(1).Type == mocker_lex.TypeSEP_LBRACE {
					d = p.parseTopologyDecl()
				} else {
					d = p.parseTopDecl()
				}
			} else {
				d = p.parseTopDecl()
			}
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

// parseTopologyDecl —— 草案（待实现，未提交）
//
//		<PkgName> "{" <EdgeRef>* "}"
//
//	 EdgeRef = IDENT "<" EdgeName ">" IDENT
//
//	 **关键**：拓扑块里**复用 edge 语法**（带 <edge_name>），但**无 body**
//	 —— 是对 top-level EdgeDecl 的引用，不是新东西
//
//	 块名 PkgName 必须 == 当前包名（用 p.currentPkg 校验）
//
//	 与 top-level EdgeDecl 的关系：
//	     Println <write> io.write {           // top-level 定义（含 body，行为层）
//	         Println.fid >> io.write.fid
//	         Println.data >> io.write.data
//	     }
//	     stdio {                              // 拓扑块（结构层）
//	         Println <write> io.write         // ← 同一组 (src, edge_name, dst)
//	     }                                    // 编译器按三元组匹配
//
// 编译器配合：
//  1. 从拓扑块收集 (src, edge_name, dst) 三元组清单 → 图结构层
//  2. 按三元组去 top-level 找对应 EdgeDecl，拿 body 走线 → 行为层
//  3. 沿三元组链追溯数据流：外部 → Println → io.write → sysio.write → syscall
//  4. 死代码消除：拓扑块里没列的边 → 视为内部细节，不强制 emit（可优化掉）
//  5. 跨包路径：拓扑条目里的 dst 可以是另一包的节点（io.write, sysio.write），
//     编译器递归读那个包的拓扑块 + 边 body
//
// AST 建议（复用 EdgeDecl，只把 body 留空）：
//
//	TopologyDecl{ PosBase, Name string, Edges []*EdgeDecl }
//	其中 Edges 是"无 body 的 EdgeDecl"—— Src/Edge/Dst 三个字段填好，Body 留 nil
//
// 与 export 的边界：
//   - 拓扑块管"图结构"（哪些边连起来）→ 编译器分析用
//   - @ 前缀管"可见性"（哪些节点包外能用）→ 模块系统用
//   - 两者完全正交，互不干扰
//
// ─────────────────────────────────────────────────────────────
// parseTopologyDecl —— 真函数
//
//	<PkgName> "{" <EdgeRef>* "}"
//	  EdgeRef = IDENT "<" EdgeName ">" IDENT
//
// 块名 == 当前包名（dispatcher 已确保）。EdgeRef 复用 EdgeDecl 形态，body 留空。
func (p *Parser) parseTopologyDecl() *ast.TopologyDecl {
	pos := p.consume(mocker_lex.TypeID).Pos // 块名
	name := p.peekN(-1).Value               // （已经 consume 过了；用 -1 拿刚 consume 的）
	_ = name

	p.consume(mocker_lex.TypeSEP_LBRACE)

	var edges []*ast.EdgeDecl
	for !p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
		// 跳过可能的 ; 分隔符（让 main { a <-> b; c <-> d; } 也能写）
		for p.match(mocker_lex.TypeSEP_SEMI) {
			p.consume(mocker_lex.TypeSEP_SEMI)
		}
		if p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
			break
		}

		edgePos := p.peek().Pos

		// src: IDENT or CALL
		var src string
		switch p.peek().Type {
		case mocker_lex.TypeID, mocker_lex.TypeCALL:
			src = p.consume(p.peek().Type).Value
		default:
			p.errorf("topology edge: expected src (IDENT/CALL), got %s", p.peek().Type)
			p.pos++
			continue
		}

		// <
		p.consume(mocker_lex.TypeOP_LT)

		// edge name: EDGE_NAME / ID / CALL
		var edgeName string
		switch p.peek().Type {
		case mocker_lex.TypeEDGE_NAME, mocker_lex.TypeID, mocker_lex.TypeCALL:
			edgeName = p.consume(p.peek().Type).Value
		default:
			p.errorf("topology edge: expected edge name, got %s", p.peek().Type)
			p.pos++
			continue
		}

		// >
		p.consume(mocker_lex.TypeOP_GT)

		// dst: IDENT or CALL
		var dst string
		switch p.peek().Type {
		case mocker_lex.TypeID, mocker_lex.TypeCALL:
			dst = p.consume(p.peek().Type).Value
		default:
			p.errorf("topology edge: expected dst (IDENT/CALL), got %s", p.peek().Type)
			p.pos++
			continue
		}

		// 可选 body：{ FlowStmt / FlowCont / FlowFanout }
		// 例：write <syscall> SYSCALL { write.fid >> SYSCALL.fid; write.data >> SYSCALL.data }
		var body []ast.Stmt
		if p.match(mocker_lex.TypeSEP_LBRACE) {
			p.consume(mocker_lex.TypeSEP_LBRACE)
			body = p.parseStmts() // 复用普通语句解析（支持 FlowStmt / FlowCont / FlowFanout）
			p.consume(mocker_lex.TypeSEP_RBRACE)
		}

		edges = append(edges, &ast.EdgeDecl{
			PosBase: ast.PosBase{P: edgePos},
			Src:     src,
			Edge:    edgeName,
			Dst:     dst,
			Body:    body,
		})
	}
	p.consume(mocker_lex.TypeSEP_RBRACE)

	return &ast.TopologyDecl{
		PosBase: ast.PosBase{P: pos},
		Name:    p.currentPkg,
		Edges:   edges,
	}
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
