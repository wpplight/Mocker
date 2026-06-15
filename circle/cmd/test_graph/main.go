package main

import (
	"fmt"
	"os"

	"circle/internal/ide"
)

func main() {
	svc := ide.NewService("/home/wpp/homework/Mocker/example")
	info, err := svc.OpenWorkspace("/home/wpp/homework/Mocker/example")
	if err != nil {
		fmt.Println("err:", err)
		os.Exit(1)
	}
	fmt.Println("=== ReparseWorkspace test ===")
	// 测试 ReparseWorkspace
	newSrc := info.MainSource + "\n// test add comment\n"
	info2, err := svc.ReparseWorkspace(info.MainFile, newSrc)
	if err != nil {
		fmt.Println("reparse err:", err)
		os.Exit(1)
	}
	fmt.Println("=== ReparseWorkspace 同款数据 ===")
	fmt.Println("Packages:")
	for _, p := range info2.Graph.Packages {
		fmt.Printf("  %s main=%v defaultCollapsed=%v nodes=%d boundary=%d\n",
			p.Name, p.IsMain, p.DefaultCollapsed, len(p.NodeIds), len(p.BoundaryNodeIds))
	}
	fmt.Println("Nodes:")
	for _, n := range info2.Graph.Nodes {
		fmt.Printf("  %-12s pkg=%-6s pos=%v\n", n.Name, n.Pkg, n.Position)
	}
	// M1.x: 验证 FlowNode.Data.members 已注入
	fmt.Println("Members (sample, hello):")
	for _, n := range info2.Graph.Nodes {
		if n.Name != "hello" {
			continue
		}
		if m, ok := n.Data["members"]; ok {
			if ms, ok := m.([]ide.NodeMember); ok {
				for _, mb := range ms {
					fmt.Printf("  [%s] %s", mb.Kind, mb.Name)
					if mb.Type != "" {
						fmt.Printf(" : %s", mb.Type)
					}
					if mb.Value != "" {
						fmt.Printf(" = %s", mb.Value)
					}
					fmt.Println()
				}
			}
		}
		break
	}
	fmt.Println("Edges:")
	for _, e := range info2.Graph.Edges {
		m := ""
		if e.CrossPackage {
			m = "⇄"
		}
		srcAttr := ""
		if e.SourceHandle != nil {
			srcAttr = *e.SourceHandle
		}
		dstAttr := ""
		if e.TargetHandle != nil {
			dstAttr = *e.TargetHandle
		}
		fmt.Printf("  %-10s %s %s.%s → %s.%s\n", e.Kind, m, e.Source, srcAttr, e.Target, dstAttr)
	}
	// M1.x: 验证 LocateNode（双击跳源用）
	fmt.Println("\n=== LocateNode test ===")
	for _, q := range []string{"main", "hello", "world", "stdio.Println", "stdio.to_string", "io.write"} {
		loc, err := svc.LocateNode(q)
		if err != nil {
			fmt.Printf("  %-20s err: %v\n", q, err)
			continue
		}
		fmt.Printf("  %-20s → %s:%d:%d\n", q, loc.Path, loc.Line, loc.Col)
	}
	// 还原
	_ = svc.SaveFile(info.MainFile, info.MainSource)
}
