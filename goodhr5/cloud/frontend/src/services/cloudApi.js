// 本文件负责封装 GoodHR 云端 API 调用。

// request 调用云端 API，并统一处理 JSON 响应和错误。
export async function request(path, options = {}) {
  const base = window.GOODHR_CLOUD_API || 'http://127.0.0.1:8080'
  const response = await fetch(`${base}${path}`, options)
  const data = await response.json()
  if (!response.ok) {
    throw new Error(data.error || '云端请求失败')
  }
  return data
}

// listPlatformAccounts 读取当前用户的平台账号映射列表。
export async function listPlatformAccounts(token, platformId) {
  const query = platformId ? `?platform_id=${encodeURIComponent(platformId)}` : ''
  return request(`/api/platform-accounts${query}`, {
    headers: { Authorization: `Bearer ${token}` }
  })
}

// listPositions 读取当前用户的岗位配置列表。
export async function listPositions(token) {
  return request('/api/positions', {
    headers: { Authorization: `Bearer ${token}` }
  })
}

// savePosition 调用云端岗位配置接口创建或更新岗位模板。
export async function savePosition(token, payload) {
  return request('/api/positions', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload)
  })
}

// deletePosition 调用云端岗位配置接口删除岗位模板。
export async function deletePosition(token, positionID) {
  return request(`/api/positions/${positionID}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}` }
  })
}

// createTask 调用云端任务接口创建任务运行记录。
export async function createTask(token, payload) {
  return request('/api/tasks', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(payload)
  })
}

// listTasks 调用云端任务接口读取任务列表。
export async function listTasks(token) {
  return request('/api/tasks', {
    headers: { Authorization: `Bearer ${token}` }
  })
}

// listTaskLogs 调用云端任务日志接口读取某个任务的日志摘要。
export async function runTask(token, taskID, agentBaseURL) {
  return request(`/api/tasks/${taskID}/run`, {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ agent_base_url: agentBaseURL })
  })
}

export async function listTaskLogs(token, taskID) {
  return request(`/api/tasks/${taskID}/logs`, {
    headers: { Authorization: `Bearer ${token}` }
  })
}
