/** 本文件负责集中维护官网搜索关键词、页面元数据和站点地址。 */

import type { Metadata } from "next";

export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || "https://goodhr.58it.cn").replace(/\/$/, "");

export const CORE_SEO_KEYWORDS = [
  "GoodHR", "招聘自动化工具", "AI招聘助手", "HR提效工具", "猎头招聘工具",
  "AI筛选简历", "自动打招呼", "招聘消息自动回复", "简历管理系统", "猎头人才库",
  "BOSS直聘招聘工具", "猎聘招聘工具", "智联招聘工具", "前程无忧简历导入",
];

type PageMetadataOptions = {
  title: string;
  description: string;
  path: string;
  keywords?: string[];
};

/** createPageMetadata 生成公开页面统一的搜索和分享元数据。 */
export function createPageMetadata({ title, description, path, keywords = [] }: PageMetadataOptions): Metadata {
  const canonical = path === "/" ? SITE_URL : `${SITE_URL}${path}`;
  const mergedKeywords = [...new Set(keywords.length ? keywords : CORE_SEO_KEYWORDS)];
  return {
    title,
    description,
    keywords: mergedKeywords,
    alternates: { canonical },
    openGraph: { type: "website", locale: "zh_CN", siteName: "GoodHR", url: canonical, title, description },
    twitter: { card: "summary_large_image", title, description },
    robots: { index: true, follow: true, googleBot: { index: true, follow: true, "max-image-preview": "large", "max-snippet": -1, "max-video-preview": -1 } },
  };
}

/** absoluteURL 将站内路径转换成完整网址。 */
export function absoluteURL(path: string) {
  return path === "/" ? SITE_URL : `${SITE_URL}${path.startsWith("/") ? path : `/${path}`}`;
}
