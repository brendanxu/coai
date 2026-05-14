// PaymentSuccess.tsx — /payment/success?order_no=xxx&provider=ls|hupijiao
//
// Public route (reads auth). Shows payment confirmation, API credentials,
// and next steps. Polls order status until paid or timeout.

import { useState, useEffect, useRef, useCallback } from "react";
import { useSearchParams, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useSelector } from "react-redux";
import axios from "axios";
import { toast } from "sonner";
import { selectAuthenticated } from "@/store/auth.ts";
import { loadMyOrder } from "@/api/orders.ts";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import { Button } from "@/components/ui/button.tsx";

const POLL_INTERVAL = 3000;
const POLL_TIMEOUT = 30000;

function CopyField({ label, value, warning }: { label: string; value: string; warning?: string }) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      toast.success(t("payment_success.copied"));
      setTimeout(() => setCopied(false), 2000);
    } catch (_e) {
      toast.error("复制失败");
    }
  };

  return (
    <div style={{ marginBottom: 16 }}>
      <div style={{ fontSize: 13, fontWeight: 600, marginBottom: 4, opacity: 0.7 }}>
        {label}
      </div>
      {warning && (
        <div style={{ fontSize: 12, color: "#b45309", marginBottom: 6 }}>
          ⚠ {warning}
        </div>
      )}
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        <div
          style={{
            flex: 1,
            fontFamily: "JetBrains Mono, monospace",
            fontSize: 13,
            background: "rgba(0,0,0,0.04)",
            borderRadius: 6,
            padding: "7px 12px",
            wordBreak: "break-all",
            border: "1px solid var(--border, #e2e8f0)",
          }}
        >
          {value || "—"}
        </div>
        <Button
          size="sm"
          variant="outline"
          onClick={handleCopy}
          style={{ flexShrink: 0, minWidth: 60 }}
        >
          {copied ? t("payment_success.copied") : t("payment_success.copy")}
        </Button>
      </div>
    </div>
  );
}

function PaymentSuccess() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  // Accept ?order_no= (internal links) OR ?subscription_id= (LS redirect).
  // Token plans use the LS subscription_id as the local order_no.
  const orderNo =
    params.get("order_no") ??
    params.get("subscription_id") ??
    "";

  const authenticated = useSelector(selectAuthenticated);

  const [orderStatus, setOrderStatus] = useState<string | null>(null);
  const [polling, setPolling] = useState(!!orderNo);
  const [timedOut, setTimedOut] = useState(false);
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [baseUrl, setBaseUrl] = useState<string>("");

  const pollStartRef = useRef(Date.now());
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Derive base URL from current window location
  useEffect(() => {
    const url = `${window.location.protocol}//${window.location.host}`;
    setBaseUrl(url);
  }, []);

  // Fetch API credentials
  const fetchCredentials = useCallback(async () => {
    if (!authenticated) return;
    try {
      const r = await axios.get<{ success: boolean; data?: { api_key?: string } }>(
        "/gtk/v1/binding"
      );
      if (r.data?.success && r.data.data?.api_key) {
        setApiKey(r.data.data.api_key);
      }
    } catch (_e) {
      // binding not found = no plan yet, ignore
    }
  }, [authenticated]);

  // Poll order status
  const pollOrder = useCallback(async () => {
    if (!orderNo) {
      setPolling(false);
      return;
    }

    const elapsed = Date.now() - pollStartRef.current;
    if (elapsed > POLL_TIMEOUT) {
      setTimedOut(true);
      setPolling(false);
      return;
    }

    const detail = await loadMyOrder(orderNo);
    if (detail) {
      setOrderStatus(detail.status);
      if (detail.status === "paid" || detail.status === "running" || detail.status === "completed") {
        setPolling(false);
        fetchCredentials();
        return;
      }
    }

    timerRef.current = setTimeout(pollOrder, POLL_INTERVAL);
  }, [orderNo, fetchCredentials]);

  useEffect(() => {
    if (orderNo && authenticated) {
      pollStartRef.current = Date.now();
      pollOrder();
    } else if (!orderNo) {
      setPolling(false);
      fetchCredentials();
    }
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [orderNo, authenticated, pollOrder, fetchCredentials]);

  if (!authenticated) {
    return (
      <div
        style={{
          minHeight: "100vh",
          background: "var(--bg, #F4EFE5)",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          fontFamily: "Manrope, sans-serif",
          padding: 24,
        }}
      >
        <Card style={{ maxWidth: 400, width: "100%", textAlign: "center" }}>
          <CardContent style={{ paddingTop: 32, paddingBottom: 32 }}>
            <div style={{ fontSize: 16, marginBottom: 16 }}>
              {t("payment_success.not_authenticated")}
            </div>
            <Link to="/login">
              <Button>{t("payment_success.login")}</Button>
            </Link>
          </CardContent>
        </Card>
      </div>
    );
  }

  const isPaid =
    orderStatus === "paid" ||
    orderStatus === "running" ||
    orderStatus === "completed";

  return (
    <div
      style={{
        minHeight: "100vh",
        background: "var(--bg, #F4EFE5)",
        fontFamily: "Manrope, sans-serif",
        padding: "40px 16px",
      }}
    >
      <div style={{ maxWidth: 600, margin: "0 auto", display: "flex", flexDirection: "column", gap: 20 }}>

        {/* Hero */}
        <Card>
          <CardContent
            style={{
              paddingTop: 36,
              paddingBottom: 36,
              textAlign: "center",
            }}
          >
            <div style={{ fontSize: 56, lineHeight: 1 }}>✅</div>
            <h1
              style={{
                fontFamily: "Fraunces, serif",
                fontSize: 28,
                fontWeight: 700,
                color: "var(--accent, #2D5A3D)",
                margin: "12px 0 6px",
              }}
            >
              {t("payment_success.title")} 🎉
            </h1>
            <p style={{ fontSize: 15, opacity: 0.7 }}>
              {t("payment_success.subtitle")}
            </p>
          </CardContent>
        </Card>

        {/* Order status card */}
        {orderNo && (
          <Card>
            <CardHeader>
              <CardTitle style={{ fontSize: 15 }}>订单状态</CardTitle>
            </CardHeader>
            <CardContent>
              <div style={{ fontSize: 13, opacity: 0.65, marginBottom: 8 }}>
                订单号: <span style={{ fontFamily: "JetBrains Mono, monospace" }}>{orderNo}</span>
              </div>
              {polling && !timedOut && (
                <div style={{ display: "flex", alignItems: "center", gap: 10, fontSize: 14 }}>
                  <span
                    style={{
                      display: "inline-block",
                      width: 16,
                      height: 16,
                      border: "2px solid var(--accent, #2D5A3D)",
                      borderTopColor: "transparent",
                      borderRadius: "50%",
                      animation: "spin 0.8s linear infinite",
                    }}
                  />
                  {t("payment_success.polling")}
                </div>
              )}
              {timedOut && (
                <div style={{ color: "#b45309", fontSize: 14 }}>
                  {t("payment_success.timeout")}
                </div>
              )}
              {isPaid && (
                <div style={{ color: "var(--accent, #2D5A3D)", fontSize: 14, fontWeight: 600 }}>
                  ✓ 支付已确认
                </div>
              )}
              {orderStatus && !isPaid && !polling && !timedOut && (
                <div style={{ fontSize: 14, opacity: 0.7 }}>
                  状态: {orderStatus}
                </div>
              )}
            </CardContent>
          </Card>
        )}

        {/* API credentials */}
        <Card>
          <CardHeader>
            <CardTitle style={{ fontSize: 15 }}>
              {t("payment_success.credentials_title")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <CopyField
              label={t("payment_success.base_url_label")}
              value={baseUrl}
            />
            <CopyField
              label={t("payment_success.api_key_label")}
              value={apiKey ?? ""}
              warning={apiKey ? t("payment_success.api_key_warning") : undefined}
            />
          </CardContent>
        </Card>

        {/* Next steps */}
        <Card>
          <CardHeader>
            <CardTitle style={{ fontSize: 15 }}>
              {t("payment_success.next_title")}
            </CardTitle>
          </CardHeader>
          <CardContent>
            <div style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <Link
                to="/dashboard"
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  color: "var(--accent, #2D5A3D)",
                  textDecoration: "none",
                  fontSize: 15,
                  fontWeight: 500,
                }}
              >
                📊 {t("payment_success.next_usage")} →
              </Link>
              <Link
                to="/docs"
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  color: "var(--accent, #2D5A3D)",
                  textDecoration: "none",
                  fontSize: 15,
                  fontWeight: 500,
                }}
              >
                📖 {t("payment_success.next_docs")} →
              </Link>
              <Link
                to="/contact"
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 8,
                  color: "var(--accent, #2D5A3D)",
                  textDecoration: "none",
                  fontSize: 15,
                  fontWeight: 500,
                }}
              >
                💬 {t("payment_success.next_support")} →
              </Link>
            </div>
          </CardContent>
        </Card>

        {/* Back home */}
        <div style={{ textAlign: "center" }}>
          <Link to="/">
            <Button variant="outline">
              {t("payment_success.back_home")}
            </Button>
          </Link>
        </div>
      </div>

      <style>{`
        @keyframes spin {
          to { transform: rotate(360deg); }
        }
      `}</style>
    </div>
  );
}

export default PaymentSuccess;
