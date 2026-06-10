// Package parser 提供 AST 可视化工具
package parser

import (
	"fmt"
	"strings"

	"circle/internal/parser/ast"
)

// Dump 把 AST 转成可读的文本形式
func Dump(f *ast.File) string {
	var sb strings.Builder
	if f.Pkg != nil {
		sb.WriteString(fmt.Sprintf("File package=%s\n", f.Pkg.Name))
	} else {
		sb.WriteString("File (no package)\n")
	}
	for _, imp := range f.Imports {
		sb.WriteString(fmt.Sprintf("  import %s\n", imp.Path))
	}
	for _, d := range f.Decls {
		dumpDecl(&sb, d, 1)
	}
	return sb.String()
}

func indent(sb *strings.Builder, depth int) {
	for i := 0; i < depth; i++ {
		sb.WriteString("  ")
	}
}

func dumpDecl(sb *strings.Builder, d ast.Decl, depth int) {
	indent(sb, depth)
	switch v := d.(type) {
	case *ast.EnumDecl:
		fmt.Fprintf(sb, "EnumDecl %s {%s}\n", v.Name, strings.Join(v.Values, ", "))
	case *ast.StructDecl:
		exportMark := ""
		if v.Exported {
			exportMark = "@"
		}
		fmt.Fprintf(sb, "%s%s %s {\n", exportMark, v.Kind, v.Name)
		for _, m := range v.Members {
			dumpMember(sb, m, depth+1)
		}
		indent(sb, depth)
		sb.WriteString("}\n")
	case *ast.EdgeDecl:
		fmt.Fprintf(sb, "EdgeDecl %s <%s> %s {\n", v.Src, v.Edge, v.Dst)
		for _, s := range v.Body {
			dumpStmt(sb, s, depth+1)
		}
		indent(sb, depth)
		sb.WriteString("}\n")
	case *ast.TopologyDecl:
		fmt.Fprintf(sb, "TopologyDecl pkg=%s (structure layer, %d edge refs)\n", v.Name, len(v.Edges))
		for _, e := range v.Edges {
			indent(sb, depth+1)
			fmt.Fprintf(sb, "EdgeRef %s <%s> %s  // 行为层在同名 top-level EdgeDecl\n",
				e.Src, e.Edge, e.Dst)
		}
	case *ast.FuncDecl:
		exportMark := ""
		if v.Exported {
			exportMark = "@"
		}
		params := make([]string, len(v.Params))
		for i, p := range v.Params {
			params[i] = fmt.Sprintf("%s %s", dumpType(p.Type), p.Name)
		}
		fmt.Fprintf(sb, "%sFuncDecl %s(%s) {\n", exportMark, v.Name, strings.Join(params, ", "))
		for _, s := range v.Body {
			dumpStmt(sb, s, depth+1)
		}
		indent(sb, depth)
		sb.WriteString("}\n")
	case *ast.ImportDecl:
		fmt.Fprintf(sb, "ImportDecl %s\n", v.Path)
	}
}

func dumpMember(sb *strings.Builder, m ast.StructMember, depth int) {
	indent(sb, depth)
	switch v := m.(type) {
	case *ast.FieldDecl:
		fmt.Fprintf(sb, "FieldDecl %s %s\n", dumpType(v.Type), v.Name)
	case *ast.VarDecl:
		fmt.Fprintf(sb, "VarDecl %s := %s\n", v.Name, dumpExpr(v.Init))
		if v.Flow != nil {
			dumpFlowChain(sb, v.Flow, depth+1)
		}
	case *ast.FlowDecl:
		fmt.Fprintf(sb, "FlowDecl %s\n", v.Head)
		if v.Chain != nil {
			dumpFlowChain(sb, v.Chain, depth+1)
		}
	case *ast.PortDecl:
		fmt.Fprintf(sb, "PortDecl >> %s %s\n", dumpType(v.Type), v.Name)
	}
}

func dumpStmt(sb *strings.Builder, s ast.Stmt, depth int) {
	indent(sb, depth)
	switch v := s.(type) {
	case *ast.VarDecl:
		fmt.Fprintf(sb, "VarDecl %s := %s\n", v.Name, dumpExpr(v.Init))
		if v.Flow != nil {
			dumpFlowChain(sb, v.Flow, depth+1)
		}
	case *ast.AssignStmt:
		fmt.Fprintf(sb, "AssignStmt %s := %s\n", strings.Join(v.Lhs, ", "), dumpExpr(v.Rhs))
	case *ast.IfStmt:
		fmt.Fprintf(sb, "IfStmt %s {\n", dumpExpr(v.Cond))
		if v.Body != nil {
			for _, s2 := range v.Body.Stmts {
				dumpStmt(sb, s2, depth+1)
			}
		}
		indent(sb, depth)
		sb.WriteString("}\n")
		if v.Else != nil {
			indent(sb, depth)
			sb.WriteString("  else ")
			switch e := v.Else.(type) {
			case *ast.IfStmt:
				fmt.Fprintf(sb, "%s\n", dumpExpr(e.Cond))
			case *ast.BlockStmt:
				sb.WriteString("{\n")
				for _, s2 := range e.Stmts {
					dumpStmt(sb, s2, depth+2)
				}
				indent(sb, depth)
				sb.WriteString("}\n")
			}
		}
	case *ast.ReturnStmt:
		if v.Value == nil {
			sb.WriteString("ReturnStmt\n")
		} else {
			fmt.Fprintf(sb, "ReturnStmt %s\n", dumpExpr(v.Value))
		}
	case *ast.Connection:
		hops := make([]string, len(v.Hops))
		for i, h := range v.Hops {
			hops[i] = dumpHop(h)
		}
		fmt.Fprintf(sb, "Connection %s\n", strings.Join(hops, " → "))
	case *ast.FlowStmt:
		steps := make([]string, len(v.Steps))
		for i, s2 := range v.Steps {
			steps[i] = dumpFlowStep(s2)
		}
		fmt.Fprintf(sb, "FlowStmt %s\n", strings.Join(steps, " >> "))
	case *ast.FlowCont:
		steps := make([]string, len(v.Steps))
		for i, s2 := range v.Steps {
			steps[i] = dumpFlowStep(s2)
		}
		fmt.Fprintf(sb, "FlowCont >> %s\n", strings.Join(steps, " >> "))
	case *ast.FlowFanout:
		fmt.Fprintf(sb, "FlowFanout src=%s (concurrent branches: %d)\n",
			dumpFlowTarget(v.Src), len(v.Branches))
		for i, b := range v.Branches {
			indent(sb, depth+1)
			steps := make([]string, len(b.Steps))
			for j, s2 := range b.Steps {
				steps[j] = dumpFlowStep(s2)
			}
			fmt.Fprintf(sb, "Branch[%d] >> %s\n", i, strings.Join(steps, " >> "))
		}
	case *ast.ExprStmtWrap:
		fmt.Fprintf(sb, "ExprStmt %s\n", dumpExpr(v.E))
	}
}

func dumpHop(h ast.ConnectionHop) string {
	switch v := h.(type) {
	case *ast.NodeRef:
		return v.Name
	case *ast.EdgeRef:
		return "<" + v.Name + ">"
	case *ast.CallRef:
		args := make([]string, len(v.Args))
		for i, a := range v.Args {
			args[i] = dumpExpr(a)
		}
		return fmt.Sprintf("%s(%s)", dumpExpr(v.Fn), strings.Join(args, ", "))
	}
	return "?"
}

func dumpFlowChain(sb *strings.Builder, c *ast.FlowChain, depth int) {
	indent(sb, depth)
	steps := make([]string, len(c.Steps))
	for i, s := range c.Steps {
		steps[i] = dumpFlowStep(s)
	}
	fmt.Fprintf(sb, "FlowChain >> %s\n", strings.Join(steps, " >> "))
}

func dumpFlowStep(s *ast.FlowStep) string {
	out := dumpFlowTarget(s.Target)
	if s.As != "" {
		out += " as " + s.As
	}
	return out
}

func dumpFlowTarget(t ast.FlowTarget) string {
	switch v := t.(type) {
	case *ast.FlowIdent:
		out := strings.Join(v.Chain, ".")
		if v.Call != nil {
			args := make([]string, len(v.Call))
			for i, a := range v.Call {
				args[i] = dumpExpr(a)
			}
			out += "(" + strings.Join(args, ", ") + ")"
		}
		return out
	case *ast.FlowExpr:
		return "(" + dumpExpr(v.Expr) + ")"
	case *ast.FlowLiteral:
		return v.Value
	}
	return "?"
}

func dumpType(t ast.TypeRef) string {
	if t == nil {
		return "<nil-type>"
	}
	switch v := t.(type) {
	case *ast.TypeName:
		return v.Name
	case *ast.TypeArray:
		return dumpType(v.Elem) + "[]"
	case *ast.TypePtr:
		return "*" + dumpType(v.Elem)
	}
	return "?"
}

func dumpExpr(e ast.Expr) string {
	if e == nil {
		return "<nil>"
	}
	switch v := e.(type) {
	case *ast.IdentExpr:
		return v.Name
	case *ast.LiteralExpr:
		switch v.Kind {
		case ast.LitString:
			return v.Value
		case ast.LitNumber:
			return v.Value
		case ast.LitBool:
			return v.Value
		}
		return v.Value
	case *ast.MemberExpr:
		return dumpExpr(v.Obj) + "." + v.Name
	case *ast.CallExpr:
		args := make([]string, len(v.Args))
		for i, a := range v.Args {
			args[i] = dumpExpr(a)
		}
		return fmt.Sprintf("%s(%s)", dumpExpr(v.Fn), strings.Join(args, ", "))
	case *ast.BinaryExpr:
		return fmt.Sprintf("(%s %s %s)", dumpExpr(v.L), v.Op, dumpExpr(v.R))
	case *ast.UnaryExpr:
		return fmt.Sprintf("(%s%s)", v.Op, dumpExpr(v.X))
	}
	return "?"
}
