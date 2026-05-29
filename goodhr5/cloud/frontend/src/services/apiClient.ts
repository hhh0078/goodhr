// GoodHR 5 云端请求工具。业务 API 模块统一调用这里。
export const TOKEN_KEY = 'goodhr5_access_token'

export class ApiError extends Error {
  status: number
  data: any

  constructor(message: string, status = 0, data: any = null) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.data = data
  }
}

export function cloudApiBase() {
  if (window.GOODHR_CLOUD_API) return window.GOODHR_CLOUD_API
  return import.meta.env.DEV ? 'http://127.0.0.1:8084' : 'https://goodhr5.58it.cn'
}

export function getAccessToken() {
  return localStorage.getItem(TOKEN_KEY) || ''
}

export function setAccessToken(token: string) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

type ApiOptions = Omit<RequestInit, 'body'> & {
  body?: BodyInit | Record<string, any> | null
  auth?: boolean
}

export async function api(path: string, opts: ApiOptions = {}): Promise<any> {
  const { auth = true, body, headers, ...rest } = opts
  const requestHeaders: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(auth ? { Authorization: `Bearer ${getAccessToken()}` } : {}),
    ...(headers as Record<string, string> | undefined),
  }

  let res: Response
  try {
    res = await fetch(`${cloudApiBase()}${path}`, {
      ...rest,
      headers: requestHeaders,
      body: serializeBody(body),
    })
  } catch (error: any) {
    throw new ApiError(error?.message || '网络请求失败', 0, null)
  }
  const data = await parseJSON(res)
  if (!res.ok || data.ok === false) throw new ApiError(data.error || '请求失败', res.status, data)
  return data
}

function serializeBody(body: ApiOptions['body']) {
  if (body == null) return undefined
  if (typeof body === 'string' || body instanceof FormData || body instanceof Blob) return body
  return JSON.stringify(body)
}

async function parseJSON(res: Response) {
  const text = await res.text()
  if (!text) return {}
  try {
    return JSON.parse(text)
  } catch {
    throw new ApiError('响应不是有效 JSON', res.status, null)
  }
}
