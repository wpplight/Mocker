# Mocker 编译器架构设计

> **Mocker** = 结构化 DSL → 单个 Go 二进制 → 即开即用的 Mock HTTP 服务
>
> 前端开发时无需再为每个接口手写 mock 服务。

---

## 一、项目定位与愿景

### 1.1 用户视角的 3 步工作流

```bash
# 1. 写一份声明式 DSL（YAML）：API 端点 + 数据规则 + 行为
$ cat api.mocker
version: 1
server:
  port: 8080

routes:
  - method: GET
    path: /api/users/:id
    response:
      status: 200
      body:
        id:   "{{ .Params.id | int }}"
        name: "{{ .Faker.Name }}"
        age:  "{{ .Faker.Int 18 60 }}"
        avatar: "https://i.pravatar.cc/300?u={{ .Params.id }}"

  - method: POST
    path: /api/login
    request:
      body:
        username: string
        password: string
    response:
      status: 200
      body:
        token: "{{ .Hash.Sha256 .Body.username .Body.password }}"
        expiresIn: 3600
    rules:
      - when: "{{ eq .Body.username \"admin\" }}"
        status: 401
        body: { error: "invalid credentials" }

# 2. 跑编译器：DSL → 单个二进制
$ mocker build api.mocker -o ./mymock
✓ 词法分析：142 tokens
✓ 语法分析：2 routes, 3 rules
✓ 语义检查：通过
✓ 代码生成：mymock (8.4 MB)

# 3. 启动二进制：立即获得 Mock HTTP 服务
$ ./mymock
🚀 Mock server listening on :8080
   GET  /api/users/:id
   POST /api/login

# 前端无需任何改动，直接 fetch('http://localhost:8080/api/users/42')
```

### 1.2 与现有方案对比

| 方案 | 写 mock 的方式 | 启动方式 | 运行时 |
| --- | --- | --- | --- |
| MSW (浏览器) | 手写 JS handler | 浏览器加载 | 浏览器内 |
| json-server | 命令行 + JSON 文件 | `json-server db.json` | Node 进程 |
| Mocker | **一份声明式 DSL** | **单个 Go 二进制** | **单进程 HTTP 服务** |

**差异化卖点**：
- **单文件、单二进制**：产物可签入仓库，前端拷走就能跑，无需 Node/Python 环境。
- **AI 友好**：DSL 简洁、声明式，LLM 可生成、可校验。
- **丰富数据生成**：内置 Faker / Hash / 模板等，避免手写死数据。
- **支持动态行为**：`when` 条件 + 状态切换，能模拟错误路径。

---

## 二、整体流水线

```
┌──────────────────────────────────────────────────┐
│ ① 输入：api.mocker（YAML DSL）                    │
│    routes / request / response / rules / server   │
└──────────┬───────────────────────────────────────┘
           │  mocker build
           ▼
┌──────────────────────────────────────────────────┐
│ ② Mocker 编译器（本仓库）                          │
│                                                   │
│  ┌──────────────┐                                 │
│  │ Lexer        │  ← 已有（glex 词法层）           │
│  │  词法分析     │    .mocker → Token 流           │
│  └──────┬───────┘                                 │
│         ▼                                         │
│  ┌──────────────┐                                 │
│  │ Parser       │  ← 下一步要做                    │
│  │  语法分析     │    Token → AST                  │
│  └──────┬───────┘                                 │
│         ▼                                         │
│  ┌──────────────┐                                 │
│  │ Semantic     │  ← 第三步                       │
│  │  语义分析     │    类型检查 / 引用解析 / 校验    │
│  └──────┬───────┘                                 │
│         ▼                                         │
│  ┌──────────────┐                                 │
│  │ IR           │  中间表示                        │
│  │  规范化 AST   │    路由表 / handler 表 / 模板表  │
│  └──────┬───────┘                                 │
│         ▼                                         │
│  ┌──────────────┐                                 │
│  │ CodeGen      │  代码生成                        │
│  │  emit Go 源码 │    → main.go / router.go / ...  │
│  └──────┬───────┘                                 │
│         │                                         │
│         ▼                                         │
│  ┌──────────────┐                                 │
│  │ Embed +      │  模板 + 运行时嵌入二进制          │
│  │ Compile      │  go build → 单文件二进制          │
│  └──────────────┘                                 │
└──────────┬───────────────────────────────────────┘
           │  emit
           ▼
┌──────────────────────────────────────────────────┐
│ ③ 产物：单个 Go 二进制（mymock）                    │
│    ├── main() 启动 HTTP 服务                      │
│    ├── 路由表（路由注册）                           │
│    ├── handler 函数（业务逻辑）                     │
│    ├── 模板引擎（渲染 body / 校验条件）             │
│    └── 运行时库：fakers / hash / template          │
└──────────┬───────────────────────────────────────┘
           │  ./mymock
           ▼
┌──────────────────────────────────────────────────┐
│ ④ 运行时：Mock HTTP 服务                          │
│    监听端口，按 method+path 分发，渲染响应          │
└──────────────────────────────────────────────────┘
```

---

## 三、目录结构

```
Mocker/                              ← 仓库根
├── go.mod                           ← module Mocker
│
├── compiler/                        ← 编译器源码（重点新建）
│   ├── cmd/
│   │   └── mocker/
│   │       └── main.go              ← CLI 入口（build / run / validate）
│   │
│   ├── internal/                    ← 编译器内部包
│   │   ├── lexer/                   ← ① 词法分析
│   │   │   ├── token.go             ← TokenType / Token
│   │   │   ├── scanner.go           ← 手写词法扫描器
│   │   │   └── lexer_test.go
│   │   │
│   │   ├── parser/                  ← ② 语法分析
│   │   │   ├── ast.go               ← AST 节点定义
│   │   │   ├── parse_routes.go      ← 路由段解析
│   │   │   ├── parse_expr.go        ← 表达式 / 模板解析
│   │   │   ├── parse_rules.go       ← rules 段解析
│   │   │   └── parser_test.go
│   │   │
│   │   ├── semantic/                ← ③ 语义分析
│   │   │   ├── check.go             ← 入口
│   │   │   ├── resolve.go           ← 路径参数 / body 字段解析
│   │   │   ├── typeck.go            ← 类型检查（string/int/bool/...）
│   │   │   ├── validate.go          ← DSL 完整性校验
│   │   │   └── semantic_test.go
│   │   │
│   │   ├── ir/                      ← ④ 中间表示
│   │   │   ├── ir.go                ← IR 数据结构
│   │   │   └── lower.go             ← AST → IR
│   │   │
│   │   └── codegen/                 ← ⑤ 代码生成
│   │       ├── template/            ← 模板（main.go.tmpl / handler.go.tmpl）
│   │       ├── render.go            ← 公共渲染工具
│   │       ├── gen_main.go          ← 生成 main 入口
│   │       ├── gen_routes.go        ← 生成路由注册
│   │       ├── gen_handlers.go      ← 生成 handler
│   │       └── codegen_test.go
│   │
│   ├── runtime/                     ← 注入到产物的运行时库
│   │   ├── faker/                   ← Faker（姓名 / 邮箱 / 数字 / ...）
│   │   ├── hash/                    ← Hash（sha256 / md5）
│   │   ├── template/                ← 模板引擎（基于 text/template）
│   │   ├── jsonutil/                ← JSON 工具
│   │   └── router/                  ← HTTP 路由库（轻量自实现 / 用 chi）
│   │
│   ├── examples/                    ← 示例 DSL
│   │   ├── basic.mocker
│   │   ├── auth.mocker
│   │   └── crud.mocker
│   │
│   └── docs/
│       └── (本文档)
│
├── lexical_analysis/                ← 已完成的 glex 词法层
│
└── docs/
    └── Mocker架构设计.md             ← 本文档
```

---

## 四、DSL 设计（api.mocker）

### 4.1 顶层结构

```yaml
# api.mocker
version: 1                          # DSL 版本（用于未来兼容性）

server:                             # 可选：服务配置
  port: 8080                        # 默认 8080
  host: "0.0.0.0"                   # 默认 0.0.0.0
  cors: true                        # 默认 true
  latency:                          # 全局延迟（模拟网络）
    min: 50                         # ms
    max: 300
  headers:                          # 全局响应头
    X-Powered-By: "Mocker"

routes:                             # 路由列表
  - method: GET
    path: /api/users/:id
    ...

types:                             # 可选：自定义类型 / 复用定义
  User:
    id: int
    name: string
    email: string
```

### 4.2 route 完整结构

```yaml
- method: GET | POST | PUT | DELETE | PATCH        # 必填
  path: /api/users/:id                              # 必填，:param 占位
  description: "Get user by id"                     # 可选，纯注释

  request:                                          # 可选：请求约束
    headers:                                        #   必填的请求头
      Authorization: "Bearer .*"
    query:                                          #   必填的查询参数
      limit: int
    body:                                           #   必填的 body 字段（含类型）
      username: string
      password: string

  response:                                         # 必填：默认响应
    status: 200
    headers:
      Content-Type: "application/json"
    body:                                           # 任意 JSON 结构
      id: "{{ .Params.id | int }}"                  #   模板表达式
      name: "{{ .Faker.Name }}"
      ...

  rules:                                            # 可选：条件分支
    - when: "{{ .Body.username == \"admin\" }}"     #   触发条件（模板里求布尔）
      status: 401
      body:
        error: "invalid credentials"
    - when: "{{ not .Body.password }}"              #   多分支
      status: 400
      body:
        error: "password required"

  latency:                                          # 可选：单路由延迟（覆盖全局）
    min: 100
    max: 500
```

### 4.3 模板表达式语法（Go text/template 子集）

`{{ ... }}` 内可用：

| 上下文 | 可用字段 | 示例 |
| --- | --- | --- |
| 模板内 | `.Params.<name>` | `{{ .Params.id }}` |
| 模板内 | `.Query.<name>` | `{{ .Query.limit }}` |
| 模板内 | `.Body.<name>` | `{{ .Body.username }}` |
| 模板内 | `.Headers.<name>` | `{{ .Headers.Authorization }}` |
| 模板内 | `.Faker.*` | `{{ .Faker.Name }}` `{{ .Faker.Int 18 60 }}` |
| 模板内 | `.Hash.*` | `{{ .Hash.Sha256 "salt" .Body.password }}` |
| 模板内 | `.Now` | `{{ .Now }}` `{{ .Now.AddDate 0 0 7 }}` |
| 模板内 | Go template 函数 | `eq / ne / lt / len / printf / ...` |

**关键原则**：模板只在 **响应构造** 和 **when 条件** 中出现；其它地方都是结构化 YAML。

---

## 五、编译器内部模块详解

### 5.1 词法分析（internal/lexer）—— **已完成**

**职责**：把 `api.mocker` 文本切成 Token 流。

**关键 Token 类型**：
```
IDENT, STRING, INT, FLOAT, BOOL, NULL,
COLON, DASH, COMMA, LBRACE, RBRACE, LBRACKET, RBRACKET,
TEMPLATE,                          // {{ ... }}
INDENT, DEDENT, NEWLINE, EOF,
KEYWORD_version, KEYWORD_server, KEYWORD_routes,
KEYWORD_method, KEYWORD_path, KEYWORD_response,
KEYWORD_request, KEYWORD_rules, KEYWORD_when,
KEYWORD_status, KEYWORD_headers, KEYWORD_body, KEYWORD_query,
...
```

**特殊处理**：
- YAML 缩进敏感的 token 流：扫描器维护 indent stack，emit `INDENT`/`DEDENT`。
- `{{ ... }}` 模板字面量：作为整体 token（`TEMPLATE`），里面只做括号匹配，**不展开分析**——模板语义留到运行时。

### 5.2 语法分析（internal/parser）—— **下一步要做**

**职责**：Token 流 → AST。

**AST 节点**：
```go
type File struct {
    Version  int
    Server   *ServerConfig
    Routes   []*Route
    Types    map[string]*TypeDef
}

type Route struct {
    Method      string
    Path        string
    Description string
    Params      []string               // :id, :name
    Request     *Request
    Response    *Response
    Rules       []*Rule
    Latency     *Latency
}

type Request struct {
    Headers map[string]string
    Query   map[string]string         // value 是类型名
    Body    map[string]string
}

type Response struct {
    Status  int
    Headers map[string]string
    Body    Expr                       // 可能是字面量 map 或模板
}

type Rule struct {
    When     Expr                      // 布尔模板
    Status   int
    Headers  map[string]string
    Body     Expr
}

type Expr interface{ exprNode() }
type ExprMap   map[string]Expr
type ExprList  []Expr
type ExprLit   interface{}             // string / int / bool / null
type ExprTmpl  string                  // {{ ... }}
```

**解析策略**：递归下降 + 缩进敏感（类似 Python 的 INDENT/DEDENT 流）。

```
parseFile()
   ├── parseVersion()
   ├── parseServer()
   ├── parseRoutes() ───► parseRoute() ──► parseRequest/Response/Rules
   └── parseTypes()
```

**错误恢复**：单条 route 解析失败 → 跳过到下一条 `-`，继续解析剩余 routes，错误信息收集到列表一起报。

### 5.3 语义分析（internal/semantic）—— **第三步**

**职责**：在 IR 之前做正确性检查。

**检查项**：

| 检查 | 说明 |
| --- | --- |
| **路径参数一致性** | `path: /users/:id` 里 `:id` 必须在响应里被合理使用（或被忽略） |
| **body 字段类型合法** | `string / int / float / bool / object / array / template` |
| **when 引用合法** | `.Body.xxx` 必须出现在 request.body 里；`.Query.xxx` 必须出现在 query 里 |
| **when 类型合法** | 顶层表达式必须是布尔（模板会渲染后断言） |
| **模板静态扫描** | 用 `text/template.Parse` 试解析 `{{ ... }}`，失败则报错（**不执行**，只 parse） |
| **重复路由** | `(method, path)` 不可重复 |
| **method 合法** | GET/POST/PUT/DELETE/PATCH |
| **status 合法** | 100-599 |
| **rules 顺序** | 第一条匹配的规则生效；后续规则仍合法但优先级更低 |
| **types 引用** | 若声明了 `types:`，则 body 字段可引用类型名作校验（可选高级功能） |

**数据结构**：
```go
type Checker struct {
    file   *ast.File
    errors []SemanticError
}

type SemanticError struct {
    Pos     token.Pos
    Message string
    Hint    string                  // 修复建议
}
```

### 5.4 中间表示（internal/ir）

**职责**：把 AST 标准化成"代码生成器友好"的形式。

```go
type IR struct {
    Server    ServerConfig
    Routes    []IRRoute
    Templates []Template             // 去重后的模板列表
}

type IRRoute struct {
    Method      string
    Path        string
    Pattern     string               // 编译后的路由模式（chi 风格）
    HandlerName string               // 生成代码里的函数名
    Request     IRRequest
    Response    IRResponse
    Rules       []IRRule
    Latency     *Latency
}

type IRRequest struct {
    Headers map[string]string
    Query   []Field                 // 含类型
    Body    []Field
}

type IRResponse struct {
    Status  int
    Headers map[string]string
    Body    IRExpr                  // 规范化后的表达式树
}

type IRRule struct {
    WhenExpr string                 // 原始模板字符串
    Status   int
    Body     IRExpr
}
```

**为什么有 IR**：AST 反映源结构；IR 反映"代码生成器需要什么"。例如：
- AST 里 `:id` 是字符串，IR 里拆成 `Params` 列表。
- AST 里 `body: { x: "{{ ... }}" }`，IR 里把模板字符串单独编号引用。

### 5.5 代码生成（internal/codegen）

**职责**：IR → Go 源文件树 → 嵌入二进制 → `go build`。

**生成的文件**（位于临时目录 `out/src/`）：
```
out/src/
├── main.go                 ← 启动 HTTP 服务、注册路由
├── routes_gen.go           ← 路由表（chi router / 自实现）
├── handlers_gen.go         ← 每个 route 一个 handleXxx 函数
├── bodies_gen.go           ← body 构造逻辑
├── templates_gen.go        ← 编译后的 template.Template 变量
└── go.mod                  ← 临时 module（指向 runtime 库）
```

**生成示例**（handlers_gen.go 节选）：
```go
//go:generate echo "DO NOT EDIT"
package main

import (
    "net/http"
    "time"
    "github.com/go-chi/chi/v5"
    "Mocker/compiler/runtime/template"
    "Mocker/compiler/runtime/faker"
)

var tplUserResponse = template.MustParse(`{
    "id":   "{{ .Params.id | int }}",
    "name": "{{ .Faker.Name }}",
    "age":  "{{ .Faker.Int 18 60 }}"
}`)

func handleGetUser(w http.ResponseWriter, r *http.Request) {
    ctx := newRenderCtx(r)              // 解析 :id / body / headers

    // 命中规则判断（按顺序）
    if matchRuleUserAdmin(ctx) {
        writeJSON(w, 401, bodyUserAdmin)
        return
    }

    // 默认响应
    delay(50, 300)                      // 模拟延迟
    data, _ := renderTemplate(tplUserResponse, ctx)
    writeJSON(w, 200, data)
}
```

**嵌入流程**：
```
1. 渲染所有模板源码 → 写到临时目录 out/src/
2. 生成临时 go.mod（require runtime 库）
3. 执行 `go build -o mymock out/src/` → 单二进制
4. 清理临时目录
```

> **关键优化**：runtime 库（faker/hash/template/router）在 `Mocker/compiler/runtime` 下，**编译时 link 进二进制**——产物**不依赖任何外部模块**（除标准库和 chi 等极少第三方）。

---

## 六、运行时库（compiler/runtime）

### 6.1 faker

```go
package faker

type Faker struct{ rng *rand.Rand }

func New() *Faker
func (f *Faker) Name() string               // "Alice Wang"
func (f *Faker) FirstName() string          // "Alice"
func (f *Faker) LastName() string           // "Wang"
func (f *Faker) Email() string              // "alice42@example.com"
func (f *Faker) Phone() string              // "138****1234"
func (f *Faker) URL() string
func (f *Faker) UUID() string               // 标准 UUID v4
func (f *Faker) Int(min, max int) int
func (f *Faker) Float(min, max float64) float64
func (f *Faker) Bool() bool
func (f *Faker) Date() string               // ISO 8601
func (f *Faker) DateTime() string
func (f *Faker) Pick(slice ...string) string // 随机选一
func (f *Faker) Paragraph(n int) string
```

**确定性模式（可选）**：CLI flag `--seed 42`，让前端开发时 mock 数据稳定可重现。

### 6.2 hash

```go
package hash

func Sha256(parts ...string) string          // 拼接后 sha256 hex
func Md5(parts ...string) string
func Bcrypt(password string) string          // 用于密码字段
func JWT(payload, secret string) string      // 简易 mock JWT
```

### 6.3 template

基于 `text/template`，注册自定义 FuncMap：
```go
func init() {
    Funcs = template.FuncMap{
        "int":    toInt,
        "float":  toFloat,
        "string": toString,
        "default": defaultVal,
        "pick":   faker.Pick,
        "json":   toJSON,
        "upper":  strings.ToUpper,
        "lower":  strings.ToLower,
        "trim":   strings.TrimSpace,
        "replace": strings.ReplaceAll,
        "join":   strings.Join,
    }
}
```

### 6.4 router

轻量包装 [chi/v5](https://github.com/go-chi/chi)：
- 支持 `:id` 路径参数
- 支持 method 分发
- 体积小（~ 50KB 进二进制）

---

## 七、CLI 设计

### 7.1 子命令

```
mocker — Mock HTTP 服务编译器

用法:
  mocker build <input.mocker> -o <binary> [选项]
  mocker run   <input.mocker> [选项]        # 编译并立即运行
  mocker validate <input.mocker>            # 只校验，不产出二进制
  mocker fmt   <input.mocker>               # 格式化（可选）

build 选项:
  -o, --output <file>        输出二进制路径（必填）
      --runtime <dir>        运行时库目录（默认 ./compiler/runtime）
      --seed <int>           Faker 随机种子（默认时间戳）
      --no-cors              关闭默认 CORS
      --ldflags <string>     传给 go build
      --keep-temp            保留临时源码目录（debug 用）
      -v, --verbose          显示进度
      -q, --quiet            只输出错误

run 选项:（继承 build 选项 + ）
      --port <int>           端口（覆盖 DSL）
      --host <string>        主机（覆盖 DSL）

通用:
  -h, --help
      --version
```

### 7.2 使用示例

```bash
# 标准用法
mocker build api.mocker -o ./mymock

# 立即跑
mocker run api.mocker --port 3000

# 校验（不发产物，用于 CI / pre-commit）
mocker validate api.mocker

# 调试：保留临时源码
mocker build api.mocker -o ./mymock --keep-temp -v
# → 临时目录: out/src/  可以 cd 进去 go build / 调

# 产物运行
./mymock
🚀 Mock server listening on :8080
   GET  /api/users/:id
   POST /api/login
```

### 7.3 进度输出（verbose）

```
$ mocker build api.mocker -o ./mymock -v
mocker v0.1.0
→ 词法分析
  ✓ 142 tokens
→ 语法分析
  ✓ 2 routes, 3 rules
→ 语义检查
  ✓ 通过（0 warning, 0 error）
→ 中间表示
  ✓ 2 routes lowered
→ 代码生成
  ✓ out/src/main.go
  ✓ out/src/routes_gen.go
  ✓ out/src/handlers_gen.go
  ✓ out/src/bodies_gen.go
  ✓ out/src/templates_gen.go
→ go build
  ✓ mymock (8.4 MB)
完成 ✓
```

---

## 八、端到端示例

### Step 1：写 DSL

```yaml
# examples/crud.mocker
version: 1
server:
  port: 8080
  cors: true

routes:
  - method: GET
    path: /api/users/:id
    response:
      status: 200
      body:
        id: "{{ .Params.id | int }}"
        name: "{{ .Faker.Name }}"
        age: "{{ .Faker.Int 18 60 }}"
        email: "{{ .Faker.Email }}"

  - method: GET
    path: /api/users
    request:
      query:
        limit: int
    response:
      status: 200
      body: "{{ range $i, $v := mkSlice .Query.limit }}{{ $v }},{{ end }}"

  - method: POST
    path: /api/login
    request:
      body:
        username: string
        password: string
    response:
      status: 200
      body:
        token: "{{ .Hash.Sha256 .Body.username .Body.password }}"
        user:
          name: "{{ .Body.username }}"

    rules:
      - when: "{{ eq .Body.username \"locked\" }}"
        status: 423
        body: { error: "account locked" }
```

### Step 2：编译

```bash
mocker build examples/crud.mocker -o ./mymock
```

### Step 3：运行

```bash
./mymock
🚀 Mock server listening on :8080
```

### Step 4：前端调用

```bash
$ curl http://localhost:8080/api/users/42
{
  "id": 42,
  "name": "Alice Wang",
  "age": 27,
  "email": "alice42@example.com"
}

$ curl -X POST http://localhost:8080/api/login \
       -H 'Content-Type: application/json' \
       -d '{"username":"admin","password":"123"}'
{
  "token": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
  "user": { "name": "admin" }
}

$ curl -X POST http://localhost:8080/api/login \
       -H 'Content-Type: application/json' \
       -d '{"username":"locked","password":"123"}'
{ "error": "account locked" }    # ← 命中第一条 rule
```

---

## 九、关键设计决策

| 决策 | 方案 | 理由 |
| --- | --- | --- |
| 输入格式 | **YAML** | AI 友好 / 人友好 / 结构清晰 / 已有 YAML 库 |
| 输出 | **单个 Go 二进制** | 零依赖、拷走即跑 |
| 模板引擎 | **`text/template` 子集** | 标准库、用户已熟、AI 生成自然 |
| 路由库 | **chi/v5** | 轻量、支持 path param、活跃维护 |
| 模板解析 | **编译器只 parse 不执行** | 编译期发现语法错；运行时再渲染 |
| Faker 集成 | **注入到模板 FuncMap** | `{{ .Faker.Name }}` 语法统一，无需特殊代码 |
| 类型系统 | **极简**（string/int/float/bool/object） | DSL 不该复杂；类型主要用来校验 |
| 错误恢复 | **错误聚合**（不 fail-fast） | DSL 是给人 / AI 写的，一次看到所有问题更友好 |
| 延迟模拟 | **min/max 区间随机** | 默认开；可关 |
| CORS | **默认开** | 前端开发默认需求 |
| 状态码默认值 | 200 / 201 / 204 按 method 推断 | 减少样板 |
| 路径参数风格 | **`:id`**（与 Express 兼容） | 前端 / 后端都熟 |
| rule 触发方式 | **第一条匹配的 rule**（短路） | 简单、可预测 |
| 请求体类型 | **可定义**（不强制执行） | mock 服务校验成本太高；只做语法检查 |
| 响应可引用请求 | **模板里 `.Body` / `.Params`** | mock 的核心价值 |
| 产物可重现 | **`--seed` 标志** | 测试 / 截图需要稳定数据 |
| 二进制大小 | **8-12 MB**（含 runtime） | 可接受；远小于 Electron |
| 编译耗时 | **首次 2-5s（go build 决定）** | 可接受；考虑缓存二次构建 < 100ms |
| 版本字段 | **`version: 1`** | 留扩展空间 |

---

## 十、与同类的对比

| 工具 | 输入 | 输出 | 运行时 | 数据生成 | 条件分支 |
| --- | --- | --- | --- | --- | --- |
| json-server | JSON 文件 | Node 进程 | Node | 静态 | 无 |
| MSW | JS handler | 浏览器代码 | 浏览器 | 手写 | JS 控制流 |
| WireMock | JSON / XML 配置 | JVM 服务 | JVM | 模板 | 支持 |
| Prism | OpenAPI | Node 服务 | Node | 示例数据 | 弱 |
| **Mocker** | **YAML DSL** | **Go 二进制** | **自包含** | **Faker 内置** | **when 表达式** |

**差异化**：唯一同时具备 *AI 友好 DSL + 单二进制产物 + 丰富数据生成* 的方案。

---

## 十一、扩展性

| 扩展项 | 改动 | 难度 |
| --- | --- | --- |
| 加新字段（如 `delay` 表达式） | lexer + parser + ir + codegen | ★★ |
| 加新 Faker（IP / CreditCard） | runtime/faker | ★ |
| 切换路由库（gin / echo） | codegen + runtime | ★★ |
| 输出其他语言（Node / Rust 二进制） | 新 codegen 后端 | ★★★★ |
| 支持 WebSocket 路由 | lexer + parser + codegen + runtime | ★★★ |
| 支持 GraphQL | 新 route 类型 + runtime | ★★★★ |
| 支持 OpenAPI 输入（互转） | 新 parser 前端 | ★★★ |
| 热重载（运行时改 DSL 不重启） | 编译器监听文件 → 重 build | ★★★ |
| 在线预览（生成 swagger UI） | codegen 加前端 | ★★ |
| 数据持久化（重启数据不丢） | runtime 加 store | ★★ |

---

## 十二、里程碑（路线图）

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| **M0** | glex 词法层（独立仓库 lexical_analysis/） | ✅ 已完成 |
| **M1** | Mocker 编译器骨架 + lexer（手写 .ce 词法分析 `mocker_lex`） | ✅ 已完成 |
| **M2** | parser（tokens → AST，含 fan-out / 拓扑块 / 入口保留名 `main`） | ✅ 已完成 |
| **M3** | semantic（类型检查 + 引用解析 + 跨包 + 拓扑校验） | ❌ 未开始 |
| **M4** | ir + codegen（AST → Go 源码 → 单二进制） | ❌ 未开始 |
| **M5** | runtime 库（sysio / io / stdio — Go 写，自举友好） | ❌ 未开始 |
| **M6** | CLI（circle build / run / validate） | ❌ 未开始 |
| **M7** | 端到端：crud.mocker → 二进制 → curl 通过 | |
| **M8** | examples/basic / auth / crud 三个示例 | |
| **M9** | 文档（DSL 参考手册 + 模板 FuncMap 参考） | |
| **M10** | CI / release（GitHub Actions 发二进制） | |

> **当前已完成**：M0（glex 词法层）+ M1（编译器骨架 + 手写 `mocker_lex`）+ M2（parser + AST）。
> 后缀已从 `.mocker` 改为 `.ce`（自举目标）。
> 5 个 example .ce 文件 0 error pass。

> **下一步**：M3（semantic 语义分析）— 详见 [circle/docs/roadmap.md](../circle/docs/roadmap.md)。

---

## 十三、风险与权衡

| 风险 | 应对 |
| --- | --- |
| go build 编译慢 | 缓存 + `--keep-temp` 调试；接受首次 2-5s |
| 二进制大（8-12MB） | 可接受；strip 后可到 5MB |
| YAML 缩进解析复杂度 | 用 INDENT/DEDENT token 流；可参考 Python |
| 模板运行时错误（用户写错） | 渲染时 panic → 服务返回 500 + 详细日志；编译器只 parse 验证语法 |
| runtime 与第三方路由库冲突 | 锁定 chi 版本；考虑后续自实现极简 router |
| DSL 表达力不够（用户需要复杂逻辑） | 接受——mock 不该替代真实服务；可加 `script:` 块（高级功能） |
| AI 生成的 DSL 有幻觉 | 编译器严格校验 + 友好报错 |
| 路径参数冲突（`:id` vs 静态段） | 路由排序：静态优先于参数 |
| 启动慢（首次解析模板） | 启动时编译所有 template，缓存 `*template.Template` |
| go 工具链依赖 | 用户需装 Go；提供预编译二进制（github release） |

---

## 十四、与已有词法层的关系

`lexical_analysis/` 是 **Mocker 编译器的前置实验**：
- 它实现了 **glex**（一个通用的 Go 词法分析器代码生成器）。
- 它**不直接被 Mocker 使用**，而是验证了"声明式定义 → 自动生成代码"的整体思路。
- Mocker 编译器自己会有一个**更专用的 lexer**（识别 YAML 缩进 + `{{ }}` 模板字面量），可能复用 glex 的部分 NFA/DFA 思路，但**不直接 import glex 的产物**。

> **为什么分开？** 因为 .mocker 文件的语法（YAML + 模板）和 glex 的输入（token 定义）语法差异太大；强行复用会让 lexer 复杂。**思想复用**，**实现独立**。

---

## 十五、总结

**Mocker = 一份声明式 DSL → 一个 Go 二进制 → 一个 Mock HTTP 服务。**

核心架构是 **compiler**（5 个内部包：lexer / parser / semantic / ir / codegen）+ **runtime**（4 个模块：faker / hash / template / router）+ **CLI**（build / run / validate）。

后续工作重点：
1. **parser**：把 tokens 拼成 AST，重点是 YAML 缩进敏感解析。
2. **semantic**：让 DSL 错误在编译期就报出来，而不是运行时 500。
3. **codegen**：让生成的代码足够简单、足够快，单二进制就能跑。
4. **runtime**：Faker / Hash / 模板是 mock 服务的灵魂，要好用。

> **不要追求**：复杂 DSL、复杂类型系统、复杂脚本能力。
> **要追求**：AI 写起来顺手、人类读起来舒服、产物跑起来稳定。
