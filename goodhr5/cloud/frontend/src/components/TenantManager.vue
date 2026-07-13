<template>
  <section class="panel">
    <div class="panel-header">
      <h2>团队管理</h2>
      <div style="display: flex; gap: 8px">
        <button v-if="!showInvite" class="ghost" @click="showInvite = true">
          + 邀请</button
        ><button v-else class="ghost" @click="showInvite = false">收起</button
        ><button class="ghost" @click="load">刷新</button>
      </div>
    </div>
    <div v-if="showInvite" class="form-grid">
      <label
        >邮箱<input
          v-model="inviteEmail"
          placeholder="member@example.com" /></label
      ><label
        >角色<select v-model="inviteRole">
          <option value="user">普通用户</option>
          <option value="admin">管理员</option>
        </select></label
      >
    </div>
    <p v-if="showInvite && msg" :class="msgType">{{ msg }}</p>
    <div v-if="showInvite" class="actions">
      <button :disabled="loading || !inviteEmail" @click="invite">邀请</button>
    </div>
    <p v-if="members.length === 0" class="hint" style="margin-top: 12px">
      暂无成员
    </p>
    <div v-else class="card-list" style="margin-top: 12px">
      <article v-for="m in members" :key="m.Email" class="card">
        <div>
          <strong>{{ m.Email }}</strong>
          <p class="card-meta">
            {{ m.role === "admin" ? "管理员" : "普通用户" }}|{{
              m.status === "pending" ? "待激活" : "已激活"
            }}
          </p>
        </div>
        <div class="card-actions">
          <button
            class="ghost"
            @click="toggleRole(m)"
            :disabled="m.Email === userEmail"
          >
            {{ m.role === "admin" ? "设为普通" : "设为管理" }}</button
          ><button
            class="ghost danger"
            @click="remove(m.Email)"
            :disabled="m.Email === userEmail"
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
import {
  deleteTenantMember,
  inviteTenantMember,
  listTenantMembers,
  updateTenantMember,
} from "../services/api/tenantApi";

const props = defineProps<{ token: string; userEmail: string }>();
const members = ref<any[]>([]);
const loading = ref(false);
const msg = ref("");
const msgType = ref("error");
const inviteEmail = ref("");
const inviteRole = ref("user");
const showInvite = ref(false);

async function load() {
  try {
    members.value = await listTenantMembers();
  } catch {}
}
async function invite() {
  loading.value = true;
  msg.value = "";
  try {
    await inviteTenantMember({
      email: inviteEmail.value,
      role: inviteRole.value,
    });
    inviteEmail.value = "";
    msg.value = "邀请成功";
    msgType.value = "success";
    await load();
  } catch (e: any) {
    msg.value = e.message;
    msgType.value = "error";
  } finally {
    loading.value = false;
  }
}
async function toggleRole(m: any) {
  const r = m.role === "admin" ? "user" : "admin";
  await updateTenantMember(m.Email, { role: r });
  await load();
}
async function remove(email: string) {
  await deleteTenantMember(email);
  await load();
}
onMounted(load);
</script>
