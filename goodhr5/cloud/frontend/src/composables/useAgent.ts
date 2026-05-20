/** 本地 Agent 探测和绑定 */
import { ref } from 'vue'
import { bindCloudUser, getLocalHealth } from '../services/localAgentApi'

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
        const candidateBaseUrl = `http://127.0.0.1:${port}`
        const data = await getLocalHealth(candidateBaseUrl)
        info.value = data
        status.value = `已连接 (端口 ${port})`
        baseUrl.value = candidateBaseUrl

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
      await bindCloudUser(baseUrl.value, {
        cloud_user_id: user.id,
        cloud_email: user.email,
        agent_token: token
      })
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
