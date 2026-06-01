import { ref } from "vue";
import {
  getUserAIConfig,
  getUserPreferences,
  updateUserAIConfig,
  updateUserPreferences,
} from "../services/api/personalConfigApi";
import { markOnboardingStep } from "../services/onboarding";

const DEFAULT_AI_BASE_URL = "https://api.deepseek.com/chat/completions";
const DEFAULT_AI_MODEL = "deepseek-v4-flash";

export function usePersonalConfig() {
  const loading = ref(false);
  const error = ref("");
  const message = ref("");
  const form = ref(defaultForm());

  async function load() {
    loading.value = true;
    error.value = "";
    try {
      const data = await getUserPreferences();
      const ai = await getUserAIConfig();
      form.value = {
        aiBaseURL: ai?.base_url || DEFAULT_AI_BASE_URL,
        aiModel: ai?.model || data?.ai_model || DEFAULT_AI_MODEL,
        aiAPIKey: "",
        aiAPIKeyMasked: ai?.api_key_masked || "",
        aiAPIKeySet: Boolean(ai?.api_key_set),
        clickFrequency: data?.click_frequency ?? 80,
        detailOpenProbability: data?.detail_open_probability ?? 80,
        detailOpenDelayMin: data?.detail_open_delay_min ?? 1,
        detailOpenDelayMax: data?.detail_open_delay_max ?? 2,
        detailCloseDelayMin: data?.detail_close_delay_min ?? 1,
        detailCloseDelayMax: data?.detail_close_delay_max ?? 2,
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
      await updateUserAIConfig({
        base_url: form.value.aiBaseURL,
        model: form.value.aiModel,
        api_key: form.value.aiAPIKey.trim(),
        temperature: 0,
        prompt_template: "",
        enabled: true,
      });
      await updateUserPreferences({
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
      });
      if (form.value.aiAPIKey.trim()) {
        form.value.aiAPIKey = "";
        form.value.aiAPIKeySet = true;
        form.value.aiAPIKeyMasked = "已更新";
      }
      message.value = "个人配置已保存";
      await markOnboardingStep("personal_config");
    } catch (e: any) {
      error.value = e.message;
    } finally {
      loading.value = false;
    }
  }

  return { form, loading, error, message, load, save };
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
    detailCloseDelayMin: 1,
    detailCloseDelayMax: 2,
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
