# glex NFA 架构设计（M3）

> glex 流水线 M3：从 AST 构造 NFA，**单 token 独立接口**，**迭代式 Thompson**，**Builder 模式**，**goccy/go-graphviz 画图**。
> 对应实现：`internal/nfa/`（`nfa.go` + `builder.go` + `rules.go` + `build.go` + `draw.go`）+ `internal/dsl/dsl.go`（加 NFA 字段）

---

## 一、定位与目标

### 在 glex 流水线里

```
DSL 文件 (.glex)
    │
    ▼  M1: dsl.ReadFile
SpecFile { Tokens: [Spec{ AST, ... }] }
    │
    ▼  M2: regex.Parse
SpecFile { Tokens: [Spec{ AST, ... }] }   ← AST 填好
    │
    ▼  M3: 本文档
SpecFile { Tokens: [Spec{ AST, NFA, ... }] }   ← 每个 token 独立 NFA
    │
    ▼  M4: 子集构造
SpecFile { DFA }
    │
    ▼  M6: codegen
生成的 lexer 包
```

### 本期目标

| 维度 | 目标 |
| --- | --- |
| **接口** | `BuildNFA(ast) → NFA*` —— 单 token 独立 |
| **算法** | 迭代式 Thompson（不用递归，避免栈溢出） |
| **数据** | NFA 结构体自管 fragment 栈 |
| **装配** | NFA 装到 Spec 上 |
| **画图** | 用 `goccy/go-graphviz` 出 PNG |

### 不在本期目标

- NFA → DFA 转换（M4）
- DFA 最小化（M5）
- 多 token NFA 合并（M4 之前不做）

---

## 二、核心设计哲学：NFA + Builder 双结构

### 设计原则：**数据与构造状态彻底分离**

| 对象 | 角色 | 生命周期 | 谁访问 |
| --- | --- | --- | --- |
| **`NFA`** | 纯数据（构造完的产物） | 长期（用户的最终交付物） | Builder 写，用户读 |
| **`Builder`** | 构造状态（fragStack、travStack） | 短期（构造完即丢弃） | 仅 Build 算法 |

**对应 Go 习惯**：
- `bytes.Buffer` / `strings.Builder` —— 写时是 builder，读时是字符串
- `json.New().Encode()` —— 写时是 Encoder，读时是 []byte
- 我们这里：`nfa.NewBuilder().Build(ast).NFA()` —— 写时是 Builder，读时是 NFA

### 错误做法 vs 正确做法

| 错误 | 正确 |
| --- | --- |
| NFA 上挂 `fragStack`（数据/构造混合） | NFA 纯数据，Builder 管 fragStack |
| 遍历栈带 `s, e` 字段 | 遍历栈只 `{node, visited}` |
| 构造状态污染用户数据 | 构造完 Builder 丢弃，NFA 干净 |

```go
// ✅ NFA 是纯数据
type NFA struct {
    Start, NextID int
    Accepts      []int
    AcceptTags   map[int]string
    Trans, Epsilon map[int]map[byte][]int
    // 没有 fragStack！
}

// ✅ Builder 装"构造时辅助"
type Builder struct {
    nfa       *NFA
    fragStack []fragment
    travStack []travEntry
}
```

> **Thompson 是"一层层包裹"——这天然是栈语义。** 栈放在 Builder 上，构造完 Builder 整个丢掉，NFA 干干净净。

---

## 三、NFA 结构体（纯数据）

```go
// internal/nfa/nfa.go
package nfa

// NFA 是构造完后的纯数据。
// 用户拿到的就是它，构造状态（fragStack 等）都在 Builder 里。
type NFA struct {
    Start      int                       // 起始状态 ID
    Accepts    []int                     // 所有接受态 ID
    AcceptTags map[int]string            // 接受态 → token key
    Trans      map[int]map[byte][]int    // 字符边
    Epsilon    map[int][]int             // ε 边
    NextID     int                       // 下一个可用状态 ID
}

func New() *NFA {
    return &NFA{
        AcceptTags: make(map[int]string),
        Trans:      make(map[int]map[byte][]int),
        Epsilon:    make(map[int][]int),
    }
}

// ── 基础操作（Builder 也用）──
func (n *NFA) NewState() int {
    id := n.NextID
    n.NextID++
    return id
}

func (n *NFA) AddEpsilon(from, to int) {
    n.Epsilon[from] = append(n.Epsilon[from], to)
}

func (n *NFA) AddTrans(from int, ch byte, to int) {
    if n.Trans[from] == nil {
        n.Trans[from] = make(map[byte][]int)
    }
    n.Trans[from][ch] = append(n.Trans[from][ch], to)
}
```

### 干净

```go
// 用户的视角
nfa := builder.NFA()  // 一个干净的 struct，没有任何"构造时残留"
fmt.Println(nfa.Start)
fmt.Println(nfa.Accepts)
```

---

## 四、Builder 结构体 + Thompson 规则

### 4.1 Builder 结构体

```go
// internal/nfa/builder.go
package nfa

import "lexical_analysis/internal/regex"

type travEntry struct {
    node    regex.Regex
    visited bool
}

type fragment struct {
    s, e int
}

// Builder 是 NFA 的构造器。
// 装"构造时辅助状态"（fragment 栈 + 遍历栈），
// 构造完可丢弃，最终只留下 b.nfa。
type Builder struct {
    nfa       *NFA
    fragStack []fragment
    travStack []travEntry
}

func NewBuilder() *Builder {
    return &Builder{
        nfa:       New(),
        fragStack: make([]fragment, 0, 16),
        travStack: make([]travEntry, 0, 16),
    }
}

// 构造完，调用方取出纯数据
func (b *Builder) NFA() *NFA { return b.nfa }

// ── fragment 栈操作 ──
func (b *Builder) pushFrag(s, e int) {
    b.fragStack = append(b.fragStack, fragment{s, e})
}
func (b *Builder) popFrag() (s, e int) {
    f := b.fragStack[len(b.fragStack)-1]
    b.fragStack = b.fragStack[:len(b.fragStack)-1]
    return f.s, f.e
}
```

### 4.2 Thompson 规则 = Builder 的方法

每个 Thompson 规则都是 **Builder 的方法**：
1. 从 `b.fragStack` pop 出子片段
2. 在 `b.nfa` 上加新状态、加 ε 边
3. 把新片段 push 回 `b.fragStack`

```go
// internal/nfa/rules.go

// 叶子：Literal / CharClass / Dot / Empty
func (b *Builder) processLeaf(r regex.Regex, tokenKey string) (s, e int) {
    s, e = b.nfa.NewState(), b.nfa.NewState()
    switch a := r.(type) {
    case *regex.Literal:
        b.nfa.AddTrans(s, a.Ch, e)
    case *regex.CharClass:
        for _, ch := range a.Chars {
            b.nfa.AddTrans(s, ch, e)
        }
    case *regex.Dot:
        for c := 0; c < 256; c++ {
            b.nfa.AddTrans(s, byte(c), e)
        }
    case *regex.Empty:
        b.nfa.AddEpsilon(s, e)
    }
    b.nfa.AcceptTags[e] = tokenKey
    b.nfa.Accepts = append(b.nfa.Accepts, e)
    return s, e
}

// Concat: pop right, pop left, 加 1 条 ε
func (b *Builder) concatRule() (s, e int) {
    rs, re := b.popFrag()    // 栈顶 = right（左子先入栈）
    ls, le := b.popFrag()    // 下面 = left
    b.nfa.AddEpsilon(le, rs)
    return ls, re
}

// Union: pop right, pop left, 新建 s,e, 加 4 条 ε
func (b *Builder) unionRule() (s, e int) {
    rs, re := b.popFrag()
    ls, le := b.popFrag()
    s, e = b.nfa.NewState(), b.nfa.NewState()
    b.nfa.AddEpsilon(s, ls)
    b.nfa.AddEpsilon(s, rs)
    b.nfa.AddEpsilon(le, e)
    b.nfa.AddEpsilon(re, e)
    return s, e
}

// Star: pop inner, 新建 s,e, 加 4 条 ε（含自环）
func (b *Builder) starRule() (s, e int) {
    is, ie := b.popFrag()
    s, e = b.nfa.NewState(), b.nfa.NewState()
    b.nfa.AddEpsilon(s, is)   // 进入
    b.nfa.AddEpsilon(s, e)    // 跳过（0 次）
    b.nfa.AddEpsilon(ie, e)   // 退出
    b.nfa.AddEpsilon(ie, is)  // 自环
    return s, e
}

// Plus: pop inner，复制 fragment 调 starRule，焊接
func (b *Builder) plusRule() (s, e int) {
    is, ie := b.popFrag()
    b.pushFrag(is, ie)        // 复制一份给 StarRule 用
    ss, se := b.starRule()
    b.nfa.AddEpsilon(ie, ss)  // 焊接
    return is, se
}

// Optional: pop inner, 新建 s,e, 加 3 条 ε
func (b *Builder) optionalRule() (s, e int) {
    is, ie := b.popFrag()
    s, e = b.nfa.NewState(), b.nfa.NewState()
    b.nfa.AddEpsilon(s, is)   // 进入
    b.nfa.AddEpsilon(s, e)    // 跳过（0 次）
    b.nfa.AddEpsilon(ie, e)   // 退出
    return s, e
}

// Group: pop inner，透传
func (b *Builder) groupRule() (s, e int) {
    return b.popFrag()
}

// Repeat: {n}, {n,}, {n,m}
// 实现见 §七
```

---

## 五、Build 入口：Builder 的方法

**遍历栈**和 **fragment 栈**都是 Builder 自己的字段，**主算法是 Builder 的方法**。

```go
// internal/nfa/build.go
package nfa

import "lexical_analysis/internal/regex"

func isLeaf(r regex.Regex) bool {
    switch r.(type) {
    case *regex.Literal, *regex.CharClass, *regex.Dot, *regex.Empty:
        return true
    }
    return false
}

// Build 是 NFA 构造的主入口
func (b *Builder) Build(root regex.Regex, tokenKey string) (s, e int) {
    b.travStack = append(b.travStack, travEntry{node: root})

    for len(b.travStack) > 0 {
        // 弹栈
        top := b.travStack[len(b.travStack)-1]
        b.travStack = b.travStack[:len(b.travStack)-1]

        if top.visited {
            // ── combine 阶段 ──
            switch a := top.node.(type) {
            case *regex.Concat:
                ns, ne := b.concatRule()
                b.pushFrag(ns, ne)
            case *regex.Union:
                ns, ne := b.unionRule()
                b.pushFrag(ns, ne)
            case *regex.Star:
                ns, ne := b.starRule()
                b.pushFrag(ns, ne)
            case *regex.Plus:
                ns, ne := b.plusRule()
                b.pushFrag(ns, ne)
            case *regex.Optional:
                ns, ne := b.optionalRule()
                b.pushFrag(ns, ne)
            case *regex.Repeat:
                ns, ne := b.repeatRule(a)
                b.pushFrag(ns, ne)
            case *regex.Group:
                ns, ne := b.groupRule()
                b.pushFrag(ns, ne)
            }
            continue
        }

        // ── 首次访问 ──
        if isLeaf(top.node) {
            s, e := b.processLeaf(top.node, tokenKey)
            b.pushFrag(s, e)
            continue
        }

        // 内部节点：自己二次入栈 + 子节点入栈
        b.travStack = append(b.travStack, travEntry{node: top.node, visited: true})
        b.pushChildren(top.node)
    }

    // 弹最后一个 fragment，就是整棵 AST 的 (s, e)
    s, e = b.popFrag()
    b.nfa.Start = s
    return s, e
}

// 子节点入栈（右先左后 → 左先 pop 完）
func (b *Builder) pushChildren(r regex.Regex) {
    if right := getRight(r); right != nil {
        b.travStack = append(b.travStack, travEntry{node: right})
    }
    if left := getLeft(r); left != nil {
        b.travStack = append(b.travStack, travEntry{node: left})
    }
    if inner := getInner(r); inner != nil {
        b.travStack = append(b.travStack, travEntry{node: inner})
    }
}

// 辅助：访问 AST 节点的字段
func getLeft(r regex.Regex) regex.Regex {
    switch a := r.(type) {
    case *regex.Concat: return a.Left
    case *regex.Union:  return a.Left
    }
    return nil
}
func getRight(r regex.Regex) regex.Regex {
    switch a := r.(type) {
    case *regex.Concat: return a.Right
    case *regex.Union:  return a.Right
    }
    return nil
}
func getInner(r regex.Regex) regex.Regex {
    switch a := r.(type) {
    case *regex.Star:     return a.Inner
    case *regex.Plus:     return a.Inner
    case *regex.Optional: return a.Inner
    case *regex.Repeat:   return a.Inner
    case *regex.Group:    return a.Inner
    }
    return nil
}
```

---

## 六、Plus 节点的特殊处理

**Plus 的语义** = `Concat(inner, Star(inner))`。但 Plus 在 AST 里是一元节点（只有 `inner`），不能直接用 `Concat` 规则。

### 解决方案：复制 fragment

```go
func (n *NFA) plusRule() (s, e int) {
    // 1. pop inner 的 fragment
    is, ie := n.PopFrag()

    // 2. 把 inner fragment 复制一份压回去
    //    让 starRule 也能 pop 到（starRule 不会重复构造 inner 状态，
    //    只是新建 2 个状态 + 加 4 条 ε 边）
    n.PushFrag(is, ie)

    // 3. starRule 弹出一个，得到 Star(inner) 的 (s, e)
    ss, se := n.starRule()

    // 4. 焊接：inner.e --ε--> star.s
    n.AddEpsilon(ie, ss)

    return is, se
}
```

**为什么这样不破坏 iterative？**

- 只复制 `(s, e)` 两个 int，**不复制图**
- `starRule` 是在原 NFA 上**新建 2 个状态 + 加 4 条 ε 边**
- 跟前面的 inner 状态**共用同一张图**
- **纯栈操作，零递归**

### 走一遍 `a+`

假设 `a+` 的 AST 是 `Plus(inner=Literal('a'))`

```
stack=[Plus/f], fragStack=[]

[1] 弹 Plus/f (not visited)
    isLeaf? No
    push {Plus, true}  // 二次入栈
    push {inner, f}     // inner 入栈

[2] 弹 inner/f (not visited)
    isLeaf? Yes (Literal)
    processLeaf → s0, s1
    PushFrag(0, 1)
    stack=[Plus/t], fragStack=[0→1]

[3] 弹 Plus/t (visited)
    plusRule():
      PopFrag → (0, 1)
      PushFrag(0, 1)        // 复制
      starRule():
        PopFrag → (0, 1)
        NewState → 2, NewState → 3
        AddEpsilon(2, 0)    // 进入
        AddEpsilon(2, 3)    // 跳过
        AddEpsilon(1, 3)    // 退出
        AddEpsilon(1, 0)    // 自环
        return (2, 3)
      AddEpsilon(1, 2)      // 焊接
      return (0, 3)
    PushFrag(0, 3)
    stack=[], fragStack=[0→3]

[4] 弹 fragStack → (0, 3)
    n.Start = 0
    return 0, 3
```

**最终 NFA**：
```
                 ┌─ε─→ [0] ─a→ [1] ─┐
                 │         ↑_______│
→ [0] ─a→ [1] ──ε──→ [2] ─┘
            ↑        └─ε─────→ [3]   (接受)
            │ 
            └─ε─────────────→ [3]   (跳过)
            └──[1]→[0] 自环
```

✓ 正确！

---

## 七、Repeat 节点的处理

```go
// internal/nfa/rules.go

// Repeat: {n}, {n,}, {n,m}
func (n *NFA) repeatRule(a *regex.Repeat) (s, e int) {
    // {n} = Concat(A, A, ..., A) n 次
    if a.Min == a.Max {
        return n.repeatExact(a.Inner, a.Min)
    }
    // {n,} = Concat(A^n, Star(A))
    if a.Max == -1 {
        return n.repeatAtLeast(a.Inner, a.Min)
    }
    // {n,m} = Union(A^n, A^(n+1), ..., A^m)
    return n.repeatRange(a.Inner, a.Min, a.Max)
}
```

### 实现策略

| 形式 | 实现 |
| --- | --- |
| `{n}` | 调 n 次 `concatRule` 串起来 |
| `{n,}` | 先串 n 次，再串一个 `Star(inner)` |
| `{n,m}` | 对 k=n..m 调 `repeatExact(inner, k)`，用 `unionRule` 串起来 |

> **这些都不需要重新遍历 AST**，纯粹是 NFA 片段的拼接。复用已有的 `concatRule` / `starRule` / `unionRule` 即可。

---

## 八、接口设计

### `internal/nfa` 导出 API

```go
package nfa

// 构造
func New() *NFA
func NewBuilder() *Builder
func (b *Builder) Build(root regex.Regex, tokenKey string) (s, e int)
func (b *Builder) NFA() *NFA  // 取出纯数据

// 访问（用户在 Builder 构造完后调）
func (n *NFA) Start() int
func (n *NFA) Accepts() []int
func (n *NFA) AcceptTag(state int) string
func (n *NFA) Trans() map[int]map[byte][]int
func (n *NFA) Epsilon() map[int][]int
func (n *NFA) NumStates() int
```

### 调用模式（DSL 层）

```go
// internal/dsl/dsl.go
import "lexical_analysis/internal/nfa"

type Spec struct {
    Key   string
    Type  string
    Name  string
    Regex string
    AST   regex.Regex
    NFA   *nfa.NFA    // ← 新增：纯数据
    Order int
}

for key, regexStr := range raw.Tokens {
    ast, _ := regex.Parse(regexStr)
    
    builder := nfa.NewBuilder()       // ← 构造器
    builder.Build(ast, key)            // ← 跑 Thompson
    
    s := Spec{
        Key:   key,
        Type:  typ,
        Name:  name,
        Regex: regexStr,
        AST:   ast,
        NFA:   builder.NFA(),          // ← 取出纯数据
        Order: order,
    }
    // builder 用完可丢弃（或不存引用，让 GC 回收）
    
    sf.Tokens = append(sf.Tokens, s)
}
```

**3 行核心代码**：
```go
builder := nfa.NewBuilder()    // 1. 准备构造器
builder.Build(ast, key)         // 2. 跑 Thompson
nfa := builder.NFA()            // 3. 取出 NFA
```

### 调用方对比

| 方案 | 调用 |
| --- | --- |
| ❌ NFA.fragStack | `nfa := nfa.New(); nfa.Build(ast, key)` —— NFA 混着构造状态 |
| ✅ Builder | `builder := nfa.NewBuilder(); builder.Build(ast, key); nfa := builder.NFA()` —— 数据干净 |

---

## 九、画图集成（goccy/go-graphviz）

### 依赖添加

```bash
go get github.com/goccy/go-graphviz
```

### 画图模块

```go
// internal/nfa/draw.go
package nfa

import (
    "context"
    "fmt"
    "github.com/goccy/go-graphviz"
)

// ToDOT 把 NFA 转成 Graphviz DOT 文本
func (n *NFA) ToDOT() string {
    var sb strings.Builder
    sb.WriteString("digraph nfa {\n")
    sb.WriteString("  rankdir=LR;\n")
    sb.WriteString("  node [shape=circle];\n")

    // 起始节点（隐形入口）
    fmt.Fprintf(&sb, "  start [shape=point];\n")
    fmt.Fprintf(&sb, "  start -> s%d;\n", n.Start)

    // 普通节点
    for state := 0; state < n.NumStates(); state++ {
        isAccept := false
        for _, acc := range n.Accepts {
            if state == acc {
                isAccept = true
                break
            }
        }
        if isAccept {
            tag := n.AcceptTag(state)
            fmt.Fprintf(&sb, "  s%d [shape=doublecircle, label=\"s%d\\n(%s)\"];\n",
                state, state, tag)
        }
    }

    // ε 边
    for from, tos := range n.Epsilon {
        for _, to := range tos {
            fmt.Fprintf(&sb, "  s%d -> s%d [label=\"ε\", style=dashed];\n", from, to)
        }
    }

    // 字符边
    for from, trans := range n.Trans {
        for ch, tos := range trans {
            label := formatCharLabel(ch)
            for _, to := range tos {
                fmt.Fprintf(&sb, "  s%d -> s%d [label=%q];\n", from, to, label)
            }
        }
    }

    sb.WriteString("}\n")
    return sb.String()
}

// ToPNG 用 goccy/go-graphviz 渲染成 PNG
func (n *NFA) ToPNG() ([]byte, error) {
    ctx := context.Background()
    g, err := graphviz.New(ctx)
    if err != nil {
        return nil, err
    }
    graph, err := g.Graph()
    if err != nil {
        return nil, err
    }
    defer graph.Close()

    // 把 NFA 装进 graphviz graph
    if err := n.populateGraph(graph); err != nil {
        return nil, err
    }

    // 渲染
    var buf bytes.Buffer
    if err := g.Render(ctx, graph, graphviz.PNG, &buf); err != nil {
        return nil, err
    }
    return buf.Bytes(), nil
}
```

### CLI 集成

```go
// cmd/glex/main.go（追加）
var (
    outputDir = flag.String("o", "./output", "输出目录")
    drawNFA   = flag.Bool("draw-nfa", false, "用 graphviz 画每个 token 的 NFA")
)

// 在解析完 sf 后
if *drawNFA {
    nfaDir := filepath.Join(*outputDir, "nfa")
    os.MkdirAll(nfaDir, 0755)

    for _, t1 := range sf.Tokens {
        // 1. 保存 DOT 文件
        dotPath := filepath.Join(nfaDir, t1.Key+".dot")
        os.WriteFile(dotPath, []byte(t1.NFA.ToDOT()), 0644)

        // 2. 渲染 PNG
        pngBytes, err := t1.NFA.ToPNG()
        if err == nil {
            pngPath := filepath.Join(nfaDir, t1.Key+".png")
            os.WriteFile(pngPath, pngBytes, 0644)
        }
    }
}
```

### 输出结构

```
output/
└── nfa/
    ├── ID.dot
    ├── ID.png
    ├── NUM.dot
    ├── NUM.png
    ├── REAL.dot
    ├── REAL.png
    ├── KW_IF.dot
    ├── KW_IF.png
    ├── OP_ADD.dot
    ├── OP_ADD.png
    └── ...
```

---

## 十、完整数据流

```
┌────────────────────────┐
│ tokens.glex (YAML)      │
└──────────┬─────────────┘
           │ dsl.ReadFile
           ▼
┌────────────────────────┐
│ SpecFile {             │
│   Tokens: [Spec{       │  ← 每个 Spec 有 Key/Type/Name/Regex
│     AST, ...           │  ← M2 填好
│   }]                   │
│ }                      │
└──────────┬─────────────┘
           │ for each spec:
           │   spec.NFA = nfa.New()
           │   spec.NFA.Build(spec.AST, spec.Key)
           ▼
┌────────────────────────┐
│ SpecFile {             │
│   Tokens: [Spec{       │
│     AST, NFA, ...      │  ← M3 填好
│   }]                   │
│ }                      │
└──────────┬─────────────┘
           │  if -draw-nfa:
           │     for each spec:
           │       ToDOT() → .dot 文件
           │       ToPNG() → .png 文件
           ▼
┌────────────────────────┐
│ output/nfa/             │
│   ├── ID.dot, ID.png    │
│   ├── NUM.dot, NUM.png  │
│   └── ...               │
└────────────────────────┘
```

---

## 十一、关键设计决策

| 决策 | 方案 | 理由 |
| --- | --- | --- |
| 算法 | **迭代（不是递归）** | 避免大 AST 栈溢出 |
| 遍历栈内容 | **`{node, visited}`** | 只管遍历，不带 (s, e) |
| 数据/构造分离 | **NFA + Builder 双结构** | NFA 纯数据，Builder 装构造状态 |
| Builder 模式 | **`nfa.NewBuilder().Build().NFA()`** | 符合 Go 习惯（bytes.Buffer / json.Encoder） |
| fragment 栈归属 | **Builder 字段** | 构造完 Builder 丢弃，NFA 干净 |
| Plus 节点 | **复制 fragment + starRule** | 零递归地实现 Plus = Inner + Star(Inner) |
| 画图库 | **goccy/go-graphviz** | 纯 Go，不依赖系统 graphviz |
| 输出格式 | **DOT + PNG** | DOT 文本可读，PNG 直观 |
| 单 token 接口 | **`NewBuilder().Build().NFA()`** | 简化 M3，先跑通单 token |
| 数据流通用 | **NFA 装到 Spec** | 后续 M4 拿 Spec.Tokens[i].NFA 就能用 |

---

## 十二、与上下游衔接

### 上游（M2 AST）

```go
// M2 提供 AST
ast, err := regex.Parse(regexStr)

// M3 接收
spec.NFA.Build(ast, key)  // 任意 AST 都能塞进去
```

### 下游（M4 子集构造）

```go
// M4 拿每个 Spec 的 NFA，做子集构造
for _, t1 := range sf.Tokens {
    dfa := subsetConstruct(t1.NFA)  // 输入 NFA，输出 DFA
    t1.DFA = dfa
}
```

---

## 十三、测试策略

### 单元测试

| 测试 | 内容 |
| --- | --- |
| `TestBuild_Literal` | 单个 Literal 的 NFA |
| `TestBuild_CharClass` | 字符类的 NFA（多条字符边） |
| `TestBuild_Concat` | Concat 的 NFA（含 ε 串联） |
| `TestBuild_Union` | Union 的 NFA（含分支） |
| `TestBuild_Star` | Star 的 NFA（含自环） |
| `TestBuild_Plus` | Plus 的 NFA（验证 fragment 复制正确性） |
| `TestBuild_Optional` | Optional 的 NFA |
| `TestBuild_Repeat_Exact` | `{n}` 的 NFA |
| `TestBuild_Repeat_Unbounded` | `{n,}` 的 NFA |
| `TestBuild_Repeat_Range` | `{n,m}` 的 NFA |
| `TestBuild_Group` | Group 透传 |
| `TestBuild_RealToken_ID` | 真实 ID 正则的 NFA |

### 端到端测试

- 跑 `glex -i examples/tokens.glex -draw-nfa`
- 检查 `output/nfa/{KEY}.png` 都生成了
- 视觉检查 ID、NUM、OP_EQ 的图是否符合预期

### 关键不变量

- 接受态数量 = 1（Thompson 构造的产物**只有一个**接受态）
- 状态数 ≤ 2 × AST 节点数
- 边数 ≤ 节点数 × 出度

---

## 十四、里程碑（M3 子任务）

| 任务 | 内容 | 验证 |
| --- | --- | --- |
| 1 | `nfa.go`（NFA struct + 基础方法） | 编译通过 |
| 2 | `builder.go`（Builder + fragment 栈） | 编译通过 |
| 3 | `rules.go`（processLeaf + 7 个 combine 规则） | 单测全过 |
| 4 | `build.go`（Build 入口 + 遍历算法 + pushChildren） | 单测全过 |
| 5 | `dsl.go` 加 NFA 字段 + Builder 装配 | 端到端跑通 |
| 6 | `draw.go`（ToDOT + ToPNG） | 输出 PNG 可看 |
| 7 | `cmd/glex/main.go` 加 `-draw-nfa` flag | CLI 跑通 |
| 8 | 端到端测试 | 所有 token 的 NFA 都画对 |

---

> **总结**：glex M3 = **NFA + Builder 双结构**（数据与构造彻底分离） + **迭代 Thompson 遍历** + **goccy/go-graphviz 出图**。架构核心是 Builder 模式：NFA 是纯数据，Builder 装"构造时辅助"（fragStack、travStack），构造完 Builder 丢弃，最终只留下干净的 NFA 给用户。
