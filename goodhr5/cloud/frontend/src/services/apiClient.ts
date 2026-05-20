// GoodHR 5 统一 API 客户端
const BASE = () => (window as any).GOODHR_CLOUD_API || 'http://127.0.0.1:8084'
export async function api(path: string, opts: RequestInit = {}): Promise<any> {
  const res = await fetch(`${BASE()}${path}`, {
    headers: { Authorization: `Bearer ${localStorage.getItem('goodhr5_access_token') || ''}`, 'Content-Type': 'application/json', ...opts.headers as Record<string, string> },
    ...opts,
  })
  const data = await res.json()
  if (!data.ok) throw new Error(data.error || '请求失败')
  return data
}
