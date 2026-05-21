/** 岗位模板管理 */
import { ref } from 'vue'
import { listPositions, savePosition, deletePosition } from '../services/cloudApi'

export function usePositions() {
  const positions = ref<any[]>([])
  const loading = ref(false)
  const error = ref('')
  const form = ref(defaultForm())

  async function load() {
    loading.value = true; error.value = ''
    try { positions.value = await listPositions() } catch (e: any) { error.value = e.message; positions.value = [] }
    finally { loading.value = false }
  }

  async function save() {
    if (!form.value.name) return
    loading.value = true; error.value = ''
    try {
      const kw = form.value.keywords.split(/[,\s]+/).filter(Boolean)
      const ek = form.value.excludeKeywords.split(/[,\s]+/).filter(Boolean)
      await savePosition({
        id: form.value.id,
        name: form.value.name,
        keywords: kw,
        exclude_keywords: ek,
        description: form.value.description,
        greet_message: form.value.greetMessage,
        is_and_mode: form.value.isAndMode,
        common_config: {
          mode_default: form.value.modeDefault,
          enable_sound: form.value.enableSound,
          scroll_delay_min: Number(form.value.scrollDelayMin),
          scroll_delay_max: Number(form.value.scrollDelayMax),
          list_view_delay_min: Number(form.value.listViewDelayMin),
          list_view_delay_max: Number(form.value.listViewDelayMax),
          detail_view_delay_min: Number(form.value.detailViewDelayMin),
          detail_view_delay_max: Number(form.value.detailViewDelayMax),
          greet_delay_min: Number(form.value.greetDelayMin),
          greet_delay_max: Number(form.value.greetDelayMax),
          click_frequency: Number(form.value.clickFrequency),
          rest_after_candidates_min: Number(form.value.restAfterCandidatesMin),
          rest_after_candidates_max: Number(form.value.restAfterCandidatesMax),
          rest_times_min: Number(form.value.restTimesMin),
          rest_times_max: Number(form.value.restTimesMax),
          rest_duration_min: Number(form.value.restDurationMin),
          rest_duration_max: Number(form.value.restDurationMax),
        },
        ai_config: {
          model: form.value.aiModel,
          position_requirement: form.value.aiPositionRequirement,
          click_prompt: form.value.aiClickPrompt,
        },
        keyword_config: {
          keyword_detail_open_probability: Number(form.value.keywordDetailOpenProbability),
          detail_mode: form.value.keywordDetailMode,
        },
      })
      await load(); resetForm()
    } catch (e: any) { error.value = e.message; positions.value = [] }
    finally { loading.value = false }
  }

  async function remove(id: string) {
    loading.value = true; error.value = ''
    try { await deletePosition(id); await load() } catch (e: any) { error.value = e.message; positions.value = [] }
    finally { loading.value = false }
  }

  function resetForm() { form.value = defaultForm() }
  function edit(pos: any) {
    const common = pos.common_config || {}
    const ai = pos.ai_config || {}
    const keyword = pos.keyword_config || {}
    form.value = {
      id: pos.id,
      name: pos.name || '',
      keywords: (pos.keywords || []).join(' '),
      excludeKeywords: (pos.exclude_keywords || []).join(' '),
      description: pos.description || '',
      greetMessage: pos.greet_message || '',
      isAndMode: pos.is_and_mode || false,
      modeDefault: common.mode_default || 'ai',
      enableSound: Boolean(common.enable_sound),
      scrollDelayMin: common.scroll_delay_min ?? 3,
      scrollDelayMax: common.scroll_delay_max ?? 8,
      listViewDelayMin: common.list_view_delay_min ?? 1,
      listViewDelayMax: common.list_view_delay_max ?? 2,
      detailViewDelayMin: common.detail_view_delay_min ?? 1,
      detailViewDelayMax: common.detail_view_delay_max ?? 2,
      greetDelayMin: common.greet_delay_min ?? 1,
      greetDelayMax: common.greet_delay_max ?? 2,
      clickFrequency: common.click_frequency ?? 80,
      restAfterCandidatesMin: common.rest_after_candidates_min ?? 0,
      restAfterCandidatesMax: common.rest_after_candidates_max ?? 0,
      restTimesMin: common.rest_times_min ?? 0,
      restTimesMax: common.rest_times_max ?? 0,
      restDurationMin: common.rest_duration_min ?? 0,
      restDurationMax: common.rest_duration_max ?? 0,
      aiModel: ai.model || '',
      aiPositionRequirement: ai.position_requirement || '',
      aiClickPrompt: ai.click_prompt || '',
      keywordDetailOpenProbability: keyword.keyword_detail_open_probability ?? 30,
      keywordDetailMode: keyword.detail_mode || 'dom',
    }
  }

  return { positions, loading, error, form, load, save, remove, resetForm, edit }
}

function defaultForm() {
  return {
    id: '',
    name: '',
    keywords: '',
    excludeKeywords: '',
    description: '',
    greetMessage: '',
    isAndMode: false,
    modeDefault: 'ai',
    enableSound: true,
    scrollDelayMin: 3,
    scrollDelayMax: 8,
    listViewDelayMin: 1,
    listViewDelayMax: 2,
    detailViewDelayMin: 1,
    detailViewDelayMax: 2,
    greetDelayMin: 1,
    greetDelayMax: 2,
    clickFrequency: 80,
    restAfterCandidatesMin: 0,
    restAfterCandidatesMax: 0,
    restTimesMin: 0,
    restTimesMax: 0,
    restDurationMin: 0,
    restDurationMax: 0,
    aiModel: '',
    aiPositionRequirement: '',
    aiClickPrompt: '',
    keywordDetailOpenProbability: 30,
    keywordDetailMode: 'dom',
  }
}
