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
