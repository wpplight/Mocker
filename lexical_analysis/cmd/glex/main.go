// glex 是 Go 词法分析器代码生成器（Go Lexer Generator）。
//
// 当前进度：M1（DSL）+ M2（AST）+ M3（NFA）+ M4（DFA）+ M5（最小化）+ M6（代码生成）已实现
//
// 用法（默认行为）：
//
//	glex -i tokens.glex                     # 读 .glex → 构 DFA → 最小化 → 生成 lexer.go
//	glex -i tokens.glex -o ./mylexer       # 自定义输出目录
//	glex -i tokens.glex -draw-nfa          # 额外画每个 token 的 NFA
//	glex -i tokens.glex -debug             # 输出所有调试信息到 output/debug/
//
// 用 -no-build-dfa / -no-gen-lexer 可以关掉默认行为。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lexical_analysis/internal/codegen"
	"lexical_analysis/internal/dfa"
	"lexical_analysis/internal/dsl"
	"lexical_analysis/internal/nfa"
	"lexical_analysis/internal/regex"
)

func main() {
	var (
		inputPath   = flag.String("i", "", "输入 .glex 定义文件路径（必填）")
		outputPath  = flag.String("o", "./output", "输出目录")
		packageName = flag.String("p", "", "覆盖包名（暂未使用）")
		verbose     = flag.Bool("v", false, "显示详细进度")
		showStats   = flag.Bool("stats", false, "仅打印统计表")
		showASTs    = flag.Bool("ast", false, "打印所有 token 的 AST")
		drawNFA     = flag.Bool("draw-nfa", false, "用 graphviz 画每个 token 的 NFA")
		buildDFA    = flag.Bool("build-dfa", true, "合并 NFA → DFA（含 M5 最小化）")
		genLexer    = flag.Bool("gen-lexer", true, "从 DFA 生成 Go 词法分析器源码到 output/<package>/lexer.go")
		debugMode   = flag.Bool("debug", false, "调试模式：输出 state.log/ast.txt/nfa/*.png/dfa.png 到 output/debug/")
		help        = flag.Bool("h", false, "显示帮助")
	)
	flag.Parse()

	if *help || *inputPath == "" {
		printUsage()
		if *inputPath == "" && !*help {
			os.Exit(1)
		}
		return
	}

	// debug 模式自动开启所有输出，但**只在 output/debug/ 下**写
	// （不重复写到 output/ 下）
	if *debugMode {
		*verbose = true
		*showStats = true
		*showASTs = true
		*drawNFA = false // debug 自己会画到 output/debug/
		*buildDFA = true
		*genLexer = false // debug 模式只调试，不过度生成产物
	}
	// -gen-lexer 隐含需要 DFA
	if *genLexer {
		*buildDFA = true
	}

	if *verbose {
		fmt.Printf("glex v0.6.0 (M1+M2+M3+M4+M5+M6)\n")
		fmt.Printf("→ 读取定义文件 %s\n", *inputPath)
	}

	sf, err := dsl.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glex: %v\n", err)
		os.Exit(1)
	}

	if *verbose {
		fmt.Printf("  ✓ 解析 %d 个 token\n", sf.TotalTokens)
		fmt.Printf("  ✓ 全部正则解析为 AST\n")
		fmt.Printf("  ✓ 全部 token 构造 NFA\n")
	}

	// ── M4: 合并 NFA → DFA ──
	if *buildDFA {
		if *verbose {
			fmt.Println("→ 合并 NFA → DFA")
		}
		nfas := make([]*nfa.NFA, 0, len(sf.Tokens))
		keys := make([]string, 0, len(sf.Tokens))
		for _, t1 := range sf.Tokens {
			nfas = append(nfas, t1.NFA)
			keys = append(keys, t1.Key)
		}
		combined := dfa.NewCombinedNFA(nfas, keys)
		raw := combined.ToDFA()
		if *verbose {
			fmt.Printf("  ✓ DFA (raw): %d states, %d accept(s)\n",
				raw.GetNumStates(), len(raw.GetAccepts()))
		}
		// M5 最小化：合并等价状态（如 s13/s14/.../s37 这 13 个 ID 接受态）
		sf.DFA = raw.Minimize()
		if *verbose {
			fmt.Printf("  ✓ DFA (minimized): %d states, %d accept(s)\n",
				sf.DFA.GetNumStates(), len(sf.DFA.GetAccepts()))
		}
	}

	if *showStats {
		fmt.Println()
		fmt.Println(sf.Stats())
	}

	if *showASTs {
		fmt.Println()
		fmt.Println("=== AST Trees ===")
		for _, t1 := range sf.Tokens {
			fmt.Printf("\n[%s]  %s\n", t1.Key, t1.Regex)
			fmt.Print(regex.Print(t1.AST))
		}
	}

	if *drawNFA {
		drawAllNFAs(sf, *outputPath, *verbose)
	}

	if *buildDFA && sf.DFA != nil && !*debugMode {
		// debug 模式下不重复写到 outputDir，写到 debug/ 即可
		drawDFA(sf, *outputPath, *verbose)
	}

	if *genLexer && sf.DFA != nil {
		writeGeneratedLexer(sf, *outputPath, *verbose)
	}

	if *debugMode {
		writeDebug(sf, *outputPath, *verbose)
	}

	_ = outputPath
	_ = packageName
}

// drawAllNFAs 输出每个 token 的 NFA（文本+DOT+PNG）
func drawAllNFAs(sf *dsl.SpecFile, outputDir string, verbose bool) {
	txtDir := filepath.Join(outputDir, "nfa_txt")
	pngDir := filepath.Join(outputDir, "nfa_png")
	os.MkdirAll(txtDir, 0755)
	os.MkdirAll(pngDir, 0755)

	if verbose {
		fmt.Println()
		fmt.Println("→ 绘制 NFA")
		fmt.Printf("  文本输出: %s/\n", txtDir)
		fmt.Printf("  PNG 输出:  %s/\n", pngDir)
	}

	for _, t1 := range sf.Tokens {
		if t1.NFA == nil {
			continue
		}
		txtPath := filepath.Join(txtDir, t1.Key+".txt")
		os.WriteFile(txtPath, []byte(t1.NFA.ToTXT()), 0644)

		dotPath := filepath.Join(txtDir, t1.Key+".dot")
		os.WriteFile(dotPath, []byte(t1.NFA.ToDOT()), 0644)

		pngPath := filepath.Join(pngDir, t1.Key+".png")
		if pngBytes, err := t1.NFA.ToPNG(); err == nil {
			os.WriteFile(pngPath, pngBytes, 0644)
			if verbose {
				fmt.Printf("  ✓ %s → %d states\n", t1.Key, t1.NFA.NumStates())
			}
		}
	}
}

// drawDFA 输出合并后的 DFA
func drawDFA(sf *dsl.SpecFile, outputDir string, verbose bool) {
	if sf.DFA == nil {
		return
	}
	txtDir := filepath.Join(outputDir, "dfa_txt")
	pngDir := filepath.Join(outputDir, "dfa_png")
	os.MkdirAll(txtDir, 0755)
	os.MkdirAll(pngDir, 0755)

	if verbose {
		fmt.Println()
		fmt.Println("→ 绘制 DFA")
		fmt.Printf("  文本: %s/dfa.txt\n", txtDir)
		fmt.Printf("  PNG:  %s/dfa.png\n", pngDir)
	}

	os.WriteFile(filepath.Join(txtDir, "dfa.txt"), []byte(sf.DFA.ToTXT()), 0644)
	os.WriteFile(filepath.Join(txtDir, "dfa.dot"), []byte(sf.DFA.ToDOT()), 0644)

	if pngBytes, err := sf.DFA.ToPNG(); err == nil {
		os.WriteFile(filepath.Join(pngDir, "dfa.png"), pngBytes, 0644)
		if verbose {
			fmt.Printf("  ✓ DFA: %d states, %d accepts\n",
				sf.DFA.GetNumStates(), len(sf.DFA.GetAccepts()))
		}
	}
}

// writeGeneratedLexer 调用 codegen 生成 Go 词法分析器源码。
func writeGeneratedLexer(sf *dsl.SpecFile, outputDir string, verbose bool) {
	pkgDir := filepath.Join(outputDir, sf.Package)
	if err := os.MkdirAll(pkgDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "glex: mkdir %s: %v\n", pkgDir, err)
		return
	}
	src, err := codegen.Generate(sf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glex: codegen: %v\n", err)
		return
	}
	lexerPath := filepath.Join(pkgDir, "lexer.go")
	if err := os.WriteFile(lexerPath, []byte(src), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "glex: write %s: %v\n", lexerPath, err)
		return
	}
	if verbose {
		fmt.Println()
		fmt.Println("→ 生成 lexer 源码")
		fmt.Printf("  ✓ %s (%d bytes, %d states, %d accepts)\n",
			lexerPath, len(src), sf.DFA.GetNumStates(), len(sf.DFA.GetAccepts()))
	}
}

// writeDebug 输出完整调试信息到 output/debug/
func writeDebug(sf *dsl.SpecFile, outputDir string, verbose bool) {
	debugDir := filepath.Join(outputDir, "debug")
	os.MkdirAll(debugDir, 0755)

	if verbose {
		fmt.Println()
		fmt.Println("→ 输出调试信息到 " + debugDir + "/")
	}

	// 1. state.log：完整流水线状态
	stateLog := buildStateLog(sf)
	os.WriteFile(filepath.Join(debugDir, "state.log"), []byte(stateLog), 0644)
	if verbose {
		fmt.Println("  ✓ state.log")
	}

	// 2. ast.txt：所有 token 的 AST
	astTxt := buildASTText(sf)
	os.WriteFile(filepath.Join(debugDir, "ast.txt"), []byte(astTxt), 0644)
	if verbose {
		fmt.Println("  ✓ ast.txt")
	}

	// 3. nfa_txt/{KEY}.txt + nfa_txt/{KEY}.dot + nfa_png/{KEY}.png
	nfaTxtDir := filepath.Join(debugDir, "nfa_txt")
	nfaPngDir := filepath.Join(debugDir, "nfa_png")
	os.MkdirAll(nfaTxtDir, 0755)
	os.MkdirAll(nfaPngDir, 0755)
	for _, t1 := range sf.Tokens {
		if t1.NFA == nil {
			continue
		}
		os.WriteFile(filepath.Join(nfaTxtDir, t1.Key+".txt"), []byte(t1.NFA.ToTXT()), 0644)
		os.WriteFile(filepath.Join(nfaTxtDir, t1.Key+".dot"), []byte(t1.NFA.ToDOT()), 0644)
		if pngBytes, err := t1.NFA.ToPNG(); err == nil {
			os.WriteFile(filepath.Join(nfaPngDir, t1.Key+".png"), pngBytes, 0644)
		}
	}
	if verbose {
		fmt.Printf("  ✓ NFA: %d 文件在 nfa_txt/ + nfa_png/\n", len(sf.Tokens))
	}

	// 4. dfa.txt + dfa.dot + dfa.png
	if sf.DFA != nil {
		os.WriteFile(filepath.Join(debugDir, "dfa.txt"), []byte(sf.DFA.ToTXT()), 0644)
		os.WriteFile(filepath.Join(debugDir, "dfa.dot"), []byte(sf.DFA.ToDOT()), 0644)
		if pngBytes, err := sf.DFA.ToPNG(); err == nil {
			os.WriteFile(filepath.Join(debugDir, "dfa.png"), pngBytes, 0644)
		}
		if verbose {
			fmt.Printf("  ✓ DFA: %d states, %d accepts\n",
				sf.DFA.GetNumStates(), len(sf.DFA.GetAccepts()))
		}
	}
}

func buildStateLog(sf *dsl.SpecFile) string {
	var sb fmtBuild
	sb.WriteString("════════════════════════════════════════════\n")
	sb.WriteString("  glex debug state log\n")
	sb.WriteString("  " + time.Now().Format("2006-01-02 15:04:05") + "\n")
	sb.WriteString("════════════════════════════════════════════\n\n")

	sb.WriteString("─── SpecFile ───\n")
	sb.WriteString("Package:    " + sf.Package + "\n")
	sb.WriteString("TotalTokens: " + itoa(sf.TotalTokens) + "\n")
	sb.WriteString("Types:      " + fmt.Sprintf("%v", sf.Types) + "\n\n")

	sb.WriteString("─── Per-token summary ───\n")
	for i, t1 := range sf.Tokens {
		sb.WriteString(fmt.Sprintf("[%2d] %-12s type=%-6s name=%-12s regex=%s\n",
			i, t1.Key, t1.Type, t1.Name, t1.Regex))
		if t1.NFA != nil {
			sb.WriteString(fmt.Sprintf("     NFA: %d states, %d accepts\n",
				t1.NFA.NumStates(), len(t1.NFA.Accepts)))
		}
	}

	if sf.DFA != nil {
		sb.WriteString("\n─── DFA ───\n")
		sb.WriteString(fmt.Sprintf("States:    %d\n", sf.DFA.GetNumStates()))
		sb.WriteString(fmt.Sprintf("Accepts:   %d\n", len(sf.DFA.GetAccepts())))
		for state, tag := range sf.DFA.GetAccepts() {
			sb.WriteString(fmt.Sprintf("  s%d → %s\n", state, tag))
		}
	}
	return sb.String()
}

func buildASTText(sf *dsl.SpecFile) string {
	var sb fmtBuild
	for _, t1 := range sf.Tokens {
		sb.WriteString(fmt.Sprintf("[%s]  %s\n", t1.Key, t1.Regex))
		sb.WriteString(regex.Print(t1.AST))
		sb.WriteString("\n")
	}
	return sb.String()
}

// fmtBuild 是 strings.Builder 的简单别名
type fmtBuild = strings.Builder

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func printUsage() {
	fmt.Print(`glex - Go 词法分析器代码生成器

用法:
  glex -i <input.glex> [选项]

必填:
  -i <file>      输入 token 定义文件（YAML）

输出:
  -o <dir>       输出目录（默认 ./output）
  -p <name>      覆盖包名（暂未使用）

默认行为（无需 flag）:
  -build-dfa     true    合并 NFA → DFA（含 M5 最小化）
  -gen-lexer     true    生成 Go 词法分析器源码到 output/<package>/lexer.go

可选:
  -v             显示详细进度
  -stats         打印统计表
  -ast           打印所有 token 的 AST
  -draw-nfa      用 graphviz 画每个 token 的 NFA
  -build-dfa=false   关掉 DFA 构造
  -gen-lexer=false   关掉代码生成
  -debug         调试模式：自动开启所有输出 + 写到 output/debug/
  -h             显示帮助

示例:
  glex -i examples/tokens.glex                  # 默认：DFA + lexer
  glex -i examples/tokens.glex -o ./mylexer     # 自定义输出
  glex -i examples/tokens.glex -gen-lexer=false # 只跑 DFA 不生成代码
  glex -i examples/tokens.glex -draw-nfa        # 额外画 NFA
  glex -i examples/tokens.glex -debug           # 全部调试信息
`)
}
