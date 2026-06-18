/** 本文件负责在 Next.js 服务端读取官网公开数据。 */

export type PublicStatsData = {
  processedResumeCount: number | null;
  todayRegisteredCount: number | null;
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

/** emptyStats 返回接口不可用时的空统计。 */
function emptyStats(): PublicStatsData {
  return { processedResumeCount: null, todayRegisteredCount: null };
}

/** safeNumber 将未知接口值转换为安全的非负数字。 */
function safeNumber(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}
