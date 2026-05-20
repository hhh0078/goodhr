<template>
  <section class="panel">
    <div class="panel-header">
      <h2>团队管理</h2>
      <button class="ghost" @click="load">刷新</button>
    </div>
    <div class="form-grid">
      <label
        >邮箱<input v-model="inviteEmail" placeholder="member@example.com"
      /></label>
      <label
        >角色<select v-model="inviteRole">
          <option value="user">普通用户</option>
          <option value="admin">管理员</option>
        </select></label
      >
    </div>
    <p v-if="msg" :class="msgType">{{ msg }}</p>
    <div class="actions">
      <button :disabled="loading || !inviteEmail" @click="invite">
        邀请成员
      </button>
    </div>
    <p v-if="members.length === 0" class="hint" style="margin-top: 12px">
      暂无成员
    </p>
    <div v-else class="card-list" style="margin-top: 12px">
      <article v-for="m in members" :key="m.email" class="card">
        <div>
          <strong>{{ m.email }}</strong>
          <p class="card-meta">
            {{ m.role === "admin" ? "管理员" : "普通用户" }} |
            {{ m.status === "pending" ? "待激活" : "已激活" }}
          </p>
          <p v-if="m.invited_by" class="card-meta">
            邀请者: {{ m.invited_by }}
          </p>
        </div>
        <div class="card-actions">
          <button
            class="ghost"
            @click="toggleRole(m)"
            :disabled="m.email === userEmail"
          >
            {{ m.role === "admin" ? "设为普通" : "设为管理" }}
          </button>
          <button
            class="ghost danger"
            @click="remove(m.email)"
            :disabled="m.email === userEmail"
          >
            移除
          </button>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
const props = defineProps<{ token: string; userEmail: string }>();
const members = ref<any[]>([]);
const loading = ref(false);
const msg = ref("");
const msgType = ref("error");
const inviteEmail = ref("");
const inviteRole = ref("user");
const api = (path: string, opts?: RequestInit) =>
  fetch(`${window.GOODHR_CLOUD_API || "http://127.0.0.1:8084"}${path}`, {
    headers: {
      Authorization: `Bearer ${props.token}`,
      "Content-Type": "application/json",
    },
    ...opts,
  }).then((r) => r.json());

async function load() {
  try {
    const d = await api("/api/tenants/members");
    members.value = d.members || [];
  } catch {}
}

async function invite() {
  loading.value = true;
  msg.value = "";
  try {
    const d = await api("/api/tenants/invite", {
      method: "POST",
      body: JSON.stringify({
        email: inviteEmail.value,
        role: inviteRole.value,
      }),
    });
    if (d.ok) {
      inviteEmail.value = "";
      msg.value = "邀请成功";
      msgType.value = "success";
      await load();
    } else {
      msg.value = d.error || "邀请失败";
      msgType.value = "error";
    }
  } catch (e: any) {
    msg.value = e.message;
    msgType.value = "error";
  } finally {
    loading.value = false;
  }
}

async function toggleRole(m: any) {
  const newRole = m.role === "admin" ? "user" : "admin";
  await api(`/api/tenants/members/${encodeURIComponent(m.email)}`, {
    method: "PUT",
    body: JSON.stringify({ role: newRole }),
  });
  await load();
}

async function remove(email: string) {
  await api(`/api/tenants/members/${encodeURIComponent(email)}`, {
    method: "DELETE",
  });
  await load();
}

onMounted(load);
</script>
