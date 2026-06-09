package dfa

// DFA 是子集构造的最终产物（确定有限自动机）。
type DFA struct {
	Start     int              // 起始状态 ID
	NumStates int              // 状态总数
	States    []dfaState       // stateId → 状态定义
	Trans     []map[byte][]int // trans[stateId][ch] = []nextStateId
	Accepts   map[int]string   // stateId → tokenKey
}

// NewDFA 新建一个空 DFA。
func NewDFA() *DFA {
	return &DFA{
		Start:   -1,
		Accepts: make(map[int]string),
	}
}

// addCanonicalState 把一个 dfaState 标准化后加入 DFA。
// 如果等价状态已存在，返回已有 ID；否则分配新 ID。
func (d *DFA) addCanonicalState(s dfaState) int {
	canon := canonicalize(s)
	for i, existing := range d.States {
		if existing.equals(canon) {
			return i
		}
	}
	id := len(d.States)
	d.States = append(d.States, canon)
	// Trans 跟着 States 一起长，保持 len(Trans) == len(States)
	if len(d.Trans) < len(d.States) {
		d.Trans = append(d.Trans, make(map[byte][]int, 1))
	}
	d.NumStates = len(d.States)
	return id
}

// GetStart 返回起始状态 ID。
func (d *DFA) GetStart() int { return d.Start }

// GetNumStates 返回状态总数。
func (d *DFA) GetNumStates() int { return d.NumStates }

// GetAccepts 返回所有接受态（stateId → tokenKey）。
func (d *DFA) GetAccepts() map[int]string { return d.Accepts }

// GetTrans 返回状态转移表。
func (d *DFA) GetTrans() []map[byte][]int { return d.Trans }

// AcceptTag 查询某状态是否接受态，返回 (tokenKey, ok)。
func (d *DFA) AcceptTag(state int) (string, bool) {
	tag, ok := d.Accepts[state]
	return tag, ok
}

// GetStateSet 返回第 i 个状态的 dfaState 定义。
func (d *DFA) GetStateSet(i int) dfaState {
	if i < 0 || i >= len(d.States) {
		return nil
	}
	return d.States[i]
}

// AddTransition 添加状态转移（去重）。
func (d *DFA) AddTransition(from int, ch byte, to int) {
	if from < 0 || from >= len(d.States) {
		return
	}
	if d.Trans[from] == nil {
		d.Trans[from] = make(map[byte][]int)
	}
	for _, t := range d.Trans[from][ch] {
		if t == to {
			return
		}
	}
	d.Trans[from][ch] = append(d.Trans[from][ch], to)
}

// SetAccept 设置接受态（先设置胜出）。
func (d *DFA) SetAccept(state int, tokenKey string) {
	if _, ok := d.Accepts[state]; !ok {
		d.Accepts[state] = tokenKey
	}
}
