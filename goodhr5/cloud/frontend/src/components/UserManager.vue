<!-- 本文件负责超级管理员查看用户，并给用户增加或减少会员天数。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <h2>用户管理</h2>
      <button class="ghost" :disabled="loading" @click="load">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <div class="adjust-box">
      <div class="form-grid">
        <label>
          用户邮箱
          <input v-model="form.email" placeholder="选择用户后自动填入" />
        </label>
        <label>
          调整天数
          <input v-model="form.days" type="number" placeholder="例如 7 或 -7" />
        </label>
        <label class="reason-field">
          调整原因
          <input
            v-model="form.reason"
            placeholder="例如：售后补偿 / 手动扣减"
          />
        </label>
      </div>
      <div class="actions">
        <button :disabled="adjusting" @click="adjustDays">
          {{ adjusting ? "调整中..." : "确认调整" }}
        </button>
        <button class="ghost" :disabled="adjusting" @click="resetForm">
          清空
        </button>
      </div>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="message" class="success">{{ message }}</p>
    <p v-if="loading" class="hint">正在读取用户列表...</p>

    <div v-if="users.length" class="user-table">
      <div class="user-row head">
        <span>用户</span>
        <span>角色</span>
        <span>会员</span>
        <span>状态</span>
        <span>本地程序</span>
        <span>邀请人</span>
        <span>注册时间</span>
        <span>操作</span>
      </div>
      <div v-for="item in users" :key="item.email" class="user-row">
        <span class="mono">{{ item.email }}</span>
        <span>{{ roleText(item.role) }}</span>
        <span>
          {{ item.subscription?.member_type || "--" }}
          <small>{{ formatDate(item.subscription?.expires_at) }}</small>
        </span>
        <span :class="item.subscription?.active ? 'success' : 'warn'">
          {{ item.subscription?.active ? "有效" : "已过期" }}
        </span>
        <span>
          {{
            item.agent?.machine_id ? shortMachine(item.agent.machine_id) : "--"
          }}
          <small>{{ item.agent?.agent_version || "" }}</small>
        </span>
        <span class="hint">{{ item.inviter_email || "--" }}</span>
        <span>{{ formatDate(item.created_at) }}</span>
        <span class="row-actions">
          <button class="ghost" @click="selectUser(item, 7)">加天数</button>
          <button class="ghost" @click="selectUser(item, -7)">减天数</button>
          <button class="ghost danger" @click="unbindAgent(item)">
            解绑本地程序
          </button>
        </span>
      </div>
    </div>
    <p v-else-if="!loading" class="hint">暂无用户</p>
  </section>
</template>

<script setup lang="ts">
import { onMounted, ref } from "vue";
import {
  adjustAdminUserSubscription,
  listAdminUsers,
  unbindAdminUserAgent,
} from "../services/api/adminApi";
import { alertError, confirmDialog, notifySuccess } from "../services/notify";

const users = ref<any[]>([]);
const loading = ref(false);
const adjusting = ref(false);
const unbinding = ref(false);
const error = ref("");
const message = ref("");
const form = ref({ email: "", days: 7, reason: "" });

/**
 * 读取超级管理员可见的用户列表。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    users.value = await listAdminUsers();
  } catch (e: any) {
    error.value = e?.message || "读取用户列表失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 选中用户并预填调整天数。
 * @param {any} user - 用户列表项。
 * @param {number} days - 默认调整天数。
 * @returns {void} 无返回值。
 */
function selectUser(user: any, days: number) {
  form.value.email = user.email || "";
  form.value.days = days;
  form.value.reason =
    days > 0 ? "超级管理员增加会员天数" : "超级管理员减少会员天数";
}

/**
 * 提交会员天数调整。
 * @returns {Promise<void>} 无返回值。
 */
async function adjustDays() {
  error.value = "";
  message.value = "";
  const days = Number(form.value.days || 0);
  if (!form.value.email.trim()) {
    error.value = "请先选择或输入用户邮箱";
    return;
  }
  if (!days) {
    error.value = "调整天数不能为 0";
    return;
  }
  adjusting.value = true;
  try {
    const data = await adjustAdminUserSubscription({
      email: form.value.email,
      days,
      reason: form.value.reason,
    });
    message.value = `已调整 ${form.value.email}：${days > 0 ? "+" : ""}${days} 天，到期 ${formatDate(data.subscription?.expires_at)}`;
    await load();
  } catch (e: any) {
    error.value = e?.message || "调整会员天数失败";
  } finally {
    adjusting.value = false;
  }
}

/**
 * 解除用户当前本地程序机器绑定。
 * @param {any} user - 用户列表项。
 * @returns {Promise<void>} 无返回值。
 */
async function unbindAgent(user: any) {
  error.value = "";
  message.value = "";
  const email = String(user?.email || "").trim();
  if (!email) {
    error.value = "用户邮箱为空，无法解除绑定";
    return;
  }
  if (!(await confirmDialog(`确定解除 ${email} 的本地程序绑定吗？`, {
    title: "解除绑定",
    confirmText: "解除",
  }))) return;
  unbinding.value = true;
  try {
    await unbindAdminUserAgent(email);
    message.value = `已解除 ${email} 的本地程序绑定`;
    notifySuccess(message.value);
    await load();
  } catch (e: any) {
    error.value = e?.message || "解除本地程序绑定失败";
    await alertError(error.value);
  } finally {
    unbinding.value = false;
  }
}

/**
 * 清空调整表单。
 * @returns {void} 无返回值。
 */
function resetForm() {
  form.value = { email: "", days: 7, reason: "" };
}

/**
 * 转换用户角色为中文。
 * @param {string} role - 后端角色值。
 * @returns {string} 中文角色。
 */
function roleText(role: string) {
  if (role === "super_admin") return "超管";
  if (role === "admin") return "管理员";
  return "成员";
}

/**
 * 格式化日期时间。
 * @param {string} value - ISO日期字符串。
 * @returns {string} 展示文案。
 */
function formatDate(value: string) {
  if (!value) return "--";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "--";
  return date.toLocaleString();
}

/**
 * 缩短机器码展示，避免用户列表过宽。
 * @param {string} value - 完整机器码。
 * @returns {string} 缩短后的机器码。
 */
function shortMachine(value: string) {
  if (!value) return "--";
  return value.length > 18 ? `${value.slice(0, 18)}...` : value;
}

onMounted(load);
</script>

<style scoped>
.adjust-box {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
  margin-bottom: 14px;
}

.reason-field {
  grid-column: 1 / -1;
}

.actions {
  display: flex;
  gap: 8px;
  margin-top: 10px;
}

.user-table {
  border: 1px solid var(--border);
  background: var(--bg-input);
  overflow-x: auto;
}

.user-row {
  display: grid;
  grid-template-columns: 230px 80px 180px 80px 190px 180px 180px 260px;
  gap: 10px;
  align-items: center;
  min-width: 1400px;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}

.user-row:last-child {
  border-bottom: 0;
}

.user-row.head {
  color: var(--fg-dim);
  background: var(--bg-panel);
}

.mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
}

.user-row small {
  display: block;
  color: var(--fg-dim);
  margin-top: 4px;
}

.row-actions {
  display: flex;
  gap: 6px;
}

.row-actions button {
  padding: 6px 8px;
}

@media (max-width: 800px) {
  .actions,
  .row-actions {
    flex-direction: column;
  }
}
</style>
