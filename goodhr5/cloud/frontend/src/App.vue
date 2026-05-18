<template>
  <div v-if="user" class="app-header">
    <h1>GoodHR 5</h1>
    <span>{{ user.email }}</span>
    <button class="ghost" @click="auth.logout">登出</button>
  </div>
  <LoginForm v-if="!user" :auth="auth" />
  <template v-else>
    <AgentPanel :agent="agent" :user="user" :token="auth.token" />
    <PositionManager :positions="positions" />
    <TaskCreator :tasks="tasks" :positions="positions.positions" />
    <TaskList :tasks="tasks" />
  </template>
</template>

<script setup>
import { onMounted, toRefs } from 'vue'
import { useAuth } from './composables/useAuth.js'
import { useAgent } from './composables/useAgent.js'
import { usePositions } from './composables/usePositions.js'
import { useTasks } from './composables/useTasks.js'
import LoginForm from './components/LoginForm.vue'
import AgentPanel from './components/AgentPanel.vue'
import PositionManager from './components/PositionManager.vue'
import TaskCreator from './components/TaskCreator.vue'
import TaskList from './components/TaskList.vue'

const auth = useAuth()
const agent = useAgent()
const positions = usePositions(auth.token)
const tasks = useTasks(auth.token, agent.baseUrl)
const { user } = toRefs(auth)

onMounted(async () => {
  await auth.loadCurrentUser()
  if (auth.user.value) {
    await agent.detect(auth.user.value, auth.token.value)
    await positions.load()
    await tasks.load()
  }
})
</script>
