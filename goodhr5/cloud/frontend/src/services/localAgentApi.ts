// 本文件负责封装浏览器页面访问 GoodHR Local Agent 的 API 调用。

// localBaseURL 根据 Agent 信息生成本地 Agent API 地址。
export function localBaseURL(agent) {
  return `http://127.0.0.1:${agent.port}`
}

// requestLocalAgent 调用本地 Agent API，并统一处理 JSON 响应和错误。
export async function requestLocalAgent(agent, path, options = {}) {
  const response = await fetch(`${localBaseURL(agent)}${path}`, options)
  const data = await response.json()
  if (!response.ok) {
    throw new Error(data.error || '本地 Agent 请求失败')
  }
  return data
}

// initLocalTask 调用本地 Agent 初始化任务目录和 candidates.json。
export async function initLocalTask(agent, payload) {
  return requestLocalAgent(agent, '/api/v1/tasks/init', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
}

// listLocalCandidates 调用本地 Agent 读取任务候选人 JSON。
export async function listLocalCandidates(agent, taskID) {
  return requestLocalAgent(agent, `/api/v1/tasks/${encodeURIComponent(taskID)}/candidates`)
}

// deleteLocalCandidate 调用本地 Agent 删除指定候选人记录。
export async function deleteLocalCandidate(agent, taskID, candidateID) {
  return requestLocalAgent(
    agent,
    `/api/v1/tasks/${encodeURIComponent(taskID)}/candidates/${encodeURIComponent(candidateID)}`,
    { method: 'DELETE' }
  )
}
