/** 任务和候选人管理 */
import { ref } from 'vue'
import { createTask, listTasks, listTaskLogs, runTask } from '../services/cloudApi.js'
import { initLocalTask, listLocalCandidates, deleteLocalCandidate } from '../services/localAgentApi.js'

export function useTasks(token, agentBaseUrl) {
  const tasks = ref([])
  const loading = ref(false)
  const error = ref('')
  const form = ref({ platformId: 'boss', platformAccountId: '', positionId: '', mode: 'keyword', matchLimit: 20 })
  const expandedTaskId = ref('')
  const taskLogs = ref({})
  const candidateExpandedTaskId = ref('')
  const taskCandidates = ref({})
  const candidateLoadingTaskId = ref('')
  const candidateError = ref('')

  async function load() {
    loading.value = true; error.value = ''
    try { tasks.value = await listTasks(token.value) } catch (e) { error.value = e.message }
    finally { loading.value = false }
  }

  async function create() {
    if (!form.value.platformAccountId) return
    loading.value = true; error.value = ''
    try { await createTask(token.value, { ...form.value }); await load(); form.value = { ...form.value, positionId: '' } }
    catch (e) { error.value = e.message } finally { loading.value = false }
  }

  async function execute(taskId) {
    loading.value = true; error.value = ''
    try { await runTask(token.value, taskId, agentBaseUrl.value); await load() }
    catch (e) { error.value = e.message } finally { loading.value = false }
  }

  async function toggleLogs(taskId) {
    if (expandedTaskId.value === taskId) { expandedTaskId.value = ''; return }
    expandedTaskId.value = taskId
    try { const logs = await listTaskLogs(token.value, taskId); taskLogs.value = { ...taskLogs.value, [taskId]: logs } } catch {}
  }

  async function toggleCandidates(task) {
    const localId = task.local_task_id || task.id
    if (candidateExpandedTaskId.value === localId) { candidateExpandedTaskId.value = ''; return }
    candidateExpandedTaskId.value = localId
    await loadCandidates(task, localId)
  }

  async function loadCandidates(task, localId) {
    candidateLoadingTaskId.value = localId; candidateError.value = ''
    try {
      const agent = { port: parseInt(agentBaseUrl.value.split(':').pop() || '9001') }
      await initLocalTask(agent, { task_id: localId, cloud_user_id: '', platform_id: task.platform_id || 'boss', platform_account_id: task.platform_account_id || '', position_snapshot: {} })
      const data = await listLocalCandidates(agent, localId)
      taskCandidates.value = { ...taskCandidates.value, [localId]: data }
    } catch (e) { candidateError.value = e.message }
    finally { candidateLoadingTaskId.value = '' }
  }

  async function removeCandidate(task, candidate) {
    const localId = candidateExpandedTaskId.value
    const agent = { port: parseInt(agentBaseUrl.value.split(':').pop() || '9001') }
    try {
      await deleteLocalCandidate(agent, localId, candidate.id)
      const items = (taskCandidates.value[localId]?.items || []).filter(c => c.id !== candidate.id)
      taskCandidates.value = { ...taskCandidates.value, [localId]: { ...taskCandidates.value[localId], items } }
    } catch {}
  }

  function localTaskID(task) { return task.local_task_id || task.id }
  function taskPositionSnapshot(task) {
    return taskCandidates.value[localTaskID(task)]?.position_snapshot || { name: '', keywords: [], exclude_keywords: [], greet_message: '' }
  }
  function candidateItems(task) { return taskCandidates.value[localTaskID(task)]?.items || [] }
  function candidateTitle(c) { return c.name || c.id || '' }
  function candidateSubtitle(c) { return c.raw_text || c.skills || '' }
  function candidateDetail(c) { return c.detail_text || '' }

  return { tasks, loading, error, form, expandedTaskId, taskLogs, candidateExpandedTaskId, taskCandidates, candidateLoadingTaskId, candidateError, load, create, execute, toggleLogs, toggleCandidates, loadCandidates, removeCandidate, localTaskID, taskPositionSnapshot, candidateItems, candidateTitle, candidateSubtitle, candidateDetail }
}
