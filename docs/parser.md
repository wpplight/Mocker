# Mocker Parser 实现指南

> 从 Token 到 AST：手把手实现 Mocker DSL 的语法分析器
>
> 适用：已完成 Lexer、准备动手写 Parser 的工程师

---

## 一、概述

### 1.1 Parser 在编译器中的位置

```
源码 (.mocker 文本)
        ↓
  ┌──────────┐
  │  Lexer   │  ← 你已完成：字符 → Token 流
  └────┬─────┘
       ↓ []Token
  ┌──────────┐
  │  Parser  │  ← 本文：Token 流 → AST
  └────┬─────┘
       ↓ *ast.File
  ┌──────────┐
  │ Semantic │  ← 后续：类型检查、引用解析
  └────┬─────┘
       ↓
  ┌──────────┐
  │ IR       │  ← 后续：规范化
  └────┬─────┘
       ↓
  ┌──────────┐
  │ Codegen  │  ← 后续：生成 Go 源码
  └──────────┘
```

### 1.2 Parser 的职责（一句话）

> **按语法规则，把扁平 Token 流组装成一棵有类型的树（AST），丢掉语法噪音（缩进、注释、空格），保留语义结构（声明、字段、表达式、数据流）。**

### 1.3 为什么选递归下降

| 方案 | 优点 | 缺点 | 适用场景 |
| --- | --- | --- | --- |
| yacc / LALR | 自动生成 | 错误信息差、状态机难调试 | 语法极简单 |
| PEG / packrat | 灵活、回溯 | Go 生态弱、性能差 | 快速原型 |
| **手写递归下降** | **错误信息好、易扩展、自举友好** | 工作量稍大 | **本项目推荐** |

**递归下降 = 一组互相调用的函数**，每个语法形式对应一个函数。

### 1.4 你要解析的语法（来自你实际的 .mocker 文件）

读完 `example/*.mocker` 后，你的 DSL 有这些语法形式：

```mocker
# 顶层
package <name>
import <name>
@<StructName> { ... }                  # 导出 struct
<StructName> { ... }                   # 非导出 struct
@<FuncName>(<params>) { ... }          # 导出函数（圆括号签名）
@<<FuncName>(<params>)> { ... }        # 导出函数（尖括号签名）
enum <Name> { A, B, C }                # 枚举

# 字段
str <name>;                           # 带类型
<name>;                               # 裸字段
<type>[] <name>;                      # 数组
<type> <name> {};                     # 带嵌套 block
<name> := <value> <name> >>           # 带初始化 + 数据流

# 数据流
<ident> >>                            # 单导出
<ident> >> <name> >>                  # 重命名
<ident> >> <ident> >> <pkg>.<member>  # 链式

# 函数体
<ident>(<args>)                       # 函数调用
<pkg>.<member>                        # 成员访问
<lhs> := <rhs>                        # 赋值
if <cond> { ... }                     # 条件
```

**结论**：语法多样性高，但**递归下降 + 1 token lookahead**可以全部覆盖。

---

## 二、AST 设计（先于 Parser 实现）

### 2.1 设计原则

**AST 是整个编译器的"中央数据结构"**——所有下游阶段（Semantic、IR、Codegen）都基于 AST 工作。

**三个核心原则**：

1. **每个节点代表一个"语法概念"**
2. **保留语义，丢弃语法噪音**（缩进、注释、分号、空格）
3. **节点自包含**（带 Pos、有类型、不依赖外部 token）

### 2.2 AST 节点总览

```
Node (interface)
├── Decl
│   ├── File              ← 整个文件
│   ├── ImportDecl        ← import stdio
│   ├── StructDecl        ← @cookie { ... }
│   ├── FuncDecl          ← login exec(...) { ... }
│   └── EnumDecl          ← enum Method { ... }
│
├── Stmt
│   ├── BlockStmt         ← { ... }
│   ├── VarDecl           ← str x; / x := v
│   ├── IfStmt            ← if cond { ... }
│   ├── AssignStmt        ← x, y := v
│   └── ReturnStmt        ← return v
│
├── Expr
│   ├── IdentExpr         ← x
│   ├── LiteralExpr       ← "hello", 42, true
│   ├── MemberExpr        ← stdio.Println
│   ├── CallExpr          ← c.bind(login)
│   └── BinaryExpr        ← a + b, x == y
│
├── TypeRef
│   ├── TypeName          ← str / num / cookie
│   ├── TypeArray         ← cookie[]
│   └── TypePtr           ← *nio.context
│
└── Field / Param / FlowStep   ← 叶子节点
```

### 2.3 完整 AST 定义

在 `compiler/internal/parser/ast.go` 中实现：

```go
package ast

import "Mocker/compiler/internal/lexer"

// ──── 通用接口 ────
type Node interface {
    Pos() lexer.Pos
    nodeMarker()
}
type Decl interface {
    Node
    declMarker()
}
type Stmt interface {
    Node
    stmtMarker()
}
type Expr interface {
    Node
    exprMarker()
}

// ──── 文件 ────
type File struct {
    PkgPos  lexer.Pos
    PkgName string
    Imports []*ImportDecl
    Decls   []Decl
}

func (f *File) Pos() lexer.Pos  { return f.PkgPos }
func (f *File) nodeMarker()     {}

type ImportDecl struct {
    Pos  lexer.Pos
    Path string
}

func (i *ImportDecl) Pos() lexer.Pos  { return i.Pos }
func (i *ImportDecl) declMarker()     {}

// ──── 声明 ────
type StructDecl struct {
    Pos      lexer.Pos
    Exported bool   // @ 前缀
    Name     string
    Fields   []*Field
}

func (s *StructDecl) Pos() lexer.Pos  { return s.Pos }
func (s *StructDecl) declMarker()     {}

type FuncDecl struct {
    Pos      lexer.Pos
    Exported bool
    Name     string
    Params   []*Param
    Returns  []*Param
    Body     *BlockStmt
}

func (f *FuncDecl) Pos() lexer.Pos    { return f.Pos }
func (f *FuncDecl) declMarker()       {}

type EnumDecl struct {
    Pos    lexer.Pos
    Name   string
    Values []string
}

func (e *EnumDecl) Pos() lexer.Pos    { return e.Pos }
func (e *EnumDecl) declMarker()       {}

// ──── 字段 ────
type Field struct {
    Pos  lexer.Pos
    Type TypeRef
    Name string
    Flow []*FlowStep   // 可选：>> 链
}

func (f *Field) Pos() lexer.Pos       { return f.Pos }

type Param struct {
    Pos  lexer.Pos
    Type TypeRef
    Name string
}

// ──── 类型 ────
type TypeRef interface {
    Pos() lexer.Pos
    typeMarker()
}

type TypeName struct {
    Pos  lexer.Pos
    Name string
}

func (t *TypeName) Pos() lexer.Pos    { return t.Pos }
func (t *TypeName) typeMarker()       {}

type TypeArray struct {
    Pos  lexer.Pos
    Elem TypeRef
}

func (t *TypeArray) Pos() lexer.Pos   { return t.Pos }
func (t *TypeArray) typeMarker()      {}

type TypePtr struct {
    Pos  lexer.Pos
    Elem TypeRef
}

func (t *TypePtr) Pos() lexer.Pos     { return t.Pos }
func (t *TypePtr) typeMarker()        {}

// ──── 语句 ────
type BlockStmt struct {
    Pos   lexer.Pos
    Stmts []Stmt
}

func (b *BlockStmt) Pos() lexer.Pos   { return b.Pos }
func (b *BlockStmt) stmtMarker()      {}

type VarDecl struct {
    Pos  lexer.Pos
    Name string
    Type TypeRef
    Init Expr
    Flow []*FlowStep
}

func (v *VarDecl) Pos() lexer.Pos     { return v.Pos }
func (v *VarDecl) stmtMarker()        {}

type IfStmt struct {
    Pos  lexer.Pos
    Cond Expr
    Body *BlockStmt
    Else Stmt
}

func (s *IfStmt) Pos() lexer.Pos      { return s.Pos }
func (s *IfStmt) stmtMarker()         {}

type AssignStmt struct {
    Pos lexer.Pos
    Lhs []Expr
    Rhs Expr
}

func (s *AssignStmt) Pos() lexer.Pos  { return s.Pos }
func (s *AssignStmt) stmtMarker()     {}

type ReturnStmt struct {
    Pos   lexer.Pos
    Value Expr
}

func (s *ReturnStmt) Pos() lexer.Pos  { return s.Pos }
func (s *ReturnStmt) stmtMarker()     {}

// ──── 表达式 ────
type IdentExpr struct {
    Pos  lexer.Pos
    Name string
}

func (e *IdentExpr) Pos() lexer.Pos   { return e.Pos }
func (e *IdentExpr) exprMarker()      {}

type LiteralExpr struct {
    Pos   lexer.Pos
    Value any   // string / int / bool
}

func (e *LiteralExpr) Pos() lexer.Pos { return e.Pos }
func (e *LiteralExpr) exprMarker()    {}

type MemberExpr struct {
    Pos  lexer.Pos
    Obj  Expr
    Name string
}

func (e *MemberExpr) Pos() lexer.Pos  { return e.Pos }
func (e *MemberExpr) exprMarker()     {}

type CallExpr struct {
    Pos  lexer.Pos
    Fn   Expr
    Args []Expr
}

func (e *CallExpr) Pos() lexer.Pos    { return e.Pos }
func (e *CallExpr) exprMarker()       {}

type BinaryExpr struct {
    Pos lexer.Pos
    Op  string
    L   Expr
    R   Expr
}

func (e *BinaryExpr) Pos() lexer.Pos  { return e.Pos }
func (e *BinaryExpr) exprMarker()     {}

// ──── 数据流 ────
type FlowStep struct {
    Pos lexer.Pos
    Src Expr    // "hello" / "out" / "stdio.Println" / 字面量
    As  string  // 重命名
}

func (s *FlowStep) Pos() lexer.Pos    { return s.Pos }
```

### 2.4 AST 节点设计要点

| 决策 | 理由 |
| --- | --- |
| 用 `interface` + `struct` 组合 | `[]Decl` 装任意声明；type switch 拿具体类型 |
| 每个节点带 `Pos()` | 错误信息、IDE 高亮都需要位置 |
| 列表用 `[]T` | 0 或多个字段 |
| 单值用 `*T` | nil 表达"无"（如 FuncDecl.Returns、IfStmt.Else） |
| 字段不用 map | `[]*Field` 比 `map[string]any` 类型安全 |
| 错误标记用空方法 | `declMarker()` 防止类型混淆 |

### 2.5 AST 可视化（每个节点都能 `String()`）

建议给每个节点加调试方法：

```go
func (s *StructDecl) String() string {
    return fmt.Sprintf("struct %s%s { %d fields }",
        ifExported(s.Exported), s.Name, len(s.Fields))
}

func (f *Field) String() string {
    typ := "<no-type>"
    if f.Type != nil { typ = f.Type.String() }
    return fmt.Sprintf("%s %s", typ, f.Name)
}
```

---

## 三、Parser 框架

### 3.1 核心数据结构

在 `compiler/internal/parser/parser.go` 中：

```go
package parser

import (
    "fmt"
    "Mocker/compiler/internal/lexer"
    "Mocker/compiler/internal/parser/ast"
)

type Parser struct {
    tok    []lexer.Token   // 一次性产出的 token
    pos    int             // 当前位置
    errors []ParseError
}

type ParseError struct {
    Pos lexer.Pos
    Msg string
}

func (e ParseError) Error() string {
    return fmt.Sprintf("parse error at %s: %s", e.Pos, e.Msg)
}
```

### 3.2 公开入口

```go
// Parse 是外部调用的入口
func Parse(src []byte) (*ast.File, []ParseError) {
    lx := lexer.New(src)
    p := &Parser{tok: lx.ScanAll()}
    file := p.parseFile()
    return file, p.errors
}
```

### 3.3 核心辅助函数

```go
// peek 看当前 token（不消费）
func (p *Parser) peek() lexer.Token {
    if p.pos >= len(p.tok) {
        return lexer.Token{Kind: lexer.EOF}
    }
    return p.tok[p.pos]
}

// peekN 看 n 个之后的 token（用于 lookahead）
func (p *Parser) peekN(n int) lexer.Token {
    idx := p.pos + n
    if idx >= len(p.tok) {
        return lexer.Token{Kind: lexer.EOF}
    }
    return p.tok[idx]
}

// consume 期望当前是某 token，否则报错；然后前进
func (p *Parser) consume(kind lexer.Kind) lexer.Token {
    if p.peek().Kind != kind {
        p.errorf("expected %s, got %s", kind, p.peek().Kind)
    }
    t := p.tok[p.pos]
    if t.Kind != lexer.EOF {
        p.pos++
    }
    return t
}

// match 只判断，不消费
func (p *Parser) match(kinds ...lexer.Kind) bool {
    for _, k := range kinds {
        if p.peek().Kind == k {
            return true
        }
    }
    return false
}

// errorf 记录错误，继续解析（不 fail-fast）
func (p *Parser) errorf(format string, args ...any) {
    p.errors = append(p.errors, ParseError{
        Pos: p.peek().Pos,
        Msg: fmt.Sprintf(format, args...),
    })
}
```

### 3.4 顶层调度器（parseFile）

```go
func (p *Parser) parseFile() *ast.File {
    file := &ast.File{}
    
    // ① package 声明（必填）
    if p.match(lexer.PACKAGE) {
        file.PkgPos = p.peek().Pos
        p.consume(lexer.PACKAGE)
        file.PkgName = p.consume(lexer.IDENT).Literal
    } else {
        p.errorf("expected 'package' at top of file")
    }
    
    // ② 顶层声明循环
    for !p.match(lexer.EOF) {
        var d ast.Decl
        switch {
        case p.match(lexer.IMPORT):
            d = p.parseImport()
        case p.match(lexer.AT):
            d = p.parseExport()       // @ 前缀
        case p.match(lexer.ENUM):
            d = p.parseEnum()
        case p.match(lexer.IDENT):
            d = p.parseDecl()         // 普通声明
        default:
            p.errorf("unexpected token %s at top level", p.peek().Kind)
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

### 3.5 错误恢复

```go
// recover 跳到下一个同步点（分号、右花括号、顶层关键字）
func (p *Parser) recover() {
    sync := map[lexer.Kind]bool{
        lexer.PACKAGE: true, lexer.IMPORT: true,
        lexer.AT: true, lexer.ENUM: true, lexer.IDENT: true,
        lexer.RBRACE: true, lexer.SEMI: true,
    }
    for !p.match(lexer.EOF) {
        if sync[p.peek().Kind] {
            return
        }
        p.pos++
    }
}
```

**关键设计**：**不 fail-fast**——一次报告所有错误，对人和 AI 都更友好。

---

## 四、parseImport / parseEnum（最简单的两个）

### 4.1 parseImport

```go
func (p *Parser) parseImport() ast.Decl {
    pos := p.consume(lexer.IMPORT).Pos
    return &ast.ImportDecl{
        Pos:  pos,
        Path: p.consume(lexer.IDENT).Literal,
    }
}
```

### 4.2 parseEnum

```go
func (p *Parser) parseEnum() ast.Decl {
    pos := p.consume(lexer.ENUM).Pos
    name := p.consume(lexer.IDENT).Literal
    p.consume(lexer.LBRACE)
    
    var values []string
    for !p.match(lexer.RBRACE) && !p.match(lexer.EOF) {
        if p.match(lexer.IDENT) {
            values = append(values, p.consume(lexer.IDENT).Literal)
        }
        if p.match(lexer.COMMA) { p.consume(lexer.COMMA) }
    }
    p.consume(lexer.RBRACE)
    
    return &ast.EnumDecl{Pos: pos, Name: name, Values: values}
}
```

---

## 五、parseStructDecl + parseField

### 5.1 parseStructDecl

```go
func (p *Parser) parseStructDecl(name string, exported bool, pos lexer.Pos) *ast.StructDecl {
    p.consume(lexer.LBRACE)
    var fields []*ast.Field
    for !p.match(lexer.RBRACE) && !p.match(lexer.EOF) {
        if f := p.parseField(); f != nil {
            fields = append(fields, f)
        }
    }
    p.consume(lexer.RBRACE)
    return &ast.StructDecl{
        Pos: pos, Exported: exported, Name: name, Fields: fields,
    }
}
```

### 5.2 parseField（5 种形式）

```go
func (p *Parser) parseField() *ast.Field {
    pos := p.peek().Pos
    f := &ast.Field{Pos: pos}
    
    // ① 类型前缀（可选）
    if p.isTypeStart() {
        f.Type = p.parseTypeRef()
    }
    
    // ② 字段名（必填）
    if !p.match(lexer.IDENT) {
        p.errorf("expected field name, got %s", p.peek().Kind)
        return nil
    }
    f.Name = p.consume(lexer.IDENT).Literal
    
    // ③ 数组后缀：cookie[] cookies;
    if p.match(lexer.LBRACKET) {
        p.consume(lexer.LBRACKET)
        p.consume(lexer.RBRACKET)
        f.Type = &ast.TypeArray{Pos: pos, Elem: f.Type}
    }
    
    // ④ 嵌套 block：cookie cookies{};
    if p.match(lexer.LBRACE) {
        p.consume(lexer.LBRACE)
        depth := 1
        for depth > 0 && !p.match(lexer.EOF) {
            switch p.peek().Kind {
            case lexer.LBRACE: depth++
            case lexer.RBRACE: depth--
            }
            p.pos++
        }
    }
    
    // ⑤ >> 流：h>>
    if p.match(lexer.RRARROW) {
        f.Flow = p.parseFlowChain()
    }
    
    // 可选分号
    if p.match(lexer.SEMI) { p.consume(lexer.SEMI) }
    
    return f
}

// isTypeStart 判断当前 IDENT 是否能作为类型开头
func (p *Parser) isTypeStart() bool {
    if !p.match(lexer.IDENT) { return false }
    name := p.peek().Literal
    switch name {
    case "str", "num", "bool", "byte", "any":
        return true
    }
    return true   // 用户自定义类型也是 IDENT
}
```

### 5.3 parseTypeRef

```go
func (p *Parser) parseTypeRef() ast.TypeRef {
    pos := p.peek().Pos
    
    // 指针：*nio.context
    if p.match(lexer.STAR) {
        p.consume(lexer.STAR)
        elem := p.parseTypeRef()
        return &ast.TypePtr{Pos: pos, Elem: elem}
    }
    
    // 类型名（必填）
    if !p.match(lexer.IDENT) {
        p.errorf("expected type name, got %s", p.peek().Kind)
        return &ast.TypeName{Pos: pos, Name: "any"}
    }
    name := p.consume(lexer.IDENT).Literal
    return &ast.TypeName{Pos: pos, Name: name}
}
```

---

## 六、parseFuncDecl（两种签名形式）

### 6.1 顶层调度

```go
func (p *Parser) parseExport() ast.Decl {
    pos := p.consume(lexer.AT).Pos
    name := p.consume(lexer.IDENT).Literal
    
    // @< params > { ... }
    if p.match(lexer.LT) {
        return p.parseFuncDeclWithAngles(name, true, pos)
    }
    
    // @Name ( params ) { ... }
    if p.match(lexer.LPAREN) {
        return p.parseFuncDeclWithParens(name, true, pos)
    }
    
    // @Name { ... }  ← struct
    return p.parseStructDecl(name, true, pos)
}

func (p *Parser) parseDecl() ast.Decl {
    pos := p.peek().Pos
    name := p.consume(lexer.IDENT).Literal
    
    if p.match(lexer.LPAREN) {
        return p.parseFuncDeclWithParens(name, false, pos)
    }
    if p.match(lexer.LT) {
        return p.parseFuncDeclWithAngles(name, false, pos)
    }
    return p.parseStructDecl(name, false, pos)
}
```

### 6.2 两种签名

```go
func (p *Parser) parseFuncDeclWithParens(name string, exported bool, pos lexer.Pos) *ast.FuncDecl {
    p.consume(lexer.LPAREN)
    params := p.parseParamList(lexer.RPAREN)
    p.consume(lexer.RPAREN)
    
    // 多返回值：func f() (str, err)
    var returns []*ast.Param
    if p.match(lexer.LPAREN) {
        p.consume(lexer.LPAREN)
        returns = p.parseParamList(lexer.RPAREN)
        p.consume(lexer.RPAREN)
    }
    
    p.consume(lexer.LBRACE)
    body := p.parseBlock()
    p.consume(lexer.RBRACE)
    
    return &ast.FuncDecl{
        Pos: pos, Exported: exported,
        Name: name, Params: params, Returns: returns, Body: body,
    }
}

func (p *Parser) parseFuncDeclWithAngles(name string, exported bool, pos lexer.Pos) *ast.FuncDecl {
    p.consume(lexer.LT)
    params := p.parseParamList(lexer.GT)
    p.consume(lexer.GT)
    p.consume(lexer.LBRACE)
    body := p.parseBlock()
    p.consume(lexer.RBRACE)
    
    return &ast.FuncDecl{
        Pos: pos, Exported: exported,
        Name: name, Params: params, Body: body,
    }
}

func (p *Parser) parseParamList(end lexer.Kind) []*ast.Param {
    var params []*ast.Param
    for !p.match(end) && !p.match(lexer.EOF) {
        typ := p.parseTypeRef()
        name := p.consume(lexer.IDENT).Literal
        params = append(params, &ast.Param{
            Pos: typ.Pos(), Type: typ, Name: name,
        })
        if p.match(lexer.COMMA) { p.consume(lexer.COMMA) }
    }
    return params
}
```

---

## 七、parseFlowChain（核心特性）

### 7.1 解析 >> 链

`hello>>out>>stdio.Println` 的解析：

```go
func (p *Parser) parseFlowChain() []*ast.FlowStep {
    var steps []*ast.FlowStep
    for p.match(lexer.RRARROW) {
        p.consume(lexer.RRARROW)
        step := &ast.FlowStep{Pos: p.peek().Pos}
        
        switch p.peek().Kind {
        case lexer.IDENT:
            name := p.consume(lexer.IDENT).Literal
            
            // 成员访问：stdio.Println
            if p.match(lexer.DOT) {
                p.consume(lexer.DOT)
                member := p.consume(lexer.IDENT).Literal
                step.Src = &ast.MemberExpr{
                    Pos: step.Pos, Obj: &ast.IdentExpr{Name: name}, Name: member,
                }
            } else {
                step.Src = &ast.IdentExpr{Name: name}
            }
            
            // 重命名：h>>msg>>
            if p.match(lexer.RRARROW) && p.peekN(1).Kind == lexer.IDENT {
                p.consume(lexer.RRARROW)
                step.As = p.consume(lexer.IDENT).Literal
            }
            
        case lexer.STRING:
            step.Src = &ast.LiteralExpr{Value: p.consume(lexer.STRING).Literal}
            
        default:
            p.errorf("unexpected token after >>: %s", p.peek().Kind)
            return steps
        }
        steps = append(steps, step)
    }
    return steps
}
```

### 7.2 main.mocker 的 flow chain 解析结果

源码：
```mocker
main {
    hello>>out>>stdio.Println
}
```

AST：
```go
&ast.FuncDecl{
    Name: "main",
    Body: &ast.BlockStmt{
        Stmts: [
            &ast.ExprStmt{
                X: &ast.FlowExpr{
                    Steps: [
                        {Src: IdentExpr("hello")},
                        {Src: IdentExpr("out")},
                        {Src: MemberExpr{Obj: IdentExpr("stdio"), Name: "Println"}},
                    ],
                },
            },
        ],
    },
}
```

---

## 八、parseBlock + parseStmt

### 8.1 parseBlock

```go
func (p *Parser) parseBlock() *ast.BlockStmt {
    pos := p.peek().Pos
    block := &ast.BlockStmt{Pos: pos}
    
    for !p.match(lexer.RBRACE) && !p.match(lexer.EOF) {
        if s := p.parseStmt(); s != nil {
            block.Stmts = append(block.Stmts, s)
        }
        if p.match(lexer.SEMI) { p.consume(lexer.SEMI) }
    }
    return block
}
```

### 8.2 parseStmt（语句分发）

```go
func (p *Parser) parseStmt() ast.Stmt {
    switch {
    case p.match(lexer.IF):
        return p.parseIf()
    case p.match(lexer.RETURN):
        return p.parseReturn()
    case p.isTypeStart() || p.match(lexer.IDENT):
        return p.parseVarDeclOrAssign()
    default:
        p.errorf("unexpected statement: %s", p.peek().Kind)
        p.pos++
        return nil
    }
}
```

### 8.3 parseIf / parseReturn

```go
func (p *Parser) parseIf() *ast.Stmt {
    pos := p.consume(lexer.IF).Pos
    cond := p.parseExpr()
    p.consume(lexer.LBRACE)
    body := p.parseBlock()
    p.consume(lexer.RBRACE)
    
    var els ast.Stmt
    if p.match(lexer.ELSE) {
        p.consume(lexer.ELSE)
        if p.match(lexer.IF) {
            els = p.parseIf()
        } else {
            p.consume(lexer.LBRACE)
            els = p.parseBlock()
            p.consume(lexer.RBRACE)
        }
    }
    return &ast.IfStmt{Pos: pos, Cond: cond, Body: body, Else: els}
}

func (p *Parser) parseReturn() ast.Stmt {
    pos := p.consume(lexer.RETURN).Pos
    var val Expr
    if !p.match(lexer.SEMI) && !p.match(lexer.RBRACE) {
        val = p.parseExpr()
    }
    return &ast.ReturnStmt{Pos: pos, Value: val}
}
```

### 8.4 parseVarDeclOrAssign

```go
func (p *Parser) parseVarDeclOrAssign() ast.Stmt {
    pos := p.peek().Pos
    
    var typ ast.TypeRef
    if p.isTypeStart() {
        typ = p.parseTypeRef()
    }
    
    name := p.consume(lexer.IDENT).Literal
    
    // name := value
    if p.match(lexer.DEFINE) {
        p.consume(lexer.DEFINE)
        init := p.parseExpr()
        decl := &ast.VarDecl{Pos: pos, Name: name, Type: typ, Init: init}
        if p.match(lexer.RRARROW) {
            decl.Flow = p.parseFlowChain()
        }
        return decl
    }
    
    // name, name := value  （多变量赋值）
    if p.match(lexer.COMMA) {
        lhs := []ast.Expr{&ast.IdentExpr{Pos: pos, Name: name}}
        for p.match(lexer.COMMA) {
            p.consume(lexer.COMMA)
            n := p.consume(lexer.IDENT).Literal
            lhs = append(lhs, &ast.IdentExpr{Name: n})
        }
        p.consume(lexer.DEFINE)
        rhs := p.parseExpr()
        return &ast.AssignStmt{Pos: pos, Lhs: lhs, Rhs: rhs}
    }
    
    // 裸字段
    decl := &ast.VarDecl{Pos: pos, Name: name, Type: typ}
    if p.match(lexer.RRARROW) {
        decl.Flow = p.parseFlowChain()
    }
    return decl
}
```

---

## 九、parseExpr（Pratt 算法）

### 9.1 为什么用 Pratt

表达式有运算符优先级（`*` 高于 `+`，`.` 最高）。递归下降直接展开会写出大量嵌套函数。**Pratt 算法**用一个循环 + 优先级表优雅处理。

### 9.2 实现

```go
func (p *Parser) parseExpr() ast.Expr {
    return p.parseBinary(1)
}

func (p *Parser) parseBinary(minPrec int) ast.Expr {
    lhs := p.parseUnary()
    
    for p.isOp() && opPrec(p.peek().Kind) >= minPrec {
        op := p.consumeOp()
        prec := opPrec(op.Kind)
        rhs := p.parseBinary(prec + 1)
        lhs = &ast.BinaryExpr{
            Pos: lhs.Pos(), Op: op.Literal, L: lhs, R: rhs,
        }
    }
    return lhs
}

func (p *Parser) parseUnary() ast.Expr {
    return p.parsePrimary()
}

func (p *Parser) parsePrimary() ast.Expr {
    pos := p.peek().Pos
    switch p.peek().Kind {
    case lexer.IDENT:
        e := p.parseIdentOrMemberOrCall()
        return e
    case lexer.STRING:
        return &ast.LiteralExpr{Pos: pos, Value: p.consume(lexer.STRING).Literal}
    case lexer.INT:
        return &ast.LiteralExpr{Pos: pos, Value: p.consume(lexer.INT).Literal}
    case lexer.TRUE:
        p.consume(lexer.TRUE)
        return &ast.LiteralExpr{Pos: pos, Value: true}
    case lexer.FALSE:
        p.consume(lexer.FALSE)
        return &ast.LiteralExpr{Pos: pos, Value: false}
    default:
        p.errorf("unexpected expression: %s", p.peek().Kind)
        return &ast.LiteralExpr{Pos: pos, Value: nil}
    }
}

func (p *Parser) parseIdentOrMemberOrCall() ast.Expr {
    pos := p.peek().Pos
    name := p.consume(lexer.IDENT).Literal
    e := ast.Expr(&ast.IdentExpr{Pos: pos, Name: name})
    
    // 成员访问链：stdio.Println.SubMember
    for p.match(lexer.DOT) {
        p.consume(lexer.DOT)
        member := p.consume(lexer.IDENT).Literal
        e = &ast.MemberExpr{Pos: pos, Obj: e, Name: member}
    }
    
    // 函数调用：c.bind(login)
    if p.match(lexer.LPAREN) {
        p.consume(lexer.LPAREN)
        args := p.parseArgs()
        p.consume(lexer.RPAREN)
        e = &ast.CallExpr{Pos: pos, Fn: e, Args: args}
    }
    
    return e
}

func (p *Parser) parseArgs() []ast.Expr {
    var args []ast.Expr
    for !p.match(lexer.RPAREN) && !p.match(lexer.EOF) {
        args = append(args, p.parseExpr())
        if p.match(lexer.COMMA) { p.consume(lexer.COMMA) }
    }
    return args
}

func (p *Parser) isOp() bool {
    return opPrec(p.peek().Kind) > 0
}

func (p *Parser) consumeOp() lexer.Token {
    t := p.peek()
    p.pos++
    return t
}

func opPrec(k lexer.Kind) int {
    switch k {
    case lexer.OR:                  return 1
    case lexer.AND:                 return 2
    case lexer.EQ, lexer.NE:        return 3
    case lexer.LT, lexer.GT, lexer.LE, lexer.GE: return 4
    case lexer.PLUS, lexer.MINUS:   return 5
    case lexer.STAR, lexer.SLASH:   return 6
    case lexer.DOT:                 return 7   // 成员访问最高
    }
    return 0
}
```

### 9.3 Pratt 优先级表

| 优先级 | 运算符 | 结合性 |
| --- | --- | --- |
| 7 | `.` (成员访问) | 左 |
| 6 | `*` `/` | 左 |
| 5 | `+` `-` | 左 |
| 4 | `<` `>` `<=` `>=` | 左 |
| 3 | `==` `!=` | 左 |
| 2 | `&&` | 左 |
| 1 | `\|\|` | 左 |

---

## 十、完整实现清单

```
compiler/internal/parser/
├── ast.go              ← AST 节点定义（第一步做）
├── parser.go           ← 框架 + parseFile + parseImport + parseEnum
├── parse_decl.go       ← parseExport / parseDecl / parseStructDecl
├── parse_field.go      ← parseField + parseTypeRef
├── parse_func.go       ← parseFuncDecl（两种签名）
├── parse_flow.go       ← parseFlowChain
├── parse_block.go      ← parseBlock + parseStmt
├── parse_expr.go       ← parseExpr（Pratt）
└── parser_test.go      ← 每个 .mocker 一个测试
```

---

## 十一、测试策略

### 11.1 每个 .mocker 文件对应一个测试

```go
package parser

import (
    "testing"
    "Mocker/compiler/internal/parser/ast"
)

func TestParseCookie(t *testing.T) {
    src := []byte(`package cookie
@cookie {
    str Domain;
    str Path;
    bool Secure;
}
`)
    file, errs := Parse(src)
    if len(errs) > 0 { t.Fatalf("errors: %v", errs) }
    
    // 断言
    if file.PkgName != "cookie" { t.Fatal("pkg") }
    sd, ok := file.Decls[0].(*ast.StructDecl)
    if !ok { t.Fatal("not struct") }
    if !sd.Exported { t.Error("not exported") }
    if len(sd.Fields) != 3 { t.Fatal("fields") }
}

func TestParseFlowChain(t *testing.T) {
    src := []byte(`package main
main {
    hello>>out>>stdio.Println
}
`)
    file, errs := Parse(src)
    if len(errs) > 0 { t.Fatalf("errors: %v", errs) }
    // ... 断言 3 个 flow step
}

func TestParseAngleSignature(t *testing.T) {
    src := []byte(`package netio
@< Post(str router) > {
    Method method := Post
}
`)
    file, errs := Parse(src)
    if len(errs) > 0 { t.Fatal(errs) }
    fd := file.Decls[0].(*ast.FuncDecl)
    if fd.Name != "Post" { t.Fatal("name") }
    if len(fd.Params) != 1 { t.Fatal("params") }
}

func TestParseEnum(t *testing.T) {
    src := []byte(`package netio
enum Method {
    Post, Get, Delete,
}
`)
    file, errs := Parse(src)
    if len(errs) > 0 { t.Fatal(errs) }
    ed := file.Decls[0].(*ast.EnumDecl)
    if ed.Name != "Method" { t.Fatal("name") }
    if len(ed.Values) != 3 { t.Fatal("values") }
}

func TestParseErrors(t *testing.T) {
    // 故意错误
    src := []byte(`package cookie @ {`)
    _, errs := Parse(src)
    if len(errs) == 0 { t.Fatal("expected errors") }
}
```

### 11.2 跑遍所有 example

```bash
# 在 parser_test.go 里加一个 TestAllExamples
func TestAllExamples(t *testing.T) {
    files, _ := filepath.Glob("../../example/**/*.mocker")
    files = append(files, glob("../../example/*.mocker")...)
    
    for _, f := range files {
        t.Run(f, func(t *testing.T) {
            src, _ := os.ReadFile(f)
            _, errs := Parse(src)
            // 只打 log，不 fail（这样能看到所有文件的解析情况）
            for _, e := range errs {
                t.Logf("%s: %s", f, e)
            }
        })
    }
}
```

---

## 十二、实施时间表

| 天 | 步骤 | 验收 |
| --- | --- | --- |
| **Day 1-2** | AST 节点定义（ast.go） | `go build` 通过 |
| **Day 3** | Parser 框架（parser.go + parseFile + parseImport + parseEnum） | `cookie.mocker` 能进 parseFile |
| **Day 4** | parseStructDecl + parseField（第 1 版：只支持 `str Name;`） | cookie.mocker 解析通过 |
| **Day 5** | 扩展 parseField（5 种形式） | cookie.mocker + 其他 struct 文件 |
| **Day 6-7** | parseFlowChain | main.mocker 数据流解析通过 |
| **Day 8** | parseFuncDecl（两种签名） | netio.mocker 函数解析通过 |
| **Day 9-10** | parseBlock + parseStmt + parseExpr | 函数体解析通过 |
| **Day 11** | 测试所有 example + 调优 | 全部通过 |
| **Day 12-14** | 错误信息打磨 + 边界 case | 可发布 |

**总计：2 周**

---

## 十三、设计决策记录

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| Parser 算法 | 递归下降 + Pratt 表达式 | 错误信息好、易扩展、自举友好 |
| Lookahead | 1 token（少数情况 2） | DSL 语法不太歧义，1 token 足够 |
| AST 节点粒度 | 每个语法概念一个 struct | 下游 type switch 清晰 |
| 字段表示 | `[]*Field`（不用 map） | 类型安全、保留顺序 |
| 空值表示 | `nil` 指针 | 明确"可选"语义 |
| 错误处理 | 聚合 + 同步点恢复 | 一次报告所有错误，AI 友好 |
| Token 流来源 | lexer 一次性产出 | parser 是同步消费，不需 channel |
| 函数体解析 | 简单递进，先做主流程 | 复杂表达式留到后期 |

---

## 十四、自举考虑

**所有 parser 函数都是"纯函数"**：

```go
func (p *Parser) parseXxx() ast.Node {
    // 输入：p.tok（隐式）、p.pos（隐式）
    // 输出：ast.Node
    // 副作用：仅修改 p.pos 和 p.errors
}
```

**这恰好对应你 DSL 的"纯函数"概念**——Stage 1 翻译时一对一对应：

```go
// Go
func (p *Parser) parseStructDecl(name string, exported bool, pos lexer.Pos) *ast.StructDecl {
    ...
}

// Mocker DSL（机械翻译）
parser.parse_struct_decl :: (name str, exported bool, pos lexer.Pos) -> *ast.StructDecl {
    ...
}
```

**AST 节点本身就是 Go struct**——Stage 1 时几乎是"复制粘贴 + 类型映射"。

---

## 十五、常见坑

### 15.1 忘记检查 EOF

```go
// ❌ 错误
for !p.match(lexer.RBRACE) {
    ...
}
// 如果文件末尾没有 }，会无限循环

// ✅ 正确
for !p.match(lexer.RBRACE) && !p.match(lexer.EOF) {
    ...
}
```

### 15.2 错误恢复太激进

```go
// ❌ 错误：recover 太激进，吃掉太多 token
func (p *Parser) recover() {
    p.pos++   // 跳 1 个就停
}

// ✅ 正确：跳到同步点（分号、}、顶层关键字）
func (p *Parser) recover() {
    sync := map[lexer.Kind]bool{...}
    for !p.match(lexer.EOF) {
        if sync[p.peek().Kind] { return }
        p.pos++
    }
}
```

### 15.3 字符串字面量没处理转义

```go
// ❌ 错误：字符串里直接是 "hello\nworld"，没处理 \n
// ✅ 正确：在 lexer 处理转义，parser 拿到的是真实字符串
```

### 15.4 Pos 没保留

```go
// ❌ 错误：忘记 Pos
type Field struct {
    Type TypeRef
    Name string
}
// 下游错误信息没法定位

// ✅ 正确：每个节点都有 Pos
type Field struct {
    Pos  lexer.Pos   // ← 必须
    Type TypeRef
    Name string
}
```

---

## 十六、与上下游的接口

### 16.1 Parser 暴露给 Semantic 的接口

```go
// compiler/internal/semantic/check.go
package semantic

import "Mocker/compiler/internal/parser/ast"

func Check(file *ast.File) []SemanticError {
    var errs []SemanticError
    
    // Semantic 直接遍历 AST：
    for _, decl := range file.Decls {
        switch d := decl.(type) {
        case *ast.StructDecl:
            errs = append(errs, checkStruct(d)...)
        case *ast.FuncDecl:
            errs = append(errs, checkFunc(d)...)
        case *ast.EnumDecl:
            errs = append(errs, checkEnum(d)...)
        }
    }
    return errs
}
```

**关键**：Semantic **不需要任何 token、不需要任何 lexer**——只看 AST。

### 16.2 Codegen 也只看 AST

```go
// compiler/internal/codegen/codegen.go
func Generate(file *ast.File) ([]byte, error) {
    var buf bytes.Buffer
    
    for _, decl := range file.Decls {
        switch d := decl.(type) {
        case *ast.StructDecl:
            generateStruct(&buf, d)
        case *ast.FuncDecl:
            generateFunc(&buf, d)
        // ...
        }
    }
    
    return buf.Bytes(), nil
}
```

---

## 十七、调试技巧

### 17.1 加 Dump 工具

```go
// compiler/internal/parser/dump.go
func Dump(file *ast.File) string {
    var sb strings.Builder
    dumpFile(&sb, file, 0)
    return sb.String()
}

func dumpFile(sb *strings.Builder, f *ast.File, depth int) {
    indent(sb, depth)
    fmt.Fprintf(sb, "File package=%s\n", f.PkgName)
    for _, d := range f.Decls {
        switch v := d.(type) {
        case *ast.StructDecl:
            fmt.Fprintf(sb, "  StructDecl %s%s {\n", ifExported(v.Exported), v.Name)
            for _, f := range v.Fields {
                fmt.Fprintf(sb, "    %s %s\n", f.Type, f.Name)
            }
            fmt.Fprintln(sb, "  }")
        case *ast.FuncDecl:
            // ...
        }
    }
}
```

### 17.2 CLI 加 dump 子命令

```go
// compiler/cmd/mocker/main.go
case "dump":
    src, _ := os.ReadFile(args[0])
    file, errs := parser.Parse(src)
    if len(errs) > 0 { ... }
    fmt.Println(parser.Dump(file))
```

```bash
mocker dump example/cookie.mocker
# 输出：
# File package=cookie
#   StructDecl cookie {
#     str Domain
#     str Path
#     bool Secure
#   }
```

**Stage 1 调试时救命工具。**

---

## 十八、总结

### 核心思想

> **AST 是中央数据结构，parser 是把 token 流组装成 AST 的程序。**

### 实现路径

1. **先 AST 节点**（数据结构）
2. **再 Parser 框架**（parseFile + 辅助函数）
3. **从最简单的开始**（parseImport + parseEnum）
4. **逐步扩展**（struct → field → flow → func → block → expr）

### 时间预算

**2 周** 可以搞定。

### 验收标准

```bash
# 跑通所有 example
for f in example/**/*.mocker; do
    go run ./cmd/mocker dump "$f" || echo "FAIL: $f"
done
```

**全部通过 = parser 完成。**

---

## 附录 A：完整 Token 类型参考

（来自你的 lexer，请按实际情况调整）

```go
// 关键字
PACKAGE, IMPORT, AT, ENUM,
IF, ELSE, RETURN,
TRUE, FALSE, ANY,

// 字面量
IDENT, STRING, INT, FLOAT,

// 分隔符
LBRACE, RBRACE, LBRACKET, RBRACKET,
LPAREN, RPAREN, LT, GT,
SEMI, COMMA, COLON, DOT,

// 运算符
PLUS, MINUS, STAR, SLASH,
EQ, NE, LT_REL, GT_REL, LE, GE,
AND, OR, NOT,
DEFINE,   // :=
RRARROW,  // >>
LARROW,   // <<
AT,       // @

// 特殊
NEWLINE, INDENT, DEDENT,
EOF,
```

## 附录 B：与上游 Lexer 的契约

```go
// compiler/internal/lexer/token.go
type Token struct {
    Kind    Kind
    Literal string   // 原始字面量
    Pos     Pos      // 行列位置
}

type Pos struct {
    Line int
    Col  int
    Off  int    // 字节偏移
}
```

Parser 假定：
- Lexer 一次性产出所有 token
- 每个 token 带 Pos
- 关键 token 是关键字而非 IDENT（`package`、`import`、`enum` 等）
- `@` 是独立 token，不与后续 IDENT 合并

## 附录 C：参考实现（精简版完整代码）

```go
// 一个完整的 parser.go（约 400 行）见项目 compiler/internal/parser/parser.go
// 这里不再重复，重点参考上面各节的子 parser 实现
```

---

> **下一步**：按 Day 1-2 开始写 `ast.go`，跑通 `go build`。然后按 Day 3 开始写 parser 框架。
>
> **两周后**，你就有了一个能解析所有 .mocker 文件的 parser。AST 是后面所有阶段的基础，**多花一天设计，省后面一月返工**。

---

## 附录 D：M4.5 节点 body 内的 sub-graph 语法

### 设计动机

M4.4 把拓扑集中在 main 节点 body，但实际写程序时，每个节点的 body 自包含地描述自己的 sub-graph 更自然：

```ce
hello {
    h := "hello"
    world w;             // sub-instance 声明
    h <add_str> w        // sub-edge 连接
    >> out_str           // port 声明
    stdio.Println p;     // 外部 sub-instance 引用
    out_str >> p.msg;    // 内部 flow
}
```

M4.5 把这种语法纳入 parseStructMember，新增 2 种形式。

### 节点 body 内的所有 StructMember 形式（M4.5）

| 形式 | 例子 | AST 类型 |
|------|------|---------|
| 0 | `>> str hey` | `PortDecl`（入度） |
| 1 | `str Domain` | `FieldDecl`（强类型字段） |
| 1.5 | `str name = expr` | `VarDecl`（显式类型） |
| 2 | `name := expr` | `VarDecl`（类型推断） |
| 3 | `h / h >> / h>>msg` | `FlowDecl`（出度） |
| **4** | `TypeName varName;` | **`SubInstanceDecl`**（M4.5 新增） |
| **5** | `src <edge> dst` | **`SubEdgeDecl`**（M4.5 新增） |

### 形式 4：SubInstanceDecl

```ce
TypeName varName;
```

- 例：`world w;`（在 hello body 内）
- 例：`stdio.Println p;`
- 与 `InstanceDecl`（main body 专用）的区别：
  - `InstanceDecl`：main 节点 body，作为 entry
  - `SubInstanceDecl`：任何节点 body，作为内部子实例

### 形式 5：SubEdgeDecl

```ce
src <edge_name> dst
```

- 例：`h <add_str> w`（在 hello body 内）
- 与 `EdgeConnDecl`（main body 专用）的区别：
  - `EdgeConnDecl`：main body，作为入口拓扑
  - `SubEdgeDecl`：任何节点 body，作为内部子图连接

### 形式 3 扩展：FlowDecl 支持内部 flow

```ce
src                                // 裸出度（隐式 emit 到自己的 output）
src >>                             // 显式出度
src >> dst.attr;                    // 内部 flow 到 sub-instance 的 input（M4.5 扩展）
```

- 例：`out_str >> p.msg;`（在 hello body 内，p 是 sub-instance）
- codegen 时把这种 flow 转成 `p.block0(out_str)`（调 sub-instance 的方法）

### parseStructMember 的派发逻辑

```go
func parseStructMember() ast.StructMember {
    tok := p.peek()

    // 形式 0：>> 开头（入度声明）
    if tok.Type == TypeOP_RRARROW { return p.parsePortDecl() }

    // 形式 5（M4.5 新增）：SubEdgeDecl `src <edge> dst`
    if tok.Type == TypeID && p.peekN(1).Type == TypeOP_LT {
        return p.parseSubEdgeDecl()
    }

    // 形式 4（M4.5 新增）：SubInstanceDecl `TypeName varName;`
    if tok.Type == TypeID && p.peekN(1).Type == TypeID {
        return p.parseSubInstanceDecl()
    }
    // 跨包：`pkg.Node varName;`
    if tok.Type == TypeCALL && p.peekN(1).Type == TypeID {
        return p.parseSubInstanceDecl()
    }

    // 形式 1：typed field
    if isTypeStart(tok.Type) && p.isTypedFieldStart() { ... }

    // 形式 2：type-inferred VarDecl
    if tok.Type == TypeID && p.peekN(1).Type == TypeOP_DEFINE { ... }

    // 形式 3：FlowDecl
    if tok.Type == TypeID { return p.parseFlowDecl() }

    ...
}
```

### 与 main body 的差异

main body 用 `InstanceDecl` + `EdgeConnDecl`（同 main package 范围内的 instance 声明 + edge）
节点 body 用 `SubInstanceDecl` + `SubEdgeDecl`（任意深度嵌套的 sub-graph）

为什么用不同类型？**语义不同**：
- `InstanceDecl` 是 entry 声明（main 用）
- `SubInstanceDecl` 是嵌套子实例声明（递归用）

### 完整例子（hello world）

```ce
hello {
    h := "hello"
    world w;
    h <add_str> w
    >> out_str
    stdio.Println p;
    out_str >> p.msg;
}

world {
    >> str words
    new := words + " world!"
    new >>
}

<add_str> {
    hello.h >> world.words
    world.new >> hello.out_str
}

main {
    hello happy;
}
```

### 参考

- `internal/parser/ast/ast.go` — `SubInstanceDecl` + `SubEdgeDecl` 定义
- `internal/parser/parse_decl.go` — `parseStructMember` 派发
- [constructor-orchestrator-design.md](../circle/docs/constructor-orchestrator-design.md) — 完整设计
- [execution.md](../circle/docs/execution.md) — 整体执行流程
