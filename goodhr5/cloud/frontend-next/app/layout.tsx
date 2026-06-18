/** 本文件负责 GoodHR 新版前端的根布局和页面元信息。 */
import type { Metadata } from "next";
import type { ReactNode } from "react";
import Providers from "./providers";
import "./globals.css";

export const metadata: Metadata = {
  title: "GoodHR - AI 招聘助手",
  description: "自动筛选候选人、自动打招呼和智能跟进，让招聘工作更高效。",
};

type RootLayoutProps = { children: ReactNode };

/** RootLayout 输出全站 HTML 结构并挂载统一主题。 */
export default function RootLayout({ children }: RootLayoutProps) {
  return (
    <html lang="zh-CN">
      <body>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
