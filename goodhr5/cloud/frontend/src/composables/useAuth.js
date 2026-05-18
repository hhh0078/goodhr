/** 云端认证逻辑 */
import { ref } from 'vue'

const TOKEN_KEY = 'goodhr5_access_token'
const CLOUD_API_BASE = window.GOODHR_CLOUD_API || 'http://127.0.0.1:8080'

export function useAuth() {
  const email = ref('')
  const code = ref('')
  const devCode = ref('')
  const token = ref(localStorage.getItem(TOKEN_KEY) || '')
  const user = ref(null)
  const error = ref('')
  const loading = ref(false)

  async function sendCode() {
    loading.value = true
    error.value = ''
    devCode.value = ''
    try {
      const res = await fetch(`${CLOUD_API_BASE}/api/auth/send-code`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: email.value })
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || '发送失败')
      if (data.debug_code) {
        devCode.value = data.debug_code
        code.value = data.debug_code
      }
    } catch (e) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function login() {
    loading.value = true
    error.value = ''
    try {
      const res = await fetch(`${CLOUD_API_BASE}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: email.value, code: code.value })
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || '登录失败')
      token.value = data.access_token
      localStorage.setItem(TOKEN_KEY, data.access_token)
      user.value = data.user
    } catch (e) {
      error.value = e.message
    } finally {
      loading.value = false
    }
  }

  async function loadCurrentUser() {
    if (!token.value) return
    try {
      const res = await fetch(`${CLOUD_API_BASE}/api/auth/me`, {
        headers: { Authorization: `Bearer ${token.value}` }
      })
      const data = await res.json()
      if (!res.ok) throw new Error(data.error || '登录已过期')
      user.value = data.user
    } catch {
      logout()
    }
  }

  function logout() {
    token.value = ''
    user.value = null
    localStorage.removeItem(TOKEN_KEY)
  }

  return { email, code, devCode, token, user, error, loading, sendCode, login, loadCurrentUser, logout, CLOUD_API_BASE }
}
