// Package d2gen SVG 渲染（用 oss.terrastruct.com/d2 库）
//
// 调用 d2lib.Compile 把 d2 脚本编译成 d2graph，再 d2svg 渲染成 SVG 字节。
// 设计目标：让用户能在 circle CLI 里直接生成 SVG，无需安装 d2 CLI。
package d2gen

import (
	"context"
	"fmt"

	"oss.terrastruct.com/d2/d2graph"
	"oss.terrastruct.com/d2/d2layouts/d2dagrelayout"
	"oss.terrastruct.com/d2/d2lib"
	"oss.terrastruct.com/d2/d2renderers/d2svg"
	"oss.terrastruct.com/d2/d2themes/d2themescatalog"
	"oss.terrastruct.com/d2/lib/textmeasure"

	"circle/internal/ir"
)

// RenderSVG 从 IRProgram 直接渲染 SVG 字节
//
// 返回 SVG 字节流，可保存为 .svg 文件后在浏览器查看。
// 失败时返回错误（含 d2 解析/layout/render 各阶段的错误）。
func RenderSVG(prog *ir.IRProgram, opts *Options) ([]byte, error) {
	script := Generate(prog, opts)
	return RenderSVGFromScript(script)
}

// RenderSVGFromScript 从 d2 脚本字符串渲染 SVG
//
// 独立函数，方便测试和复用。内部用 d2lib + d2svg。
// 依赖 d2lib 自带的 layout 处理（d2lib.Compile 内部调 d2layouts）。
func RenderSVGFromScript(script string) ([]byte, error) {
	ctx := context.Background()

	// 创建文字测量 ruler（必须提供，否则 layout 阶段无法计算文本尺寸）
	ruler, err := textmeasure.NewRuler()
	if err != nil {
		return nil, fmt.Errorf("d2 ruler: %w", err)
	}

	// 需要提供 LayoutResolver（d2lib 调它）
	compileOpts := &d2lib.CompileOptions{
		Layout: ptrString("dagre"),
		Ruler:  ruler,
		LayoutResolver: func(engine string) (d2graph.LayoutGraph, error) {
			if engine == "dagre" {
				return func(ctx context.Context, g *d2graph.Graph) error {
					return d2dagrelayout.Layout(ctx, g, nil)
				}, nil
			}
			return nil, fmt.Errorf("unsupported layout engine: %s", engine)
		},
	}

	renderOpts := &d2svg.RenderOpts{
		ThemeID: ptrInt64(d2themescatalog.NeutralDefault.ID),
	}

	// d2lib.Compile 同时做 layout 和 export，返回 Diagram + Graph
	d, _, err := d2lib.Compile(ctx, script, compileOpts, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("d2 compile: %w", err)
	}
	if d == nil {
		return nil, fmt.Errorf("d2 compile: nil diagram")
	}

	// 渲染 SVG（用编译后的 Diagram）
	svgData, err := d2svg.Render(d, renderOpts)
	if err != nil {
		return nil, fmt.Errorf("d2svg render: %w", err)
	}

	return svgData, nil
}

// ptrString 返回 string 指针（d2lib 需要 *string）
func ptrString(s string) *string {
	return &s
}

// ptrInt64 返回 int64 指针（d2svg 需要 *int64）
func ptrInt64(n int64) *int64 {
	return &n
}

// RenderSVGToFile 渲染并写到文件（CLI 入口）
func RenderSVGToFile(prog *ir.IRProgram, opts *Options, path string) error {
	data, err := RenderSVG(prog, opts)
	if err != nil {
		return err
	}
	return writeFile(path, data)
}

// writeFile 写文件（独立函数，方便 mock 测试）
func writeFile(path string, data []byte) error {
	return writeFileImpl(path, data)
}
