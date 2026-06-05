/** 本地 Agent 探测 */
import { ref } from "vue";
import { getLocalHealth } from "../services/localAgentApi";
const LOCAL_PORTS = [9001, 9002, 9003, 9004, 9005, 9006, 9007, 9008, 9009];

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
      if (systemAppConfig?.local_agent_version != info.value?.version) {
        status.value = "版本过低，请更新本地程序";
        info.value = null;
        baseUrl.value = "";
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
