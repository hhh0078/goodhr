/** 本文件负责服务端读取永久公开的候选人推荐报告，并禁止搜索引擎收录。 */

import type { Metadata } from "next";
import RecommendationReportClient from "./RecommendationReportClient";
import styles from "./recommendation.module.css";
import { normalizeRecommendation } from "@/lib/recommendation";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "候选人推荐报告 - GoodHR",
  robots: { index: false, follow: false, nocache: true },
  referrer: "no-referrer",
};

/** RecommendationPage 返回一份无需登录、移动端和打印友好的推荐报告。 */
export default async function RecommendationPage({ params }: { params: Promise<{ publicId: string }> }) {
  const { publicId } = await params;
  const response = await loadRecommendation(publicId);
  if (!response) {
    return <main className={styles.errorPage}><section><h1>这份推荐报告没有找到</h1><p>可能链接写错了，也可能 HR 已经撤销公开。你可以回去请他重新发一份。</p></section></main>;
  }
  return <RecommendationReportClient recommendation={response} apiBaseURL={browserCloudBaseURL()} />;
}

/** loadRecommendation 从云端读取公开报告，接口失败时返回空值交给页面友好提示。 */
async function loadRecommendation(publicID: string) {
  const id = encodeURIComponent(String(publicID || "").trim());
  if (!id) return null;
  try {
    const response = await fetch(`${serverCloudBaseURL()}/api/public/recommendations/${id}`, { cache: "no-store" });
    if (!response.ok) return null;
    const body = await response.json() as { recommendation?: unknown };
    const recommendation = normalizeRecommendation(body.recommendation);
    return recommendation.publicID ? recommendation : null;
  } catch {
    return null;
  }
}

/** serverCloudBaseURL 返回 Next 服务端访问云端 Go 后端的地址。 */
function serverCloudBaseURL() {
  const fallback = process.env.NODE_ENV === "production" ? "https://goodhr5.58it.cn" : "http://127.0.0.1:8084";
  return (process.env.CLOUD_API_BASE || process.env.NEXT_PUBLIC_CLOUD_API_BASE || fallback).replace(/\/$/, "");
}

/** browserCloudBaseURL 返回浏览器懒加载公开沟通记录时使用的地址。 */
function browserCloudBaseURL() {
  const fallback = process.env.NODE_ENV === "production" ? "https://goodhr5.58it.cn" : "http://127.0.0.1:8084";
  return (process.env.NEXT_PUBLIC_CLOUD_API_BASE || fallback).replace(/\/$/, "");
}
