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

  async function detect(user = null, token = "") {
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
        status.value = `已连接 (端口 ${port})`;
        baseUrl.value = candidateBaseUrl;
        // 用云端存储的版本号覆盖 /health 的版本
        const SYSTEM_APP_CONFIG_CACHE_KEY = "system_app_config";
        let systemAppConfig = JSON.parse(
          localStorage.getItem(SYSTEM_APP_CONFIG_CACHE_KEY) || "{}",
        );

        try {
          if (systemAppConfig?.local_agent_version != info.value?.version) {
            status.value = "版本过低，请更新本地程序";
            info.value = null;
            baseUrl.value = "";
            wsStatus.value = "未连接";
            machineConflict.value = false;
            checking.value = false;
            return;
          }

          // info.value = { ...info.value, version: ca.agent_version };
        } catch {}
        if (!user || !token) {
          bindStatus.value = "未登录";
          wsStatus.value = "未连接";
          wsError.value = "";
          checking.value = false;
          return;
        }
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
        await markOnboardingStep("local_agent");
        await bind(user, token);
        await refreshWSStatus();
        return;
      } catch {}
    }
    status.value = "未检测到本地程序";
    info.value = null;
    baseUrl.value = "";
    wsStatus.value = "未连接";
    machineConflict.value = false;
    checking.value = false;
  }

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
