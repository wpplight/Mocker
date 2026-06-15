package ide

import (
	"strings"

	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// emitStructMembers 把节点 body 的 []StructMember 拍平成 []NodeMember
//
// 目的是给前端 PropertiesPanel 显示 + 拖拽编辑用。
// 只取前端关心的字段，不暴露完整 AST。
func emitStructMembers(members []ast.StructMember) []NodeMember {
	out := make([]NodeMember, 0, len(members))
	for _, m := range members {
		switch v := m.(type) {
		case *ast.PortDecl:
			t := ""
			if v.Type != nil {
				t = typeRefToString(v.Type)
			}
			out = append(out, NodeMember{
				Kind: "port_in",
				Name: v.Name,
				Type: t,
			})
		case *ast.VarDecl:
			out = append(out, NodeMember{
				Kind:  "var",
				Name:  v.Name,
				Value: exprToString(v.Init),
			})
		case *ast.FieldDecl:
			out = append(out, NodeMember{
				Kind: "field",
				Name: v.Name,
				Type: typeRefToString(v.Type),
			})
		case *ast.FlowDecl:
			out = append(out, NodeMember{
				Kind:  "flow",
				Name:  v.Head,
				Value: flowChainToString(v.Chain),
			})
		case *ast.SubInstanceDecl:
			out = append(out, NodeMember{
				Kind: "sub_instance",
				Name: v.Name,
				Type: v.Type,
			})
		case *ast.SubEdgeDecl:
			out = append(out, NodeMember{
				Kind:  "sub_edge",
				Name:  v.Src + " → " + v.Dst,
				Type:  "<" + v.Edge + ">",
				Value: v.Src,
			})
		case *ast.InstanceDecl:
			out = append(out, NodeMember{
				Kind: "instance",
				Name: v.Name,
				Type: v.Type,
			})
		case *ast.EdgeConnDecl:
			out = append(out, NodeMember{
				Kind:  "edge_conn",
				Name:  v.Src + " <" + v.Edge + "> " + v.Dst,
				Value: v.Src + " <" + v.Edge + "> " + v.Dst,
			})
		case *ast.IfStmt:
			out = append(out, NodeMember{
				Kind:  "control",
				Name:  "if",
				Value: exprToString(v.Cond),
			})
		case *ast.ForStmt:
			out = append(out, NodeMember{
				Kind:  "control",
				Name:  "for",
				Value: forStmtToString(v),
			})
		case *ast.WhileStmt:
			out = append(out, NodeMember{
				Kind:  "control",
				Name:  "while",
				Value: exprToString(v.Cond),
			})
		case *ast.AssignStmt:
			out = append(out, NodeMember{
				Kind:  "assign",
				Name:  strings.Join(v.Lhs, ","),
				Value: exprToString(v.Rhs),
			})
		}
	}
	return out
}

// emitNodeDetail 从 NodeSymbol 出 NodeDetail（合并 AST 信息）
func emitNodeDetail(sym *semantic.NodeSymbol, file *ast.File) NodeDetail {
	d := NodeDetail{
		Name:     sym.Name,
		Exported: sym.Exported,
		Kind:     "node",
		Members:  []NodeMember{},
	}

	// 找到对应 StructDecl 拿 body
	for _, decl := range file.Decls {
		s, ok := decl.(*ast.StructDecl)
		if !ok || s.Name != sym.Name {
			continue
		}
		d.Members = emitStructMembers(s.Members)
		break
	}
	return d
}

// emitEdgeDetail 从 EdgeDecl 出 EdgeDetail
func emitEdgeDetail(e *ast.EdgeDecl) EdgeDetail {
	body := make([]string, 0, len(e.Body))
	for _, stmt := range e.Body {
		body = append(body, stmtToString(stmt))
	}
	return EdgeDetail{
		Src:  e.Src,
		Edge: e.Edge,
		Dst:  e.Dst,
		Body: body,
	}
}

// ──── helpers ────

// typeRefToString TypeRef → 字符串
func typeRefToString(ref ast.TypeRef) string {
	if ref == nil {
		return ""
	}
	switch v := ref.(type) {
	case *ast.TypeName:
		return v.Name
	case *ast.TypeArray:
		return typeRefToString(v.Elem) + "[]"
	case *ast.TypePtr:
		return "*" + typeRefToString(v.Elem)
	}
	return "?"
}

// exprToString Expr → 字符串（最简化版本，去掉注释 / 缩进）
func exprToString(e ast.Expr) string {
	if e == nil {
		return ""
	}
	// 简化：直接走 Position + 拿源码切片在 caller 做
	// 这里只是 MVP，先用空实现
	return ""
}

// flowChainToString FlowChain → 字符串
func flowChainToString(chain *ast.FlowChain) string {
	if chain == nil {
		return ""
	}
	parts := make([]string, 0, len(chain.Steps))
	for _, s := range chain.Steps {
		parts = append(parts, flowTargetToString(s.Target))
	}
	return ">> " + strings.Join(parts, " >> ")
}

// flowTargetToString FlowTarget → 字符串
func flowTargetToString(t ast.FlowTarget) string {
	switch v := t.(type) {
	case *ast.FlowIdent:
		return strings.Join(v.Chain, ".")
	case *ast.FlowLiteral:
		return "\"" + v.Value + "\""
	case *ast.FlowExpr:
		return "<expr>"
	}
	return ""
}

// stmtToString Stmt → 单行字符串
func stmtToString(s ast.Stmt) string {
	switch v := s.(type) {
	case *ast.FlowStmt:
		parts := make([]string, 0, len(v.Steps))
		for _, st := range v.Steps {
			parts = append(parts, flowTargetToString(st.Target))
		}
		return strings.Join(parts, " >> ")
	case *ast.FlowCont:
		parts := make([]string, 0, len(v.Steps))
		for _, st := range v.Steps {
			parts = append(parts, flowTargetToString(st.Target))
		}
		return ">> " + strings.Join(parts, " >> ")
	}
	return ""
}

// forStmtToString ForStmt → 字符串
func forStmtToString(v *ast.ForStmt) string {
	parts := []string{"for"}
	if v.Init != nil {
		parts = append(parts, stmtToString(v.Init))
	}
	parts = append(parts, ";")
	if v.Cond != nil {
		parts = append(parts, exprToString(v.Cond))
	}
	parts = append(parts, ";")
	if v.Post != nil {
		parts = append(parts, stmtToString(v.Post))
	}
	return strings.Join(parts, " ")
}