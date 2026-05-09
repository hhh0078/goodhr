import { computed, onMounted, onUnmounted, reactive, watch } from "vue";
import {
  createDefaultSettings,
  createEmptyPosition,
  DEFAULT_LOGS,
  IDENTITY_KEY,
  STORAGE_KEY,
} from "../constants/defaults.js";
import {
  bindIdentity,
  fetchSettings,
  fetchSystemConfig,
  registerAuthUser,
  saveSettings,
} from "../services/api.js";
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

function now() {
  return new Date().toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
}

function inferIdentityType(identifier) {
  return identifier.includes("@") ? "email" : "phone";
}

function resolveRegisterEmail(identifier) {
  const value = (identifier || "").trim();
  if (!value) return "";
  if (value.includes("@")) return value.toLowerCase();
  return `${value}@phone.goodhr.local`;
}

function formatBalance(balance) {
  if (balance === null || balance === undefined || balance === "") return "";
  const numeric = Number(balance);
  if (Number.isFinite(numeric)) {
    return `¥ ${numeric.toFixed(2)}`;
  }
  return `¥ ${balance}`;
}

function parseVersionPart(part) {
  const value = Number(part);
  return Number.isFinite(value) ? value : 0;
}

function compareVersions(a, b) {
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

function stripLegacyDefaultPositions(positions) {
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

function normalizeSettings(payload) {
  const base = createDefaultSettings();
  if (!payload || typeof payload !== "object") return base;

  const sanitizedPositions = stripLegacyDefaultPositions(payload.positions);
  const currentPositionName =
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

/**
 * 使用系统默认值填充空的AI配置字段
 */
function fillDefaultsFromSystemConfig(settings, systemConfig) {
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

export function usePanelStore() {
  const settings = reactive(createDefaultSettings());
  const ui = reactive({
    ready: false,
    binding: false,
    saving: false,
    running: false,
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
      models: [],
      ads: [],
      update_info: {
        version: APP_VERSION,
        content: "优化配置结构与广告位展示。",
        force_update: false,
      },
    },
  });
  const logs = reactive([...DEFAULT_LOGS]);

  let autoSaveTimer = null;
  let hasShownUpdatePrompt = false;

  const currentPosition = computed(() => {
    return (
      settings.positions.find(
        (item) => item.name === settings.currentPositionName,
      ) ||
      settings.positions[0] ||
      null
    );
  });

  const effectiveClickPrompt = computed(() => {
    return (
      settings.aiConfig.clickPrompt?.trim() ||
      ui.systemConfig.default_click_prompt ||
      ""
    );
  });

  const effectiveApiKey = computed(() => {
    return settings.aiConfig.apiKey || "";
  });

  const effectiveModel = computed(() => {
    return settings.aiConfig.model || ui.systemConfig.default_model || "";
  });

  const availableModels = computed(() => {
    return Array.isArray(ui.systemConfig.models) ? ui.systemConfig.models : [];
  });

  function resetClickPrompt() {
    const defaultPrompt = ui.systemConfig.default_click_prompt || "";
    settings.aiConfig.clickPrompt = defaultPrompt;
    pushLog("Prompt 已重置为系统默认", "success");
  }

  function validateClickPrompt(prompt) {
    const value = (prompt || "").trim();
    if (!value.includes("${候选人信息}") || !value.includes("${岗位信息}")) {
      return false;
    }
    return true;
  }

  function pushLog(message, type = "info") {
    logs.push({ type, message, time: now() });
    if (logs.length > 120) {
      logs.splice(0, logs.length - 120);
    }
  }

  function openUpdatePage() {
    const url =
      ui.systemConfig.update_info?.download_url || ui.systemConfig.website_url;
    if (!url) {
      return;
    }
    globalThis.open(url, "_blank", "noopener,noreferrer");
  }

  function showNativeUpdatePrompt() {
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

  function ensureCurrentPosition() {
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

  function addPosition() {
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

  function removePosition(name) {
    settings.positions = settings.positions.filter(
      (item) => item.name !== name,
    );
    ensureCurrentPosition();
    pushLog(`已删除岗位 ${name}`, "warning");
  }

  function addKeyword(kind) {
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

  function removeKeyword(kind, keyword) {
    if (!currentPosition.value) return;
    const key = kind === "include" ? "keywords" : "excludeKeywords";
    currentPosition.value[key] = currentPosition.value[key].filter(
      (item) => item !== keyword,
    );
  }

  async function syncToLocalStorage() {
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

  async function bindAccount() {
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
    } catch (error) {
      pushLog(error.message, "error");
    } finally {
      ui.binding = false;
    }
  }

  async function saveRemote(options = {}) {
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
      await pushSettingsToPage(effectiveSettings, currentPosition.value);
      if (!silent) {
        pushLog("配置已保存到后端", "success");
      }
    } catch (error) {
      pushLog(error.message, "error");
    } finally {
      ui.saving = false;
    }
  }

  function requestAutoSave() {
    if (!ui.ready || !settings.identity) return;
    globalThis.clearTimeout(autoSaveTimer);
    autoSaveTimer = globalThis.setTimeout(() => {
      saveRemote({ silent: true });
    }, 180);
  }

  async function reloadRemote() {
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
    } catch (error) {
      pushLog(`刷新失败: ${error.message}`, "error");
    }
  }

  async function loadSystemConfig() {
    try {
      const response = await fetchSystemConfig("frontend");
      Object.assign(ui.systemConfig, response.config?.config_value || {});
      fillDefaultsFromSystemConfig(settings, ui.systemConfig);
      showNativeUpdatePrompt();
    } catch (error) {
      pushLog(`系统配置加载失败: ${error.message}`, "warning");
    }
  }

  async function startRun() {
    if (!currentPosition.value) {
      pushLog("请先创建岗位", "error");
      return;
    }
    if (
      settings.runMode === "ai" &&
      !currentPosition.value.description.trim()
    ) {
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
      await startRunOnPage(effectiveSettings, currentPosition.value);
      ui.running = true;
      pushLog(
        `已启动${settings.runMode === "ai" ? "AI" : "关键词"}模式`,
        "success",
      );
    } catch (error) {
      pushLog(`启动失败: ${error.message}`, "error");
    }
  }

  async function stopRun() {
    try {
      await stopRunOnPage();
    } catch (_) {
      // ignore
    }
    ui.running = false;
    pushLog("运行已停止", "warning");
  }

  let detachLogs = () => {};

  async function syncAuthProfile(options = {}) {
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
      const balance =
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
    } catch (error) {
      if (!silent) {
        pushLog(`AI账号同步失败: ${error.message}`, "warning");
      }
    }
  }

  onMounted(async () => {
    const stored = await storageGet([STORAGE_KEY, IDENTITY_KEY]);
    await loadSystemConfig();
    if (stored[STORAGE_KEY]) {
      Object.assign(settings, normalizeSettings(stored[STORAGE_KEY]));
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
    detachLogs = attachRuntimeLogListener((entry) => {
      pushLog(entry.message, entry.type || "info");
    });
    ui.ready = true;
  });

  onUnmounted(() => {
    detachLogs();
    globalThis.clearTimeout(autoSaveTimer);
  });

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
    startRun,
    stopRun,
  };
}
