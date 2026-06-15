package ide

import (
	"fmt"

	"circle/internal/ir"
	"circle/internal/semantic"
)

// BuildGraph 跑完整 pipeline 产生 React Flow 吃的 GraphData
//
// 流程：
//  1. workspace 扫包 + BFS 加载
//  2. semantic.CheckAll 多包语义检查
//  3. ir.Lower 把 AST 拍平到 IR
//  4. ir.BuildGraph(prog) 出 IRGraph
//  5. irGraphToGraphData() 翻译成 React Flow JSON（共享 helper）
func (s *Service) BuildGraph() (*GraphData, error) {
	workspace := s.workspaceDir
	if workspace == "" {
		workspace = "."
	}

	// 1. 扫 workspace
	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: workspace})

	// 2. 找 main
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		return nil, fmt.Errorf("find main package: %w", err)
	}

	// 3. BFS 加载
	files, _ := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)

	// 4. 语义检查
	wresult := semantic.CheckAll(files)

	// 5. IR Lower + BuildGraph
	prog := ir.Lower(wresult)
	g := ir.BuildGraph(prog)

	_ = scanErrs
	return irGraphToGraphData(g, files), nil
}

// ptrString 字符串 → 指针（前端吃 *string）
func ptrString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}