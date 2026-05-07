import { useTranslation } from "react-i18next";
import DocsLayout, { DocsTocItem } from "@/components/Marketing/DocsLayout.tsx";

/**
 * /docs — single comprehensive technical reference for greentokey BYOK
 * users (indie devs, vibe coders) using the Token 套餐 (Layer 2) gateway.
 *
 * Single-page design rationale: see DocsLayout.tsx header comment.
 *
 * Locale switch: full zh + en parity. Code samples are language-neutral
 * (curl / Python / TypeScript) and shared across both — only prose
 * translates.
 *
 * NOT a generated reference. Hand-written so we can call out the things
 * a CoAI/NewAPI-flavored gateway does that vanilla OpenAI clients don't
 * expect (e.g. credits-based pricing, X-Quota-Used response header).
 */
function Docs() {
  const { i18n } = useTranslation();
  const isEn = (i18n.language || "").toLowerCase().startsWith("en");
  return isEn ? <DocsEN /> : <DocsCN />;
}

const TOC_CN: DocsTocItem[] = [
  { id: "quickstart", label: "快速开始" },
  { id: "auth", label: "鉴权" },
  { id: "endpoints", label: "API 端点", level: 3 },
  { id: "models", label: "模型与定价" },
  { id: "credits", label: "Credits 计费" },
  { id: "sdk", label: "SDK 与示例" },
  { id: "errors", label: "错误码" },
  { id: "rate-limits", label: "速率限制" },
  { id: "faq", label: "常见问题" },
];

const TOC_EN: DocsTocItem[] = [
  { id: "quickstart", label: "Quickstart" },
  { id: "auth", label: "Authentication" },
  { id: "endpoints", label: "API endpoints", level: 3 },
  { id: "models", label: "Models & pricing" },
  { id: "credits", label: "Credits billing" },
  { id: "sdk", label: "SDK & examples" },
  { id: "errors", label: "Error codes" },
  { id: "rate-limits", label: "Rate limits" },
  { id: "faq", label: "FAQ" },
];

const CURL_EXAMPLE = `curl https://api.greentokey.com/v1/chat/completions \\
  -H "Authorization: Bearer sk-xxxxxxxxxxxxxxxx" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`;

const PYTHON_EXAMPLE = `from openai import OpenAI

client = OpenAI(
    api_key="sk-xxxxxxxxxxxxxxxx",
    base_url="https://api.greentokey.com/v1",
)

resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(resp.choices[0].message.content)`;

const TS_EXAMPLE = `import OpenAI from "openai";

const client = new OpenAI({
  apiKey: "sk-xxxxxxxxxxxxxxxx",
  baseURL: "https://api.greentokey.com/v1",
});

const resp = await client.chat.completions.create({
  model: "gpt-4o-mini",
  messages: [{ role: "user", content: "Hello!" }],
});
console.log(resp.choices[0].message.content);`;

const STREAM_EXAMPLE = `curl https://api.greentokey.com/v1/chat/completions \\
  -H "Authorization: Bearer sk-xxxxxxxxxxxxxxxx" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "deepseek-chat",
    "stream": true,
    "messages": [{"role": "user", "content": "讲个笑话"}]
  }'`;

function DocsCN() {
  return (
    <DocsLayout
      eyebrow="开发者文档"
      title="API 文档"
      updated="2026-05-03"
      toc={TOC_CN}
    >
      <p>
        greentokey 的 Token 套餐（Layer 2）提供 OpenAI 兼容的 API 入口。
        买一份套餐，拿一把 <code>sk-xxx</code> Key，就能直接调用 GPT、
        Claude、DeepSeek、Gemini 等多家模型。前端代码不用改，把{" "}
        <code>base_url</code> 指过来即可。
      </p>

      <div className="callout">
        <p>
          <strong>BYOK ≠ 中转(当前技术状态):</strong>我们当前在应用层
          不存储 prompt / response 内容,仅记录元数据(模型名、token 数、
          时间戳)用于计费。这描述的是当前实现状态,不是永久承诺 —
          配置可在仓库 <code>infra/coai-config.yaml.example</code> +
          NewAPI dashboard 验证;任何变更会同步更新{" "}
          <a href="/privacy">隐私政策</a>。
        </p>
      </div>

      <h2 id="quickstart">快速开始</h2>

      <p>3 步上手：</p>

      <ol>
        <li>
          注册并购买任一 Token 套餐（详见{" "}
          <a href="/token-plans">Token 套餐</a>）
        </li>
        <li>
          在 <a href="/account">账户页</a> 复制你的 <code>sk-xxx</code> API Key
        </li>
        <li>
          把任意 OpenAI 兼容客户端的 <code>base_url</code> 改成{" "}
          <code>https://api.greentokey.com/v1</code>
        </li>
      </ol>

      <h3>第一次调用 (curl)</h3>
      <pre>
        <code>{CURL_EXAMPLE}</code>
      </pre>

      <h2 id="auth">鉴权</h2>

      <p>
        所有 API 请求都需要在 <code>Authorization</code> 头里带上你的
        sk-key：
      </p>
      <pre>
        <code>Authorization: Bearer sk-xxxxxxxxxxxxxxxx</code>
      </pre>

      <p>
        Key 的特性：
      </p>
      <ul>
        <li>每个套餐订单生成一把独立 Key（方便分项目隔离 + 审计）</li>
        <li>Key 不会过期；套餐到期后调用会返回 <code>402 quota_exhausted</code></li>
        <li>
          Key 泄漏后立即去 <a href="/account">账户页</a> 撤销并重新生成
        </li>
      </ul>

      <h3 id="endpoints">主要端点</h3>
      <table>
        <thead>
          <tr>
            <th>端点</th>
            <th>说明</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><code>POST /v1/chat/completions</code></td>
            <td>对话补全（支持 stream）</td>
          </tr>
          <tr>
            <td><code>POST /v1/embeddings</code></td>
            <td>向量化</td>
          </tr>
          <tr>
            <td><code>POST /v1/images/generations</code></td>
            <td>文生图</td>
          </tr>
          <tr>
            <td><code>POST /v1/audio/transcriptions</code></td>
            <td>语音转文字</td>
          </tr>
          <tr>
            <td><code>GET /v1/models</code></td>
            <td>列出当前套餐可用的模型</td>
          </tr>
        </tbody>
      </table>

      <h2 id="models">模型与定价</h2>

      <p>
        我们把模型按算力成本分成 3 档，每个套餐里 1 credit 在不同档位
        换算成不同的实际配额：
      </p>

      <table>
        <thead>
          <tr>
            <th>档位</th>
            <th>系数</th>
            <th>典型模型</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td><strong>轻量 light</strong></td>
            <td>0.5×</td>
            <td>gpt-4o-mini, deepseek-chat, gemini-flash, qwen-turbo</td>
          </tr>
          <tr>
            <td><strong>标准 standard</strong></td>
            <td>1.0×</td>
            <td>gpt-4o, claude-3-5-sonnet, gemini-pro, qwen-plus</td>
          </tr>
          <tr>
            <td><strong>高级 premium</strong></td>
            <td>3.0×</td>
            <td>o1-preview, claude-3-opus, gemini-1.5-pro-large</td>
          </tr>
        </tbody>
      </table>

      <p>
        可用模型完整列表会随着上游开放动态变化。运行时调用{" "}
        <code>GET /v1/models</code> 拿到你的套餐当前能用的全部模型 + 当前
        系数。
      </p>

      <h2 id="credits">Credits 计费</h2>

      <p>
        每个套餐预付一定数量的 credits。1 credit ≈ 1500 quota（NewAPI
        内部计量单位），quota 与上游 token 数大致 1:1。
      </p>

      <p>调用消耗的 credits 公式：</p>
      <pre>
        <code>credits 消耗 = (input_tokens + output_tokens × 4) × 系数 / 1500</code>
      </pre>

      <p>
        每次调用响应头里有：
      </p>
      <ul>
        <li>
          <code>X-Quota-Used</code> — 本次消耗的 quota
        </li>
        <li>
          <code>X-Quota-Remaining</code> — 套餐剩余 quota
        </li>
        <li>
          <code>X-Quota-Tier</code> — 实际命中的档位
        </li>
      </ul>

      <p>
        套餐快用完时（&lt; 10%），账户页会有提醒；用完后会返回 HTTP 402
        而不是悄悄请求上游 API（避免计费意外）。
      </p>

      <h2 id="sdk">SDK 与示例</h2>

      <p>
        OpenAI 官方 SDK 直接能用 — 改一个 <code>base_url</code> 就行。
      </p>

      <h3>Python</h3>
      <pre>
        <code>{PYTHON_EXAMPLE}</code>
      </pre>

      <h3>TypeScript / Node.js</h3>
      <pre>
        <code>{TS_EXAMPLE}</code>
      </pre>

      <h3>流式 (SSE)</h3>
      <pre>
        <code>{STREAM_EXAMPLE}</code>
      </pre>

      <p>
        其它工具（Cursor / Cline / Roo Code / OpenWebUI / LiteLLM 等）只
        需要在配置里把 <code>OpenAI Base URL</code> 改成{" "}
        <code>https://api.greentokey.com/v1</code> 即可。
      </p>

      <h2 id="errors">错误码</h2>
      <table>
        <thead>
          <tr>
            <th>HTTP</th>
            <th><code>error.code</code></th>
            <th>含义</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>401</td>
            <td><code>invalid_api_key</code></td>
            <td>Key 无效或被撤销</td>
          </tr>
          <tr>
            <td>402</td>
            <td><code>quota_exhausted</code></td>
            <td>套餐配额用完</td>
          </tr>
          <tr>
            <td>403</td>
            <td><code>model_not_in_plan</code></td>
            <td>请求的模型不在你的套餐档位内</td>
          </tr>
          <tr>
            <td>429</td>
            <td><code>rate_limit_exceeded</code></td>
            <td>触发速率限制（见下）</td>
          </tr>
          <tr>
            <td>502 / 503</td>
            <td><code>upstream_error</code></td>
            <td>上游 API 临时不可用，按 retry-after 重试</td>
          </tr>
        </tbody>
      </table>

      <h2 id="rate-limits">速率限制</h2>
      <p>
        默认每个 sk-key 限制：
      </p>
      <ul>
        <li>RPM（每分钟请求数）：60</li>
        <li>TPM（每分钟 token 数）：60,000</li>
        <li>并发：10</li>
      </ul>
      <p>
        高并发场景请联系 <a href="/contact">support</a> 申请提额。
      </p>

      <h2 id="faq">常见问题</h2>

      <h3>会被国内 GFW 影响吗？</h3>
      <p>
        api.greentokey.com 走 Cloudflare CDN，国内大多数运营商可直连。
        如果你的网络环境特殊（如某些校园网），可以使用我们的内地直连节点
        <code>cn.api.greentokey.com</code>（暂未对外，按需开通）。
      </p>

      <h3>支持哪些上游模型？</h3>
      <p>
        OpenAI / Anthropic / Google / DeepSeek / Qwen / 智谱 / Moonshot /
        百川 等。完整列表见运行时{" "}
        <code>GET /v1/models</code>，或参考{" "}
        <a href="/pool">模型池</a> 页面。
      </p>

      <h3>能开发票吗？</h3>
      <p>
        可以。月度结账后联系{" "}
        <a href="/contact">support</a> 索取增值税普通发票（电子）。
        企业台头需提前提供税号。
      </p>

      <h3>响应时间有承诺吗？</h3>
      <p>
        我们对网关层（greentokey 自己控制的部分）承诺 99.5% 可用率 + p95
        额外开销 &lt;100ms。上游 API 的延迟和可用性以原厂 SLA 为准。
      </p>
    </DocsLayout>
  );
}

function DocsEN() {
  return (
    <DocsLayout
      eyebrow="Developer docs"
      title="API documentation"
      updated="2026-05-03"
      toc={TOC_EN}
    >
      <p>
        The greentokey Token Plan (Layer 2) is an OpenAI-compatible API
        gateway. Buy a plan, get an <code>sk-xxx</code> key, and call GPT,
        Claude, DeepSeek, Gemini and others through one endpoint. No code
        changes — just point your client&apos;s <code>base_url</code> at us.
      </p>

      <div className="callout">
        <p>
          <strong>BYOK ≠ proxying (current technical state):</strong> under
          the current configuration we do not store prompts or completions
          at the application layer; we log only metadata (model, token count,
          timestamp) for billing. This describes current implementation, not
          a perpetual commitment — configuration is verifiable in the
          repository (<code>infra/coai-config.yaml.example</code>) and the
          NewAPI dashboard, and any change will be reflected in the{" "}
          <a href="/privacy">privacy policy</a>.
        </p>
      </div>

      <h2 id="quickstart">Quickstart</h2>
      <p>Three steps:</p>
      <ol>
        <li>
          Sign up and buy any plan on{" "}
          <a href="/token-plans">Token Plans</a>
        </li>
        <li>
          Copy your <code>sk-xxx</code> API key from the{" "}
          <a href="/account">account page</a>
        </li>
        <li>
          Set your client&apos;s <code>base_url</code> to{" "}
          <code>https://api.greentokey.com/v1</code>
        </li>
      </ol>

      <h3>First call (curl)</h3>
      <pre>
        <code>{CURL_EXAMPLE}</code>
      </pre>

      <h2 id="auth">Authentication</h2>
      <p>
        Every request needs your sk-key in the <code>Authorization</code>{" "}
        header:
      </p>
      <pre>
        <code>Authorization: Bearer sk-xxxxxxxxxxxxxxxx</code>
      </pre>
      <ul>
        <li>Each plan order generates a separate key (good for project isolation + audit)</li>
        <li>Keys don&apos;t expire; calls return <code>402 quota_exhausted</code> when the plan runs out</li>
        <li>If a key leaks, revoke and regenerate from the <a href="/account">account page</a></li>
      </ul>

      <h3 id="endpoints">Main endpoints</h3>
      <table>
        <thead><tr><th>Endpoint</th><th>Purpose</th></tr></thead>
        <tbody>
          <tr><td><code>POST /v1/chat/completions</code></td><td>Chat completion (streaming supported)</td></tr>
          <tr><td><code>POST /v1/embeddings</code></td><td>Embeddings</td></tr>
          <tr><td><code>POST /v1/images/generations</code></td><td>Image generation</td></tr>
          <tr><td><code>POST /v1/audio/transcriptions</code></td><td>Speech to text</td></tr>
          <tr><td><code>GET /v1/models</code></td><td>List models available on your plan</td></tr>
        </tbody>
      </table>

      <h2 id="models">Models &amp; pricing</h2>
      <p>
        Models are grouped into 3 tiers by upstream cost. 1 credit
        translates to different effective quota across tiers:
      </p>
      <table>
        <thead><tr><th>Tier</th><th>Multiplier</th><th>Examples</th></tr></thead>
        <tbody>
          <tr><td><strong>light</strong></td><td>0.5×</td><td>gpt-4o-mini, deepseek-chat, gemini-flash, qwen-turbo</td></tr>
          <tr><td><strong>standard</strong></td><td>1.0×</td><td>gpt-4o, claude-3-5-sonnet, gemini-pro, qwen-plus</td></tr>
          <tr><td><strong>premium</strong></td><td>3.0×</td><td>o1-preview, claude-3-opus, gemini-1.5-pro-large</td></tr>
        </tbody>
      </table>
      <p>
        The full live list shifts with upstream availability. Call{" "}
        <code>GET /v1/models</code> at runtime for the current set on your plan.
      </p>

      <h2 id="credits">Credits billing</h2>
      <p>
        Plans are prepaid in credits. 1 credit ≈ 1500 quota units (NewAPI
        internal); quota maps roughly 1:1 to upstream tokens.
      </p>
      <p>Per-call credit cost:</p>
      <pre><code>credits = (input_tokens + output_tokens × 4) × multiplier / 1500</code></pre>
      <p>Response headers on every call:</p>
      <ul>
        <li><code>X-Quota-Used</code> — quota spent on this call</li>
        <li><code>X-Quota-Remaining</code> — quota left on the plan</li>
        <li><code>X-Quota-Tier</code> — tier the model resolved to</li>
      </ul>
      <p>
        At &lt; 10% remaining we surface a banner on the account page.
        At 0 we return 402 — we never silently double-bill upstream.
      </p>

      <h2 id="sdk">SDK &amp; examples</h2>
      <p>The OpenAI SDKs work — just change one URL.</p>
      <h3>Python</h3>
      <pre><code>{PYTHON_EXAMPLE}</code></pre>
      <h3>TypeScript / Node.js</h3>
      <pre><code>{TS_EXAMPLE}</code></pre>
      <h3>Streaming (SSE)</h3>
      <pre><code>{STREAM_EXAMPLE}</code></pre>
      <p>
        Tools like Cursor / Cline / Roo Code / OpenWebUI / LiteLLM all work
        — set their OpenAI Base URL to{" "}
        <code>https://api.greentokey.com/v1</code>.
      </p>

      <h2 id="errors">Error codes</h2>
      <table>
        <thead><tr><th>HTTP</th><th><code>error.code</code></th><th>Meaning</th></tr></thead>
        <tbody>
          <tr><td>401</td><td><code>invalid_api_key</code></td><td>Key invalid or revoked</td></tr>
          <tr><td>402</td><td><code>quota_exhausted</code></td><td>Plan out of credits</td></tr>
          <tr><td>403</td><td><code>model_not_in_plan</code></td><td>Model not in your tier</td></tr>
          <tr><td>429</td><td><code>rate_limit_exceeded</code></td><td>Throttled (see below)</td></tr>
          <tr><td>502 / 503</td><td><code>upstream_error</code></td><td>Upstream temporarily down — retry per retry-after</td></tr>
        </tbody>
      </table>

      <h2 id="rate-limits">Rate limits</h2>
      <p>Per-key default:</p>
      <ul>
        <li>RPM: 60</li>
        <li>TPM: 60,000</li>
        <li>Concurrency: 10</li>
      </ul>
      <p>Need more? Email <a href="/contact">support</a>.</p>

      <h2 id="faq">FAQ</h2>
      <h3>Will Chinese network conditions affect me?</h3>
      <p>
        api.greentokey.com is fronted by Cloudflare and reaches most
        Chinese ISPs reliably. Special-case access (campus networks, etc.)
        can use <code>cn.api.greentokey.com</code> on request.
      </p>
      <h3>Which upstream models are supported?</h3>
      <p>
        OpenAI / Anthropic / Google / DeepSeek / Qwen / Zhipu / Moonshot /
        Baichuan and more. Live list via <code>GET /v1/models</code> or
        the <a href="/pool">model pool</a> page.
      </p>
      <h3>Can I get an invoice?</h3>
      <p>
        Yes. Email <a href="/contact">support</a> after checkout for an
        electronic VAT invoice (Chinese 增值税普通发票). Provide the tax
        ID up front for company titles.
      </p>
      <h3>Latency &amp; uptime?</h3>
      <p>
        We commit 99.5% uptime for the gateway layer with &lt; 100ms p95
        overhead. Upstream latency follows each provider&apos;s SLA.
      </p>
    </DocsLayout>
  );
}

export default Docs;
