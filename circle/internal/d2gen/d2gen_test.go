package d2gen

import (
	"strings"
	"testing"

	"circle/internal/ir"
)

// buildTestProgram 构造一个简单的 IRProgram 用于测试
//
// 拓扑：
//
//	hello → world (add_str, 双向：hello.h → world.words, world.new → hello.out_str)
//	hello → Println (SubFlow: out_str >> p.msg)
//	Println → write (SubFlow)
func buildTestProgram() *ir.IRProgram {
	prog := ir.NewIRProgram()

	// ---- package main ----
	mainPkg := ir.NewIRPackage("main")
	mainPkg.IsMain = true

	// hello 节点
	hello := &ir.IRNode{
		Name:    "hello",
		Pkg:     "main",
		Kind:    ir.NodeKindNode,
		Inputs:  []ir.IRInput{{Name: "out_str", Type: ir.Str()}},
		Outputs: []ir.IROutput{{Name: "h", Type: ir.Str()}},
		Init:    []ir.IRStmt{},
		SubInstances: []*ir.IRSubInstance{
			{TypeName: "main.world", InstanceName: "w"},
			{TypeName: "stdio.Println", InstanceName: "p"},
		},
		SubEdges: []*ir.IRSubEdge{
			{SrcAttr: "h", EdgeName: "add_str", DstInstance: "w", DstAttr: "words", RetAttr: "out_str"},
		},
		SubFlows: []*ir.IRSubFlow{
			{SrcAttr: "out_str", DstInstance: "p", DstAttr: "msg"},
		},
	}

	// world 节点
	world := &ir.IRNode{
		Name:    "world",
		Pkg:     "main",
		Kind:    ir.NodeKindNode,
		Inputs:  []ir.IRInput{{Name: "words", Type: ir.Str()}},
		Outputs: []ir.IROutput{{Name: "new", Type: ir.Str()}},
	}

	// main 节点（拓扑容器）
	mainNode := &ir.IRNode{
		Name: "main",
		Pkg:  "main",
		Kind: ir.NodeKindNode,
	}

	mainPkg.Nodes = map[string]*ir.IRNode{
		"hello": hello,
		"world": world,
		"main":  mainNode,
	}

	// 顶层 edge: hello <add_str> world（双向）
	addStrEdge := &ir.IREdge{
		Src:  "hello",
		Name: "add_str",
		Dst:  "world",
		Kind: ir.EdgeSync,
		Flow: []ir.IRFlowOp{
			{Op: ir.FlowOpSend, Src: "hello", SrcAttr: "h", Dst: "world", DstAttr: "words"},
			{Op: ir.FlowOpSend, Src: "world", SrcAttr: "new", Dst: "hello", DstAttr: "out_str"},
		},
	}
	ek := ir.EdgeKey{Src: "hello", Name: "add_str", Dst: "world"}
	mainPkg.Edges = map[ir.EdgeKey]*ir.IREdge{ek: addStrEdge}

	mainPkg.Topology = &ir.IRTopology{
		Edges: []ir.EdgeKey{ek},
		VarInstances: map[string]string{
			"happy": "hello",
		},
		AllNodes: []string{"hello"},
	}

	prog.AddPackage(mainPkg)

	// ---- package stdio ----
	stdioPkg := ir.NewIRPackage("stdio")
	printlnNode := &ir.IRNode{
		Name:    "Println",
		Pkg:     "stdio",
		Kind:    ir.NodeKindNode,
		Inputs:  []ir.IRInput{{Name: "msg", Type: ir.Str()}},
		Outputs: []ir.IROutput{{Name: "fid", Type: ir.Num()}, {Name: "data", Type: ir.Str()}},
		SubInstances: []*ir.IRSubInstance{
			{TypeName: "io.write", InstanceName: "out"},
		},
		SubFlows: []*ir.IRSubFlow{
			{SrcAttr: "fid", DstInstance: "out", DstAttr: "fid"},
			{SrcAttr: "data", DstInstance: "out", DstAttr: "data"},
		},
	}
	stdioPkg.Nodes = map[string]*ir.IRNode{"Println": printlnNode}
	prog.AddPackage(stdioPkg)

	// ---- package io ----
	ioPkg := ir.NewIRPackage("io")
	writeNode := &ir.IRNode{
		Name:    "write",
		Pkg:     "io",
		Kind:    ir.NodeKindNode,
		Inputs:  []ir.IRInput{{Name: "fid", Type: ir.Num()}, {Name: "data", Type: ir.Str()}},
		Outputs: []ir.IROutput{{Name: "fid", Type: ir.Num()}, {Name: "data", Type: ir.Str()}},
		SubFlows: []*ir.IRSubFlow{
			{SrcAttr: "fid", DstInstance: "SYSCALL", DstAttr: "fid"},
			{SrcAttr: "data", DstInstance: "SYSCALL", DstAttr: "data"},
		},
	}
	ioPkg.Nodes = map[string]*ir.IRNode{"write": writeNode}
	prog.AddPackage(ioPkg)

	return prog
}

func TestGenerate_BidirectionalEdge(t *testing.T) {
	prog := buildTestProgram()
	d2 := Generate(prog, nil)

	// hello <-> world 应该使用双向箭头
	if !strings.Contains(d2, "hello <-> world") {
		t.Errorf("expected bidirectional edge 'hello <-> world', got:\n%s", d2)
	}
	// 不应该有单向的 hello -> world
	if strings.Contains(d2, "hello -> world") {
		t.Errorf("should NOT have unidirectional 'hello -> world' when bidirectional exists, got:\n%s", d2)
	}
}

func TestGenerate_BidirectionalEdgeLabel(t *testing.T) {
	prog := buildTestProgram()
	d2 := Generate(prog, nil)

	// 标签应该是 <add_str>
	if !strings.Contains(d2, `hello <-> world: "<add_str>"`) {
		t.Errorf("expected bidirectional edge with label '<add_str>', got:\n%s", d2)
	}
}

func TestGenerate_SubFlowEdgesUnidirectional(t *testing.T) {
	prog := buildTestProgram()
	d2 := Generate(prog, nil)

	// SubFlow edges 应该保持单向
	if !strings.Contains(d2, `hello -> Println: ">>"`) {
		t.Errorf("expected 'hello -> Println: \">>\"', got:\n%s", d2)
	}
	if !strings.Contains(d2, `Println -> write: ">>"`) {
		t.Errorf("expected 'Println -> write: \">>\"', got:\n%s", d2)
	}
}

func TestGenerate_MainEntryUnidirectional(t *testing.T) {
	prog := buildTestProgram()
	d2 := Generate(prog, nil)

	// main_entry → hello 应该保持单向
	if !strings.Contains(d2, "main_entry -> hello") {
		t.Errorf("expected 'main_entry -> hello', got:\n%s", d2)
	}
}

func TestGenerate_Direction(t *testing.T) {
	prog := buildTestProgram()
	d2 := Generate(prog, &Options{Direction: "down"})

	if !strings.Contains(d2, "direction: down") {
		t.Errorf("expected 'direction: down', got:\n%s", d2)
	}
}

func TestGenerate_SubEdgeBidirectionalPair(t *testing.T) {
	// 测试 SubEdge 双向对：A 有 SubEdge 到 B，同时 B 有 SubEdge 到 A
	prog := ir.NewIRProgram()

	mainPkg := ir.NewIRPackage("main")
	mainPkg.IsMain = true

	a := &ir.IRNode{
		Name: "A",
		Pkg:  "main",
		Kind: ir.NodeKindNode,
		SubInstances: []*ir.IRSubInstance{
			{TypeName: "main.B", InstanceName: "b"},
		},
		SubEdges: []*ir.IRSubEdge{
			{SrcAttr: "x", EdgeName: "req", DstInstance: "b", DstAttr: "in"},
		},
	}

	b := &ir.IRNode{
		Name: "B",
		Pkg:  "main",
		Kind: ir.NodeKindNode,
		SubInstances: []*ir.IRSubInstance{
			{TypeName: "main.A", InstanceName: "a"},
		},
		SubEdges: []*ir.IRSubEdge{
			{SrcAttr: "out", EdgeName: "resp", DstInstance: "a", DstAttr: "y"},
		},
	}

	mainNode := &ir.IRNode{Name: "main", Pkg: "main", Kind: ir.NodeKindNode}

	mainPkg.Nodes = map[string]*ir.IRNode{"A": a, "B": b, "main": mainNode}
	mainPkg.Topology = &ir.IRTopology{
		VarInstances: map[string]string{"entry": "A"},
		AllNodes:     []string{"A"},
	}
	prog.AddPackage(mainPkg)

	d2 := Generate(prog, nil)

	// 应该有双向箭头
	if !strings.Contains(d2, "A <-> B") {
		t.Errorf("expected bidirectional edge 'A <-> B', got:\n%s", d2)
	}
	// 标签应包含两个方向的 edge name
	if !strings.Contains(d2, "<req>") || !strings.Contains(d2, "<resp>") {
		t.Errorf("expected labels '<req>' and '<resp>' in bidirectional edge, got:\n%s", d2)
	}
}

// TestGenerate_UnidirectionalTopLevelEdge 测试无双向流时的 top-level edge
func TestGenerate_UnidirectionalTopLevelEdge(t *testing.T) {
	prog := ir.NewIRProgram()

	mainPkg := ir.NewIRPackage("main")
	mainPkg.IsMain = true

	a := &ir.IRNode{Name: "A", Pkg: "main", Kind: ir.NodeKindNode}
	b := &ir.IRNode{Name: "B", Pkg: "main", Kind: ir.NodeKindNode}
	mainNode := &ir.IRNode{Name: "main", Pkg: "main", Kind: ir.NodeKindNode}
	mainPkg.Nodes = map[string]*ir.IRNode{"A": a, "B": b, "main": mainNode}

	// 单向 top-level edge: A -> B
	ek := ir.EdgeKey{Src: "A", Name: "e1", Dst: "B"}
	edge := &ir.IREdge{
		Src:  "A",
		Name: "e1",
		Dst:  "B",
		Kind: ir.EdgeSync,
		Flow: []ir.IRFlowOp{
			{Op: ir.FlowOpSend, Src: "A", SrcAttr: "x", Dst: "B", DstAttr: "y"},
		},
	}
	mainPkg.Edges = map[ir.EdgeKey]*ir.IREdge{ek: edge}
	mainPkg.Topology = &ir.IRTopology{
		VarInstances: map[string]string{"entry": "A"},
	}
	prog.AddPackage(mainPkg)

	d2 := Generate(prog, nil)

	// 应该是单向箭头
	if !strings.Contains(d2, `A -> B: "<e1>"`) {
		t.Errorf("expected unidirectional edge 'A -> B: \"<e1>\"', got:\n%s", d2)
	}
	// 不应该有双向箭头
	if strings.Contains(d2, "A <-> B") {
		t.Errorf("should NOT have bidirectional edge when flow is unidirectional, got:\n%s", d2)
	}
}
