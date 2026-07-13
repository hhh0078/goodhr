<!-- 本文件负责展示邀请活动说明、邀请链接和邀请列表。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <h2>邀请</h2>
      <button class="ghost" :disabled="loading" @click="load">刷新</button>
    </div>

    <div class="invite-hero">
      <div>
        <h3>{{ config.activity_title || "邀请好友奖励会员天数" }}</h3>
        <p>{{ config.activity_description || defaultDescription }}</p>
      </div>
      <div class="reward-box">
        <strong>{{ config.register_reward_days || 0 }} 天</strong>
        <span>注册奖励</span>
      </div>
      <div class="reward-box">
        <strong>{{ config.paid_month_reward_days || 0 }} 天/月</strong>
        <span>充值奖励</span>
      </div>
    </div>

    <div class="invite-link-box">
      <label>
        我的邀请链接
        <input :value="inviteURL" readonly />
      </label>
      <button class="ghost primary" @click="copyInviteURL">复制链接</button>
    </div>
    <p v-if="message" class="success">{{ message }}</p>
    <p v-if="error" class="error">{{ error }}</p>

    <div class="records-head">
      <h3>邀请列表</h3>
      <span class="hint">共 {{ invitees.length }} 人</span>
    </div>
    <div v-if="invitees.length" class="invite-list">
      <div v-for="item in invitees" :key="item.id" class="invite-row">
        <div>
          <strong>{{ item.email }}</strong>
          <p class="hint">ID {{ item.id }}</p>
        </div>
        <span>{{ formatDate(item.created_at) }}</span>
        <span :class="item.invite_registered_rewarded_at ? 'success' : 'warn'">
          {{ item.invite_registered_rewarded_at ? "已发注册奖励" : "未发奖励" }}
        </span>
      </div>
    </div>
    <p v-else class="hint">暂无邀请记录</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { getInvitationSummary } from "../services/api/invitationApi";

const loading = ref(false);
const error = ref("");
const message = ref("");
const inviteID = ref("");
const config = ref<any>({});
const invitees = ref<any[]>([]);
const defaultDescription =
  "邀请好友注册成功后，邀请人可获得注册奖励；好友充值会员后，邀请人还可按购买月份获得额外会员天数。(临时邮箱会注册失败)";
const inviteURL = computed(() => {
  const url = new URL("https://goodhr5.58it.cn");
  if (inviteID.value) url.searchParams.set("invite", inviteID.value);
  return url.toString();
});

/**
 * 读取邀请活动和邀请列表。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    const data = await getInvitationSummary();
    inviteID.value = data.invite_id || "";
    config.value = data.config || {};
    invitees.value = data.invitees || [];
  } catch (e: any) {
    error.value = e?.message || "读取邀请信息失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 复制邀请链接到剪贴板。
 * @returns {Promise<void>} 无返回值。
 */
async function copyInviteURL() {
  error.value = "";
  message.value = "";
  try {
    await navigator.clipboard.writeText(inviteURL.value);
    message.value = "邀请链接已复制";
  } catch {
    error.value = "复制失败，请手动选择链接复制";
  }
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

onMounted(load);
</script>

<style scoped>
.invite-hero {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 130px 130px;
  gap: 12px;
  border: 1px solid var(--border);
  background: var(--bg-input);
  padding: 12px;
  margin-bottom: 12px;
}

.invite-hero p {
  color: var(--fg-dim);
  line-height: 1.7;
}

.reward-box {
  border: 1px solid var(--border);
  padding: 10px;
  text-align: center;
}

.reward-box strong {
  display: block;
  font-size: 20px;
}

.reward-box span {
  color: var(--fg-dim);
  font-size: 12px;
}

.invite-link-box {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 8px;
  align-items: end;
}

.records-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 18px 0 8px;
}

.invite-list {
  border: 1px solid var(--border);
  background: var(--bg-input);
}

.invite-row {
  display: grid;
  grid-template-columns: minmax(180px, 1fr) 180px 110px;
  gap: 10px;
  align-items: center;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}

.invite-row:last-child {
  border-bottom: 0;
}

@media (max-width: 760px) {
  .invite-hero,
  .invite-link-box,
  .invite-row {
    grid-template-columns: 1fr;
  }
}
</style>
