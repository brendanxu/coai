import { ReactNode } from "react";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";

/**
 * Shared layout for Privacy / Terms / About / FAQ marketing pages.
 *
 * Why a dedicated component (vs each page wiring its own Header/Footer):
 * - Locks in a consistent reading width (--max-content prose at 720px,
 *   tighter than the 1120px marketing default for legal copy reading)
 * - Standard typography rhythm: h1 / h2 / p / ul styling matches v0.7
 *   editorial design without each page rewriting it
 * - Single place to add jump-to-section TOC if we need it later
 *
 * Usage:
 *   <LegalLayout title="..." updated="2026-05-03">
 *     <h2>Section</h2>
 *     <p>...</p>
 *   </LegalLayout>
 */
export default function LegalLayout({
  title,
  eyebrow,
  updated,
  children,
}: {
  title: string;
  eyebrow?: string;
  updated?: string;
  children: ReactNode;
}) {
  return (
    <>
      <Header />
      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto px-6 py-16 md:py-24" style={{ maxWidth: "780px" }}>
          <header className="mb-10 md:mb-14 pb-8 border-b border-border-soft">
            {eyebrow && (
              <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
                {eyebrow}
              </p>
            )}
            <h1 className="font-display text-3xl md:text-5xl tracking-tight leading-tight mb-4">
              {title}
            </h1>
            {updated && (
              <p className="text-sm text-muted-foreground">最后更新 · {updated}</p>
            )}
          </header>

          {/* Prose — semantic markup styled by tailwind/typography tokens */}
          <article className="prose-legal text-secondary-foreground/90 leading-relaxed">
            {children}
          </article>
        </div>
        <Footer />
      </main>

      {/* Inline prose styles — keep proximity to component since v0.7 doesn't
          ship a typography plugin and we don't want to commit to one yet. */}
      <style>{`
        .prose-legal h2 {
          font-family: var(--font-display);
          font-size: 1.5rem;
          font-weight: 600;
          margin-top: 2.5rem;
          margin-bottom: 0.75rem;
          letter-spacing: -0.01em;
        }
        .prose-legal h3 {
          font-family: var(--font-display);
          font-size: 1.125rem;
          font-weight: 600;
          margin-top: 1.75rem;
          margin-bottom: 0.5rem;
        }
        .prose-legal p {
          margin-bottom: 1rem;
          line-height: 1.7;
        }
        .prose-legal ul, .prose-legal ol {
          margin-bottom: 1rem;
          padding-left: 1.5rem;
        }
        .prose-legal ul {
          list-style: disc;
        }
        .prose-legal ol {
          list-style: decimal;
        }
        .prose-legal li {
          margin-bottom: 0.4rem;
          line-height: 1.65;
        }
        .prose-legal strong {
          font-weight: 600;
          color: hsl(var(--foreground));
        }
        .prose-legal a {
          color: hsl(var(--primary));
          text-decoration: underline;
          text-underline-offset: 3px;
        }
        .prose-legal a:hover {
          color: hsl(var(--primary-deep));
        }
        .prose-legal code {
          font-family: var(--font-mono);
          font-size: 0.85em;
          background: hsl(var(--muted));
          padding: 0.15rem 0.4rem;
          border-radius: 4px;
        }
        .prose-legal blockquote {
          border-left: 3px solid hsl(var(--primary));
          padding-left: 1rem;
          color: hsl(var(--muted-foreground));
          margin: 1.5rem 0;
          font-style: italic;
        }
        .prose-legal hr {
          border: 0;
          border-top: 1px solid hsl(var(--border-soft));
          margin: 2.5rem 0;
        }
      `}</style>
    </>
  );
}
