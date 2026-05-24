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

export async function getUserAIConfig() {
  const data = await api('/api/config/user-ai')
  return data.config
}

export async function updateUserAIConfig(payload: any) {
  const data = await api('/api/config/user-ai', { method: 'PUT', body: payload })
  return data.config
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

export async function updateTask(taskID: string, payload: any) {
  const data = await api(`/api/tasks/${encodeURIComponent(taskID)}`, { method: 'PUT', body: payload })
  return data.task
}

export async function deleteTask(taskID: string) {
  return api(`/api/tasks/${encodeURIComponent(taskID)}`, { method: 'DELETE' })
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

export async function listTaskLogs(
  taskID: string,
  params: { since?: string; before?: string; limit?: number } = {},
) {
  const queryParams = new URLSearchParams()
  if (params.since) queryParams.set('since', params.since)
  if (params.before) queryParams.set('before', params.before)
  if (params.limit) queryParams.set('limit', String(params.limit))
  const query = queryParams.toString() ? `?${queryParams.toString()}` : ''
  const data = await api(`/api/tasks/${taskID}/logs${query}`)
  return data
}

/**
 * 清空指定任务的云端日志摘要。
 * @param {string} taskID - 任务 ID。
 * @returns {Promise<void>} 无返回值。
 */
export async function clearTaskLogs(taskID: string) {
  await api(`/api/tasks/${taskID}/logs`, { method: 'DELETE' })
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

export async function updateCookie(cookieID: string, payload: any) {
  const data = await api(`/api/cookies/${encodeURIComponent(cookieID)}`, {
    method: 'PUT',
    body: payload,
  })
  return data.cookie
}

/**
 * 更新平台账号 cookie 的登录状态。
 * @param {string} cookieID - cookie 账号 ID。
 * @param {string} status - 目标状态，支持 available、expired、in_use。
 * @returns {Promise<any>} 返回后端状态更新结果。
 */
export async function updateCookieStatus(cookieID: string, status: string) {
  return api(`/api/cookies/${encodeURIComponent(cookieID)}/status`, {
    method: 'PUT',
    body: { status },
  })
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

/**
 * 读取管理员可见的系统原始配置 JSON。
 * @returns {Promise<any[]>} 返回系统配置列表。
 */
export async function listAdminSystemConfigs() {
  const data = await api('/api/admin/system/configs/')
  return data.configs
}

/**
 * 保存管理员可见的系统原始配置 JSON。
 * @param {string} configKey - 系统配置键。
 * @param {string} configValue - JSON 字符串形式的配置值。
 * @returns {Promise<any>} 返回保存后的系统配置。
 */
export async function updateAdminSystemConfig(configKey: string, configValue: string) {
  const data = await api(`/api/admin/system/configs/${encodeURIComponent(configKey)}`, {
    method: 'PUT',
    body: { config_value: configValue },
  })
  return data.config
}

/**
 * 读取系统默认 AI 提示词。
 * @returns {Promise<any>} 返回 filter_prompt 和 open_detail_prompt。
 */
export async function getDefaultPrompts() {
  const data = await api('/api/system/default-prompts')
  return data.prompts || {}
}

export async function getUserPreferences() {
  const data = await api('/api/config/user-preferences')
  return data.config
}

export async function updateUserPreferences(payload: any) {
  const data = await api('/api/config/user-preferences', { method: 'PUT', body: payload })
  return data.config
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
