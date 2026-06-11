// Package codegen 把 IR → Go 源码（M4.2）
//
// Task A（MVP）：硬编码 hello world emit 模板
//   - 已知 main.ce 的 hello + say 结构
//   - 已知 stdio.ce 的 @Println
//   - 已知 io.ce 的 @write + SYSCALL 路由
//   - emit 出能跑的 Go 二进制
//
// 后续升级路径（M4.2-M4.3）：
//   - 用 go/ast 构造 emit（更类型安全）
//   - 支持任意 IR（不只 hello world）
package codegen

import (
	"bytes"
	"fmt"
	"strings"
)

// helloWorldSrc 是 Task A 硬编码的 hello world Go 源码
//
// 真实数据流：hello.h → say.hey/my/world → stdio.Println → io.write → SYSCALL → syscall.Write
// MVP 把整条链路 inline 成一个 Go 文件
const helloWorldSrc = `package main

import (
	"syscall"
	"time"
)

// hello: 创建字符串 h，发到 say
type Hello struct {
	h_ch chan string
}

func (h *Hello) run() {
	h.h_ch <- "hello world!"
}

// say: 3 个端口，每个端口收到数据后调用 stdio.Println
type Say struct {
	hey_ch   chan string
	my_ch    chan string
	world_ch chan string
}

func (s *Say) run() {
	for {
		select {
		case v := <-s.hey_ch:
			go stdio_Println(v)
		case v := <-s.my_ch:
			go stdio_Println(v)
		case v := <-s.world_ch:
			go stdio_Println(v)
		}
	}
}

// stdio.Println 的 inline 实现
// 完整路径：stdio.Println → io.write(fid=1, data) → SYSCALL(保留字) → syscall.Write
func stdio_Println(msg string) {
	data := msg + "\n"
	syscall.Write(1, []byte(data))
}

func main() {
	// 创建节点
	h := &Hello{h_ch: make(chan string)}
	s := &Say{
		hey_ch:   make(chan string),
		my_ch:    make(chan string),
		world_ch: make(chan string),
	}

	// 拓扑连线（main 包的 main{} 块）：
	// hello <out> say  async (fanout 3 分支)
	go func() {
		for {
			select {
			case v := <-h.h_ch:
				s.hey_ch <- v
				s.my_ch <- v
				s.world_ch <- v
			}
		}
	}()

	// 启动节点
	go h.run()
	go s.run()

	// MVP：等 100ms 让 goroutine 跑完
	time.Sleep(100 * time.Millisecond)
}
`

// simpleHelloSrc 最简版本（fallback）
const simpleHelloSrc = `package main

import (
	"syscall"
)

func main() {
	syscall.Write(1, []byte("hello world!\n"))
}
`

// EmitHelloWorldGo 返回 hello world 完整版的 Go 源码
func EmitHelloWorldGo() string {
	var buf bytes.Buffer
	buf.WriteString(helloWorldSrc)
	return buf.String()
}

// EmitSimpleHello 返回最简版本的 Go 源码
func EmitSimpleHello() string {
	return simpleHelloSrc
}

// FormatHelloExample 把两个版本的 emit 都展示出来（debug 用）
func FormatHelloExample() string {
	var sb strings.Builder
	sb.WriteString("=== EmitHelloWorldGo 输出 ===\n")
	sb.WriteString(EmitHelloWorldGo())
	sb.WriteString("\n=== EmitSimpleHello 输出 ===\n")
	sb.WriteString(EmitSimpleHello())
	return sb.String()
}

// FormatGoCode 包装（占位，未来 go/ast 时改这里）
func FormatGoCode(raw string) string {
	return raw
}

// ValidationCheck 简单的 sanity check（确保 emit 出的是合法 Go 源码骨架）
func ValidationCheck(goCode string) []string {
	var errs []string
	checks := []struct {
		name string
		ok   bool
	}{
		{"package main", strings.Contains(goCode, "package main")},
		{"import syscall", strings.Contains(goCode, `"syscall"`)},
		{"func main", strings.Contains(goCode, "func main()")},
		{"hello world string", strings.Contains(goCode, "hello world!")},
	}
	for _, c := range checks {
		if !c.ok {
			errs = append(errs, fmt.Sprintf("missing: %s", c.name))
		}
	}
	return errs
}
