/** 文件作用说明：通过现有云端鉴权只读查询本人岗位的筛选历史，不调用本地任务或 AI。 */
"use client";
import { cloudRequest } from "./admin-api";
import { screeningPageSize, screeningQuery, type ScreeningFilters, type ScreeningRecord } from "./position-screening";

/** loadPositionScreeningRecords 读取当前岗位的服务端筛选与分页结果，调用者负责取消过期查询。 */
export async function loadPositionScreeningRecords(positionID: string, page: number, filters: ScreeningFilters, signal: AbortSignal): Promise<{ records: ScreeningRecord[]; total: number }> {
  const result = await cloudRequest(`/api/positions/${encodeURIComponent(positionID)}/screening-records?${screeningQuery(page, filters)}`, { signal });
  if (result.ok !== true || !Array.isArray(result.records) || !Number.isSafeInteger(result.total) || result.total < 0 || result.page_size !== screeningPageSize || result.records.length > screeningPageSize
    || result.records.some((record: ScreeningRecord) => !record || typeof record.id !== "string" || record.position_id !== positionID || !record.first || !record.second)) throw new Error("筛选记录没读完整，请重试。");
  return { records: result.records, total: result.total };
}
