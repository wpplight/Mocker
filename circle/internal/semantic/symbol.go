package semantic

import (
	"fmt"

	"circle/internal/parser/ast"
)

// ──── 符号 ────

// NodeSymbol 一个节点（@name 或 name）的符号信息
//
// 节点 = 1 个"内含 n 个 block 的图"（用户拍板的 block 模型）
// 每个 block 有 a 个入度（>> type name）和 b 个出度（name >>）
// "node.x" 的外部访问 = 某个 block 的入度（Input）或出度（Output）
//
// 术语说明（按用户拍板）：
//   - **入度（Input）** = 数据流进 block 的入口，有类型（>> type name）
//   - **出度（Output）** = block 计算出往外送的值，没显式类型（name >>）
//   - **没有"port"这个概念** —— 只用图论里的入度/出度
type NodeSymbol struct {
	Name     string         // 节点名
	Exported bool           // 是否带 @ 前缀（包外可见）
	Kind     ast.StructKind // Plain / Node / Edge
	Inputs   []*InputSymbol // 入度列表（>> type name）— block 的 a 个入度
	Outputs  []string       // 出度列表（name >> 之类）— block 的 b 个出度
	Pos      ast.Pos        // 节点声明位置（用于错误信息）
}

// InputSymbol 一个入度（>> type name 声明）的符号信息
type InputSymbol struct {
	Name string
	Type Type
	Pos  ast.Pos
}

// FlowSymbol 一个出度的简化表示（MVP 不带类型，靠使用点推断）
type FlowSymbol struct {
	Name string
	Pos  ast.Pos
}

// EdgeKey 边在符号表里的 key：三元组 (src, edge, dst)
type EdgeKey struct {
	Src  string
	Edge string
	Dst  string
}

// String 方便错误信息用
func (k EdgeKey) String() string {
	return fmt.Sprintf("%s <%s> %s", k.Src, k.Edge, k.Dst)
}

// ──── 符号表 ────

// SymbolTable 一个文件（或一个包）的符号表
//
// MVP 范围：单文件符号表。
// 跨包符号表（按 import 加载 .ce 文件）后续阶段再做。
type SymbolTable struct {
	PkgName string

	// 节点符号
	Nodes map[string]*NodeSymbol

	// 边符号（按 EdgeKey 索引）
	Edges map[EdgeKey]*ast.EdgeDecl
}

// NewSymbolTable 构造一个空符号表
func NewSymbolTable(pkgName string) *SymbolTable {
	return &SymbolTable{
		PkgName: pkgName,
		Nodes:   map[string]*NodeSymbol{},
		Edges:   map[EdgeKey]*ast.EdgeDecl{},
	}
}

// ──── 符号表建立（Resolver）────

// ResolveFile 从一个 AST 文件构建符号表
//
// 返回：符号表 + 错误列表（不 fail-fast）
//   - 节点 / 边 / 拓扑块都收进符号表
//   - 重复定义 / 拼写错误等会在此阶段被抓出
func ResolveFile(file *ast.File) (*SymbolTable, []SemanticError) {
	if file == nil || file.Pkg == nil {
		return NewSymbolTable(""), []SemanticError{{
			Pos: ast.Pos{Line: 1, Col: 1},
			Msg: "missing package declaration",
		}}
	}
	table := NewSymbolTable(file.Pkg.Name)
	var errs []SemanticError

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.StructDecl:
			if err := table.addNode(d); err != nil {
				errs = append(errs, *err)
			}
		case *ast.EdgeDecl:
			table.addEdge(d)
			// 简化：没有 TopologyDecl，main body 由 CheckMainBody 单独校验
		}
	}

	return table, errs
}

// addNode 把一个 StructDecl 加进符号表
// 返回非 nil 表示有错（重复定义等）
func (t *SymbolTable) addNode(d *ast.StructDecl) *SemanticError {
	if d.Name == "" {
		return nil // 防御性
	}
	if _, exists := t.Nodes[d.Name]; exists {
		return &SemanticError{
			Pos:  d.Pos(),
			Msg:  fmt.Sprintf("duplicate definition: %s", d.Name),
			Hint: fmt.Sprintf("each node/struct must have a unique name; previous %s was defined elsewhere", d.Name),
		}
	}

	ns := &NodeSymbol{
		Name:     d.Name,
		Exported: d.Exported,
		Kind:     d.Kind,
		Inputs:   []*InputSymbol{},
		Outputs:  []string{},
		Pos:      d.Pos(),
	}

	// 扫描 members：
	//   - PortDecl (>> type name)        → ns.Inputs（block 入度）
	//   - FlowDecl (name >> / name >> target) → ns.Outputs（block 出度）
	//   - VarDecl  (name := value)        → 局部变量，不入符号表（外部不可访问）
	//   - FieldDecl (type name)            → MVP 不入符号表
	seenOutput := map[string]bool{} // Outputs 内去重
	for _, m := range d.Members {
		switch mm := m.(type) {
		case *ast.PortDecl:
			ns.Inputs = append(ns.Inputs, &InputSymbol{
				Name: mm.Name,
				Type: resolveTypeRef(mm.Type),
				Pos:  mm.Pos(),
			})
		case *ast.FlowDecl:
			// 出符号：name >>  →  这个 name 是节点的"出属性"
			if !seenOutput[mm.Head] {
				seenOutput[mm.Head] = true
				ns.Outputs = append(ns.Outputs, mm.Head)
			}
		}
		// VarDecl / FieldDecl 暂不入符号表
	}

	t.Nodes[d.Name] = ns
	return nil
}

// addEdge 把一个 EdgeDecl 加进符号表
func (t *SymbolTable) addEdge(d *ast.EdgeDecl) {
	key := EdgeKey{Src: d.Src, Edge: d.Edge, Dst: d.Dst}
	// 重复定义？后定义的覆盖前者（MVP 简化），更严谨应该报错
	t.Edges[key] = d
}

// ──── 符号表查询 ────

// GetNode 查节点，未找到返回 nil
func (t *SymbolTable) GetNode(name string) *NodeSymbol {
	return t.Nodes[name]
}

// GetInput 在某节点上查入度（>> type name 声明的），未找到返回 nil
func (t *SymbolTable) GetInput(nodeName, inputName string) *InputSymbol {
	ns := t.Nodes[nodeName]
	if ns == nil {
		return nil
	}
	for _, p := range ns.Inputs {
		if p.Name == inputName {
			return p
		}
	}
	return nil
}

// HasOutput 查某节点是否声明了某个出度（FlowDecl.Head）
func (t *SymbolTable) HasOutput(nodeName, outputName string) bool {
	ns := t.Nodes[nodeName]
	if ns == nil {
		return false
	}
	for _, o := range ns.Outputs {
		if o == outputName {
			return true
		}
	}
	return false
}

// GetExport 外部访问 `node.x` 时的查表
//
// "node.x" 的语义（用户拍板）：访问 x 这个名字，可能是
//   - 某个 block 的入度（Input，>> type name 声明）→ 读入度接收到的值
//   - 某个 block 的出度（Output，name >> 声明）→ 读 block 计算出的值
//
// 两者任一命中即合法。
//
// 顺序：先查 output 再查 input（passthrough 同名时视为 output，可当 src 读）
func (t *SymbolTable) GetExport(nodeName, attrName string) (in *InputSymbol, isOutput bool, found bool) {
	ns := t.Nodes[nodeName]
	if ns == nil {
		return nil, false, false
	}
	if t.HasOutput(nodeName, attrName) {
		return nil, true, true
	}
	if p := t.GetInput(nodeName, attrName); p != nil {
		return p, false, true
	}
	return nil, false, false
}

// GetEdge 查边（按三元组），未找到返回 nil
func (t *SymbolTable) GetEdge(src, edge, dst string) *ast.EdgeDecl {
	return t.Edges[EdgeKey{Src: src, Edge: edge, Dst: dst}]
}

// LookupInputType 便捷方法：查 (node, input) 的类型；找不到返 TypeUnknown
// 只查 Input，不查 Output
func (t *SymbolTable) LookupInputType(nodeName, inputName string) Type {
	p := t.GetInput(nodeName, inputName)
	if p == nil {
		return TypeUnknown
	}
	return p.Type
}

// LookupExportType 查 `node.x` 的类型
// 优先 Input 的类型；Output 没有声明类型，返回 TypeUnknown
func (t *SymbolTable) LookupExportType(nodeName, attrName string) Type {
	in, _, ok := t.GetExport(nodeName, attrName)
	if !ok {
		return TypeUnknown
	}
	if in != nil {
		return in.Type
	}
	return TypeUnknown
}

// AllNodeNames 返回所有节点名（调试 / dump 用）
func (t *SymbolTable) AllNodeNames() []string {
	names := make([]string, 0, len(t.Nodes))
	for n := range t.Nodes {
		names = append(names, n)
	}
	return names
}

// String 符号表的文本 dump（debug 用）
func (t *SymbolTable) String() string {
	s := fmt.Sprintf("SymbolTable pkg=%s, %d nodes, %d edges\n",
		t.PkgName, len(t.Nodes), len(t.Edges))
	for name, ns := range t.Nodes {
		exp := ""
		if ns.Exported {
			exp = "@"
		}
		s += fmt.Sprintf("  %snode %s%s {\n", exp, name, kindName(ns.Kind))
		for _, p := range ns.Inputs {
			s += fmt.Sprintf("    >> %s %s   (in)\n", p.Type, p.Name)
		}
		for _, o := range ns.Outputs {
			s += fmt.Sprintf("    %s >>        (out)\n", o)
		}
		s += "  }\n"
	}
	for key, e := range t.Edges {
		async := ""
		if ClassifyEdge(e) == EdgeAsync {
			async = " (async/goroutine)"
		} else {
			async = " (sync)"
		}
		s += fmt.Sprintf("  edge %s%s → body %d stmts\n", key, async, len(e.Body))
	}
	return s
}

func kindName(k ast.StructKind) string {
	switch k {
	case ast.StructKindPlain:
		return "struct"
	case ast.StructKindNode:
		return ""
	case ast.StructKindEdge:
		return "edge"
	}
	return "?"
}
