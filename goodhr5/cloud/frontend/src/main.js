import { createApp, ref } from 'vue'
import './style.css'

const LOCAL_PORTS = [9001, 9002, 9003, 9004, 9005, 9006, 9007, 9008, 9009]

const App = {
  setup() {
    const agentStatus = ref('未检测')
    const agentInfo = ref(null)
    const checking = ref(false)

    async function detectLocalAgent() {
      checking.value = true
      agentInfo.value = null
      agentStatus.value = '检测中'

      for (const port of LOCAL_PORTS) {
        try {
          const response = await fetch(`http://127.0.0.1:${port}/health`, {
            method: 'GET',
            cache: 'no-store'
          })
          if (!response.ok) {
            continue
          }
          const data = await response.json()
          agentInfo.value = { ...data, port }
          agentStatus.value = '已连接'
          checking.value = false
          return
        } catch {
          // Try next port.
        }
      }

      agentStatus.value = '未连接'
      checking.value = false
    }

    return {
      agentStatus,
      agentInfo,
      checking,
      detectLocalAgent
    }
  },
  template: `
    <main class="shell">
      <section class="topbar">
        <div>
          <h1>GoodHR 5</h1>
          <p>云端控制台 + 本地执行器</p>
        </div>
        <button type="button" :disabled="checking" @click="detectLocalAgent">
          {{ checking ? '检测中...' : '检测本地程序' }}
        </button>
      </section>

      <section class="panel">
        <h2>本地 Agent</h2>
        <dl>
          <dt>状态</dt>
          <dd>{{ agentStatus }}</dd>
          <dt>端口</dt>
          <dd>{{ agentInfo?.port || '-' }}</dd>
          <dt>版本</dt>
          <dd>{{ agentInfo?.version || '-' }}</dd>
          <dt>机器码</dt>
          <dd>{{ agentInfo?.machine_id || '-' }}</dd>
        </dl>
      </section>
    </main>
  `
}

createApp(App).mount('#app')
