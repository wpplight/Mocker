package ide

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"circle/internal/ir"
	"circle/internal/parser"
	"circle/internal/parser/ast"
	"circle/internal/semantic"
)

// OpenWorkspace 打开一个工作区，返回 WorkspaceInfo
//
// 流程：
//  1. 校验目录存在
//  2. semantic.ScanWorkspace 扫所有 .ce 文件
//  3. 找 main 包 + 读 main 文件内容
//  4. semantic.CheckAll 多包检查
//  5. ir.Lower + ir.BuildGraph 出 GraphData
//  6. ParseWorkspace 出 ParsedFile（驱动 PropertiesPanel）
//
// 这是 M0.5 的核心 API —— 替代之前用户手工编辑代码的流程。
func (s *Service) OpenWorkspace(root string) (*WorkspaceInfo, error) {
	// 1. 校验 + 转绝对路径
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory: %s", abs)
	}

	// 2. 记住 workspace
	s.workspaceDir = abs

	return s.runFullPipeline(abs, "")
}

// ReparseWorkspace 编辑器实时反馈用 —— 把 in-memory 源码写到磁盘后再跑一次全 pipeline
//
// 统一入口：所有"代码改了要看新图"的路径都走这里，
//
//	保证 GraphEditor 始终显示的是"从 main 触发的完整多包图"。
//
// 流程：
//  1. 把 src 写到 <workspace>/<path>
//  2. 跑 OpenWorkspace 同款 pipeline（scan → BFS load → check → IR → graph）
//  3. 返回完整 WorkspaceInfo（GraphData 带 Packages / CrossPackage / Kind）
//
// 副作用：磁盘文件被 in-memory 内容覆盖（用户没 Ctrl+S 也算保存了）。
//
//	后续如果想做"未保存草稿"语义，再加 isDirty 标记。
func (s *Service) ReparseWorkspace(path, src string) (*WorkspaceInfo, error) {
	if s.workspaceDir == "" {
		return nil, fmt.Errorf("no workspace opened")
	}

	// 1. 写盘
	abs, err := s.absInWorkspace(path)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(abs, []byte(src), 0644); err != nil {
		return nil, fmt.Errorf("write file: %w", err)
	}

	// 2. 跑完整 pipeline
	return s.runFullPipeline(s.workspaceDir, src)
}

// runFullPipeline 跑"打开工作区"或"重新解析"的全流程，返回完整 WorkspaceInfo
//
// 提取这个 helper 是为了 OpenWorkspace 和 ReparseWorkspace 共用同一份逻辑，
// 保证两者产出的图完全一致（不会出现"启动时是包级图 / 编辑时是单文件图"）。
//
// latestMainSrc 是 ReparseWorkspace 刚写盘的内容（用于 MainSource 字段），
// OpenWorkspace 传 "" 时 fallback 到磁盘读到的内容。
func (s *Service) runFullPipeline(absRoot, latestMainSrc string) (*WorkspaceInfo, error) {
	abs := absRoot

	// 3. 扫包
	pkgMap, scanErrs := semantic.ScanWorkspace(semantic.ScanOptions{Root: abs})

	// 4. 找 main
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		return nil, fmt.Errorf("find main package: %w", err)
	}

	// 5. BFS 加载
	files, bfsErrs := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)

	// 6. 语义检查
	wresult := semantic.CheckAll(files)

	// 7. 文件清单（按包分组 + 按路径排序）
	var files2 []FileInfo
	for _, pkg := range pkgMap {
		for _, fpath := range pkg.Files {
			relPath, _ := filepath.Rel(abs, fpath)
			fi, _ := os.Stat(fpath)
			var size int64
			if fi != nil {
				size = fi.Size()
			}
			files2 = append(files2, FileInfo{
				Path:    filepath.ToSlash(relPath),
				AbsPath: fpath,
				Pkg:     pkg.Name,
				Size:    size,
			})
		}
	}
	sort.Slice(files2, func(i, j int) bool {
		return files2[i].Path < files2[j].Path
	})

	// 8. 读 main.ce 内容
	mainRelPath := "main.ce"
	var mainSource string
	var mainAbsPath string
	for _, fpath := range mainInfo.Files {
		if strings.HasSuffix(fpath, "main.ce") {
			mainRelPath, _ = filepath.Rel(abs, fpath)
			mainAbsPath = fpath
			break
		}
	}
	if mainAbsPath == "" && len(mainInfo.Files) > 0 {
		// fallback：包里第一个 .ce 文件当 main
		mainAbsPath = mainInfo.Files[0]
		mainRelPath, _ = filepath.Rel(abs, mainAbsPath)
	}
	// 优先用 ReparseWorkspace 刚写盘的最新内容
	if latestMainSrc != "" {
		mainSource = latestMainSrc
	} else if data, err := os.ReadFile(mainAbsPath); err == nil {
		mainSource = string(data)
	}

	// 9. 出 GraphData
	prog := ir.Lower(wresult)
	g := ir.BuildGraph(prog)
	addCrossPackageEdges(g, prog) // M1: 补 sub-instance / sub-flow 推出的跨包逻辑边
	graph := irGraphToGraphData(g, files)

	// 10. 出 ParsedFile
	pf := workspaceToParsedFile(files, wresult)
	// M1：把 `pf.Graph` 替换成 IR 级别多包图（含 packages / boundary / crossPackage）。
	//     `pf.Graph` 原本由 buildGraphFromFile（AST 级别）填充，只含 main 包的 instance，
	//     跨包折叠功能需要完整多包视图，所以用 `*graph`（已带 packages）覆盖。
	//     PropertiesPanel 用的是 `pf.Nodes`（NodeDetail 数组），不受影响。
	pf.Graph = *graph

	// 11. 收集 errors
	diags := []Diagnostic{}
	for _, e := range scanErrs {
		diags = append(diags, errorToDiagnostic(e))
	}
	for _, e := range bfsErrs {
		diags = append(diags, errorToDiagnostic(e))
	}
	for _, e := range wresult.Errors {
		diags = append(diags, errorToDiagnostic(e))
	}

	return &WorkspaceInfo{
		Root:       abs,
		PkgName:    mainInfo.Name,
		MainFile:   filepath.ToSlash(mainRelPath),
		MainSource: mainSource,
		Files:      files2,
		Graph:      *graph,
		Parsed:     pf,
		Errors:     diags,
	}, nil
}

// LoadFile 读单个 .ce 文件
//
// 文件必须在 workspace 内（防越界）。
func (s *Service) LoadFile(path string) (string, error) {
	if s.workspaceDir == "" {
		return "", fmt.Errorf("no workspace opened")
	}
	abs, err := s.absInWorkspace(path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	return string(data), nil
}

// SaveFile 写单个 .ce 文件
//
// 写完会重新跑 ParseWorkspace 把内存里的 AST/IR 同步更新。
// 注意：这是 MVP —— 写回只更新内存，不触发 SerializeToSource（M1）。
func (s *Service) SaveFile(path string, content string) error {
	if s.workspaceDir == "" {
		return fmt.Errorf("no workspace opened")
	}
	abs, err := s.absInWorkspace(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// LocateNode 在 workspace 里定位一个节点的源码位置
//
// 用于"双击图节点跳到 split 编辑器"：
//   - qualifiedName 形如 "main"（main 包内）或 "stdio.Println"（跨包）
//   - 返回所在 .ce 文件的相对路径 + struct 起始 Pos（行/列，从 1 开始计）
//
// 设计：每次调用都重新扫 + 解析（不缓存），保证最新磁盘内容。
// 不修改 workspace 状态。
type NodeLocation struct {
	Path string `json:"path"` // workspace 内相对路径（如 "stdio/stdio.ce"）
	Line int    `json:"line"` // 1-based
	Col  int    `json:"col"`  // 1-based
	Name string `json:"name"` // 节点名
}

func (s *Service) LocateNode(qualifiedName string) (*NodeLocation, error) {
	if s.workspaceDir == "" {
		return nil, fmt.Errorf("no workspace opened")
	}

	// 拆 pkg.name
	pkgName, nodeName := splitQualified(qualifiedName)
	if nodeName == "" {
		// 没有 pkg 前缀（如 "hello"）→ 走 main 包
		nodeName = pkgName
		pkgName = ""
	}

	// 重新扫一次（不修改任何状态）
	pkgMap, err := semantic.ScanWorkspace(semantic.ScanOptions{Root: s.workspaceDir})
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}

	// 没指定包 → 用 main
	if pkgName == "" {
		mainInfo, err := semantic.FindMainPackage(pkgMap)
		if err != nil {
			return nil, fmt.Errorf("find main: %w", err)
		}
		pkgName = mainInfo.Name
	}

	pkg, ok := pkgMap[pkgName]
	if !ok {
		return nil, fmt.Errorf("package %q not found in workspace", pkgName)
	}

	// 遍历包内文件，定位 struct
	for _, fpath := range pkg.Files {
		src, rerr := os.ReadFile(fpath)
		if rerr != nil {
			continue
		}
		file, _ := parser.Parse(src)
		if file == nil {
			continue
		}
		for _, decl := range file.Decls {
			sd, ok := decl.(*ast.StructDecl)
			if !ok || sd.Name != nodeName {
				continue
			}
			rel, _ := filepath.Rel(s.workspaceDir, fpath)
			pos := sd.Pos()
			return &NodeLocation{
				Path: filepath.ToSlash(rel),
				Line: pos.Line + 1, // parser 0-based，编辑器 1-based
				Col:  pos.Col + 1,
				Name: nodeName,
			}, nil
		}
	}
	return nil, fmt.Errorf("struct %q not found in package %q", nodeName, pkgName)
}

// splitQualified 拆 "pkg.name" → ("pkg", "name")；没有 "." 时 ("name", "")
func splitQualified(s string) (pkg, name string) {
	i := strings.LastIndex(s, ".")
	if i < 0 {
		return s, ""
	}
	return s[:i], s[i+1:]
}

// ParseFile 单独解析一个文件（编辑器实时反馈用）
//
// 不会改 workspace 状态，只返回 ParsedFile。
func (s *Service) ParseFile(path string) (*ParsedFile, error) {
	src, err := s.LoadFile(path)
	if err != nil {
		return nil, err
	}
	return s.ParseSource(src)
}

// InspectNode 取一个节点的 IR 运行时状态（M2 动态分析）
//
// MVP（M0.5）：直接返回静态 IR 信息，不实际跑到运行时。
// M2：codegen 加 debug emit 后，从子进程 stdout 抓 IRNodeState。
func (s *Service) InspectNode(nodeName string) (*IRNodeState, error) {
	prog := s.lastProgram()
	if prog == nil {
		return nil, fmt.Errorf("no program loaded")
	}
	for _, pkg := range prog.Packages {
		if n, ok := pkg.Nodes[nodeName]; ok {
			state := &IRNodeState{
				NodeName: nodeName,
				Inputs:   map[string]any{},
				Outputs:  map[string]any{},
			}
			for _, in := range n.Inputs {
				state.Inputs[in.Name] = in.Type.Name
			}
			for _, out := range n.Outputs {
				state.Outputs[out.Name] = nil // MVP：未知
			}
			for i, blk := range n.Blocks {
				state.Blocks = append(state.Blocks, BlockState{
					Name:   fmt.Sprintf("block[%d]", i),
					Status: blockStatus(blk),
				})
			}
			return state, nil
		}
	}
	return nil, fmt.Errorf("node %q not found", nodeName)
}

// ──── helpers ────

// absInWorkspace 把相对路径转绝对路径，并校验在工作区内（防越界）
func (s *Service) absInWorkspace(path string) (string, error) {
	cwd := s.workspaceDir
	abs := path
	if !filepath.IsAbs(path) {
		abs = filepath.Join(cwd, path)
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path escapes workspace: %s", path)
	}
	return abs, nil
}

// lastProgram 取最近一次 BuildGraph / OpenWorkspace 留下的 IRProgram
//
// 实现：跑一次 silent pipeline。这是个 MVP 实现，每次 InspectNode 都重新 lower 一次。
// M2 可以缓存到 Service 里。
func (s *Service) lastProgram() *ir.IRProgram {
	if s.workspaceDir == "" {
		return nil
	}
	pkgMap, _ := semantic.ScanWorkspace(semantic.ScanOptions{Root: s.workspaceDir})
	mainInfo, err := semantic.FindMainPackage(pkgMap)
	if err != nil {
		return nil
	}
	files, _ := semantic.LoadWorkspaceBFS(mainInfo.Name, pkgMap)
	wresult := semantic.CheckAll(files)
	return ir.Lower(wresult)
}

// blockStatus 把 IRBlock 转成 BlockState.Status（M0.5 MVP 静态判断）
func blockStatus(blk ir.IRBlock) string {
	if blk.IsAutoExec {
		return "auto-exec"
	}
	return "idle"
}

// workspaceToParsedFile 从 WorkspaceResult 拍成 ParsedFile
//
// M0.5：合并所有包到一个 ParsedFile（M1 会按包拆分）。
// 全部 slice 字段必须初始化成空 slice（非 nil），保证 JSON 输出 [] 而非 null，
// 前端能直接调 .length / .map 不用 null check。
func workspaceToParsedFile(files map[string]*ast.File, wresult *semantic.WorkspaceResult) *ParsedFile {
	pf := &ParsedFile{
		Imports: []string{},
		Nodes:   []NodeDetail{},
		Edges:   []EdgeDetail{},
		Enums:   []EnumDetail{},
		Graph: GraphData{
			Nodes: []FlowNode{},
			Edges: []FlowEdge{},
		},
	}

	// 第一遍：拿 main 包作为主视图
	mainFile, ok := files["main"]
	if !ok {
		return pf
	}

	pf.PackageName = mainFile.Pkg.Name
	for _, imp := range mainFile.Imports {
		pf.Imports = append(pf.Imports, imp.Path)
	}

	// enums / nodes / edges
	buildGraphFromFile(pf, mainFile, wresult.Tables["main"])
	_ = wresult
	return pf
}

// irGraphToGraphData 把 ir.IRGraph 转成 React Flow 吃的 GraphData
//
// M1：在原 Nodes / Edges 之上输出 Packages 分组 + 跨包边标记。
// （从 graph.go 抽出来，workspace 和 BuildGraph 都能用）
//
// files 用来在 FlowNode.Data 里塞 `members`，让 MockerNode 能渲染节点 body
// （vars / fields / sub_instances / sub_edges 等）。
// 传 nil 时不会报错，只是 body 不会展开（包/边等只显示 header 的节点不影响）。
func irGraphToGraphData(g *ir.IRGraph, files map[string]*ast.File) *GraphData {
	data := &GraphData{}

	// 0. 先建 node 索引和包映射（name → pkg）
	nodePkg := map[string]string{} // strip-pkg name → pkg name
	for name, n := range g.Nodes {
		pkg := ""
		if n.IRNode != nil {
			pkg = n.IRNode.Pkg
		}
		nodePkg[name] = pkg
	}

	// 1. 包汇总：扫描所有节点按 pkg 分组
	pkgOrder := []string{} // 包出现顺序（main 在前）
	pkgSeen := map[string]bool{}
	pkgNodes := map[string][]string{} // pkg → node ids
	for name := range g.Nodes {
		pkg := nodePkg[name]
		if !pkgSeen[pkg] {
			pkgSeen[pkg] = true
			pkgOrder = append(pkgOrder, pkg)
		}
		pkgNodes[pkg] = append(pkgNodes[pkg], "node-"+name)
	}

	// 2. 跨包边识别：扫描所有 edge，src 和 dst 都是真实节点且包不同 → 标记两端 boundary
	//
	// 注意：dst 可能是 SYSCALL 保留字 / 节点内部 instance var（"out" 等），
	//       这些不在 g.Nodes 里 → 它们的 pkg 为空。要把它们当作"同包内"
	//       （不是真正的跨包连接），不要画 ⇄ 标记。
	boundary := map[string]bool{} // node-id（"node-X"）→ 是否 boundary
	crossEdges := 0
	for _, e := range g.Edges {
		srcPkg, srcIsNode := nodePkg[e.Src]
		_, dstIsNode := nodePkg[e.Dst]
		if !srcIsNode || !dstIsNode {
			continue // src 或 dst 不是真实节点 → 跳过
		}
		dstPkg := nodePkg[e.Dst]
		if srcPkg != dstPkg {
			boundary["node-"+e.Src] = true
			boundary["node-"+e.Dst] = true
			crossEdges++
		}
	}

	// 3. 按生命周期边推层次布局：parent 在 child 上方
	//
	// 算法：
	//   - 用 lifecycle 边构建 parent → children 索引
	//   - 找 root（无 parent 的节点：main 节点、顶层入口）
	//   - BFS 给每个节点打 level（root=0, child=parent.level+1）
	//   - 同一 level 内的节点按 pkg / name 排，水平均布
	//   - 孤立节点（无 lifecycle 边进出）退回 pkg 分列布局
	parentOf := map[string]string{} // child name → parent name
	childrenOf := map[string][]string{}
	for _, e := range g.Edges {
		// lifecycle 边：FromTopology=false + 无 attr
		if e.FromTopology {
			continue
		}
		if e.SrcAttr != "" || e.DstAttr != "" {
			continue
		}
		if _, ok := g.Nodes[e.Src]; !ok {
			continue
		}
		if _, ok := g.Nodes[e.Dst]; !ok {
			continue
		}
		// 避免自环
		if e.Src == e.Dst {
			continue
		}
		if _, exists := parentOf[e.Dst]; !exists {
			parentOf[e.Dst] = e.Src
		}
		childrenOf[e.Src] = append(childrenOf[e.Src], e.Dst)
	}
	// 排序 children 让布局稳定
	for k := range childrenOf {
		sort.Strings(childrenOf[k])
	}

	// BFS 打 level
	level := map[string]int{}
	queue := []string{}
	// 找 root：没有 parent 的节点
	for name := range g.Nodes {
		if _, hasParent := parentOf[name]; !hasParent {
			level[name] = 0
			queue = append(queue, name)
		}
	}
	// BFS
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range childrenOf[cur] {
			if _, ok := level[child]; !ok {
				level[child] = level[cur] + 1
				queue = append(queue, child)
			}
		}
	}
	// 兜底：没分到 level 的节点（孤儿）放 level 0
	for name := range g.Nodes {
		if _, ok := level[name]; !ok {
			level[name] = 0
		}
	}

	// 按 level 分组 → 再按 pkg / name 排序稳定布局
	levelBuckets := map[int][]string{}
	for name, lv := range level {
		levelBuckets[lv] = append(levelBuckets[lv], name)
	}
	for lv := range levelBuckets {
		sort.Slice(levelBuckets[lv], func(i, j int) bool {
			ai, bi := levelBuckets[lv][i], levelBuckets[lv][j]
			api, bpi := nodePkg[ai], nodePkg[bi]
			if api != bpi {
				return api < bpi
			}
			return ai < bi
		})
	}
	// 每个节点在 level 内的列位置
	colInLevel := map[string]int{}
	for lv := 0; ; lv++ {
		names, ok := levelBuckets[lv]
		if !ok {
			break
		}
		for i, n := range names {
			colInLevel[n] = i
		}
	}

	// M1.x：父-子 x 对齐 —— 让 dataflow 边（父.attr → 子.input）能短一些
	//
	// 单父的节点 = 父的 x；
	// 多父的节点 = 第一个父的 x（但稍后会被兄弟节点挤开）；
	// 顶层节点（无父）= 维持原 pkg/name 排序的 x。
	childXAligned := map[string]int{}
	parents := map[string][]string{} // name → 直接父列表
	for _, e := range g.Edges {
		// lifecycle 边：FromTopology=false + 无 SrcAttr/DstAttr（来自 addCrossPackageEdges）
		if !e.FromTopology && e.SrcAttr == "" && e.DstAttr == "" {
			parents[e.Dst] = append(parents[e.Dst], e.Src)
		}
	}
	// 多轮迭代：祖→孙也能对齐
	for round := 0; round < 3; round++ {
		for name := range g.Nodes {
			ps := parents[name]
			if len(ps) == 0 {
				continue
			}
			for _, p := range ps {
				if pc, ok := childXAligned[p]; ok {
					childXAligned[name] = pc
					break
				}
				if pc, ok := colInLevel[p]; ok {
					childXAligned[name] = pc
					break
				}
			}
		}
	}
	// 同一 level 内有多个孩子对齐到同一 col 时，按 name 顺序横向展开
	// —— 防止兄弟节点堆叠到同一 x
	type childInfo struct {
		name  string
		col   int
		order int
	}
	byLevel := map[int][]childInfo{}
	for name, lv := range level {
		c, ok := childXAligned[name]
		if !ok {
			continue
		}
		// 找原排序里的 order
		idx := 0
		for i, n := range levelBuckets[lv] {
			if n == name {
				idx = i
				break
			}
		}
		byLevel[lv] = append(byLevel[lv], childInfo{name: name, col: c, order: idx})
	}
	// 同 col 的按 order 展开到连续 col
	for _, infos := range byLevel {
		// 按 col 升序，再按 order 升序
		sort.SliceStable(infos, func(i, j int) bool {
			if infos[i].col != infos[j].col {
				return infos[i].col < infos[j].col
			}
			return infos[i].order < infos[j].order
		})
		// 同 col 内的兄弟：从 col 开始横向展开
		colCursor := 0
		lastCol := -1
		for _, ci := range infos {
			if ci.col != lastCol {
				colCursor = ci.col
				lastCol = ci.col
			}
			colInLevel[ci.name] = colCursor
			colCursor++
		}
	}
	// 同 col 的"未对齐"节点（顶层 / 无父）也按 name 顺序接在最后
	for lv := 0; ; lv++ {
		names, ok := levelBuckets[lv]
		if !ok {
			break
		}
		sort.SliceStable(names, func(i, j int) bool {
			return colInLevel[names[i]] < colInLevel[names[j]]
		})
		levelBuckets[lv] = names
	}

	// 按 name 输出节点（保持 pkgOrder 让 Packages 列表稳定）
	// 决定每个节点的 (x, y)
	//
	// 居中策略：不做整体居中，每一列固定从 x=60 开始
	// —— 父-子 x 对齐后视觉上就是一颗直立的树，dataflow 边（左右 handle）短而清晰
	maxWidth := 1
	for _, names := range levelBuckets {
		if len(names) > maxWidth {
			maxWidth = len(names)
		}
	}
	const colWidth = 360
	const rowHeight = 280
	const leftPadding = 60
	positionFor := func(name string) map[string]float64 {
		lv := level[name]
		col := colInLevel[name]
		return map[string]float64{
			"x": float64(leftPadding + col*colWidth),
			"y": float64(60 + lv*rowHeight),
		}
	}

	for _, pkg := range pkgOrder {
		for _, nodeID := range pkgNodes[pkg] {
			name := nodeID[len("node-"):]
			n := g.Nodes[name]
			var kind string
			if n.IRNode != nil {
				kind = n.IRNode.Kind.String()
			}
			qualifiedName := name
			if pkg != "" {
				qualifiedName = pkg + "." + name
			}
			isB := boundary[nodeID]
			// 包默认折叠（非 main 包且有跨包边时）
			defaultCollapse := "expanded"
			if pkg != "" && !isPkgMain(pkg, g) {
				defaultCollapse = "package" // 受包折叠控制
			}

			// 找对应 StructDecl 拿 body members（MockerNode 渲染用）
			var members []NodeMember
			if files != nil {
				for _, f := range files {
					if f == nil {
						continue
					}
					for _, decl := range f.Decls {
						s, ok := decl.(*ast.StructDecl)
						if !ok || s.Name != name {
							continue
						}
						members = emitStructMembers(s.Members)
						break
					}
					if members != nil {
						break
					}
				}
			}
			if members == nil {
				members = []NodeMember{}
			}

			data.Nodes = append(data.Nodes, FlowNode{
				ID:            nodeID,
				Type:          "mockerNode",
				Name:          name,
				QualifiedName: qualifiedName,
				Pkg:           pkg,
				Exported:      n.IRNode != nil && n.IRNode.Exported,
				IsBoundary:    isB,
				CollapseState: defaultCollapse,
				Position:      positionFor(name),
				Data: map[string]any{
					"label":      name,
					"qualified":  qualifiedName,
					"kind":       kind,
					"exported":   n.IRNode != nil && n.IRNode.Exported,
					"isAutoExec": n.IsAutoExec,
					"isTerminal": n.IsTerminal,
					"isBoundary": isB,
					"fanIn":      n.FanInCount,
					"fanOut":     n.FanOutCount,
					// M1.x：把 body members 喂给前端 MockerNode，
					// 渲染 vars / fields / sub_instances / sub_edges / flow 等
					"members": members,
				},
			})
		}
	}

	// 4. 输出 FlowEdge（带包信息 + crossPackage + kind 标记）
	for _, e := range g.Edges {
		animated := e.Kind == ir.GraphEdgeFanout || e.SrcAttr == "" && e.DstAttr == "" && !e.FromTopology
		srcPkg, srcIsNode := nodePkg[e.Src]
		dstPkg, dstIsNode := nodePkg[e.Dst]
		cross := srcIsNode && dstIsNode && srcPkg != dstPkg

		// 推导 kind：
		//   - FromTopology + 无 attr → "topology"（main 的 Edges）
		//   - FromTopology + 有 attr → "topology"（仍然是 topology 边）
		//   - 来自 addCrossPackageEdges，FromTopology=false + 无 attr → "lifecycle"
		//   - 来自 addCrossPackageEdges，FromTopology=false + 有 attr → "dataflow"
		//   - 来自 BuildGraph block.Flow（FromTopology=false）→ "flow"
		kind := "flow"
		if e.FromTopology {
			kind = "topology"
		} else if e.SrcAttr == "" && e.DstAttr == "" {
			// 没 attr 且不在 topology → 来自 addCrossPackageEdges 的 SubInstance
			kind = "lifecycle"
		} else {
			// 有 attr（SrcAttr 或 DstAttr 之一）→ dataflow
			kind = "dataflow"
		}

		// 推导 React Flow handle id：
		//   - lifecycle（创建关系） → 节点顶部 lifecycle-in/lifecycle-out
		//   - dataflow / flow        → 节点左/右侧 port-in-X / port-out-X
		//   - topology（main 内的 <edge>） → 节点左/右侧（复用 port handle）
		srcHandle := e.SrcAttr
		dstHandle := e.DstAttr
		switch kind {
		case "lifecycle":
			srcHandle = "lifecycle-out"
			dstHandle = "lifecycle-in"
		case "dataflow", "flow", "topology":
			if srcHandle != "" {
				srcHandle = "port-out-" + srcHandle
			}
			if dstHandle != "" {
				dstHandle = "port-in-" + dstHandle
			}
		}

		data.Edges = append(data.Edges, FlowEdge{
			ID:           fmt.Sprintf("edge-%s-%s-%s-%d", e.Src, e.SrcAttr, e.Dst, e.Branch),
			Source:       "node-" + e.Src,
			Target:       "node-" + e.Dst,
			SourceHandle: ptrString(srcHandle),
			TargetHandle: ptrString(dstHandle),
			EdgeName:     fmt.Sprintf("%s.%s → %s.%s", e.Src, e.SrcAttr, e.Dst, e.DstAttr),
			Animated:     animated,
			SrcPkg:       srcPkg,
			DstPkg:       dstPkg,
			CrossPackage: cross,
			Kind:         kind,
			// Data 给 React Flow 自定义 edge 用：
			//   - data.kind     → StyledEdge 着色 / 动画
			//   - data.edgeName → 边标签显示
			//   - data.srcPkg/dstPkg/crossPackage → PropertiesPanel 详情
			Data: map[string]any{
				"kind":         kind,
				"edgeName":     fmt.Sprintf("%s.%s → %s.%s", e.Src, e.SrcAttr, e.Dst, e.DstAttr),
				"srcPkg":       srcPkg,
				"dstPkg":       dstPkg,
				"crossPackage": cross,
				"srcAttr":      e.SrcAttr,
				"dstAttr":      e.DstAttr,
			},
		})
	}

	// 5. 输出 PackageInfo
	for _, pkg := range pkgOrder {
		isMain := isPkgMain(pkg, g)
		boundaryIds := []string{}
		for _, id := range pkgNodes[pkg] {
			if boundary[id] {
				boundaryIds = append(boundaryIds, id)
			}
		}
		data.Packages = append(data.Packages, PackageInfo{
			Name:            pkg,
			IsMain:          isMain,
			NodeIds:         pkgNodes[pkg],
			BoundaryNodeIds: boundaryIds,
			// 默认折叠所有 non-main 包 —— 避免 stdio/io/netio 等标准库把图撑爆；
			// main 包是用户代码的核心脉络，保持展开。
			DefaultCollapsed: !isMain,
		})
	}

	return data
}

// isPkgMain 判断包是否是入口包（main）
func isPkgMain(pkg string, _ *ir.IRGraph) bool {
	return pkg == "" || pkg == "main"
}

// addCrossPackageEdges 给 g.Edges 补上"跨包逻辑边"——基于 SubInstances / SubFlows。
//
// 背景：IRGraph 只看 topology + block.Flow，sub-instance 之间的连接在节点 body
//
//	内部（IRNode.SubInstances / SubFlows）。这些连接在 codegen 时被处理，
//	但 IDE 可视化需要把它们画成跨包 / 跨节点边。
//
// 例子：main.hello body 里有 `stdio.Println p; out_str >> p.msg;`
//
//	→ 补一条 dataflow 边：main.hello.out_str → stdio.Println.msg
//	→ 补一条 lifecycle 边：main.hello → stdio.Println（创建 / 归属）
//
// 另外补 main 节点 body 的 VarInstances（main 是入口，它的 sub-instance 不在
//
//	IRNode.SubInstances 里，而在 main.Topology.VarInstances）。
//
// 边的 kind：
//   - SubInstance → "lifecycle"（父→子创建 / 归属）
//   - SubFlow / SubEdge → "dataflow"（数据流，带 attr）
//   - main.VarInstance → "lifecycle"（main 创建子节点）
func addCrossPackageEdges(g *ir.IRGraph, prog *ir.IRProgram) {
	for _, pkg := range prog.Packages {
		for _, n := range pkg.Nodes {
			// 1) 处理节点的 SubInstances / SubFlows / SubEdges
			addNodeSubEdges(g, n)

			// 2) main 节点还要处理 Topology.VarInstances
			if n.Name == "main" && pkg.Topology != nil {
				for instName, typeName := range pkg.Topology.VarInstances {
					_ = instName
					subStrip := stripPkgName(typeName)
					if _, exists := g.Nodes[subStrip]; !exists {
						continue
					}
					g.Edges = append(g.Edges, &ir.GraphEdge{
						Src:          n.Name,
						Dst:          subStrip,
						Kind:         ir.GraphEdgeDirect,
						FromTopology: false,
					})
				}
			}
		}
	}
}

// addNodeSubEdges 给单个节点加 SubInstances / SubFlows / SubEdges 推出的边
func addNodeSubEdges(g *ir.IRGraph, n *ir.IRNode) {
	if len(n.SubInstances) == 0 {
		return
	}
	// 把 instanceName → SubInstance 建索引
	subByInst := map[string]*ir.IRSubInstance{}
	for _, sub := range n.SubInstances {
		subByInst[sub.InstanceName] = sub
	}
	// 1) SubEdges：父节点 attr <edge> subInstance.attr → dataflow 边
	for _, se := range n.SubEdges {
		sub, ok := subByInst[se.DstInstance]
		if !ok {
			continue
		}
		subStrip := stripPkgName(sub.TypeName)
		if _, exists := g.Nodes[subStrip]; !exists {
			continue
		}
		g.Edges = append(g.Edges, &ir.GraphEdge{
			Src:          n.Name,
			SrcAttr:      se.SrcAttr,
			Dst:          subStrip,
			DstAttr:      se.DstAttr,
			Kind:         ir.GraphEdgeDirect,
			FromTopology: false,
		})
	}
	// 2) SubFlows：父节点 attr >> subInstance.input → dataflow 边
	for _, sf := range n.SubFlows {
		sub, ok := subByInst[sf.DstInstance]
		if !ok {
			continue
		}
		subStrip := stripPkgName(sub.TypeName)
		if _, exists := g.Nodes[subStrip]; !exists {
			continue
		}
		g.Edges = append(g.Edges, &ir.GraphEdge{
			Src:          n.Name,
			SrcAttr:      sf.SrcAttr,
			Dst:          subStrip,
			DstAttr:      sf.DstAttr,
			Kind:         ir.GraphEdgeDirect,
			FromTopology: false,
		})
	}
	// 3) SubInstances 本身：父节点 → subInstance（lifecycle 边，不带 attr）
	for _, sub := range n.SubInstances {
		subStrip := stripPkgName(sub.TypeName)
		if _, exists := g.Nodes[subStrip]; !exists {
			continue
		}
		g.Edges = append(g.Edges, &ir.GraphEdge{
			Src:          n.Name,
			Dst:          subStrip,
			Kind:         ir.GraphEdgeDirect,
			FromTopology: false,
		})
	}
}

// stripPkgName 剥掉 "stdio.Println" 这样的包前缀，还原成短名 "Println"
func stripPkgName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}
