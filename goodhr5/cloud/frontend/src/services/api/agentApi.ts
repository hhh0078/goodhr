// 本文件负责云端 Agent 绑定相关接口。
import { api } from "../apiClient";

/**
 * 绑定当前云端账号和本地 Agent 机器信息。
 * @param {any} payload - 包含 machine_id、agent_version、local_port 和 public_key 的绑定参数。
 * @returns {Promise<any>} 返回云端保存后的 Agent 绑定信息。
 */
export async function bindAgent(payload: any) {
  const data = await api("/api/agents/bind", { method: "POST", body: payload });
  return data.agent;
}
