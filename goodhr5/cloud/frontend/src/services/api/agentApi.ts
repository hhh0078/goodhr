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

/**
 * 读取当前云端账号已绑定的本地 Agent 机器。
 * @returns {Promise<any>} 返回当前绑定信息，没有绑定时返回 null。
 */
export async function currentAgent() {
  const data = await api("/api/agents/current");
  return data.agent || null;
}
