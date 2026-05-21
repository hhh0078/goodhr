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
          detail_mode: form.value.detailMode,
        },
        ai_config: {
          position_requirement: form.value.aiPositionRequirement,
          click_prompt: form.value.aiClickPrompt,
        },
        keyword_config: {
          keyword_detail_open_probability: Number(form.value.keywordDetailOpenProbability),
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
      detailMode: common.detail_mode || keyword.detail_mode || 'dom',
      aiPositionRequirement: ai.position_requirement || '',
      aiClickPrompt: ai.click_prompt || '',
      keywordDetailOpenProbability: keyword.keyword_detail_open_probability ?? 30,
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
    detailMode: 'dom',
    aiPositionRequirement: '',
    aiClickPrompt: '',
    keywordDetailOpenProbability: 30,
  }
}
