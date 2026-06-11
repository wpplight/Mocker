// Package semantic 实现 Mocker 编译器的语义分析层（M3）。
//
// 职责：拿到 parser 产出的 AST，做
//   - 符号表建立（per-file / per-package）
//   - 跨包解析（import → 找到 .ce 文件）
//   - 类型检查（port 类型、edge body 点对点类型）
//   - 拓扑块校验（entry 三元组必须在 EdgeDecl 里）
//   - 入口点分析（package main + main{} 拓扑）
//   - goroutine 决策（edge body 含 fan-out → async → codegen 开 goroutine）
//
// 不做的（留给后续阶段）：
//   - IR 降级            → 归 M4
//   - Go 源码 emit        → 归 M4
//   - go build 编排      → 归 M6
//   - 跨包 import 实际加载 → MVP 不做（先单文件）
package semantic

import (
	"fmt"

	"circle/internal/parser/ast"
)

// ──── 类型系统（MVP 简化版）────
//
// 只支持内置基础类型（str / num / bool / byte / any）。
// 用户自定义类型（struct）暂归 any。
// 类型推断：str 字面量 → TypeStr；num 字面量 → TypeNum；
//           bool 字面量 → TypeBool；其他 → TypeUnknown（保守不报）。

// Type Mocker 类型的简化枚举
type Type int

const (
	TypeUnknown Type = iota // 推断不出（保守不报类型错）
	TypeStr                 // 字符串
	TypeNum                 // 数字
	TypeBool                // 布尔
	TypeByte                // 字节
	TypeAny                 // 任意
)

func (t Type) String() string {
	switch t {
	case TypeStr:
		return "str"
	case TypeNum:
		return "num"
	case TypeBool:
		return "bool"
	case TypeByte:
		return "byte"
	case TypeAny:
		return "any"
	default:
		return "?"
	}
}

// ──── 语义错误 ────

// SemanticError 语义检查错误
//
// 收集所有错误后一次性返回（不 fail-fast），方便用户/AI 一次看完
type SemanticError struct {
	Pos  ast.Pos // 错误位置（来自 AST 节点）
	Msg  string  // 人类可读的错误描述
	Hint string  // 可选的修复建议
}

func (e SemanticError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("semantic error at line %d col %d: %s (hint: %s)",
			e.Pos.Line, e.Pos.Col, e.Msg, e.Hint)
	}
	return fmt.Sprintf("semantic error at line %d col %d: %s",
		e.Pos.Line, e.Pos.Col, e.Msg)
}

// ──── 通用 helpers ────

// resolveTypeRef 把 AST 的 TypeRef 解析成简化的 Type
//
// MVP 只处理：
//   - str / num / bool / byte / any 内置类型
//   - 用户自定义标识符（归 TypeAny）
//   - *T / T[] 暂归 TypeAny
func resolveTypeRef(ref ast.TypeRef) Type {
	if ref == nil {
		return TypeUnknown
	}
	switch t := ref.(type) {
	case *ast.TypeName:
		switch t.Name {
		case "str":
			return TypeStr
		case "num":
			return TypeNum
		case "bool":
			return TypeBool
		case "byte":
			return TypeByte
		case "any":
			return TypeAny
		default:
			// 用户自定义类型（struct）→ 保守归 Any
			return TypeAny
		}
	case *ast.TypeArray:
		return TypeAny // 数组暂不细化
	case *ast.TypePtr:
		return TypeAny // 指针暂不细化
	}
	return TypeUnknown
}
