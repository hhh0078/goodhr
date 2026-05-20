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

/**
 * 绑定当前云端账号和本地 Agent 机器信息。
 * @param {any} payload - 包含 machine_id、agent_version、local_port 和 public_key 的绑定参数。
 * @returns {Promise<any>} 返回云端保存后的 Agent 绑定信息。
 */
export async function bindAgent(payload: any) {
  const data = await api('/api/agents/bind', { method: 'POST', body: payload })
  return data.agent
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

export async function runTask(taskID: string) {
  const data = await api(`/api/tasks/${taskID}/run`, {
    method: 'POST',
  })
  return data
}

export async function stopTask(taskID: string) {
  const data = await api(`/api/tasks/${taskID}/stop`, { method: 'POST' })
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

export async function createCookie(payload: any) {
  const data = await api('/api/cookies/create', {
    method: 'POST',
    body: payload,
  })
  return data.cookie
}

export async function claimCookie(cookieID: string, payload: any = {}) {
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/claim`, {
    method: 'POST',
    body: payload,
  })
}

export async function releaseCookie(cookieID: string) {
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/release`, {
    method: 'POST',
  })
}

export async function listPlatformConfigs() {
  const data = await api('/api/platforms/config/')
  return data.configs
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
