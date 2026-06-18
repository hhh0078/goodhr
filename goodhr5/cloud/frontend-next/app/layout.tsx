/** 本文件负责 GoodHR 新版前端的根布局和页面元信息。 */
import type { Metadata } from "next";
import type { ReactNode } from "react";
import InviteCapture from "@/components/InviteCapture";
import Providers from "./providers";
import "./globals.css";

export const metadata: Metadata = {
  metadataBase: new URL(process.env.NEXT_PUBLIC_SITE_URL || "https://goodhr5.58it.cn"),
  title: { default: "GoodHR - AI 招聘助手", template: "%s | GoodHR" },
  description: "GoodHR 招聘自动化工具，支持候选人筛选、详情分析、自动打招呼和任务管理。",
  keywords: ["AI 招聘", "招聘自动化", "候选人筛选", "自动打招呼", "GoodHR"],
  alternates: { canonical: "/" },
  openGraph: { type: "website", locale: "zh_CN", siteName: "GoodHR", title: "GoodHR - AI 招聘助手", description: "把重复招聘交给 GoodHR，把时间留给人。" },
  robots: { index: true, follow: true },
};

type RootLayoutProps = { children: ReactNode };

/** RootLayout 输出全站 HTML 结构并挂载统一主题。 */
export default function RootLayout({ children }: RootLayoutProps) {
  return (
    <html lang="zh-CN">
      <body>
        <Providers><InviteCapture />{children}</Providers>
      </body>
    </html>
  );
}
