/** 本文件负责浏览器端岗位运行启动前的 AI 余额和本地程序版本强制校验。 */

export const MINIMUM_POSITION_BALANCE_YUAN = 0.1;

export type PositionStartGuardFailure = {
  code: string;
  title: string;
  message: string;
};

/** latestLocalAgentRelease 读取当前系统对应的本地程序版本要求。 */
export function latestLocalAgentRelease(config: any) {
  const item = Array.isArray(config?.local_agent) ? config.local_agent[0] || {} : {};
  const isWindows = typeof navigator !== "undefined" && navigator.userAgent.toLowerCase().includes("windows");
  return {
    version: String(item.version || "").trim(),
    url: String(isWindows ? item.url_win || item.url_windows || item.url || "" : item.url_mac || item.url_macos || item.url || "").trim(),
    note: String(item.note || item.changelog || item.description || item.release_note || "").trim(),
  };
}

/** evaluatePositionStartGuard 按岗位是否使用 AI 判断余额和本地程序版本是否允许启动。 */
export function evaluatePositionStartGuard(wallet: any, currentVersion: unknown, requiredVersion: unknown, usesAI = true): PositionStartGuardFailure | null {
  if (usesAI) {
    const balance = walletBalanceYuan(wallet);
    if (balance == null) {
      return {
        code: "ai_balance_unavailable",
        title: "余额检查失败",
        message: "暂时没有读到 AI 余额。为了避免岗位运行中途停下，本次不会开始，请刷新页面后重试。",
      };
    }
    if (balance < MINIMUM_POSITION_BALANCE_YUAN) {
      return {
        code: "ai_balance_insufficient",
        title: "AI 余额不足",
        message: `当前 AI 余额为 ￥${balance.toFixed(4)}，低于岗位运行启动要求的 ￥${MINIMUM_POSITION_BALANCE_YUAN.toFixed(2)}。请先充值，本次岗位运行不会开始。`,
      };
    }
  }
  const current = String(currentVersion || "").trim();
  const required = String(requiredVersion || "").trim();
  if (!current || !required) {
    return {
      code: "agent_version_unavailable",
      title: "版本检查失败",
      message: "暂时没有读到本地程序当前版本或后台要求版本。本次岗位运行不会开始，请重新连接本地程序后重试。",
    };
  }
  if (isVersionLower(current, required)) {
    return {
      code: "agent_version_outdated",
      title: "本地程序版本过低",
      message: `当前版本为 ${current}，后台要求版本为 ${required}。请先完成更新，本次岗位运行不会开始。`,
    };
  }
  return null;
}

/** positionUsesAI 判断岗位的基础筛选或详情筛选是否启用了 AI 模式。 */
export function positionUsesAI(position: any) {
  const commonConfig = position?.common_config || {};
  const baseMode = String(commonConfig.mode_default || "").trim().toLowerCase();
  const detailMode = String(commonConfig.detail_mode || "").trim().toLowerCase();
  return baseMode === "ai" || detailMode === "ai";
}

/** walletBalanceYuan 将钱包接口的不同余额字段统一转换为元。 */
export function walletBalanceYuan(wallet: any): number | null {
  const source = wallet?.wallet || wallet || {};
  if (source.balance_units != null) return finiteNumber(source.balance_units, 10000);
  if (source.balance != null) return finiteNumber(source.balance, 1);
  if (source.balance_cents != null) return finiteNumber(source.balance_cents, 100);
  return null;
}

/** isVersionLower 判断当前版本是否低于目标版本。 */
export function isVersionLower(current: string, target: string) {
  return compareVersion(target, current) > 0;
}

/** compareVersion 按点分数字比较版本号。 */
export function compareVersion(left: string, right: string) {
  const leftParts = parseVersionParts(left);
  const rightParts = parseVersionParts(right);
  const maxLen = Math.max(leftParts.length, rightParts.length);
  for (let index = 0; index < maxLen; index += 1) {
    const leftValue = leftParts[index] || 0;
    const rightValue = rightParts[index] || 0;
    if (leftValue > rightValue) return 1;
    if (leftValue < rightValue) return -1;
  }
  return 0;
}

/** parseVersionParts 将版本号拆成数字片段。 */
function parseVersionParts(value: string) {
  return String(value || "").trim().replace(/^v/i, "").split(".").map((part) => {
    const match = part.trim().match(/^\d+/);
    return match ? Number(match[0]) : 0;
  });
}

/** finiteNumber 读取有限数值并按指定单位换算。 */
function finiteNumber(value: unknown, divisor: number) {
  const number = Number(value);
  return Number.isFinite(number) ? number / divisor : null;
}
