package semantic

import (
	"fmt"
	"sort"
	"strings"

	"circle/internal/parser/ast"
)

// ──── Edge body 分析 ────
//
// 关键设计（用户拍板）：
//   - 边 body 含 FlowFanout（>> >>）→ 边是 async，
//     **每条分支一个 goroutine**
//   - 边 body 只有 FlowStmt / FlowCont（普通 >>）→ 边是 sync，
//     **像函数调用，开新 goroutine 没必要**
//
// goroutine 决策权在边，不在节点，不在拓扑块。
// 这条信息会传给后续 IR/codegen 阶段决定 emit 形式。

// EdgeKind 边的运行时形态（sync / async）
type EdgeKind int

const (
	EdgeSync  EdgeKind = iota // 同步，函数调用形式
	EdgeAsync                 // 异步，每分支一个 goroutine
)

func (k EdgeKind) String() string {
	switch k {
	case EdgeSync:
		return "sync"
	case EdgeAsync:
		return "async"
	}
	return "?"
}

// ClassifyEdge 根据 body 内容判断 EdgeKind
//
// 规则：body 里只要有 FlowFanout → async；否则 → sync
// 链式（>>）和续行（FlowCont）都不触发 goroutine
func ClassifyEdge(edge *ast.EdgeDecl) EdgeKind {
	for _, stmt := range edge.Body {
		if _, ok := stmt.(*ast.FlowFanout); ok {
			return EdgeAsync
		}
	}
	return EdgeSync
}

// AsyncBranchCount 数 edge body 里的 FlowFanout 分支总数
//
//	e.g. body = { msg >> ; >>dst1 ; >>dst2 }  → 2 个分支 → 2 个 goroutine
//	e.g. body = { msg >> dst }                → 0（不是 fanout）→ 0 个
//
// 给 codegen 用：emit 时开 N 个 goroutine
func AsyncBranchCount(edge *ast.EdgeDecl) int {
	n := 0
	for _, stmt := range edge.Body {
		if ff, ok := stmt.(*ast.FlowFanout); ok {
			n += len(ff.Branches)
		}
	}
	return n
}

// ──── Edge body 类型检查 ────

// CheckEdgeBody 校验边的 body 里点对点连线的类型
//
// MVP 检查：
//  1. FlowStmt 里的每步 target 是否是已知节点
//  2. FlowStmt 的 src.port 和 dst.port 类型是否一致
//  3. FlowFanout 的 src 和各分支的 target 类型
func CheckEdgeBody(edge *ast.EdgeDecl, table *SymbolTable) []SemanticError {
	var errs []SemanticError
	for _, stmt := range edge.Body {
		switch s := stmt.(type) {
		case *ast.FlowStmt:
			errs = append(errs, checkFlowStmt(s, edge, table)...)
		case *ast.FlowCont:
			// FlowCont 是 FlowStmt 的续行形式，单独不会出现在边 body 里
			//（边 body 是 stmt 列表）。保守跳过。
			_ = s
		case *ast.FlowFanout:
			errs = append(errs, checkFlowFanout(s, edge, table)...)
		}
	}
	return errs
}

// checkFlowStmt 校验链式 FlowStmt
//
// 边 body 的 FlowStmt 约定：2 步，`src.x >> dst.y`。
//   - 第 1 步在 src 节点的 in/out 里
//   - 第 2 步在 dst 节点的 in/out 里
//   - 两步的 in/out 类型应该一致
func checkFlowStmt(stmt *ast.FlowStmt, edge *ast.EdgeDecl, table *SymbolTable) []SemanticError {
	var errs []SemanticError
	if len(stmt.Steps) < 2 {
		errs = append(errs, SemanticError{
			Pos:  stmt.Pos(),
			Msg:  fmt.Sprintf("edge %s body has incomplete flow chain (%d steps)", edgeKey(edge), len(stmt.Steps)),
			Hint: "edge body flow must be `src.x >> dst.y` (at least 2 steps)",
		})
		return errs
	}

	src := edge.Src
	dst := edge.Dst

	first := stmt.Steps[0]
	last := stmt.Steps[len(stmt.Steps)-1]

	// 拆 "say.hey" → ("say", "hey")
	firstNode, firstAttr, firstOK := extractPortName(first.Target)
	lastNode, lastAttr, lastOK := extractPortName(last.Target)
	if !firstOK || !lastOK {
		// 不是 in/out 引用（跨包 call / string literal / 二元 op 等）
		// MVP 暂不深入检查，靠后续 IR 阶段处理
		return errs
	}

	// ① 校验 firstNode（如果有前缀）== src，firstAttr 在 src 节点的 in/out 列表里
	if firstNode != "" && firstNode != src {
		errs = append(errs, SemanticError{
			Pos:  first.Target.Pos(),
			Msg:  fmt.Sprintf("edge %s body: first step node %q doesn't match src %q", edgeKey(edge), firstNode, src),
			Hint: fmt.Sprintf("use %q (or just %q if same node)", src+"."+firstAttr, firstAttr),
		})
	} else if firstAttr != "" {
		if _, _, ok := table.GetExport(src, firstAttr); !ok {
			errs = append(errs, SemanticError{
				Pos:  first.Target.Pos(),
				Msg:  fmt.Sprintf("edge %s body: %q has no in/out on src node %q", edgeKey(edge), firstAttr, src),
				Hint: fmt.Sprintf("declare `>> type %s` (in) or `%s >>` (out) in %s", firstAttr, firstAttr, src),
			})
		}
	}

	// ② 校验 lastNode（如果有前缀）== dst，lastAttr 在 dst 节点的 in/out 列表里
	if lastNode != "" && lastNode != dst {
		errs = append(errs, SemanticError{
			Pos:  last.Target.Pos(),
			Msg:  fmt.Sprintf("edge %s body: last step node %q doesn't match dst %q", edgeKey(edge), lastNode, dst),
			Hint: fmt.Sprintf("use %q (or just %q if same node)", dst+"."+lastAttr, lastAttr),
		})
	} else if lastAttr != "" {
		if _, _, ok := table.GetExport(dst, lastAttr); !ok {
			errs = append(errs, SemanticError{
				Pos:  last.Target.Pos(),
				Msg:  fmt.Sprintf("edge %s body: %q has no in/out on dst node %q", edgeKey(edge), lastAttr, dst),
				Hint: fmt.Sprintf("declare `>> type %s` (in) or `%s >>` (out) in %s", lastAttr, lastAttr, dst),
			})
		}
	}

	// ③ 校验 firstAttr 和 lastAttr 的类型一致（允许 TypeUnknown）
	srcType := table.LookupExportType(src, firstAttr)
	dstType := table.LookupExportType(dst, lastAttr)
	if srcType != TypeUnknown && dstType != TypeUnknown && srcType != dstType {
		errs = append(errs, SemanticError{
			Pos: first.Target.Pos(),
			Msg: fmt.Sprintf("edge %s body: type mismatch %s.%s (%s) → %s.%s (%s)",
				edgeKey(edge), src, firstAttr, srcType, dst, lastAttr, dstType),
			Hint: "in/out types must match for direct connection",
		})
	}

	return errs
}

// checkFlowFanout 校验 fan-out FlowFanout
//
// 校验 src 的 type 和各分支 target 的 type
func checkFlowFanout(ff *ast.FlowFanout, edge *ast.EdgeDecl, table *SymbolTable) []SemanticError {
	var errs []SemanticError
	src := edge.Src
	dst := edge.Dst

	// src 是 FlowTarget
	_, srcPort, _ := extractPortName(ff.Src)
	srcType := table.LookupExportType(src, srcPort)

	// 各分支
	for i, br := range ff.Branches {
		if len(br.Steps) == 0 {
			continue
		}
		// 分支的 target 应该是 dst 的某个 in/out
		branchTarget := br.Steps[len(br.Steps)-1].Target
		_, branchPort, branchOK := extractPortName(branchTarget)
		if !branchOK {
			continue
		}

		// 校验 in/out 存在
		if _, _, ok := table.GetExport(dst, branchPort); !ok {
			errs = append(errs, SemanticError{
				Pos: branchTarget.Pos(),
				Msg: fmt.Sprintf("edge %s fanout branch[%d]: %q has no in/out on node %q",
					edgeKey(edge), i, branchPort, dst),
				Hint: fmt.Sprintf("declare `>> type %s` (in) or `%s >>` (out) in %s", branchPort, branchPort, dst),
			})
			continue
		}

		// 校验类型
		branchType := table.LookupExportType(dst, branchPort)
		if srcType != TypeUnknown && branchType != TypeUnknown && srcType != branchType {
			errs = append(errs, SemanticError{
				Pos: branchTarget.Pos(),
				Msg: fmt.Sprintf("edge %s fanout branch[%d]: type mismatch %s.%s (%s) → %s.%s (%s)",
					edgeKey(edge), i, src, srcPort, srcType, dst, branchPort, branchType),
				Hint: "fanout branches must accept same type as source",
			})
		}
	}

	return errs
}

// ──── Helpers ────

// edgeKey 把 EdgeDecl 简化成 "src <edge> dst" 字符串（错误信息用）
func edgeKey(e *ast.EdgeDecl) string {
	return fmt.Sprintf("%s <%s> %s", e.Src, e.Edge, e.Dst)
}

// InferEdgeEndpoints Style 2 语法糖：从 body 推导 edge 的 src/dst
//
// 调用时机：在解析完 EdgeDecl 之后，跑语义检查之前
//
// 规则：
//   - Style1：edge.Src 和 edge.Dst 都已显式指定 → 不动
//   - Style2：edge.Src 或 edge.Dst 为空 → 扫描 body 推导
//   - FlowStmt / FlowCont：first step 的前缀 = src, last step 的前缀 = dst
//   - FlowFanout：src 已知（FlowFanout.Src 的前缀），但多个分支 → 不支持 Style2，报错
//
// 限制（违反时返回 SemanticError）：
//   - body 必须有唯一 src 和唯一 dst
//   - 不支持 fanout（多个 dst）
//   - 不支持 multi-source（多个 src）
//
// 推导成功后填回 edge.Src / edge.Dst
func InferEdgeEndpoints(edge *ast.EdgeDecl) []SemanticError {
	// Style1 已经显式，不需要推导
	if edge.Src != "" && edge.Dst != "" {
		return nil
	}

	srcs := map[string]bool{}
	dsts := map[string]bool{}

	for _, stmt := range edge.Body {
		switch s := stmt.(type) {
		case *ast.FlowStmt:
			if len(s.Steps) < 2 {
				continue
			}
			// first step → src; last step → dst
			first := s.Steps[0].Target
			last := s.Steps[len(s.Steps)-1].Target

			if srcPrefix, ok := extractNodePrefix(first); ok {
				srcs[srcPrefix] = true
			}
			if dstPrefix, ok := extractNodePrefix(last); ok {
				dsts[dstPrefix] = true
			}

		case *ast.FlowCont:
			if len(s.Steps) == 0 {
				continue
			}
			// FlowCont 的 src 来自前面的 stmt，这里只取 dst
			last := s.Steps[len(s.Steps)-1].Target
			if dstPrefix, ok := extractNodePrefix(last); ok {
				dsts[dstPrefix] = true
			}

		case *ast.FlowFanout:
			// Fanout 不支持 Style2（多个 dst）
			return []SemanticError{{
				Pos:  s.Pos(),
				Msg:  fmt.Sprintf("edge %s Style 2 语法糖不支持 fan-out（多个 dst）", edgeNameForSugar(edge)),
				Hint: "用 Style 1: src <edge> dst { ... } 显式指明 src/dst",
			}}
		}
	}

	// 校验：单一 src + 单一 dst
	if len(srcs) == 0 {
		return []SemanticError{{
			Pos:  edge.Pos(),
			Msg:  fmt.Sprintf("edge %s Style 2 推导失败：body 里没有 src", edgeNameForSugar(edge)),
			Hint: "用 Style 1: src <edge> dst { ... } 显式指明 src",
		}}
	}
	if len(dsts) == 0 {
		return []SemanticError{{
			Pos:  edge.Pos(),
			Msg:  fmt.Sprintf("edge %s Style 2 推导失败：body 里没有 dst", edgeNameForSugar(edge)),
			Hint: "用 Style 1: src <edge> dst { ... } 显式指明 dst",
		}}
	}
	if len(srcs) > 1 {
		return []SemanticError{{
			Pos:  edge.Pos(),
			Msg:  fmt.Sprintf("edge %s Style 2 推导失败：body 里有多个 src %v", edgeNameForSugar(edge), setToSortedSlice(srcs)),
			Hint: "用 Style 1: src <edge> dst { ... } 显式指明 src",
		}}
	}
	if len(dsts) > 1 {
		return []SemanticError{{
			Pos:  edge.Pos(),
			Msg:  fmt.Sprintf("edge %s Style 2 推导失败：body 里有多个 dst %v", edgeNameForSugar(edge), setToSortedSlice(dsts)),
			Hint: "用 Style 1: src <edge> dst { ... } 显式指明 dst",
		}}
	}

	// 填回去
	for s := range srcs {
		edge.Src = s
		break
	}
	for d := range dsts {
		edge.Dst = d
		break
	}
	return nil
}

// edgeNameForSugar Style 2 错误信息用：<edge>（src/dst 未知）
func edgeNameForSugar(e *ast.EdgeDecl) string {
	if e.Src == "" && e.Dst == "" {
		return fmt.Sprintf("<%s>", e.Edge)
	}
	return fmt.Sprintf("%s <%s> %s", e.Src, e.Edge, e.Dst)
}

// extractNodePrefix 从 FlowTarget 提取节点前缀（"."之前）
//
// 例：Println.fid → "Println"; io.write.fid → "io.write"; fid → ""
func extractNodePrefix(t ast.FlowTarget) (string, bool) {
	ident, isIdent := t.(*ast.FlowIdent)
	if !isIdent || len(ident.Chain) == 0 {
		return "", false
	}
	last := ident.Chain[len(ident.Chain)-1]
	if idx := strings.LastIndex(last, "."); idx >= 0 {
		return last[:idx], true
	}
	// 单 ident 无 "."，当 src/dst 都需要上下文，标记为不可推导
	return "", false
}

// setToSortedSlice map → sorted []string（debug 用）
func setToSortedSlice(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// extractPortName 从 FlowTarget 提取出 (nodeName, portName)
//
// lexer 把 "node.port" 合并成单个 CALL token，所以 FlowIdent.Chain
// 可能是 ["node.port"]（单元素）而不是 ["node", "port"]（两元素）。
// 这里按最后一个 '.' 切开，得到 (nodeName, portName)。
//
// 返回：
//   - "say.hey"   → ("say",  "hey",  true)
//   - "hey"       → ("",     "hey",  true)
//   - FlowLiteral  → ("",     "",     false)
func extractPortName(t ast.FlowTarget) (nodeName, portName string, ok bool) {
	ident, isIdent := t.(*ast.FlowIdent)
	if !isIdent || len(ident.Chain) == 0 {
		return "", "", false
	}
	last := ident.Chain[len(ident.Chain)-1]
	if idx := strings.LastIndex(last, "."); idx >= 0 {
		return last[:idx], last[idx+1:], true
	}
	// 单 ident（无 '.'），当 port 名查
	return "", last, true
}

// portNameOnly 便捷：只取 port 名（忽略 node 前缀）
func portNameOnly(t ast.FlowTarget) (string, bool) {
	_, p, ok := extractPortName(t)
	return p, ok
}

// ptypeName 用来在 hint 里展示（p 是 GetInput 返回的，nil 也安全）
func ptypeName(p *InputSymbol) string {
	if p == nil {
		return "type"
	}
	return p.Type.String()
}

// 避免 lint 警告
var _ = strings.HasPrefix
