import { computed, reactive, ref } from "vue";
import {
  createDefaultState,
  DEFAULT_LOGS,
} from "../constants/goodhr.js";

function timestamp() {
  return new Date().toLocaleTimeString("zh-CN", {
    hour12: false,
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function useGoodHrState() {
  const state = reactive(createDefaultState());
  const ui = reactive({
    ready: false,
    isRunning: false,
    isDownloading: false,
    logExpanded: false,
    showAiConfig: false,
    bindingPhone: false,
    checkingBalance: false,
    keywordDraft: "",
    excludeKeywordDraft: "",
    positionDraft: "",
    aiPositionDraft: "",
    loadingRanking: false,
    rankingError: "",
    modelCards: [],
    aiStatusText: "未连接",
    aiBalanceText: "余额: --",
  });
  const rankings = ref([]);
  const logs = ref([...DEFAULT_LOGS]);

  const currentPosition = computed(
    () =>
      state.positions.find((position) => position.name === state.currentPositionName) ||
      null,
  );

  const freeKeywords = computed(() => currentPosition.value?.keywords || []);
  const freeExcludeKeywords = computed(
    () => currentPosition.value?.excludeKeywords || [],
  );

  function pushLog(message, type = "info") {
    const prefixMap = {
      info: ">",
      success: "√",
      warning: "?",
      error: "!",
    };

    logs.value.push({
      type,
      message,
      prefix: prefixMap[type] || ">",
      time: timestamp(),
    });

    if (logs.value.length > 100) {
      logs.value.splice(0, logs.value.length - 100);
    }
  }

  function clearLogs() {
    logs.value = [...DEFAULT_LOGS];
  }

  function ensurePositionSelected() {
    if (!state.currentPositionName && state.positions[0]) {
      state.currentPositionName = state.positions[0].name;
    }
  }

  function normalizeCurrentPositionName(value) {
    if (!value) return "";
    return typeof value === "string" ? value : value.name || "";
  }

  function applyServerPayload(data) {
    if (!data || typeof data !== "object") {
      return false;
    }

    if (Array.isArray(data.positions)) {
      state.positions = data.positions;
    }

    state.currentPositionName = normalizeCurrentPositionName(data.currentPosition);

    if (typeof data.isAndMode === "boolean") state.isAndMode = data.isAndMode;
    if (typeof data.matchLimit === "number") state.matchLimit = data.matchLimit;
    if (typeof data.enableSound === "boolean") state.enableSound = data.enableSound;
    if (typeof data.scrollDelayMin === "number") state.scrollDelayMin = data.scrollDelayMin;
    if (typeof data.scrollDelayMax === "number") state.scrollDelayMax = data.scrollDelayMax;
    if (typeof data.clickFrequency === "number") state.clickFrequency = data.clickFrequency;

    if (data.communicationConfig) {
      state.communicationConfig = {
        ...state.communicationConfig,
        ...data.communicationConfig,
      };
    }

    if (data.ai_config) {
      state.aiConfig = {
        ...state.aiConfig,
        ...data.ai_config,
      };
    }

    if (data.ai_expire_time) {
      state.aiExpireTime = data.ai_expire_time;
    }

    ensurePositionSelected();
    return true;
  }

  function addPosition(fromAi = false) {
    const draft = (fromAi ? ui.aiPositionDraft : ui.positionDraft).trim();
    if (!draft) return { ok: false, message: "请输入岗位名称" };
    if (state.positions.some((item) => item.name === draft)) {
      return { ok: false, message: "该岗位已存在" };
    }

    state.positions.push({
      name: draft,
      keywords: [],
      excludeKeywords: [],
      description: "",
    });
    state.currentPositionName = draft;
    ui.positionDraft = "";
    ui.aiPositionDraft = "";
    return { ok: true, message: `已添加岗位：${draft}` };
  }

  function removePosition(name) {
    state.positions = state.positions.filter((item) => item.name !== name);
    if (state.currentPositionName === name) {
      state.currentPositionName = state.positions[0]?.name || "";
    }
  }

  function selectPosition(name) {
    state.currentPositionName = name;
  }

  function addKeyword(type = "include") {
    const draft =
      type === "exclude" ? ui.excludeKeywordDraft.trim() : ui.keywordDraft.trim();
    if (!draft || !currentPosition.value) return null;

    const target =
      type === "exclude"
        ? currentPosition.value.excludeKeywords
        : currentPosition.value.keywords;

    if (!target.includes(draft)) {
      target.push(draft);
    }

    ui.keywordDraft = "";
    ui.excludeKeywordDraft = "";
    return draft;
  }

  function removeKeyword(keyword, type = "include") {
    if (!currentPosition.value) return;
    const key = type === "exclude" ? "excludeKeywords" : "keywords";
    currentPosition.value[key] = currentPosition.value[key].filter(
      (item) => item !== keyword,
    );
  }

  return {
    state,
    ui,
    rankings,
    logs,
    currentPosition,
    freeKeywords,
    freeExcludeKeywords,
    pushLog,
    clearLogs,
    ensurePositionSelected,
    applyServerPayload,
    addPosition,
    removePosition,
    selectPosition,
    addKeyword,
    removeKeyword,
  };
}
