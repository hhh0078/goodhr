import { createApp, onMounted, ref } from 'vue'
import './style.css'

const LOCAL_PORTS = [9001, 9002, 9003, 9004, 9005, 9006, 9007, 9008, 9009]
const CLOUD_API_BASE = window.GOODHR_CLOUD_API || 'http://127.0.0.1:8080'
const TOKEN_KEY = 'goodhr5_access_token'

const App = {
  setup() {
    const email = ref('')
    const code = ref('')
    const devCode = ref('')
    const authToken = ref(localStorage.getItem(TOKEN_KEY) || '')
    const user = ref(null)
    const authError = ref('')
    const authLoading = ref(false)
    const agentStatus = ref('未检测')
    const agentInfo = ref(null)
    const checking = ref(false)

    async function sendCode() {
      authLoading.value = true
      authError.value = ''
      devCode.value = ''

      try {
        const response = await fetch(`${CLOUD_API_BASE}/api/auth/send-code`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: email.value })
        })
        const data = await response.json()
        if (!response.ok) {
          throw new Error(data.error || '验证码发送失败')
        }
        if (data.debug_code) {
          devCode.value = data.debug_code
          code.value = data.debug_code
        }
      } catch (error) {
        authError.value = error.message
      } finally {
        authLoading.value = false
      }
    }

    async function login() {
      authLoading.value = true
      authError.value = ''

      try {
        const response = await fetch(`${CLOUD_API_BASE}/api/auth/login`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ email: email.value, code: code.value })
        })
        const data = await response.json()
        if (!response.ok) {
          throw new Error(data.error || '登录失败')
        }

        authToken.value = data.access_token
        localStorage.setItem(TOKEN_KEY, data.access_token)
        user.value = data.user
        await detectLocalAgent()
      } catch (error) {
        authError.value = error.message
      } finally {
        authLoading.value = false
      }
    }

    async function loadCurrentUser() {
      if (!authToken.value) {
        return
      }

      try {
        const response = await fetch(`${CLOUD_API_BASE}/api/auth/me`, {
          headers: { Authorization: `Bearer ${authToken.value}` }
        })
        const data = await response.json()
        if (!response.ok) {
          throw new Error(data.error || '登录已过期')
        }
        user.value = data.user
        await detectLocalAgent()
      } catch {
        logout()
      }
    }

    function logout() {
      authToken.value = ''
      user.value = null
      agentStatus.value = '未检测'
      agentInfo.value = null
      localStorage.removeItem(TOKEN_KEY)
    }

    async function detectLocalAgent() {
      if (!user.value) {
        return
      }

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

    onMounted(() => {
      loadCurrentUser()
    })

    return {
      email,
      code,
      devCode,
      user,
      authError,
      authLoading,
      agentStatus,
      agentInfo,
      checking,
      sendCode,
      login,
      logout,
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
        <button v-if="user" type="button" :disabled="checking" @click="detectLocalAgent">
          {{ checking ? '检测中...' : '检测本地程序' }}
        </button>
      </section>

      <section v-if="!user" class="panel auth-panel">
        <h2>邮箱验证码登录</h2>
        <div class="form-grid">
          <label>
            邮箱
            <input v-model="email" type="email" autocomplete="email" placeholder="your@example.com" />
          </label>
          <label>
            验证码
            <input v-model="code" type="text" inputmode="numeric" maxlength="4" placeholder="4位验证码" />
          </label>
        </div>
        <p v-if="devCode" class="hint">开发验证码：{{ devCode }}</p>
        <p v-if="authError" class="error">{{ authError }}</p>
        <div class="actions">
          <button type="button" :disabled="authLoading || !email" @click="sendCode">
            {{ authLoading ? '处理中...' : '发送验证码' }}
          </button>
          <button type="button" :disabled="authLoading || !email || !code" @click="login">
            登录
          </button>
        </div>
      </section>

      <section v-else class="panel user-panel">
        <h2>云端账号</h2>
        <dl>
          <dt>邮箱</dt>
          <dd>{{ user.email }}</dd>
        </dl>
        <div class="actions">
          <button type="button" class="ghost" @click="logout">退出登录</button>
        </div>
      </section>

      <section v-if="user" class="panel">
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

      <section v-if="user && agentStatus === '未连接'" class="panel notice">
        <h2>未检测到本地程序</h2>
        <p>请先下载并启动 GoodHR 本地程序。启动后保持程序运行，再回到本页面重新检测。</p>
        <div class="actions">
          <a class="button secondary" href="#" aria-disabled="true">下载本地程序</a>
          <button type="button" :disabled="checking" @click="detectLocalAgent">
            重新检测
          </button>
        </div>
        <ol>
          <li>下载 GoodHR 本地程序。</li>
          <li>打开本地程序，它会自动尝试监听 9001-9009 端口。</li>
          <li>回到云端页面点击重新检测。</li>
        </ol>
      </section>
    </main>
  `
}

createApp(App).mount('#app')
