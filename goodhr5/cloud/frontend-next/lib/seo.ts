/** 本文件负责集中维护官网搜索关键词、页面元数据和站点地址。 */

import type { Metadata } from "next";

export const SITE_URL = (process.env.NEXT_PUBLIC_SITE_URL || "https://goodhr5.58it.cn").replace(/\/$/, "");

export const RECRUITMENT_PLATFORMS = ["BOSS直聘", "猎聘", "智联招聘", "前程无忧51job", "拉勾招聘", "58同城招聘", "店长直聘", "赶集直招", "鱼泡直聘", "脉脉招聘", "实习僧", "牛客招聘", "应届生求职", "国聘", "丁香人才", "最佳东方", "建筑英才网", "中国人才热线", "卓博人才网", "一览英才网"];

export const RECRUITMENT_AUTOMATION_ACTIONS = ["自动化工具", "自动打招呼", "AI自动打招呼", "自动筛选", "AI筛选", "自动回复", "AI自动回复", "简历下载"];

export const PLATFORM_AUTOMATION_KEYWORDS = RECRUITMENT_PLATFORMS.flatMap((platform) => RECRUITMENT_AUTOMATION_ACTIONS.map((action) => `${platform}${action}`));

export const CORE_SEO_KEYWORDS = [
  "AI招聘软件", "AI招聘助手", "招聘自动化工具", "HR招聘软件", "猎头招聘工具", "自动筛选简历", "AI筛选简历",
  "自动打招呼", "AI自动打招呼", "招聘自动回复", "AI自动回复消息", "招聘平台自动化", "招聘简历下载",
  "BOSS直聘自动化", "BOSS自动打招呼", "BOSS AI打招呼", "BOSS自动筛选", "BOSS AI筛选", "BOSS自动回复",
  "BOSS简历下载", "猎聘自动化工具", "猎聘自动打招呼", "猎聘AI筛选", "猎聘自动回复", "猎聘简历下载",
  "智联招聘自动化", "智联自动打招呼", "智联AI筛选", "智联自动回复", "智联简历下载", "前程无忧自动化",
  "51job自动打招呼", "拉勾招聘自动化", "58同城招聘自动化", "店长直聘自动打招呼", "赶集直招自动化",
  "鱼泡直聘自动化", "脉脉招聘自动化", "招聘机器人", "候选人筛选", "人才筛选", "招聘效率工具", "GoodHR",
  ...PLATFORM_AUTOMATION_KEYWORDS,
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
  const mergedKeywords = [...new Set([...keywords, ...CORE_SEO_KEYWORDS])];
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
