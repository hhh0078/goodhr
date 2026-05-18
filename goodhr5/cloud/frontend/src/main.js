// 本文件负责 GoodHR 5 云端首页的邮箱登录、本地 Agent 探测和账号绑定初始化。
import { computed, createApp, onMounted, ref } from 'vue'
import {
  createTask,
  deletePosition,
  listPlatformAccounts,
  listPositions,
  listTaskLogs,
  listTasks,
  savePosition
} from './services/cloudApi.js'
import { deleteLocalCandidate, initLocalTask, listLocalCandidates } from './services/localAgentApi.js'
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
    const positions = ref([])
    const positionError = ref('')
    const positionLoading = ref(false)
    const taskError = ref('')
    const taskLoading = ref(false)
    const expandedTaskId = ref('')
    const taskLogs = ref({})
    const candidateExpandedTaskId = ref('')
    const taskCandidates = ref({})
    const candidateLoadingTaskId = ref('')
    const candidateError = ref('')
    const taskForm = ref({
      platformId: 'boss',
      platformAccountId: '',
      positionId: '',
      mode: 'keyword',
      matchLimit: 20
    })
    const positionForm = ref({
      id: '',
      name: '',
      keywords: '',
      excludeKeywords: '',
      description: '',
      greetMessage: '',
      isAndMode: false
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
        // 登录成功后读取岗位配置，用于复用关键词和问候语模板。
        await loadPositions()
        // 登录成功后读取平台账号映射，用于任务创建时选择账号。
        await loadPlatformAccounts()
        // 登录成功后读取云端任务列表，用于恢复任务控制台状态。
        await loadTasks()
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
        // 恢复登录态后读取岗位配置，用于刷新岗位模板面板。
        await loadPositions()
        // 恢复登录态后读取平台账号映射，用于刷新任务创建表单。
        await loadPlatformAccounts()
        // 恢复登录态后读取云端任务列表，用于刷新任务控制台。
        await loadTasks()
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
      positions.value = []
      tasks.value = []
      taskLogs.value = {}
      taskCandidates.value = {}
      expandedTaskId.value = ''
      candidateExpandedTaskId.value = ''
      positionError.value = ''
      taskError.value = ''
      candidateError.value = ''
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

    // loadPositions 调用云端岗位配置 API 读取岗位模板列表。
    async function loadPositions() {
      if (!authToken.value) {
        return
      }

      try {
        positionError.value = ''
        // 调用岗位配置列表接口，用于网页复用关键词和默认问候语模板。
        const data = await listPositions(authToken.value)
        positions.value = data.positions || []
      } catch (error) {
        positionError.value = error.message
      }
    }

    // savePositionDraft 调用云端岗位配置 API 保存岗位模板。
    async function savePositionDraft() {
      positionLoading.value = true
      positionError.value = ''

      try {
        // 调用岗位配置保存接口，把当前表单写成一个可复用模板。
        await savePosition(authToken.value, {
          id: positionForm.value.id,
          name: positionForm.value.name,
          keywords: parseLineItems(positionForm.value.keywords),
          exclude_keywords: parseLineItems(positionForm.value.excludeKeywords),
          description: positionForm.value.description,
          greet_message: positionForm.value.greetMessage,
          is_and_mode: positionForm.value.isAndMode
        })
        resetPositionForm()
        // 保存成功后重新读取岗位模板列表，保证页面和云端一致。
        await loadPositions()
      } catch (error) {
        positionError.value = error.message
      } finally {
        positionLoading.value = false
      }
    }

    // editPosition 将已有岗位模板回填到表单，便于修改。
    function editPosition(position) {
      positionForm.value = {
        id: position.id,
        name: position.name || '',
        keywords: (position.keywords || []).join('\n'),
        excludeKeywords: (position.exclude_keywords || []).join('\n'),
        description: position.description || '',
        greetMessage: position.greet_message || '',
        isAndMode: Boolean(position.is_and_mode)
      }
    }

    // removePosition 调用云端岗位配置 API 删除岗位模板。
    async function removePosition(positionID) {
      positionLoading.value = true
      positionError.value = ''

      try {
        // 调用岗位配置删除接口，移除当前不再使用的模板。
        await deletePosition(authToken.value, positionID)
        if (positionForm.value.id === positionID) {
          resetPositionForm()
        }
        // 删除成功后重新读取岗位模板列表，保持页面和云端一致。
        await loadPositions()
      } catch (error) {
        positionError.value = error.message
      } finally {
        positionLoading.value = false
      }
    }

    // resetPositionForm 清空岗位模板编辑表单。
    function resetPositionForm() {
      positionForm.value = {
        id: '',
        name: '',
        keywords: '',
        excludeKeywords: '',
        description: '',
        greetMessage: '',
        isAndMode: false
      }
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

    // createTaskDraft 调用云端任务 API 创建任务记录。
    async function createTaskDraft() {
      taskError.value = ''
      const account = platformAccounts.value.find((item) => item.id === taskForm.value.platformAccountId)
      if (!account) {
        taskError.value = '请先选择平台账号'
        return
      }

      taskLoading.value = true
      try {
        // 调用云端任务 API 创建任务元信息，后续再交给 Local Agent 执行。
        const data = await createTask(authToken.value, {
          platform_id: taskForm.value.platformId,
          platform_account_id: account.id,
          position_id: taskForm.value.positionId,
          mode: taskForm.value.mode,
          match_limit: Number(taskForm.value.matchLimit) || 0
        })
        if (agentInfo.value) {
          // 调用本地任务初始化接口，为云端任务创建对应的本地 candidates.json。
          await initializeLocalTask(data.task)
        }
        // 创建成功后重新读取云端任务列表，保证页面展示与云端一致。
        await loadTasks()
      } catch (error) {
        taskError.value = error.message
      } finally {
        taskLoading.value = false
      }
    }

    // loadTasks 调用云端任务 API 读取当前用户任务列表。
    async function loadTasks() {
      if (!authToken.value) {
        return
      }

      try {
        taskError.value = ''
        // 调用云端任务列表接口，用于展示任务统计摘要。
        const data = await listTasks(authToken.value)
        tasks.value = (data.tasks || []).map((task) => ({
          ...task,
          platform_account_name: accountName(task.platform_account_id),
          position_name: positionName(task.position_id)
        }))
      } catch (error) {
        taskError.value = error.message
      }
    }

    // toggleTaskLogs 展开或收起任务日志面板。
    async function toggleTaskLogs(taskID) {
      if (expandedTaskId.value === taskID) {
        expandedTaskId.value = ''
        return
      }

      expandedTaskId.value = taskID
      await loadTaskLogs(taskID)
    }

    // toggleTaskCandidates 展开或收起任务候选人面板。
    async function toggleTaskCandidates(task) {
      const taskID = localTaskID(task)
      if (candidateExpandedTaskId.value === taskID) {
        candidateExpandedTaskId.value = ''
        return
      }

      candidateExpandedTaskId.value = taskID
      // 展开候选人面板时调用本地 Agent，读取该任务自己的 candidates.json。
      await loadTaskCandidates(task)
    }

    // loadTaskCandidates 调用本地 Agent 读取任务候选人列表。
    async function loadTaskCandidates(task) {
      if (!agentInfo.value) {
        candidateError.value = '本地 Agent 未连接，无法读取候选人数据'
        return
      }

      const taskID = localTaskID(task)
      candidateLoadingTaskId.value = taskID
      candidateError.value = ''

      try {
        // 读取候选人前先初始化本地任务目录，避免旧任务没有本地 JSON。
        await initializeLocalTask(task)
        // 调用本地候选人读取接口，候选人详情只从用户电脑上的 JSON 获取。
        const data = await listLocalCandidates(agentInfo.value, taskID)
        taskCandidates.value = {
          ...taskCandidates.value,
          [taskID]: data.data || {}
        }
      } catch (error) {
        candidateError.value = error.message
        taskCandidates.value = {
          ...taskCandidates.value,
          [taskID]: {}
        }
      } finally {
        candidateLoadingTaskId.value = ''
      }
    }

    // removeCandidate 调用本地 Agent 删除任务里的候选人记录。
    async function removeCandidate(task, candidate) {
      if (!agentInfo.value) {
        candidateError.value = '本地 Agent 未连接，无法删除候选人数据'
        return
      }

      const taskID = localTaskID(task)
      candidateError.value = ''

      try {
        // 调用本地候选人删除接口，只修改当前任务对应的本地 JSON。
        await deleteLocalCandidate(agentInfo.value, taskID, candidate.id)
        // 删除后重新读取候选人列表，用于保持页面和本地 JSON 一致。
        await loadTaskCandidates(task)
      } catch (error) {
        candidateError.value = error.message
      }
    }

    // initializeLocalTask 调用本地 Agent 创建云端任务对应的本地任务目录。
    async function initializeLocalTask(task) {
      if (!agentInfo.value || !task) {
        return
      }

      const position = positions.value.find((item) => item.id === task.position_id)

      // 调用本地任务初始化接口，保证每个云端任务都有独立候选人 JSON。
      await initLocalTask(agentInfo.value, {
        task_id: localTaskID(task),
        cloud_user_id: user.value.email,
        platform_id: task.platform_id,
        platform_account_id: task.platform_account_id,
        position_snapshot: position
          ? {
              id: position.id,
              name: position.name,
              keywords: position.keywords || [],
              exclude_keywords: position.exclude_keywords || [],
              description: position.description || '',
              greet_message: position.greet_message || '',
              is_and_mode: Boolean(position.is_and_mode)
            }
          : {}
      })
    }

    // loadTaskLogs 调用云端任务日志 API 读取任务运行摘要。
    async function loadTaskLogs(taskID) {
      try {
        taskError.value = ''
        // 调用云端任务日志接口，用于任务卡片展开时展示运行过程。
        const data = await listTaskLogs(authToken.value, taskID)
        taskLogs.value = {
          ...taskLogs.value,
          [taskID]: data.logs || []
        }
      } catch (error) {
        taskError.value = error.message
      }
    }

    // accountName 根据平台账号 ID 返回可读账号名称。
    function accountName(accountID) {
      const account = platformAccounts.value.find((item) => item.id === accountID)
      return account?.display_name || accountID
    }

    // positionName 根据岗位模板 ID 返回可读名称。
    function positionName(positionID) {
      if (!positionID) {
        return ''
      }
      const position = positions.value.find((item) => item.id === positionID)
      return position?.name || positionID
    }

    // localTaskID 返回云端任务对应的本地任务 ID。
    function localTaskID(task) {
      return task.local_task_id || task.id
    }

    // candidateTitle 返回候选人在列表中的主要展示名称。
    function candidateTitle(candidate) {
      return candidate.name || candidate.title || candidate.candidate_name || candidate.id || '未命名候选人'
    }

    // candidateSubtitle 返回候选人在列表中的辅助展示信息。
    function candidateSubtitle(candidate) {
      const parts = [
        candidate.age,
        candidate.education,
        candidate.experience,
        candidate.status || candidate.result
      ].filter(Boolean)
      return parts.join(' / ') || '暂无摘要'
    }

    // candidateDetail 返回候选人的详情摘要文本。
    function candidateDetail(candidate) {
      return candidate.detail || candidate.details || candidate.skills || candidate.description || candidate.raw_text || ''
    }

    // taskCandidateItems 返回本地任务数据里的候选人数组。
    function taskCandidateItems(task) {
      const taskData = taskCandidates.value[localTaskID(task)] || {}
      return taskData.items || []
    }

    // taskPositionSnapshot 返回本地任务里保存的岗位模板快照。
    function taskPositionSnapshot(task) {
      const taskData = taskCandidates.value[localTaskID(task)] || {}
      return taskData.position_snapshot || {}
    }

    // parseLineItems 把多行文本拆成字符串数组。
    function parseLineItems(value) {
      return String(value || '')
        .split('\n')
        .map((item) => item.trim())
        .filter(Boolean)
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
      positions,
      positionError,
      positionLoading,
      selectedPlatformAccounts,
      positionForm,
      taskForm,
      taskError,
      taskLoading,
      expandedTaskId,
      taskLogs,
      candidateExpandedTaskId,
      taskCandidates,
      candidateLoadingTaskId,
      candidateError,
      tasks,
      sendCode,
      login,
      logout,
      detectLocalAgent,
      loadPositions,
      savePositionDraft,
      editPosition,
      removePosition,
      resetPositionForm,
      loadPlatformAccounts,
      onPlatformChange,
      createTaskDraft,
      loadTasks,
      toggleTaskLogs,
      toggleTaskCandidates,
      loadTaskCandidates,
      removeCandidate,
      localTaskID,
      positionName,
      candidateTitle,
      candidateSubtitle,
      candidateDetail,
      taskCandidateItems,
      taskPositionSnapshot
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

      <section v-if="user" class="panel">
        <div class="section-header">
          <h2>岗位模板</h2>
          <button type="button" class="ghost" @click="loadPositions">刷新模板</button>
        </div>
        <div class="form-grid">
          <label>
            名称
            <input v-model="positionForm.name" type="text" placeholder="例如：带货主播" />
          </label>
          <label class="toggle-field">
            匹配模式
            <label class="checkbox-row">
              <input v-model="positionForm.isAndMode" type="checkbox" />
              <span>使用 AND 匹配</span>
            </label>
          </label>
          <label>
            关键词
            <textarea v-model="positionForm.keywords" rows="5" placeholder="每行一个关键词"></textarea>
          </label>
          <label>
            排除词
            <textarea v-model="positionForm.excludeKeywords" rows="5" placeholder="每行一个排除词"></textarea>
          </label>
          <label>
            岗位描述
            <textarea v-model="positionForm.description" rows="4" placeholder="岗位说明"></textarea>
          </label>
          <label>
            默认问候语
            <textarea v-model="positionForm.greetMessage" rows="4" placeholder="默认打招呼文案"></textarea>
          </label>
        </div>
        <p v-if="positionError" class="error">{{ positionError }}</p>
        <div class="actions">
          <button type="button" :disabled="positionLoading || !positionForm.name" @click="savePositionDraft">
            {{ positionLoading ? '保存中...' : (positionForm.id ? '更新模板' : '保存模板') }}
          </button>
          <button type="button" class="ghost" :disabled="positionLoading" @click="resetPositionForm">清空表单</button>
        </div>
        <p v-if="positions.length === 0" class="hint">暂无岗位模板</p>
        <div v-else class="position-list">
          <article v-for="position in positions" :key="position.id" class="position-card">
            <div>
              <strong>{{ position.name }}</strong>
              <p>{{ position.is_and_mode ? 'AND 匹配' : 'OR 匹配' }}</p>
              <p class="position-meta">关键词：{{ (position.keywords || []).join(' / ') || '无' }}</p>
              <p class="position-meta">排除词：{{ (position.exclude_keywords || []).join(' / ') || '无' }}</p>
            </div>
            <div class="actions compact">
              <button type="button" class="ghost" @click="editPosition(position)">编辑</button>
              <button type="button" class="ghost danger" :disabled="positionLoading" @click="removePosition(position.id)">删除</button>
            </div>
          </article>
        </div>
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
            岗位模板
            <select v-model="taskForm.positionId">
              <option value="">不使用模板</option>
              <option v-for="position in positions" :key="position.id" :value="position.id">
                {{ position.name }}
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
          <button type="button" :disabled="taskLoading || selectedPlatformAccounts.length === 0" @click="createTaskDraft">
            {{ taskLoading ? '创建中...' : '创建任务' }}
          </button>
        </div>
      </section>

      <section v-if="user" class="panel">
        <div class="section-header">
          <h2>任务列表</h2>
          <button type="button" class="ghost" @click="loadTasks">刷新任务</button>
        </div>
        <p v-if="tasks.length === 0" class="hint">暂无任务</p>
        <div v-else class="task-list">
          <article v-for="task in tasks" :key="task.id" class="task-item">
            <div>
              <strong>{{ task.platform_account_name }}</strong>
              <p>{{ task.platform_id }} / {{ task.mode }} / 上限 {{ task.match_limit }}</p>
              <p v-if="task.position_name">岗位模板：{{ task.position_name }}</p>
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
            <div class="actions compact">
              <button type="button" class="ghost" @click="toggleTaskLogs(task.id)">
                {{ expandedTaskId === task.id ? '收起日志' : '展开日志' }}
              </button>
              <button type="button" class="ghost" @click="toggleTaskCandidates(task)">
                {{ candidateExpandedTaskId === localTaskID(task) ? '收起候选人' : '查看候选人' }}
              </button>
            </div>
            <div v-if="expandedTaskId === task.id" class="log-panel">
              <p v-if="!taskLogs[task.id] || taskLogs[task.id].length === 0" class="hint">暂无日志</p>
              <ol v-else>
                <li v-for="log in taskLogs[task.id]" :key="log.id">
                  <span>{{ log.level }}</span>
                  <strong>{{ log.message }}</strong>
                </li>
              </ol>
            </div>
            <div v-if="candidateExpandedTaskId === localTaskID(task)" class="candidate-panel">
              <div class="section-header subtle">
                <h3>候选人</h3>
                <button type="button" class="ghost" :disabled="candidateLoadingTaskId === localTaskID(task)" @click="loadTaskCandidates(task)">
                  {{ candidateLoadingTaskId === localTaskID(task) ? '读取中...' : '刷新候选人' }}
                </button>
              </div>
              <p v-if="candidateError" class="error">{{ candidateError }}</p>
              <div v-if="taskPositionSnapshot(task).name" class="snapshot-panel">
                <strong>{{ taskPositionSnapshot(task).name }}</strong>
                <p>{{ taskPositionSnapshot(task).is_and_mode ? 'AND 匹配' : 'OR 匹配' }}</p>
                <p class="snapshot-meta">关键词：{{ (taskPositionSnapshot(task).keywords || []).join(' / ') || '无' }}</p>
                <p class="snapshot-meta">排除词：{{ (taskPositionSnapshot(task).exclude_keywords || []).join(' / ') || '无' }}</p>
                <p v-if="taskPositionSnapshot(task).greet_message" class="snapshot-meta">问候语：{{ taskPositionSnapshot(task).greet_message }}</p>
              </div>
              <p v-if="taskCandidateItems(task).length === 0" class="hint">
                暂无候选人数据
              </p>
              <div v-else class="candidate-list">
                <article v-for="candidate in taskCandidateItems(task)" :key="candidate.id" class="candidate-card">
                  <div>
                    <strong>{{ candidateTitle(candidate) }}</strong>
                    <p>{{ candidateSubtitle(candidate) }}</p>
                    <p v-if="candidateDetail(candidate)" class="candidate-detail">{{ candidateDetail(candidate) }}</p>
                  </div>
                  <button type="button" class="ghost danger" @click="removeCandidate(task, candidate)">删除</button>
                </article>
              </div>
            </div>
          </article>
        </div>
      </section>
    </main>
  `
}

createApp(App).mount('#app')
