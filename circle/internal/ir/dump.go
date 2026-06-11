// Package ir dump.go —— IR 调试打印
package ir

import (
	"fmt"
	"strings"
)

// DumpProgram 把 IRProgram 漂亮地打印出来（debug 用）
func DumpProgram(p *IRProgram) string {
	if p == nil {
		return "<nil IRProgram>"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== IRProgram ===\n"))
	sb.WriteString(fmt.Sprintf("main: %s\n", p.PkgName))
	for name, pkg := range p.Packages {
		sb.WriteString(DumpPackage(name, pkg))
	}
	if p.Topology != nil {
		sb.WriteString("--- main.Topology ---\n")
		sb.WriteString(DumpTopology(p.Topology))
	}
	return sb.String()
}

func DumpPackage(name string, p *IRPackage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- package %s ---\n", name))
	sb.WriteString(fmt.Sprintf("  Nodes: %d\n", len(p.Nodes)))
	for n, node := range p.Nodes {
		sb.WriteString(DumpNode(n, node))
	}
	sb.WriteString(fmt.Sprintf("  Edges: %d\n", len(p.Edges)))
	for k, e := range p.Edges {
		sb.WriteString(fmt.Sprintf("    %s <%s> %s (%s)\n", k.Src, k.Name, k.Dst, e.Kind))
		for i, op := range e.Flow {
			sb.WriteString(fmt.Sprintf("      op[%d] %s\n", i, dumpFlowOp(op)))
		}
	}
	if p.Topology != nil {
		sb.WriteString("  Topology:\n")
		sb.WriteString(DumpTopology(p.Topology))
	}
	return sb.String()
}

func DumpNode(name string, n *IRNode) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("    @%s (kind=%s):\n", name, n.Kind))
	for _, in := range n.Inputs {
		sb.WriteString(fmt.Sprintf("      >> %s %s\n", in.Type, in.Name))
	}
	for _, out := range n.Outputs {
		sb.WriteString(fmt.Sprintf("      %s >>\n", out.Name))
	}
	for _, s := range n.Init {
		sb.WriteString(fmt.Sprintf("      init: %v\n", s))
	}
	for i, b := range n.Blocks {
		sb.WriteString(fmt.Sprintf("      block[%d] in=%v out=%v auto=%v\n", i, b.Inputs, b.Outputs, b.IsAutoExec))
	}
	return sb.String()
}

func DumpTopology(t *IRTopology) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("    Edges (%d):\n", len(t.Edges)))
	for _, ek := range t.Edges {
		sb.WriteString(fmt.Sprintf("      %s <%s> %s\n", ek.Src, ek.Name, ek.Dst))
	}
	if len(t.AutoExecNodes) > 0 {
		sb.WriteString(fmt.Sprintf("    AutoExec: %v\n", t.AutoExecNodes))
	}
	if len(t.AllNodes) > 0 {
		sb.WriteString(fmt.Sprintf("    AllNodes: %v\n", t.AllNodes))
	}
	return sb.String()
}

func dumpFlowOp(op IRFlowOp) string {
	switch op.Op {
	case FlowOpSend:
		return fmt.Sprintf("send %s.%s -> %s.%s", op.Src, op.SrcAttr, op.Dst, op.DstAttr)
	case FlowOpCall:
		return fmt.Sprintf("call %s.%s -> %s.%s", op.Src, op.SrcAttr, op.Dst, op.DstAttr)
	case FlowOpBranchSend:
		return fmt.Sprintf("branch[%d] send %s.%s -> %s.%s", op.Branch, op.Src, op.SrcAttr, op.Dst, op.DstAttr)
	}
	return fmt.Sprintf("op(%d) %s.%s -> %s.%s", op.Op, op.Src, op.SrcAttr, op.Dst, op.DstAttr)
}
