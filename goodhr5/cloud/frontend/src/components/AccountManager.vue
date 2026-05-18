<template>
  <section class="panel">
    <div class="panel-header"><h2>平台账号管理</h2><button class="ghost" @click="load">刷新</button></div>
    <p v-if="!agentBaseUrl" class="hint">需要先连接本地 Agent</p>
    <template v-else>
      <div class="form-grid">
        <label>平台<select v-model="form.platformId"><option value="boss">Boss直聘</option><option value="zhaopin">智联招聘</option><option value="liepin">猎聘</option></select></label>
        <label>显示名称<input v-model="form.displayName" placeholder="我的Boss账号" /></label>
      </div>
      <p v-if="errorMsg" class="error">{{ errorMsg }}</p>
      <div class="actions">
        <button :disabled="loading || !form.displayName" @click="createProfile">{{ loading ? '创建中...' : '创建账号' }}</button>
      </div>
      <p v-if="localProfiles.length === 0 && cloudAccounts.length === 0" class="hint" style="margin-top:12px">暂无账号，请先在招聘平台登录，然后在此创建对应的 profile。</p>
      <div v-else class="card-list" style="margin-top:12px">
        <article v-for="acc in localProfiles" :key="acc.id" class="card">
          <div><strong>{{ acc.display_name }}</strong><p class="card-meta">{{ acc.platform_id }} | {{ acc.id }}</p></div>
          <div class="card-actions">
            <button v-if="!isSynced(acc)" class="ghost" @click="syncToCloud(acc)">同步到云端</button>
            <button v-else class="ghost" disabled>已同步</button>
            <button class="ghost danger" @click="deleteProfile(acc.id)">删除</button>
          </div>
        </article>
        <article v-for="acc in cloudAccounts" :key="acc.id" class="card">
          <div><strong>{{ acc.display_name }}</strong><p class="card-meta">{{ acc.platform_id }} | {{ acc.local_profile_id }}</p></div>
          <div class="card-actions"><button class="ghost danger" @click="deleteCloud(acc.id)">删除</button></div>
        </article>
      </div>
    </template>
  </section>
</template>

<script setup>
import { onMounted, ref } from 'vue'
const props = defineProps({ token: String, agentBaseUrl: String })
const localProfiles = ref([]); const cloudAccounts = ref([])
const loading = ref(false); const errorMsg = ref('')
const form = ref({ platformId: 'boss', displayName: '' })

async function load() {
  if (!props.agentBaseUrl) return
  const { listPlatformAccounts } = await import('../services/cloudApi.js')
  const { fetch: f } = window; const h = { 'Content-Type': 'application/json' }
  try {
    const r1 = await f(`${props.agentBaseUrl}/api/v1/profiles`); const d1 = await r1.json()
    localProfiles.value = d1.profiles || []
    cloudAccounts.value = (await listPlatformAccounts(props.token)).accounts || []
  } catch (e) { errorMsg.value = e.message }
}

function isSynced(local) { return cloudAccounts.value.some(a => a.local_profile_id === local.id) }

async function createProfile() {
  loading.value = true; errorMsg.value = ''
  try {
    await fetch(`${props.agentBaseUrl}/api/v1/profiles`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ platform_id: form.value.platformId, display_name: form.value.displayName }) })
    form.value.displayName = ''; await load()
  } catch (e) { errorMsg.value = e.message } finally { loading.value = false }
}

async function syncToCloud(local) {
  loading.value = true; errorMsg.value = ''
  try {
    const { createPlatformAccount } = await import('../services/cloudApi.js')
    await createPlatformAccount(props.token, { platform_id: local.platform_id, display_name: local.display_name, local_profile_id: local.id })
    await load()
  } catch (e) { errorMsg.value = e.message } finally { loading.value = false }
}

async function deleteProfile(id) {
  loading.value = true; errorMsg.value = ''
  try { await fetch(`${props.agentBaseUrl}/api/v1/profiles/${id}`, { method: 'DELETE' }); await load() }
  catch (e) { errorMsg.value = e.message } finally { loading.value = false }
}

async function deleteCloud(id) {
  loading.value = true; errorMsg.value = ''
  try { const { deletePlatformAccount } = await import('../services/cloudApi.js'); await deletePlatformAccount(props.token, id); await load() }
  catch (e) { errorMsg.value = e.message } finally { loading.value = false }
}

onMounted(load)
</script>
