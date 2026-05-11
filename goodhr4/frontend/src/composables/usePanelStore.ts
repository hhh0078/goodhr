/**
 * 面板状态管理 composable（单例模式）
 * 集中管理应用设置、UI状态、日志、账号绑定等所有响应式状态
 *
 * 所有 reactive/computed 状态在模块级别创建，确保跨组件共享同一实例。
 * onMounted/onUnmounted 只在首个调用组件时注册，避免重复初始化。
 */

import { computed, reactive, watch } from "vue";
import {
  createDefaultSettings,
  createEmptyPosition,
  DEFAULT_LOGS,
  IDENTITY_KEY,
  STORAGE_KEY,
  LOGS_KEY,
  MAX_LOGS,
} from "../constants/defaults.js";
import type {
  Settings,
  Position,
  SystemConfig,
  LogEntry,
} from "../constants/defaults.js";
import {
  bindIdentity,
  fetchSettings,
  fetchSystemConfig,
  registerAuthUser,
  saveSettings,
} from "../services/api.js";
import { chatWithAI, configureAI } from "../services/ai.js";
import {
  attachRuntimeLogListener,
  getManifestVersion,
  pushSettingsToPage,
  startRunOnPage,
  stopRunOnPage,
  storageGet,
  storageSet,
} from "../services/extension.js";
import { deepClone } from "../utils/clone.js";
import { APP_VERSION } from "../constants/appVersion.js";

/** 获取当前时间的格式化字符串 */
function now(): string {
  return new Date().toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

/** 根据标识符推断身份类型 */
function inferIdentityType(identifier: string): string {
  return identifier.includes("@") ? "email" : "phone";
}

/** 将标识符转为注册邮箱格式 */
function resolveRegisterEmail(identifier: string): string {
  const value = (identifier || "").trim();
  if (!value) return "";
  if (value.includes("@")) return value.toLowerCase();
  return `${value}@phone.goodhr.local`;
}

/** 格式化余额显示 */
function formatBalance(balance: number | null | undefined | string): string {
  if (balance === null || balance === undefined || balance === "") return "";
  const numeric = Number(balance);
  if (Number.isFinite(numeric)) {
    return `¥ ${numeric.toFixed(2)}`;
  }
  return `¥ ${balance}`;
}

/** 解析版本号片段 */
function parseVersionPart(part: string | undefined): number {
  const value = Number(part);
  return Number.isFinite(value) ? value : 0;
}

/** 比较版本号大小，返回 1 / -1 / 0 */
function compareVersions(a: string, b: string): number {
  const left = String(a || "")
    .replace(/^v/i, "")
    .split(".");
  const right = String(b || "")
    .replace(/^v/i, "")
    .split(".");
  const length = Math.max(left.length, right.length);
  for (let i = 0; i < length; i += 1) {
    const l = parseVersionPart(left[i]);
    const r = parseVersionPart(right[i]);
    if (l > r) return 1;
    if (l < r) return -1;
  }
  return 0;
}

/** 过滤掉旧版默认岗位 */
function stripLegacyDefaultPositions(positions: any[]): Position[] {
  if (!Array.isArray(positions)) {
    return [];
  }

  return positions.filter((position) => {
    if (!position || typeof position !== "object") {
      return false;
    }

    const isLegacyDefault =
      position.name === "普工操作工" &&
      Array.isArray(position.keywords) &&
      position.keywords.includes("组装工") &&
      position.keywords.includes("包装工") &&
      position.description ===
        "1 普工操作工\n2 长白班两班倒\n3 服从安排\n4 组装工\n5 包装工";

    return !isLegacyDefault;
  });
}

/** 规范化设置对象，填入默认值 */
function normalizeSettings(payload: any): Settings {
  const base = createDefaultSettings();
  if (!payload || typeof payload !== "object") return base;

  const sanitizedPositions = stripLegacyDefaultPositions(payload.positions);
  const currentPositionName: string =
    typeof payload.currentPosition === "string"
      ? payload.currentPosition
      : payload.currentPosition?.name || payload.currentPositionName || "";

  const { ai_config, ai_expire_time, extra_settings, ...rest } = payload;

  return {
    ...base,
    ...rest,
    currentPositionName: currentPositionName || base.currentPositionName,
    positions: sanitizedPositions.length ? sanitizedPositions : base.positions,
    communicationConfig: {
      ...base.communicationConfig,
      ...(payload.communicationConfig || {}),
    },
    companyInfo: {
      ...base.companyInfo,
      ...(payload.companyInfo || {}),
    },
    jobInfo: {
      ...base.jobInfo,
      ...(payload.jobInfo || {}),
    },
    runModeConfig: {
      ...base.runModeConfig,
      ...(payload.runModeConfig || {}),
    },
    aiConfig: {
      ...base.aiConfig,
      ...(ai_config || payload.aiConfig || {}),
    },
    aiExpireTime: ai_expire_time || payload.aiExpireTime || base.aiExpireTime,
  };
}

/** 使用系统默认值填充空的 AI 配置字段 */
function fillDefaultsFromSystemConfig(
  settings: Settings,
  systemConfig: any,
): void {
  if (
    !settings.aiConfig.clickPrompt?.trim() &&
    systemConfig.default_click_prompt
  ) {
    settings.aiConfig.clickPrompt = systemConfig.default_click_prompt;
  }
  if (!settings.aiConfig.model && systemConfig.default_model) {
    settings.aiConfig.model = "";
  }
}

/** UI 状态接口 */
export interface UIState {
  ready: boolean;
  binding: boolean;
  saving: boolean;
  running: boolean;
  optimizing: boolean;
  activeView: "main" | "logs";
  configTab: "runtime" | "ai";
  configExpanded: boolean;
  identityInput: string;
  positionDraft: string;
  includeDraft: string;
  excludeDraft: string;
  systemConfig: SystemConfig;
}

// ════════════════════════════════════════════════
// 模块级单例状态
// ════════════════════════════════════════════════

const settings = reactive<Settings>(createDefaultSettings());
const ui = reactive<UIState>({
  ready: false,
  binding: false,
  saving: false,
  running: false,
  optimizing: false,
  activeView: "main",
  configTab: "runtime",
  configExpanded: false,
  identityInput: "",
  positionDraft: "",
  includeDraft: "",
  excludeDraft: "",
  systemConfig: {
    website_url: "http://goodhr.58it.cn",
    contact_url: "http://58it.cn",
    donate_url: "http://58it.cn",
    share_url: "http://goodhr.58it.cn",
    announcement: ["免费版用于关键词筛选，AI版用于岗位说明智能判断。"],
    default_click_prompt: "",
    default_model: "",
    optimize_prompt: "",
    models: [],
    ads: [],
    update_info: {
      version: APP_VERSION,
      content: "优化配置结构与广告位展示。",
      force_update: false,
    },
  },
});
const logs = reactive<LogEntry[]>([...DEFAULT_LOGS]);

let autoSaveTimer: ReturnType<typeof setTimeout> | null = null;
let hasShownUpdatePrompt = false;
let initialized = false;
let detachLogs = () => {};

const currentPosition = computed<Position | null>(() => {
  return (
    settings.positions.find(
      (item) => item.name === settings.currentPositionName,
    ) ||
    settings.positions[0] ||
    null
  );
});

const effectiveClickPrompt = computed<string>(() => {
  return (
    settings.aiConfig.clickPrompt?.trim() ||
    ui.systemConfig.default_click_prompt ||
    ""
  );
});

const effectiveApiKey = computed<string>(() => {
  return settings.aiConfig.apiKey || "";
});

const effectiveModel = computed<string>(() => {
  return settings.aiConfig.model || ui.systemConfig.default_model || "";
});

const availableModels = computed(() => {
  return Array.isArray(ui.systemConfig.models) ? ui.systemConfig.models : [];
});

// ════════════════════════════════════════════════
// 方法
// ════════════════════════════════════════════════

/** 重置点击 Prompt 为系统默认 */
function resetClickPrompt(): void {
  const defaultPrompt = ui.systemConfig.default_click_prompt || "";
  settings.aiConfig.clickPrompt = defaultPrompt;
  pushLog("Prompt 已重置为系统默认", "success");
}

/** 验证 Prompt 是否包含必要的标记符 */
function validateClickPrompt(prompt: string): boolean {
  const value = (prompt || "").trim();
  if (!value.includes("${候选人信息}") || !value.includes("${岗位信息}")) {
    return false;
  }
  return true;
}

/** 添加一条日志 */
function pushLog(message: string, type: LogEntry["type"] = "info"): void {
  logs.push({ type, message, time: now() });
  if (logs.length > MAX_LOGS) {
    logs.splice(0, logs.length - MAX_LOGS);
  }
  storageSet({ [LOGS_KEY]: logs.slice() }).catch((e: Error) =>
    console.error("保存日志失败:", e),
  );
}

/** 打开更新页面 */
function openUpdatePage(): void {
  const url =
    ui.systemConfig.update_info?.download_url || ui.systemConfig.website_url;
  if (!url) {
    return;
  }
  globalThis.open(url, "_blank", "noopener,noreferrer");
}

/** 显示原生更新提示弹窗 */
function showNativeUpdatePrompt(): void {
  if (hasShownUpdatePrompt) {
    return;
  }

  const updateInfo = ui.systemConfig.update_info;
  if (!updateInfo || typeof updateInfo !== "object") {
    return;
  }

  const version = String(updateInfo.version || "").trim();
  const content = String(updateInfo.content || "").trim();
  if (!version && !content) {
    return;
  }
  const currentVersion = getManifestVersion();
  if (!version || compareVersions(version, currentVersion) <= 0) {
    return;
  }

  hasShownUpdatePrompt = true;

  const title = updateInfo.force_update ? "强制更新" : "版本更新";
  const message = [title, version ? `版本：v${version}` : "", content]
    .filter(Boolean)
    .join("\n");

  if (updateInfo.force_update) {
    globalThis.alert(message);
    openUpdatePage();
    return;
  }

  const shouldUpdate = globalThis.confirm(`${message}\n\n是否前往更新？`);
  if (shouldUpdate) {
    openUpdatePage();
  }
}

/** 确保当前选中岗位有效 */
function ensureCurrentPosition(): void {
  if (!settings.positions.length) {
    settings.currentPositionName = "";
    return;
  }
  if (
    settings.currentPositionName &&
    settings.positions.some(
      (item) => item.name === settings.currentPositionName,
    )
  ) {
    return;
  }
  if (!settings.currentPositionName) {
    settings.currentPositionName = settings.positions[0].name;
  }
}

/** 新增岗位 */
function addPosition(): void {
  const name = ui.positionDraft.trim();
  if (!name) {
    pushLog("请输入岗位名称", "warning");
    return;
  }
  if (settings.positions.some((item) => item.name === name)) {
    pushLog("岗位已存在", "warning");
    return;
  }
  settings.positions.push(createEmptyPosition(name));
  settings.currentPositionName = name;
  ui.positionDraft = "";
  pushLog(`已新增岗位 ${name}`, "success");
}

/** 删除岗位 */
function removePosition(name: string): void {
  settings.positions = settings.positions.filter((item) => item.name !== name);
  ensureCurrentPosition();
  pushLog(`已删除岗位 ${name}`, "warning");
}

/** 添加关键词或排除词 */
function addKeyword(kind: "include" | "exclude"): void {
  if (!currentPosition.value) return;
  const draft =
    kind === "include" ? ui.includeDraft.trim() : ui.excludeDraft.trim();
  if (!draft) return;
  const list =
    kind === "include"
      ? currentPosition.value.keywords
      : currentPosition.value.excludeKeywords;
  if (!list.includes(draft)) {
    list.push(draft);
    pushLog(
      `已添加${kind === "include" ? "关键词" : "排除词"} ${draft}`,
      "success",
    );
  }
  if (kind === "include") ui.includeDraft = "";
  else ui.excludeDraft = "";
}

/** 移除关键词或排除词 */
function removeKeyword(kind: "include" | "exclude", keyword: string): void {
  if (!currentPosition.value) return;
  const key = kind === "include" ? "keywords" : "excludeKeywords";
  currentPosition.value[key] = currentPosition.value[key].filter(
    (item) => item !== keyword,
  );
}

/** 同步设置到本地存储 */
async function syncToLocalStorage(): Promise<void> {
  const payload = deepClone(settings);
  await storageSet({
    [STORAGE_KEY]: payload,
    [IDENTITY_KEY]: settings.identity,
    hr_assistant_settings: {
      positions: payload.positions,
      currentPosition: currentPosition.value || null,
      isAndMode: payload.isAndMode,
      matchLimit: payload.matchLimit,
      enableSound: payload.enableSound,
      scrollDelayMin: payload.scrollDelayMin,
      scrollDelayMax: payload.scrollDelayMax,
      clickFrequency: payload.clickFrequency,
      communicationConfig: payload.communicationConfig,
      companyInfo: payload.companyInfo,
      jobInfo: payload.jobInfo,
      runModeConfig: payload.runModeConfig,
    },
    ai_config: payload.aiConfig,
    ai_expire_time: payload.aiExpireTime,
    selected_tab: payload.runMode,
    hr_assistant_identity: payload.identity,
    hr_assistant_phone:
      payload.identityType === "phone" ? payload.identity : "",
  });
}

/** 绑定账号 */
async function bindAccount(): Promise<void> {
  const identifier = ui.identityInput.trim();
  if (!identifier) {
    pushLog("请输入邮箱或手机号", "error");
    return;
  }
  ui.binding = true;
  try {
    const response = await bindIdentity(identifier);
    const normalized = normalizeSettings(response.settings);
    Object.assign(settings, normalized, {
      identity: response.account.identifier,
      identityType: response.account.identity_type,
      version: getManifestVersion(),
    });
    ui.identityInput = response.account.identifier;
    await syncAuthProfile();
    await syncToLocalStorage();
    pushLog(
      `绑定成功，已自动${response.created ? "注册" : "同步"}账号`,
      "success",
    );
  } catch (error: any) {
    pushLog(error.message, "error");
  } finally {
    ui.binding = false;
  }
}

/** 保存设置到远程 */
async function saveRemote(options: { silent?: boolean } = {}): Promise<void> {
  const { silent = false } = options;
  if (!settings.identity) {
    if (!silent) {
      pushLog("请先绑定邮箱或手机号", "error");
    }
    return;
  }
  ui.saving = true;
  try {
    await saveSettings(settings.identity, deepClone(settings));
    await syncToLocalStorage();
    const effectiveSettings = deepClone(settings);
    effectiveSettings.aiConfig.clickPrompt = effectiveClickPrompt.value;
    effectiveSettings.aiConfig.apiKey = effectiveApiKey.value;
    effectiveSettings.aiConfig.model = effectiveModel.value;
    await pushSettingsToPage(effectiveSettings, currentPosition.value!);
    if (!silent) {
      pushLog("配置已保存到后端", "success");
    }
  } catch (error: any) {
    pushLog(error.message, "error");
  } finally {
    ui.saving = false;
  }
}

/** 请求自动保存（防抖） */
function requestAutoSave(): void {
  if (!ui.ready || !settings.identity) return;
  globalThis.clearTimeout(autoSaveTimer as any);
  autoSaveTimer = globalThis.setTimeout(() => {
    saveRemote({ silent: true });
  }, 180);
}

/** 从后端刷新配置 */
async function reloadRemote(): Promise<void> {
  if (!settings.identity) return;
  try {
    const response = await fetchSettings(settings.identity);
    Object.assign(settings, normalizeSettings(response.settings), {
      identity: response.account.identifier,
      identityType: response.account.identity_type,
      version: getManifestVersion(),
    });
    await syncAuthProfile({ silent: true });
    await syncToLocalStorage();
    pushLog("已从后端刷新配置", "success");
  } catch (error: any) {
    pushLog(`刷新失败: ${error.message}`, "error");
  }
}

/** 加载系统配置 */
async function loadSystemConfig(): Promise<void> {
  try {
    const response = await fetchSystemConfig("frontend");
    Object.assign(ui.systemConfig, response.config?.config_value || {});
    fillDefaultsFromSystemConfig(settings, ui.systemConfig);
    showNativeUpdatePrompt();
  } catch (error: any) {
    pushLog(`系统配置加载失败: ${error.message}`, "warning");
  }
}

/** 启动运行 */
async function startRunAction(): Promise<void> {
  if (!currentPosition.value) {
    pushLog("请先创建岗位", "error");
    return;
  }
  if (settings.runMode === "ai" && !currentPosition.value.description.trim()) {
    pushLog("AI模式需要填写岗位说明", "error");
    return;
  }
  if (
    settings.runMode === "ai" &&
    !validateClickPrompt(effectiveClickPrompt.value)
  ) {
    pushLog(
      "⚠️ Prompt 必须包含 ${候选人信息} 和 ${岗位信息} 标记符，请检查或点击重置",
      "error",
    );
    return;
  }
  try {
    await syncToLocalStorage();
    const effectiveSettings = deepClone(settings);
    effectiveSettings.aiConfig.clickPrompt = effectiveClickPrompt.value;
    effectiveSettings.aiConfig.apiKey = effectiveApiKey.value;
    effectiveSettings.aiConfig.model = effectiveModel.value;
    await startRunOnPage(effectiveSettings, currentPosition.value!);
    ui.running = true;
    ui.activeView = "logs";
    pushLog(
      `已启动${settings.runMode === "ai" ? "AI" : "关键词"}模式`,
      "success",
    );
  } catch (error: any) {
    pushLog(`启动失败: ${error.message}`, "error");
  }
}

/** 停止运行 */
async function stopRunAction(): Promise<void> {
  try {
    await stopRunOnPage();
  } catch (_) {
    // ignore
  }
  ui.running = false;
  pushLog("运行已停止", "warning");
}

/** 使用 AI 优化岗位说明 */
async function optimizeJobDescription(): Promise<void> {
  if (!currentPosition.value) {
    pushLog("请先选择岗位", "error");
    return;
  }
  const description = currentPosition.value.description?.trim();
  if (!description) {
    pushLog("请先填写岗位说明", "warning");
    return;
  }

  ui.optimizing = true;
  pushLog("正在使用AI优化岗位说明...", "info");

  try {
    const optimizePrompt = (ui.systemConfig.optimize_prompt || "").replace(
      /\$\{岗位要求\}/g,
      description,
    );

    const messages = [{ role: "user", content: optimizePrompt }];

    const result = await chatWithAI({ messages, temperature: 0.7 });
    currentPosition.value.description = result.trim();
    pushLog("岗位说明优化完成", "success");
  } catch (error: any) {
    pushLog(`AI优化失败: ${error.message}`, "error");
  } finally {
    ui.optimizing = false;
  }
}

/** 同步 AI 认证资料 */
async function syncAuthProfile(
  options: { silent?: boolean } = {},
): Promise<void> {
  const { silent = false } = options;
  const email = resolveRegisterEmail(settings.identity || ui.identityInput);
  if (!email) {
    return;
  }
  try {
    const authData = await registerAuthUser(email);
    const apiKey = authData.api_key?.key || "";
    if (apiKey) {
      settings.aiConfig.apiKey = apiKey;
    }
    const balance: number | null =
      authData.cny_balance ??
      authData.balance ??
      authData.user?.balance ??
      authData.account?.balance ??
      null;
    settings.aiBalance = balance;
    settings.aiBalanceText = formatBalance(balance);
    settings.authUser = authData.user || null;
    settings.authApiKey = authData.api_key || null;
    if (!silent) {
      pushLog("已同步 AI 账号信息与秘钥", "success");
    }
  } catch (error: any) {
    if (!silent) {
      pushLog(`AI账号同步失败: ${error.message}`, "warning");
    }
  }
}

/** 初始化：加载存储、系统配置、注册 watch 等，只执行一次 */
async function initOnce(): Promise<void> {
  if (initialized) return;
  initialized = true;

  const stored = await storageGet([STORAGE_KEY, IDENTITY_KEY, LOGS_KEY]);
  await loadSystemConfig();
  if (stored[STORAGE_KEY]) {
    Object.assign(settings, normalizeSettings(stored[STORAGE_KEY]));
  }
  if (Array.isArray(stored[LOGS_KEY]) && stored[LOGS_KEY].length) {
    logs.splice(0, logs.length, ...stored[LOGS_KEY]);
  }
  settings.version = getManifestVersion();
  settings.identity = stored[IDENTITY_KEY] || settings.identity || "";
  settings.identityType = settings.identity
    ? inferIdentityType(settings.identity)
    : "";
  ui.identityInput = settings.identity;
  ensureCurrentPosition();
  if (settings.identity) {
    await reloadRemote();
  }
  await syncAuthProfile({ silent: true });
  await syncToLocalStorage();
  detachLogs = attachRuntimeLogListener((entry: any) => {
    pushLog(entry.message, entry.type || "info");
  });
  ui.ready = true;
}

/** 清理：移除监听器和定时器 */
function cleanup(): void {
  detachLogs();
  globalThis.clearTimeout(autoSaveTimer as any);
}

// ════════════════════════════════════════════════
// 模块级 watch（只注册一次）
// ════════════════════════════════════════════════

watch(
  settings,
  async () => {
    if (!ui.ready) return;
    ensureCurrentPosition();
    await syncToLocalStorage();
    requestAutoSave();
  },
  { deep: true },
);

watch(
  [effectiveApiKey, effectiveModel],
  ([apiKey, model]) => {
    configureAI({ apiKey, model });
  },
  { immediate: true },
);

// 首次导入时自动初始化
initOnce();

// ════════════════════════════════════════════════
// 导出
// ════════════════════════════════════════════════

/**
 * 面板状态管理 composable
 * 返回模块级单例的所有响应式状态和方法，确保跨组件共享
 */
export function usePanelStore() {
  return {
    settings,
    ui,
    logs,
    effectiveClickPrompt,
    effectiveApiKey,
    effectiveModel,
    availableModels,
    currentPosition,
    addPosition,
    removePosition,
    addKeyword,
    removeKeyword,
    bindAccount,
    saveRemote,
    requestAutoSave,
    reloadRemote,
    resetClickPrompt,
    validateClickPrompt,
    optimizeJobDescription,
    pushLog,
    startRun: startRunAction,
    stopRun: stopRunAction,
    cleanup,
  };
}
