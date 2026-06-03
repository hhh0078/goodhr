/** 本地 Agent 探测和绑定 */
import { ref } from "vue";
import { bindAgent, currentAgent } from "../services/api/agentApi";
import {
  bindCloudUser,
  getCloudWSStatus,
  getLocalHealth,
} from "../services/localAgentApi";
import { markOnboardingStep } from "../services/onboarding";

const LOCAL_PORTS = [9001, 9002, 9003, 9004, 9005, 9006, 9007, 9008, 9009];

export function useAgent() {
  const status = ref("未检测到本地程序");
  const info = ref(null);
  const bindStatus = ref("未绑定");
  const bindError = ref("");
  const wsStatus = ref("未连接");
  const wsError = ref("");
  const checking = ref(false);
  const baseUrl = ref("");
  const machineConflict = ref(false);

  async function detect(user, token) {
    if (!user) return;
    if (checking.value) return;
    checking.value = true;
    info.value = null;
    bindStatus.value = "未绑定";
    bindError.value = "";
    machineConflict.value = false;

    for (const port of LOCAL_PORTS) {
      try {
        const candidateBaseUrl = `http://127.0.0.1:${port}`;
        const data = await getLocalHealth(candidateBaseUrl);
        info.value = data;
        const machineID = String(data?.machine_id || "").trim();
        const boundAgent = await currentAgent();
        const boundMachineID = String(boundAgent?.machine_id || "").trim();
        if (boundMachineID && machineID && boundMachineID !== machineID) {
          machineConflict.value = true;
          status.value = "该账号已经绑定其它电脑";
          bindStatus.value = "绑定失败";
          bindError.value = "该账号已经绑定其它电脑，请联系管理员解除绑定";
          baseUrl.value = "";
          wsStatus.value = "未连接";
          wsError.value = "";
          checking.value = false;
          return;
        }

        status.value = `已连接 (端口 ${port})`;
        baseUrl.value = candidateBaseUrl;
        await markOnboardingStep("local_agent");
        await bind(user, token);
        await refreshWSStatus();
        try {
          const cloudAgent = await currentAgent();
          if (cloudAgent?.agent_version) {
            info.value = { ...info.value, version: cloudAgent.agent_version };
          }
          if (cloudAgent?.version_warning) {
            info.value = { ...info.value, version_warning: cloudAgent.version_warning };
          }
        } catch {}
        return;
      } catch {
        /* 端口不可达，继续下一个 */
      }
    }

    status.value = "未检测到本地程序";
    info.value = null;
    baseUrl.value = "";
    wsStatus.value = "未连接";
    machineConflict.value = false;
    checking.value = false;
  }

  //

  async function bind(user, token) {
    bindStatus.value = "绑定中";
    bindError.value = "";
    try {
      const machineID = info.value?.machine_id || "";
      if (!machineID) throw new Error("本地程序缺少机器码");
      const publicKey = info.value?.public_key || "";
      if (!publicKey)
        throw new Error("本地程序缺少加密公钥，请更新并重启本地程序");
      await bindCloudUser(baseUrl.value, {
        cloud_user_id: user.id,
        cloud_email: user.email,
        agent_token: token,
        public_key: publicKey,
      });
      await bindAgent({
        machine_id: machineID,
        agent_version: info.value?.version || "",
        local_port: info.value?.port || 0,
        public_key: publicKey,
      });
      bindStatus.value = "已绑定";
    } catch (e) {
      bindError.value = e.message;
      bindStatus.value = "绑定失败";
    } finally {
      checking.value = false;
    }
  }

  /**
   * 刷新本地 Agent 的 WebSocket 连接状态。
   * @returns {Promise<void>} 无返回值。
   */
  async function refreshWSStatus() {
    if (!baseUrl.value) return;
    try {
      const data = await getCloudWSStatus(baseUrl.value);
      wsStatus.value = data.status || (data.connected ? "已连接" : "未连接");
      wsError.value = data.last_error || "";
    } catch (e) {
      wsStatus.value = "状态未知";
      wsError.value = e.message;
    }
  }

  return {
    status,
    info,
    bindStatus,
    bindError,
    wsStatus,
    wsError,
    checking,
    baseUrl,
    machineConflict,
    detect,
    refreshWSStatus,
  };
}
