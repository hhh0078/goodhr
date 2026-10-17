/** 本文件负责 GoodHR 新版前端的根布局和页面元信息。 */
import { Suspense } from "react";
import type { Metadata } from "next";
import type { ReactNode } from "react";
import { AntdRegistry } from "@ant-design/nextjs-registry";
import InviteCapture from "@/components/InviteCapture";
import StructuredData from "@/components/StructuredData";
import GoodHRAnalytics from "@/components/GoodHRAnalytics";
import { CORE_SEO_KEYWORDS, SITE_URL } from "@/lib/seo";
import Providers from "./providers";
import "./globals.css";
import "./admin.css";

export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  applicationName: "GoodHR",
  title: {
    default: "GoodHR AI招聘助手 - 简历库、猎头推荐与HR招聘流程",
    template: "%s | GoodHR AI招聘助手",
  },
  description:
    "GoodHR 是面向 HR 和猎头的招聘系统，覆盖招聘平台自动筛选与沟通、结构化简历库、猎头推荐记录和企业 HR 招聘流程。",
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
  openGraph: {
    type: "website",
    locale: "zh_CN",
    siteName: "GoodHR",
    url: SITE_URL,
    title: "GoodHR AI招聘助手",
    description:
      "面向 HR 和猎头的招聘系统，支持招聘平台自动化、结构化简历库、猎头推荐流程和企业 HR 招聘流程。",
  },
  twitter: {
    card: "summary_large_image",
    title: "GoodHR AI招聘助手",
    description: "面向 HR 和猎头的招聘自动化工具。",
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      "max-image-preview": "large",
      "max-snippet": -1,
      "max-video-preview": -1,
    },
  },
};

type RootLayoutProps = { children: ReactNode };

/** RootLayout 输出全站 HTML 结构并挂载统一主题。 */
export default function RootLayout({ children }: RootLayoutProps) {
  return (
    <html lang="zh-CN">
      <body>
        <StructuredData
          data={[
            {
              "@context": "https://schema.org",
              "@type": "Organization",
              name: "GoodHR",
              url: SITE_URL,
              email: "1224299352@qq.com",
              telephone: "+86-17607080935",
              description:
                "面向 HR、招聘团队和猎头顾问的招聘系统，连接招聘平台自动化、简历库、猎头推荐和企业招聘流程。",
            },
            {
              "@context": "https://schema.org",
              "@type": "WebSite",
              name: "GoodHR",
              url: SITE_URL,
              inLanguage: "zh-CN",
              description:
                "提供招聘平台自动筛选与沟通、结构化简历库、猎头推荐流程和企业 HR 招聘流程的招聘系统。",
            },
          ]}
        />
        <AntdRegistry>
          <Providers>
            <InviteCapture />
            {children}
          </Providers>
        </AntdRegistry>
        <Suspense fallback={null}><GoodHRAnalytics /></Suspense>
      </body>
    </html>
  );
}
