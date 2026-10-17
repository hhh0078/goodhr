/** 本文件负责在 Next.js 服务端读取官网公开数据。 */

import { normalizeFirefoxTemporaryRelease, type FirefoxTemporaryRelease } from "./firefox-temporary-release";
import { SITE_URL } from "./seo";

export type { FirefoxTemporaryRelease } from "./firefox-temporary-release";

export type PublicStatsData = {
  processedResumeCount: number | null;
  todayRegisteredCount: number | null;
};

export type PublicPlanData = {
	id: string;
	name: string;
	memberType: string;
  durationDays: number;
  originalPrice: number;
  discountAmount: number;
  allowAutoReply: boolean;
	description: string;
	features: string[];
};

export type LocalAgentUpdate = {
	version: string;
	urlWin: string;
	urlMac: string;
	sha256: string;
	note: string;
};

export type GuideVideo = {
	id: string;
	title: string;
	description: string;
	src: string;
	enabled: boolean;
};

/** BrowserExtensionRelease 表示后台维护的插件兼容版本及各浏览器实际开放的安装包版本。 */
export type BrowserExtensionRelease = {
  version: string; note: string; url_chrome: string; url_firefox: string;
  version_chrome?: string; version_firefox?: string;
  sha256_chrome: string; sha256_firefox: string; firefox_signed: boolean;
};

/** getBrowserExtensionRelease 在服务端读取插件下载配置，按浏览器展示实际可下载的版本。 */
export async function getBrowserExtensionRelease(): Promise<BrowserExtensionRelease | null> {
  if (process.env.GOODHR_STATIC_EXPORT === "1") return null;
  try {
    const response = await fetch(`${cloudBaseURL()}/api/system/browser-extension`, {cache: "no-store"});
    if (!response.ok) return null;
    const data = await response.json();
    return data?.release?.version ? data.release as BrowserExtensionRelease : null;
  } catch { return null; }
}

/** getFirefoxTemporaryRelease 读取与本地程序相同的临时发布清单，失败时不冒充最新版本。 */
export async function getFirefoxTemporaryRelease(): Promise<FirefoxTemporaryRelease | null> {
  if (process.env.GOODHR_STATIC_EXPORT === "1") return null;
  try {
    const response = await fetch(new URL("/zip/goodhr-resume-firefox-latest.json", SITE_URL), {
      cache: "no-store",
      redirect: "error",
      signal: AbortSignal.timeout(6000),
    });
    if (!response.ok || Number(response.headers.get("content-length")) > 16384) return null;
    const body = await response.text();
    if (body.length > 16384) return null;
    return normalizeFirefoxTemporaryRelease(JSON.parse(body), SITE_URL);
  } catch {
    return null;
  }
}

/** getPublicStats 在服务端读取官网统计，失败时返回空数据且不影响页面。 */
export async function getPublicStats(): Promise<PublicStatsData> {
  const baseURL = cloudBaseURL();
  try {
    const response = await fetch(`${baseURL}/api/public/stats/today`, { next: { revalidate: 300 } });
    if (!response.ok) return emptyStats();
    const data = await response.json();
    return {
      processedResumeCount: safeNumber(data.processed_resume_count),
      todayRegisteredCount: safeNumber(data.today_registered_count),
    };
  } catch {
    return emptyStats();
  }
}

/** getPublicPlans 在服务端读取无需登录的订阅套餐配置。 */
export async function getPublicPlans(): Promise<PublicPlanData[]> {
	const baseURL = cloudBaseURL();
	try {
    const response = await fetch(`${baseURL}/api/subscription/plans`, { next: { revalidate: 300 } });
    if (!response.ok) return [];
    const data = await response.json();
    return Array.isArray(data?.plans) ? data.plans.map(normalizePlan).filter((item: PublicPlanData) => item.id) : [];
  } catch {
		return [];
	}
}

/** getLocalAgentUpdates 在服务端读取官网本地程序更新记录。 */
export async function getLocalAgentUpdates(): Promise<LocalAgentUpdate[]> {
	if (process.env.GOODHR_STATIC_EXPORT === "1") return [];
	const baseURL = cloudBaseURL();
	try {
		const response = await fetch(`${baseURL}/api/system/local-agent-updates`, { cache: "no-store" });
		if (!response.ok) return [];
		const data = await response.json();
		return Array.isArray(data?.local_agent) ? data.local_agent.map(normalizeLocalAgentUpdate) : [];
	} catch {
		return [];
	}
}

/** getGuideVideos 在服务端读取官网视频教程配置。 */
export async function getGuideVideos(): Promise<GuideVideo[]> {
	if (process.env.GOODHR_STATIC_EXPORT === "1") return [];
	const baseURL = cloudBaseURL();
	try {
		const response = await fetch(`${baseURL}/api/help/guide`, { cache: "no-store" });
		if (!response.ok) return [];
		const data = await response.json();
		const source = Array.isArray(data?.guide?.videos) ? data.guide.videos : Array.isArray(data?.videos) ? data.videos : [];
		return source.map(normalizeGuideVideo).filter((item: GuideVideo) => item.enabled && item.title && item.src);
	} catch {
		return [];
	}
}

/** cloudBaseURL 返回服务端访问云端 API 的统一地址。 */
function cloudBaseURL() {
	const fallback = process.env.NODE_ENV === "production"
		? "https://goodhr.58it.cn"
		: "http://127.0.0.1:8084";
	return (process.env.CLOUD_API_BASE || process.env.NEXT_PUBLIC_CLOUD_API_BASE || fallback).replace(/\/$/, "");
}

/** normalizePlan 将云端套餐字段转换为官网展示结构。 */
function normalizePlan(value: Record<string, unknown>): PublicPlanData {
  return {
    id: String(value?.id || ""),
    name: String(value?.name || "订阅套餐"),
    memberType: String(value?.member_type || "free"),
    durationDays: Math.max(0, Number(value?.duration_days || 0)),
    originalPrice: Math.max(0, Number(value?.original_price || 0)),
    discountAmount: Math.max(0, Number(value?.discount_amount || 0)),
    allowAutoReply: Boolean(value?.allow_auto_reply),
    description: String(value?.description || ""),
		features: Array.isArray(value?.features) ? value.features.map((item) => String(item)).filter(Boolean) : [],
	};
}

/** normalizeLocalAgentUpdate 将接口更新记录整理成下载页需要的字段。 */
function normalizeLocalAgentUpdate(value: any): LocalAgentUpdate {
	return {
		version: String(value?.version || ""),
		urlWin: String(value?.url_win || ""),
		urlMac: String(value?.url_mac || ""),
		sha256: String(value?.sha256 || ""),
		note: String(value?.note || ""),
	};
}

/** normalizeGuideVideo 将系统指南视频字段整理成页面展示结构。 */
function normalizeGuideVideo(value: Record<string, unknown>): GuideVideo {
	return {
		id: String(value?.id || value?.title || ""),
		title: String(value?.title || ""),
		description: String(value?.description || ""),
		src: String(value?.src || value?.url || value?.iframe_url || value?.video_url || ""),
		enabled: value?.enabled !== false,
	};
}

/** emptyStats 返回接口不可用时的空统计。 */
function emptyStats(): PublicStatsData {
	return { processedResumeCount: null, todayRegisteredCount: null };
}

/** safeNumber 将未知接口值转换为安全的非负数字。 */
function safeNumber(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}
