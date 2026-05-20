<template>
  <section class="panel">
    <div class="panel-header">
      <h2>平台账号</h2>
      <div style="display: flex; gap: 8px">
        <button v-if="!showForm" class="ghost" @click="showForm = true">
          + 新增</button
        ><button v-else class="ghost" @click="showForm = false">收起</button
        ><button class="ghost" @click="load">刷新</button>
      </div>
    </div>
    <p v-if="!token" class="hint">需要登录</p>
    <template v-if="showForm && token"
      ><div class="form-grid">
        <label
          >平台<select v-model="form.platformId">
            <option value="boss">Boss直聘</option>
            <option value="zhaopin">智联招聘</option>
            <option value="liepin">猎聘</option>
          </select></label
        ><label
          >名称<input v-model="form.displayName" placeholder="我的Boss"
        /></label>
      </div>
      <p v-if="msg" :class="msgType">{{ msg }}</p>
      <div class="actions">
        <button :disabled="loading || !form.displayName" @click="create">
          {{ loading ? "处理中..." : "创建" }}
        </button>
      </div></template
    >
    <p v-if="accounts.length === 0" class="hint">暂无账号</p>
    <div v-else class="card-list" style="margin-top: 8px">
      <article v-for="a in accounts" :key="a.id" class="card">
        <div>
          <strong>{{ a.display_name || a.id }}</strong>
          <p class="card-meta">
            {{ a.platform_id }} | cookie:{{ a.cookie_status || "无" }}
          </p>
        </div>
        <button class="ghost danger" @click="del(a)">删除</button>
      </article>
    </div>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  createCookie,
  createPlatformAccount,
  deletePlatformAccount,
  listCookies,
  listPlatformAccounts,
} from "../services/cloudApi";

const props = defineProps<{ token: string; agentBaseUrl: string }>();
const accounts = ref<any[]>([]);
const loading = ref(false);
const msg = ref("");
const msgType = ref("error");
const form = ref({ platformId: "boss", displayName: "" });
const showForm = ref(false);

async function load() {
  try {
    const list: any[] = await listPlatformAccounts();
    let cookies: any[] = [];
    try {
      cookies = await listCookies();
    } catch {}
    for (const a of list) {
      const m = cookies.find((x: any) => x.platform_id === a.platform_id);
      if (m) a.cookie_status = m.status;
    }
    accounts.value = list;
  } catch {}
}
async function create() {
  loading.value = true;
  msg.value = "";
  try {
    await createPlatformAccount({
      platform_id: form.value.platformId,
      display_name: form.value.displayName,
      local_profile_id: form.value.displayName,
    });
    if (props.agentBaseUrl)
      await createCookie(
        {
          platform_id: form.value.platformId,
          display_name: form.value.displayName,
        },
        props.agentBaseUrl,
      );
    form.value.displayName = "";
    msg.value = "创建成功";
    msgType.value = "success";
    await load();
  } catch (e: any) {
    msg.value = e.message;
    msgType.value = "error";
  } finally {
    loading.value = false;
  }
}
async function del(a: any) {
  try {
    await deletePlatformAccount(a.id);
  } catch {}
  await load();
}
onMounted(load);
</script>
