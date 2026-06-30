/** 本文件负责在 Next.js 服务端读取官网公开数据。 */

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

/** getPublicStats 在服务端读取官网统计，失败时返回空数据且不影响页面。 */
export async function getPublicStats(): Promise<PublicStatsData> {
  const baseURL = (process.env.CLOUD_API_BASE || process.env.NEXT_PUBLIC_CLOUD_API_BASE || "https://goodhr5.58it.cn").replace(/\/$/, "");
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
	const baseURL = cloudBaseURL();
	try {
		const response = await fetch(`${baseURL}/api/system/local-agent-updates`, { next: { revalidate: 300 } });
		if (!response.ok) return [];
		const data = await response.json();
		return Array.isArray(data?.local_agent) ? data.local_agent.map(normalizeLocalAgentUpdate) : [];
	} catch {
		return [];
	}
}

/** cloudBaseURL 返回服务端访问云端 API 的统一地址。 */
function cloudBaseURL() {
	return (process.env.CLOUD_API_BASE || process.env.NEXT_PUBLIC_CLOUD_API_BASE || "https://goodhr5.58it.cn").replace(/\/$/, "");
}

/** normalizePlan 将云端套餐字段转换为官网展示结构。 */
function normalizePlan(value: Record<string, unknown>): PublicPlanData {
  return {
    id: String(value?.id || ""),
    name: String(value?.name || "订阅套餐"),
    memberType: String(value?.member_type || "plus"),
    durationDays: Math.max(0, Number(value?.duration_days || 0)),
    originalPrice: Math.max(0, Number(value?.original_price || 0)),
    discountAmount: Math.max(0, Number(value?.discount_amount || 0)),
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

/** emptyStats 返回接口不可用时的空统计。 */
function emptyStats(): PublicStatsData {
	return { processedResumeCount: null, todayRegisteredCount: null };
}

/** safeNumber 将未知接口值转换为安全的非负数字。 */
function safeNumber(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}
