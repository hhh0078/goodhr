/** 本文件统一读取和上报用户首次跑通招聘岗位运行的流程状态。 */

import { cloudRequest } from "./admin-api";

export type UserFlowStep =
  | "agent_detected"
  | "runtime_ready"
  | "position_created"
  | "platform_login_verified"
  | "position_started"
  | "first_resume_processed"
  | "first_greet_success";

export type UserFlowState = {
  version: number;
  stage: UserFlowStep | "completed";
  stage_name: string;
  state: "pending" | "blocked" | "completed";
  reason_code?: string;
  message?: string;
  steps: Partial<Record<UserFlowStep, { status: "pending" | "blocked" | "completed" }>>;
};

type UserFlowReport = {
  step: UserFlowStep;
  status?: "pending" | "blocked" | "completed";
  reason_code?: string;
  message?: string;
  source?: string;
  position_id?: string;
  metadata?: Record<string, unknown>;
};

/** loadUserFlow 读取云端保存的当前流程快照。 */
export async function loadUserFlow(): Promise<UserFlowState> {
  const data = await cloudRequest("/api/user-flow");
  return data.flow;
}

/** reportUserFlow 上报流程事件；失败时静默返回，绝不阻断用户原操作。 */
export async function reportUserFlow(report: UserFlowReport) {
  const status = report.status || "completed";
  const cacheKey = `goodhr_user_flow_${report.step}_${status}_${report.reason_code || ""}`;
  if (typeof window !== "undefined" && sessionStorage.getItem(cacheKey)) return;
  try {
    await cloudRequest("/api/user-flow", {
      method: "POST",
      body: { ...report, status, source: report.source || "frontend" },
    });
    if (typeof window !== "undefined") sessionStorage.setItem(cacheKey, "1");
  } catch {
    // 流程记录是辅助数据，云端暂时不可用时不影响正常招聘操作。
  }
}
