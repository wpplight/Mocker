// Package semantic 类型推导 + 隐式初始化检查
//
// 解决 roadmap 里 #3 完整类型推导 + #5 隐式初始化检查
//
// 设计：
//   - NodeTypeEnv：每个节点的局部类型环境
//   - ResolveNodeBody：走 body 收集 local var 声明 + 推断类型
//   - InferExprType：推任意 Expr 的类型
//   - CheckNodeBody：use-before-decl 检查 + 表达式类型检查
//
// 重点支持：
//   - 字面量：str / num / bool
//   - ident：查 env
//   - member：node.attr 查符号表
//   - binary：+ / - / * / / 推断
//   - 拼接糖 msg+nl 必须是 str+str
package semantic

import (
	"fmt"
	"strings"

	"circle/internal/parser/ast"
)

// ──── 节点类型环境 ────

// NodeTypeEnv 节点内局部类型环境
//
// Locals：:=  或 =  声明的变量
// Outputs：name >> 的输出名（类型由 flow 推断，初始化时为 TypeUnknown）
type NodeTypeEnv struct {
	NodeName string
	Locals   map[string]Type
	Outputs  map[string]Type
}

func NewNodeTypeEnv(nodeName string) *NodeTypeEnv {
	return &NodeTypeEnv{
		NodeName: nodeName,
		Locals:   map[string]Type{},
		Outputs:  map[string]Type{},
	}
}

func (e *NodeTypeEnv) Lookup(name string) (Type, bool) {
	if t, ok := e.Locals[name]; ok {
		return t, true
	}
	if t, ok := e.Outputs[name]; ok {
		return t, true
	}
	return TypeUnknown, false
}

func (e *NodeTypeEnv) Set(name string, t Type) {
	e.Locals[name] = t
}

func (e *NodeTypeEnv) SetOutput(name string, t Type) {
	e.Outputs[name] = t
}

// ──── Body 解析：收集 decl + 推断类型 ────

// ResolveNodeBody 解析节点的 body：收集 local vars + 初始化类型
//
// 走 body 里所有 member：
//   - VarDecl (name := expr / type name = expr)：推 expr 类型，记入 Locals
//   - FlowDecl (name >>)：name 是个输出，初始 TypeUnknown
//   - FieldDecl (type name)：name 是个 typed 字段，存为 Local
//   - PortDecl (>> type name)：这是输入口，存为 Output（带类型）
//
// 注意：这个 pass 只收集 decl，**不**做 use-before-decl（那需要先 resolve 完）
// use-before-decl 在 CheckNodeBody 里做（可看到所有 decls 后再扫 expression）
func ResolveNodeBody(nodeName string, members []ast.StructMember) *NodeTypeEnv {
	env := &NodeTypeEnv{
		NodeName: nodeName,
		Locals:   map[string]Type{},
		Outputs:  map[string]Type{},
	}
	for _, m := range members {
		resolveMemberIntoEnv(m, env)
	}
	return env
}

func resolveMemberIntoEnv(m ast.StructMember, env *NodeTypeEnv) {
	switch s := m.(type) {
	case *ast.VarDecl:
		// 推断 Init 的类型
		t := InferExprType(s.Init, env)
		env.Set(s.Name, t)
		// 如果有 Flow（h := "hi" >>），这个 h 也是个 output
		if s.Flow != nil && len(s.Flow.Steps) > 0 {
			env.SetOutput(s.Name, t)
		}

	case *ast.FlowDecl:
		// h >> （裸 out）或 h >> a.b （out with chain）
		env.SetOutput(s.Head, TypeUnknown)

	case *ast.FieldDecl:
		// 显式 type name（无 Init）。type 在 FieldDecl.Type
		if s.Type != nil {
			t := resolveTypeRef(s.Type)
			env.Set(s.Name, t)
		}

	case *ast.PortDecl:
		// >> type name（输入口）
		if s.Type != nil {
			t := resolveTypeRef(s.Type)
			env.SetOutput(s.Name, t)
		}
	}
}

// ──── 表达式类型推导 ────

// InferExprType 推断任意 Expr 的类型
func InferExprType(e ast.Expr, env *NodeTypeEnv) Type {
	if e == nil {
		return TypeUnknown
	}
	switch ex := e.(type) {
	case *ast.LiteralExpr:
		return literalType(ex)

	case *ast.IdentExpr:
		if t, ok := env.Lookup(ex.Name); ok {
			return t
		}
		return TypeUnknown

	case *ast.MemberExpr:
		// 走 a.b
		// 先看 a 是不是 ident（常见情况：node.attr）
		if id, ok := ex.Obj.(*ast.IdentExpr); ok {
			// 优先从 env 查（locals / outputs）
			if t, ok := env.Lookup(id.Name + "." + ex.Name); ok {
				return t
			}
			// 再看是不是当前节点的 output
			if id.Name == env.NodeName {
				if t, ok := env.Lookup(ex.Name); ok {
					return t
				}
			}
			return TypeAny
		}
		return TypeAny

	case *ast.BinaryExpr:
		return inferBinaryType(ex, env)

	case *ast.UnaryExpr:
		return inferUnaryType(ex, env)

	case *ast.CallExpr:
		return TypeAny // MVP
	}
	return TypeUnknown
}

func literalType(l *ast.LiteralExpr) Type {
	switch l.Kind {
	case ast.LitString:
		return TypeStr
	case ast.LitNumber:
		return TypeNum
	case ast.LitBool:
		return TypeBool
	}
	return TypeUnknown
}

func inferBinaryType(b *ast.BinaryExpr, env *NodeTypeEnv) Type {
	lt := InferExprType(b.L, env)
	rt := InferExprType(b.R, env)

	if lt == TypeUnknown || rt == TypeUnknown {
		return TypeUnknown
	}

	switch b.Op {
	case "+":
		if lt == TypeStr && rt == TypeStr {
			return TypeStr
		}
		if lt == TypeNum && rt == TypeNum {
			return TypeNum
		}
		return TypeUnknown
	case "-", "*", "/", "%":
		if lt == TypeNum && rt == TypeNum {
			return TypeNum
		}
		return TypeUnknown
	case "==", "!=", "<", ">", "<=", ">=":
		return TypeBool
	case "&&", "||":
		return TypeBool
	}
	return TypeUnknown
}

func inferUnaryType(u *ast.UnaryExpr, env *NodeTypeEnv) Type {
	xt := InferExprType(u.X, env)
	switch u.Op {
	case "-":
		if xt == TypeNum {
			return TypeNum
		}
	case "!":
		if xt == TypeBool {
			return TypeBool
		}
	}
	return TypeUnknown
}

// ──── Body 检查：use-before-decl + 表达式类型错误 ────

// CheckNodeBody 检查节点的 body
//
// 跑两个检查：
//  1. 隐式初始化检查：每个 ident 引用都必须在使用前声明
//  2. 表达式类型检查：拼接糖 msg+nl 必须是 str+str 等
//
// 注意：env 必须在调用前已经 ResolveNodeBody 跑过一次
func CheckNodeBody(nodeName string, members []ast.StructMember, env *NodeTypeEnv) []SemanticError {
	var errs []SemanticError

	// 标记所有 declared 的名字
	declared := map[string]bool{}
	collectDeclaredNames(members, declared)

	// 走每个 member
	for _, m := range members {
		errs = append(errs, checkMember(m, env, declared)...)
	}
	return errs
}

// collectDeclaredNames 收集 body 里所有 declared 的名字
func collectDeclaredNames(members []ast.StructMember, out map[string]bool) {
	for _, m := range members {
		switch s := m.(type) {
		case *ast.VarDecl:
			out[s.Name] = true
		case *ast.FlowDecl:
			out[s.Head] = true
		case *ast.FieldDecl:
			out[s.Name] = true
		case *ast.PortDecl:
			out[s.Name] = true
		}
	}
}

// checkMember 查一个 member 里的所有 expression
func checkMember(m ast.StructMember, env *NodeTypeEnv, declared map[string]bool) []SemanticError {
	var errs []SemanticError

	switch s := m.(type) {
	case *ast.VarDecl:
		// 1. 查 Init
		errs = append(errs, checkExpr(s.Init, env, declared)...)
		// 2. 表达式类型检查（拼接糖等）
		errs = append(errs, checkExprType(s.Init, env)...)
		// 3. Flow chain
		if s.Flow != nil {
			for _, step := range s.Flow.Steps {
				errs = append(errs, checkFlowStep(step, env, declared)...)
			}
		}

	case *ast.FlowDecl:
		// Head 必须 declared（locals 或 outputs）
		for _, step := range s.Chain.Steps {
			errs = append(errs, checkFlowStep(step, env, declared)...)
		}
	}
	return errs
}

// checkFlowStep 检查一个 flow step
func checkFlowStep(step *ast.FlowStep, env *NodeTypeEnv, declared map[string]bool) []SemanticError {
	var errs []SemanticError

	switch st := step.Target.(type) {
	case *ast.FlowIdent:
		// a.b 或 a
		if len(st.Chain) == 0 {
			break
		}
		// 多 ident（如 `a.b`）：a 是节点名，b 是字段
		if len(st.Chain) >= 2 {
			nodeName := st.Chain[0]
			if !declared[nodeName] && !isNodeName(env, nodeName) {
				errs = append(errs, SemanticError{
					Pos:  step.Pos(),
					Msg:  fmt.Sprintf("flow step references undeclared node %q", nodeName),
					Hint: fmt.Sprintf("declare `@%s { ... }` or import it from another package", nodeName),
				})
			}
		}
		// 单 ident：检查是不是 declared（locals 或 outputs）
		// 但如果 chain 包含 "."（如 "stdio.Println.fid"），是跨包/跨节点引用，不算 undeclared
		if len(st.Chain) == 1 {
			name := st.Chain[0]
			if strings.Contains(name, ".") {
				break // 跨包引用，留给 edge body 检查
			}
			if !declared[name] {
				errs = append(errs, SemanticError{
					Pos:  step.Pos(),
					Msg:  fmt.Sprintf("flow step references undeclared name %q", name),
					Hint: fmt.Sprintf("declare with `%s := expr` or `%s >>` first", name, name),
				})
			}
		}

	case *ast.FlowExpr:
		// 拼接糖：递归查内部 expression
		errs = append(errs, checkExpr(st.Expr, env, declared)...)
		errs = append(errs, checkExprType(st.Expr, env)...)
	}
	return errs
}

// checkExpr 走 expression 找 ident 引用
func checkExpr(e ast.Expr, env *NodeTypeEnv, declared map[string]bool) []SemanticError {
	if e == nil {
		return nil
	}
	var errs []SemanticError
	switch ex := e.(type) {
	case *ast.IdentExpr:
		if !declared[ex.Name] {
			errs = append(errs, SemanticError{
				Pos:  e.Pos(),
				Msg:  fmt.Sprintf("use of undeclared name %q", ex.Name),
				Hint: fmt.Sprintf("declare with `%s := expr` or `type %s` first", ex.Name, ex.Name),
			})
		}
	case *ast.MemberExpr:
		if id, ok := ex.Obj.(*ast.IdentExpr); ok {
			if !declared[id.Name] && !isNodeName(env, id.Name) {
				errs = append(errs, SemanticError{
					Pos:  e.Pos(),
					Msg:  fmt.Sprintf("use of undeclared node %q", id.Name),
					Hint: fmt.Sprintf("declare `@%s { ... }` or import it from another package", id.Name),
				})
			}
		}
		errs = append(errs, checkExpr(ex.Obj, env, declared)...)
	case *ast.BinaryExpr:
		errs = append(errs, checkExpr(ex.L, env, declared)...)
		errs = append(errs, checkExpr(ex.R, env, declared)...)
	case *ast.UnaryExpr:
		errs = append(errs, checkExpr(ex.X, env, declared)...)
	case *ast.CallExpr:
		errs = append(errs, checkExpr(ex.Fn, env, declared)...)
		for _, arg := range ex.Args {
			errs = append(errs, checkExpr(arg, env, declared)...)
		}
	}
	return errs
}

// checkExprType 检查 expression 的类型是否合理
func checkExprType(e ast.Expr, env *NodeTypeEnv) []SemanticError {
	if e == nil {
		return nil
	}
	var errs []SemanticError

	switch ex := e.(type) {
	case *ast.BinaryExpr:
		lt := InferExprType(ex.L, env)
		rt := InferExprType(ex.R, env)

		if lt != TypeUnknown && rt != TypeUnknown && lt != TypeAny && rt != TypeAny {
			switch ex.Op {
			case "+":
				if !isCompatibleForAdd(lt, rt) {
					errs = append(errs, SemanticError{
						Pos:  e.Pos(),
						Msg:  fmt.Sprintf("cannot use %s + %s (types %s and %s are incompatible)", ex.L, ex.R, lt, rt),
						Hint: "both sides must be str (concatenation) or both num (addition)",
					})
				}
			case "-", "*", "/", "%":
				if lt != TypeNum || rt != TypeNum {
					errs = append(errs, SemanticError{
						Pos:  e.Pos(),
						Msg:  fmt.Sprintf("operator %q requires num operands, got %s and %s", ex.Op, lt, rt),
						Hint: "both sides must be num",
					})
				}
			}
		}

		errs = append(errs, checkExprType(ex.L, env)...)
		errs = append(errs, checkExprType(ex.R, env)...)

	case *ast.UnaryExpr:
		xt := InferExprType(ex.X, env)
		if xt != TypeUnknown && xt != TypeAny {
			switch ex.Op {
			case "-":
				if xt != TypeNum {
					errs = append(errs, SemanticError{
						Pos:  e.Pos(),
						Msg:  fmt.Sprintf("unary - requires num, got %s", xt),
						Hint: "operand must be num",
					})
				}
			case "!":
				if xt != TypeBool {
					errs = append(errs, SemanticError{
						Pos:  e.Pos(),
						Msg:  fmt.Sprintf("unary ! requires bool, got %s", xt),
						Hint: "operand must be bool",
					})
				}
			}
		}
		errs = append(errs, checkExprType(ex.X, env)...)

	case *ast.CallExpr:
		errs = append(errs, checkExprType(ex.Fn, env)...)
		for _, arg := range ex.Args {
			errs = append(errs, checkExprType(arg, env)...)
		}
	}
	return errs
}

// isCompatibleForAdd 算 + 的两边类型是否兼容
func isCompatibleForAdd(l, r Type) bool {
	if l == r {
		return l == TypeStr || l == TypeNum
	}
	return false
}

// isNodeName 检查 name 是不是 env 所在包的某个节点名
//
// MVP：暂时返回 true（让跨包的 node 不误报）
func isNodeName(env *NodeTypeEnv, name string) bool {
	return true
}
