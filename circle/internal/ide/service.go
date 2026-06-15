// Package ide 提供给 Mocker Studio（wails v2 GUI）调用的 service 层。
//
// 设计原则：
//   - Service 直接 import circle/internal/*（parser / semantic / ir / codegen）
//   - 通过 JSON struct（types.go）与前端通信，对齐 studio/.../store/editor.ts 的 ParsedFile
//   - 不做 marshal 内部 AST（emit_json.go 提供有限的、面向前端的展示字段）
//
// 调用入口：
//   - Service.ParseSource(src)        → 单文件解析（编辑器主流程）
//   - Service.ParseWorkspace()        → 解析整个 workspace
//   - Service.BuildGraph()            → 解析 + IR Lower → IRGraph JSON（驱动 React Flow）
//   - Service.Compile(opts)           → 编译 + 返回结果
//   - Service.Run(opts)               → 编译 + 流式运行
//   - Service.ApplyEdit(edit)         → 拖拽编辑（M1）
//   - Service.SerializeToSource()      → AST → .ce 文本（M1）
//   - Service.Version()               → 版本信息
package ide

import (
	"context"
	"runtime"
)

// Service wails v2 注入的 IDE 服务
//
// 单例无状态（除 workspaceDir / ctx），所有方法线程安全。
// wails v2 通过反射调用这些方法，所以必须导出（首字母大写）。
type Service struct {
	workspaceDir string
	ctx          context.Context
}

// NewService 构造 Service
//
// workspaceDir 可以为空，调用 ParseWorkspace 时会用 "."。
func NewService(workspaceDir string) *Service {
	return &Service{
		workspaceDir: workspaceDir,
	}
}

// SetContext 由 wails v2 OnStartup 注入 runtime context。
//
// 注意：wails v2 也会把这个方法生成给前端（带 context.Context 参数），
// 但前端不会调它——它是 wails v2 OnStartup 回调专用的注入口。
// 后续 EventEmit / EventsOn 都靠 s.ctx。
func (s *Service) SetContext(ctx context.Context) {
	s.ctx = ctx
}

// SetWorkspace 切换工作区（前端打开新项目时调）
func (s *Service) SetWorkspace(dir string) {
	s.workspaceDir = dir
}

// GetWorkspace 取当前工作区
func (s *Service) GetWorkspace() string {
	return s.workspaceDir
}

// Version 返回版本信息
func (s *Service) Version() *VersionInfo {
	return &VersionInfo{
		App:       "Mocker Studio",
		Build:     "dev",
		CircleDir: s.workspaceDir,
		GoVersion: runtime.Version(),
	}
}
