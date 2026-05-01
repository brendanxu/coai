// Boot-time seed for the Layer 3 catalog.
//
// Why seed in code (vs SQL migration):
//   - System prompts live next to the agent definition. Reviewers see
//     prompt + price + credits in one diff.
//   - Updates are atomic git commits, not migration version chains.
//   - Easy to add agents/services without writing SQL.
//
// Idempotency contract:
//   - Each agent and service is identified by its `slug` (UNIQUE).
//   - Seed never overwrites an existing row (operator may have edited
//     it via SQL or admin UI; their edits win).
//   - Seed only INSERTs missing rows. Re-running on a populated table
//     is a no-op.
//   - Failure of one row does not block the others.
//
// Order of operations: agents first, then services (which reference
// agent_slug). The service.Migrate call in main.go runs the schema
// before this; SeedCatalog is called immediately after.

package service

import (
	"chat/globals"
	"database/sql"
	"errors"
	"fmt"
)

// SeedAgent is the strawman agent definition embedded in this binary.
// Hand-edit the struct values to tune system prompts; commit the diff.
type SeedAgent struct {
	Slug           string
	Name           string
	Description    string
	SystemPrompt   string
	PreferredModel string
	MinTier        string // light | standard | premium
	InputsSchema   string // JSON-Schema string; "" = no validation
	Status         string // active | draft | retired
}

// SeedService is the strawman service definition. agent_slug must
// reference a SeedAgent in this same file.
type SeedService struct {
	Slug            string
	Name            string
	Description     string
	Category        string // diy_agent | content_pack | managed_ops
	AgentSlug       string
	PriceCNYCents   int64
	IncludedCredits int
	BillingType     string // one_time | monthly | per_use
	Status          string
	LSVariantID     string // "" until founder configures LS variant
	DisplayOrder    int
}

// agentSeeds is the canonical agent registry. Edit here, ship via git.
//
// Tier rationale (per `coai/newapi/credit.go` 3-tier table):
//   - light    = 0.5 credits/call — DeepSeek-chat, Qwen-flash etc.
//   - standard = 1.0 credits/call — DeepSeek-r1, Claude-haiku, GPT-4o-mini
//   - premium  = 3.0 credits/call — GPT-4o, Claude Sonnet/Opus, o1
//
// MinTier on each agent declares the FLOOR for that agent's model
// quality. The runtime can pick a higher-tier model if needed; it
// cannot pick lower.
var agentSeeds = []SeedAgent{
	{
		Slug:           "xhs-copy-writer",
		Name:           "小红书内容生成器",
		Description:    "为民宿主生成高转化的小红书 caption + 标签 + emoji，针对 1-3 张房型/场景照片的输入。",
		PreferredModel: "deepseek-chat",
		MinTier:        "light",
		Status:         "active",
		// InputsSchema deliberately empty for v0.10 — runtime accepts
		// free-form input. v0.11 should add a JSON Schema validator
		// to lock the input shape (image URLs + theme tags).
		InputsSchema: "",
		SystemPrompt: `你是专门为云南大理民宿运营的小红书内容生成器。

任务：根据用户上传的房间/场景照片描述 + 主题词，生成 1 篇小红书 caption（150-300 字），包含：
1. 一个抓眼球的开头钩子（避开"姐妹们"、"宝子们"这种通用开场）
2. 3-4 段身临其境的场景描写（强调五感：视觉/触觉/嗅觉）
3. 实用信息块（位置/价格区间/适合人群），用 emoji 分隔
4. 5-8 个中文标签（包含 #大理民宿 #大理 #洱海 + 主题词的相关变体）
5. 结尾 CTA：引导私信详询而非直接报价

风格要求：
- 真实感优先于销售感。不要写"超值"、"限时"、"性价比"这些营销词
- 民宿主的人设是亲切的本地老板，不是营销号
- 季节感强：四季的大理是不同产品，按当前月份调整意境
- 避开"网红"、"出片"等已经被滥用的词

输出格式：直接给 caption 文本，不要解释或加 markdown 包装。`,
	},
	{
		Slug:           "mansu-managed-orchestrator",
		Name:           "民宿全闭环代运营调度器",
		Description:    "月度代运营服务的内部调度 agent。负责内容批量生成、自动发布排期、评论意图分类、私信回复草稿、ROI 月报。",
		PreferredModel: "deepseek-r1",
		MinTier:        "standard",
		Status:         "active",
		InputsSchema:   "",
		SystemPrompt: `你是一个民宿小红书运营调度器。

每个月的输入：
- 民宿元数据（位置/房型/价格/特色）
- 上月运营数据（曝光/互动/转化/订单归因）
- 本月运营目标（增订单 / 涨粉 / 维护老客户）

输出 4 个子任务的执行计划，每个任务交给对应的下游 agent 执行：
1. 内容生成调度：本月生成多少篇、按什么主题分布、按什么节奏发布
2. 互动管理调度：哪些评论需要老板亲自回复、哪些可以模板化、风险舆情拦截规则
3. 私信意图分类：报价/咨询/投诉/合作 4 类的关键词与回复模板
4. ROI 归因记录：本月哪些订单可归因到小红书，归因方法学

输出 JSON 格式，可直接被下游 agent 消费。具体的 caption 生成、回复文案不在此处生成——只规划。`,
	},
}

// serviceSeeds is the canonical service catalog. ls_variant_id is
// blank initially; founder fills via UPDATE after creating the LS
// variant in dashboard.
var serviceSeeds = []SeedService{
	{
		Slug:            "xhs-single-post",
		Name:            "单图小红书内容",
		Description:     "上传 1-3 张照片 + 主题词，立刻生成 1 篇可发布的小红书 caption。¥19 一次性，使用一次。",
		Category:        "diy_agent",
		AgentSlug:       "xhs-copy-writer",
		PriceCNYCents:   1900, // ¥19
		IncludedCredits: 60,   // ~30 standard calls — generous for one caption
		BillingType:     "one_time",
		Status:          "active",
		LSVariantID:     "",
		DisplayOrder:    10,
	},
	{
		Slug:            "xhs-monthly-pack",
		Name:            "月度内容包 30 篇",
		Description:     "一次提交一个月的素材，自动生成 30 篇小红书内容（不同主题、不同房型、不同节气）。¥299 一次性，30 天内使用。",
		Category:        "content_pack",
		AgentSlug:       "xhs-copy-writer",
		PriceCNYCents:   29900, // ¥299
		IncludedCredits: 1500,  // ~750 standard calls — 30 posts × ~50 calls buffer
		BillingType:     "one_time",
		Status:          "active",
		LSVariantID:     "",
		DisplayOrder:    20,
	},
	{
		Slug:            "mansu-managed-ops",
		Name:            "全闭环代运营",
		Description:     "我们替您运营整个小红书账号：内容生成 + 自动发布 + 评论互动 + 私信意图分类 + 月度 ROI 报告。¥1,980/月，预付 3 月起。",
		Category:        "managed_ops",
		AgentSlug:       "mansu-managed-orchestrator",
		PriceCNYCents:   198000, // ¥1,980
		IncludedCredits: 12000,  // ~6000 standard calls / month — generous for the orchestrator + sub-agents
		BillingType:     "monthly",
		Status:          "active",
		LSVariantID:     "",
		DisplayOrder:    30,
	},
}

// SeedCatalog inserts missing agents + services. Call once on boot
// after service.Migrate.
//
// Per-row failures are logged and continue — the goal is best-effort
// idempotency. A constraint violation on an existing slug is normal
// (the seed already ran; we don't overwrite operator edits) and not
// surfaced as an error.
func SeedCatalog(db *sql.DB) error {
	for _, a := range agentSeeds {
		if err := seedAgent(db, a); err != nil {
			// Don't return — keep seeding the rest.
			globals.Warn(fmt.Sprintf("service: seed agent %s failed: %v", a.Slug, err))
		}
	}
	for _, s := range serviceSeeds {
		if err := seedService(db, s); err != nil {
			globals.Warn(fmt.Sprintf("service: seed service %s failed: %v", s.Slug, err))
		}
	}
	return nil
}

// seedAgent inserts or skips. Returns nil if skipped (already exists).
func seedAgent(db *sql.DB, a SeedAgent) error {
	exists, err := agentExists(db, a.Slug)
	if err != nil {
		return fmt.Errorf("check agent exists: %w", err)
	}
	if exists {
		return nil
	}
	_, err = globals.ExecDb(db, `
		INSERT INTO gtk_agent
		  (slug, name, description, system_prompt, preferred_model,
		   min_tier, inputs_schema, status, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
	`, a.Slug, a.Name, a.Description, a.SystemPrompt, a.PreferredModel,
		a.MinTier, nullableString(a.InputsSchema), a.Status)
	return err
}

// seedService inserts or skips. Same idempotency contract as seedAgent.
func seedService(db *sql.DB, s SeedService) error {
	exists, err := serviceExists(db, s.Slug)
	if err != nil {
		return fmt.Errorf("check service exists: %w", err)
	}
	if exists {
		return nil
	}
	_, err = globals.ExecDb(db, `
		INSERT INTO gtk_service
		  (slug, name, description, category, agent_slug,
		   price_cny_cents, included_credits, billing_type, status,
		   ls_variant_id, display_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, s.Slug, s.Name, s.Description, s.Category, s.AgentSlug,
		s.PriceCNYCents, s.IncludedCredits, s.BillingType, s.Status,
		nullableString(s.LSVariantID), s.DisplayOrder)
	return err
}

func agentExists(db *sql.DB, slug string) (bool, error) {
	var dummy int
	row := globals.QueryRowDb(db, `SELECT 1 FROM gtk_agent WHERE slug = ?`, slug)
	err := row.Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func serviceExists(db *sql.DB, slug string) (bool, error) {
	var dummy int
	row := globals.QueryRowDb(db, `SELECT 1 FROM gtk_service WHERE slug = ?`, slug)
	err := row.Scan(&dummy)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// nullableString returns sql.NullString-equivalent: empty becomes
// untyped nil so the DB column gets SQL NULL instead of empty string.
// MySQL doesn't really care; SQLite ENUM-style CHECK constraints might,
// and ls_variant_id should genuinely be NULL not "" until configured.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
