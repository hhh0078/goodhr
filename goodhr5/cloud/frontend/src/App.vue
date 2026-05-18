<template>
  <div v-if="user" class="app-header">
    <h1>GoodHR 5</h1>
    <span>{{ user.email }}</span>
    <button class="ghost" @click="auth.logout">登出</button>
  </div>
  <LoginForm v-if="!user" :auth="auth" />
  <template v-else>
    <AgentPanel :agent="agent" :user="user" :token="auth.token" />
    <AccountManager :token="auth.token.value" :agent-base-url="agent.baseUrl.value" />
    <PositionManager :positions="positions" />
    <TaskCreator :tasks="tasks" :positions="positions.positions" :token="auth.token.value" />
    <TaskList :tasks="tasks" />
  </template>
</template>

<script setup lang="ts">
import { onMounted, toRefs, watch } from 'vue'
import { useAuth } from './composables/useAuth'
import { useAgent } from './composables/useAgent'
import { usePositions } from './composables/usePositions'
import { useTasks } from './composables/useTasks'
import LoginForm from './components/LoginForm.vue'
import AgentPanel from './components/AgentPanel.vue'
import AccountManager from './components/AccountManager.vue'
import PositionManager from './components/PositionManager.vue'
import TaskCreator from './components/TaskCreator.vue'
import TaskList from './components/TaskList.vue'

const auth = useAuth()
const agent = useAgent()
const positions = usePositions(auth.token)
const tasks = useTasks(auth.token, agent.baseUrl)
const { user } = toRefs(auth)

// 登录成功后自动探测 Agent 并加载数据
watch(user, async (u) => {
  if (u) {
    await agent.detect(u, auth.token.value)
    await positions.load()
    await tasks.load()
  }
})

onMounted(async () => {
  await auth.loadCurrentUser()
  if (auth.user.value) {
    await agent.detect(auth.user.value, auth.token.value)
    await positions.load()
    await tasks.load()
  }
})
</script>
