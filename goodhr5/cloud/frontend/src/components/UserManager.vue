<!-- 本文件负责超级管理员查看用户，并给用户增加或减少会员天数。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <h2>用户管理</h2>
      <button class="ghost" :disabled="loading" @click="load">
        {{ loading ? "刷新中..." : "刷新" }}
      </button>
    </div>

    <div class="admin-user-stats">
      <div>
        <span>今日注册</span>
        <strong>{{ stats.today_registered_count || 0 }}</strong>
      </div>
      <div>
        <span>绑定程序</span>
        <strong>{{ stats.agent_binding_count || 0 }}</strong>
      </div>
    </div>

    <div class="search-row">
      <label>
        搜索用户
        <input
          v-model="searchText"
          placeholder="邮箱、角色、状态、邀请人"
          @keyup.enter="search"
        />
      </label>
      <label>
        每页数量
        <select v-model.number="pageSize" @change="changePageSize">
          <option :value="10">10</option>
          <option :value="20">20</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
        </select>
      </label>
      <button :disabled="loading" @click="search">搜索</button>
      <button class="ghost" :disabled="loading" @click="resetSearch">
        重置
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

    <div v-if="total > 0" class="pagination-row top">
      <span>共 {{ total }} 个用户，第 {{ page }} / {{ totalPages }} 页</span>
      <div>
        <button class="ghost" :disabled="loading || page <= 1" @click="goPage(page - 1)">
          上一页
        </button>
        <button
          class="ghost"
          :disabled="loading || page >= totalPages"
          @click="goPage(page + 1)"
        >
          下一页
        </button>
      </div>
    </div>

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

    <div v-if="total > 0" class="pagination-row">
      <span>共 {{ total }} 个用户，第 {{ page }} / {{ totalPages }} 页</span>
      <div>
        <button class="ghost" :disabled="loading || page <= 1" @click="goPage(page - 1)">
          上一页
        </button>
        <button
          class="ghost"
          :disabled="loading || page >= totalPages"
          @click="goPage(page + 1)"
        >
          下一页
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  adjustAdminUserSubscription,
  listAdminUsers,
  unbindAdminUserAgent,
} from "../services/api/adminApi";
import { alertError, confirmDialog, notifySuccess } from "../services/notify";

const users = ref<any[]>([]);
const stats = ref<any>({});
const page = ref(1);
const pageSize = ref(20);
const total = ref(0);
const searchText = ref("");
const loading = ref(false);
const adjusting = ref(false);
const unbinding = ref(false);
const error = ref("");
const message = ref("");
const form = ref({ email: "", days: 7, reason: "" });
const totalPages = computed(() =>
  Math.max(1, Math.ceil(total.value / pageSize.value)),
);

/**
 * 读取超级管理员可见的用户列表。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await listAdminUsers({
      page: page.value,
      page_size: pageSize.value,
      q: searchText.value,
    });
    users.value = data.users || [];
    total.value = Number(data.total || 0);
    page.value = Number(data.page || page.value);
    pageSize.value = Number(data.page_size || pageSize.value);
    stats.value = data.stats || {};
  } catch (e: any) {
    error.value = e?.message || "读取用户列表失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 按当前关键词重新搜索用户。
 * @returns {Promise<void>} 无返回值。
 */
async function search() {
  page.value = 1;
  await load();
}

/**
 * 清空搜索并重新读取第一页。
 * @returns {Promise<void>} 无返回值。
 */
async function resetSearch() {
  searchText.value = "";
  page.value = 1;
  await load();
}

/**
 * 切换用户列表页码。
 * @param {number} nextPage - 目标页码。
 * @returns {Promise<void>} 无返回值。
 */
async function goPage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages.value) return;
  page.value = nextPage;
  await load();
}

/**
 * 切换每页数量并回到第一页。
 * @returns {Promise<void>} 无返回值。
 */
async function changePageSize() {
  page.value = 1;
  await load();
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
.admin-user-stats {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  margin-bottom: 12px;
}

.admin-user-stats > div {
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
}

.admin-user-stats span {
  display: block;
  color: var(--fg-dim);
  font-size: 12px;
}

.admin-user-stats strong {
  display: block;
  margin-top: 4px;
  color: var(--fg);
  font-size: 24px;
}

.search-row {
  display: grid;
  grid-template-columns: minmax(260px, 1fr) 140px auto auto;
  gap: 10px;
  align-items: end;
  margin-bottom: 12px;
}

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

.pagination-row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 10px;
  margin-top: 12px;
  color: var(--fg-dim);
}

.pagination-row.top {
  margin: 0 0 12px;
}

.pagination-row > div {
  display: flex;
  gap: 8px;
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
  .row-actions,
  .pagination-row {
    flex-direction: column;
    align-items: stretch;
  }

  .admin-user-stats,
  .search-row {
    grid-template-columns: 1fr;
  }
}
</style>
