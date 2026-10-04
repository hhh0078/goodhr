/** 本文件负责简历库筛选参数的白名单校验、地址解析和查询生成。 */

export type ResumeFilters = {
  keyword: string;
  positionID: string;
  platformID: string;
  phoneStatus: string;
  conditionStatus: string;
  sort: string;
};

export const DEFAULT_RESUME_FILTERS: ResumeFilters = {
  keyword: "",
  positionID: "",
  platformID: "",
  phoneStatus: "has",
  conditionStatus: "all_matched",
  sort: "second_score_desc",
};

/** allowedResumeParam 读取并校验一个简历库枚举参数。 */
function allowedResumeParam(
  params: { get(name: string): string | null },
  name: string,
  allowed: readonly string[],
  missingFallback: string,
  invalidFallback: string,
) {
  const value = params.get(name);
  if (value === null) return missingFallback;
  const normalized = value.trim().toLowerCase();
  return allowed.includes(normalized) ? normalized : invalidFallback;
}

/** resumeFiltersFromParams 从页面地址读取并校验简历库筛选条件。 */
export function resumeFiltersFromParams(params: {
  get(name: string): string | null;
}): ResumeFilters {
  return {
    keyword: params.get("q") || params.get("keyword") || "",
    positionID: (params.get("position_id") || "").trim(),
    platformID: allowedResumeParam(
      params,
      "platform_id",
      ["boss", "zhaopin", "hliepin", "liepin"],
      "",
      "",
    ),
    phoneStatus: allowedResumeParam(
      params,
      "has_phone",
      ["has", "none", "all"],
      DEFAULT_RESUME_FILTERS.phoneStatus,
      "all",
    ),
    conditionStatus: allowedResumeParam(
      params,
      "condition_status",
      ["all_matched", "pending", "unmatched", "untracked", "all"],
      DEFAULT_RESUME_FILTERS.conditionStatus,
      "all",
    ),
    sort: allowedResumeParam(
      params,
      "sort",
      ["second_score_desc", "recent"],
      DEFAULT_RESUME_FILTERS.sort,
      "recent",
    ),
  };
}

/** resumePageNumber 从地址参数读取安全的正整数页码。 */
export function resumePageNumber(
  value: string | null,
  fallback: number,
  maximum?: number,
) {
  const parsed = Number.parseInt(value || "", 10);
  if (!Number.isFinite(parsed) || parsed <= 0) return fallback;
  return maximum ? Math.min(parsed, maximum) : parsed;
}

/** resumeListQuery 生成后端查询和浏览器地址共用的简历筛选参数。 */
export function resumeListQuery(
  filters: ResumeFilters,
  page: number,
  pageSize: number,
) {
  const query = new URLSearchParams({
    page: String(page),
    page_size: String(pageSize),
    has_phone: filters.phoneStatus,
    condition_status: filters.conditionStatus,
    sort: filters.sort,
  });
  if (filters.keyword.trim()) query.set("q", filters.keyword.trim());
  if (filters.positionID) query.set("position_id", filters.positionID);
  if (filters.platformID) query.set("platform_id", filters.platformID);
  return query;
}
