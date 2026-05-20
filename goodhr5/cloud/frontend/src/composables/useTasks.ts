/** 任务和候选人管理 */
import { ref } from 'vue'
import { createTask, listTasks, listTaskLogs, runTask } from '../services/cloudApi'
import { initLocalTask, listLocalCandidates, deleteLocalCandidate } from '../services/localAgentApi'

export function useTasks(agentBaseUrl: Ref<string>) {
  const tasks = ref<any[]>([])
  const loading = ref(false)
  const error = ref('')
  const form = ref({ platformId: 'boss', platformAccountId: '', positionId: '', mode: 'keyword', matchLimit: 20 })
  const expandedTaskId = ref('')
  const taskLogs = ref<Record<string, any[]>>({})
  const candidateExpandedTaskId = ref('')
  const taskCandidates = ref<Record<string, any>>({})
  const candidateLoadingTaskId = ref('')
  const candidateError = ref('')

  async function load() {
    loading.value = true; error.value = ''
    try { tasks.value = await listTasks() } catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function create() {
    if (!form.value.platformAccountId) return
    loading.value = true; error.value = ''
    try { await createTask({ ...form.value }); await load(); form.value = { ...form.value, positionId: '' } }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function execute(taskId: string) {
    loading.value = true; error.value = ''
    try { await runTask(taskId, agentBaseUrl.value); await load() }
    catch (e: any) { error.value = e.message }
    finally { loading.value = false }
  }

  async function toggleLogs(taskId: string) {
    if (expandedTaskId.value === taskId) { expandedTaskId.value = ''; return }
    expandedTaskId.value = taskId
    try { const logs = await listTaskLogs(taskId); taskLogs.value = { ...taskLogs.value, [taskId]: logs } } catch {}
  }

  async function toggleCandidates(task: any) {
    const localId = task.local_task_id || task.id
    if (candidateExpandedTaskId.value === localId) { candidateExpandedTaskId.value = ''; return }
    candidateExpandedTaskId.value = localId; await loadCandidates(task, localId)
  }

  async function loadCandidates(task: any, localId: string) {
    candidateLoadingTaskId.value = localId; candidateError.value = ''
    try {
      await initLocalTask(agentBaseUrl.value, { task_id: localId, cloud_user_id: '', platform_id: task.platform_id || 'boss', platform_account_id: task.platform_account_id || '', position_snapshot: {} })
      const data = await listLocalCandidates(agentBaseUrl.value, localId)
      taskCandidates.value = { ...taskCandidates.value, [localId]: data }
    } catch (e: any) { candidateError.value = e.message }
    finally { candidateLoadingTaskId.value = '' }
  }

  async function removeCandidate(task: any, candidate: any) {
    const localId = candidateExpandedTaskId.value
    try {
      await deleteLocalCandidate(agentBaseUrl.value, localId, candidate.id)
      const items = (taskCandidates.value[localId]?.items || []).filter((c: any) => c.id !== candidate.id)
      taskCandidates.value = { ...taskCandidates.value, [localId]: { ...taskCandidates.value[localId], items } }
    } catch {}
  }

  function localTaskID(task: any) { return task.local_task_id || task.id }
  function taskPositionSnapshot(task: any) { return taskCandidates.value[localTaskID(task)]?.position_snapshot || {} }
  function candidateItems(task: any) { return taskCandidates.value[localTaskID(task)]?.items || [] }
  function candidateTitle(c: any) { return c.name || c.id || '' }
  function candidateSubtitle(c: any) { return c.raw_text || c.skills || '' }
  function candidateDetail(c: any) { return c.detail_text || '' }

  return { tasks, loading, error, form, expandedTaskId, taskLogs, candidateExpandedTaskId, taskCandidates, candidateLoadingTaskId, candidateError, load, create, execute, toggleLogs, toggleCandidates, loadCandidates, removeCandidate, localTaskID, taskPositionSnapshot, candidateItems, candidateTitle, candidateSubtitle, candidateDetail }
}
