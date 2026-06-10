// Command circle 是 Mocker DSL 编译器入口。
//
// 用法：
//
//	circle -ast -i <file.mocker>     解析并打印 AST
//	circle -i <file.mocker>          解析并打印 AST（默认）
//	circle -ast -i -                 从 stdin 读
//	circle -h                        帮助
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"circle/internal/parser"
)

func main() {
	var (
		showAST  = flag.Bool("ast", true, "解析并打印 AST（默认开启）")
		input    = flag.String("i", "", "输入文件路径；- 表示从 stdin 读")
		showHelp = flag.Bool("h", false, "显示帮助")
	)
	flag.Usage = usage
	flag.Parse()

	if *showHelp {
		usage()
		return
	}

	if *input == "" {
		// 默认：尝试用第 1 个位置参数
		if flag.NArg() >= 1 {
			*input = flag.Arg(0)
		} else {
			usage()
			os.Exit(1)
		}
	}

	// 读源
	src, err := readSource(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read error: %v\n", err)
		os.Exit(1)
	}

	// 解析
	file, errs := parser.Parse(src)

	// 打印错误
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "%s\n", e)
	}

	if file == nil {
		os.Exit(1)
	}

	if *showAST {
		fmt.Println("=== AST ===")
		fmt.Println(parser.Dump(file))
	}

	// 退出码：有错误就非 0
	if len(errs) > 0 {
		os.Exit(2)
	}
}

func readSource(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.TrimSpace(`
circle — Mocker DSL 编译器

用法:
  circle -ast -i <file.mocker>   解析并打印 AST
  circle -i <file.mocker>        解析并打印 AST（默认开启）
  circle -i -                    从 stdin 读
  circle -h                      显示本帮助

选项:
  -ast    解析并打印 AST（默认 true）
  -i      输入文件路径（必填）；"-" 表示 stdin
  -h      显示帮助

示例:
  circle -ast -i example/main.mocker
  cat example/cookie.mocker | circle -i -
`))
}
