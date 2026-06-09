# M4 合并 NFA 任务文档（路径 D：预算 ε-closure）

> glex 流水线 M4：用**路径 D** 把多个 token 的 NFA 整合成一个 DFA。
> 核心思想：**每个 token 的 ε-closure(start) 算一次**，合并后是**无 ε 的 DFA 子集构造**。
> 对应实现：`internal/dfa/`

---

## 一、任务定位

### 在 glex 流水线里

```
M1: dsl.ReadFile       ─→  SpecFile
M2: regex.Parse        ─→  SpecFile (AST 填好)
M3: nfa.Builder.Build  ─→  SpecFile (23 个 NFA 填好)  ← 当前
                                            ↓
M4: 路径 D              ─→  SpecFile (DFA 填好)        ← 本文档
                                            ↓
M5: 最小化
M6: codegen
```

### 本期目标

| 维度 | 目标 |
| --- | --- |
| **输入** | `[]*nfa.NFA`（23 个 token 的 NFA 列表） |
| **输出** | 1 个 DFA（识别任一 token） |
| **接口** | `CombinedNFA.ToDFA() *DFA` |
| **算法** | 路径 D：预算 ε-closure + 无 ε 子集构造 |

---

## 二、路径 D 是什么

### 核心创新

> **每 token 算一次 ε-closure(start) → 缓存**
> **跨 NFA 跑子集构造时 → 不再算 ε-closure**

### 为什么不复制 NFA 数据

| 路径 | 数据 | 状态 | 推荐 |
| --- | --- | --- | --- |
| A. 物理 Total NFA | **拷贝** + offset + super start | 1 个大 NFA struct | ★★ |
| B. 逻辑 Total NFA | 持 NFA 引用 | CombinedNFA struct | ★★★ |
| **D. 预算 ε-closure**（推荐） | 持 NFA 引用 + 预算 closure | CombinedNFA + TokenStartSet 数组 | ★★★★★ |

**路径 D 的特别之处**：
- 复用 B 的"不复制"优势
- **额外**：把每 token 的 `ε-closure(start)` **预算出来**（一次性）
- 合并后是**无 ε 的子集构造**——**完全不用扫 ε 边**

### 路径 D 的"散装 + 拼接"

把 23 个 NFA 想象成 **23 个并行的状态机**：

```
输入字符  走进来
   ↓
   23 个状态机同时跑
   ↓
各 token 在该时刻可达的状态 → 拼成"DFA 状态集"
   ↓
下一字符
   ↓
23 个状态机各自 move → 拼成"新 DFA 状态集"
   ↓
... 直到不动点
```

**全程不重组 NFA**——只需"每 token 自己跑"。

---

## 三、数据结构

```go
// internal/dfa/combined.go
package dfa

import "lexical_analysis/internal/nfa"

// ─────────────────────────────────────────────
// 1. 跨 NFA 状态表示
// ─────────────────────────────────────────────

// TokenStartSet 表示"DFA 状态下，某个 token 的 NFA 状态子集"
type TokenStartSet struct {
    TokenIdx int   // 哪个 token
    States   []int // ε-closure 后的状态列表（已排序）
}

// ─────────────────────────────────────────────
// 2. CombinedNFA：持 NFA 引用（不复制）
// ─────────────────────────────────────────────

type CombinedNFA struct {
    TokenNFAs []*nfa.NFA  // 所有 token 的 NFA
    TokenKeys []string    // 每个 token 的 key（按 Order 排）
    Order     map[string]int  // tokenKey → Order 索引（接受态冲突时用）
}

func NewCombinedNFA(nfas []*nfa.NFA, keys []string) *CombinedNFA

// ─────────────────────────────────────────────
// 3. DFA 状态 = 标准化的 TokenStartSet 列表
// ─────────────────────────────────────────────

// dfaState 是"已排序+去重"的 TokenStartSet 列表
type dfaState []TokenStartSet

func (s dfaState) hash() string    // 用于去重
func (s dfaState) equals(o dfaState) bool

// ─────────────────────────────────────────────
// 4. DFA 主体
// ─────────────────────────────────────────────

type DFA struct {
    Start     int                  // 起始状态
    NumStates int                  // 状态总数
    States    []dfaState           // stateId → 状态定义（调试用）
    Trans     []map[byte][]int     // trans[state][ch] = []nextState（去重）
    Accepts   map[int]string       // stateId → tokenKey
}

func NewDFA() *DFA
func (d *DFA) addCanonicalState(s dfaState) int
```

---

## 四、核心算法

### 4.1 预算 ε-closure（每 token 算一次）

```go
// 算单个 NFA 状态的 ε-closure（BFS）
func epsilonClosure(nfa *nfa.NFA, start int) []int {
    visited := map[int]bool{start: true}
    queue := []int{start}
    for len(queue) > 0 {
        s := queue[0]
        queue = queue[1:]
        for _, to := range nfa.Epsilon[s] {
            if !visited[to] {
                visited[to] = true
                queue = append(queue, to)
            }
        }
    }
    result := make([]int, 0, len(visited))
    for s := range visited {
        result = append(result, s)
    }
    sort.Ints(result)
    return result
}

// 算一组 NFA 状态的 ε-closure
func epsilonClosureSet(nfa *nfa.NFA, starts []int) []int {
    visited := make(map[int]bool)
    queue := []int{}
    for _, s := range starts {
        if !visited[s] {
            visited[s] = true
            queue = append(queue, s)
        }
    }
    for len(queue) > 0 {
        s := queue[0]
        queue = queue[1:]
        for _, to := range nfa.Epsilon[s] {
            if !visited[to] {
                visited[to] = true
                queue = append(queue, to)
            }
        }
    }
    result := make([]int, 0, len(visited))
    for s := range visited {
        result = append(result, s)
    }
    sort.Ints(result)
    return result
}

// 预算每 token 的 start closure
func (c *CombinedNFA) computeStartSet() []TokenStartSet {
    result := make([]TokenStartSet, len(c.TokenNFAs))
    for i, nfa := range c.TokenNFAs {
        result[i] = TokenStartSet{
            TokenIdx: i,
            States:   epsilonClosure(nfa, nfa.Start),
        }
    }
    return result
}
```

**重要**：预算**只算一次**——后续 move 操作里，**不需要再算整个 NFA 的 ε-closure**，只需在 move 触及的**小集合**上做（高效）。

### 4.2 DFA 状态标准化与去重

```go
// dfaState 必须按 token 索引排序，内部 States 也排序
// 这样等价的状态集有相同的字符串表示

func (s dfaState) hash() string {
    parts := make([]string, len(s))
    for i, ts := range s {
        parts[i] = fmt.Sprintf("%d:%v", ts.TokenIdx, ts.States)
    }
    // 不需要再排序 token 索引（保持调用顺序）
    return strings.Join(parts, "|")
}

func (s dfaState) equals(o dfaState) bool {
    if len(s) != len(o) {
        return false
    }
    for i := range s {
        if s[i].TokenIdx != o[i].TokenIdx {
            return false
        }
        if !equalIntSlice(s[i].States, o[i].States) {
            return false
        }
    }
    return true
}

// 标准化：保证 tokenIdx 升序
func canonicalize(sets []TokenStartSet) []TokenStartSet {
    sorted := make([]TokenStartSet, len(sets))
    copy(sorted, sets)
    sort.Slice(sorted, func(i, j int) bool {
        return sorted[i].TokenIdx < sorted[j].TokenIdx
    })
    return sorted
}
```

### 4.3 跨 NFA move 函数

```go
// 从一个 DFA 状态（s dfaState）读字符 ch，到达新的 DFA 状态
func (c *CombinedNFA) move(s dfaState, ch byte) dfaState {
    var result dfaState
    for _, ts := range s {
        nfa := c.TokenNFAs[ts.TokenIdx]
        
        // 1. 从 ts.States 读 ch
        var moved []int
        for _, st := range ts.States {
            if tos, ok := nfa.Trans[st][ch]; ok {
                moved = append(moved, tos...)
            }
        }
        if len(moved) == 0 {
            continue
        }
        
        // 2. move 后的状态可能引入新 ε 边，需要再算 ε-closure
        //    注意：这只在 moved 这个小集合上算，不是全图
        closure := epsilonClosureSet(nfa, moved)
        
        result = append(result, TokenStartSet{
            TokenIdx: ts.TokenIdx,
            States:   closure,
        })
    }
    return result
}
```

**关键**：
- 每个 token 独立 move
- move 后**只对触及的小集合**算 ε-closure（不是全图）
- 各 token 结果**拼接**起来

### 4.4 接受态冲突解决

```go
// 多个 token 可能同时接受同一字符串
// 规则：先定义优先（用 Order 决定）
func (c *CombinedNFA) determineAccept(s dfaState) (string, bool) {
    bestToken := ""
    bestOrder := -1
    
    for _, ts := range s {
        nfa := c.TokenNFAs[ts.TokenIdx]
        for _, st := range ts.States {
            if _, ok := nfa.AcceptTags[st]; ok {
                tag := c.TokenKeys[ts.TokenIdx]
                order := c.Order[tag]
                if bestToken == "" || order < bestOrder {
                    bestToken = tag
                    bestOrder = order
                }
                break
            }
        }
    }
    return bestToken, bestToken != ""
}
```

### 4.5 DFA 子集构造（BFS 到不动点）

```go
func (c *CombinedNFA) ToDFA() *DFA {
    dfa := NewDFA()
    
    // 1. 起点：所有 token 的 start closure 拼接
    startSets := c.computeStartSet()
    s0 := dfa.addCanonicalState(canonicalize(startSets))
    dfa.Start = s0
    
    // 2. 起点接受态
    if tag, ok := c.determineAccept(dfa.States[s0]); ok {
        dfa.Accepts[s0] = tag
    }
    
    // 3. BFS 探索
    worklist := []int{s0}
    visited := map[string]bool{hashState(dfa.States[s0]): true}
    
    for len(worklist) > 0 {
        s := worklist[0]
        worklist = worklist[1:]
        
        // 遍历所有字符
        for ch := 0; ch < 256; ch++ {
            nextSets := c.move(dfa.States[s], byte(ch))
            if len(nextSets) == 0 {
                continue  // 死路
            }
            
            nextCanonical := canonicalize(nextSets)
            sNext := dfa.addCanonicalState(nextCanonical)
            
            h := hashState(dfa.States[sNext])
            if !visited[h] {
                visited[h] = true
                if tag, ok := c.determineAccept(dfa.States[sNext]); ok {
                    dfa.Accepts[sNext] = tag
                }
                worklist = append(worklist, sNext)
            }
            
            // 添加转移
            if dfa.Trans[s] == nil {
                dfa.Trans[s] = make(map[byte][]int)
            }
            dfa.Trans[s][byte(ch)] = append(dfa.Trans[s][byte(ch)], sNext)
        }
    }
    
    return dfa
}
```

---

## 五、完整数据流

```
┌────────────────────────────────────────────────────────┐
│  1. 预算阶段（一次性）                                  │
│                                                        │
│  TokenNFAs[0]: ε-closure(start) → Sets₀                │
│  TokenNFAs[1]: ε-closure(start) → Sets₁                │
│  ...                                                   │
│  TokenNFAs[22]: ε-closure(start) → Sets₂₂              │
│                                                        │
│  ↓ 缓存到 startSets 数组                                │
└────────────────────────────────────────────────────────┘
                       ↓
┌────────────────────────────────────────────────────────┐
│  2. 子集构造阶段（BFS 到不动点）                        │
│                                                        │
│  worklist = [start_canonical]                          │
│  visited = {hash(start): true}                        │
│                                                        │
│  while worklist:                                       │
│    s = worklist.pop()                                  │
│    for ch in 0..255:                                   │
│      next_sets = move(s, ch)  # 各 token 独立 move + 拼接│
│      if next_sets 为空: continue                       │
│      s_next = canonicalize(next_sets)                  │
│      if s_next not in visited:                         │
│        visited.add(s_next)                             │
│        dfa.accepts[s_next] = determine_accept(next_sets)│
│        worklist.push(s_next)                          │
│      dfa.trans[s][ch] = s_next                       │
│                                                        │
│  ↓ 跑完返回 dfa                                        │
└────────────────────────────────────────────────────────┘
                       ↓
                     *DFA
```

---

## 六、关键设计决策

| 决策 | 方案 | 理由 |
| --- | --- | --- |
| **路径** | **D（预算 ε-closure）** | 不复制数据，预算一次，后续无 ε |
| **数据组织** | `CombinedNFA` 持 NFA 引用 | 不冗余，调试友好 |
| **DFA 状态编码** | `[]TokenStartSet` | 跨 NFA，集合即状态 |
| **去重** | 标准化 + hash | O(1) 查找 |
| **接受态冲突** | **先定义优先**（按 .glex 文件顺序） | 行为可预测 |
| **NFA AcceptTags** | **只在 Build 末尾的最终 e 上标记** | 叶子节点的 e 是中转点，不能误标为接受（参见 nfa/rules.go 注释） |
| **DSL 解析** | 用 `yaml.Node` 手动遍历 | Go map 迭代顺序随机，会破坏"位置越靠前优先级越高"语义 |
| **move 后的 ε-closure** | 只在触及小集合上算 | 性能 + 简单 |
| **字符集** | 0..255 全遍历 | ASCII 子集足够 |

### 关于"先定义优先"的两个关键修复

**问题 1：NFA AcceptTags 污染**
旧版 `processLeaf` 给**每个**叶子节点的 `e` 状态都打了 `AcceptTags[e] = tokenKey`。
例如 KW_FOR (`for` = Concat(Concat('f','o'),'r'))，构造完后 AcceptTags[1], [3], [5] 全是 KW_FOR。
合并 DFA 时，`determineAccept` 只要遇到"经过叶子 e"的状态就误判为接受，导致 KW 错误抢占 ID。

**修复**：[nfa/rules.go](file:///home/wpp/homework/complit-reason/lexical_analysis/internal/nfa/rules.go) `processLeaf` 不再设 AcceptTags。
只有 [nfa/build.go](file:///home/wpp/homework/complit-reason/lexical_analysis/internal/nfa/build.go) 末尾最终的 e 才被打标记。

**问题 2：YAML 解析丢顺序**
旧版 `dsl.go` 用 `map[string]string` 解析 tokens，Go map 迭代顺序随机。
即使 .glex 文件里把 KW_* 放在 ID 之前，运行时也可能反过来——优先级完全失效。

**修复**：[internal/dsl/dsl.go](file:///home/wpp/homework/complit-reason/lexical_analysis/internal/dsl/dsl.go) 改用 `yaml.Node`，手动遍历 MappingNode 的 `Content`（`[k1, v1, k2, v2, ...]`）保留文件顺序。

---

## 七、API 详细

### `internal/dfa/combined.go`

```go
package dfa

// 构造
func NewCombinedNFA(nfas []*nfa.NFA, keys []string) *CombinedNFA

// 主入口
func (c *CombinedNFA) ToDFA() *DFA

// 中间步骤（可单独调用 / 测试）
func (c *CombinedNFA) computeStartSet() []TokenStartSet
func (c *CombinedNFA) move(s dfaState, ch byte) dfaState
func (c *CombinedNFA) determineAccept(s dfaState) (string, bool)
```

### `internal/dfa/closure.go`

```go
// 标准 BFS ε-closure
func epsilonClosure(nfa *nfa.NFA, start int) []int
func epsilonClosureSet(nfa *nfa.NFA, starts []int) []int
```

### `internal/dfa/dfa.go`

```go
// DFA 主体
type DFA struct { ... }
func NewDFA() *DFA
func (d *DFA) addCanonicalState(sets []TokenStartSet) int
func (d *DFA) Start() int
func (d *DFA) NumStates() int
func (d *DFA) Accepts() map[int]string
func (d *DFA) Trans() []map[byte][]int
func (d *DFA) AcceptTag(state int) (string, bool)
```

### `internal/dfa/draw.go`

```go
// DOT 输出（用系统 dot 渲染）
func (d *DFA) ToDOT() string
func (d *DFA) ToPNG() ([]byte, error)
```

---

## 八、典型场景预估

| Token | NFA 状态数 | 预算 start closure |
| --- | --- | --- |
| `ID` | 6 | ~3 状态 |
| `NUM` | 8 | ~3 状态 |
| `REAL` | 14 | ~5 状态 |
| `KW_*`（4 个） | 4-10 | ~2-4 状态 |
| `OP_*`（10 个） | 2-4 | ~1-2 状态 |
| `SEP_*`（6 个） | 2 | ~1 状态 |
| **总计** | ~150 | ~100 状态 |

**预估 DFA 状态数**：200-500（典型值）

**性能预算**：
- 预算 ε-closure：23 × O(5) = ~115 次操作
- DFA 子集构造：~500 × 256 = 128K 次 move 操作
- **总耗时**：< 1 秒（普通机器）

---

## 九、典型错误情况

| 情况 | 处理 |
| --- | --- |
| 任何 token 都不接受 | DFA 全空（无 accept 态） |
| 多个 token 接受同一字符串 | **先定义优先**（Order 决定） |
| 单字符 token（如 `;`） | 状态转移直接走 1 步 |
| move 触及 0 状态 | 不创建边（DFA "死路"） |
| 同一字符到多目标（NFA 非确定） | DFA 子集构造自然处理（每个目标画一条边） |
| ε-closure 包含 start 自身 | BFS visited 跳过 |

---

## 十、测试策略

### 单元测试（`internal/dfa/dfa_test.go`）

| 测试 | 内容 |
| --- | --- |
| `TestEpsilonClosure` | BFS 正确性 |
| `TestEpsilonClosureSet` | 集合 ε-closure 正确性 |
| `TestMakeCanonical` | DFA 状态标准化（等价合并） |
| `TestMove` | 跨 token move 正确性 |
| `TestDetermineAccept` | 接受态冲突解决 |
| `TestToDFA_Simple` | 2 token 合并 |
| `TestToDFA_Overlapping` | 共享前缀（如 KW_IF / KW_IS） |
| `TestToDFA_LongestMatch` | 接受态冲突 |
| `TestToDFA_RealTokens` | 真实 23 token 合并 |
| `TestToDOT` | DOT 输出可解析 |
| `TestToPNG` | PNG 输出有效 |

### 端到端测试

- 跑 `glex -i examples/tokens.glex -build-dfa`
- 跑 `glex -i examples/tokens.glex -draw-dfa`（用 graphviz 画 DFA）
- 对 `input.txt` 跑 Tokenize，验证 token 流

---

## 十一、里程碑（M4 子任务）

| 任务 | 文件 | 内容 | 验收 |
| --- | --- | --- | --- |
| 1 | `internal/dfa/closure.go` | `epsilonClosure` + `epsilonClosureSet` | 单测全过 |
| 2 | `internal/dfa/state.go` | `dfaState` 类型 + hash + equals + canonicalize | 单测全过 |
| 3 | `internal/dfa/dfa.go` | `DFA` struct + addCanonicalState | 编译通过 |
| 4 | `internal/dfa/combined.go` | `CombinedNFA` struct + New + computeStartSet + move + determineAccept | 单测全过 |
| 5 | `internal/dfa/subset.go` | `ToDFA` 主入口（BFS 子集构造） | 单测全过 |
| 6 | `internal/dfa/draw.go` | DOT 输出 + ToPNG（用系统 dot） | 渲染可看 |
| 7 | `internal/dsl/dsl.go` | 装配 DFA 到 SpecFile | 端到端跑通 |
| 8 | `cmd/glex/main.go` | 加 `-build-dfa` / `-draw-dfa` flags | CLI 跑通 |
| 9 | 端到端测试 | 23 token 合并成功 + 跑通 input.txt | |

---

## 十二、为什么路径 D 比物理合并好（回顾）

| 维度 | 物理 Total NFA（路径 A） | 路径 D |
| --- | --- | --- |
| 数据拷贝 | 必须（offset） | **不拷贝** |
| 状态 ID 调试 | ❌ 偏移后看不清 | ✅ 局部 ID 保持 |
| 子集构造 | 标准 | 略改（move 内 ε-closure） |
| 性能 | 慢（拷贝 + 标准子集） | **快**（预算一次 + 简单子集） |
| 内存 | 23 份 + 1 份合并 | **23 份**（不冗余） |
| 教学价值 | 标准教科书写法 | 展示"虚合并"思路 |
| 最终 DFA | 一样 | **一样** |

**核心洞察**：
> DFA 是"概念上的大图"，但**数据上不一定要物理合并**。
> 
> 每个 token NFA 保持独立——"大图"只是**子集构造过程中的瞬时态**。

---

> **总结**：M4 路径 D = **散装 NFA + 预算 ε-closure + 跨 NFA 子集构造**。
> 23 个 NFA 保持独立，DFA 状态是"每个 token 当前状态"的集合。最终 DFA 与物理合并路径产出**完全一致**，但**代码更简洁、内存更省、调试更友好**。
