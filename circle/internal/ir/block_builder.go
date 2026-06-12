// Package ir block_builder.go —— Block 范围构造器
//
// 从用户拍板的设计：
//   - 一个节点可以包含多个 block（每个 block 是独立的执行单元）
//   - 一个 block 由"输入端口" + "输出端口" + "body stmts" 组成
//   - **连续 >> name**（无 name >> 插入）→ 合并到同一个 block（多入度）
//   - 一旦出现 name >>，**该 block 进入"已输出"状态**
//   - 下一个 >> name：如果当前 block 已有输出 → 关闭当前，开新 block
//   - 否则 → 加入当前 block
//
// 例子：
//
//	@write {
//	    >> num fid          \  连续 2 个入度，无输出中断
//	    >> str data         /  → 合成 1 个 block (in=[fid,data], out=[fid,data])
//	    fid >>
//	    data >>
//	}
//
//	@Println {
//	    >> str msg         \
//	    nl := "\n"          \  1 个入度 + 2 个 stmt + 2 个出度
//	    fid := 1            /  → 1 个 block (in=[msg], out=[fid,data])
//	    fid >>             /
//	    data := msg + nl   /
//	    data >>
//	}
//
//	@calc {
//	    >> num a           \  入度 a
//	    a >>                → 出度 a，关闭 block
//	    >> num b           → 新 block，入度 b
//	    b >>                → 出度 b
//	}
//	→ 2 个 block (one per input)
package ir

import (
	"circle/internal/parser/ast"
)

// BlockBuilder 构造节点的所有 blocks
//
// 用法：
//
//	bb := NewBlockBuilder()
//	bb.AddInput("fid", pos)
//	bb.AddInput("data", pos)
//	bb.AddStmt(stmt1)
//	bb.AddStmt(stmt2)
//	bb.AddOutput("fid", nil, pos)
//	bb.AddOutput("data", nil, pos)
//	blocks := bb.Blocks()
type BlockBuilder struct {
	blocks       []*IRBlock
	currentBlock *IRBlock
}

// NewBlockBuilder 新构造器
func NewBlockBuilder() *BlockBuilder {
	return &BlockBuilder{}
}

// AddInput 处理 `>> type name`
//
// 规则：
//   - 没有 currentBlock → 开新 block（首入度）
//   - currentBlock 已有输出 → 关闭当前，开新 block
//   - currentBlock 无输出 → 加入当前（多入度合并）
func (b *BlockBuilder) AddInput(name string, pos ast.Pos) *IRBlock {
	if b.currentBlock == nil {
		// 首入度，开新 block
		return b.openNewBlock(name, pos)
	}
	if hasOutputs(b.currentBlock) {
		// 当前 block 已有输出，关闭并开新
		return b.openNewBlock(name, pos)
	}
	// 加入当前 block
	b.currentBlock.Inputs = append(b.currentBlock.Inputs, name)
	return b.currentBlock
}

// AddOutput 处理 `name >> [chain]`
//
// 规则：
//   - 没有 currentBlock → 开 auto-exec block（无入度有输出）
//   - 有 currentBlock → 加入当前（multi-output within block）
func (b *BlockBuilder) AddOutput(name string, chain []*ast.FlowStep, pos ast.Pos) *IRBlock {
	if b.currentBlock == nil {
		// 没 current → 开 auto-exec block
		b.currentBlock = &IRBlock{IsAutoExec: true, Pos: pos}
		b.blocks = append(b.blocks, b.currentBlock)
	}
	// 加入当前 block
	b.currentBlock.Outputs = append(b.currentBlock.Outputs, BlockOutput{
		Name:   name,
		StopAt: len(b.currentBlock.Stmts),
	})
	if chain != nil {
		b.currentBlock.Flow = append(b.currentBlock.Flow, resolveFlowOpsFromSteps(name, chain)...)
	}
	return b.currentBlock
}

// AddStmt 处理 `name := expr` / `type name = expr`
//
// 规则：
//   - 没有 currentBlock → 调用方应该把 stmt 放到 Init（不进 block）
//   - 有 currentBlock → 加到 Stmts
//
// 返回是否成功加入（false 表示无 currentBlock）
func (b *BlockBuilder) AddStmt(stmt IRStmt) bool {
	if b.currentBlock == nil {
		return false
	}
	b.currentBlock.Stmts = append(b.currentBlock.Stmts, stmt)
	return true
}

// HasCurrentBlock 返回是否有 current block
func (b *BlockBuilder) HasCurrentBlock() bool {
	return b.currentBlock != nil
}

// Blocks 返回所有构造的 blocks（拷贝值）
func (b *BlockBuilder) Blocks() []IRBlock {
	out := make([]IRBlock, 0, len(b.blocks))
	for _, bk := range b.blocks {
		out = append(out, *bk)
	}
	return out
}

// openNewBlock 关闭当前，开新 block，返回新 block
func (b *BlockBuilder) openNewBlock(name string, pos ast.Pos) *IRBlock {
	newB := &IRBlock{
		Inputs: []string{name},
		Pos:    pos,
	}
	b.blocks = append(b.blocks, newB)
	b.currentBlock = newB
	return newB
}

// hasOutputs 检查 block 是否有任何输出
func hasOutputs(b *IRBlock) bool {
	return b != nil && len(b.Outputs) > 0
}