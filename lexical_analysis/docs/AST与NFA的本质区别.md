# AST 与 NFA 的本质区别

> 理解 AST 和 NFA 的关系，是理解 glex（以及几乎所有编译器）核心机制的关键。

---

## 一、AST 是什么

在 glex 的流水线里，**AST = 正则表达式的解析产物**，是内存中的一棵**节点树**。

```
YAML 文件
   │  "ID: '[a-zA-Z_][a-zA-Z0-9_]*'"
   ▼
DSL 解析  ──→  TokenSpec{Key, Type, Name, Regex 字符串}
   │  "[a-zA-Z_][a-zA-Z0-9_]*"
   ▼
正则 parser  ──→  ★ AST 树（这一步的产物） ★
   │
   ▼
Thompson 构造  ──→  NFA 状态图
   │
   ▼
子集构造法  ──→  DFA 状态表
```

> **AST 是 M2（正则解析）的输出，M3（Thompson 构造）的输入。**

---

## 二、AST 的具体产物

### 2.1 节点类型（共 8 种）

```go
// internal/regex/ast.go
type Regex interface{ regexNode() }

type Literal  struct{ Ch byte }                // 字面字符  'a'
type CharClass struct {                        // 字符类
    Chars  []byte
    Negate bool
}
type Concat   struct{ Left, Right Regex }      // 连接  A B
type Union    struct{ Left, Right Regex }      // 选择  A|B
type Star     struct{ Inner Regex }            // 0+ 次  A*
type Plus     struct{ Inner Regex }            // 1+ 次  A+
type Optional struct{ Inner Regex }            // 0/1 次  A?
type Group    struct{ Inner Regex }            // 分组  (A)
type Dot      struct{}                         // 任意单字符 .
```

### 2.2 真实例子：ID 的 AST

输入字符串：`"[a-zA-Z_][a-zA-Z0-9_]*"`

解析后的 Go 值：

```go
root := &Concat{
    Left: &CharClass{
        Chars:  []byte{'a'..'z', 'A'..'Z', '_'},  // 范围已展开
        Negate: false,
    },
    Right: &Star{
        Inner: &CharClass{
            Chars:  []byte{'a'..'z', 'A'..'Z', '0'..'9', '_'},
            Negate: false,
        },
    },
}
```

可视化（`ast.Print(root)`）：

```
Concat
├── CharClass [a-z A-Z _]
└── Star
    └── CharClass [a-z A-Z 0-9 _]
```

### 2.3 glex 内部的存储

```go
type Spec struct {
    Key   string       // "ID"
    Type  string       // "ID"
    Name  string       // "ID"
    Regex string       // 原始字符串 "[a-zA-Z_][a-zA-Z0-9_]*"
    AST   regex.Regex  // ★ 解析后的树
}

var specs []Spec = []Spec{
    {Key: "ID",   ..., AST: <上面那棵 Concat 树>},
    {Key: "NUM",  ..., AST: <另一棵 Concat 树>},
    {Key: "OP_LE",..., AST: <一棵 Concat 树>},
    ...
}
```

**M2 完成后，每个 spec 的 `AST` 字段就填好了。**

### 2.4 AST 能干什么

| 用途 | 说明 |
| --- | --- |
| **喂给 Thompson** | 每个 AST 节点递归构造 NFA 片段 |
| **打印调试** | `Print(root)` 一目了然 |
| **单元测试** | 断言输入字符串产生特定树形 |
| **等价性检查** | 比较两棵 AST 是否结构相同 |
| **正则改写/优化** | 直接操作树节点，避开 NFA 的边 |

---

## 三、NFA 是什么

**NFA** = Nondeterministic Finite Automaton，**状态机**，由 Thompson 构造从 AST **机械地**产出。

```go
// internal/nfa/nfa.go
type NFA struct {
    Start        int                          // 起始状态 ID
    Accepts      []int                        // 接受态 ID 列表
    Transitions  map[int]map[byte][]int       // 状态 × 字符 → 目标状态集合
    Epsilons     map[int][]int                // ε 边（不要字符就能走）
    AcceptTags   map[int]string               // 接受态 → 哪个 token
}
```

关键：**NFA 有状态、有边、可以循环**。

### 同样那条 ID 正则的 NFA

```
                  CharClass{字母/_}
   → [s0] ─────────────────────────→ [s1] ─ε→ [s2] ───CharClass{字母/数字/_}──→ [s3]
                                                                          ↑    │
                                                                          └────┘
                                                                              [s3] 接受
```

---

## 四、AST vs NFA：3 个本质区别

### 区别 ① ：AST 是**树**，NFA 是**图**（含环）

```
AST:
       Concat
       /    \
   CharA    Star
              |
           CharB

（每个节点只有一个父节点，结构 = 树）


NFA:
   →[s0]─a→[s1]─ε→[s2]─b→[s3]
                       ↑   │
                       └───┘  ← ★ 环！AST 表达不出来
                       [s3] 接受
```

> NFA 的"环"= AST 的 `Star` / `Plus` 节点。
> 树没有环，环只能用图表达。

### 区别 ② ：AST **没有 ε 边**，NFA **靠 ε 边胶合片段**

Thompson 构造的核心动作就是**加 ε 边**：

| AST 节点 | Thompson 构造动作 |
| --- | --- |
| `Concat(A, B)` | A.end ─ε→ B.start |
| `Union(A, B)` | 新 start ─ε→ A.start；新 start ─ε→ B.start；A.end, B.end ─ε→ 新 end |
| `Star(A)` | 新 start ─ε→ 新 end（跳过）<br>新 start ─ε→ A.start<br>A.end ─ε→ 新 end<br>A.end ─ε→ A.start（**环**）|
| `Plus(A)` | A.end ─ε→ A.start（环） |

**AST 完全没有"ε 边"这个概念**。NFA 比 AST **多出来**的就是这些边。

### 区别 ③ ：AST **描述**，NFA **执行**

| 任务 | AST | NFA |
| --- | --- | --- |
| 打印阅读 | ✓ 树形清晰 | ✗ 一堆状态和边 |
| 改写/优化正则 | ✓ 改树节点 | ✗ 难 |
| 单元测试 | ✓ 断言树形 | ✓ 断言状态转移 |
| **实际匹配输入** | ✗ 需要解释执行 | ✓ 状态机直接跑 |
| 转 DFA | ✗ | ✓（子集构造法） |
| 最小化 | ✗ | ✓ |

---

## 五、为什么不能跳过 AST 直接到 NFA？

理论上可以，但**实际工程几乎都做 AST 这步**，原因：

### 1. 优先级解析

字符串 `ab|c` 有歧义：
- 解读 1：`(ab)|c`（先 `ab` 再或 `c`）
- 解读 2：`a(b|c)`（先 `a` 再 `b|c`）

AST parser **按运算符优先级解析**（`|` < concat < `*+?` < atom），产出**唯一**的树。

NFA 阶段**只接受已消歧义的结构**。

### 2. Thompson 构造是"模式化"的

Thompson 对每个 AST 节点有**固定规则**：

```
看到 Concat  →  加 ε 串联
看到 Star    →  加 ε 自环
看到 CharClass → 字符类边
```

NFA 没有"结构"概念，看到 NFA 一堆状态和边，**没法做模式化转换**。

### 3. 复用性

未来想：
- 分析正则（求 first 字符集）
- 优化正则（去冗余）
- 求两正则交集/并集

**操作 AST 比操作 NFA 容易一万倍**。

---

## 六、直观类比

| 概念 | 类比 |
| --- | --- |
| **AST** | 菜谱："放盐 5 克，加糖 2 克，搅拌 3 分钟"——**描述** |
| **NFA** | 厨师在厨房**实际动手**："左手拿盐，右手拿勺"——**执行状态** |

菜谱看完，**不会做菜**。要把菜谱"翻译"成动作序列，才能真的做。

AST → NFA 就是这个"翻译"。

---

## 七、完整转换链

```
正则字符串          "[a-zA-Z_][a-zA-Z0-9_]*"
   │
   ▼  M2: parser
AST 树              Concat(CharClass, Star(CharClass))
   │                （无 ε 边，无环，纯树）
   │
   ▼  M3: Thompson
NFA 状态图          [s0]→[s1]─ε→[s2]↔[s3] 接受
   │                （有 ε 边，有环，是图）
   │
   ▼  M4: 子集构造法
DFA 状态表          [NumStates][NumClasses]int{...}
   │                （去不确定性，每输入唯一目标）
   │
   ▼  M5: 最小化
更小的 DFA          状态数 ↓
   │
   ▼  M6: 渲染
Go 代码             []int{...} 字面量
   │
   ▼  运行
Token 流
```

**每一步都是把"一种表示"转成"另一种表示"**：

| 步骤 | 加什么 | 减什么 |
| --- | --- | --- |
| 字符串 → AST | 结构 | 字符串歧义 |
| AST → NFA | 状态、ε 边、环 | 树形结构（不再"易读"） |
| NFA → DFA | 确定性 | 状态数（指数级膨胀！） |
| DFA → 最小 DFA | 等价合并 | 冗余状态 |
| DFA → Go 代码 | 具体可执行 | 抽象表示 |

---

## 八、一句话总结

> **AST = 正则的"工程师视图"（易读、易改、易测）**
> **NFA = 正则的"机器视图"（可执行、可转 DFA、可最小化）**
> **Thompson 构造 = 把前者机械地、可证明地转成后者。**

两者**表示同一件事**，但服务的对象不同：

- 写 glex 的人**写 AST 节点**、**调 Thompson 函数**
- 跑词法分析的人**跑 NFA / DFA 状态机**

---

## 九、附录：glex 里 AST 到 NFA 的实际代码骨架

```go
// internal/nfa/thompson.go
func Build(ast regex.Regex, nfa *NFA) (start, end int) {
    switch a := ast.(type) {
    case *regex.Literal:
        s := nfa.NewState()
        e := nfa.NewState()
        nfa.AddTransition(s, a.Ch, e)
        return s, e

    case *regex.CharClass:
        s := nfa.NewState()
        e := nfa.NewState()
        for _, ch := range a.Chars {
            nfa.AddTransition(s, ch, e)
        }
        return s, e

    case *regex.Concat:
        lStart, lEnd := Build(a.Left, nfa)
        rStart, rEnd := Build(a.Right, nfa)
        nfa.AddEpsilon(lEnd, rStart)   // ★ 这里就是 AST 没有的 ε 边
        return lStart, rEnd

    case *regex.Star:
        s := nfa.NewState()
        e := nfa.NewState()
        inner_s, inner_e := Build(a.Inner, nfa)
        nfa.AddEpsilon(s, inner_s)     // 跳过
        nfa.AddEpsilon(s, e)           // 跳过
        nfa.AddEpsilon(inner_e, s)     // 环
        nfa.AddEpsilon(inner_e, e)     // 退出
        return s, e

    // ... Plus, Optional, Union 类似
    }
}
```

**注意 `AddEpsilon` 调用**——这是 AST 里**完全没有**的概念，是 Thompson 构造**额外加的**。

---

> 看到这里，你应该能回答这个问题了：**AST 之后就是 NFA，对，但 NFA 比 AST 多三样东西——状态、ε 边、环**。这三样让 NFA "能跑"，也让 AST "跑不起来"。
