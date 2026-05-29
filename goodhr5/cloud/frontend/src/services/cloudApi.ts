// GoodHR 5 云端 API 封装。所有函数返回解析后的数据，不是原始响应。
import { api, cloudApiBase, getAccessToken } from './apiClient'

export async function sendLoginCode(email: string) {
  return api('/api/auth/send-code', { method: 'POST', auth: false, body: { email } })
}

export async function loginByCode(email: string, code: string, inviterID = '') {
  return api('/api/auth/login', { method: 'POST', auth: false, body: { email, code, inviter_id: inviterID } })
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

/**
 * 读取简历库候选人列表。
 * @param {{ taskId?: string; positionId?: string; keyword?: string; page?: number; pageSize?: number }} params - 搜索和分页条件。
 * @returns {Promise<any>} 返回候选人简历分页结果。
 */
export async function listCandidates(params: { taskId?: string; positionId?: string; keyword?: string; page?: number; pageSize?: number } = {}) {
  const query = new URLSearchParams()
  if (params.taskId) query.set('task_id', params.taskId)
  if (params.positionId) query.set('position_id', params.positionId)
  if (params.keyword) query.set('keyword', params.keyword)
  if (params.page) query.set('page', String(params.page))
  if (params.pageSize) query.set('page_size', String(params.pageSize))
  const suffix = query.toString() ? `?${query.toString()}` : ''
  const data = await api(`/api/candidates${suffix}`)
  return {
    items: data.candidates || [],
    total: Number(data.total || 0),
    page: Number(data.page || params.page || 1),
    pageSize: Number(data.page_size || params.pageSize || 20),
  }
}

/**
 * 读取候选人详情。
 * @param {string} candidateID - 候选人 ID。
 * @returns {Promise<any>} 返回候选人详情。
 */
export async function getCandidate(candidateID: string) {
  const data = await api(`/api/candidates/${encodeURIComponent(candidateID)}`)
  return data.candidate
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

/**
 * 读取当前用户订阅状态。
 * @returns {Promise<any>} 返回会员类型、到期时间和有效状态。
 */
export async function getSubscriptionStatus() {
  const data = await api('/api/subscription/status')
  return data.subscription
}

/**
 * 读取系统订阅套餐列表。
 * @returns {Promise<any[]>} 返回订阅套餐数组。
 */
export async function listSubscriptionPlans() {
  const data = await api('/api/subscription/plans')
  return data.plans || []
}

/**
 * 兑换会员激活码。
 * @param {string} code - 用户输入的激活码。
 * @returns {Promise<any>} 返回新的订阅状态。
 */
export async function redeemActivationCode(code: string) {
  const data = await api('/api/activation-codes/redeem', { method: 'POST', body: { code } })
  return data.subscription
}

/**
 * 读取当前用户邀请信息。
 * @returns {Promise<any>} 返回邀请配置、邀请ID和邀请列表。
 */
export async function getInvitationSummary() {
  return api('/api/invitations/summary')
}

/**
 * 读取超级管理员可见的激活码列表。
 * @returns {Promise<any[]>} 返回激活码数组。
 */
export async function listAdminActivationCodes() {
  const data = await api('/api/admin/activation-codes')
  return data.codes || []
}

/**
 * 超级管理员批量生成激活码。
 * @param {any} payload - 包含天数、备注和数量。
 * @returns {Promise<any[]>} 返回生成的激活码数组。
 */
export async function createAdminActivationCodes(payload: any) {
  const data = await api('/api/admin/activation-codes', { method: 'POST', body: payload })
  return data.codes || []
}

/**
 * 创建订阅支付订单。
 * @param {string} planID - 订阅套餐 ID。
 * @returns {Promise<any>} 返回支付订单和支付平台提交参数。
 */
export async function createPaymentOrder(planID: string) {
  return api('/api/payment/orders', { method: 'POST', body: { plan_id: planID } })
}

/**
 * 读取当前用户支付记录。
 * @returns {Promise<any[]>} 返回当前用户支付记录数组。
 */
export async function listPaymentOrders() {
  const data = await api('/api/payment/orders')
  return data.orders || []
}

/**
 * 读取超级管理员可见的全部支付记录。
 * @returns {Promise<any[]>} 返回全部支付记录数组。
 */
export async function listAdminPaymentOrders() {
  const data = await api('/api/admin/payment/orders')
  return data.orders || []
}

/**
 * 读取超级管理员可见的用户列表。
 * @returns {Promise<any[]>} 返回用户数组。
 */
export async function listAdminUsers() {
  const data = await api('/api/admin/users')
  return data.users || []
}

/**
 * 超级管理员调整指定用户会员天数。
 * @param {any} payload - 包含 email、days 和 reason。
 * @returns {Promise<any>} 返回新的订阅状态。
 */
export async function adjustAdminUserSubscription(payload: any) {
  return api('/api/admin/users', { method: 'POST', body: payload })
}

/**
 * 读取当前用户教学状态和教学配置。
 * @returns {Promise<any>} 返回教学状态和配置。
 */
export async function getOnboardingStatus() {
  return api('/api/onboarding/status')
}

/**
 * 标记当前用户已完成教学。
 * @returns {Promise<any>} 返回后端保存后的教学状态。
 */
export async function completeOnboarding() {
  return api('/api/onboarding/complete', { method: 'POST' })
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
 * 读取前端公共系统配置。
 * @returns {Promise<any>} 返回本地执行器版本要求和公告列表。
 */
export async function getSystemAppConfig() {
  const data = await api('/api/system/app-config')
  return data.config || {}
}

/**
 * 读取帮助中心系统指南。
 * @returns {Promise<any>} 返回系统指南 JSON。
 */
export async function getSystemGuide() {
  const data = await api('/api/help/guide')
  return data.guide || {}
}

/**
 * 流式调用帮助中心 AI 助手。
 * @param {any[]} messages - 当前聊天上下文。
 * @param {(chunk: string) => void} onChunk - 每段文本回调。
 * @returns {Promise<string>} 返回完整回答文本。
 */
export async function streamHelpChat(messages: any[], onChunk: (chunk: string) => void) {
  const res = await fetch(`${cloudApiBase()}/api/help/chat`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${getAccessToken()}`,
    },
    body: JSON.stringify({ messages }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(parseStreamError(text) || '帮助助手请求失败')
  }
  if (!res.body) return ''
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let result = ''
  while (true) {
    const { value, done } = await reader.read()
    if (done) break
    const chunk = decoder.decode(value, { stream: true })
    if (!chunk) continue
    result += chunk
    onChunk(chunk)
  }
  const tail = decoder.decode()
  if (tail) {
    result += tail
    onChunk(tail)
  }
  return result
}

/**
 * 从流式错误响应中提取错误文案。
 * @param {string} text - 原始响应文本。
 * @returns {string} 错误文案。
 */
function parseStreamError(text: string) {
  try {
    const data = JSON.parse(text)
    return data.error || data.detail || ''
  } catch {
    return text
  }
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
