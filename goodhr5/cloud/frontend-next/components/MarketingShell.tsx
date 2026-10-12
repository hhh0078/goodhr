/** 本文件负责官网内页的统一页头、内容区和页脚结构。 */

import "@/components/public-site.css";
import type { ReactNode } from "react";
import SiteFooter from "./SiteFooter";
import SiteHeader from "./SiteHeader";

type MarketingShellProps = {
  eyebrow: string;
  title: string;
  description: string;
  children: ReactNode;
  showIntro?: boolean;
};

/** MarketingShell 输出官网内页统一布局。 */
export default function MarketingShell({
  eyebrow,
  title,
  description,
  children,
  showIntro = true,
}: MarketingShellProps) {
  return (
    <div className="public-marketing public-marketingshell-1">
      <SiteHeader />
      <main>
        {showIntro ? (
          <section className="public-marketingshell-4">
            <div className="public-container">
              <p className="public-text public-marketingshell-6">{eyebrow}</p>
              <h1 className="public-text public-marketingshell-7">{title}</h1>
              <p className="public-text public-marketingshell-8">{description}</p>
            </div>
          </section>
        ) : null}
        {children}
      </main>
      <SiteFooter />
    </div>
  );
}
