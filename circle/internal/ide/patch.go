package ide

import "fmt"

// ApplyEdit 应用一个编辑操作（M1 拖拽编辑）
//
// MVP（M0）：直接报错，提示 "未实现"
// M1：实现 edit op 分发 + AST 内存修改 + 返回新 GraphData
func (s *Service) ApplyEdit(edit Edit) (*GraphData, error) {
	return nil, fmt.Errorf("ApplyEdit not implemented yet (M1)")
}

// SerializeToSource 把内存里的 AST 反向 emit 回 .ce 文本（M1）
//
// MVP（M0）：直接报错
func (s *Service) SerializeToSource() (string, error) {
	return "", fmt.Errorf("SerializeToSource not implemented yet (M1)")
}