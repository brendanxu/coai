import { useTranslation } from "react-i18next";
import LegalLayout from "@/components/Marketing/LegalLayout.tsx";

/**
 * 服务条款 / Terms of Service.
 *
 * Why this exists: ICP filing requires it; payment platforms (Alipay,
 * LemonSqueezy) require linkable T&C; 民宿 customers paying ¥1980/月
 * deserve clear refund + cancellation language.
 *
 * Conservative early-stage language — heavily caveated, no SLA promises
 * we can't keep yet. Founder lawyer review required before enterprise
 * customers (post-PMF).
 */
function Terms() {
  const { i18n } = useTranslation();
  const isEn = (i18n.language || "").toLowerCase().startsWith("en");
  if (isEn) return <TermsEN />;
  return <TermsCN />;
}

function TermsCN() {
  return (
    <LegalLayout
      eyebrow="服务条款"
      title="使用 greentokey 的规则"
      updated="2026-05-03"
    >
      <p>
        本条款约定你使用 greentokey 服务的权利和义务。**注册账号或购买任何服务即视为同意本条款**。
      </p>

      <h2>1. 我们提供的服务</h2>
      <p>
        greentokey 提供两类服务:
      </p>
      <ul>
        <li>
          <strong>Token 套餐</strong>:基于 NewAPI 的多模型 API 聚合服务。¥99/月起,我们生成你的 sk-xxx API Key,你可调用池中所有上线模型。
        </li>
        <li>
          <strong>服务市场</strong>:基于 AI 的具体服务,例如小红书内容生成、月度内容包、民宿全闭环代运营等。按服务定价,购买后获得对应内容产出。
        </li>
      </ul>

      <h2>2. 账号</h2>
      <ul>
        <li>账号必须由真实自然人或合法机构持有,不接受测试号 / 一次性邮箱 / 已被注销的实体</li>
        <li>账号不得转让 / 共享给第三方使用</li>
        <li>账号被盗用造成的损失由你承担,但发现盗用后立刻通知我们,我们可锁定 + 协助找回</li>
        <li>我们有权在严重违反本条款时锁定或删除账号(见 第 8 节)</li>
      </ul>

      <h2>3. 你的责任</h2>
      <p>使用我们的服务,你承诺:</p>
      <ul>
        <li>
          <strong>合法用途</strong>:不利用我们的 API 或服务做违反中国法律的事(包括但不限于:生成违禁内容 / 攻击他人系统 / 数据爬取至侵权水平 / 假冒他人身份)
        </li>
        <li>
          <strong>不滥用</strong>:不用同一 API Key 做远超你套餐承载量的并发请求(我们有限流);不通过自动化手段大量注册账号;不利用我们做钓鱼 / 诈骗 / 黑产。
        </li>
        <li>
          <strong>付费义务</strong>:Token 套餐按月预付;服务市场购买后即扣款,不存在延期支付。
        </li>
        <li>
          <strong>合规上游</strong>:你调用的上游 LLM 模型(GPT/Claude/DeepSeek 等)有它们各自的 acceptable use policy,你也需要遵守。
        </li>
      </ul>

      <h2>4. 我们的责任</h2>
      <p>我们承诺:</p>
      <ul>
        <li>
          <strong>服务可用</strong>:尽力保持平台 99% 月度可用率(早期阶段不写入合同,但是我们的内部目标)。重大宕机我们 status 页公告 + 邮件通知。
        </li>
        <li>
          <strong>不读你的 prompt</strong>:除非排查严重故障 / 安全事件 / 法律传唤,否则我们不主动读取 API 调用 prompt 文本。
        </li>
        <li>
          <strong>响应支持</strong>:工作日 (周一-五) 24 小时内首次回复;非工作日尽力。
        </li>
      </ul>

      <h2>5. 退款政策</h2>

      <h3>Token 套餐 (¥99/月)</h3>
      <ul>
        <li><strong>未使用 7 天内退全款</strong>:如果你购买后 7 日内调用 API 不超过 100 次,无理由全额退款</li>
        <li><strong>已使用按比例</strong>:如已大量使用,按已用 credits 比例扣除,余额按月剩余天数退还</li>
        <li><strong>套餐之外加扣的不可退</strong>:超出套餐 credit 上限的额外消耗(如有)按 token 计费且不可退</li>
      </ul>

      <h3>服务市场</h3>
      <ul>
        <li>
          <strong>DIY 智能体类(¥19、¥299 等一次性)</strong>:服务执行前可全额退款。**服务执行(agent 已 run)后**不退款,因为 token 已消耗。
        </li>
        <li>
          <strong>月度代运营(¥1980/月 mansu-managed-ops)</strong>:30 天若入住率没改善,无理由退 50%(即退 ¥990)。预付 3 月的客户:首月走 30 天保障,后续月份按提前 1 天通知不再扣费。
        </li>
      </ul>

      <h3>退款怎么发起</h3>
      <p>
        在 <a href="/contact">/contact</a> 留言"我要退款 + 订单号 + 原因",我们 24 小时回复。退款经原支付通道(支付宝/LemonSqueezy)原路返回,3-7 个工作日到账(取决于支付通道)。
      </p>

      <h2>6. 服务变更</h2>
      <ul>
        <li>
          <strong>价格调整</strong>:任何涨价**提前 30 天**邮件 / 微信通知现有客户。在通知期内,你可以无理由退款全部未使用余额。
        </li>
        <li>
          <strong>服务下线</strong>:某个具体服务(如某种 DIY 智能体)下线,我们退还该服务的所有未使用 credits,不退已使用部分。
        </li>
        <li>
          <strong>模型池调整</strong>:池中某个模型如被上游禁用 / 涨价 5x 以上,我们有权从池中移除。会提前通知一周(如非紧急合规要求)。
        </li>
        <li>
          <strong>本条款修改</strong>:本条款修改我们提前 30 天页面公告,期间你可以拒绝并退款。
        </li>
      </ul>

      <h2>7. 数据所有权</h2>
      <ul>
        <li>
          <strong>你输入的内容</strong>(prompt / 图片 / 文字 / 民宿信息) — **所有权归你**,我们仅作为"代客调用"使用一次,不做训练 / 二次销售。
        </li>
        <li>
          <strong>AI 生成的内容</strong>(caption / 文案 / 翻译) — **版权归你**,你可商用 / 修改 / 转售。我们不主张任何 copyright。
        </li>
        <li>
          <strong>我们的代码 / Prompt 模板 / Agent 设计</strong> — 知识产权归 greentokey,你不能反向工程或二次出售。
        </li>
      </ul>

      <h2>8. 终止</h2>
      <p>本条款在以下情况终止:</p>
      <ul>
        <li>你主动注销账号(在 <a href="/contact">/contact</a> 留言)</li>
        <li>你严重违反第 3 节(违法用途 / 重大滥用 / 拖欠付款 30 天以上),我们书面通知后锁定账号</li>
        <li>greentokey 整体停止运营。我们至少**提前 90 天**公告,期间你可导出全部数据,余额按未使用部分原路退回</li>
      </ul>

      <h2>9. 免责声明</h2>
      <ul>
        <li>
          <strong>AI 输出不保证准确</strong>:我们提供的是工具,AI 生成的内容可能不准确 / 不适宜 / 有偏见。**你在发布前必须人工审核**。我们不对 AI 输出导致的商业损失负责。
        </li>
        <li>
          <strong>上游模型可用性</strong>:GPT/Claude/DeepSeek 等上游模型由各自厂商提供。如某模型上游故障 / 限流 / 关闭,我们尽力切换其他模型,但不保证你点的某个具体模型一定可用。
        </li>
        <li>
          <strong>不可抗力</strong>:战争 / 自然灾害 / 政府行为 / 互联网基础设施重大故障导致的服务中断,我们不承担违约责任。
        </li>
      </ul>

      <h2>10. 法律适用 + 争议</h2>
      <p>
        本条款适用 <strong>中华人民共和国法律</strong>(不含港澳台)。任何争议优先协商;协商不成提交 <strong>greentokey 注册地有管辖权的人民法院</strong>处理。
      </p>

      <h2>11. 联系我们</h2>
      <p>
        条款问题、争议、违规举报:在 <a href="/contact">/contact</a> 留言,标明"条款相关",24 小时回复。
      </p>

      <hr />
      <p style={{ fontSize: "0.85em", color: "hsl(var(--muted-foreground))" }}>
        本条款由 greentokey 团队起草,持续迭代。修改记录在 git 历史可查询(repo: github.com/tanaxu626/greentokey)。重大法律咨询建议自行联系律师。
      </p>
    </LegalLayout>
  );
}

function TermsEN() {
  return (
    <LegalLayout
      eyebrow="Terms of Service"
      title="Rules for using greentokey"
      updated="2026-05-03"
    >
      <p>
        These Terms govern your use of greentokey services. <strong>Creating an account or purchasing any service constitutes acceptance.</strong>
      </p>

      <h2>1. What we provide</h2>
      <p>greentokey offers two service axes:</p>
      <ul>
        <li>
          <strong>Token Plans</strong>: NewAPI-routed multi-model API aggregation. ¥99/month and up; we issue your sk-xxx API key for access to all online pool models.
        </li>
        <li>
          <strong>Service Marketplace</strong>: AI-delivered concrete services — Xiaohongshu content generation, monthly content packs, full-management for homestays. Priced per service; purchase yields the corresponding output.
        </li>
      </ul>

      <h2>2. Accounts</h2>
      <ul>
        <li>Accounts must belong to a real person or registered entity</li>
        <li>Accounts are non-transferable, not shareable</li>
        <li>You bear losses from account compromise; notify us immediately upon discovery</li>
        <li>We may lock or delete accounts for serious violations (see Section 8)</li>
      </ul>

      <h2>3. Your responsibilities</h2>
      <p>By using our services you agree to:</p>
      <ul>
        <li><strong>Lawful use</strong>: not use our API or services for illegal purposes under PRC law (including: generating restricted content / system attacks / mass scraping / impersonation)</li>
        <li><strong>No abuse</strong>: no concurrent requests beyond plan capacity; no automated mass-account registration; no phishing / fraud / illicit operations</li>
        <li><strong>Payment</strong>: Token plans are pre-paid monthly; marketplace purchases charged at order time</li>
        <li><strong>Upstream compliance</strong>: each upstream model (GPT / Claude / DeepSeek / etc.) has its own acceptable-use policy that also applies</li>
      </ul>

      <h2>4. Our responsibilities</h2>
      <ul>
        <li><strong>Availability</strong>: best effort 99% monthly uptime (internal target, not contractual at this stage). Major outages posted on status page + email</li>
        <li><strong>Privacy</strong>: we don't read your prompt content unless investigating critical incidents / legal subpoena</li>
        <li><strong>Support</strong>: first response within 24 hours on weekdays; best effort on weekends</li>
      </ul>

      <h2>5. Refunds</h2>

      <h3>Token Plans (¥99/month)</h3>
      <ul>
        <li><strong>7-day no-questions full refund</strong> if you used the API ≤100 times</li>
        <li><strong>Pro-rated</strong> for higher usage: deduct used credits, refund remaining month</li>
        <li><strong>Overage charges non-refundable</strong> (per-token billing for usage beyond plan limit)</li>
      </ul>

      <h3>Service Marketplace</h3>
      <ul>
        <li><strong>One-time DIY agents (¥19, ¥299)</strong>: full refund before service execution. Once the agent has run (tokens consumed), no refund.</li>
        <li><strong>Monthly managed-ops (¥1980/mo)</strong>: 30-day no-improvement 50% refund (¥990). 3-month-prepaid customers: month 1 covers the 30-day guarantee; subsequent months stop on 1-day notice.</li>
      </ul>

      <h3>How to request</h3>
      <p>
        Leave a message at <a href="/contact">/contact</a> with "refund + order ID + reason"; we respond within 24 hours. Refunds go via the original payment channel (Alipay / LemonSqueezy) and arrive 3-7 business days later.
      </p>

      <h2>6. Service changes</h2>
      <ul>
        <li><strong>Price changes</strong>: 30-day advance notice via email / WeChat. Customers may refund unused balance during the notice period</li>
        <li><strong>Service retirement</strong>: refund unused credits for the retired service; used portion non-refundable</li>
        <li><strong>Pool model changes</strong>: a model removed from pool (upstream banned / 5x+ price increase) will be announced 1 week in advance unless emergency compliance</li>
        <li><strong>These Terms</strong>: changes posted 30 days in advance; you may decline and refund</li>
      </ul>

      <h2>7. Data ownership</h2>
      <ul>
        <li><strong>Your inputs</strong> (prompts / images / homestay info) — ownership stays yours; we use it for one-shot service delivery, not training or resale</li>
        <li><strong>AI-generated outputs</strong> (captions / copy / translations) — copyright yours; commercial use, modification, resale all permitted</li>
        <li><strong>Our code / prompt templates / agent designs</strong> — IP belongs to greentokey; reverse engineering / resale prohibited</li>
      </ul>

      <h2>8. Termination</h2>
      <p>These Terms terminate when:</p>
      <ul>
        <li>You voluntarily close your account (request via <a href="/contact">/contact</a>)</li>
        <li>You materially breach Section 3 (illegal use / serious abuse / 30+ days payment overdue); we lock the account after written notice</li>
        <li>greentokey discontinues operations. We give at least <strong>90 days advance notice</strong>; you can export data; unused balance refunded</li>
      </ul>

      <h2>9. Disclaimers</h2>
      <ul>
        <li><strong>AI output accuracy</strong>: we provide tools; AI output may be inaccurate / inappropriate / biased. <strong>You must review before publishing</strong>. We're not liable for business losses from AI output</li>
        <li><strong>Upstream model availability</strong>: pool models are provided by their respective vendors. If a model upstream fails / rate limits / shuts down, we route to alternates but don't guarantee any specific model's availability</li>
        <li><strong>Force majeure</strong>: war / disasters / government action / major internet infrastructure failures — we're not liable for breach</li>
      </ul>

      <h2>10. Governing law + disputes</h2>
      <p>
        These Terms are governed by <strong>the laws of the People's Republic of China</strong> (mainland, not HK / Macao / Taiwan). Disputes go to negotiation first; if unresolved, to a competent court at greentokey's place of registration.
      </p>

      <h2>11. Contact</h2>
      <p>
        Terms questions, disputes, abuse reports: leave a message at <a href="/contact">/contact</a> marked "Terms-related"; we respond within 24 hours.
      </p>

      <hr />
      <p style={{ fontSize: "0.85em", color: "hsl(var(--muted-foreground))" }}>
        These Terms drafted by the greentokey team; iterative. Change history visible in git (github.com/tanaxu626/greentokey). Consult a lawyer for material legal questions.
      </p>
    </LegalLayout>
  );
}

export default Terms;
