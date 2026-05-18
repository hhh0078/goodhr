// 本文件负责 GoodHR 5 云端首页的邮箱登录、本地 Agent 探测和账号绑定初始化。
import { computed, createApp, onMounted, ref } from 'vue'
import { listPlatformAccounts } from './services/cloudApi.js'
import './style.css'

const LOCAL_PORTS = [9001, 9002, 9003, 9004, 9005, 9006, 9007, 9008, 9009]
const CLOUD_API_BASE = window.GOODHR_CLOUD_API || 'http://127.0.0.1:8080'
const TOKEN_KEY = 'goodhr5_access_token'
const LOCAL_TOKEN_KEY = 'goodhr5_local_agent_token'

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
    const bindStatus = ref('未绑定')
    const bindError = ref('')
    const checking = ref(false)
    const platformAccounts = ref([])
    const taskError = ref('')
    const taskForm = ref({
      platformId: 'boss',
      platformAccountId: '',
      mode: 'keyword',
      matchLimit: 20
    })
    const tasks = ref([])

    const selectedPlatformAccounts = computed(() => {
      return platformAccounts.value.filter((account) => account.platform_id === taskForm.value.platformId)
    })

    // sendCode 调用云端接口发送邮箱验证码。
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

    // login 调用云端登录接口，用验证码换取访问 token。
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
        // 登录成功后探测本地 Agent，用于初始化本地执行环境。
        await detectLocalAgent()
        // 登录成功后读取平台账号映射，用于任务创建时选择账号。
        await loadPlatformAccounts()
      } catch (error) {
        authError.value = error.message
      } finally {
        authLoading.value = false
      }
    }

    // loadCurrentUser 使用本地保存的 token 恢复云端登录态。
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
        // 恢复登录态后探测本地 Agent，用于保持云端和本地的绑定状态。
        await detectLocalAgent()
        // 恢复登录态后读取平台账号映射，用于刷新任务创建表单。
        await loadPlatformAccounts()
      } catch {
        logout()
      }
    }

    // logout 清理云端登录态和当前页面上的本地 Agent 状态。
    function logout() {
      authToken.value = ''
      user.value = null
      agentStatus.value = '未检测'
      agentInfo.value = null
      bindStatus.value = '未绑定'
      bindError.value = ''
      platformAccounts.value = []
      tasks.value = []
      taskError.value = ''
      localStorage.removeItem(TOKEN_KEY)
    }

    // detectLocalAgent 依次探测 9001-9009，找到本地 Agent 后执行绑定初始化。
    async function detectLocalAgent() {
      if (!user.value) {
        return
      }

      checking.value = true
      agentInfo.value = null
      bindStatus.value = '未绑定'
      bindError.value = ''
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
          // 探测成功后初始化绑定，让云端和本地 Agent 都知道当前账号和机器。
          await initializeAgentBinding(agentInfo.value)
          checking.value = false
          return
        } catch {
          // Try next port.
        }
      }

      agentStatus.value = '未连接'
      checking.value = false
    }

    // initializeAgentBinding 同步云端机器绑定，并把云端账号写入本地 Agent。
    async function initializeAgentBinding(agent) {
      if (!agent?.machine_id) {
        bindStatus.value = '绑定失败'
        bindError.value = '本地 Agent 未返回 machine_id'
        return
      }

      bindStatus.value = '绑定中'

      try {
        // 调用云端机器绑定接口，用于记录当前账号对应的本地机器。
        await bindCloudAgent(agent)
        // 调用本地账号绑定接口，用于让 Local Agent 保存当前云端账号。
        await bindLocalAgent(agent)
        bindStatus.value = '已绑定'
      } catch (error) {
        bindStatus.value = '绑定失败'
        bindError.value = error.message
      }
    }

    // bindCloudAgent 调用云端 API 保存账号和机器码绑定关系。
    async function bindCloudAgent(agent) {
      const response = await fetch(`${CLOUD_API_BASE}/api/agents/bind`, {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${authToken.value}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({
          machine_id: agent.machine_id,
          agent_version: agent.version || '',
          local_port: agent.port
        })
      })
      const data = await response.json()
      if (!response.ok) {
        throw new Error(data.error || '云端机器绑定失败')
      }
    }

    // bindLocalAgent 调用本地 API 保存当前云端账号信息。
    async function bindLocalAgent(agent) {
      const localToken = ensureLocalAgentToken()
      const response = await fetch(`http://127.0.0.1:${agent.port}/api/v1/session/bind-cloud-user`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          cloud_user_id: user.value.email,
          cloud_email: user.value.email,
          agent_token: localToken
        })
      })
      const data = await response.json()
      if (!response.ok) {
        throw new Error(data.error || '本地账号绑定失败')
      }
    }

    // ensureLocalAgentToken 读取或生成本地 Agent 调用 token。
    function ensureLocalAgentToken() {
      const saved = localStorage.getItem(LOCAL_TOKEN_KEY)
      if (saved) {
        return saved
      }

      const token = crypto.randomUUID()
      localStorage.setItem(LOCAL_TOKEN_KEY, token)
      return token
    }

    // loadPlatformAccounts 调用云端 API 读取当前平台账号映射。
    async function loadPlatformAccounts() {
      if (!authToken.value) {
        return
      }

      try {
        taskError.value = ''
        // 调用云端平台账号接口，供任务创建表单选择不同账号/profile。
        const data = await listPlatformAccounts(authToken.value, '')
        platformAccounts.value = data.accounts || []
        if (!taskForm.value.platformAccountId && selectedPlatformAccounts.value.length > 0) {
          taskForm.value.platformAccountId = selectedPlatformAccounts.value[0].id
        }
      } catch (error) {
        taskError.value = error.message
      }
    }

    // onPlatformChange 在切换平台时自动选择该平台的第一个账号。
    function onPlatformChange() {
      const firstAccount = selectedPlatformAccounts.value[0]
      taskForm.value.platformAccountId = firstAccount?.id || ''
    }

    // createTaskDraft 创建一个前端任务草稿，后续会接入云端任务 API。
    function createTaskDraft() {
      taskError.value = ''
      const account = platformAccounts.value.find((item) => item.id === taskForm.value.platformAccountId)
      if (!account) {
        taskError.value = '请先选择平台账号'
        return
      }

      const task = {
        id: `task_${Date.now()}`,
        platform_id: taskForm.value.platformId,
        platform_account_id: account.id,
        platform_account_name: account.display_name,
        mode: taskForm.value.mode,
        match_limit: Number(taskForm.value.matchLimit) || 0,
        status: 'created',
        scanned_count: 0,
        greeted_count: 0,
        skipped_count: 0,
        failed_count: 0
      }
      tasks.value.unshift(task)
    }

    onMounted(() => {
      // 页面加载时尝试恢复登录态，恢复成功后再探测本地 Agent。
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
      bindStatus,
      bindError,
      checking,
      platformAccounts,
      selectedPlatformAccounts,
      taskForm,
      taskError,
      tasks,
      sendCode,
      login,
      logout,
      detectLocalAgent,
      loadPlatformAccounts,
      onPlatformChange,
      createTaskDraft
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
          <dt>绑定</dt>
          <dd>{{ bindStatus }}</dd>
        </dl>
        <p v-if="bindError" class="error">{{ bindError }}</p>
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

      <section v-if="user" class="panel task-panel">
        <div class="section-header">
          <h2>创建任务</h2>
          <button type="button" class="ghost" @click="loadPlatformAccounts">刷新账号</button>
        </div>
        <div class="form-grid">
          <label>
            平台
            <select v-model="taskForm.platformId" @change="onPlatformChange">
              <option value="boss">Boss直聘</option>
              <option value="zhaopin">智联招聘</option>
              <option value="liepin">猎聘</option>
            </select>
          </label>
          <label>
            账号
            <select v-model="taskForm.platformAccountId">
              <option value="">请选择账号</option>
              <option v-for="account in selectedPlatformAccounts" :key="account.id" :value="account.id">
                {{ account.display_name }} / {{ account.local_profile_id }}
              </option>
            </select>
          </label>
          <label>
            筛选模式
            <select v-model="taskForm.mode">
              <option value="keyword">关键词筛选</option>
              <option value="ai">AI筛选</option>
            </select>
          </label>
          <label>
            匹配上限
            <input v-model="taskForm.matchLimit" type="number" min="1" />
          </label>
        </div>
        <p v-if="selectedPlatformAccounts.length === 0" class="hint">
          当前平台还没有账号映射，请先通过本地 Agent 创建 profile 并同步到云端账号映射。
        </p>
        <p v-if="taskError" class="error">{{ taskError }}</p>
        <div class="actions">
          <button type="button" :disabled="selectedPlatformAccounts.length === 0" @click="createTaskDraft">
            创建任务
          </button>
        </div>
      </section>

      <section v-if="user" class="panel">
        <h2>任务列表</h2>
        <p v-if="tasks.length === 0" class="hint">暂无任务</p>
        <div v-else class="task-list">
          <article v-for="task in tasks" :key="task.id" class="task-item">
            <div>
              <strong>{{ task.platform_account_name }}</strong>
              <p>{{ task.platform_id }} / {{ task.mode }} / 上限 {{ task.match_limit }}</p>
            </div>
            <dl>
              <dt>状态</dt>
              <dd>{{ task.status }}</dd>
              <dt>扫描</dt>
              <dd>{{ task.scanned_count }}</dd>
              <dt>打招呼</dt>
              <dd>{{ task.greeted_count }}</dd>
              <dt>跳过</dt>
              <dd>{{ task.skipped_count }}</dd>
              <dt>失败</dt>
              <dd>{{ task.failed_count }}</dd>
            </dl>
          </article>
        </div>
      </section>
    </main>
  `
}

createApp(App).mount('#app')
