// GoodHR 5 云端 API 封装。所有函数返回解析后的数据，不是原始响应。
import { api } from './apiClient'

export async function sendLoginCode(email: string) {
  return api('/api/auth/send-code', { method: 'POST', auth: false, body: { email } })
}

export async function loginByCode(email: string, code: string) {
  return api('/api/auth/login', { method: 'POST', auth: false, body: { email, code } })
}

export async function currentUser() {
  const data = await api('/api/auth/me')
  return data.user
}

export async function listPositions() {
  const data = await api('/api/positions')
  return data.positions
}

export async function savePosition(payload: any) {
  const data = await api('/api/positions', { method: 'POST', body: payload })
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
  const data = await api('/api/platform-accounts/create', { method: 'POST', body: payload })
  return data.account
}

export async function deletePlatformAccount(accountID: string) {
  await api(`/api/platform-accounts/${accountID}`, { method: 'DELETE' })
}

export async function createTask(payload: any) {
  const data = await api('/api/tasks', { method: 'POST', body: payload })
  return data.task
}

export async function listTasks() {
  const data = await api('/api/tasks')
  return data.tasks
}

export async function runTask(taskID: string, agentBaseURL: string) {
  const data = await api(`/api/tasks/${taskID}/run`, {
    method: 'POST',
    body: { agent_base_url: agentBaseURL },
  })
  return data
}

export async function listTaskLogs(taskID: string) {
  const data = await api(`/api/tasks/${taskID}/logs`)
  return data.logs
}

export async function listCookies() {
  const data = await api('/api/cookies')
  return data.cookies
}

export async function createCookie(payload: any, agentBaseURL = '') {
  const data = await api('/api/cookies/create', {
    method: 'POST',
    headers: agentBaseURL ? { 'X-GoodHR-Agent-BaseURL': agentBaseURL } : undefined,
    body: agentBaseURL ? { ...payload, agent_base_url: agentBaseURL } : payload,
  })
  return data.cookie
}

export async function listTenantMembers() {
  const data = await api('/api/tenants/members')
  return data.members
}

export async function inviteTenantMember(payload: any) {
  const data = await api('/api/tenants/invite', { method: 'POST', body: payload })
  return data
}

export async function updateTenantMember(email: string, payload: any) {
  return api(`/api/tenants/members/${encodeURIComponent(email)}`, { method: 'PUT', body: payload })
}

export async function deleteTenantMember(email: string) {
  return api(`/api/tenants/members/${encodeURIComponent(email)}`, { method: 'DELETE' })
}
