/** 文件作用说明：封装猎头客户和甲方岗位资料接口，复用统一鉴权并显式传递编辑版本。 */
"use client";
import { cloudRequest } from "./admin-api";
export type CustomerContact = { name: string; position: string; phone: string; email: string };
export type CustomerContract = { name: string; url: string };
export type HeadhunterCustomer = { id: string; name: string; short_name: string; code: string; industry: string; scale: string; city: string; status: string; source: string; owner_email: string; contacts: CustomerContact[]; contracts: CustomerContract[]; remark: string; revision: number; can_manage?: boolean };
export type HeadhunterJob = { id: string; customer_id: string; customer_name?: string; name: string; city: string; department: string; headcount: number; salary_min: number; salary_max: number; education: string; experience: string; status: string; priority: string; owner_email: string; description: string; requirements: string; remark: string; position_ids: string[]; revision: number; can_manage?: boolean };
export type HeadhunterMetadata = { members: Array<{ email: string; role?: string; status?: string }>; positions: Array<{ id: string; name: string; platform_id?: string; user_email?: string; is_open?: boolean }> };
export const customerStatuses = [{ value: "potential", label: "洽谈中" }, { value: "active", label: "合作中" }, { value: "paused", label: "已暂停" }];
export const jobStatuses = [{ value: "open", label: "招聘中" }, { value: "paused", label: "已暂停" }, { value: "closed", label: "已关闭" }];
export const jobPriorities = [{ value: "low", label: "不着急" }, { value: "normal", label: "正常" }, { value: "high", label: "优先" }, { value: "urgent", label: "急招" }];

/** loadHeadhunterCustomers 分页读取可见客户，用于岗位选择，不把第一页当成全部客户。 */
export async function loadHeadhunterCustomers(signal?: AbortSignal): Promise<HeadhunterCustomer[]> {
  const items: HeadhunterCustomer[] = [];
  for (let page = 1; page <= 1000; page += 1) {
    const result = await cloudRequest(`/api/headhunter/customers?page=${page}&page_size=100`, { signal });
    if (!Array.isArray(result.items)) throw new Error("客户列表没读完整，请重试。");
    const fresh = result.items.filter((customer: HeadhunterCustomer) => !items.some((item) => item.id === customer.id)); items.push(...fresh);
    if (!fresh.length || items.length >= Number(result.total)) break;
  }
  return items;
}

/** loadHeadhunterMetadata 读取当前团队可用负责人和平台岗位，不包含 AI 或联系人密钥。 */
export async function loadHeadhunterMetadata(signal?: AbortSignal): Promise<HeadhunterMetadata> {
  const result = await cloudRequest("/api/headhunter/metadata", { signal });
  if (!Array.isArray(result.members) || !Array.isArray(result.positions)) throw new Error("团队和岗位选项没读完整，请重试。");
  return result;
}

/** saveHeadhunterEntity 使用服务端版本保护编辑，冲突时保留用户草稿供核对。 */
export async function saveHeadhunterEntity(kind: "customers" | "jobs", entity: HeadhunterCustomer | HeadhunterJob) {
  const writable = kind === "customers"
    ? ["name", "short_name", "code", "industry", "scale", "city", "status", "source", "owner_email", "contacts", "contracts", "remark", "revision"]
    : ["customer_id", "name", "city", "department", "headcount", "salary_min", "salary_max", "education", "experience", "status", "priority", "owner_email", "description", "requirements", "remark", "position_ids", "revision"];
  const fields = entity as unknown as Record<string, unknown>;
  const body = Object.fromEntries(writable.filter((key) => fields[key] !== undefined).map((key) => [key, fields[key]]));
  const result = await cloudRequest(`/api/headhunter/${kind}${entity.id ? `/${encodeURIComponent(entity.id)}` : ""}`, { method: entity.id ? "PUT" : "POST", body });
  const saved = kind === "customers" ? result.customer : result.job;
  if (!saved?.id) throw new Error("保存结果没确认，请重新查询后核对一下。");
  return saved;
}
