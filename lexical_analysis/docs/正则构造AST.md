# 从正则表达式构造 AST

> glex 流水线 M2 的核心：把字符串形式的正则解析成一棵 AST。
> 对应实现：`internal/regex/parse.go` + `internal/regex/ast.go`。

---

## 一、整体流程

```
正则字符串      "[+\-]?[0-9]+(\.[0-9]+)?"
    │
    ▼  字符流（带 position 指针）
┌──────────────────┐
│ parser 递归下降  │   ← 本文档核心
└────────┬─────────┘
         │
         ▼
AST 树（Go struct）
    │
    ▼  M3 Thompson
NFA 状态图
```

**一个 parser 干两件事**：
1. **切字符**：维护 pos 指针，按需前进
2. **建节点**：根据当前字符决定建什么类型的节点

---

## 二、关键概念：运算符优先级

正则表达式的运算符有严格的优先级，**优先级决定了解析时的函数调用顺序**。

### 优先级（从低到高）

| 优先级 | 运算符 | 语法 | 例子 |
| --- | --- | --- | --- |
| 0（最低） | **Union** | `A \| B` | `if\|else` |
| 1 | **Concat** | `A B`（隐式邻接） | `ab` |
| 2 | **Quantifier** | `A*` `A+` `A?` `A{n,m}` | `a+` |
| 3（最高） | **Atom** | 字面字符 / `\x` / `[..]` / `(..)` / `.` | `[abc]` |

### 优先级例子

| 正则 | 解析结果 | 说明 |
| --- | --- | --- |
| `ab\|c` | `(ab)\|c` | concat > union |
| `a\|bc` | `a\|(bc)` | concat > union |
| `ab*` | `a(b*)` | quantifier > concat |
| `(ab)*` | `(ab)*` | group 提优先级 |
| `a\|b*` | `a\|(b*)` | quantifier > union |

### 优先级在代码里的体现

> **优先级越高，对应的 parse 函数越深**。低优先级的函数调用高优先级的函数。

```go
parseUnion()      // 优先级 0
    → parseConcat()    // 优先级 1
        → parseQuantified()   // 优先级 2
            → parseAtom()      // 优先级 3
```

调用链就是优先级链，**自然表达优先级**。

---

## 三、完整 BNF 文法

把上述优先级写成 BNF：

```bnf
regex      = union
union      = concat ('|' concat)*                          # 左结合
concat     = quantified*                                    # 零或多段
quantified = atom ('*' | '+' | '?' | repeat)?              # 可选量词
atom       = literal | escape | charclass | group | '.'
literal    = <除元字符外的任何字符>
escape     = '\' <任意字符>                                 # \d \D \w \W \s \S \n \t \\ \( \) ...
charclass  = '[' '^'? items ']'
items      = item*
item       = range | escape
range      = char ('-' char)?
group      = '(' regex ')'
repeat     = '{' number (',' number?)? '}'                 # {n} {n,} {n,m}
number     = [0-9]+
```

---

## 四、递归下降：每个优先级一个函数

### 4.1 parseUnion（最低优先级）

```go
func (p *parser) parseUnion() (Regex, error) {
    left, err := p.parseConcat()          // ① 先 parse 左边
    if err != nil { return nil, err }
    for p.peek() == '|' {                  // ② 看有没有 |
        p.next()                          //    消耗 |
        right, err := p.parseConcat()     // ③ parse 右边
        if err != nil { return nil, err }
        left = &Union{Left: left, Right: right}  // ④ 左结合合并
    }
    return left, nil
}
```

**关键点**：左结合 — `a|b|c` = `(a|b)|c`，不是 `a|(b|c)`。

### 4.2 parseConcat（隐式连接）

```go
func (p *parser) parseConcat() (Regex, error) {
    var parts []Regex
    for !p.eof() && p.peek() != '|' && p.peek() != ')' {
        part, err := p.parseQuantified()  // ① 反复 parse quantified
        if err != nil { return nil, err }
        parts = append(parts, part)
    }
    // ② 把多段左折叠成 Concat
    return foldConcat(parts), nil
}
```

`abc` 不需要显式连接符，**靠"前一个 quantified 紧接后一个 quantified"**实现隐式 concat。

### 4.3 parseQuantified（处理量词后缀）

```go
func (p *parser) parseQuantified() (Regex, error) {
    atom, err := p.parseAtom()             // ① 先 parse 原子
    if err != nil { return nil, err }
    switch p.peek() {
    case '*': p.next(); return &Star{Inner: atom}, nil
    case '+': p.next(); return &Plus{Inner: atom}, nil
    case '?': p.next(); return &Optional{Inner: atom}, nil
    case '{': return p.parseRepeat(atom)
    }
    return atom, nil                        // ② 没量词，原样返回
}
```

**关键点**：量词是**后缀**（suffix），作用在**紧邻的原子**上。

### 4.4 parseAtom（最高优先级）

```go
func (p *parser) parseAtom() (Regex, error) {
    ch := p.peek()
    switch ch {
    case '[':  return p.parseCharClass()    // 字符类
    case '\\': p.next(); return p.parseEscape()
    case '(':  return p.parseGroup()        // 分组
    case '.':  p.next(); return &Dot{}, nil
    case 0:    return nil, p.errf("unexpected EOF")
    default:   p.next(); return &Literal{Ch: ch}, nil
    }
}
```

---

## 五、特殊节点的解析

### 5.1 字符类 `[..]`

```
[a-zA-Z_]    → CharClass{a-z, A-Z, _}
[^abc]       → CharClass{a, b, c}, Negate=true
[\-\+\d]     → CharClass{-, +, 0-9}
```

**关键点**：
- `[` 后跟可选 `^`（取反）
- 范围 `a-z` 在 parser 阶段**展开**成字符数组
- 字符类内的 `\d` `\w` `\s` 也展开成字符数组（**消灭转义**）
- AST 里没有"范围"概念，全是 flat 字符数组

```go
func (p *parser) parseCharClass() (Regex, error) {
    p.next()  // '['
    negate := false
    if p.peek() == '^' { negate = true; p.next() }

    var chars []byte
    for !p.eof() && p.peek() != ']' {
        ch, err := p.readCharClassChar()  // 处理转义
        if err != nil { return nil, err }

        // 检查是不是 a-z 范围
        if p.peek() == '-' && p.pos+1 < len(p.s) && p.s[p.pos+1] != ']' {
            p.next()  // '-'
            end, err := p.readCharClassChar()
            if err != nil { return nil, err }
            for c := ch; c <= end; c++ {
                chars = append(chars, c)  // 展开范围
            }
        } else {
            chars = append(chars, ch)
        }
    }
    if p.eof() { return nil, p.errf("unterminated [") }
    p.next()  // ']'
    return &CharClass{Chars: dedup(chars), Negate: negate}, nil
}
```

### 5.2 转义 `\x`

支持 3 类转义：
- **简写类**：`\d \D \w \W \s \S` → CharClass
- **控制字符**：`\n \t \r \0` → Literal
- **字面转义**：`\( \) \[ \] \\ \. \* \+ \? \| \^ \$ \-` → Literal
- **其它任意** `\x` → Literal(`x`)

```go
func (p *parser) parseEscape() (Regex, error) {
    ch := p.next()  // 调用前已读 \
    switch ch {
    case 'd': return &CharClass{Chars: rangeBytes('0', '9'), Negate: false}, nil
    case 'D': return &CharClass{Chars: rangeBytes('0', '9'), Negate: true}, nil
    case 'w': return &CharClass{Chars: wordChars(), Negate: false}, nil
    // ... \W \s \S
    case 'n': return &Literal{Ch: '\n'}, nil
    // ... \t \r
    default:  return &Literal{Ch: ch}, nil  // 其它字面
    }
}
```

### 5.3 分组 `(..)`

```go
func (p *parser) parseGroup() (Regex, error) {
    p.next()  // '('
    inner, err := p.parseUnion()           // 内部是完整 regex
    if err != nil { return nil, err }
    if p.peek() != ')' {
        return nil, p.errf("expected ')', got %q", p.peek())
    }
    p.next()  // ')'
    return &Group{Inner: inner}, nil
}
```

### 5.4 重复 `{n,m}`

**严格模式**：`{` 必须是合法的重复，不能退化成字面 `{`：

```go
func (p *parser) parseRepeat(atom Regex) (Regex, error) {
    p.next()  // '{'
    min, max, ok, err := p.parseRange()
    if err != nil { return nil, err }
    if !ok {
        return nil, p.errf("invalid repeat: expected number after '{'")
    }
    if p.peek() != '}' {
        return nil, p.errf("expected '}' in repeat")
    }
    p.next()  // '}'
    return &Repeat{Min: min, Max: max, Inner: atom}, nil
}
```

支持的 3 种形式：
- `{n}` → `Min=Max=n`
- `{n,}` → `Min=n, Max=-1`（无穷）
- `{n,m}` → `Min=n, Max=m`

---

## 六、完整示例：解析 `[+\-]?[0-9]+(\.[0-9]+)?`

### 输入
```
[+\-]?[0-9]+(\.[0-9]+)?
```

### 步骤追踪

**parseUnion → parseConcat（开始）**

```
pos=0  读到 '['  → parseCharClass
        范围 [+\-] → CharClass{+, -}
        读到 ']'  → 返回 CharClass{+,-}
        紧跟 '?'  → Optional
        返回 Optional(CharClass{+,-})

pos=8  读到 '['  → parseCharClass
        范围 [0-9] → CharClass{0..9}
        读到 ']'  → 返回 CharClass{0..9}
        紧跟 '+'  → Plus
        返回 Plus(CharClass{0..9})

pos=14 读到 '('  → parseGroup
        读到 '\'  → parseEscape
        读到 '.'  → Literal('.')
        读到 '['  → parseCharClass
        范围 [0-9] → CharClass{0..9}
        读到 ']'  → 返回 CharClass{0..9}
        紧跟 '+'  → Plus
        返回 Plus(CharClass{0..9})
        读到 ')'  → Group wrapping
        返回 Group(Concat(Literal('.'), Plus(CharClass{0..9})))
        紧跟 '?'  → Optional
        返回 Optional(Group(Concat(Literal('.'), Plus(CharClass{0..9}))))

pos=27 EOF → parseConcat 结束
        parseUnion 看到 EOF（无 |）→ 返回
```

### 生成的 AST

```
Concat
├── Optional
│   └── CharClass [+-]
├── Plus
│   └── CharClass [0-9]
└── Optional
    └── Group
        └── Concat
            ├── Literal "."
            └── Plus
                └── CharClass [0-9]
```

### 用 `Print()` 验证

```go
r, _ := regex.Parse(`[+\-]?[0-9]+(\.[0-9]+)?`)
fmt.Println(regex.Print(r))
```

输出（树形）：

```
Concat
├── Optional
│   └── CharClass [+-]
├── Plus
│   └── CharClass [0-9]
└── Optional
    └── Group
        └── Concat
            ├── Literal "."
            └── Plus
                └── CharClass [0-9]
```

---

## 七、错误处理与位置信息

### 7.1 ParseError 结构

```go
type ParseError struct {
    Msg   string  // 错误描述
    Pos   int     // 出错位置（byte 偏移）
    Input string  // 原始输入（用于显示上下文）
}
```

### 7.2 错误消息示例

```
regex: parse error at position 12: expected '}' in repeat
  near: "a{3,5b"
              ^
```

`^` 指向出错位置。

### 7.3 错误恢复策略

我们采用**快速失败**（fail-fast）：

- 遇到语法错误立即停止，返回错误
- 调用方可以选择重试、跳过、降级
- 不在错误处"猜测正确语法"（避免歧义）

---

## 八、Go 实现骨架

完整的 parser 包含 4 层调用：

```go
// internal/regex/parse.go

type parser struct {
    s   string
    pos int
}

// 入口
func Parse(input string) (Regex, error) {
    p := &parser{s: input}
    r, err := p.parseUnion()      // 最低优先级
    if err != nil { return nil, err }
    if !p.eof() {
        return nil, p.errf("unexpected character %q", p.peek())
    }
    return r, nil
}

// 4 个 parse 函数（按优先级从低到高）
func (p *parser) parseUnion()      (Regex, error) { ... }  // 优先级 0
func (p *parser) parseConcat()     (Regex, error) { ... }  // 优先级 1
func (p *parser) parseQuantified() (Regex, error) { ... }  // 优先级 2
func (p *parser) parseAtom()       (Regex, error) { ... }  // 优先级 3

// 辅助：parser
func (p *parser) peek() byte  { ... }
func (p *parser) next() byte  { ... }
func (p *parser) eof() bool   { ... }
func (p *parser) errf(...) error { ... }
```

### 完整调用链

```
Parse(input)
    └─ parseUnion           [Union 优先级]
        └─ parseConcat      [Concat 优先级]
            └─ parseQuantified  [Quantifier 优先级]
                └─ parseAtom [Atom 优先级]
                    ├─ parseCharClass (字符类)
                    ├─ parseEscape (转义)
                    └─ parseGroup (分组)
                ← 视情况返回
            ← 视情况返回
        ← 视情况返回
    ← 视情况返回
```

---

## 九、测试策略

### 9.1 单元测试覆盖

| 测试类别 | 例子 |
| --- | --- |
| **基础节点** | `a` → Literal; `.` → Dot |
| **字符类** | `[abc]`, `[a-z]`, `[^abc]`, `[\d]` |
| **量词** | `a*`, `a+`, `a?`, `a{3}`, `a{2,5}`, `a{2,}` |
| **转义** | `\d`, `\+`, `\(`, `\n` |
| **优先级** | `ab\|c`, `a\|bc`, `ab*`, `(ab)*` |
| **真实 token** | ID, NUM, REAL, OP_EQ |
| **错误** | `[abc`, `(ab`, `[z-a]`, `a{`, `a{-1}`, `a{5,2}` |

### 9.2 表驱动测试模式

```go
func TestParse(t *testing.T) {
    tests := []struct {
        name, in, want string
    }{
        {"literal", "a", "Literal \"a\""},
        {"charclass", "[a-z]", "CharClass [a-z]"},
        {"star", "a*", "Star\n└── Literal \"a\""},
        // ...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse(tt.in)
            if err != nil { t.Fatal(err) }
            if s := strings.TrimSpace(Print(got)); s != tt.want {
                t.Errorf("got:\n%s\nwant:\n%s", s, tt.want)
            }
        })
    }
}
```

---

## 十、扩展语法

新增语法时，按以下流程：

| 步骤 | 内容 |
| --- | --- |
| 1 | 在 `ast.go` 加新节点类型 |
| 2 | 在 `parse.go` 的 `parseAtom` / `parseQuantified` 加新 case |
| 3 | 添加 `internal/regex/parse_test.go` 测试 |
| 4 | 在 `print.go` 加 label 支持 |
| 5 | 在 `print.go` 的 `nodeChildren` 加 children |
| 6 | （M3 时）在 `internal/nfa/thompson.go` 加 Thompson 规则 |

**已有节点不受影响**——switch case 隔离。

---

## 十一、关键设计决策

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| 解析策略 | **递归下降** | AST 结构和 BNF 一一对应，代码即文档 |
| 优先级表达 | **函数调用层级** | 低优先级 → 调高优先级，调用链就是优先级链 |
| 字符类范围 | **parser 阶段展开** | AST 里全是 flat 字符数组，下游不用处理范围 |
| 重复严格性 | **严格**（`{x` 报错） | 比 Go regex 的宽松行为更友好，错误立即暴露 |
| 转义展开 | **parser 阶段** | `\d` 在 AST 里就是 CharClass，Thompson 不用处理 |
| 错误信息 | **含位置** | 便于定位 + 修复 |
| 错误恢复 | **快速失败** | 简单可靠，不掩盖错误 |

---

## 十二、与 Thompson 构造的衔接

parser 产出的 AST 直接喂给 M3：

```go
// M2 产出
ast, _ := regex.Parse(`[+\-]?[0-9]+`)

// M3 输入
nfa := nfa.New()
start, end := nfa.Build(ast, "NUM")
```

**AST 是 M2 → M3 的接口契约**：
- M2 承诺产出 `Regex` 树
- M3 承诺接收 `Regex` 树

两个阶段**互不依赖内部实现**——只通过 `internal/regex/ast.go` 里的 11 个 struct 类型对接。

---

> **总结**：从正则到 AST = **BNF 文法 → 递归下降 parser → 树形数据结构**。优先级靠函数调用层级表达，字符类在 parser 阶段就展开成 flat 数组，转义也提前消灭。产出的 AST 是 M3 Thompson 构造的输入。
