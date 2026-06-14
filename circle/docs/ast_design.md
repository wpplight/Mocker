# Mocker AST 构造设计

> 从 `mocker_lex.Tokenize` 的 token 流到一棵强类型的 AST
>
> 配套：[main.ebnf](../main.ebnf) (语法) / [lexer.go](../mocker_lex/lexer.go) (词法)

---

## 一、概述

### 1.1 Parser 在编译器中的位置

```
源 .mocker 文件
      ↓
┌──────────────┐
│ mocker_lex   │   ← 已完成：字符 → []Token
│   Tokenize   │
└──────┬───────┘
       ↓ []mocker_lex.Token
┌──────────────┐
│   parser     │   ← 本文档：Token → AST
│              │
└──────┬───────┘
       ↓ *ast.File
┌──────────────┐
│  semantic    │   ← 后续：类型检查 / 引用解析
└──────┬───────┘
       ↓
┌──────────────┐
│     IR       │   ← 后续：规范化（codegen 友好形式）
└──────┬───────┘
       ↓
┌──────────────┐
│   codegen    │   ← 后续：emit Go 源码
└──────────────┘
```

### 1.2 Parser 的一句话职责

> **按 EBNF 规则，把扁平的 `[]Token` 组装成一棵有类型的树（AST），丢掉语法噪音（缩进、注释、空格、可选分号），保留语义结构（声明、字段、表达式、数据流、图连接）。**

### 1.3 关键技术选型

| 选型 | 理由 |
| --- | --- |
| **手写递归下降** | 错误信息好、易扩展、自举友好 |
| **1 token lookahead** | Mocker 语法不太歧义，1 token 足够 |
| **Pratt 算法做表达式** | 处理运算符优先级最干净 |
| **聚合错误，不 fail-fast** | 一次报所有错，对人和 AI 都友好 |
| **AST 节点都带 Pos** | 错误定位、IDE 高亮的基础 |

---

## 二、AST 节点总览

### 2.1 设计原则

1. **每个节点代表一个语法概念**（File / StructDecl / FlowStmt / MemberExpr…）
2. **保留语义，丢弃噪音**（缩进、可选分号、注释不进 AST）
3. **节点自包含**（带 Pos、带类型、不依赖外部 token）

### 2.2 节点继承树

```
Node (interface)
├── Decl                        ← 顶层 / 函数级声明
│   ├── File                    ← 整个文件
│   ├── ImportDecl              ← import stdio
│   ├── PackageDecl             ← package main
│   ├── EnumDecl                ← enum Method { ... }
│   ├── StructDecl              ← @cookie / hello{...} / <out>{...}
│   ├── EdgeDecl                ← hello <out> say { ... }
│   └── FuncDecl                ← main{...} / Post(str router){...}
│
├── Stmt                        ← 语句（出现在函数体 / 边体 / 节点体）
│   ├── BlockStmt               ← { ... }
│   ├── VarDecl                 ← h := "hi"   /   str h
│   ├── AssignStmt              ← a, b := expr
│   ├── IfStmt                  ← if cond { ... }
│   ├── ReturnStmt              ← return v
│   ├── PortDecl                ← >> str hey
│   ├── FieldDecl               ← str Domain;
│   ├── Connection              ← hello <out> stdio.Println
│   ├── FlowStmt                ← a >> b >> c
│   └── FlowCont                ← >>say.hay    (续行)
│
├── Expr                        ← 表达式
│   ├── IdentExpr               ← x
│   ├── LiteralExpr             ← "hello" / 42 / true
│   ├── MemberExpr              ← a.b / stdio.Println
│   ├── CallExpr                ← foo(a, b)
│   ├── BinaryExpr              ← a + b / x == y
│   └── UnaryExpr               ← -x / !flag
│
├── TypeRef                     ← 类型引用
│   ├── TypeName                ← str / num / bool / 用户自定义
│   ├── TypeArray               ← cookie[]
│   └── TypePtr                 ← *nio.context
│
├── FlowTarget                  ← 数据流的目标
│   ├── FlowIdent               ← a / a.b / stdio.Println
│   ├── FlowCall                ← foo(a, b)
│   └── FlowLiteral             ← "hello"
│
└── ConnectionHop               ← 图连接中的一个 hop
    ├── NodeRef                 ← hello
    ├── EdgeRef                 ← <out>
    └── CallRef                 ← stdio.Println(...)
```

### 2.3 叶子节点（无子节点）

```
Param         ← 函数参数：(str name)
FlowStep      ← 数据流的一步：target + 可选 rename
```

---

## 三、完整 Go 结构定义

> 建议目录：`circle/internal/parser/ast/ast.go`
>
> 依赖：`mocker_lex`（只用 `Type` / `Value`，不依赖 `Kind`）

### 3.1 通用接口

```go
package ast

import "circle/mocker_lex"

// Pos token 位置（行/列/字节偏移）
type Pos = mocker_lex.Pos   // 或重新定义

// Node 所有 AST 节点的根接口
type Node interface {
    Pos() Pos
    nodeMarker()
}

// Decl 声明节点
type Decl interface {
    Node
    declMarker()
}

// Stmt 语句节点
type Stmt interface {
    Node
    stmtMarker()
}

// Expr 表达式节点
type Expr interface {
    Node
    exprMarker()
}

// TypeRef 类型引用
type TypeRef interface {
    Node
    typeMarker()
}
```

### 3.2 文件

```go
// File 整个 .mocker 文件
type File struct {
    Pkg     *PackageDecl
    Imports []*ImportDecl
    Decls   []Decl         // EnumDecl / StructDecl / EdgeDecl / FuncDecl 混合
}

func (f *File) Pos() Pos        { return f.Pkg.Pos() }

// PackageDecl package main
type PackageDecl struct {
    PkgPos  Pos
    PkgName string
}
func (d *PackageDecl) Pos() Pos { return d.PkgPos }

// ImportDecl import stdio
type ImportDecl struct {
    Pos  Pos
    Path string
}
func (d *ImportDecl) Pos() Pos  { return d.Pos }
```

### 3.3 声明

```go
// EnumDecl enum Method { Post, Get, Delete }
type EnumDecl struct {
    Pos    Pos
    Name   string
    Values []string
}
func (d *EnumDecl) Pos() Pos { return d.Pos }

// StructDecl 表示以下三种之一：
//   1. 普通结构体：   @cookie { str Domain; }
//   2. 节点：         hello { h := "hi" }
//   3. 边（单边）：   <out> { h>>msg>> }
type StructDecl struct {
    Pos      Pos
    Kind     StructKind   // 区分普通 / 节点 / 边
    Exported bool         // @ 前缀
    Name     string
    Members  []StructMember
}

type StructKind int
const (
    StructKindPlain StructKind = iota
    StructKindNode
    StructKindEdge
)
func (d *StructDecl) Pos() Pos { return d.Pos }

// StructMember 是 struct/node/edge body 里的成员
// 用一个 Sum Type 表示，多种合法形式
//
// M4.5 新增：
//   - SubInstanceDecl：节点 body 内的 sub-instance 声明（`world w;`）
//   - SubEdgeDecl：节点 body 内的 sub-edge 连接（`h <add_str> w`）
//   - FlowDecl 扩展支持内部 flow 到 sub-instance（`out_str >> p.msg;`）
type StructMember interface {
    Node
    structMemberMarker()
}

// FieldDecl 强类型字段：str Domain;
type FieldDecl struct {
    Pos  Pos
    Type TypeRef
    Name string
}
func (f *FieldDecl) Pos() Pos { return f.Pos }

// VarDecl 节点体里的变量声明：h := "hi"
type VarDecl struct {
    Pos  Pos
    Name string
    Init Expr
    Flow *FlowChain
}
func (v *VarDecl) Pos() Pos { return v.Pos }

// FlowDecl 裸字段 / 导出字段：h  /  h >>  /  h>>msg>>
// M4.5：也可作为内部 flow 到 sub-instance 的 input
//   例：`out_str >> p.msg;` — out_str 流到 p 这个 sub-instance 的 msg 端口
type FlowDecl struct {
    Pos   Pos
    Head  string       // 字段名
    Chain *FlowChain   // 可选的 >> 链
}
func (f *FlowDecl) Pos() Pos { return f.Pos }

// PortDecl 端口声明：>> str hey
type PortDecl struct {
    Pos  Pos
    Type TypeRef
    Name string
    Body []Stmt        // 端口体（INDENT Stmt+ DEDENT）
}
func (p *PortDecl) Pos() Pos { return p.Pos }

// SubInstanceDecl（M4.5 新增）
// 节点 body 内的 sub-instance 声明：`TypeName varName;`
//
// 例：`world w;`（在 hello 节点 body 内声明一个 world 实例）
//
// 与 InstanceDecl（main body 用）的区别：
//   - InstanceDecl：main 节点 body 专用，作为 entry 声明
//   - SubInstanceDecl：任何节点 body 内可用，作为内部子实例
type SubInstanceDecl struct {
    Pos  Pos
    Type string // 节点类型（"world" / "stdio.Println"）
    Name string // 实例名（"w" / "p"）
}
func (s *SubInstanceDecl) Pos() Pos { return s.Pos }

// SubEdgeDecl（M4.5 新增）
// 节点 body 内的 sub-edge 连接：`src <edge_name> dst`
//
// 例：`h <add_str> w`（在 hello 内部，h → w，via <add_str> 边）
//
// 与 EdgeConnDecl（main body 用）的区别：
//   - EdgeConnDecl：main 节点 body 专用，作为入口拓扑
//   - SubEdgeDecl：任何节点 body 内可用，作为内部子图连接
type SubEdgeDecl struct {
    Pos  Pos
    Src  string // 源实例名（节点 body 内的 sub-instance）
    Edge string // 边名
    Dst  string // 目标实例名
}
func (s *SubEdgeDecl) Pos() Pos { return s.Pos }

// EdgeDecl 边声明（带源/汇的完整形式）：hello <out> say { ... }
type EdgeDecl struct {
    Pos   Pos
    Src   string       // 源节点名
    Edge  string       // 边名（可含 -）
    Dst   string       // 汇节点名
    Body  []Stmt
}
func (e *EdgeDecl) Pos() Pos { return e.Pos }

// FuncDecl 函数声明：main{...} / Post(str router){...} / <Post(str router)>{...}
type FuncDecl struct {
    Pos      Pos
    Name     string
    Exported bool
    Params   []*Param
    Body     []Stmt
}
func (f *FuncDecl) Pos() Pos { return f.Pos }

// Param 函数 / 边形参
type Param struct {
    Pos  Pos
    Type TypeRef
    Name string
}
```

### 3.4 类型

```go
// TypeName str / num / bool / 用户自定义
type TypeName struct {
    Pos  Pos
    Name string
}
func (t *TypeName) Pos() Pos { return t.Pos }

// TypeArray cookie[]
type TypeArray struct {
    Pos  Pos
    Elem TypeRef
}
func (t *TypeArray) Pos() Pos { return t.Pos }

// TypePtr *nio.context
type TypePtr struct {
    Pos  Pos
    Elem TypeRef
}
func (t *TypePtr) Pos() Pos { return t.Pos }
```

### 3.5 语句

```go
// BlockStmt { ... }  (用于 if/else 内部)
type BlockStmt struct {
    Pos   Pos
    Stmts []Stmt
}
func (b *BlockStmt) Pos() Pos { return b.Pos }

// IfStmt if cond { ... } else { ... }
type IfStmt struct {
    Pos  Pos
    Cond Expr
    Body *BlockStmt
    Else Stmt          // 可能是 *BlockStmt 或 *IfStmt
}
func (s *IfStmt) Pos() Pos { return s.Pos }

// ReturnStmt return v
type ReturnStmt struct {
    Pos   Pos
    Value Expr
}
func (s *ReturnStmt) Pos() Pos { return s.Pos }

// AssignStmt a, b := expr
type AssignStmt struct {
    Pos Pos
    Lhs []*IdentExpr
    Rhs Expr
}
func (s *AssignStmt) Pos() Pos { return s.Pos }

// Connection 图连接：hello <out> stdio.Println
type Connection struct {
    Pos  Pos
    Hops []ConnectionHop
}
func (c *Connection) Pos() Pos { return c.Pos }

// ConnectionHop 是 Connection 的一跳
type ConnectionHop interface {
    Node
    connectionHopMarker()
}

// NodeRef 节点引用：hello
type NodeRef struct {
    Pos  Pos
    Name string
}
func (n *NodeRef) Pos() Pos { return n.Pos }

// EdgeRef 边引用：<out>
type EdgeRef struct {
    Pos  Pos
    Name string    // 边名，可含 -
}
func (e *EdgeRef) Pos() Pos { return e.Pos }

// CallRef 调用引用：stdio.Println 或 stdio.Println(x)
type CallRef struct {
    Pos  Pos
    Fn   Expr
    Args []Expr
}
func (c *CallRef) Pos() Pos { return c.Pos }
```

### 3.6 数据流

```go
// FlowStmt 完整数据流：hello >> out >> stdio.Println
type FlowStmt struct {
    Pos   Pos
    Steps []*FlowStep
}
func (f *FlowStmt) Pos() Pos { return f.Pos }

// FlowCont 续行：>>say.hay
// 语法上是独立 Stmt；语义上是上一条 FlowStmt 的延续
type FlowCont struct {
    Pos   Pos
    Steps []*FlowStep
}
func (f *FlowCont) Pos() Pos { return f.Pos }

// FlowStep 数据流的一步
type FlowStep struct {
    Pos    Pos
    Target FlowTarget
    As     string    // 可选的重命名
}
func (s *FlowStep) Pos() Pos { return s.Pos }

// FlowChain 字段后的导出链：>> / >> msg / >> msg >> target
// 出现在 VarDecl / FlowDecl 之后
type FlowChain struct {
    Steps []*FlowStep
}

// FlowTarget 一步里的目标
type FlowTarget interface {
    Node
    flowTargetMarker()
}

// FlowIdent 标识符 / 成员访问：a / a.b / stdio.Println
type FlowIdent struct {
    Pos   Pos
    Chain []string   // ["stdio", "Println"]
    Call  []Expr     // 可选的调用参数
}
func (f *FlowIdent) Pos() Pos { return f.Pos }

// FlowLiteral 字符串字面量："hello world!"
type FlowLiteral struct {
    Pos   Pos
    Value string
}
func (f *FlowLiteral) Pos() Pos { return f.Pos }
```

### 3.7 表达式

```go
// IdentExpr x
type IdentExpr struct {
    Pos  Pos
    Name string
}
func (e *IdentExpr) Pos() Pos { return e.Pos }

// LiteralExpr "hello" / 42 / true / false
type LiteralExpr struct {
    Pos   Pos
    Kind  LiteralKind
    Value string    // 原始字面量
}
type LiteralKind int
const (
    LitString LiteralKind = iota
    LitNumber
    LitBool
)
func (e *LiteralExpr) Pos() Pos { return e.Pos }

// MemberExpr a.b / stdio.Println
type MemberExpr struct {
    Pos  Pos
    Obj  Expr
    Name string
}
func (e *MemberExpr) Pos() Pos { return e.Pos }

// CallExpr foo(a, b)
type CallExpr struct {
    Pos  Pos
    Fn   Expr
    Args []Expr
}
func (e *CallExpr) Pos() Pos { return e.Pos }

// BinaryExpr a + b / x == y
type BinaryExpr struct {
    Pos Pos
    Op  string    // "+" "-" "*" "/" "==" "!=" "<" ">" "<=" ">=" "&&" "||"
    L   Expr
    R   Expr
}
func (e *BinaryExpr) Pos() Pos { return e.Pos }

// UnaryExpr -x / !flag
type UnaryExpr struct {
    Pos Pos
    Op  string    // "-" "!"
    X   Expr
}
func (e *UnaryExpr) Pos() Pos { return e.Pos }
```

---

## 四、Parser 架构

### 4.1 目录结构

```
circle/internal/parser/
├── ast/
│   └── ast.go              ← 所有节点定义（本文件设计）
├── parser.go               ← Parser 结构 + Parse() 入口
├── parse_file.go           ← parseFile / parsePackage / parseImport
├── parse_decl.go           ← parseDecl 分发 + parseEnum/Struct/Edge/Func
├── parse_struct.go         ← parseStructDecl + parseStructMember + parsePort
├── parse_func.go           ← parseFuncDecl + parseParamList
├── parse_stmt.go           ← parseBlock + parseStmt + parseVarDecl/Assign/If/Return
├── parse_flow.go           ← parseConnection + parseFlowStmt + parseFlowCont
├── parse_expr.go           ← parseExpr (Pratt)
├── parse_type.go           ← parseTypeRef
├── parse_field.go          ← parseFieldDecl / parseFlowDecl
├── errors.go               ← ParseError 类型
├── recover.go              ← 错误恢复
└── parser_test.go          ← 单元测试
```

### 4.2 核心数据结构

```go
package parser

import (
    "circle/internal/parser/ast"
    "circle/mocker_lex"
)

type Parser struct {
    tokens []mocker_lex.Token
    pos    int              // 当前位置
    errors []ParseError     // 错误聚合
}

type ParseError struct {
    Pos mocker_lex.Pos
    Msg string
    Hint string             // 可选修复建议
}

func (e ParseError) Error() string {
    if e.Hint != "" {
        return fmt.Sprintf("parse error at %s: %s (hint: %s)", e.Pos, e.Msg, e.Hint)
    }
    return fmt.Sprintf("parse error at %s: %s", e.Pos, e.Msg)
}
```

### 4.3 公开入口

```go
// Parse 把 .mocker 源码解析成 AST
// 错误以列表形式返回（不 panic）
func Parse(src []byte) (*ast.File, []ParseError) {
    tokens, lexErr := mocker_lex.Tokenize(string(src))
    if lexErr != nil {
        return nil, []ParseError{{Pos: mocker_lex.Pos{}, Msg: lexErr.Error()}}
    }

    p := &Parser{tokens: tokens}
    file := p.parseFile()

    // 自动追加 EOF token（如果 lexer 没追加）
    if len(p.tokens) == 0 || p.tokens[len(p.tokens)-1].Type != mocker_lex.TypeEOF {
        p.tokens = append(p.tokens, mocker_lex.Token{Type: mocker_lex.TypeEOF})
    }

    return file, p.errors
}
```

### 4.4 核心辅助函数

```go
// peek 看当前 token（不消费）
func (p *Parser) peek() mocker_lex.Token {
    if p.pos >= len(p.tokens) {
        return mocker_lex.Token{Type: mocker_lex.TypeEOF}
    }
    return p.tokens[p.pos]
}

// peekN 看 n 个之后的 token（用于 2-token lookahead）
func (p *Parser) peekN(n int) mocker_lex.Token {
    idx := p.pos + n
    if idx >= len(p.tokens) {
        return mocker_lex.Token{Type: mocker_lex.TypeEOF}
    }
    return p.tokens[idx]
}

// consume 期望当前是某 token，否则报错；然后前进
func (p *Parser) consume(kind mocker_lex.Type) mocker_lex.Token {
    tok := p.peek()
    if tok.Type != kind {
        p.errorf("expected %s, got %s", kind, tok.Type)
        return tok   // 不前进，留给错误恢复
    }
    if tok.Type != mocker_lex.TypeEOF {
        p.pos++
    }
    return tok
}

// match 只判断，不消费
func (p *Parser) match(kinds ...mocker_lex.Type) bool {
    cur := p.peek().Type
    for _, k := range kinds {
        if cur == k {
            return true
        }
    }
    return false
}

// errorf 记录错误（不中断解析）
func (p *Parser) errorf(format string, args ...any) {
    p.errors = append(p.errors, ParseError{
        Pos: p.peek().Value,  // 简化：实际应该是 tok.Pos
        Msg: fmt.Sprintf(format, args...),
    })
}
```

### 4.5 错误恢复

```go
// recover 跳到下一个同步点
// 同步点：下一个顶层关键字 / 右花括号 / EOF
func (p *Parser) recover() {
    sync := map[mocker_lex.Type]bool{
        mocker_lex.TypeSYS_PACK:    true,
        mocker_lex.TypeSYS_IMPORT:  true,
        mocker_lex.TypeKW_ENUM:     true,
        mocker_lex.TypeKW_IF:       true,
        mocker_lex.TypeKW_RETURN:   true,
        mocker_lex.TypeKW_TRUE:     true,
        mocker_lex.TypeKW_FALSE:    true,
        mocker_lex.TypeID:          true,
        mocker_lex.TypeSEP_RBRACE:  true,
        mocker_lex.TypeEOF:         true,
    }
    for !p.match(mocker_lex.TypeEOF) {
        if sync[p.peek().Type] {
            return
        }
        p.pos++
    }
}
```

---

## 五、关键解析函数

### 5.1 parseFile（顶层调度器）

```go
func (p *Parser) parseFile() *ast.File {
    file := &ast.File{}

    // ① package 声明（必填）
    if p.peek().Type == mocker_lex.TypeSYS_PACK {
        file.Pkg = p.parsePackage()
    } else {
        p.errorf("expected 'package' at top of file")
    }

    // ② 顶层声明循环
    for !p.match(mocker_lex.TypeEOF) {
        var d ast.Decl
        switch p.peek().Type {
        case mocker_lex.TypeSYS_IMPORT:
            d = p.parseImport()
        case mocker_lex.TypeKW_ENUM:
            d = p.parseEnum()
        case mocker_lex.TypeSEP_AT:
            d = p.parseStruct()  // @name 或 @<name>
        case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
            d = p.parseTopDecl()  // IDENT / CALL / EDGE_NAME 起头的复杂分发
        case mocker_lex.TypeSEP_LT:
            d = p.parseStruct()  // <out> 单边形式
        default:
            p.errorf("unexpected token %s at top level", p.peek().Type)
            p.recover()
            continue
        }
        if d != nil {
            file.Decls = append(file.Decls, d)
        }
    }
    return file
}
```

### 5.2 parseTopDecl（IDENT 开头的顶层声明分发）

> 这一段最关键 —— IDENT 后面跟什么决定它是 StructDecl / EdgeDecl / FuncDecl / EnumDecl 的延续 / ImportDecl 的延续 / 等等。

```go
func (p *Parser) parseTopDecl() ast.Decl {
    // peek 看 IDENT 后面是什么
    //   - "{"        → StructDecl
    //   - "<"        → EdgeDecl
    //   - "(" 或 "<" → FuncDecl（带形参）
    //   - other      → 错误
    namePos := p.peek().Pos
    name := p.consume(mocker_lex.TypeID).Value

    switch p.peek().Type {
    case mocker_lex.TypeSEP_LBRACE:
        return p.parseStructBody(namePos, name, false)

    case mocker_lex.TypeOP_LT:
        // hello < out > say { ... }  或  hello <out>  (EdgeRef 形)
        // 看后续是不是 EDGE_NAME + > + IDENT
        if p.isEdgeSig() {
            return p.parseEdgeDecl(namePos, name)
        }
        // 尖括号签名：@<Post(str router)>
        return p.parseFuncDecl(name, true)

    case mocker_lex.TypeSEP_LPAREN:
        return p.parseFuncDecl(name, false)

    default:
        p.errorf("expected '{', '<' or '(' after top-level IDENT %q, got %s",
            name, p.peek().Type)
        return nil
    }
}
```

### 5.3 parseStruct（结构体 / 节点 / 边单边形式）

```go
func (p *Parser) parseStruct() *ast.StructDecl {
    pos := p.peek().Pos
    exported := false

    // 可选 @ 前缀
    if p.match(mocker_lex.TypeSEP_AT) {
        p.consume(mocker_lex.TypeSEP_AT)
        exported = true
    }

    // 名字：IDENT 或 <EDGE_NAME>
    var name string
    var kind ast.StructKind = ast.StructKindPlain
    switch p.peek().Type {
    case mocker_lex.TypeID, mocker_lex.TypeCALL:
        name = p.consume(p.peek().Type).Value
    case mocker_lex.TypeSEP_LT:
        kind = ast.StructKindEdge
        p.consume(mocker_lex.TypeSEP_LT)
        name = p.consume(mocker_lex.TypeEDGE_NAME).Value
        p.consume(mocker_lex.TypeOP_GT)
    default:
        p.errorf("expected struct name, got %s", p.peek().Type)
        return nil
    }

    p.consume(mocker_lex.TypeSEP_LBRACE)
    var members []ast.StructMember
    for !p.match(mocker_lex.TypeSEP_RBRACE, mocker_lex.TypeEOF) {
        m := p.parseStructMember()
        if m != nil {
            members = append(members, m)
        }
    }
    p.consume(mocker_lex.TypeSEP_RBRACE)

    if kind == ast.StructKindEdge {
        kind = ast.StructKindNode   // 单边 <name>{...} 视为一种节点
    } else if isNodeLike(name, members) {
        kind = ast.StructKindNode   // 启发式：含 PortDecl/FlowDecl 视为节点
    }

    return &ast.StructDecl{
        Pos: pos, Kind: kind, Exported: exported,
        Name: name, Members: members,
    }
}
```

### 5.4 parseStructMember（成员分发的 5 种形式）

```go
func (p *Parser) parseStructMember() ast.StructMember {
    pos := p.peek().Pos
    tok := p.peek()

    switch {
    // 形式 0：>> str hey  → PortDecl
    case tok.Type == mocker_lex.TypeOP_RRARROW:
        return p.parsePortDecl()

    // 形式 1：str Domain;  → FieldDecl（IDENT 后是 ; {  或 >>）
    case isTypeStart(tok) && isTypedField():
        typ := p.parseTypeRef()
        name := p.consume(mocker_lex.TypeID).Value
        return &ast.FieldDecl{Pos: pos, Type: typ, Name: name}

    // 形式 2：h := "hi"  → VarDecl
    case tok.Type == mocker_lex.TypeID && p.peekN(1).Type == mocker_lex.TypeOP_DEFINE:
        return p.parseVarDeclInStruct()

    // 形式 3：h / h >> / h>>msg>>  → FlowDecl
    case tok.Type == mocker_lex.TypeID:
        return p.parseFlowDecl()
    }

    p.errorf("unexpected token %s in struct body", tok.Type)
    p.pos++
    return nil
}
```

### 5.5 parsePortDecl（端口声明）

```go
func (p *Parser) parsePortDecl() *ast.PortDecl {
    pos := p.consume(mocker_lex.TypeOP_RRARROW).Pos
    typ := p.parseTypeRef()
    name := p.consume(mocker_lex.TypeID).Value

    decl := &ast.PortDecl{Pos: pos, Type: typ, Name: name}

    // 可选缩进体（由 lexer 跟踪 INDENT/DEDENT）
    // 如果下一 token 是 INDENT，消化后解析 Stmt+ 直到 DEDENT
    // 当前 lexer 不产 INDENT/DEDENT，端口体可以暂时用 { } 替代
    // 或者端口体为 0 条 Stmt
    return decl
}
```

### 5.6 parseFlowDecl（裸字段 + 导出后缀）

```go
func (p *Parser) parseFlowDecl() *ast.FlowDecl {
    pos := p.peek().Pos
    name := p.consume(mocker_lex.TypeID).Value

    decl := &ast.FlowDecl{Pos: pos, Head: name}
    if p.match(mocker_lex.TypeOP_RRARROW) {
        decl.Chain = p.parseFlowChain()
    }
    return decl
}

// parseFlowChain 解析 >> 链：>> / >> msg / >> msg >> target / >> "str"
// 第一个 >> 必须存在
func (p *Parser) parseFlowChain() *ast.FlowChain {
    chain := &ast.FlowChain{}
    for p.match(mocker_lex.TypeOP_RRARROW) {
        p.consume(mocker_lex.TypeOP_RRARROW)
        step := &ast.FlowStep{Pos: p.peek().Pos}

        switch p.peek().Type {
        case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
            step.Target = p.parseFlowIdent()
        case mocker_lex.TypeSTRING:
            step.Target = &ast.FlowLiteral{
                Pos: p.peek().Pos,
                Value: p.consume(mocker_lex.TypeSTRING).Value,
            }
        default:
            p.errorf("expected flow target after >>, got %s", p.peek().Type)
            return chain
        }

        // 可选重命名：>> msg
        if p.match(mocker_lex.TypeOP_RRARROW) && p.peekN(1).Type == mocker_lex.TypeID {
            p.consume(mocker_lex.TypeOP_RRARROW)
            step.As = p.consume(mocker_lex.TypeID).Value
        }

        chain.Steps = append(chain.Steps, step)
    }
    return chain
}
```

### 5.7 parseConnection（图连接）

```go
// hello <out> stdio.Println
// hello <out> say
func (p *Parser) parseConnection() *ast.Connection {
    pos := p.peek().Pos
    conn := &ast.Connection{Pos: pos}

    for {
        hop := p.parseConnectionHop()
        if hop == nil {
            break
        }
        conn.Hops = append(conn.Hops, hop)

        // 下一个 token 还能起一个新 hop 吗？
        if !p.isConnectionHopStart() {
            break
        }
    }
    return conn
}

func (p *Parser) parseConnectionHop() ast.ConnectionHop {
    pos := p.peek().Pos
    switch p.peek().Type {
    case mocker_lex.TypeSEP_LT:
        p.consume(mocker_lex.TypeSEP_LT)
        name := p.consume(mocker_lex.TypeEDGE_NAME).Value
        p.consume(mocker_lex.TypeOP_GT)
        return &ast.EdgeRef{Pos: pos, Name: name}
    case mocker_lex.TypeID, mocker_lex.TypeCALL:
        e := p.parsePrimaryExpr()   // IDENT / a.b / a.b.c / foo(args)
        // 如果是 CallExpr，包装成 CallRef
        if call, ok := e.(*ast.CallExpr); ok {
            return &ast.CallRef{Pos: pos, Fn: call.Fn, Args: call.Args}
        }
        // 否则是 MemberExpr / IdentExpr → 拿名字作为 NodeRef
        return &ast.NodeRef{Pos: pos, Name: extractIdentName(e)}
    default:
        return nil
    }
}
```

### 5.8 parseExpr（Pratt 算法）

```go
// 优先级（高 → 低）：
//   7  .  (成员访问)
//   6  *  /
//   5  +  -
//   4  <  >  <=  >=
//   3  == !=
//   2  &&
//   1  ||

func (p *Parser) parseExpr() ast.Expr {
    return p.parseBinary(1)
}

func (p *Parser) parseBinary(minPrec int) ast.Expr {
    lhs := p.parseUnary()

    for p.isOp() && opPrec(p.peek().Type) >= minPrec {
        op := p.peek()
        p.pos++
        prec := opPrec(op.Type)
        rhs := p.parseBinary(prec + 1)
        lhs = &ast.BinaryExpr{
            Pos: lhs.Pos(), Op: op.Value,
            L: lhs, R: rhs,
        }
    }
    return lhs
}

func (p *Parser) parseUnary() ast.Expr {
    if p.match(mocker_lex.TypeOP_SUB, mocker_lex.TypeOP_NOT) {
        op := p.consume(p.peek().Type)
        x := p.parseUnary()
        return &ast.UnaryExpr{Pos: op.Pos, Op: op.Value, X: x}
    }
    return p.parsePrimary()
}

func (p *Parser) parsePrimary() ast.Expr {
    pos := p.peek().Pos
    switch p.peek().Type {
    case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
        return p.parseIdentOrMemberOrCall()
    case mocker_lex.TypeSTRING:
        return &ast.LiteralExpr{
            Pos: pos, Kind: ast.LitString,
            Value: p.consume(mocker_lex.TypeSTRING).Value,
        }
    case mocker_lex.TypeNUM:
        return &ast.LiteralExpr{
            Pos: pos, Kind: ast.LitNumber,
            Value: p.consume(mocker_lex.TypeNUM).Value,
        }
    case mocker_lex.TypeKW_TRUE:
        p.consume(mocker_lex.TypeKW_TRUE)
        return &ast.LiteralExpr{Pos: pos, Kind: ast.LitBool, Value: "true"}
    case mocker_lex.TypeKW_FALSE:
        p.consume(mocker_lex.TypeKW_FALSE)
        return &ast.LiteralExpr{Pos: pos, Kind: ast.LitBool, Value: "false"}
    case mocker_lex.TypeSEP_LPAREN:
        p.consume(mocker_lex.TypeSEP_LPAREN)
        e := p.parseExpr()
        p.consume(mocker_lex.TypeSEP_RPAREN)
        return e
    default:
        p.errorf("unexpected token in expression: %s", p.peek().Type)
        return &ast.LiteralExpr{Pos: pos, Value: ""}
    }
}

func (p *Parser) parseIdentOrMemberOrCall() ast.Expr {
    pos := p.peek().Pos
    first := p.consume(p.peek().Type).Value
    e := ast.Expr(&ast.IdentExpr{Pos: pos, Name: first})

    // 成员访问链：a.b.c
    for p.match(mocker_lex.TypeSEP_DOT) {
        p.consume(mocker_lex.TypeSEP_DOT)
        name := p.consume(mocker_lex.TypeID).Value
        e = &ast.MemberExpr{Pos: pos, Obj: e, Name: name}
    }

    // 可选调用：(args)
    if p.match(mocker_lex.TypeSEP_LPAREN) {
        p.consume(mocker_lex.TypeSEP_LPAREN)
        var args []ast.Expr
        for !p.match(mocker_lex.TypeSEP_RPAREN, mocker_lex.TypeEOF) {
            args = append(args, p.parseExpr())
            if p.match(mocker_lex.TypeSEP_COMMA) {
                p.consume(mocker_lex.TypeSEP_COMMA)
            }
        }
        p.consume(mocker_lex.TypeSEP_RPAREN)
        e = &ast.CallExpr{Pos: pos, Fn: e, Args: args}
    }
    return e
}

func opPrec(t mocker_lex.Type) int {
    switch t {
    case mocker_lex.TypeOP_OR:                 return 1
    case mocker_lex.TypeOP_AND:                return 2
    case mocker_lex.TypeOP_EQ, mocker_lex.TypeOP_NE: return 3
    case mocker_lex.TypeOP_LT, mocker_lex.TypeOP_GT,
         mocker_lex.TypeOP_LE, mocker_lex.TypeOP_GE: return 4
    case mocker_lex.TypeOP_ADD, mocker_lex.TypeOP_SUB: return 5
    case mocker_lex.TypeOP_MUL, mocker_lex.TypeOP_DIV: return 6
    case mocker_lex.TypeSEP_DOT:                return 7
    }
    return 0
}
```

### 5.9 parseStmt（语句分发的优先级）

```go
func (p *Parser) parseStmt() ast.Stmt {
    tok := p.peek()
    switch tok.Type {
    case mocker_lex.TypeKW_IF:
        return p.parseIf()
    case mocker_lex.TypeKW_RETURN:
        return p.parseReturn()
    case mocker_lex.TypeOP_RRARROW:
        // >> 流语句：>> say.hay（续行） 或 >> str hey（端口？端口只在 struct 里）
        return p.parseFlowCont()
    case mocker_lex.TypeSEP_LT:
        // Connection 起始：<out> stdio.Println
        return p.parseConnection()
    case mocker_lex.TypeID, mocker_lex.TypeCALL, mocker_lex.TypeEDGE_NAME:
        return p.parseStmtDispatch()  // VarDecl / AssignStmt / Connection
    default:
        p.errorf("unexpected statement starting with %s", tok.Type)
        p.pos++
        return nil
    }
}

// parseStmtDispatch IDENT 起头语句的分发
//   IDENT "{"           → 不可能（顶层才是 {）
//   IDENT ":=" EXPR      → VarDecl
//   IDENT "," IDENT ":=" → AssignStmt
//   IDENT ">>"           → FlowStmt
//   IDENT <next IDENT>   → Connection
func (p *Parser) parseStmtDispatch() ast.Stmt {
    pos := p.peek().Pos
    saved := p.pos

    // 试 Connection：IDENT < ... 或 IDENT ...
    if p.isConnectionStart() {
        return p.parseConnection()
    }

    // 试 VarDecl / AssignStmt
    if p.peekN(1).Type == mocker_lex.TypeOP_DEFINE {
        return p.parseVarDeclOrAssign()
    }

    // 试 FlowStmt：IDENT >> ...
    if p.peekN(1).Type == mocker_lex.TypeOP_RRARROW {
        return p.parseFlowStmt()
    }

    p.errorf("cannot start statement with IDENT %q", p.peek().Value)
    p.pos++
    _ = saved
    _ = pos
    return nil
}
```

---

## 六、关键设计决策

| 决策 | 方案 | 理由 |
| --- | --- | --- |
| Parser 算法 | 递归下降 + Pratt 表达式 | 错误信息好、易扩展 |
| Lookahead | 1 token（少数 2） | DSL 语法不太歧义 |
| AST 节点粒度 | 每个语法概念一个 struct | type switch 清晰 |
| 错误处理 | 聚合 + 同步点恢复 | 一次报告所有错 |
| 节点自包含 | 每个节点带 `Pos()` | 错误定位基础 |
| `Connection` 表示 | 序列的 `[]ConnectionHop` | 节点 / 边 / 调用 自由组合 |
| `FlowChain` 独立 | 抽离出 `FlowStep` 列表 | 同时支持 VarDecl / FlowDecl / FlowStmt |
| EdgeRef.Name | 保留连字符原样 | 上层不需知道连字符 |
| CALL / EDGE_NAME / ID | 都进 Lexer 阶段就识别好 | Parser 只看 Type 不用纠结字符串 |
| `PortDecl.Body` | 暂留 `[]Stmt` 字段 | 等 lexer 支持 INDENT/DEDENT 再激活 |

---

## 七、与 lexer 的契约

```go
// parser 只依赖 mocker_lex 的 3 个符号
import "circle/mocker_lex"

type Token = mocker_lex.Token
type Type  = mocker_lex.Type
type Pos   = mocker_lex.Pos

// parser 用到的 token 类型（来自 lexer.go）
// 关键字类：TypeSYS_PACK / TypeSYS_IMPORT / TypeKW_ENUM / ...
// 运算符类：TypeOP_RRARROW / TypeOP_DEFINE / TypeOP_AND / ...
// 定界符类：TypeSEP_LBRACE / TypeSEP_RPAREN / TypeSEP_DOT / ...
// 字面量类：TypeSTRING / TypeNUM
// 标识符类：TypeID / TypeCALL / TypeEDGE_NAME
// 特殊：     TypeEOF
```

**Parser 不应该做的：**
- 不看 `Token.Value` 区分关键字 —— 用 `Type` 判断
- 不解析字符串字面量的转义 —— lexer 阶段就处理完
- 不重新实现 longest-match —— lexer 阶段已保证

---

## 八、测试策略

### 8.1 每个 .mocker 一个测试

```go
// circle/internal/parser/parser_test.go

func TestParseCookie(t *testing.T) {
    src := []byte(`package cookie

@cookie {
    str Domain
    str Path
    bool Secure
}
`)
    file, errs := Parse(src)
    assertNoErrors(t, errs)
    assertPkg(t, file, "cookie")
    if len(file.Decls) != 1 { t.Fatal("decl count") }

    sd, ok := file.Decls[0].(*ast.StructDecl)
    if !ok { t.Fatal("not struct") }
    if !sd.Exported { t.Error("not exported") }
    if len(sd.Members) != 3 { t.Fatal("field count") }
}

func TestParseFlowChain(t *testing.T) {
    src := []byte(`package main

main {
    hello <out> stdio.Println
}
`)
    file, errs := Parse(src)
    assertNoErrors(t, errs)

    fd := file.Decls[0].(*ast.FuncDecl)
    if len(fd.Body) != 1 { t.Fatal("stmt count") }

    conn, ok := fd.Body[0].(*ast.Connection)
    if !ok { t.Fatal("not connection") }
    if len(conn.Hops) != 3 { t.Fatal("hop count") }
}

func TestParsePortDecl(t *testing.T) {
    src := []byte(`package main

say {
    >> str hey
    >> str my
}
`)
    file, errs := Parse(src)
    assertNoErrors(t, errs)

    sd := file.Decls[0].(*ast.StructDecl)
    ports := 0
    for _, m := range sd.Members {
        if _, ok := m.(*ast.PortDecl); ok {
            ports++
        }
    }
    if ports != 2 { t.Fatalf("port count: got %d", ports) }
}
```

### 8.2 跑遍 example 目录

```go
func TestAllExamples(t *testing.T) {
    files, _ := filepath.Glob("../../example/**/*.mocker")
    files = append(files, glob("../../example/*.mocker")...)

    for _, f := range files {
        t.Run(f, func(t *testing.T) {
            src, _ := os.ReadFile(f)
            _, errs := Parse(src)
            for _, e := range errs {
                t.Logf("%s: %s", f, e)
            }
        })
    }
}
```

---

## 九、实施计划（2 周）

| 天 | 步骤 | 验收 |
| --- | --- | --- |
| Day 1-2 | `ast/ast.go` 全部节点定义 + `go build` 通过 | 编译无错 |
| Day 3 | `parser.go` 框架 + `parseFile` + `parseImport` + `parseEnum` | cookie.mocker 进 parseFile |
| Day 4 | `parseStructDecl` + `parseStructMember`（5 种形式） | cookie.mocker 解析通过 |
| Day 5 | `parseFlowChain` + `parseFlowDecl` | main.mocker 数据流解析通过 |
| Day 6 | `parseEdgeDecl` + `parseFuncDecl`（两种签名） | netio.mocker 解析通过 |
| Day 7 | `parsePortDecl` + 端口体支持 | say{} 节点解析通过 |
| Day 8 | `parseConnection` + `parseFlowStmt` + `parseFlowCont` | main{...} 解析通过 |
| Day 9-10 | `parseExpr`（Pratt）+ `parseStmt` 分发 | 函数体解析通过 |
| Day 11 | 错误恢复 + 错误信息打磨 | 多错误一次报 |
| Day 12 | 跑遍 example + 修复边界 case | 全部通过 |
| Day 13-14 | 文档、调试工具、PR review | 可合并 |

---

## 十、AST 可视化（调试工具）

```go
// circle/internal/parser/dump.go

func Dump(f *ast.File) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("File package=%s\n", f.Pkg.PkgName))
    for _, d := range f.Decls {
        dumpDecl(&sb, d, 1)
    }
    return sb.String()
}

func dumpDecl(sb *strings.Builder, d ast.Decl, depth int) {
    indent(sb, depth)
    switch v := d.(type) {
    case *ast.StructDecl:
        fmt.Fprintf(sb, "StructDecl %s%s {\n",
            ifExported(v.Exported), v.Name)
        for _, m := range v.Members {
            dumpMember(sb, m, depth+1)
        }
    case *ast.FuncDecl:
        fmt.Fprintf(sb, "FuncDecl %s(%d params, %d stmts)\n",
            v.Name, len(v.Params), len(v.Body))
    case *ast.EdgeDecl:
        fmt.Fprintf(sb, "EdgeDecl %s <%s> %s {\n", v.Src, v.Edge, v.Dst)
    // ...
    }
}
```

```bash
# CLI 集成（之后）
$ circle parse example/main.mocker
File package=main
  StructDecl hello { ... }
  EdgeDecl hello <out> say { ... }
  StructDecl say { ... }
  FuncDecl main(0 params, 1 stmts)
    Connection: hello → <out> → say
```

---

## 十一、总结

> **AST 是中央数据结构，Parser 是把 token 流组装成 AST 的程序。**

**核心原则：**
1. **AST 节点和 EBNF 产生式一一对应** —— 形式化语法 → 强类型结构
2. **每个节点都带 `Pos()`** —— 错误定位、IDE 高亮
3. **错误聚合，不 fail-fast** —— 对人和 AI 都更友好
4. **递归下降 + Pratt 表达式** —— 经典组合，覆盖 Mocker 全部语法
5. **从最简单的开始**（parseImport / parseEnum）→ 逐步扩展到 Flow / Connection / Expr

**下一步：** 按 Day 1 开始写 `ast/ast.go`，跑通 `go build`。然后按 Day 3 开始写 parser 框架。**两周后**，你就有了一个能解析所有 `.mocker` 文件的 parser，AST 是后面所有阶段（semantic / IR / codegen）的基础。**多花一天设计 AST，省后面一月返工。**
