<template>
  <section class="panel">
    <div class="panel-header"><h2>Cookie 管理</h2><button class="ghost" @click="load">刷新</button></div>
    <p v-if="!token" class="hint">需要登录</p>
    <template v-else>
      <div class="form-grid">
        <label>平台<select v-model="form.platformId"><option value="boss">Boss直聘</option><option value="zhaopin">智联招聘</option><option value="liepin">猎聘</option></select></label>
        <label>名称<input v-model="form.displayName" placeholder="我的Boss账号cookie" /></label>
      </div>
      <p v-if="msg" :class="msgType">{{ msg }}</p>
      <div class="actions">
        <button :disabled="loading || !form.displayName" @click="createCookie">新增 Cookie</button>
      </div>
      <p v-if="cookies.length === 0" class="hint" style="margin-top:12px">暂无 cookie，新增后将自动导航到登录页获取。</p>
      <div v-else class="card-list" style="margin-top:12px">
        <article v-for="c in cookies" :key="c.id" class="card">
          <div><strong>{{ c.display_name || c.id }}</strong><p class="card-meta">{{ c.platform_id }} | {{ c.cookie_type }} | {{ c.status }}</p></div>
          <div class="card-actions"><button class="ghost danger" @click="deleteCookie(c.id)">删除</button></div>
        </article>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
const props = defineProps<{ token: string }>()
const cookies = ref<any[]>([]); const loading = ref(false); const msg = ref(''); const msgType = ref('error')
const form = ref({ platformId: 'boss', displayName: '' })
const api = (path: string, opts?: RequestInit) => fetch(`${(window as any).GOODHR_CLOUD_API || 'http://127.0.0.1:8080'}${path}`, { headers: { Authorization: `Bearer ${props.token}`, 'Content-Type': 'application/json' }, ...opts }).then(r => r.json())

async function load() {
  try { const d = await api('/api/cookies'); cookies.value = d.cookies || [] } catch {}
}

async function createCookie() {
  loading.value = true; msg.value = ''
  try {
    const d = await api('/api/cookies/create', { method: 'POST', body: JSON.stringify({ platform_id: form.value.platformId, display_name: form.value.displayName }) })
    if (d.ok) { form.value.displayName = ''; msg.value = '创建成功'; msgType.value = 'success'; await load() }
    else { msg.value = d.error || '失败'; msgType.value = 'error' }
  } catch (e: any) { msg.value = e.message; msgType.value = 'error' }
  finally { loading.value = false }
}

async function deleteCookie(id: string) {
  await api(`/api/cookies/${id}`, { method: 'DELETE' }); await load()
}

onMounted(load)
</script>
