package semantic

import (
	"fmt"

	"circle/internal/parser/ast"
)

// ──── 拓扑块校验 ────
//
// 拓扑块的语义（用户拍板）：
//   - 块名 == 当前包名（dispatcher 已确保）
//   - body 里的每条 entry (src <edge> dst) 是对 top-level EdgeDecl 的**引用**
//   - **必须能找到对应 EdgeDecl**（带 body 的实现）—— 否则语义无效
//   - 反过来：top-level 定义的 EdgeDecl 如果没在拓扑块里列出，**只是警告**
//     （未导出的内部边，私有实现，无需外部访问）
//
// 拓扑块在 IR 阶段是"程序启动时建哪些边"的依据，
// 这里保证**结构和实现一致**。

// CheckTopology 校验拓扑块里的每条 entry 都能找到对应 EdgeDecl
//
// 单包版（向后兼容）：只查本包符号表
func CheckTopology(topo *ast.TopologyDecl, table *SymbolTable) []SemanticError {
	return CheckTopologyCross(topo, table, nil)
}

// CheckTopologyCross 跨包版：跨包节点也能找到
func CheckTopologyCross(topo *ast.TopologyDecl, table *SymbolTable, allTables map[string]*SymbolTable) []SemanticError {
	var errs []SemanticError
	seen := map[EdgeKey]bool{}

	for _, entry := range topo.Edges {
		key := EdgeKey{Src: entry.Src, Edge: entry.Edge, Dst: entry.Dst}

		// 1. 同一拓扑块里不允许重复 entry
		if seen[key] {
			errs = append(errs, SemanticError{
				Pos:  entry.Pos(),
				Msg:  fmt.Sprintf("topology %s: duplicate entry %s", topo.Name, key),
				Hint: "remove the duplicate line",
			})
			continue
		}
		seen[key] = true

		// 2. src / dst 节点必须存在（本地或跨包）
		if !nodeExists(entry.Src, table, allTables) {
			errs = append(errs, SemanticError{
				Pos:  entry.Pos(),
				Msg:  fmt.Sprintf("topology %s: source node %q not found", topo.Name, entry.Src),
				Hint: fmt.Sprintf("declare `@%s { ... }` in this file, or import it from another package", entry.Src),
			})
		}
		if !nodeExists(entry.Dst, table, allTables) {
			errs = append(errs, SemanticError{
				Pos:  entry.Pos(),
				Msg:  fmt.Sprintf("topology %s: dest node %q not found", topo.Name, entry.Dst),
				Hint: fmt.Sprintf("declare `@%s { ... }` in this file, or import it from another package", entry.Dst),
			})
		}

		// 3. 拓扑 entry 必须能找到对应 EdgeDecl（带 body 的实现）
		// 例外：entry 本身带 body（inline edge，一次性使用边）→ 跳过
		// 例外：src/dst 是保留节点（SYSCALL/EXIT/ALLOC）→ 编译器内置，跳过
		if len(entry.Body) == 0 && !IsReservedNode(entry.Dst) {
			impl := table.GetEdge(entry.Src, entry.Edge, entry.Dst)
			if impl == nil {
				errs = append(errs, SemanticError{
					Pos:  entry.Pos(),
					Msg:  fmt.Sprintf("topology %s: entry %s has no matching edge declaration (with body)", topo.Name, key),
					Hint: fmt.Sprintf("add `%s { body }` at top level (defines what flows inside the edge)", key),
				})
				continue
			}
		}

		// 4. EdgeDecl 必须是 exported 的（如果跨包用）
		//    MVP 暂不强制：单文件下都 OK
	}
	return errs
}

// nodeExists 节点存在性检查（本地或跨包）
//
// SYSCALL / EXIT / ALLOC 等是编译器内置保留节点（保留字），
// 不在 .ce 文件里，但语义层要认。
func nodeExists(name string, localTable *SymbolTable, allTables map[string]*SymbolTable) bool {
	if localTable.GetNode(name) != nil {
		return true
	}
	if IsReservedNode(name) {
		return true
	}
	if allTables == nil {
		return false
	}
	if _, pkg := crossLookupNode(name, localTable, allTables); pkg != "" {
		return true
	}
	return false
}

// IsReservedNode 检查是否是编译器内置保留节点
//
// SYSCALL 等保留字不需要在 .ce 文件里声明，编译器自带 emit 模板。
func IsReservedNode(name string) bool {
	switch name {
	case "SYSCALL", "EXIT", "ALLOC":
		return true
	}
	return false
}

// CheckEdgesForTopology 检查 top-level 定义的 EdgeDecl 哪些没出现在拓扑块里
//
// 用途：在语义分析结束时，输出"未导出的内部边"警告
//
// MVP：只数，不报（让上层决定要不要 warn）
func CheckEdgesForTopology(topo *ast.TopologyDecl, table *SymbolTable) []EdgeKey {
	var orphans []EdgeKey
	for key := range table.Edges {
		found := false
		for _, entry := range topo.Edges {
			if entry.Src == key.Src && entry.Edge == key.Edge && entry.Dst == key.Dst {
				found = true
				break
			}
		}
		if !found {
			orphans = append(orphans, key)
		}
	}
	return orphans
}
