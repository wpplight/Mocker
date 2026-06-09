// Package nfa 提供非确定有限自动机（NFA）的内存表示与 Thompson 构造。
//
// 架构：
//   - NFA：纯数据结构，构造完的产物
//   - Builder：构造时辅助（fragStack + travStack）
//   - rules.go：Thompson 构造规则（Builder 的方法）
//   - build.go：Build 入口（Builder 的方法）
//   - draw.go：DOT 输出 + goccy/go-graphviz 渲染
package nfa

// NFA 是非确定有限自动机的内存表示（纯数据）。
// 构造完后的最终产物，供用户读。
type NFA struct {
	Start      int                    // 起始状态 ID
	Accepts    []int                  // 所有接受态 ID
	AcceptTags map[int]string         // 接受态 → token key
	Trans      map[int]map[byte][]int // 字符边 (state × char → []target)
	Epsilon    map[int][]int          // ε 边 (state → []target)
	NextID     int                    // 下一个可用状态 ID
}

// New 新建一个空 NFA。
func New() *NFA {
	return &NFA{
		AcceptTags: make(map[int]string),
		Trans:      make(map[int]map[byte][]int),
		Epsilon:    make(map[int][]int),
	}
}

// NewState 分配新状态 ID。
func (n *NFA) NewState() int {
	id := n.NextID
	n.NextID++
	return id
}

// AddEpsilon 添加 ε 边。
func (n *NFA) AddEpsilon(from, to int) {
	n.Epsilon[from] = append(n.Epsilon[from], to)
}

// AddTrans 添加字符边。
func (n *NFA) AddTrans(from int, ch byte, to int) {
	if n.Trans[from] == nil {
		n.Trans[from] = make(map[byte][]int)
	}
	n.Trans[from][ch] = append(n.Trans[from][ch], to)
}

// NumStates 返回已分配状态数。
func (n *NFA) NumStates() int {
	return n.NextID
}
