# Codex Prompt — L1 W5: Forward client cache_control marker

> **使用说明**(给 founder):整个文件 cat 出来粘贴给 codex CLI 即可。不需要任何额外解释 — codex 看完这个 prompt 就有完整执行 context。
>
> ```bash
> cat docs/codex-dispatch/W5-PROMPT-FOR-CODEX.md | codex exec
> ```
>
> 或在 codex CLI 里用 `--full-auto` 模式发完整 prompt。

---

# 你的任务

你是一个 codex agent。在 greentokey 项目里实施 **L1 W5: Forward client cache_control marker**。这是一个 4-6 小时的工程改动,大约 300 LoC + 15 测试用例,目标是让客户通过我们的 token 分销网关使用 Anthropic prompt cache 时,他们的 `cache_control` marker 能完整 forward 到上游 Anthropic API,使 cache 真正生效。

工作目录:`/Users/brendanxu/tanaxu/greentokey/coai-v0.7-design`
工作分支:**已经在** `feat/v0.16-admin-concierge-orders` 分支上,在这个分支上加 commits 即可,不要切分支。

## 0. 你是谁,greentokey 是什么

greentokey 是一个 BYOK + token 分销 + 民宿 SaaS 的多层 Go 后端,基于 CoAI fork(在 `adapter/`、`channel/`、`utils/`、`manager/` 等目录是 CoAI 上游代码的 fork,改动有 fork 分叉成本)。

3 层架构:
- **L1 (token 分销网关)**:客户拿 `sk-tnx-xxx` 直接打 OpenAI 兼容的 `/v1/chat/completions`,我们路由到上游 Anthropic / OpenAI / DeepSeek 等 provider。本任务在这一层。
- **L2 (套餐 + 支付)**:LemonSqueezy + 虎皮椒,套餐表 `gtk_plan` 等。
- **L3 (民宿 SaaS marketplace)**:`service/runtime.go` 内部 agent 调用,跟本任务无关。

本任务是 L1 层。**改 fork 上游文件 = 风险,改前要想清楚**。

## 1. 已完成的前置工作(不要重做)

`feat/v0.16-admin-concierge-orders` 上有 4 个最近 commit 你必须懂:

### W1 (`858b2ad`): Schema 落地

- `plans/migration.go` 加了 `gtk_app_usage_log` V2 字段(`input_tokens`, `output_tokens`, `cache_write_tokens`, `cache_read_tokens`, `cache_ttl`, `upstream_cost_micro`, `client_charge_micro`, `markup_multiplier`)
- 新建 `gtk_provider_pricing` 表(seed 16 行 Anthropic Sonnet 4.5/Haiku 3.5 + DeepSeek + OpenAI)
- 新建 `gtk_billing_config` 表(seed `markup_multiplier=1.300`)
- 7 单测全 PASS

### W2 (`f8fe3a9`): Adapter 解析 upstream Usage

- `globals/types.go` 加了 `UpstreamUsage` 类型(`InputTokens / OutputTokens / CacheWriteTokens / CacheReadTokens / CacheTTL`)
- `globals.Chunk` 加了 `UpstreamUsage *UpstreamUsage` 字段
- `adapter/claude/types.go` 加了 `Usage` struct(`input_tokens`, `output_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens`)
- `adapter/claude/chat.go::CreateStreamChatRequest` 在 closure 里累积 `message_start` (input + cache_creation/read) + `message_delta` (output) 的 partial usage,stream 结束时 emit 一个终结 `Chunk{UpstreamUsage: ...}`
- `adapter/openai/types.go` 加了 `Usage` + `PromptTokensDetails` + `NormaliseUsage` 函数
- `adapter/openai/chat.go::CreateStreamChatRequest` 在 closure 抓 `usage` block (when `stream_options.include_usage=true`),emit 终结 chunk
- `adapter/deepseek/struct.go` 加 `prompt_cache_hit_tokens` 到现有 Usage struct + `NormaliseUsage` 函数
- `adapter/deepseek/chat.go::CreateStreamChatRequest` 同样 emit 终结 chunk
- 14 单测全 PASS

### W3+W4 (`5d2074a`): Buffer + Charge 4 类计费

- `utils/buffer.go::Buffer` 加了 `Upstream *globals.UpstreamUsage` 字段 + `RecordUpstreamUsage(*globals.UpstreamUsage)` 方法
- `Buffer.WriteChunk` 现在会捕获 `data.UpstreamUsage` 调 `RecordUpstreamUsage`,空 content 终结 chunk 不再增加 Times
- `Buffer.GetQuota / GetRecordQuota` 当 `Upstream != nil` 时优先调 `CountUpstreamQuota`(4 类计费)
- `utils/buffer.go::Charge` interface 加了 3 个方法:`GetCacheRead() / GetCacheWrite5m() / GetCacheWrite1h() float32`
- `channel/types.go::Charge` struct 加了 `CacheRead`, `CacheWrite5m`, `CacheWrite1h` 字段(`mapstructure` tag,viper config 加载)
- `channel/charge.go` impl 3 个 getter,**unset 时 fall back**:CacheRead → GetInput, CacheWrite5m → GetInput × 1.25, CacheWrite1h → GetInput × 2.0(确保不赔钱)
- `utils/tokenizer.go` 新增 `CountUpstreamQuota(charge Charge, usage *globals.UpstreamUsage) float32` — 4 类独立计费
- 9 单测全 PASS,**包含 5 子场景的 `TestCountUpstreamQuota_NeverLoseMoney` 验证 customer charge ≥ upstream cost AND markup 守 1.30**

### Path 1 当前完整度

经过 W1-W4,这两个客户类型已经端到端通了:
- ✅ **OpenAI 兼容客户(GPT-4o 等)**:上游 auto-cache → adapter 解析 `cached_tokens` → 计费按 `GetCacheRead`
- ✅ **DeepSeek 客户**:上游 KV cache → adapter 解析 `prompt_cache_hit_tokens` → 计费按 `GetCacheRead`

唯一**没通**的:
- ⚠️ **Anthropic 原生客户(显式发 `cache_control` marker)**:客户 marker 在 `globals.Message.Content string` 这里被截断 → adapter 收不到 → 上游收不到 → 不 cache

**这是 W5 要解的事**。

## 2. W5 要解的具体问题

客户(比如有人用 SDK 写应用)发请求像这样:

```bash
curl https://api.greentokey.com/v1/chat/completions \
  -H "Authorization: Bearer sk-tnx-xxx" \
  -d '{
    "model": "claude-sonnet-4-5",
    "messages": [{
      "role": "system",
      "content": [
        {"type":"text","text":"<long system prompt ≥1024 tokens>","cache_control":{"type":"ephemeral","ttl":"5m"}}
      ]
    }, {
      "role":"user","content":"hi"
    }]
  }'
```

要求:
1. `cache_control` marker 在我们 adapter 反序列化时**保留**(目前 `globals.Message.Content` 是 `string`,会丢)
2. 在 adapter 重新 marshal 给上游 Anthropic 时,marker 在 system 字段 / content blocks 里**完整呈现**
3. 上游 Anthropic 真正缓存,response 含 `cache_creation_input_tokens > 0`(首次)/ `cache_read_input_tokens > 0`(后续)
4. 客户的 `cache_control.ttl`(`5m` 或 `1h`)被记录,流到 `Buffer.PreferredCacheTTL`,在 `RecordUpstreamUsage` 时塞到 `usage.CacheTTL`,W4 计费按 `GetCacheWrite5m()` 或 `GetCacheWrite1h()` 路由
5. **OpenAI 客户兼容**:发 `content: "hello"` 字符串还能正常工作(很多老 OpenAI 客户用这种 shape)
6. **OpenAI 客户也支持发数组**:`content: [{"type":"text","text":"...","cache_control":{...}}]` — OpenAI auto-cache 不需要 marker,但有些 OpenAI 兼容 proxy 支持转发,我们透传 marker 不害人

## 3. 必须先读的文件

按顺序读这些文件,**不要跳**:

```bash
# 看 globals 的核心 types(改动起点在这)
cat globals/types.go

# 看 adapter common 的 ChatProps(adapter 收到的入参)
find adapter -name "chat_props.go" -o -name "*.go" | xargs grep -l "ChatProps" | head -3
cat adapter/common/*.go 2>/dev/null | head -200

# 看 Claude adapter 现有 types(W2 已加 Usage,你要扩 ChatBody.System + Message.Content)
cat adapter/claude/types.go
cat adapter/claude/chat.go

# 看 OpenAI adapter 现有 types
cat adapter/openai/types.go | head -120
cat adapter/openai/processor.go

# 看 manager 怎么构造 ChatProps(发请求的入口)
cat manager/chat.go | head -200

# 看 controller 怎么解析 客户请求(JSON unmarshal 起点)
find controller -name "*.go" 2>/dev/null | head -3
grep -rn "ShouldBindJSON\|BindJSON" controller/ manager/ 2>/dev/null | head -10

# 看 utils.Buffer 的现有结构(你要加 PreferredCacheTTL)
cat utils/buffer.go | head -100
```

注意:`globals.Message.Content` 是 `string`,这是改动的核心起点。

## 4. 推荐实施步骤(按这个顺序做,每步独立 commit)

### Step 1 — globals.MessageContent 类型

新建一个能承载两种 shape 的类型:

```go
// globals/types.go

// MessageContent carries either a plain string (legacy OpenAI clients)
// or a list of typed blocks (Anthropic-style with cache_control).
// Custom Marshal/Unmarshal dispatches by JSON shape so both client
// shapes pass through transparently.
type MessageContent struct {
    Plain  string         `json:"-"`
    Blocks []ContentBlock `json:"-"`
}

type ContentBlock struct {
    Type         string        `json:"type"`
    Text         string        `json:"text,omitempty"`
    ImageURL     *ImageURL     `json:"image_url,omitempty"`
    CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type CacheControl struct {
    Type string `json:"type"`           // "ephemeral"
    TTL  string `json:"ttl,omitempty"`  // "5m" | "1h"
}

type ImageURL struct {
    URL    string  `json:"url"`
    Detail *string `json:"detail,omitempty"`
}

func (m MessageContent) MarshalJSON() ([]byte, error) {
    if len(m.Blocks) > 0 {
        return json.Marshal(m.Blocks)
    }
    return json.Marshal(m.Plain)
}

func (m *MessageContent) UnmarshalJSON(data []byte) error {
    // string shape
    var s string
    if err := json.Unmarshal(data, &s); err == nil {
        m.Plain = s
        return nil
    }
    // array shape
    var blocks []ContentBlock
    if err := json.Unmarshal(data, &blocks); err == nil {
        m.Blocks = blocks
        return nil
    }
    return fmt.Errorf("MessageContent: expected string or array, got %s", string(data))
}

// String returns a flat-text view for callers that don't care about blocks.
// Concatenates all text blocks; ignores image / cache_control metadata.
func (m MessageContent) String() string {
    if m.Plain != "" {
        return m.Plain
    }
    var b strings.Builder
    for _, blk := range m.Blocks {
        if blk.Type == "text" {
            b.WriteString(blk.Text)
        }
    }
    return b.String()
}

// HasCacheControl returns the most-restrictive ttl found in any block,
// or "" if no cache_control marker is present. Used by the adapter to
// pick 5m vs 1h pricing on the W4 billing path.
func (m MessageContent) HasCacheControl() (ttl string, ok bool) {
    for _, blk := range m.Blocks {
        if blk.CacheControl != nil {
            t := blk.CacheControl.TTL
            if t == "" { t = "5m" }   // Anthropic default
            return t, true
        }
    }
    return "", false
}
```

然后修改 `globals.Message`:

```go
type Message struct {
    Role             string         `json:"role"`
    Content          MessageContent `json:"content"`  // was string
    Name             *string        `json:"name,omitempty"`
    FunctionCall     *FunctionCall  `json:"function_call,omitempty"`
    ToolCallId       *string        `json:"tool_call_id,omitempty"`
    ToolCalls        *ToolCalls     `json:"tool_calls,omitempty"`
    ReasoningContent *string        `json:"reasoning_content,omitempty"`
}
```

**这一步会破坏 30+ 文件的编译**。修复方法 = 给所有 `msg.Content` 引用加 `.String()`(读)或 `MessageContent{Plain: x}`(写)。运行:

```bash
# 找出所有 Message.Content 用法
grep -rn "\.Content" --include="*.go" | grep -v "_test.go" | grep -v "interface{}" | head -50

# 用 sed 自动修(读路径用 .String())
# 但你需要手动 review 每个 case,因为有的是 wri tepath
```

写测试 `globals/message_test.go`:

```go
func TestMessageContent_StringRoundTrip(t *testing.T) {
    in := []byte(`{"role":"user","content":"hello"}`)
    var m Message
    if err := json.Unmarshal(in, &m); err != nil { t.Fatal(err) }
    if m.Content.String() != "hello" { t.Errorf("got %q", m.Content.String()) }
    out, _ := json.Marshal(m)
    if !bytes.Contains(out, []byte(`"content":"hello"`)) {
        t.Errorf("round-trip lost shape: %s", out)
    }
}

func TestMessageContent_ArrayRoundTrip(t *testing.T) {
    in := []byte(`{"role":"system","content":[{"type":"text","text":"ctx","cache_control":{"type":"ephemeral","ttl":"5m"}}]}`)
    var m Message
    if err := json.Unmarshal(in, &m); err != nil { t.Fatal(err) }
    if len(m.Content.Blocks) != 1 { t.Fatalf("blocks=%d", len(m.Content.Blocks)) }
    if m.Content.Blocks[0].CacheControl == nil { t.Fatal("cache_control lost") }
    if ttl, ok := m.Content.HasCacheControl(); !ok || ttl != "5m" {
        t.Errorf("ttl=%q ok=%v", ttl, ok)
    }
}
```

**Commit 这一步**:`feat(globals): MessageContent — typed dual-shape (string|array) with cache_control`

### Step 2 — Claude adapter forwarding

`adapter/claude/types.go`:

```go
// ChatBody.System 改成 interface{} 以支持两种 shape
type ChatBody struct {
    Messages    []Message    `json:"messages"`
    MaxTokens   int          `json:"max_tokens"`
    Model       string       `json:"model"`
    System      interface{}  `json:"system,omitempty"`  // string OR []SystemBlock
    Stream      bool         `json:"stream"`
    Temperature *float32     `json:"temperature,omitempty"`
    TopP        *float32     `json:"top_p,omitempty"`
    TopK        *int         `json:"top_k,omitempty"`
}

// SystemBlock is the typed form of system when caller wants cache_control
// on the system prompt.
type SystemBlock struct {
    Type         string                 `json:"type"`           // always "text"
    Text         string                 `json:"text"`
    CacheControl *globals.CacheControl  `json:"cache_control,omitempty"`
}

// Message struct already has Content interface{} — that's fine.
```

`adapter/claude/chat.go::GetSystemPrompt`:重写,如果 source globals.Message.Content.Blocks 里的 system 块有 cache_control,返回 `[]SystemBlock`,否则返回拼接后的纯字符串。

`adapter/claude/chat.go::GetMessages`:改写,把 `globals.MessageContent.Blocks` 完整转换成 `claude.MessageContent` 数组(已存在),保留 cache_control 字段。

测试 `adapter/claude/cache_control_test.go`:

```go
func TestGetChatBody_PassesCacheControl(t *testing.T) {
    props := &adaptercommon.ChatProps{
        Model: "claude-sonnet-4-5",
        Message: []globals.Message{
            {Role: "system", Content: globals.MessageContent{
                Blocks: []globals.ContentBlock{{
                    Type: "text", Text: "long ctx",
                    CacheControl: &globals.CacheControl{Type: "ephemeral", TTL: "5m"},
                }},
            }},
            {Role: "user", Content: globals.MessageContent{Plain: "hi"}},
        },
    }
    c := &ChatInstance{}
    body := c.GetChatBody(props, true)
    out, _ := json.Marshal(body)
    if !bytes.Contains(out, []byte(`"cache_control"`)) {
        t.Errorf("marker stripped: %s", out)
    }
    if !bytes.Contains(out, []byte(`"ephemeral"`)) {
        t.Errorf("ephemeral type missing: %s", out)
    }
}
```

**Commit 这一步**:`feat(adapter/claude): forward cache_control marker in system + content blocks`

### Step 3 — OpenAI adapter forwarding

OpenAI 的 `Message.Content` 已经是 `MessageContents` typed array,只需:
- `adapter/openai/types.go::MessageContent` 加 `CacheControl *globals.CacheControl` 字段
- `adapter/openai/processor.go::formatMessages` 在转换 `globals.Message → openai.Message` 时,把每个 `globals.ContentBlock.CacheControl` 复制到 `openai.MessageContent.CacheControl`

OpenAI 上游会忽略这个未知字段,但有些 OpenAI 兼容 proxy(NewAPI 自己也是)会 forward,无副作用。

测试类似 Step 2。

**Commit**:`feat(adapter/openai): forward cache_control marker on content blocks`

### Step 4 — Buffer.PreferredCacheTTL 捕获

`utils/buffer.go::Buffer`:

```go
type Buffer struct {
    // ... existing fields ...
    PreferredCacheTTL string `json:"-"`  // "" | "5m" | "1h"
}

func NewBuffer(model string, history []globals.Message, charge Charge) *Buffer {
    token := initInputToken(model, history)

    // Detect cache_control TTL across all messages so RecordUpstreamUsage
    // can stamp Buffer.Upstream.CacheTTL correctly. First non-empty wins.
    var ttl string
    for _, m := range history {
        if t, ok := m.Content.HasCacheControl(); ok {
            ttl = t
            break
        }
    }

    return &Buffer{
        Model:             model,
        Quota:             CountInputQuota(charge, token),
        InputTokens:       token,
        Charge:            charge,
        FunctionCall:      nil,
        ToolCalls:         nil,
        ToolCallsCursor:   0,
        StartTime:         ToPtr(time.Now()),
        PreferredCacheTTL: ttl,
    }
}
```

修改 `RecordUpstreamUsage`:

```go
func (b *Buffer) RecordUpstreamUsage(u *globals.UpstreamUsage) {
    if u == nil {
        return
    }
    // Stamp the customer-declared TTL onto the upstream block so W4
    // billing routes cache_write to the right rate (5m vs 1h).
    if u.CacheTTL == "" && b.PreferredCacheTTL != "" {
        u.CacheTTL = b.PreferredCacheTTL
    }
    b.Upstream = u
    if u.InputTokens > 0 {
        b.InputTokens = u.InputTokens
    }
}
```

测试 `utils/preferred_ttl_test.go`:

```go
func TestRecordUpstreamUsage_StampsTTL(t *testing.T) {
    b := &Buffer{PreferredCacheTTL: "1h"}
    b.RecordUpstreamUsage(&globals.UpstreamUsage{InputTokens: 100, CacheWriteTokens: 50})
    if b.Upstream.CacheTTL != "1h" {
        t.Errorf("ttl=%q want 1h", b.Upstream.CacheTTL)
    }
}

func TestRecordUpstreamUsage_DoesntOverwrite(t *testing.T) {
    b := &Buffer{PreferredCacheTTL: "5m"}
    b.RecordUpstreamUsage(&globals.UpstreamUsage{CacheTTL: "1h", InputTokens: 100})
    if b.Upstream.CacheTTL != "1h" {
        t.Errorf("buffer overrode adapter-supplied ttl: got %q", b.Upstream.CacheTTL)
    }
}
```

**Commit**:`feat(utils/buffer): PreferredCacheTTL — bridge client marker to billing`

### Step 5 — 验证全链路

跑全套测试,确认没破:

```bash
go test ./... -count=1 -vet=off 2>&1 | grep -E "FAIL|ok " | tail -30
go build ./... 2>&1 | grep -v "warning\|libwebp" | head -10
```

如果发现 `globals.Message.Content` 用法没全改完(编译错),逐个 fix。

如果有 e2e infra(Playwright + 真实 sandbox 账号),跑一次双调用 cache 命中测试。否则在 PR 描述里写好手动测试 curl。

## 5. Commit message 模板

每个 step commit 用 conventional commits 格式 + 具体的"为什么这样改":

```
feat(<scope>): <one-line summary> — <important detail>

<paragraph: what changed and why>

<paragraph: tests added>

<paragraph: cross-references>

Refs: docs/codex-dispatch/W5-cache-control-forward.md
Co-Authored-By: codex@anthropic <noreply@anthropic.com>
```

不要 squash 4 个 step,founder 要看 incremental diff 做 review。

## 6. 不要做的事(out of scope)

- ❌ **不要改 reasoning content shape**:`globals.Message.ReasoningContent *string` 保持 string,这是 DeepSeek reasoning 模型用的,跟 cache 无关
- ❌ **不要做 image / multi-modal 重构**:现有 vision 路径(`utils.ExtractImages` 等)继续用 `Content.String()` 即可
- ❌ **不要改 NewAPI 网关 v0.13.x 任何代码**:它已经透传 cache_control(verified W2 阶段)
- ❌ **不要写 gtk_app_usage_log writer**:这表当前 0 写入路径,本任务不接通它
- ❌ **不要碰 service/runtime.go(L3 民宿 marketplace)**:跟本任务无关
- ❌ **不要改 charge config yaml**:运营自己加 `cache_read` / `cache_write_5m` / `cache_write_1h` 字段,不在 codex 范围

## 7. 风险 + 怎么应对

| 风险 | 缓解 |
|---|---|
| Refactoring `globals.Message.Content` 破 30+ 文件 | 先加 `String()` accessor,grep `\.Content` 找所有引用,逐个改 read 路径用 `.String()`,write 路径用 `MessageContent{Plain: x}`。每改 5 个文件跑一遍 `go build`. |
| OpenAI 客户发 plain string 在新类型下解析失败 | `MessageContent.UnmarshalJSON` 双 shape dispatch,测试覆盖了 `{"content":"hello"}` 和 `{"content":[...]}`两种 |
| 老 reasoning content 路径 mutate `m.Content` 当字符串用 | 改成 mutate `m.Content.Plain`(string 字段保持可写) |
| Anthropic-native `/v1/messages` 路径(如果 CoAI 暴露)不经过 globals.Message | grep `/v1/messages` controller,如果走另一条路径,本任务可能不需要修;founder 现状只有 `/v1/chat/completions` 入口 |
| `ChatBody.System interface{}` JSON marshal 时类型推断错 | 显式 type-switch 在 GetSystemPrompt:返回 `string` 或 `[]SystemBlock`,直接赋值 `interface{}` 字段。Marshal 自动正确 |

## 8. 验收命令(发 PR 前自己跑)

```bash
# 1. 全部包编译 + 测试通过
cd /Users/brendanxu/tanaxu/greentokey/coai-v0.7-design
go build ./...                              # 0 errors (warnings ok)
go test ./... -count=1 -vet=off | grep -E "FAIL|ok " | tail -30  # 全部 ok

# 2. 必要的 grep 证据(显示新代码确实被加进去了)
grep -rn "cache_control" adapter/claude/ adapter/openai/ globals/ | wc -l   # ≥ 5
grep -n "PreferredCacheTTL" utils/buffer.go                                 # ≥ 1
grep -n "MessageContent" globals/types.go                                   # ≥ 3

# 3. 测试覆盖度(按 commits 数清点)
git log --oneline | head -10                # ≥ 4 个 W5 相关 commits
git log --oneline --grep="W5\|cache_control\|MessageContent\|PreferredCache" | head

# 4. 关键文件改动
git diff --stat 858b2ad..HEAD globals/ adapter/claude/ adapter/openai/ utils/  # 见到改动
```

## 9. 完成后写一份 W5-DONE.md 报告

写到 `docs/codex-dispatch/W5-DONE.md`,内容:
- 4 个 commit hash + 一句话描述
- 测试统计(新增 / 全套 PASS 数)
- 任何踩过的坑(回报给 founder + tana)
- 下一步建议(比如运营 charge config 怎么加 cache_read 字段)
- 已知 limitation(比如 Anthropic-native /v1/messages 是否覆盖)

founder 看到这份报告就知道 W5 是否真的完整,可以直接 deploy。

## 10. 关于 fork 上游

`globals/types.go` + `adapter/` 都是 CoAI fork。本次改动会跟上游 diverge,以后 rebase CoAI 头疼是预期的。**不要试图 PR 回上游**(这是 greentokey 的差异化卖点)。在 PR 描述里写清楚 fork 风险即可。

---

**最后**:遇到 ambiguity,选**最不破坏现有 OpenAI 兼容客户**的做法。客户体验 > 代码优雅。

完成后报回 founder + tana 这边整合 review。

— end of prompt —
