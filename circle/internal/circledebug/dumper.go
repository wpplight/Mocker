// Package circledebug 把编译器各阶段输出写到 ./debug/ 文件夹
//
// 用法：
//
//	d := circledebug.New("debug")
//	d.Add("01-ast.txt", astDump)
//	d.Add("02-semantic.txt", semDump)
//	d.Add("03-ir.txt", irDump)
//	d.Flush() // 写所有文件
//
// 文件名约定：
//   - 01-ast.txt      ：每个包的 AST dump
//   - 02-semantic.txt ：符号表 + 错误 + 入口点
//   - 03-ir.txt       ：IR dump
package circledebug

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Dumper debug 输出收集器
type Dumper struct {
	Dir   string
	Files map[string]string
}

// New 构造 dumper，dir 是目标文件夹
func New(dir string) *Dumper {
	return &Dumper{
		Dir:   dir,
		Files: map[string]string{},
	}
}

// Add 加一个文件（name 是相对 dir 的文件名）
func (d *Dumper) Add(name, content string) {
	if d == nil {
		return
	}
	d.Files[name] = content
}

// Flush 写所有文件到 dir
//
// 行为：
//   - 如果 dir 不存在，创建
//   - 按文件名排序写
//   - 如果目录已存在同名文件，覆盖
func (d *Dumper) Flush() error {
	if d == nil || len(d.Files) == 0 {
		return nil
	}
	if err := os.MkdirAll(d.Dir, 0755); err != nil {
		return fmt.Errorf("create debug dir %s: %w", d.Dir, err)
	}

	names := make([]string, 0, len(d.Files))
	for n := range d.Files {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		path := filepath.Join(d.Dir, name)
		content := d.Files[name]
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// FlushToStdout 把所有文件输出到 stdout（用于 run 模式不想写盘的情况）
func (d *Dumper) FlushToStdout() {
	if d == nil || len(d.Files) == 0 {
		return
	}
	names := make([]string, 0, len(d.Files))
	for n := range d.Files {
		names = append(names, n)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Printf("===== debug/%s =====\n%s\n", name, d.Files[name])
	}
}