/** 岗位模板管理 */
import { ref } from 'vue'
import { getDefaultPrompts, listPositions, savePosition, deletePosition } from '../services/cloudApi'
import { markOnboardingStep } from '../services/onboarding'

export function usePositions() {
  const positions = ref<any[]>([])
  const loading = ref(false)
  const error = ref('')
  const form = ref(defaultForm())
  const defaultPrompts = ref({ filter_prompt: '', open_detail_prompt: '', review_prompt: '' })

  async function load() {
    loading.value = true; error.value = ''
    try {
      const [list, prompts] = await Promise.all([listPositions(), getDefaultPrompts()])
      positions.value = list
      defaultPrompts.value = normalizeDefaultPrompts(prompts)
      fillEmptyDefaultPrompts()
    } catch (e: any) { error.value = e.message; positions.value = [] }
    finally { loading.value = false }
  }

  async function save() {
    if (!form.value.name) return
    loading.value = true; error.value = ''
    try {
      fillEmptyDefaultPrompts()
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
          detail_mode: form.value.detailMode,
        },
        ai_config: {
          position_requirement: form.value.aiPositionRequirement,
          filter_prompt: form.value.aiFilterPrompt,
          greet_prompt: form.value.aiFilterPrompt,
          click_prompt: form.value.aiFilterPrompt,
          open_detail_prompt: form.value.aiOpenDetailPrompt,
          review_prompt: form.value.aiReviewPrompt,
          detail_score_threshold: Number(form.value.detailScoreThreshold || 0),
          greet_score_threshold: Number(form.value.greetScoreThreshold || 0),
        },
        keyword_config: {},
      })
      await markOnboardingStep('position_template')
      await load(); resetForm()
    } catch (e: any) { error.value = e.message; positions.value = [] }
    finally { loading.value = false }
  }

  async function remove(id: string) {
    loading.value = true; error.value = ''
    try { await deletePosition(id); await load() } catch (e: any) { error.value = e.message; positions.value = [] }
    finally { loading.value = false }
  }

  /**
   * 清空岗位模板表单，并自动填入系统默认提示词。
   * @returns {void} 无返回值。
   */
  function resetForm() {
    form.value = defaultForm()
    fillEmptyDefaultPrompts()
  }

  /**
   * 将打开详情提示词重置为系统默认值。
   * @returns {void} 无返回值。
   */
  function resetOpenDetailPrompt() {
    form.value.aiOpenDetailPrompt = normalizePromptText(defaultPrompts.value.open_detail_prompt || '')
  }

  /**
   * 将最终筛选提示词重置为系统默认值。
   * @returns {void} 无返回值。
   */
  function resetFilterPrompt() {
    form.value.aiFilterPrompt = normalizePromptText(defaultPrompts.value.filter_prompt || '')
  }

  /**
   * 将复核提示词重置为系统默认值。
   * @returns {void} 无返回值。
   */
  function resetReviewPrompt() {
    form.value.aiReviewPrompt = normalizePromptText(defaultPrompts.value.review_prompt || '')
  }

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
      detailMode: common.detail_mode || keyword.detail_mode || 'dom',
      aiPositionRequirement: ai.position_requirement || '',
      aiFilterPrompt: normalizePromptText(ai.greet_prompt || ai.filter_prompt || ai.click_prompt || ''),
      aiOpenDetailPrompt: normalizePromptText(ai.open_detail_prompt || ''),
      aiReviewPrompt: normalizePromptText(ai.review_prompt || ''),
      detailScoreThreshold: String(ai.detail_score_threshold ?? 60),
      greetScoreThreshold: String(ai.greet_score_threshold ?? 70),
    }
    fillEmptyDefaultPrompts()
  }

  /**
   * 空提示词字段自动补齐系统默认值。
   * @returns {void} 无返回值。
   */
  function fillEmptyDefaultPrompts() {
    if (!form.value.aiFilterPrompt && defaultPrompts.value.filter_prompt) {
      form.value.aiFilterPrompt = normalizePromptText(defaultPrompts.value.filter_prompt)
    }
    if (!form.value.aiOpenDetailPrompt && defaultPrompts.value.open_detail_prompt) {
      form.value.aiOpenDetailPrompt = normalizePromptText(defaultPrompts.value.open_detail_prompt)
    }
  }

  return {
    positions,
    loading,
    error,
    form,
    defaultPrompts,
    load,
    save,
    remove,
    resetForm,
    edit,
    resetOpenDetailPrompt,
    resetFilterPrompt,
    resetReviewPrompt,
  }
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
    detailMode: 'dom',
    aiPositionRequirement: '',
    aiFilterPrompt: '',
    aiOpenDetailPrompt: '',
    aiReviewPrompt: '',
    detailScoreThreshold: '60',
    greetScoreThreshold: '70',
  }
}

/**
 * 标准化后端返回的系统默认提示词。
 * @param {any} value - 后端提示词对象。
 * @returns {{filter_prompt: string; open_detail_prompt: string; review_prompt: string}} 标准化后的提示词。
 */
function normalizeDefaultPrompts(value: any) {
  return {
    filter_prompt: normalizePromptText(String(value?.filter_prompt || '')),
    open_detail_prompt: normalizePromptText(String(value?.open_detail_prompt || '')),
    review_prompt: normalizePromptText(String(value?.review_prompt || '')),
  }
}

/**
 * 将字面量 \n 还原为真实换行，兼容历史脏数据展示。
 * @param {string} text - 原始提示词文本。
 * @returns {string} 处理后的多行文本。
 */
function normalizePromptText(text: string) {
  return String(text || '').replace(/\\n/g, '\n')
}
