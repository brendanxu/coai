import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Badge } from "@/components/ui/badge.tsx";
import {
  ArrowRight,
  ExternalLink,
  Phone,
  RotateCw,
} from "lucide-react";
import { toast } from "sonner";

import {
  listLeads,
  patchLeadStatus,
  makeDemoLeads,
  type Lead,
  type LeadStatus,
} from "@/api/admin-mansu.ts";

/**
 * /admin/mansu — founder concierge workspace for the 民宿 wedge.
 *
 * Why this exists: the v0.12 GTM is concierge-first (founder personally
 * onboards each 民宿 customer for the first 5-10 contracts) before the
 * DIY agent runs autonomously. Founder needs a single-pane view of:
 *   1. Who has submitted a lead (pipeline)
 *   2. Where each lead is (kanban: new → contacted → signed → running → done)
 *   3. Quick-jump to the active service order's runner page
 *
 * Single-pane kanban (not table) because founder's brain works that way:
 * "what's blocking each contract right now". Cards are dense — 1 lead =
 * ~80px tall — so a typical pipeline of 6-15 prospects fits without
 * scroll on a 13" laptop.
 *
 * Backend: /api/gtk/v1/admin/leads may not be live at v0.12 ship; page
 * falls back to a demo dataset (makeDemoLeads) so founder can review
 * the UX immediately. Once the endpoint lands, real data takes over
 * automatically — no client cutover.
 */
export default function Mansu() {
  const { t } = useTranslation();
  const [leads, setLeads] = useState<Lead[]>([]);
  const [loading, setLoading] = useState(true);
  const [demoMode, setDemoMode] = useState(false);

  async function refresh() {
    setLoading(true);
    const resp = await listLeads();
    if (resp.ok) {
      setLeads(resp.leads);
      setDemoMode(false);
    } else if (resp.kind === "not-found") {
      setLeads(makeDemoLeads());
      setDemoMode(true);
    } else {
      toast.error(resp.message);
    }
    setLoading(false);
  }

  useEffect(() => {
    refresh();
  }, []);

  async function moveLead(lead: Lead, next: LeadStatus) {
    if (demoMode) {
      // Local-only state shuffle in demo mode.
      setLeads((prev) =>
        prev.map((l) => (l.id === lead.id ? { ...l, status: next } : l)),
      );
      toast.success(`Demo: ${lead.homestay_name} → ${STAGES[next].label}`);
      return;
    }
    const resp = await patchLeadStatus(lead.id, next);
    if (resp.ok) {
      toast.success(t("admin.mansu.move-ok", "状态已更新"));
      refresh();
    } else {
      toast.error(resp.message);
    }
  }

  return (
    <div className="mansu space-y-4">
      <Card className="admin-card">
        <CardHeader className="select-none flex flex-row items-center justify-between">
          <div>
            <CardTitle>{t("admin.mansu", "民宿 · 礼宾台")}</CardTitle>
            <p className="text-xs text-muted-foreground mt-1">
              {t(
                "admin.mansu.subtitle",
                "线索进展看板 · 拖拽不可用,用箭头按钮在阶段间移动",
              )}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {demoMode && (
              <Badge variant="outline" className="text-xs">
                Demo 数据 · backend 未上线
              </Badge>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={refresh}
              disabled={loading}
            >
              <RotateCw
                className={`w-3.5 h-3.5 mr-1.5 ${loading ? "animate-spin" : ""}`}
              />
              {t("refresh", "刷新")}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <KanbanBoard leads={leads} onMove={moveLead} />
        </CardContent>
      </Card>

      <Card className="admin-card">
        <CardHeader className="select-none">
          <CardTitle className="text-sm">
            {t("admin.mansu.stats", "本周小结")}
          </CardTitle>
        </CardHeader>
        <CardContent>
          <Stats leads={leads} />
        </CardContent>
      </Card>
    </div>
  );
}

const STAGES: Record<
  LeadStatus,
  { label: string; cls: string; next?: LeadStatus; prev?: LeadStatus }
> = {
  new: { label: "新提交", cls: "bg-blue-50 border-blue-200", next: "contacted" },
  contacted: { label: "已联系", cls: "bg-amber-50 border-amber-200", next: "signed", prev: "new" },
  signed: { label: "已签约", cls: "bg-emerald-50 border-emerald-200", next: "running", prev: "contacted" },
  running: { label: "进行中", cls: "bg-primary/5 border-primary/20", next: "done", prev: "signed" },
  done: { label: "已完成", cls: "bg-muted border-border-soft", prev: "running" },
  lost: { label: "未成交", cls: "bg-muted/50 border-border-soft" },
};

const STAGE_ORDER: LeadStatus[] = ["new", "contacted", "signed", "running", "done"];

function KanbanBoard({
  leads,
  onMove,
}: {
  leads: Lead[];
  onMove: (lead: Lead, next: LeadStatus) => void;
}) {
  const grouped = useMemo(() => {
    const acc: Record<LeadStatus, Lead[]> = {
      new: [],
      contacted: [],
      signed: [],
      running: [],
      done: [],
      lost: [],
    };
    for (const lead of leads) {
      acc[lead.status].push(lead);
    }
    return acc;
  }, [leads]);

  return (
    <div className="grid grid-cols-1 md:grid-cols-5 gap-3">
      {STAGE_ORDER.map((stage) => (
        <div key={stage} className={`rounded-md border ${STAGES[stage].cls} p-2.5`}>
          <header className="flex items-center justify-between mb-2">
            <h3 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {STAGES[stage].label}
            </h3>
            <span className="text-xs text-muted-foreground">
              {grouped[stage].length}
            </span>
          </header>
          <div className="space-y-2">
            {grouped[stage].length === 0 ? (
              <p className="text-xs text-muted-foreground text-center py-4">
                —
              </p>
            ) : (
              grouped[stage].map((lead) => (
                <LeadCard key={lead.id} lead={lead} onMove={onMove} />
              ))
            )}
          </div>
        </div>
      ))}
    </div>
  );
}

function LeadCard({
  lead,
  onMove,
}: {
  lead: Lead;
  onMove: (lead: Lead, next: LeadStatus) => void;
}) {
  const stage = STAGES[lead.status];
  return (
    <article className="bg-background rounded border border-border-soft p-2.5 text-sm shadow-sm">
      <header className="flex items-start justify-between gap-2 mb-1">
        <h4 className="font-medium leading-tight">{lead.homestay_name || "(未填名)"}</h4>
        <span className="text-xs text-muted-foreground shrink-0">
          {formatDate(lead.created_at)}
        </span>
      </header>
      <p className="text-xs text-muted-foreground mb-1.5">{lead.homestay_loc}</p>
      <div className="flex items-center gap-2 text-xs text-muted-foreground mb-2">
        {lead.wechat && (
          <span className="inline-flex items-center gap-1">
            <span className="font-mono">{lead.wechat}</span>
          </span>
        )}
        {lead.phone && (
          <span className="inline-flex items-center gap-1">
            <Phone className="w-3 h-3" />
            {lead.phone}
          </span>
        )}
      </div>
      {lead.notes && (
        <p className="text-xs text-secondary-foreground mb-2 line-clamp-2">
          {lead.notes}
        </p>
      )}
      <footer className="flex items-center gap-1.5">
        {stage.prev && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 text-xs"
            onClick={() => onMove(lead, stage.prev!)}
            title={`回退到 ${STAGES[stage.prev].label}`}
          >
            ←
          </Button>
        )}
        {stage.next && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-1.5 text-xs"
            onClick={() => onMove(lead, stage.next!)}
            title={`移动到 ${STAGES[stage.next].label}`}
          >
            <ArrowRight className="w-3 h-3" />
          </Button>
        )}
        {lead.active_order_no && (
          <Link
            to={`/services/run/${lead.active_order_no}`}
            className="ml-auto text-xs text-primary hover:underline inline-flex items-center gap-1"
            target="_blank"
          >
            <ExternalLink className="w-3 h-3" />
            订单
          </Link>
        )}
      </footer>
    </article>
  );
}

function Stats({ leads }: { leads: Lead[] }) {
  const counts = useMemo(() => {
    const c: Record<LeadStatus, number> = {
      new: 0,
      contacted: 0,
      signed: 0,
      running: 0,
      done: 0,
      lost: 0,
    };
    for (const l of leads) c[l.status]++;
    return c;
  }, [leads]);

  const conversion =
    counts.new + counts.contacted > 0
      ? Math.round(
          ((counts.signed + counts.running + counts.done) /
            (counts.new + counts.contacted + counts.signed + counts.running + counts.done)) *
            100,
        )
      : 0;

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
      <Stat label="本周新提交" value={counts.new} />
      <Stat label="跟进中" value={counts.contacted + counts.signed + counts.running} />
      <Stat label="已成交客户" value={counts.signed + counts.running + counts.done} />
      <Stat label="转化率" value={`${conversion}%`} />
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="border border-border-soft rounded p-3">
      <p className="text-xs text-muted-foreground mb-1">{label}</p>
      <p className="font-display text-2xl">{value}</p>
    </div>
  );
}

function formatDate(iso: string) {
  const d = new Date(iso);
  const m = d.getMonth() + 1;
  const day = d.getDate();
  return `${m}/${day}`;
}
