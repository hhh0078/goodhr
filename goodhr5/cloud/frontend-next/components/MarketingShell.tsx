/** 本文件负责官网内页的统一页头、内容区和页脚结构。 */

import "@/components/public-site.css";
import type { ReactNode } from "react";
import SiteFooter from "./SiteFooter";
import SiteHeader from "./SiteHeader";
import PublicIllustration, { type PublicIllustrationKind } from "./PublicIllustration";
import PlatformLogo from "./admin/PlatformLogo";

type MarketingShellProps = {
  eyebrow: string;
  title: string;
  description: string;
  children: ReactNode;
  showIntro?: boolean;
  platformID?: string;
};

/** MarketingShell 输出官网内页统一布局。 */
export default function MarketingShell({
  eyebrow,
  title,
  description,
  children,
  showIntro = true,
  platformID,
}: MarketingShellProps) {
  const illustration: PublicIllustrationKind = eyebrow.includes("招聘") ? "hero" : eyebrow.includes("工具") ? "analysis" : eyebrow.includes("联系") ? "greeting" : eyebrow.includes("文章") ? "analysis" : "hero";
  return (
    <div className="public-marketing public-marketingshell-1">
      <SiteHeader />
      <main>
        {showIntro ? (
          <section className="public-marketingshell-4">
            <div className="public-container public-interior-intro">
              <div>
              <div className="public-interior-eyebrow">
                {platformID ? <span className="public-platform-logo" aria-hidden="true"><PlatformLogo platformID={platformID} size={42} /></span> : null}
                <p className="public-text public-marketingshell-6">{eyebrow}</p>
              </div>
              <h1 className="public-text public-marketingshell-7">{title}</h1>
              <p className="public-text public-marketingshell-8">{description}</p>
              </div>
              <PublicIllustration kind={illustration} className="public-interior-art" priority />
            </div>
          </section>
        ) : null}
        {children}
      </main>
      <SiteFooter />
    </div>
  );
}
