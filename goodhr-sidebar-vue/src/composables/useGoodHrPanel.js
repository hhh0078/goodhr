import { watch } from "vue";
import { STORAGE_KEY } from "../constants/goodhr.js";
import { getConfig, getManifestVersion, storageSet } from "../lib/chrome.js";
import { useGoodHrState } from "./useGoodHrState.js";
import {
  checkAiBalance,
  createAiAccount,
  fetchAiModels,
  fetchRankingData,
  fetchServerSettings,
  loadBootstrapStorage,
  saveAiModel,
  saveBoundPhone,
  updateServerSettings,
} from "../services/goodhrApi.js";

export function useGoodHrPanel() {
  const store = useGoodHrState();
  const {
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
    addPosition: addPositionState,
    removePosition,
    selectPosition,
    addKeyword: addKeywordState,
    removeKeyword,
  } = store;

  async function loadRankingList() {
    ui.loadingRanking = true;
    ui.rankingError = "";
    try {
      const data = await fetchRankingData();
      rankings.value = Array.isArray(data) ? data : [];
    } catch (error) {
      ui.rankingError = error.message;
      rankings.value = [];
      pushLog(`加载打赏记录失败: ${error.message}`, "error");
    } finally {
      ui.loadingRanking = false;
    }
  }

  async function loadModelCards() {
    if (!state.phone) {
      ui.modelCards = [];
      return;
    }

    try {
      const data = await fetchAiModels(state.phone);
      if (!data.success || data.code !== 200) {
        throw new Error(data.message || "获取模型列表失败");
      }
      ui.modelCards = data.data.models.map((model) => ({
        name: model.name,
        description: model.description || "暂无描述",
        inputPrice: `输入: ¥${model.input_price}/M`,
        outputPrice: `输出: ¥${model.output_price}/M`,
        ratio: model.ratio,
      }));
      state.aiConfig.model = data.data.model || state.aiConfig.model;
    } catch (error) {
      pushLog(`模型列表加载失败: ${error.message}`, "warning");
      ui.modelCards = [
        {
          name: state.aiConfig.model,
          description: "当前默认模型",
          inputPrice: "输入: --",
          outputPrice: "输出: --",
          ratio: "--",
        },
      ];
    }
  }

  async function syncSettingsFromServer() {
    if (!state.phone) return false;
    const data = await fetchServerSettings(state.phone);
    if (data && Object.keys(data).length > 0) {
      applyServerPayload(data);
      pushLog("已从服务器同步配置", "success");
      return true;
    }
    return false;
  }

  function buildServerPayload() {
    return {
      positions: state.positions,
      currentPosition: currentPosition.value || null,
      isAndMode: state.isAndMode,
      matchLimit: state.matchLimit,
      enableSound: state.enableSound,
      scrollDelayMin: state.scrollDelayMin,
      scrollDelayMax: state.scrollDelayMax,
      clickFrequency: state.clickFrequency,
      communicationConfig: state.communicationConfig,
      ai_config: state.aiConfig,
      ai_expire_time: state.aiExpireTime,
    };
  }

  async function persist() {
    await storageSet({
      [STORAGE_KEY]: JSON.parse(JSON.stringify(state)),
      hr_assistant_phone: state.phone,
      selected_tab: state.currentTab,
      hr_assistant_settings: {
        positions: state.positions,
        currentPosition: currentPosition.value || null,
        isAndMode: state.isAndMode,
        matchLimit: state.matchLimit,
        enableSound: state.enableSound,
        scrollDelayMin: state.scrollDelayMin,
        scrollDelayMax: state.scrollDelayMax,
        clickFrequency: state.clickFrequency,
        communicationConfig: state.communicationConfig,
      },
      ai_config: state.aiConfig,
      ai_expire_time: state.aiExpireTime,
    });
  }

  async function handleBalanceCheck() {
    if (!state.phone) {
      ui.aiStatusText = "未绑定手机号";
      ui.aiBalanceText = "余额: --";
      return;
    }

    ui.checkingBalance = true;
    ui.aiStatusText = "连接中...";
    try {
      const response = await checkAiBalance(state.phone);
      const balance = parseFloat(response?.data?.balance) || 0;
      ui.aiStatusText = "已连接";
      ui.aiBalanceText = `余额: ¥${balance.toFixed(4)}`;
      pushLog(`账号余额: ¥${balance.toFixed(4)}`, "info");
    } catch (error) {
      ui.aiStatusText = "连接失败";
      ui.aiBalanceText = "余额: --";
      pushLog(`余额检查失败: ${error.message}`, "error");
    } finally {
      ui.checkingBalance = false;
    }
  }

  async function ensureAiTrial() {
    if (state.aiExpireTime || !state.phone) return;
    const expireDate = new Date();
    expireDate.setDate(expireDate.getDate() + 3);
    state.aiExpireTime = expireDate.toISOString().split("T")[0];
    await updateServerSettings(state.phone, buildServerPayload());
    pushLog("赠送AI版本3天试用期", "success");
  }

  async function bindPhone(phone, mode = "free") {
    ui.bindingPhone = true;
    try {
      if (!phone || !/^1\d{10}$/.test(phone)) {
        throw new Error("请输入正确的手机号");
      }

      state.phone = phone;
      await saveBoundPhone(phone);

      try {
        pushLog("正在创建AI平台账号...", "info");
        const createResponse = await createAiAccount(phone);
        const balance = createResponse?.data?.balance;
        if (balance !== undefined) {
          pushLog(`AI平台账号创建成功，初始余额: ¥${balance}`, "success");
        }
      } catch (error) {
        pushLog(`AI平台账号创建失败: ${error.message}`, "warning");
      }

      const hasServerData = await syncSettingsFromServer();
      if (hasServerData) {
        pushLog(`已从手机号 ${phone} 同步配置`, "success");
      } else {
        pushLog(`手机号 ${phone} 绑定成功，暂无配置数据`, "success");
      }

      await ensureAiTrial();
      await loadModelCards();

      if (mode === "ai" || state.currentTab === "ai") {
        await handleBalanceCheck();
      }
    } catch (error) {
      pushLog(error.message, "error");
      throw error;
    } finally {
      ui.bindingPhone = false;
    }
  }

  async function saveAiConfig() {
    if (!state.phone) {
      pushLog("未绑定手机号", "error");
      return;
    }
    if (
      !state.aiConfig.clickPrompt.includes("${候选人信息}") ||
      !state.aiConfig.clickPrompt.includes("${岗位信息}")
    ) {
      pushLog(
        "查看候选人详情提示语必须包含${候选人信息}和${岗位信息}标记符",
        "error",
      );
      return;
    }

    await saveAiModel(state.phone, state.aiConfig.model);
    await updateServerSettings(state.phone, buildServerPayload());
    ui.showAiConfig = false;
    pushLog("AI配置已保存", "success");
    await handleBalanceCheck();
  }

  async function hydrate() {
    state.version = getConfig().VERSION || getManifestVersion();
    const result = await loadBootstrapStorage(STORAGE_KEY);

    if (result[STORAGE_KEY]) {
      Object.assign(state, result[STORAGE_KEY]);
    }
    if (result.hr_assistant_phone) {
      state.phone = result.hr_assistant_phone;
    }
    if (result.selected_tab) {
      state.currentTab = result.selected_tab;
    }
    if (result.hr_assistant_settings) {
      applyServerPayload({
        ...result.hr_assistant_settings,
        ai_config: result.ai_config,
        ai_expire_time: result.ai_expire_time,
      });
    }

    ensurePositionSelected();

    if (state.phone) {
      try {
        await syncSettingsFromServer();
      } catch (error) {
        pushLog(`同步配置失败: ${error.message}`, "error");
      }
    }

    await Promise.all([
      loadRankingList(),
      state.phone ? loadModelCards() : Promise.resolve(),
      state.currentTab === "ai" && state.phone
        ? handleBalanceCheck()
        : Promise.resolve(),
    ]);

    ui.ready = true;
  }

  function addPosition(fromAi = false) {
    const result = addPositionState(fromAi);
    if (!result.ok) {
      pushLog(result.message, "error");
      return;
    }
    pushLog(result.message, "success");
  }

  function addKeyword(type = "include") {
    const value = addKeywordState(type);
    if (!value) return;
    pushLog(
      type === "exclude" ? `已添加排除词：${value}` : `已添加关键词：${value}`,
      "success",
    );
  }

  function startRun() {
    if (!currentPosition.value) {
      pushLog("请先选择岗位", "error");
      return;
    }
    ui.isRunning = true;
    pushLog(
      `开始${state.currentTab === "ai" ? "AI" : "免费"}模式，岗位：${currentPosition.value.name}`,
      "info",
    );
  }

  function stopRun() {
    ui.isRunning = false;
    pushLog("已停止运行", "warning");
  }

  watch(
    state,
    () => {
      if (ui.ready) {
        persist();
      }
    },
    { deep: true },
  );

  watch(
    () => state.currentTab,
    async (tab) => {
      if (!ui.ready) return;
      pushLog(`切换到${tab === "ai" ? "AI高级版" : "免费版"}`, "info");
      if (tab === "ai" && state.phone) {
        await handleBalanceCheck();
      }
    },
  );

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
    hydrate,
    addPosition,
    removePosition,
    selectPosition,
    addKeyword,
    removeKeyword,
    bindPhone,
    saveAiConfig,
    startRun,
    stopRun,
    loadRankingData: loadRankingList,
  };
}
