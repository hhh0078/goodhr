// GoodHR 5 云端 API 封装。所有函数返回解析后的数据，不是原始响应。
import { api } from './apiClient'

export async function listPositions() {
  const data = await api('/api/positions')
  return data.positions
}

export async function savePosition(payload: any) {
  const data = await api('/api/positions', { method: 'POST', body: JSON.stringify(payload) })
  return data.position
}

export async function deletePosition(positionID: string) {
  await api(`/api/positions/${positionID}`, { method: 'DELETE' })
}

export async function listPlatformAccounts() {
  const data = await api('/api/platform-accounts')
  return data.accounts
}

export async function createPlatformAccount(payload: any) {
  const data = await api('/api/platform-accounts/create', { method: 'POST', body: JSON.stringify(payload) })
  return data.account
}

export async function deletePlatformAccount(accountID: string) {
  await api(`/api/platform-accounts/${accountID}`, { method: 'DELETE' })
}

export async function createTask(payload: any) {
  const data = await api('/api/tasks', { method: 'POST', body: JSON.stringify(payload) })
  return data.task
}

export async function listTasks() {
  const data = await api('/api/tasks')
  return data.tasks
}

export async function runTask(taskID: string, agentBaseURL: string) {
  const data = await api(`/api/tasks/${taskID}/run`, {
    method: 'POST',
    body: JSON.stringify({ agent_base_url: agentBaseURL }),
  })
  return data
}

export async function listTaskLogs(taskID: string) {
  const data = await api(`/api/tasks/${taskID}/logs`)
  return data.logs
}
