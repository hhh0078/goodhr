/** 本地 Agent 探测和绑定 */
import { ref } from 'vue'

const LOCAL_PORTS = [9001, 9002, 9003, 9004, 9005, 9006, 9007, 9008, 9009]

export function useAgent() {
  const status = ref('未检测')
  const info = ref(null)
  const bindStatus = ref('未绑定')
  const bindError = ref('')
  const checking = ref(false)
  const baseUrl = ref('')

  async function detect(user, token) {
    if (!user) return
    checking.value = true
    info.value = null
    bindStatus.value = '未绑定'
    bindError.value = ''
    status.value = '检测中'

    for (const port of LOCAL_PORTS) {
      try {
        const res = await fetch(`http://127.0.0.1:${port}/health`, { cache: 'no-store' })
        if (!res.ok) continue
        const data = await res.json()
        info.value = data
        status.value = `已连接 (端口 ${port})`
        baseUrl.value = `http://127.0.0.1:${port}`

        // 如果本地未绑定当前云端用户，自动绑定
        if (data.bound_cloud_user_id !== user.id) {
          await bind(user, token)
        } else {
          bindStatus.value = '已绑定'
        }
        return
      } catch { /* 端口不可达，继续下一个 */ }
    }

    status.value = '未检测到本地程序'
    info.value = null
    baseUrl.value = ''
    checking.value = false
  }

  async function bind(user, token) {
    bindStatus.value = '绑定中'
    bindError.value = ''
    try {
      const res = await fetch(`${baseUrl.value}/api/v1/session/bind-cloud-user`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          cloud_user_id: user.id,
          cloud_email: user.email,
          agent_token: token
        })
      })
      const data = await res.json()
      if (!res.ok || !data.ok) throw new Error(data.error || '绑定失败')
      bindStatus.value = '已绑定'
    } catch (e) {
      bindError.value = e.message
      bindStatus.value = '绑定失败'
    } finally {
      checking.value = false
    }
  }

  return { status, info, bindStatus, bindError, checking, baseUrl, detect }
}
