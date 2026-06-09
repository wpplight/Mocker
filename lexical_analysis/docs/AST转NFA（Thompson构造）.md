# AST 转 NFA（Thompson 构造）

> glex 流水线 M3 的核心：从正则 AST 机械地构造 NFA。
> 这是连接"易读结构"和"可执行状态机"的桥梁。

---

## 一、为什么需要这一步

| 形态 | 谁能看 | 谁能用 |
| --- | --- | --- |
| **AST** | 工程师（看、改、测试） | ✗ 不能直接匹配输入 |
| **NFA** | 机器（执行） | ✓ 可以模拟匹配 |

AST 描述"是什么"，NFA 描述"怎么跑"。**Thompson 构造**就是把前者机械地、可证明地转成后者。

## Thompson 构造的 3 个性质

1. **机械性**：每个 AST 节点有固定规则
2. **局部性**：只看一个节点，不依赖兄弟节点
3. **正确性**：定理保证 — `L(Thompson(AST)) = L(AST)`，即两者识别同一语言

---

## 二、核心概念：ε 边和 ε-closure

### ε 边

NFA 里有一种**不消耗字符**就能走的边，叫 **ε 转移**。AST 里**完全没有**这个概念，是 Thompson 构造**额外加的**。

```
→ [s0] --a--> [s1] --ε--> [s2] --b--> [s3]
```

不读字符，从 s1 直接跳到 s2。

### ε-closure（NFA 模拟的基石）

> 从某个状态出发，**只走 ε 边**能到达的所有状态（包括自己）。

```
→ [s0] --a--> [s1] --ε--> [s2] --ε--> [s3]
                  ↑_______ε_______│
                  └────────────────┘

ε-closure({s0}) = {s0}
ε-closure({s1}) = {s1, s2, s3}
ε-closure({s2}) = {s2, s3}
ε-closure({s3}) = {s3}
```

后续 M4（子集构造法）就是反复计算 ε-closure 来构建 DFA 的。

---

## 三、每个 AST 节点的构造规则

下面是 glex 实现的全部 11 种节点。约定：
- `→` 表示 NFA 片段的**起点**
- `( )` 表示**终点**（接受态）
- `A.start`, `A.end` 表示子片段 A 的起止状态

### 3.1 字面字符 `Literal(c)`

```
→ [s] --c--> (e)
```

两状态，一字符边。

### 3.2 字符类 `CharClass` / `.`

字符类的 NFA 边是"类里的任一字符"：

```
→ [s] --{a,b,c,...}--> (e)
```

`Dot` 同理，只是字符类是"全部字节"。

### 3.3 连接 `Concat(A, B)`

把 A 的终点和 B 的起点用 ε 边连起来：

```
→ A.start ──A──→ A.end ──ε──→ B.start ──B──→ B.end
```

### 3.4 选择 `Union(A, B)`

新建起点 s 和终点 e，加 ε 边分支：

```
                ┌─ε─→ A.start ──A──→ A.end ──┐
→ [s] ──────────┤                            ├──ε──→ (e)
                └─ε─→ B.start ──B──→ B.end ──┘
```

### 3.5 星号 `Star(A)`（0+ 次）

新建起止 s、e，加 4 条 ε 边：
- `s --ε--> e`（**跳过**，匹配 0 次）
- `s --ε--> A.start`（**进入**）
- `A.end --ε--> e`（**退出**）
- `A.end --ε--> A.start`（**自环**，多次）

```
       ┌─ε─→ A.start ──A──→ A.end ──┐
       │              ↑____________│
→ [s] ─┤              │   ε (环)   │
       │              │____________│
       └─ε──────────────────────────────→ (e)
```

### 3.6 加号 `Plus(A)`（1+ 次）

`Plus(A) = Concat(A, Star(A))`，即"先来一次，再来 0+ 次"：

```
→ A.start ──A──→ A.end ──ε──→ [s'] ──Star(A)──→ (e')
```

### 3.7 可选 `Optional(A)`（0/1 次）

`Optional(A) = Union(A, Empty)`，即"要么 A，要么空"：

```
                ┌─ε─→ A.start ──A──→ A.end ──┐
→ [s] ──────────┤                            ├──ε──→ (e)
                └─ε─────────────────────────┘
```

### 3.8 重复 `Repeat{Min, Max}(A)`

**三种情况**：

| 形式 | 展开 | 示例 |
| --- | --- | --- |
| `{n}` (Min=Max=n) | `Concat(A, A, ..., A)` n 次 | `a{3}` = `aaa` |
| `{n,}` (Max=-1) | `Concat(A^n, Star(A))` | `a{2,}` = `aa a*` |
| `{n,m}` | `Union(A^n, A^(n+1), ..., A^m)` | `a{2,3}` = `aa \| aaa` |

### 3.9 分组 `Group(A)`

语法糖，等价于 A 自身（但改变优先级）。NFA 构造上**就是 A 本身的片段**。

### 3.10 空 `Empty`

```
→ [s] ──ε──→ (e)
```

一状态，一 ε 边。

---

## 四、完整示例：ID → AST → NFA

### 输入
正则字符串：`[a-zA-Z_][a-zA-Z0-9_]*`

### AST
```
Concat
├── CharClass [a-z A-Z _]
└── Star
    └── CharClass [a-z A-Z 0-9 _]
```

### 构造过程

**Step 1：先构造两个 CharClass 片段**

```
A = → [0] ──[a-zA-Z_]──→ (1)
B = → [2] ──[a-zA-Z0-9_]──→ (3)
```

**Step 2：把 B 包成 Star（B 是 Star 的 inner）**

新建 s=4, e=5，加 4 条 ε 边：

```
       ┌─ε──→ B.start(=2) ──[a-zA-Z0-9_]──→ B.end(=3) ──┐
       │                  ↑__________________│  ε (环)   │
→ [4] ─┤                  └─────────────────────────────│
       └─ε───────────────────────────────────────────→ (5)
```

**Step 3：把 A 和 Star(B) Concat 起来**

A.end(1) ──ε──→ Star(B).start(4)：

```
→ [0] ──[a-zA-Z_]──→ (1) ──ε──→ [4] ──Star 内部如上图──→ (5)
```

### 最终 NFA

```
                       ┌─ε─→ [2] ──[a-zA-Z0-9_]─→ [3] ─┐
                       │              ↑__________________│
→ [0] ─[a-zA-Z_]─→ [1] ──ε──→ [4] ──┤
                       │              └───────────────────→ [5] (接受)
                       └─ε───────────────────────────────────→ [5]
```

**关键观察**：
- 5 个状态（NFA 状态数 ≤ AST 节点数的 2 倍）
- 2 个字符边（对应 2 个 CharClass）
- 4 条 ε 边（Concat 1 + Star 4 - 1 复用 = 4）
- 1 个接受态 [5]

### 状态表表示

```go
type NFA struct {
    Start   int                          // 0
    Accepts []int                        // [5]
    Trans   map[int]map[byte][]int       // 字符边
    Epsilon map[int][]int                // ε 边
}
```

具体内容：
```
Epsilon:
  0 → []
  1 → [4]              // Concat 串联
  2 → []
  3 → [2]              // Star 自环
  4 → [2, 5]           // Star 跳过 + 进入
  5 → []

Trans:
  0 → {'[a-zA-Z_]' char} → [1]
  2 → {'[a-zA-Z0-9_]' char} → [3]
  4 → [] (no char)
  ...
```

---

## 五、合并 NFA（多个 token → 一个 NFA）

glex 的 NFA 不只是"一条正则一个 NFA"，而是**所有 token 合并成一个 NFA**。

**做法**：
1. 每条 token 单独 Thompson 构造 → 得到各自的 (start, end)
2. 给每个 end 打标签（"接受 → 哪个 token"）
3. 新建超级起点 S0，从 S0 用 ε 边连到每个 token NFA 的 start

```
                 ┌─ε─→ ID.start ──ID NFA──→ ID.end (tag=ID)
                 │
                 ├─ε─→ NUM.start ──NUM NFA──→ NUM.end (tag=NUM)
→ [S0] ──────────┤
                 ├─ε─→ OP_ADD.start ──...──→ OP_ADD.end (tag=OP_ADD)
                 │
                 └─ε─→ ... (其它 token)
```

后续 M4（子集构造法）就基于这个**总 NFA** 做。

### 接受态标签

每个 NFA 接受态带"接受 → 哪个 token 名"：

```go
type NFA struct {
    ...
    AcceptTags map[int]string  // 接受态 ID → token key
}

// 例：
// AcceptTags[5] = "ID"          // ID NFA 的接受态 5
// AcceptTags[12] = "OP_ADD"     // OP_ADD NFA 的接受态 12
```

后续 DFA 构造时，**多个 NFA 接受态可能合并到同一个 DFA 状态**——这时候用：
1. **最长匹配**：选读得最长的那个 token
2. **先定义优先**：同长度时选 DSL 中更靠前的 token

---

## 六、Go 代码骨架

```go
// internal/nfa/nfa.go
package nfa

type NFA struct {
    NextStateID int
    Start       int
    Accepts     []int
    AcceptTags  map[int]string  // 接受态 → token key
    Trans       map[int]map[byte][]int  // state × char → []target
    Epsilon     map[int][]int   // state → []target (ε 边)
}

func New() *NFA {
    return &NFA{
        AcceptTags: make(map[int]string),
        Trans:      make(map[int]map[byte][]int),
        Epsilon:    make(map[int][]int),
    }
}

func (n *NFA) NewState() int {
    id := n.NextStateID
    n.NextStateID++
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

```go
// internal/nfa/thompson.go
package nfa

import "lexical_analysis/internal/regex"

// Build 把 AST 转成 NFA，返回 (start, end) 状态 ID。
// 接受态通过 tokenKey 参数打标签。
func (n *NFA) Build(r regex.Regex, tokenKey string) (start, end int) {
    switch a := r.(type) {
    case *regex.Literal:
        s := n.NewState()
        e := n.NewState()
        n.AddTrans(s, a.Ch, e)
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e

    case *regex.CharClass:
        s := n.NewState()
        e := n.NewState()
        for _, ch := range a.Chars {
            n.AddTrans(s, ch, e)
        }
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e

    case *regex.Dot:
        s := n.NewState()
        e := n.NewState()
        for b := 0; b < 256; b++ {
            n.AddTrans(s, byte(b), e)
        }
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e

    case *regex.Concat:
        ls, le := n.Build(a.Left, tokenKey)
        rs, re := n.Build(a.Right, tokenKey)
        n.AddEpsilon(le, rs)
        return ls, re

    case *regex.Union:
        s := n.NewState()
        e := n.NewState()
        ls, le := n.Build(a.Left, tokenKey)
        rs, re := n.Build(a.Right, tokenKey)
        n.AddEpsilon(s, ls)
        n.AddEpsilon(s, rs)
        n.AddEpsilon(le, e)
        n.AddEpsilon(re, e)
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e

    case *regex.Star:
        s := n.NewState()
        e := n.NewState()
        is, ie := n.Build(a.Inner, tokenKey)
        n.AddEpsilon(s, is)     // 进入
        n.AddEpsilon(s, e)      // 跳过（0 次）
        n.AddEpsilon(ie, e)     // 退出
        n.AddEpsilon(ie, is)    // 自环（多次）
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e

    case *regex.Plus:
        is, ie := n.Build(a.Inner, tokenKey)
        ss, se := n.Build(&regex.Star{Inner: a.Inner}, tokenKey)
        n.AddEpsilon(ie, ss)
        return is, se

    case *regex.Optional:
        s := n.NewState()
        e := n.NewState()
        is, ie := n.Build(a.Inner, tokenKey)
        n.AddEpsilon(s, is)     // 进入
        n.AddEpsilon(s, e)      // 跳过
        n.AddEpsilon(ie, e)     // 退出
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e

    case *regex.Repeat:
        return n.buildRepeat(a, tokenKey)

    case *regex.Group:
        return n.Build(a.Inner, tokenKey)  // 透传

    case *regex.Empty:
        s := n.NewState()
        e := n.NewState()
        n.AddEpsilon(s, e)
        n.AcceptTags[e] = tokenKey
        n.Accepts = append(n.Accepts, e)
        return s, e
    }
    panic("unknown AST node type")
}

// buildRepeat 处理 {n}, {n,}, {n,m}
func (n *NFA) buildRepeat(a *regex.Repeat, tokenKey string) (start, end int) {
    if a.Min == a.Max {
        // {n} = Concat(A, A, ..., A)
        if a.Min == 0 { return n.Build(&regex.Empty{}, tokenKey) }
        if a.Min == 1 { return n.Build(a.Inner, tokenKey) }
        curS, curE := n.Build(a.Inner, tokenKey)
        for i := 1; i < a.Min; i++ {
            nextS, nextE := n.Build(a.Inner, tokenKey)
            n.AddEpsilon(curE, nextS)
            curS, curE = curS, nextE
        }
        n.AcceptTags[curE] = tokenKey
        n.AcceptE(curE)
        return curS, curE
    }
    if a.Max == -1 {
        // {n,} = Concat(A^n, Star(A))
        if a.Min == 0 { return n.Build(&regex.Star{Inner: a.Inner}, tokenKey) }
        // 先 Concat n 次
        curS, curE := n.Build(a.Inner, tokenKey)
        for i := 1; i < a.Min; i++ {
            nextS, nextE := n.Build(a.Inner, tokenKey)
            n.AddEpsilon(curE, nextS)
            curS, curE = curS, nextE
        }
        // 再接 Star
        ss, se := n.Build(&regex.Star{Inner: a.Inner}, tokenKey)
        n.AddEpsilon(curE, ss)
        n.AcceptTags[se] = tokenKey
        n.Accepts = append(n.Accepts, se)
        return curS, se
    }
    // {n,m} = Union(A^n, A^(n+1), ..., A^m)
    parts := []regex.Regex{}
    for k := a.Min; k <= a.Max; k++ {
        parts = append(parts, repeatExact(a.Inner, k))
    }
    return n.Build(&regex.Union{Left: parts[0], Right: unionOf(parts[1:])}, tokenKey)
}
```

---

## 七、Thompson 构造的关键性质

### 性质 1：状态数有界
> 对一棵有 N 个内部节点的 AST，Thompson 构造产生的 NFA 至多有 **2N+1** 个状态。

我们的实现：每个内部节点新建 s、e 两个状态（部分节点复用），所以状态数大致是 AST 节点数的 2 倍。

### 性质 2：每状态出度有界
- 字符边：至多 1 条（同一字符在 NFA 里就是 1 条）
- ε 边：至多 2 条（Star 的 4 条 ε 边分摊到不同位置）

### 性质 3：等价性
> `L(NFA) = L(原正则)`
> 即 NFA 识别的字符串集合 = 正则描述的字符串集合。

这是**正确的保证**，可以放心交给后续步骤（M4 子集构造法）。

### 性质 4：可扩展性
新增 AST 节点？只需在 `Build` 的 switch 里加一个 case。**已有节点不受影响**。

---

## 八、与 AST 的本质区别回顾

| 维度 | AST | NFA |
| --- | --- | --- |
| 形态 | 树 | 图（可有环） |
| 状态 | 没有 | 一组状态 ID |
| 转移 | 父子引用 | 字符边 + ε 边 |
| ε 边 | **没有** | 大量（连接片段用） |
| 环 | **不能有** | 可以（Star / Plus 自环） |
| 接受 | 树根 | 标记的接受态 |
| 谁写 | 工程师 | Thompson 函数 |
| 谁跑 | 没人（需解释执行） | NFA 模拟器 |

**一句话**：NFA 比 AST 多的就是**状态、ε 边、环**。这三样是 Thompson 构造**机械地、可证明地**加上去的。

---

## 九、Thompson → 子集构造（衔接）

Thompson 构造出的 NFA 是**非确定的**（可能多个 ε 边、多个字符目标）。

下一步 M4（**子集构造法**）会：
1. 把 NFA 状态集合压缩成 DFA 状态
2. 每个 DFA 状态 = "当前可能处于的 NFA 状态集合"
3. 反复计算 ε-closure，直到不动点

这样就能把 NFA → DFA，去掉不确定性，得到一个**快速匹配**的状态机。

---

## 十、给 glex 实现者的建议

1. **先实现核心节点**：`Literal`、`CharClass`、`Concat`、`Union`、`Star`
2. **Plus / Optional / Group 可以基于上面实现**
3. **Repeat 是复杂度的来源**，可以延后
4. **打印 NFA**：每构造一个 token，打印一次状态图，调试用
5. **ε-closure 单独写个函数**，M4 也要用

---

> **总结**：Thompson 构造 = "**AST 节点 → NFA 片段**"的机械翻译。每个 AST 节点有固定规则，规则只引入 ε 边和自环，不引入不确定性。构造完的 NFA 是 M4（子集构造法）的输入。
