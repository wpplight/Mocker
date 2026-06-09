// Package dsl 负责读取并解析 .glex 词法分析器定义文件，产出"全局统计表"。
//
// 全局统计表（SpecFile）记录：
//   - 所有 token 的有序列表（保留 DSL 中定义顺序）
//   - 所有 type（去重、排序）
//   - 每个 type 下包含哪些 token（TypeTokens）
//   - 每个 type 的 token 数量（TypeCount）
//   - 输出包名（Package）
//
// 此外，每个 Spec 都已包含解析好的 AST（regex.Regex），下游 NFA 构造可直接使用。
package dsl

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"lexical_analysis/internal/dfa"
	"lexical_analysis/internal/nfa"
	"lexical_analysis/internal/regex"
)

// ─────────────────────────────────────────────────────────────
// 数据结构
// ─────────────────────────────────────────────────────────────

// Spec 是单个 token 的完整描述。
type Spec struct {
	Key   string      // DSL 中原始 key，如 "OP_ADD"
	Type  string      // 解析出的 type，如 "OP"
	Name  string      // 解析出的 name，如 "ADD"
	Regex string      // 原始正则字符串
	AST   regex.Regex // 已解析的 AST 树
	NFA   *nfa.NFA    // 已构造的 NFA（纯数据）
	Order int         // 在 DSL 中的出现顺序（用于冲突解决）
}

// IsSingle 判断该 token 是否是其 type 的唯一实例（即 name == type）。
func (s Spec) IsSingle() bool {
	return s.Type == s.Name
}

// ─────────────────────────────────────────────────────────────

// SpecFile 是 glex 流水线中 M1（DSL 解析）的产物，
// 即"全局统计表"。
type SpecFile struct {
	Package string // 输出包名

	// ── 主数据 ──
	Tokens []Spec // 按 DSL 顺序排列的所有 token

	// ── 全局统计 ──
	Types       []string          // 去重 + 排序的所有 type
	TypeTokens  map[string][]Spec // type → 属于该 type 的 token 列表
	TypeCount   map[string]int    // type → token 数量
	TotalTokens int               // token 总数

	// ── M4 合并后的 DFA ──
	DFA *dfa.DFA // 所有 token 合并后的单个 DFA（可空，未装配）
}

// TypeExists 判断 type 是否存在。
func (sf *SpecFile) TypeExists(typ string) bool {
	_, ok := sf.TypeTokens[typ]
	return ok
}

// TokensOfType 返回指定 type 的所有 token。
// 返回的切片是内部副本，修改不影响原数据。
func (sf *SpecFile) TokensOfType(typ string) []Spec {
	src, ok := sf.TypeTokens[typ]
	if !ok {
		return nil
	}
	out := make([]Spec, len(src))
	copy(out, src)
	return out
}

// CountOfType 返回指定 type 的 token 数量（0 表示不存在）。
func (sf *SpecFile) CountOfType(typ string) int {
	return sf.TypeCount[typ]
}

// Stats 返回可读的统计信息摘要。
func (sf *SpecFile) Stats() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Package:       %s\n", sf.Package)
	fmt.Fprintf(&sb, "Total tokens:  %d\n", sf.TotalTokens)
	fmt.Fprintf(&sb, "Types (%d):\n", len(sf.Types))
	for _, t := range sf.Types {
		fmt.Fprintf(&sb, "  %s: %2d token(s)  [", t, sf.TypeCount[t])
		toks := sf.TypeTokens[t]
		for i, tok := range toks {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(tok.Key)
		}
		sb.WriteString("]\n")
	}
	return sb.String()
}

// String 简要描述该 SpecFile。
func (sf *SpecFile) String() string {
	return fmt.Sprintf("SpecFile{package=%s, tokens=%d, types=%d}",
		sf.Package, sf.TotalTokens, len(sf.Types))
}

// ─────────────────────────────────────────────────────────────
// 读取 / 解析
// ─────────────────────────────────────────────────────────────

// 内部使用的 raw 形式（YAML 友好）。
// Tokens 用 yaml.Node 而不是 map[string]string，因为
// map 的迭代顺序是随机的，会破坏"位置越靠前优先级越高"的语义。
type rawSpecFile struct {
	Package string    `yaml:"package"`
	Tokens  yaml.Node `yaml:"tokens"`
}

// ReadFile 读取并解析 .glex 文件，返回填充好的 SpecFile。
// 同时为每个 token 解析出 AST（regex.Regex）。
func ReadFile(path string) (*SpecFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}
	return Read(data)
}

// Read 直接从字节切片解析 DSL（用于测试或嵌入场景）。
func Read(data []byte) (*SpecFile, error) {
	var raw rawSpecFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml unmarshal: %w", err)
	}
	return buildSpecFile(&raw)
}

// ReadString 直接从字符串解析 DSL。
func ReadString(s string) (*SpecFile, error) {
	return Read([]byte(s))
}

// buildSpecFile 把 raw 形式构建成带统计的 SpecFile。
func buildSpecFile(raw *rawSpecFile) (*SpecFile, error) {
	if raw.Package == "" {
		return nil, fmt.Errorf("missing 'package' field")
	}

	// 从 yaml.Node 中按文件顺序提取 (key, regex) 对
	pairs, err := extractTokenPairs(&raw.Tokens)
	if err != nil {
		return nil, err
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no tokens defined under 'tokens:'")
	}

	sf := &SpecFile{
		Package:    raw.Package,
		TypeTokens: make(map[string][]Spec, len(pairs)),
		TypeCount:  make(map[string]int, len(pairs)),
	}

	seen := make(map[string]bool, len(pairs))
	order := 0
	for _, p := range pairs {
		key, regexStr := p.key, p.regex
		if key == "" {
			return nil, fmt.Errorf("empty token key")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate token key: %q", key)
		}
		seen[key] = true

		if regexStr == "" {
			return nil, fmt.Errorf("empty regex for token %q", key)
		}

		typ, name, err := parseKey(key)
		if err != nil {
			return nil, err
		}

		ast, err := regex.Parse(regexStr)
		if err != nil {
			return nil, fmt.Errorf("parse regex for %q: %w", key, err)
		}

		// 构造 NFA（用 Builder 模式）
		builder := nfa.NewBuilder()
		builder.Build(ast, key)
		tokenNFA := builder.NFA()

		s := Spec{
			Key:   key,
			Type:  typ,
			Name:  name,
			Regex: regexStr,
			AST:   ast,
			NFA:   tokenNFA,
			Order: order,
		}
		order++

		sf.Tokens = append(sf.Tokens, s)
		sf.TypeTokens[typ] = append(sf.TypeTokens[typ], s)
		sf.TypeCount[typ]++
	}

	// 构建排序的 Types 列表
	typeSet := make(map[string]struct{}, len(sf.Tokens))
	for _, t := range sf.Tokens {
		typeSet[t.Type] = struct{}{}
	}
	sf.Types = make([]string, 0, len(typeSet))
	for t := range typeSet {
		sf.Types = append(sf.Types, t)
	}
	sort.Strings(sf.Types)

	sf.TotalTokens = len(sf.Tokens)
	return sf, nil
}

// tokenPair 一次提取的 (key, regex)，按 .glex 文件顺序。
type tokenPair struct {
	key   string
	regex string
}

// extractTokenPairs 从 yaml.Node 中按文件顺序提取 token 定义。
// yaml.Node 是 yaml.v3 中能保留映射顺序的底层 AST。
func extractTokenPairs(node *yaml.Node) ([]tokenPair, error) {
	if node == nil {
		return nil, fmt.Errorf("tokens field missing")
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("'tokens:' must be a YAML mapping")
	}
	// MappingNode 的 Content 是 [k1, v1, k2, v2, ...] 顺序排列
	pairs := make([]tokenPair, 0, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("token key must be a scalar at line %d", keyNode.Line)
		}
		if valNode.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("token value must be a scalar at line %d", valNode.Line)
		}
		pairs = append(pairs, tokenPair{
			key:   keyNode.Value,
			regex: valNode.Value,
		})
	}
	return pairs, nil
}

// parseKey 解析 "TYPE_NAME" → (TYPE, NAME)。
//   - "ID"       → ("ID", "ID")
//   - "OP_ADD"   → ("OP", "ADD")
//   - "_FOO"     → 错误（type 为空）
//   - "FOO_"     → 错误（name 为空）
func parseKey(key string) (typ, name string, err error) {
	parts := strings.SplitN(key, "_", 2)
	if len(parts) == 1 {
		// 无下划线：name = type = key
		return parts[0], parts[0], nil
	}
	if parts[0] == "" {
		return "", "", fmt.Errorf("invalid key %q: empty type", key)
	}
	if parts[1] == "" {
		return "", "", fmt.Errorf("invalid key %q: empty name", key)
	}
	return parts[0], parts[1], nil
}
