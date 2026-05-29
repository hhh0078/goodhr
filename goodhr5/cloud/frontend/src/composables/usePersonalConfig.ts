import { ref } from "vue";
import {
  getUserAIConfig,
  getUserPreferences,
  updateUserAIConfig,
  updateUserPreferences,
} from "../services/api/personalConfigApi";
import { markOnboardingStep } from "../services/onboarding";

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
        aiBaseURL: ai?.base_url || "",
        aiModel: ai?.model || data?.ai_model || "",
        aiAPIKey: "",
        aiAPIKeyMasked: ai?.api_key_masked || "",
        aiAPIKeySet: Boolean(ai?.api_key_set),
        clickFrequency: data?.click_frequency ?? 80,
        detailOpenProbability: data?.detail_open_probability ?? 30,
        scrollDelayMin: data?.scroll_delay_min ?? 3,
        scrollDelayMax: data?.scroll_delay_max ?? 8,
        listViewDelayMin: data?.list_view_delay_min ?? 1,
        listViewDelayMax: data?.list_view_delay_max ?? 2,
        detailViewDelayMin: data?.detail_view_delay_min ?? 1,
        detailViewDelayMax: data?.detail_view_delay_max ?? 2,
        greetDelayMin: data?.greet_delay_min ?? 1,
        greetDelayMax: data?.greet_delay_max ?? 2,
        restAfterCandidatesMin: data?.rest_after_candidates_min ?? 0,
        restAfterCandidatesMax: data?.rest_after_candidates_max ?? 0,
        restTimesMin: data?.rest_times_min ?? 0,
        restTimesMax: data?.rest_times_max ?? 0,
        restDurationMin: data?.rest_duration_min ?? 0,
        restDurationMax: data?.rest_duration_max ?? 0,
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
        scroll_delay_min: Number(form.value.scrollDelayMin || 0),
        scroll_delay_max: Number(form.value.scrollDelayMax || 0),
        list_view_delay_min: Number(form.value.listViewDelayMin || 0),
        list_view_delay_max: Number(form.value.listViewDelayMax || 0),
        detail_view_delay_min: Number(form.value.detailViewDelayMin || 0),
        detail_view_delay_max: Number(form.value.detailViewDelayMax || 0),
        greet_delay_min: Number(form.value.greetDelayMin || 0),
        greet_delay_max: Number(form.value.greetDelayMax || 0),
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
    aiBaseURL: "",
    aiModel: "",
    aiAPIKey: "",
    aiAPIKeyMasked: "",
    aiAPIKeySet: false,
    clickFrequency: 80,
    detailOpenProbability: 30,
    scrollDelayMin: 3,
    scrollDelayMax: 8,
    listViewDelayMin: 1,
    listViewDelayMax: 2,
    detailViewDelayMin: 1,
    detailViewDelayMax: 2,
    greetDelayMin: 1,
    greetDelayMax: 2,
    restAfterCandidatesMin: 0,
    restAfterCandidatesMax: 0,
    restTimesMin: 0,
    restTimesMax: 0,
    restDurationMin: 0,
    restDurationMax: 0,
  };
}
