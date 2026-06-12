package semantic

import (
	"fmt"
	"strings"

	"circle/internal/parser/ast"
)

// ──── 拓扑块校验 ────
//
// 简化版：去掉了 TopologyDecl，main 节点 body 里的 EdgeConnDecl 由 CheckMainBody 校验。
// 保留本文件是因为入口点（checker.go）仍需做跨包 edge 查找的辅助函数。

// CheckMainBody 校验 main 节点 body 里的内容
//
// 检查项：
//   - InstanceDecl 的 type 必须存在（本地或跨包）
//   - InstanceDecl 的 name 不重复
//   - EdgeConnDecl 的 src/dst 都必须对应一个已声明的 instance
//   - EdgeConnDecl 的 edge 必须能找到 top-level EdgeDecl（跨包用 CALL 表示）
func CheckMainBody(mainNode *ast.StructDecl, table *SymbolTable, allTables map[string]*SymbolTable) []SemanticError {
	var errs []SemanticError
	if mainNode == nil {
		return errs
	}

	// 收集 InstanceDecl（varMap: name → type）
	varMap := map[string]string{}
	seen := map[string]bool{}
	for _, m := range mainNode.Members {
		inst, ok := m.(*ast.InstanceDecl)
		if !ok {
			continue
		}
		// 检查重复
		if seen[inst.Name] {
			errs = append(errs, SemanticError{
				Pos:  inst.Pos(),
				Msg:  fmt.Sprintf("main: duplicate instance name %q", inst.Name),
				Hint: "use a different instance name",
			})
			continue
		}
		seen[inst.Name] = true

		// 检查 type 存在
		if !nodeExists(inst.Type, table, allTables) {
			errs = append(errs, SemanticError{
				Pos:  inst.Pos(),
				Msg:  fmt.Sprintf("main: instance %q declares unknown type %q", inst.Name, inst.Type),
				Hint: fmt.Sprintf("declare `@%s { ... }` in this file, or import it from another package", inst.Type),
			})
			continue
		}
		varMap[inst.Name] = inst.Type
	}

	// 校验 EdgeConnDecl
	for _, m := range mainNode.Members {
		conn, ok := m.(*ast.EdgeConnDecl)
		if !ok {
			continue
		}
		// src/dst 必须先声明
		if _, ok := varMap[conn.Src]; !ok {
			errs = append(errs, SemanticError{
				Pos:  conn.Pos(),
				Msg:  fmt.Sprintf("main: edge src instance %q not declared", conn.Src),
				Hint: fmt.Sprintf("add `var %s <type>;` before this edge", conn.Src),
			})
		}
		if _, ok := varMap[conn.Dst]; !ok {
			errs = append(errs, SemanticError{
				Pos:  conn.Pos(),
				Msg:  fmt.Sprintf("main: edge dst instance %q not declared", conn.Dst),
				Hint: fmt.Sprintf("add `var %s <type>;` before this edge", conn.Dst),
			})
		}

		// edge 必须在符号表里有对应 EdgeDecl（带 body）
		// 因为 var name 是局部的，这里用 src/dst 的 type 来查找
		srcType := varMap[conn.Src]
		dstType := varMap[conn.Dst]
		if srcType == "" || dstType == "" {
			continue
		}
		impl := table.GetEdge(srcType, conn.Edge, dstType)
		if impl == nil {
			errs = append(errs, SemanticError{
				Pos:  conn.Pos(),
				Msg:  fmt.Sprintf("main: edge %s <%s> %s has no matching edge declaration (with body)", srcType, conn.Edge, dstType),
				Hint: fmt.Sprintf("add `%s <%s> %s { body }` at top level", srcType, conn.Edge, dstType),
			})
		}
	}

	return errs
}

// IsReservedNode 判断是不是编译器内置保留节点
//
//   - SYSCALL / EXIT / ALLOC 等保留字出现在 edge Dst 时不需要 body
func IsReservedNode(name string) bool {
	switch name {
	case "SYSCALL", "EXIT", "ALLOC":
		return true
	}
	return false
}

// nodeExists 节点是否存在（本地 + 跨包）
//
// 支持两种 name 形式：
//   - 短名："hello" → 在本包 + 所有跨包表里查
//   - 全名："stdio.Println" → 拆 pkg + node，分别在跨包表里查
func nodeExists(name string, local *SymbolTable, all map[string]*SymbolTable) bool {
	// 短名：直接查
	if local != nil && local.GetNode(name) != nil {
		return true
	}

	// 跨包查找（短名：尝试每个 pkg）
	if all != nil {
		for pkgName, t := range all {
			if t.GetNode(name) != nil {
				return true
			}
			// 全名形式：尝试 pkgName + "." + name（针对不带 pkg 的输入）
			if t.GetNode(pkgName+"."+name) != nil {
				return true
			}
		}
	}

	// 全名形式："stdio.Println" → 拆 pkg + node
	if idx := strings.LastIndex(name, "."); idx > 0 {
		pkg := name[:idx]
		nodeName := name[idx+1:]
		if all != nil {
			if t, ok := all[pkg]; ok {
				if t.GetNode(nodeName) != nil {
					return true
				}
			}
		}
		// 本地表也试一遍（以防 pkgName 误填）
		if local != nil && local.GetNode(nodeName) != nil {
			return true
		}
	}

	return false
}
