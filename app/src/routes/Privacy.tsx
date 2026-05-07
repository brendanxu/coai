import { useTranslation } from "react-i18next";
import LegalLayout from "@/components/Marketing/LegalLayout.tsx";

/**
 * 隐私政策 / Privacy Policy.
 *
 * Why this exists: Chinese commercial-domain ICP filing requires a
 * publicly-linked privacy policy. Also a real legal need — we collect
 * contact info (gtk_lead), payment metadata (gtk_ls_subscription),
 * and API call logs (NewAPI quota tracking).
 *
 * 中文版 + EN version maintained in parallel — i18n switch picks one.
 *
 * NOT legal advice. Founder should have a lawyer review before serving
 * to enterprise customers (post-PMF). For 民宿 wedge + indie devs at
 * v0.12 scale this is sufficient cover for ICP + Cloudflare CDN.
 */
function Privacy() {
  const { i18n } = useTranslation();
  const isEn = (i18n.language || "").toLowerCase().startsWith("en");

  if (isEn) return <PrivacyEN />;
  return <PrivacyCN />;
}

function PrivacyCN() {
  return (
    <LegalLayout
      eyebrow="隐私政策"
      title="我们如何处理你的数据"
      updated="2026-05-03"
    >
      <p>
        greentokey（"我们"）尊重每一位用户（"你"）的隐私。本政策说明我们收集什么信息、为什么收集、怎么用、什么时候删，以及你能行使的权利。
      </p>
      <p>
        本政策适用于 <strong>greentokey.com</strong>、
        <strong>www.greentokey.com</strong>、<strong>api.greentokey.com</strong>，
        及其上承载的 Token 套餐 API 服务、AI 服务市场、demo 预约表单。
      </p>

      <h2>1. 我们收集什么</h2>

      <h3>1.1 你主动提供的</h3>
      <ul>
        <li>
          <strong>预约 demo 时</strong>：微信号 / 手机号 / 民宿名 / 位置 /
          备注（你只填你愿意填的）
        </li>
        <li>
          <strong>购买服务时</strong>：支付平台（支付宝 / 微信支付 / LemonSqueezy）
          回传的订单号 + 金额 + 渠道。
          <strong>我们不接触你的银行卡或支付宝密码。</strong>
        </li>
        <li>
          <strong>购买 Token 套餐后</strong>：我们生成你的 sk-xxx API Key，
          关联到你的账号
        </li>
        <li>
          <strong>使用服务时</strong>：你上传的图片 / 文字 / 主题词
          （仅用于生成你购买的内容；
          不外传，不用于训练模型）
        </li>
      </ul>

      <h3>1.2 自动收集的</h3>
      <ul>
        <li>
          <strong>API 调用日志</strong>：模型名、token 数、时间戳、调用源 IP。
          用于计费 + 限流 + 故障排查。不记录 prompt / response 文本。
        </li>
        <li>
          <strong>访问日志</strong>：IP + UA + 访问 URL + 时间戳。
          Cloudflare CDN + Caddy 边缘各保留 7-30 天。
        </li>
      </ul>

      <h3>1.3 我们<strong>不</strong>收集</h3>
      <ul>
        <li>你的小红书账号密码（民宿代运营服务从不要求密码，内容生成后推到你手机草稿箱）</li>
        <li>你的支付凭证（卡号 / 密码 / CVV）</li>
        <li>位置 GPS（你填的"位置"是文本字段，非定位数据）</li>
        <li>第三方 cookie 跟踪（仅 Cloudflare 必需的反 DDoS cookie）</li>
      </ul>

      <h2>2. 我们怎么用</h2>
      <ol>
        <li>
          <strong>履行服务</strong>：响应你的 API 调用、生成你购买的内容、Founder 联系你跟进 demo。
        </li>
        <li>
          <strong>计费</strong>：API token 数 → credits → 月度结算。
          支付平台代收款项。
        </li>
        <li>
          <strong>故障排查 + 安全</strong>：异常调用拦截、IP 限流、滥用调查。
        </li>
        <li>
          <strong>产品改进</strong>：聚合统计（"DeepSeek 占 60% 调用量"），不识别个人。
        </li>
      </ol>

      <h2>3. 我们和谁共享</h2>

      <p><strong>必要的第三方</strong>（你的数据通过它们流转的环节）：</p>
      <ul>
        <li>
          <strong>支付宝</strong> / <strong>LemonSqueezy</strong> / <strong>虎皮椒</strong>{" "}
          — 收单 + 退款。共享：订单号、金额。
        </li>
        <li>
          <strong>NewAPI 上游模型供应商</strong>（OpenAI / Anthropic / DeepSeek /
          阿里云 / 智谱 / 月之暗面 等）— 你的 prompt 内容会发送给上游模型 API
          以生成 response。<strong>当前技术配置下</strong>,我们不在应用层
          存储 prompt 文本(NewAPI prompt log 设置见仓库 <code>infra/coai-config.yaml.example</code>)。
          这是当前实现状态而非永久性合同承诺 — 任何配置变更会同步更新本页。
          上游各家 API 的隐私政策由它们自行公布。
        </li>
        <li>
          <strong>Cloudflare</strong> — CDN + DDoS 防护。共享：访问日志（IP / UA / URL）。
        </li>
      </ul>

      <p>
        <strong>我们不卖你的数据。</strong> 不向广告网络、数据经纪商、营销列表
        提供任何客户信息。
      </p>

      <h2>4. 数据保留期</h2>
      <ul>
        <li>
          <strong>预约 demo 留资</strong>：90 天内 founder 跟进 → 转客户/拒绝/无回应。
          90 天后未转化的留资行清理。
        </li>
        <li>
          <strong>付费客户</strong>：账号信息保留至账号注销 + 法务要求的最少留存期（中国《电商法》要求订单数据 3 年）。
        </li>
        <li>
          <strong>API 调用日志</strong>：90 天滚动留存（用于计费对账 + 安全审计）。
        </li>
        <li>
          <strong>访问日志</strong>：Cloudflare 7 天 / 我们边缘 30 天。
        </li>
      </ul>

      <h2>5. 你的权利</h2>
      <ul>
        <li>
          <strong>查阅</strong>：要求看我们存的关于你的所有数据
        </li>
        <li>
          <strong>更正</strong>：要求改错的信息
        </li>
        <li>
          <strong>删除</strong>：要求清除你的账号 + 留资。
          已发生的订单数据按法务要求保留（中国《电商法》3 年）
        </li>
        <li>
          <strong>导出</strong>：要求结构化导出你的数据（JSON 格式）
        </li>
        <li>
          <strong>撤回同意</strong>：随时停用账号、不再使用我们的服务
        </li>
      </ul>

      <p>行使任何上述权利，发邮件 / 微信联系我们（见 <strong>第 8 节</strong>）。
      24 小时内首次响应，30 天内完成。</p>

      <h2>6. 数据安全</h2>
      <ul>
        <li>所有跟我们的连接走 HTTPS（TLS 1.2+）</li>
        <li>API 私钥 + 数据库密码以 chmod 600 文件存储，不进 git，不进日志</li>
        <li>支付宝集成走 RSA-2048 公私钥签名</li>
        <li>VPS 仅暴露 80/443，NewAPI 内核网络隔离不可外达</li>
        <li>每日数据库自动备份，offsite 复制到 Cloudflare R2 异地</li>
      </ul>

      <h2>7. 政策变更</h2>
      <p>
        重大变更（涉及数据收集范围 / 共享对象 / 保留期）我们会：
      </p>
      <ul>
        <li>提前 30 天在本页公告</li>
        <li>邮件 / 微信通知所有付费客户</li>
        <li>给你 30 天选择停用账号 + 删除数据的窗口</li>
      </ul>

      <h2>8. 联系我们</h2>
      <p>
        隐私问题、数据请求、投诉：通过{" "}
        <a href="/contact">/contact</a> 留下你的联系方式，
        founder 24 小时内回复。
      </p>
      <p>
        境内运营主体 + ICP 备案信息将在备案完成后于 footer 公示。
      </p>
    </LegalLayout>
  );
}

function PrivacyEN() {
  return (
    <LegalLayout
      eyebrow="Privacy Policy"
      title="How we handle your data"
      updated="2026-05-03"
    >
      <p>
        greentokey ("we") respects every user's ("you") privacy. This policy
        covers what we collect, why, how we use it, when we delete it, and the
        rights you can exercise.
      </p>
      <p>
        This applies to <strong>greentokey.com</strong>,
        <strong>www.greentokey.com</strong>, <strong>api.greentokey.com</strong>{" "}
        and the Token plan API service, AI service marketplace, and demo
        booking form hosted on them.
      </p>

      <h2>1. What we collect</h2>

      <h3>1.1 What you provide</h3>
      <ul>
        <li>
          <strong>Demo booking</strong>: WeChat ID / phone / homestay name /
          location / notes (only fields you choose to fill)
        </li>
        <li>
          <strong>Purchases</strong>: payment platform (Alipay / WeChat Pay /
          LemonSqueezy) returns order ID + amount + channel.
          <strong> We never see your card or Alipay password.</strong>
        </li>
        <li>
          <strong>Token plan</strong>: we generate your sk-xxx API key bound to
          your account
        </li>
        <li>
          <strong>Service usage</strong>: images, text, theme tags you upload
          (used only to generate the content you purchased; not shared, not used
          to train models)
        </li>
      </ul>

      <h3>1.2 What we collect automatically</h3>
      <ul>
        <li>
          <strong>API call logs</strong>: model name, token count, timestamp,
          source IP. Used for billing + rate limiting + debugging. Prompt and
          response text are NOT logged.
        </li>
        <li>
          <strong>Access logs</strong>: IP + UA + URL + timestamp. Retained
          7-30 days at Cloudflare CDN + Caddy edge.
        </li>
      </ul>

      <h3>1.3 What we do <strong>not</strong> collect</h3>
      <ul>
        <li>Your Xiaohongshu password (managed-ops never asks for it; content goes to your phone draft box)</li>
        <li>Your payment credentials (card number / password / CVV)</li>
        <li>GPS location (the "location" field is text, not coordinates)</li>
        <li>Third-party tracking cookies (only Cloudflare's anti-DDoS cookies)</li>
      </ul>

      <h2>2. How we use it</h2>
      <ol>
        <li>
          <strong>Service delivery</strong>: respond to API calls, generate
          purchased content, founder follow-up after demo booking.
        </li>
        <li>
          <strong>Billing</strong>: API token count → credits → monthly
          settlement. Payment platforms collect funds.
        </li>
        <li>
          <strong>Debugging + safety</strong>: anomaly detection, IP rate
          limiting, abuse investigation.
        </li>
        <li>
          <strong>Product improvement</strong>: aggregated stats ("DeepSeek
          accounts for 60% of calls"), no individual identification.
        </li>
      </ol>

      <h2>3. Who we share with</h2>

      <p><strong>Necessary third parties</strong> (your data flows through):</p>
      <ul>
        <li>
          <strong>Alipay</strong> / <strong>LemonSqueezy</strong> /{" "}
          <strong>hupijiao</strong> — payment + refunds. Shared: order ID,
          amount.
        </li>
        <li>
          <strong>NewAPI upstream model providers</strong> (OpenAI / Anthropic /
          DeepSeek / Aliyun / Zhipu / Moonshot, etc.) — your prompt is sent to
          the upstream model API to generate a response. <strong>Under the
          current technical configuration</strong>, we do not store prompt
          text at the application layer (NewAPI prompt-log settings: see
          repository <code>infra/coai-config.yaml.example</code>). This
          describes current implementation, not a binding perpetual commitment
          — any configuration change will be reflected on this page. Upstream
          privacy policies are published by each provider.
        </li>
        <li>
          <strong>Cloudflare</strong> — CDN + DDoS protection. Shared: access
          logs (IP / UA / URL).
        </li>
      </ul>

      <p>
        <strong>We do not sell your data.</strong> Never to ad networks, data
        brokers, or marketing lists.
      </p>

      <h2>4. Retention</h2>
      <ul>
        <li>
          <strong>Demo leads</strong>: founder follows up within 90 days →
          converted / declined / no-response. Unconverted lead rows purged
          after 90 days.
        </li>
        <li>
          <strong>Paying customers</strong>: account info kept until
          deactivation + minimum legal retention (China E-commerce Law requires
          order data 3 years).
        </li>
        <li>
          <strong>API call logs</strong>: 90-day rolling window (billing
          reconciliation + security audit).
        </li>
        <li>
          <strong>Access logs</strong>: Cloudflare 7 days / our edge 30 days.
        </li>
      </ul>

      <h2>5. Your rights</h2>
      <ul>
        <li><strong>Access</strong>: see all data we hold about you</li>
        <li><strong>Correction</strong>: fix incorrect info</li>
        <li>
          <strong>Deletion</strong>: erase your account + leads. Existing
          orders preserved per legal retention (3 years per China E-commerce
          Law).
        </li>
        <li><strong>Export</strong>: structured export (JSON)</li>
        <li>
          <strong>Withdraw consent</strong>: stop using the service anytime
        </li>
      </ul>

      <p>
        To exercise any right, contact us (see <strong>Section 8</strong>).
        First response within 24 hours; completion within 30 days.
      </p>

      <h2>6. Security</h2>
      <ul>
        <li>All connections to us go over HTTPS (TLS 1.2+)</li>
        <li>
          API private keys + DB passwords stored as chmod 600 files; never in
          git, never in logs
        </li>
        <li>Alipay integration uses RSA-2048 public/private key signing</li>
        <li>
          VPS exposes only 80/443; NewAPI core is network-isolated, not reachable
          externally
        </li>
        <li>
          Daily DB backups + offsite replication to Cloudflare R2 (different
          region)
        </li>
      </ul>

      <h2>7. Changes</h2>
      <p>For material changes (collection scope / sharing partners / retention):</p>
      <ul>
        <li>30-day advance notice on this page</li>
        <li>Email / WeChat notification to all paying customers</li>
        <li>30-day window to deactivate + delete data</li>
      </ul>

      <h2>8. Contact</h2>
      <p>
        Privacy questions, data requests, complaints: leave your contact at{" "}
        <a href="/contact">/contact</a>; founder responds within 24 hours.
      </p>
      <p>
        Mainland operating entity + ICP filing details will be posted in the
        footer once filing completes.
      </p>
    </LegalLayout>
  );
}

export default Privacy;
