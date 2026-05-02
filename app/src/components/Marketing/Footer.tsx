import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";

/**
 * Marketing-side footer.
 *
 * Why this exists alongside the v0.7 minimal aesthetic: 中国大陆备案要求
 * (ICP / 公网安备) needs to be visible site-wide; commercial sites without
 * footer ICP filing get blocked. Also: Wechat / 朋友圈 share previews
 * benefit from the persistent contact info.
 *
 * 备案号是 placeholder ("待备案"); founder will UPDATE this once ICP
 * filing completes (1-3 weeks for first-time, only needed for commercial
 * Chinese-domain sites).
 *
 * Keep design minimal — neutral text, low-key links. The marketing page
 * is busy enough above; footer should breathe.
 */
export default function Footer() {
  const { t } = useTranslation();
  const year = new Date().getFullYear();

  return (
    <footer className="border-t border-border/50 mt-24">
      <div className="max-w-6xl mx-auto px-6 py-10">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-8 text-sm">
          {/* Brand */}
          <div className="space-y-2">
            <div className="flex items-center gap-2 font-display font-medium">
              <img src="/logo.svg" alt="greentokey" className="h-5 w-5" />
              <span>greentokey</span>
            </div>
            <p className="text-xs text-muted-foreground">
              {t(
                "footer.tagline",
                "民宿主的小红书运营官 · 大理环洱海首发",
              )}
            </p>
          </div>

          {/* Product */}
          <div className="space-y-2">
            <h4 className="font-medium text-xs uppercase tracking-wide text-muted-foreground">
              {t("footer.product", "产品")}
            </h4>
            <ul className="space-y-1.5">
              <li>
                <Link to="/token-plans" className="hover:text-foreground text-muted-foreground">
                  {t("nav.token", "Token 套餐")}
                </Link>
              </li>
              <li>
                <Link to="/services" className="hover:text-foreground text-muted-foreground">
                  {t("nav.services", "服务市场")}
                </Link>
              </li>
              <li>
                <Link to="/pool" className="hover:text-foreground text-muted-foreground">
                  {t("nav.pool", "模型池")}
                </Link>
              </li>
            </ul>
          </div>

          {/* Contact */}
          <div className="space-y-2">
            <h4 className="font-medium text-xs uppercase tracking-wide text-muted-foreground">
              {t("footer.contact", "联系")}
            </h4>
            <ul className="space-y-1.5 text-muted-foreground">
              <li>
                <Link to="/contact" className="hover:text-foreground">
                  {t("footer.book-demo", "预约 demo")}
                </Link>
              </li>
              <li className="text-xs">
                {t("footer.hours", "周一至周日 9:00-21:00")}
              </li>
            </ul>
          </div>

          {/* Legal */}
          <div className="space-y-2">
            <h4 className="font-medium text-xs uppercase tracking-wide text-muted-foreground">
              {t("footer.legal", "条款")}
            </h4>
            <ul className="space-y-1.5 text-muted-foreground">
              <li>
                <Link to="/privacy" className="hover:text-foreground">
                  {t("footer.privacy", "隐私政策")}
                </Link>
              </li>
              <li>
                <Link to="/terms" className="hover:text-foreground">
                  {t("footer.terms", "服务条款")}
                </Link>
              </li>
            </ul>
          </div>
        </div>

        <div className="mt-8 pt-6 border-t border-border/40 flex flex-col md:flex-row justify-between gap-2 text-xs text-muted-foreground">
          <div>
            © {year} greentokey · {t("footer.rights", "保留所有权利")}
          </div>
          <div className="space-x-3">
            {/* ICP placeholder — founder updates once filing completes. */}
            <span>{t("footer.icp", "ICP 备案: 待备案")}</span>
          </div>
        </div>
      </div>
    </footer>
  );
}
