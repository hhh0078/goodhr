// GoodHR 5 本地 Agent API 封装
export function agentURL(base: string, path: string): string {
  if (base.endsWith('/')) base = base.slice(0, -1)
  return `${base}${path}`
}

async function req(base: string, path: string, opts: RequestInit = {}) {
  const res = await fetch(agentURL(base, path), { headers: { 'Content-Type': 'application/json' }, ...opts })
  const data = await res.json()
  if (!res.ok || !data.ok) throw new Error(data.error || 'Local Agent 请求失败')
  return data
}

export async function initLocalTask(base: string, payload: any) {
  return req(base, '/api/v1/tasks/init', { method: 'POST', body: JSON.stringify(payload) })
}

export async function listLocalCandidates(base: string, taskID: string) {
  const data = await req(base, `/api/v1/tasks/${encodeURIComponent(taskID)}/candidates`)
  return data.data || data
}

export async function deleteLocalCandidate(base: string, taskID: string, candidateID: string) {
  return req(base, `/api/v1/tasks/${encodeURIComponent(taskID)}/candidates/${encodeURIComponent(candidateID)}`, { method: 'DELETE' })
}

export async function listLocalScreenshots(base: string, taskID: string) {
  const data = await req(base, `/api/v1/tasks/${encodeURIComponent(taskID)}/screenshots`)
  return data.screenshots
}

export async function listLocalProfiles(base: string) {
  const data = await req(base, '/api/v1/profiles')
  return data.profiles
}
