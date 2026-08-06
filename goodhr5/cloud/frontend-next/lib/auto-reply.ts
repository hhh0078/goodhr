/** 本文件负责自动回复前端强类型模型、接口封装和安全数据转换。 */
"use client";

import { cloudRequest } from "./admin-api";
import {
  DEFAULT_RESUME_REQUEST_MESSAGE,
  duplicateConditionContent,
  emptyPositionAutoReplyConfig,
} from "./auto-reply-rules";

export {
  DEFAULT_RESUME_REQUEST_MESSAGE,
  duplicateConditionContent,
  emptyPositionAutoReplyConfig,
} from "./auto-reply-rules";

export type AutoReplyConditionType = "required" | "confirm" | "bonus";

export type CompanyProfile = {
  id: string;
  name: string;
  address: string;
  contact: string;
  overview: string;
  extra_info: string;
  updated_at: string;
};

export type PositionReplyCondition = {
  id: string;
  type: AutoReplyConditionType;
  content: string;
  sort_order: number;
  enabled: boolean;
};

export type PositionAutoReplyConfig = {
  position_id: string;
  company_profile_id: string;
  enabled: boolean;
  position_description: string;
  resume_request_message: string;
  poll_interval_seconds: number;
  max_threads_per_checkpoint: number;
  version: number;
  conditions: PositionReplyCondition[];
};

export type PositionAutoReplyStatus = {
  enabled: boolean;
  configured_enabled: boolean;
  version: number;
  allow_auto_reply: boolean;
};

export type AutoReplyConfigSuggestion = {
  id: string;
  position_id: string;
  company_profile_id: string;
  suggestion_type: string;
  operation: string;
  target_id: string;
  proposed_value: unknown;
  reason: string;
  status: string;
  created_at: string;
};

/** emptyCompanyProfile 创建不会共享引用的空公司档案表单。 */
export function emptyCompanyProfile(): CompanyProfile {
  return {
    id: "",
    name: "",
    address: "",
    contact: "",
    overview: "",
    extra_info: "",
    updated_at: "",
  };
}

/** loadCompanyProfiles 读取当前团队共享的公司档案。 */
export async function loadCompanyProfiles() {
  const payload = recordValue(
    await cloudRequest("/api/auto-reply/company-profiles"),
  );
  return arrayValue(payload.company_profiles).map(normalizeCompanyProfile);
}

/** saveCompanyProfile 新增或更新一份团队共享公司档案。 */
export async function saveCompanyProfile(item: CompanyProfile) {
  const path = item.id
    ? `/api/auto-reply/company-profiles/${encodeURIComponent(item.id)}`
    : "/api/auto-reply/company-profiles";
  const payload = recordValue(
    await cloudRequest(path, {
      method: item.id ? "PUT" : "POST",
      body: {
        name: item.name.trim(),
        address: item.address.trim(),
        contact: item.contact.trim(),
        overview: item.overview.trim(),
        extra_info: item.extra_info.trim(),
      },
    }),
  );
  return normalizeCompanyProfile(payload.company_profile);
}

/** deleteCompanyProfile 删除一份未被岗位使用的团队公司档案。 */
export async function deleteCompanyProfile(profileID: string) {
  await cloudRequest(
    `/api/auto-reply/company-profiles/${encodeURIComponent(profileID)}`,
    { method: "DELETE" },
  );
}

/** loadPositionAutoReplyConfig 读取岗位自动回复完整配置。 */
export async function loadPositionAutoReplyConfig(positionID: string) {
  const payload = recordValue(
    await cloudRequest(
      `/api/auto-reply/positions/${encodeURIComponent(positionID)}/config`,
    ),
  );
  return normalizePositionAutoReplyConfig(payload.config, positionID);
}

/** savePositionAutoReplyConfig 保存岗位自动回复配置和全部条件。 */
export async function savePositionAutoReplyConfig(
  item: PositionAutoReplyConfig,
) {
  const payload = recordValue(
    await cloudRequest(
      `/api/auto-reply/positions/${encodeURIComponent(item.position_id)}/config`,
      {
        method: "PUT",
        body: {
          company_profile_id: item.company_profile_id,
          enabled: item.enabled,
          position_description: item.position_description.trim(),
          resume_request_message: item.resume_request_message.trim(),
          poll_interval_seconds: item.poll_interval_seconds,
          max_threads_per_checkpoint: item.max_threads_per_checkpoint,
          conditions: item.conditions.map((condition, index) => ({
            id: condition.id,
            type: condition.type,
            content: condition.content.trim(),
            sort_order: index,
            enabled: condition.enabled,
          })),
        },
      },
    ),
  );
  return normalizePositionAutoReplyConfig(payload.config, item.position_id);
}

/** loadPositionAutoReplyStatus 读取岗位自动回复实时开关和会员权限。 */
export async function loadPositionAutoReplyStatus(
  positionID: string,
): Promise<PositionAutoReplyStatus> {
  const payload = recordValue(
    await cloudRequest(
      `/api/auto-reply/positions/${encodeURIComponent(positionID)}/status`,
    ),
  );
  const subscription = recordValue(payload.subscription);
  return {
    enabled: Boolean(payload.enabled),
    configured_enabled: Boolean(payload.configured_enabled),
    version: finiteNumber(payload.version),
    allow_auto_reply: Boolean(subscription.allow_auto_reply),
  };
}

/** loadAutoReplySuggestions 读取当前团队的配置修改建议。 */
export async function loadAutoReplySuggestions(status = "pending") {
  const query = new URLSearchParams({ status, limit: "100" });
  const payload = recordValue(
    await cloudRequest(`/api/auto-reply/suggestions?${query.toString()}`),
  );
  return arrayValue(payload.suggestions).map(normalizeAutoReplySuggestion);
}

/** reviewAutoReplySuggestion 审核一条配置修改建议但不直接改原配置。 */
export async function reviewAutoReplySuggestion(
  suggestionID: string,
  status: "approved" | "rejected",
) {
  const payload = recordValue(
    await cloudRequest(
      `/api/auto-reply/suggestions/${encodeURIComponent(suggestionID)}/review`,
      { method: "POST", body: { status } },
    ),
  );
  return normalizeAutoReplySuggestion(payload.suggestion);
}

/** normalizeCompanyProfile 把公司档案接口值转换为表单类型。 */
function normalizeCompanyProfile(value: unknown): CompanyProfile {
  const source = recordValue(value);
  return {
    id: stringValue(source.id),
    name: stringValue(source.name),
    address: stringValue(source.address),
    contact: stringValue(source.contact),
    overview: stringValue(source.overview),
    extra_info: stringValue(source.extra_info),
    updated_at: stringValue(source.updated_at),
  };
}

/** normalizePositionAutoReplyConfig 把岗位配置接口值转换为安全表单。 */
function normalizePositionAutoReplyConfig(
  value: unknown,
  positionID: string,
): PositionAutoReplyConfig {
  const source = recordValue(value);
  return {
    position_id: stringValue(source.position_id) || positionID,
    company_profile_id: stringValue(source.company_profile_id),
    enabled: Boolean(source.enabled),
    position_description: stringValue(source.position_description),
    resume_request_message:
      stringValue(source.resume_request_message) ||
      DEFAULT_RESUME_REQUEST_MESSAGE,
    poll_interval_seconds: finiteNumber(source.poll_interval_seconds) || 5,
    max_threads_per_checkpoint:
      finiteNumber(source.max_threads_per_checkpoint) || 3,
    version: finiteNumber(source.version),
    conditions: arrayValue(source.conditions).map((condition, index) => {
      const item = recordValue(condition);
      const type = stringValue(item.type);
      return {
        id: stringValue(item.id),
        type: isConditionType(type) ? type : "confirm",
        content: stringValue(item.content),
        sort_order: finiteNumber(item.sort_order) || index,
        enabled: item.enabled !== false,
      };
    }),
  };
}

/** normalizeAutoReplySuggestion 把 AI 配置建议转换为审核列表类型。 */
function normalizeAutoReplySuggestion(
  value: unknown,
): AutoReplyConfigSuggestion {
  const source = recordValue(value);
  return {
    id: stringValue(source.id),
    position_id: stringValue(source.position_id),
    company_profile_id: stringValue(source.company_profile_id),
    suggestion_type: stringValue(source.suggestion_type),
    operation: stringValue(source.operation),
    target_id: stringValue(source.target_id),
    proposed_value: source.proposed_value ?? {},
    reason: stringValue(source.reason),
    status: stringValue(source.status),
    created_at: stringValue(source.created_at),
  };
}

/** isConditionType 判断字符串是否为后端支持的岗位条件类型。 */
function isConditionType(value: string): value is AutoReplyConditionType {
  return value === "required" || value === "confirm" || value === "bonus";
}

/** recordValue 把未知接口值安全转换为普通对象。 */
function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

/** arrayValue 把未知接口值安全转换为数组。 */
function arrayValue(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

/** stringValue 把未知接口值转换为字符串。 */
function stringValue(value: unknown) {
  return typeof value === "string" ? value.trim() : "";
}

/** finiteNumber 把未知接口值转换为有限非负数字。 */
function finiteNumber(value: unknown) {
  const parsed = Number(value ?? 0);
  return Number.isFinite(parsed) ? Math.max(0, parsed) : 0;
}
