import { ref } from "vue";
import {
  getUserAIConfig,
  getUserPreferences,
  updateUserAIConfig,
  updateUserPreferences,
} from "../services/api/personalConfigApi";
import {
  chatWithLocalAI,
  getLocalAIConfig,
  getLocalSettings,
  saveLocalAIConfig,
  saveLocalSettings,
} from "../services/localAgentApi";
import { markOnboardingStep } from "../services/onboarding";

const DEFAULT_AI_BASE_URL =
  "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions";
const DEFAULT_AI_MODEL = "qwen3.7-plus";

export function usePersonalConfig() {
  const loading = ref(false);
  const error = ref("");
  const message = ref("");
  const form = ref(defaultForm());

  async function load() {
    loading.value = true;
    error.value = "";
    try {
      const data = isLocalConsole()
        ? await getLocalSettings(localAgentBase())
        : await getUserPreferences();
      const ai = isLocalConsole()
        ? await getLocalAIConfig(localAgentBase())
        : await getUserAIConfig();
      form.value = {
        aiBaseURL: ai?.base_url || DEFAULT_AI_BASE_URL,
        aiModel: ai?.model || data?.ai_model || DEFAULT_AI_MODEL,
        aiAPIKey: "",
        aiAPIKeyMasked: localAIKeyLabel(ai),
        aiAPIKeySet: isLocalConsole() ? Boolean(ai?.api_key) : Boolean(ai?.api_key_set),
        clickFrequency: data?.click_frequency ?? 80,
        detailOpenProbability: data?.detail_open_probability ?? 80,
        detailOpenDelayMin: data?.detail_open_delay_min ?? 1,
        detailOpenDelayMax: data?.detail_open_delay_max ?? 2,
        detailCloseDelayMin: data?.detail_close_delay_min ?? 0,
        detailCloseDelayMax: data?.detail_close_delay_max ?? 0,
        greetBeforeDelayMin: data?.greet_before_delay_min ?? 1,
        greetBeforeDelayMax: data?.greet_before_delay_max ?? 2,
        restAfterCandidatesMin: data?.rest_after_candidates_min ?? 40,
        restAfterCandidatesMax: data?.rest_after_candidates_max ?? 70,
        restTimesMin: data?.rest_times_min ?? 2,
        restTimesMax: data?.rest_times_max ?? 3,
        restDurationMin: data?.rest_duration_min ?? 2,
        restDurationMax: data?.rest_duration_max ?? 7,
      };
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  async function save() {
    loading.value = true;
    error.value = "";
    message.value = "";
    try {
      await verifyAIBeforeSave();
      if (isLocalConsole()) {
        const localPayload: any = {
          base_url: form.value.aiBaseURL,
          model: form.value.aiModel,
          temperature: 0,
          provider: "openai-compatible",
        };
        if (form.value.aiAPIKey.trim()) {
          localPayload.api_key = form.value.aiAPIKey.trim();
        }
        await saveLocalAIConfig(localAgentBase(), localPayload);
        await saveLocalSettings(localAgentBase(), preferencePayload());
      } else {
        await updateUserAIConfig({
          base_url: form.value.aiBaseURL,
          model: form.value.aiModel,
          api_key: form.value.aiAPIKey.trim(),
          temperature: 0,
          prompt_template: "",
          enabled: true,
        });
        await updateUserPreferences(preferencePayload());
      }
      if (form.value.aiAPIKey.trim()) {
        form.value.aiAPIKey = "";
        form.value.aiAPIKeySet = true;
        form.value.aiAPIKeyMasked = "已更新";
      }
      message.value = isLocalConsole() ? "本地个人配置已保存" : "个人配置已保存";
      await markOnboardingStep("personal_config");
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  /**
   * 保存个人配置前直接请求用户填写的 AI 平台，确认当前配置可用。
   * @returns {Promise<void>} AI 返回包含“成功”时通过，否则抛出错误。
   */
  async function verifyAIBeforeSave() {
    const apiURL = form.value.aiBaseURL.trim();
    const model = form.value.aiModel.trim();
    let apiKey = form.value.aiAPIKey.trim();
    if (!apiURL) throw new Error("请先填写 AI API 地址");
    if (!model) throw new Error("请先填写 AI 模型");

    if (isLocalConsole()) {
      if (!apiKey) {
        const saved = await getLocalAIConfig(localAgentBase());
        apiKey = String(saved?.api_key || "").trim();
      }
      if (!apiKey) throw new Error("请先填写 AI Key");
      const data = await chatWithLocalAI(localAgentBase(), {
        messages: [{ role: "user", content: "请只返回两个字：成功" }],
        temperature: 0,
        config: {
          base_url: apiURL,
          model,
          api_key: apiKey,
        },
      });
      if (!String(data?.content || "").includes("成功")) {
        throw new Error(`AI 测试未通过，返回信息：\n${data?.content || "无返回内容"}`);
      }
      return;
    }
    if (!apiKey) throw new Error("保存前请重新输入 AI Key，用于测试当前配置是否可用");

    const response = await fetch(apiURL, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify({
        model,
        messages: [
          {
            role: "user",
            content: "请只返回两个字：成功",
          },
        ],
        temperature: 0,
        stream: false,
      }),
    });
    const rawText = await response.text();
    const parsed = parseAIResponse(rawText);
    const resultText = extractAIContent(parsed) || rawText;
    if (!response.ok || !String(resultText).includes("成功")) {
      throw new Error(`AI 测试未通过，返回信息：\n${formatAIResponse(parsed, rawText)}`);
    }
  }

  /**
   * 生成个人偏好保存参数。
   * @returns {any} 个人偏好参数。
   */
  function preferencePayload() {
    return {
      ai_model: form.value.aiModel,
      click_frequency: Number(form.value.clickFrequency || 0),
      detail_open_probability: Number(form.value.detailOpenProbability || 0),
      detail_open_delay_min: Number(form.value.detailOpenDelayMin || 0),
      detail_open_delay_max: Number(form.value.detailOpenDelayMax || 0),
      detail_close_delay_min: Number(form.value.detailCloseDelayMin || 0),
      detail_close_delay_max: Number(form.value.detailCloseDelayMax || 0),
      greet_before_delay_min: Number(form.value.greetBeforeDelayMin || 0),
      greet_before_delay_max: Number(form.value.greetBeforeDelayMax || 0),
      rest_after_candidates_min: Number(form.value.restAfterCandidatesMin || 0),
      rest_after_candidates_max: Number(form.value.restAfterCandidatesMax || 0),
      rest_times_min: Number(form.value.restTimesMin || 0),
      rest_times_max: Number(form.value.restTimesMax || 0),
      rest_duration_min: Number(form.value.restDurationMin || 0),
      rest_duration_max: Number(form.value.restDurationMax || 0),
    };
  }

  return { form, loading, error, message, load, save };
}

/**
 * 尝试解析 AI 平台响应 JSON。
 * @param {string} rawText - AI 平台原始响应文本。
 * @returns {any} JSON 对象或 null。
 */
function parseAIResponse(rawText: string) {
  if (!rawText) return null;
  try {
    return JSON.parse(rawText);
  } catch {
    return null;
  }
}

/**
 * 从 OpenAI 兼容响应中提取助手返回文本。
 * @param {any} data - AI 平台响应对象。
 * @returns {string} 助手文本内容。
 */
function extractAIContent(data: any) {
  const content = data?.choices?.[0]?.message?.content;
  if (Array.isArray(content)) {
    return content
      .map((item: any) => item?.text || item?.content || "")
      .join("")
      .trim();
  }
  return String(content || "").trim();
}

/**
 * 格式化 AI 平台失败响应，方便用户排查。
 * @param {any} data - 已解析响应。
 * @param {string} rawText - 原始响应文本。
 * @returns {string} 可展示的响应信息。
 */
function formatAIResponse(data: any, rawText: string) {
  if (data) return JSON.stringify(data, null, 2);
  return rawText || "无返回内容";
}

function defaultForm() {
  return {
    aiBaseURL: DEFAULT_AI_BASE_URL,
    aiModel: DEFAULT_AI_MODEL,
    aiAPIKey: "",
    aiAPIKeyMasked: "",
    aiAPIKeySet: false,
    clickFrequency: 80,
    detailOpenProbability: 80,
    detailOpenDelayMin: 1,
    detailOpenDelayMax: 2,
    detailCloseDelayMin: 0,
    detailCloseDelayMax: 0,
    greetBeforeDelayMin: 1,
    greetBeforeDelayMax: 2,
    restAfterCandidatesMin: 40,
    restAfterCandidatesMax: 70,
    restTimesMin: 2,
    restTimesMax: 3,
    restDurationMin: 2,
    restDurationMax: 7,
  };
}

/**
 * 判断当前是否运行在本地控制台。
 * @returns {boolean} 本地控制台返回 true。
 */
function isLocalConsole() {
  if (typeof window === "undefined") return false;
  const hostname = window.location.hostname;
  const port = Number(window.location.port || "0");
  return (hostname === "localhost" || hostname === "127.0.0.1") && port >= 9001 && port <= 9009;
}

/**
 * 返回 Local Agent 基础地址。
 * @returns {string} Local Agent 地址。
 */
function localAgentBase() {
  return window.location.origin;
}

/**
 * 返回本地 AI Key 显示文案。
 * @param {any} ai - AI 配置。
 * @returns {string} 显示文案。
 */
function localAIKeyLabel(ai: any) {
  if (isLocalConsole()) return ai?.api_key ? "本地已保存明文 Key" : "";
  return ai?.api_key_masked || "";
}
