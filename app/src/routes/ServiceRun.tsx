import { useEffect, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import {
  CheckCircle2,
  ChevronRight,
  Clock,
  Loader2,
  Upload,
  XCircle,
} from "lucide-react";

import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import { Textarea } from "@/components/ui/textarea.tsx";
import { Skeleton } from "@/components/ui/skeleton.tsx";
import { toast } from "sonner";

import {
  fetchServiceOrder,
  submitServiceRun,
  type ServiceOrder,
  type ServiceResult,
} from "@/api/service-order.ts";

/**
 * /services/run/:order_no — DIY agent runner page.
 *
 * Customer flow:
 *   1. buys a service on /services or via concierge
 *   2. receives this URL (in WeChat / dashboard / receipt email)
 *   3. lands here, fills inputs, submits, watches progress, gets result
 *
 * Backend may not be live at v0.12 ship — page renders friendly demo
 * mode when /api/gtk/v1/service-order/:order_no returns 404, so the
 * route is reachable for QA + screenshots ahead of the cutover.
 *
 * Polling: when status=running we poll every 4s. We don't open a WS
 * because the agent steps are 30s-3min (not stream-worthy) and polling
 * keeps the implementation simple + works through any CDN.
 */
export default function ServiceRun() {
  const { order_no } = useParams<{ order_no: string }>();
  const [searchParams] = useSearchParams();
  // v0.15: ?token=... in the URL lets the customer bypass login. Founder
  // shares this URL in WeChat right after a manual concierge sale; the
  // token is the per-order secret minted at order creation.
  const accessToken = searchParams.get("token") || undefined;
  const { t } = useTranslation();
  const [order, setOrder] = useState<ServiceOrder | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // Form state — kept simple, generic key/value. Per-service field
  // schemas come from order.service_kind in a later sprint.
  const [theme, setTheme] = useState("");
  const [extra, setExtra] = useState("");
  const [files, setFiles] = useState<FileList | null>(null);

  useEffect(() => {
    if (!order_no) return;
    let cancelled = false;
    let pollTimer: number | null = null;

    async function load() {
      const resp = await fetchServiceOrder(order_no!, accessToken);
      if (cancelled) return;
      if (resp.ok) {
        setOrder(resp.order);
        setError(null);
      } else if (resp.kind === "not-found") {
        // Pre-cutover: show demo placeholder so the URL is reachable.
        setOrder(makeDemoOrder(order_no!));
        setError(null);
      } else {
        setError(resp.message);
      }
      setLoading(false);
    }
    load();

    // Poll while running
    pollTimer = window.setInterval(() => {
      if (order?.status === "running") load();
    }, 4000);

    return () => {
      cancelled = true;
      if (pollTimer) window.clearInterval(pollTimer);
    };
  }, [order_no, order?.status, accessToken]);

  async function onSubmit() {
    if (!order_no) return;
    if (!theme.trim()) {
      toast.error(t("services.run.theme-required", "请填写主题词"));
      return;
    }
    setSubmitting(true);
    const resp = await submitServiceRun(order_no, {
      fields: { theme, extra },
      files: files ? Array.from(files).map((f) => ({ name: "image", data: f })) : undefined,
    }, accessToken);
    setSubmitting(false);
    if (resp.ok) {
      setOrder(resp.order);
      toast.success(t("services.run.submitted", "已提交,生成中..."));
    } else if (resp.kind === "not-found") {
      // Demo mode — fake the transition for visual QA
      setOrder({ ...makeDemoOrder(order_no), status: "running" });
      toast.success(t("services.run.demo-submitted", "Demo: 已模拟提交"));
    } else {
      toast.error(resp.message);
    }
  }

  return (
    <>
      <Header />
      <main className="flex-1">
        <div className="mx-auto px-6 py-12 md:py-16" style={{ maxWidth: "880px" }}>
          {/* Breadcrumb */}
          <nav className="mb-6 text-sm text-muted-foreground flex items-center gap-1.5">
            <Link to="/services" className="hover:text-foreground">
              {t("nav.services", "服务市场")}
            </Link>
            <ChevronRight className="w-3.5 h-3.5" />
            <span>{t("services.run.breadcrumb", "运行")}</span>
            {order_no && (
              <>
                <ChevronRight className="w-3.5 h-3.5" />
                <code className="font-mono text-xs">{order_no}</code>
              </>
            )}
          </nav>

          {loading && <RunSkeleton />}
          {!loading && error && <ErrorPanel message={error} />}
          {!loading && order && (
            <RunContent
              order={order}
              theme={theme}
              setTheme={setTheme}
              extra={extra}
              setExtra={setExtra}
              setFiles={setFiles}
              onSubmit={onSubmit}
              submitting={submitting}
            />
          )}
        </div>
      </main>
      <Footer />
    </>
  );
}

function RunSkeleton() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-32 w-full rounded-lg" />
      <Skeleton className="h-48 w-full rounded-lg" />
    </div>
  );
}

function ErrorPanel({ message }: { message: string }) {
  return (
    <div className="border border-destructive/30 bg-destructive/5 rounded-lg p-6">
      <div className="flex items-start gap-3">
        <XCircle className="w-5 h-5 text-destructive shrink-0 mt-0.5" />
        <div>
          <h2 className="font-medium mb-1">无法加载订单</h2>
          <p className="text-sm text-muted-foreground">{message}</p>
          <Link to="/contact" className="text-sm text-primary hover:underline mt-3 inline-block">
            联系 support →
          </Link>
        </div>
      </div>
    </div>
  );
}

function RunContent({
  order,
  theme,
  setTheme,
  extra,
  setExtra,
  setFiles,
  onSubmit,
  submitting,
}: {
  order: ServiceOrder;
  theme: string;
  setTheme: (v: string) => void;
  extra: string;
  setExtra: (v: string) => void;
  setFiles: (f: FileList | null) => void;
  onSubmit: () => void;
  submitting: boolean;
}) {
  return (
    <>
      <OrderHeader order={order} />

      {order.status === "pending" && (
        <InputForm
          theme={theme}
          setTheme={setTheme}
          extra={extra}
          setExtra={setExtra}
          setFiles={setFiles}
          onSubmit={onSubmit}
          submitting={submitting}
        />
      )}

      {order.status === "running" && <RunningPanel />}

      {order.status === "complete" && order.result && (
        <ResultPanel result={order.result} />
      )}

      {order.status === "failed" && (
        <FailurePanel error={order.error || "未知错误"} />
      )}
    </>
  );
}

function OrderHeader({ order }: { order: ServiceOrder }) {
  const statusBadge = STATUS_LABEL[order.status];
  return (
    <header className="mb-8 pb-6 border-b border-border-soft">
      <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-2">
        {order.service_kind === "diy-agent" ? "自助代运行" : "礼宾代办"}
      </p>
      <h1 className="font-display text-3xl md:text-4xl tracking-tight leading-tight mb-3">
        {order.service_name}
      </h1>
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <code className="font-mono text-xs px-2 py-0.5 bg-muted rounded">
          {order.order_no}
        </code>
        <span
          className={`inline-flex items-center gap-1.5 text-xs px-2 py-0.5 rounded-full ${statusBadge.cls}`}
        >
          {statusBadge.icon}
          {statusBadge.text}
        </span>
      </div>
    </header>
  );
}

const STATUS_LABEL: Record<
  string,
  { text: string; icon: JSX.Element; cls: string }
> = {
  pending: {
    text: "待提交",
    icon: <Clock className="w-3 h-3" />,
    cls: "bg-muted text-foreground",
  },
  running: {
    text: "运行中",
    icon: <Loader2 className="w-3 h-3 animate-spin" />,
    cls: "bg-primary/10 text-primary",
  },
  complete: {
    text: "已完成",
    icon: <CheckCircle2 className="w-3 h-3" />,
    cls: "bg-emerald-100 text-emerald-700",
  },
  failed: {
    text: "失败",
    icon: <XCircle className="w-3 h-3" />,
    cls: "bg-destructive/10 text-destructive",
  },
};

function InputForm({
  theme,
  setTheme,
  extra,
  setExtra,
  setFiles,
  onSubmit,
  submitting,
}: {
  theme: string;
  setTheme: (v: string) => void;
  extra: string;
  setExtra: (v: string) => void;
  setFiles: (f: FileList | null) => void;
  onSubmit: () => void;
  submitting: boolean;
}) {
  const { t } = useTranslation();
  return (
    <section className="space-y-5">
      <div>
        <label className="block text-sm font-medium mb-1.5">
          {t("services.run.theme-label", "主题词")}{" "}
          <span className="text-destructive">*</span>
        </label>
        <Input
          value={theme}
          onChange={(e) => setTheme(e.target.value)}
          placeholder={t(
            "services.run.theme-placeholder",
            "例:洱海日落 / 民宿早餐 / 周末慢生活",
          )}
          maxLength={80}
        />
        <p className="text-xs text-muted-foreground mt-1">
          {t(
            "services.run.theme-hint",
            "一句话告诉 agent 这次发什么内容,越具体越好",
          )}
        </p>
      </div>

      <div>
        <label className="block text-sm font-medium mb-1.5">
          {t("services.run.upload-label", "素材图片")}
        </label>
        <div className="border-2 border-dashed border-border rounded-lg p-6 text-center hover:bg-muted/30 transition-colors">
          <Upload className="w-6 h-6 text-muted-foreground mx-auto mb-2" />
          <input
            type="file"
            multiple
            accept="image/*"
            onChange={(e) => setFiles(e.target.files)}
            className="text-sm w-full"
          />
          <p className="text-xs text-muted-foreground mt-2">
            {t(
              "services.run.upload-hint",
              "建议 3-9 张,横构图优先,jpg/png 各 ≤ 8MB",
            )}
          </p>
        </div>
      </div>

      <div>
        <label className="block text-sm font-medium mb-1.5">
          {t("services.run.extra-label", "备注 (可选)")}
        </label>
        <Textarea
          value={extra}
          onChange={(e) => setExtra(e.target.value)}
          rows={3}
          placeholder={t(
            "services.run.extra-placeholder",
            "想突出的卖点 / 想避免的话题 / 特殊要求",
          )}
          maxLength={500}
        />
      </div>

      <div className="pt-2">
        <Button
          onClick={onSubmit}
          disabled={submitting || !theme.trim()}
          className="rounded-full px-6"
        >
          {submitting && <Loader2 className="w-4 h-4 mr-2 animate-spin" />}
          {submitting
            ? t("services.run.submitting", "提交中...")
            : t("services.run.submit", "开始生成")}
        </Button>
      </div>
    </section>
  );
}

function RunningPanel() {
  return (
    <div className="border border-border-soft rounded-lg p-8 text-center">
      <Loader2 className="w-8 h-8 text-primary mx-auto mb-3 animate-spin" />
      <h2 className="font-display text-lg mb-1">Agent 正在工作</h2>
      <p className="text-sm text-muted-foreground">
        通常 1-3 分钟。生成中可以离开,完成后会更新本页;急用可以挂着别关。
      </p>
    </div>
  );
}

function ResultPanel({ result }: { result: ServiceResult }) {
  return (
    <section className="space-y-4">
      <h2 className="font-display text-xl">生成结果</h2>
      <div className="border border-border-soft rounded-lg p-5 bg-card">
        <RenderResult result={result} />
      </div>
      <p className="text-xs text-muted-foreground">
        不满意?在下方反馈,我们重跑一次免费。
      </p>
    </section>
  );
}

function RenderResult({ result }: { result: ServiceResult }) {
  if (result.kind === "text") {
    return <pre className="whitespace-pre-wrap text-sm leading-relaxed">{result.content}</pre>;
  }
  if (result.kind === "image") {
    return (
      <img
        src={result.url}
        alt={result.alt || ""}
        className="max-w-full rounded"
      />
    );
  }
  if (result.kind === "rednote-post") {
    return (
      <div className="space-y-3">
        <h3 className="font-medium">{result.title}</h3>
        <pre className="whitespace-pre-wrap text-sm leading-relaxed">{result.body}</pre>
        {result.tags.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {result.tags.map((tag) => (
              <span
                key={tag}
                className="text-xs px-2 py-0.5 bg-muted rounded-full text-muted-foreground"
              >
                #{tag}
              </span>
            ))}
          </div>
        )}
        {result.images.length > 0 && (
          <div className="grid grid-cols-3 gap-2">
            {result.images.map((src, i) => (
              <img key={i} src={src} alt="" className="rounded aspect-square object-cover" />
            ))}
          </div>
        )}
      </div>
    );
  }
  if (result.kind === "multi") {
    return (
      <div className="space-y-4">
        {result.items.map((item, i) => (
          <RenderResult key={i} result={item} />
        ))}
      </div>
    );
  }
  return null;
}

function FailurePanel({ error }: { error: string }) {
  return (
    <div className="border border-destructive/30 bg-destructive/5 rounded-lg p-6">
      <div className="flex items-start gap-3">
        <XCircle className="w-5 h-5 text-destructive shrink-0 mt-0.5" />
        <div className="flex-1">
          <h2 className="font-medium mb-1">这次没跑成功</h2>
          <p className="text-sm text-muted-foreground mb-3">{error}</p>
          <p className="text-sm">
            点 <Link to="/contact" className="text-primary hover:underline">联系 support</Link>{" "}
            或重新提交。失败的运行不扣额度。
          </p>
        </div>
      </div>
    </div>
  );
}

// Demo placeholder while backend is offline.
function makeDemoOrder(orderNo: string): ServiceOrder {
  return {
    order_no: orderNo,
    service_id: 0,
    service_name: "民宿小红书代发 · 单条",
    service_kind: "diy-agent",
    status: "pending",
    inputs: null,
    result: null,
    error: null,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
}
