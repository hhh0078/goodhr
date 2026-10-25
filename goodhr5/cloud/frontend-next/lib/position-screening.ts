/** 文件作用说明：定义岗位两轮筛选记录与查询合同，统一空分数、实际耗时及本地时间展示。 */
export type ScreeningRound = {
  kind: "ai" | "keyword" | "none";
  status: "not_executed" | "running" | "passed" | "rejected" | "failed" | "cancelled";
  accepted: boolean | null; score: number | null; threshold: number | null; reason: string;
  started_at: string | null; finished_at: string | null; duration_ms: number | null;
};
export type ScreeningRecord = {
  id: string; position_id: string; attempt_id: string; task_id: string; platform_id: string;
  platform_candidate_id: string; candidate_key: string; candidate_name: string; occurred_at: string;
  revision: number; completed: boolean; termination_reason: string;
  first: ScreeningRound; second: ScreeningRound; created_at: string; updated_at: string;
};
export type ScreeningRoundFilters = { scoreMin: number | null; scoreMax: number | null; startedFrom: string; startedTo: string };
export type ScreeningFilters = { first: ScreeningRoundFilters; second: ScreeningRoundFilters };
export const screeningPageSize = 20;
export const screeningStatusLabels: Record<ScreeningRound["status"], string> = { not_executed: "未执行", running: "进行中", passed: "通过", rejected: "不通过", failed: "失败", cancelled: "已取消" };

/** emptyScreeningFilters 返回独立的两轮空条件，避免重置时共享可变对象。 */
export function emptyScreeningFilters(): ScreeningFilters {
  return { first: { scoreMin: null, scoreMax: null, startedFrom: "", startedTo: "" }, second: { scoreMin: null, scoreMax: null, startedFrom: "", startedTo: "" } };
}

/** screeningQuery 校验各轮独立范围并生成服务端分页条件；零分保留，空值不发送，时间上限不包含。 */
export function screeningQuery(page: number, filters: ScreeningFilters): URLSearchParams {
  const query = new URLSearchParams({ page: String(Number.isSafeInteger(page) && page > 0 ? page : 1), page_size: String(screeningPageSize) });
  for (const key of ["first", "second"] as const) {
    const filter = filters[key]; const label = key === "first" ? "第一轮" : "第二轮";
    for (const value of [filter.scoreMin, filter.scoreMax]) if (value !== null && (!Number.isFinite(value) || value < 0 || value > 100)) throw new Error(`${label}分数需要在 0～100 之间。`);
    if (filter.scoreMin !== null && filter.scoreMax !== null && filter.scoreMin > filter.scoreMax) throw new Error(`${label}最低分不能大于最高分。`);
    if (filter.scoreMin !== null) query.set(`${key}_score_min`, String(filter.scoreMin));
    if (filter.scoreMax !== null) query.set(`${key}_score_max`, String(filter.scoreMax));
    if (filter.startedFrom && filter.startedTo && Date.parse(filter.startedFrom) >= Date.parse(filter.startedTo)) throw new Error(`${label}结束时间需要晚于开始时间。`);
    for (const [suffix, value] of [["from", filter.startedFrom], ["to", filter.startedTo]]) {
      if (!value) continue;
      const time = new Date(value); if (!Number.isFinite(time.getTime())) throw new Error(`${label}时间没读完整，请重新选择。`);
      query.set(`${key}_started_${suffix}`, time.toISOString());
    }
  }
  return query;
}

/** screeningScore 保留真实零分；未执行和关键词判断不冒充已评分。 */
export function screeningScore(round: ScreeningRound): string {
  if (round.status === "not_executed") return "未评分";
  if (round.kind === "keyword") return "关键词判断，不评分";
  return typeof round.score === "number" && Number.isFinite(round.score) ? `${round.score} 分` : "未评分";
}

/** screeningDuration 按真实毫秒显示秒数，缺失值不写成零耗时。 */
export function screeningDuration(value: number | null): string {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? `${Number((value / 1000).toFixed(3))} 秒` : "未记录";
}

/** screeningTime 显示当前电脑时区的日期和时分秒，缺失或损坏时间保持未记录。 */
export function screeningTime(value: string | null): string {
  const date = value ? new Date(value) : null;
  return date && Number.isFinite(date.getTime()) ? date.toLocaleString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }) : "未记录";
}
