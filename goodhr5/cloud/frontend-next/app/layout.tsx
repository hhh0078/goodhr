/** 本文件负责 GoodHR 新版前端的根布局和页面元信息。 */
import Script from "next/script";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import InviteCapture from "@/components/InviteCapture";
import StructuredData from "@/components/StructuredData";
import { CORE_SEO_KEYWORDS, GITHUB_REPOSITORY_URL, SITE_URL } from "@/lib/seo";
import Providers from "./providers";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  applicationName: "GoodHR",
  title: { default: "GoodHR AI招聘助手 - 自动筛选简历、自动打招呼与招聘消息回复", template: "%s | GoodHR AI招聘助手" },
  description: "GoodHR 是完全开源的 HR 和猎头招聘自动化工具，覆盖招聘平台自动筛选、AI筛选简历、自动打招呼、AI打招呼、自动回复消息和简历下载管理。",
  keywords: CORE_SEO_KEYWORDS,
  authors: [{ name: "GoodHR", url: SITE_URL }],
  creator: "GoodHR",
  publisher: "GoodHR",
  icons: {
    icon: [
      { url: "/favicon.ico" },
      { url: "/icon.png", type: "image/png", sizes: "192x192" },
    ],
    apple: [{ url: "/apple-icon.png", sizes: "180x180", type: "image/png" }],
  },
  category: "招聘软件",
  referrer: "origin-when-cross-origin",
  alternates: { canonical: "/" },
  openGraph: { type: "website", locale: "zh_CN", siteName: "GoodHR", url: SITE_URL, title: "GoodHR AI招聘助手", description: "完全开源的招聘自动化工具，支持自动筛选候选人、自动打招呼、AI招聘消息回复和简历管理。" },
  twitter: { card: "summary_large_image", title: "GoodHR AI招聘助手", description: "面向 HR 和猎头的完全开源招聘自动化工具。" },
  robots: { index: true, follow: true, googleBot: { index: true, follow: true, "max-image-preview": "large", "max-snippet": -1, "max-video-preview": -1 } },
};

type RootLayoutProps = { children: ReactNode };

/** RootLayout 输出全站 HTML 结构并挂载统一主题。 */
export default function RootLayout({ children }: RootLayoutProps) {
  return (
    <html lang="zh-CN">
      <body>
        <StructuredData data={[
          { "@context": "https://schema.org", "@type": "Organization", name: "GoodHR", url: SITE_URL, email: "1224299352@qq.com", telephone: "+86-17607080935", sameAs: [GITHUB_REPOSITORY_URL], description: "面向 HR、招聘团队和猎头顾问的完全开源 AI 招聘自动化工具。" },
          { "@context": "https://schema.org", "@type": "WebSite", name: "GoodHR", url: SITE_URL, inLanguage: "zh-CN", sameAs: [GITHUB_REPOSITORY_URL], description: "完全开源的招聘平台自动筛选、自动打招呼、AI自动回复和简历管理工具。" },
        ]} />
				<Providers><InviteCapture />{children}</Providers>
				<Script id="baidu-analytics" strategy="afterInteractive">{`var _hmt = _hmt || [];
(function() {
  var hm = document.createElement("script");
  hm.src = "https://hm.baidu.com/hm.js?089e2d5bc4ddf06b7bf5ab053d5b6fe1";
  var s = document.getElementsByTagName("script")[0];
  s.parentNode.insertBefore(hm, s);
})();`}</Script>
			</body>
    </html>
  );
}
