// GoodHR 5 本地 Agent API 封装
export function agentURL(base: string, path: string): string {
  if (base.endsWith('/')) base = base.slice(0, -1)
  return `${base}${path}`
}

type AgentRequestOptions = Omit<RequestInit, 'body'> & {
  body?: BodyInit | Record<string, any> | null
}

async function req(base: string, path: string, opts: AgentRequestOptions = {}) {
  const { body, ...rest } = opts
  const res = await fetch(agentURL(base, path), {
    headers: { 'Content-Type': 'application/json', ...(opts.headers as Record<string, string> | undefined) },
    ...rest,
    body: serializeBody(body),
  })
  const data = await res.json()
  if (!res.ok || !data.ok) throw new Error(data.error || 'Local Agent 请求失败')
  return data
}

function serializeBody(body: AgentRequestOptions['body']) {
  if (body == null) return undefined
  if (typeof body === 'string' || body instanceof FormData || body instanceof Blob) return body
  return JSON.stringify(body)
}

export async function getLocalHealth(base: string) {
  const res = await fetch(agentURL(base, '/health'), { cache: 'no-store' })
  const data = await res.json()
  if (!res.ok) throw new Error(data.error || 'Local Agent 不可用')
  return data
}

export async function bindCloudUser(base: string, payload: any) {
  return req(base, '/api/v1/session/bind-cloud-user', { method: 'POST', body: payload })
}

export async function initLocalTask(base: string, payload: any) {
  return req(base, '/api/v1/tasks/init', { method: 'POST', body: payload })
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
