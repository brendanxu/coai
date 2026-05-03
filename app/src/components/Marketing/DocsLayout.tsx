import { ReactNode, useEffect, useState } from "react";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";

export type DocsTocItem = {
  id: string;
  label: string;
  level?: 2 | 3;
};

/**
 * Layout for /docs and similar dev-facing technical reading pages.
 *
 * Why a dedicated component (vs reusing LegalLayout):
 * - Wider reading column (940px) so code blocks have room
 * - Sticky TOC sidebar on desktop — devs scan, they don't read top-to-bottom
 * - Code-block typography hard-coded for monospace + dark contrast
 *   (without forcing the whole prose into a different font)
 *
 * Single comprehensive page beats N split pages at this scale:
 *   - SEO: one URL, more inbound links land
 *   - Maintenance: one file to edit
 *   - UX: cmd-F works
 */
export default function DocsLayout({
  title,
  eyebrow,
  updated,
  toc,
  children,
}: {
  title: string;
  eyebrow?: string;
  updated?: string;
  toc?: DocsTocItem[];
  children: ReactNode;
}) {
  const [activeId, setActiveId] = useState<string>("");

  useEffect(() => {
    if (!toc || toc.length === 0) return;

    const observer = new IntersectionObserver(
      (entries) => {
        const visible = entries.filter((e) => e.isIntersecting);
        if (visible.length > 0) {
          setActiveId(visible[0].target.id);
        }
      },
      { rootMargin: "-80px 0px -60% 0px" },
    );

    toc.forEach(({ id }) => {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    });

    return () => observer.disconnect();
  }, [toc]);

  return (
    <>
      <Header />
      <main className="flex-1 overflow-y-auto">
        <div className="mx-auto px-6 py-12 md:py-16" style={{ maxWidth: "1200px" }}>
          <header className="mb-8 md:mb-12 pb-6 border-b border-border-soft">
            {eyebrow && (
              <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
                {eyebrow}
              </p>
            )}
            <h1 className="font-display text-3xl md:text-5xl tracking-tight leading-tight mb-3">
              {title}
            </h1>
            {updated && (
              <p className="text-sm text-muted-foreground">最后更新 · {updated}</p>
            )}
          </header>

          <div className="grid grid-cols-1 md:grid-cols-[1fr_220px] gap-12">
            {/* Content */}
            <article className="prose-docs text-secondary-foreground/90 leading-relaxed min-w-0">
              {children}
            </article>

            {/* Sticky TOC — desktop only */}
            {toc && toc.length > 0 && (
              <aside className="hidden md:block">
                <div className="sticky top-20">
                  <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
                    本页内容
                  </p>
                  <nav className="text-sm space-y-1.5 border-l border-border-soft">
                    {toc.map((item) => (
                      <a
                        key={item.id}
                        href={`#${item.id}`}
                        className={`block pl-3 -ml-px border-l transition-colors ${
                          item.level === 3 ? "pl-6 text-xs" : ""
                        } ${
                          activeId === item.id
                            ? "border-primary text-foreground"
                            : "border-transparent text-muted-foreground hover:text-foreground"
                        }`}
                      >
                        {item.label}
                      </a>
                    ))}
                  </nav>
                </div>
              </aside>
            )}
          </div>
        </div>
        <Footer />
      </main>

      <style>{`
        .prose-docs h2 {
          font-family: var(--font-display);
          font-size: 1.75rem;
          font-weight: 600;
          margin-top: 3rem;
          margin-bottom: 1rem;
          letter-spacing: -0.01em;
          scroll-margin-top: 80px;
        }
        .prose-docs h2:first-of-type { margin-top: 0; }
        .prose-docs h3 {
          font-family: var(--font-display);
          font-size: 1.25rem;
          font-weight: 600;
          margin-top: 2rem;
          margin-bottom: 0.5rem;
          scroll-margin-top: 80px;
        }
        .prose-docs h4 {
          font-weight: 600;
          margin-top: 1.5rem;
          margin-bottom: 0.5rem;
          font-size: 1rem;
        }
        .prose-docs p {
          margin-bottom: 1rem;
          line-height: 1.7;
        }
        .prose-docs ul, .prose-docs ol {
          margin-bottom: 1rem;
          padding-left: 1.5rem;
        }
        .prose-docs ul { list-style: disc; }
        .prose-docs ol { list-style: decimal; }
        .prose-docs li {
          margin-bottom: 0.4rem;
          line-height: 1.65;
        }
        .prose-docs strong {
          font-weight: 600;
          color: hsl(var(--foreground));
        }
        .prose-docs a {
          color: hsl(var(--primary));
          text-decoration: underline;
          text-underline-offset: 3px;
        }
        .prose-docs a:hover { color: hsl(var(--primary-deep)); }
        .prose-docs code {
          font-family: var(--font-mono);
          font-size: 0.85em;
          background: hsl(var(--muted));
          padding: 0.15rem 0.4rem;
          border-radius: 4px;
          color: hsl(var(--foreground));
        }
        .prose-docs pre {
          background: #1e1e1e;
          color: #e0e0e0;
          padding: 1.1rem 1.3rem;
          border-radius: 8px;
          overflow-x: auto;
          margin: 1.25rem 0;
          font-size: 0.85rem;
          line-height: 1.55;
        }
        .prose-docs pre code {
          background: transparent;
          color: inherit;
          padding: 0;
          font-size: inherit;
        }
        .prose-docs table {
          width: 100%;
          border-collapse: collapse;
          margin: 1.5rem 0;
          font-size: 0.9rem;
        }
        .prose-docs th, .prose-docs td {
          border: 1px solid hsl(var(--border-soft));
          padding: 0.6rem 0.85rem;
          text-align: left;
        }
        .prose-docs th {
          background: hsl(var(--muted));
          font-weight: 600;
        }
        .prose-docs blockquote {
          border-left: 3px solid hsl(var(--primary));
          padding-left: 1rem;
          color: hsl(var(--muted-foreground));
          margin: 1.5rem 0;
        }
        .prose-docs hr {
          border: 0;
          border-top: 1px solid hsl(var(--border-soft));
          margin: 2.5rem 0;
        }
        .prose-docs .callout {
          background: hsl(var(--muted));
          border-left: 3px solid hsl(var(--primary));
          padding: 1rem 1.2rem;
          border-radius: 0 6px 6px 0;
          margin: 1.5rem 0;
        }
        .prose-docs .callout p:last-child { margin-bottom: 0; }
      `}</style>
    </>
  );
}
