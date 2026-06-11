package semantic

// Workspace scanner + BFS loader
// 跨包编译支持（M3 Task B）+ 多文件同包共享（M3 扩展）
//
// 用法（CLI 调用）：
//   circle                       // 从 cwd 扫描，找 main，BFS 加载所有可达包
//   circle -i <file.ce>          // 单文件模式（旧行为，向后兼容）
//
// 设计（按用户拍板）：
//   1. 编译时如果没指定 main 文件 → 对工作区进行扫描，查找 main 位置，
//      顺便记录所有包名（package name → file path）
//   2. 然后从 main 进行一个广度优先的迭代扫描：
//        - 不断入队 / 出队 package
//        - 首次入队的 package 会在下一轮循环中提取其 import
//        - 反复直到没有新的入队
//   3. 拿到所有包后做多包语义检查（每包建符号表 + 跨包查找）
//
// 多文件同包（用户拍板）：
//   - 同一个文件夹内的多个 .ce 文件可以使用同一个 package 名
//   - 这些文件共享变量（合并为一个 SymbolTable）
//   - 跨文件夹同名 = 错误（避免歧义）

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"circle/internal/parser"
	"circle/internal/parser/ast"
)

// ──── PackageInfo（一个包 = 同一文件夹下的多个 .ce 文件）────

// PackageInfo 描述一个包
//
// 一个包可以由同一个文件夹下的多个 .ce 文件组成，这些文件的 package 名必须一致，
// 它们共享同一个 PackageInfo，进而共享同一个 SymbolTable。
type PackageInfo struct {
	Name   string   // 包名
	Folder string   // 包所在文件夹
	Files  []string // 包内所有 .ce 文件路径（sorted）
}

// ──── 工作区扫描 ────

// ScanOptions 控制工作区扫描行为
type ScanOptions struct {
	Root       string // 工作区根目录（默认 "."）
	SkipHidden bool   // 跳过隐藏目录（默认 true）
}

// ScanWorkspace 扫描工作区找所有 .ce 文件，按 package 名分组
//
// 返回：pkg_name → *PackageInfo 的映射 + 扫描/解析错误
//
// 同文件夹同名 → 合并到同一 PackageInfo
// 跨文件夹同名 → 报错（duplicate package in different folders）
func ScanWorkspace(opts ScanOptions) (map[string]*PackageInfo, []SemanticError) {
	if opts.Root == "" {
		opts.Root = "."
	}

	pkgMap := map[string]*PackageInfo{}
	var errs []SemanticError

	walkErr := filepath.WalkDir(opts.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, SemanticError{
				Pos: ast.Pos{},
				Msg: fmt.Sprintf("walk error at %s: %v", path, err),
			})
			return nil
		}

		// 跳过隐藏目录
		if d.IsDir() {
			if opts.SkipHidden && strings.HasPrefix(d.Name(), ".") && path != opts.Root {
				return filepath.SkipDir
			}
			return nil
		}

		// 只处理 .ce 文件
		if !strings.HasSuffix(d.Name(), ".ce") {
			return nil
		}

		// 提取 package 名
		pkgName, perr := extractPackageName(path)
		if perr != nil {
			errs = append(errs, SemanticError{
				Pos: ast.Pos{},
				Msg: fmt.Sprintf("%s: %v", path, perr),
			})
			return nil
		}
		if pkgName == "" {
			return nil
		}

		// 计算文件所在文件夹（用于判断同文件夹）
		folder := filepath.Dir(path)

		// 已存在同名包？
		if existing, ok := pkgMap[pkgName]; ok {
			if existing.Folder == folder {
				// 同文件夹：合并到现有 PackageInfo
				existing.Files = append(existing.Files, path)
			} else {
				// 跨文件夹同名：报错
				errs = append(errs, SemanticError{
					Pos: ast.Pos{},
					Msg: fmt.Sprintf("duplicate package %q in different folders: %s vs %s",
						pkgName, existing.Folder, folder),
					Hint: "rename one of them, or merge files into one folder",
				})
			}
			return nil
		}

		// 新包
		pkgMap[pkgName] = &PackageInfo{
			Name:   pkgName,
			Folder: folder,
			Files:  []string{path},
		}
		return nil
	})

	if walkErr != nil {
		errs = append(errs, SemanticError{
			Pos: ast.Pos{},
			Msg: fmt.Sprintf("scan workspace: %v", walkErr),
		})
	}

	// 排序每个包的 files（保证 BFS 顺序稳定）
	for _, info := range pkgMap {
		sort.Strings(info.Files)
	}

	return pkgMap, errs
}

// extractPackageName 从 .ce 文件中提取 package 名
func extractPackageName(path string) (string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	file, perrs := parser.Parse(src)
	if file == nil {
		if len(perrs) > 0 {
			return "", fmt.Errorf("parse: %s", perrs[0].Error())
		}
		return "", fmt.Errorf("parse returned nil file")
	}
	if file.Pkg == nil {
		return "", nil
	}
	return file.Pkg.Name, nil
}

// ──── 找 main 包 ────

// FindMainPackage 从扫描结果中找到 main 包
func FindMainPackage(scan map[string]*PackageInfo) (*PackageInfo, error) {
	info, ok := scan["main"]
	if !ok {
		names := make([]string, 0, len(scan))
		for n := range scan {
			names = append(names, n)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("no main package found in workspace (scanned %d packages: %s)",
			len(scan), strings.Join(names, ", "))
	}
	return info, nil
}

// ──── BFS 加载 ────

// LoadWorkspaceBFS 从 startPkg 开始 BFS，加载所有可达包
//
// 算法（严格按用户拍板的描述）：
//  1. visited = {startPkg}
//     queue = [startPkg]
//  2. while queue 非空：
//     a. P = dequeue
//     b. 解析 P 的所有 .ce 文件（同一文件夹下多个文件）
//     c. 合并成一个 "merged File"（同一 package 名 + 合并 Decls）
//     d. 提取 P 的 import（"首次入队的 package 在下一轮循环中提取 import"）
//     e. 对每个 import I：
//     - 如果 I 未在 visited：
//     visited.add(I)
//     queue.append(I)    ← "首次入队"
//     f. 回到 step a
//  3. 直到 queue 为空
//
// 返回：pkg_name → merged AST + errors
func LoadWorkspaceBFS(startPkg string, scan map[string]*PackageInfo) (map[string]*ast.File, []SemanticError) {
	files := map[string]*ast.File{}
	var errs []SemanticError

	visited := map[string]bool{startPkg: true}
	queue := []string{startPkg}

	for len(queue) > 0 {
		// a. 出队
		pkg := queue[0]
		queue = queue[1:]

		// b. 找包
		info, ok := scan[pkg]
		if !ok {
			errs = append(errs, SemanticError{
				Pos:  ast.Pos{},
				Msg:  fmt.Sprintf("package %q imported but not found in workspace", pkg),
				Hint: fmt.Sprintf("ensure %q has a .ce file with `package %s` declaration", pkg, pkg),
			})
			continue
		}

		// c. 解析包内所有 .ce 文件，合并 AST
		merged, perrs := parseAndMergePackage(info)
		for _, pe := range perrs {
			errs = append(errs, pe)
		}
		if merged != nil {
			files[pkg] = merged
		}

		// d. 提取 import 入队
		if merged != nil {
			for _, decl := range merged.Decls {
				imp, ok := decl.(*ast.ImportDecl)
				if !ok {
					continue
				}
				if !visited[imp.Path] {
					visited[imp.Path] = true
					queue = append(queue, imp.Path)
				}
			}
		}
	}

	return files, errs
}

// parseAndMergePackage 解析包内所有文件，合并成一个虚拟 File
//
// 设计：
//   - 第一个文件的 package 声明作"主" package
//   - 所有文件的 Decls 合并（保留顺序）
//   - 同一文件中重复定义由 semantic 层抓（不在这里管）
func parseAndMergePackage(info *PackageInfo) (*ast.File, []SemanticError) {
	var errs []SemanticError
	var allDecls []ast.Decl
	var mainPkg *ast.PackageDecl

	for _, filePath := range info.Files {
		src, rerr := os.ReadFile(filePath)
		if rerr != nil {
			errs = append(errs, SemanticError{
				Pos:  ast.Pos{},
				Msg:  fmt.Sprintf("read %s: %v", filePath, rerr),
				Hint: "check file permissions",
			})
			continue
		}
		file, parseErrs := parser.Parse(src)
		for _, pe := range parseErrs {
			errs = append(errs, SemanticError{
				Pos:  ast.Pos{},
				Msg:  fmt.Sprintf("%s: %s", filePath, pe.Error()),
				Hint: "",
			})
		}
		if file == nil {
			continue
		}

		// 第一个文件定义主 package
		if mainPkg == nil && file.Pkg != nil {
			mainPkg = file.Pkg
		} else if file.Pkg != nil && mainPkg != nil && file.Pkg.Name != mainPkg.Name {
			// 同一文件夹内文件 package 名不一致（应该是 scanner 抓的，但兜底）
			errs = append(errs, SemanticError{
				Pos:  ast.Pos{},
				Msg:  fmt.Sprintf("package name mismatch within folder %s: %s vs %s", info.Folder, mainPkg.Name, file.Pkg.Name),
				Hint: "all files in same folder must declare the same package name",
			})
		}

		// 合并 Decls
		allDecls = append(allDecls, file.Decls...)
	}

	if mainPkg == nil {
		return nil, errs
	}

	merged := &ast.File{
		Pkg:   mainPkg,
		Decls: allDecls,
	}
	return merged, errs
}

// ──── 跨包查找 helper ────

// LookupPackage 在跨包符号表中查节点
func LookupPackage(tables map[string]*SymbolTable, pkgName, nodeName string) *NodeSymbol {
	t, ok := tables[pkgName]
	if !ok {
		return nil
	}
	return t.GetNode(nodeName)
}

// LookupPackageExport 跨包查 in/out
func LookupPackageExport(tables map[string]*SymbolTable, pkgName, nodeName, attrName string) (*InputSymbol, bool, bool) {
	t, ok := tables[pkgName]
	if !ok {
		return nil, false, false
	}
	return t.GetExport(nodeName, attrName)
}
