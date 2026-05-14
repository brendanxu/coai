// Checkout.tsx — /checkout?plan_code=token-99
//
// Auth-gated mid-step payment confirmation page. Shows payment method
// selection + order summary. Calls hupijiao (CNY) or LemonSqueezy (USD)
// checkout endpoints and redirects / shows QR inline.

import { useState } from "react";
import { useSearchParams, Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import axios from "axios";
import { toast } from "sonner";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Label } from "@/components/ui/label.tsx";

type PayMethod = "wechat" | "alipay" | "card";

interface MethodCardProps {
  id: PayMethod;
  selected: PayMethod;
  onSelect: (m: PayMethod) => void;
  flag: string;
  label: string;
  price: string;
  desc: string;
  chip?: string;
}

function MethodCard({
  id,
  selected,
  onSelect,
  flag,
  label,
  price,
  desc,
  chip,
}: MethodCardProps) {
  const active = selected === id;
  return (
    <div
      role="radio"
      aria-checked={active}
      tabIndex={0}
      onClick={() => onSelect(id)}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") onSelect(id);
      }}
      style={{
        border: `2px solid ${active ? "var(--accent)" : "var(--border, #e2e8f0)"}`,
        borderRadius: 10,
        padding: "14px 16px",
        cursor: "pointer",
        background: active ? "rgba(45,90,61,0.05)" : "transparent",
        transition: "border-color 0.15s",
        display: "flex",
        alignItems: "flex-start",
        gap: 12,
        outline: "none",
      }}
    >
      {/* radio dot */}
      <div
        style={{
          marginTop: 3,
          width: 18,
          height: 18,
          borderRadius: "50%",
          border: `2px solid ${active ? "var(--accent)" : "#999"}`,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
        }}
      >
        {active && (
          <div
            style={{
              width: 9,
              height: 9,
              borderRadius: "50%",
              background: "var(--accent)",
            }}
          />
        )}
      </div>
      <div style={{ flex: 1 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <span style={{ fontSize: 15, fontWeight: 600 }}>
            {flag} {label}
          </span>
          {chip && (
            <span
              style={{
                fontSize: 11,
                background: "var(--accent)",
                color: "#fff",
                borderRadius: 4,
                padding: "1px 6px",
                fontFamily: "Manrope, sans-serif",
              }}
            >
              {chip}
            </span>
          )}
        </div>
        <div
          style={{
            fontSize: 13,
            color: "var(--ink, #2B2118)",
            opacity: 0.65,
            marginTop: 2,
          }}
        >
          {desc}
        </div>
      </div>
      <div
        style={{
          fontSize: 14,
          fontWeight: 700,
          color: "var(--accent)",
          whiteSpace: "nowrap",
          flexShrink: 0,
        }}
      >
        {price}
      </div>
    </div>
  );
}

function Checkout() {
  const { t } = useTranslation();
  const [params] = useSearchParams();
  const planCode = params.get("plan_code") ?? "token-99";

  const [method, setMethod] = useState<PayMethod>("wechat");
  const [remarks, setRemarks] = useState("");
  const [agreeTerms, setAgreeTerms] = useState(false);
  const [agreePrivacy, setAgreePrivacy] = useState(false);
  const [loading, setLoading] = useState(false);
  const [qrUrl, setQrUrl] = useState<string | null>(null);

  const canContinue = agreeTerms && agreePrivacy && !loading;
  const isCny = method === "wechat" || method === "alipay";

  const handleContinue = async () => {
    if (!canContinue) return;
    setLoading(true);
    try {
      if (isCny) {
        const r = await axios.post<{
          success: boolean;
          data?: { code_url?: string; h5_url?: string };
        }>(`/payment/hupijiao/checkout?plan_code=${planCode}`);
        const data = r.data?.data;
        if (data?.h5_url) {
          window.location.href = data.h5_url;
          return;
        }
        if (data?.code_url) {
          setQrUrl(data.code_url);
          setLoading(false);
          return;
        }
        throw new Error("No payment URL returned");
      } else {
        const r = await axios.get<{
          success: boolean;
          data?: { url?: string };
        }>(`/payment/checkout?plan_code=${planCode}`);
        const url = r.data?.data?.url;
        if (url) {
          window.location.href = url;
          return;
        }
        throw new Error("No checkout URL returned");
      }
    } catch (_e) {
      toast.error(t("checkout.error_checkout_failed"));
      setLoading(false);
    }
  };

  return (
    <div
      style={{
        minHeight: "100vh",
        background: "var(--bg, #F4EFE5)",
        fontFamily: "Manrope, sans-serif",
        padding: "32px 16px",
      }}
    >
      <div
        style={{
          maxWidth: 900,
          margin: "0 auto",
        }}
      >
        <h1
          style={{
            fontFamily: "Fraunces, serif",
            fontSize: 26,
            fontWeight: 700,
            color: "var(--ink, #2B2118)",
            marginBottom: 28,
          }}
        >
          {t("checkout.title")}
        </h1>

        <div
          style={{
            display: "flex",
            gap: 24,
            flexDirection: "row",
            alignItems: "flex-start",
          }}
        >
          {/* Left column */}
          <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 20 }}>
            {/* Payment method */}
            <Card>
              <CardHeader>
                <CardTitle style={{ fontSize: 16 }}>
                  {t("checkout.method_title")}
                </CardTitle>
              </CardHeader>
              <CardContent>
                <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                  <MethodCard
                    id="wechat"
                    selected={method}
                    onSelect={setMethod}
                    flag="🇨🇳"
                    label={t("checkout.method_wechat")}
                    price="¥99"
                    desc={t("checkout.method_wechat_desc")}
                    chip={t("checkout.method_recommended")}
                  />
                  <MethodCard
                    id="alipay"
                    selected={method}
                    onSelect={setMethod}
                    flag="🇨🇳"
                    label={t("checkout.method_alipay")}
                    price="¥99"
                    desc={t("checkout.method_alipay_desc")}
                  />
                  <MethodCard
                    id="card"
                    selected={method}
                    onSelect={setMethod}
                    flag="🌏"
                    label={t("checkout.method_card")}
                    price="~$14"
                    desc={t("checkout.method_card_desc")}
                  />
                </div>
              </CardContent>
            </Card>

            {/* Remarks */}
            <Card>
              <CardContent style={{ paddingTop: 20 }}>
                <Label htmlFor="checkout-remarks" style={{ marginBottom: 6, display: "block" }}>
                  {t("checkout.remarks_label")}
                </Label>
                <textarea
                  id="checkout-remarks"
                  rows={3}
                  value={remarks}
                  onChange={(e) => setRemarks(e.target.value)}
                  placeholder={t("checkout.remarks_placeholder")}
                  style={{
                    width: "100%",
                    borderRadius: 8,
                    border: "1px solid var(--border, #e2e8f0)",
                    padding: "8px 12px",
                    fontSize: 14,
                    fontFamily: "Manrope, sans-serif",
                    background: "transparent",
                    resize: "vertical",
                    outline: "none",
                    color: "var(--ink, #2B2118)",
                  }}
                />
              </CardContent>
            </Card>

            {/* Agreements */}
            <Card>
              <CardContent style={{ paddingTop: 20, display: "flex", flexDirection: "column", gap: 12 }}>
                <label
                  style={{ display: "flex", alignItems: "center", gap: 10, cursor: "pointer", fontSize: 14 }}
                >
                  <input
                    type="checkbox"
                    checked={agreeTerms}
                    onChange={(e) => setAgreeTerms(e.target.checked)}
                    style={{ width: 16, height: 16, accentColor: "var(--accent)" }}
                  />
                  <span>
                    {t("checkout.agree_terms")}{" "}
                    <Link
                      to="/terms"
                      style={{ color: "var(--accent)", textDecoration: "underline" }}
                    >
                      《{t("checkout.agree_terms_link")}》
                    </Link>
                  </span>
                </label>
                <label
                  style={{ display: "flex", alignItems: "center", gap: 10, cursor: "pointer", fontSize: 14 }}
                >
                  <input
                    type="checkbox"
                    checked={agreePrivacy}
                    onChange={(e) => setAgreePrivacy(e.target.checked)}
                    style={{ width: 16, height: 16, accentColor: "var(--accent)" }}
                  />
                  <span>
                    {t("checkout.agree_privacy")}{" "}
                    <Link
                      to="/privacy"
                      style={{ color: "var(--accent)", textDecoration: "underline" }}
                    >
                      《{t("checkout.agree_privacy_link")}》
                    </Link>
                  </span>
                </label>
              </CardContent>
            </Card>
          </div>

          {/* Right sticky summary */}
          <div style={{ width: 280, flexShrink: 0, position: "sticky", top: 24 }}>
            <Card>
              <CardHeader>
                <CardTitle style={{ fontSize: 16 }}>
                  {t("checkout.summary_title")}
                </CardTitle>
              </CardHeader>
              <CardContent style={{ display: "flex", flexDirection: "column", gap: 14 }}>
                <div>
                  <div style={{ fontWeight: 600, fontSize: 15 }}>
                    {t("checkout.summary_plan")}
                  </div>
                  <div style={{ fontSize: 13, opacity: 0.65, marginTop: 3 }}>
                    {t("checkout.summary_credits")}
                  </div>
                </div>

                <div
                  style={{
                    borderTop: "1px solid var(--border, #e2e8f0)",
                    paddingTop: 12,
                  }}
                >
                  <div style={{ fontSize: 22, fontWeight: 700, color: "var(--accent)" }}>
                    {isCny
                      ? t("checkout.summary_price_cny")
                      : t("checkout.summary_price_usd")}
                  </div>
                  <div style={{ fontSize: 12, opacity: 0.55, marginTop: 2 }}>
                    {t("checkout.summary_tax")}
                  </div>
                </div>

                <Button
                  onClick={handleContinue}
                  disabled={!canContinue}
                  style={{
                    background: canContinue ? "var(--accent)" : undefined,
                    color: canContinue ? "#fff" : undefined,
                    width: "100%",
                    fontSize: 15,
                    fontWeight: 600,
                  }}
                >
                  {loading ? "..." : t("checkout.continue")}
                </Button>

                <div style={{ fontSize: 11, opacity: 0.55, textAlign: "center" }}>
                  {t("checkout.continue_hint")}
                </div>

                {/* QR code display */}
                {qrUrl && (
                  <div style={{ textAlign: "center", marginTop: 8 }}>
                    <div style={{ fontSize: 13, marginBottom: 8, fontWeight: 600 }}>
                      扫码支付
                    </div>
                    <img
                      src={`https://api.qrserver.com/v1/create-qr-code/?size=160x160&data=${encodeURIComponent(qrUrl)}`}
                      alt="QR code"
                      style={{ width: 160, height: 160, borderRadius: 8 }}
                    />
                    <div style={{ fontSize: 11, opacity: 0.55, marginTop: 6 }}>
                      使用微信或支付宝扫码
                    </div>
                  </div>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>
    </div>
  );
}

export default Checkout;
