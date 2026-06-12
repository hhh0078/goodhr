/** 本地 Agent 探测 */
import { ref } from "vue";
import { getLocalHealth } from "../services/localAgentApi";
import { markOnboardingStep } from "../services/onboarding";
const LOCAL_PORTS = [95271, 95272, 95273, 95274, 95275, 95276, 95277, 95278, 95279];

export function useAgent() {
  const status = ref("未检测到本地程序");
  const info = ref(null);
  const checking = ref(false);
  const baseUrl = ref("");

  /**
   * 主动扫描本机 Local Agent 端口。
   * @returns {Promise<void>} 无返回值。
   */
  async function detect() {
    if (checking.value) return;
    checking.value = true;
    info.value = null;
    for (const port of LOCAL_PORTS) {
      try {
        const candidateBaseUrl = `http://127.0.0.1:${port}`;
        const data = await getLocalHealth(candidateBaseUrl);
        info.value = data;
        status.value = `已连接 (端口 ${port})`;
        baseUrl.value = candidateBaseUrl;
        validateLocalVersion();
        if (baseUrl.value) {
          await markOnboardingStep("local_agent");
        }
        checking.value = false;
        return;
      } catch {}
    }
    status.value = "未检测到本地程序";
    info.value = null;
    baseUrl.value = "";
    checking.value = false;
  }

  /**
   * 根据缓存的系统配置检查本地程序版本。
   * @returns {void} 无返回值。
   */
  function validateLocalVersion() {
    try {
      const systemAppConfig = JSON.parse(
        localStorage.getItem("system_app_config") || "{}",
      );
      const requiredVersion = String(systemAppConfig?.local_agent_version || "").trim();
      if (requiredVersion && requiredVersion !== info.value?.version) {
        status.value = "本地程序需要更新";
      }
    } catch {}
  }

  return {
    status,
    info,
    checking,
    baseUrl,
    detect,
  };
}
