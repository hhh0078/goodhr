<!-- 本文件负责超级管理员生成和查看会员激活码。 -->
<template>
  <section class="panel">
    <div class="panel-header">
      <h2>激活码管理</h2>
      <button class="ghost" :disabled="loading" @click="load">刷新</button>
    </div>

    <div class="form-grid">
      <label>
        天数
        <input v-model="form.days" type="number" min="1" />
      </label>
      <label>
        数量
        <input v-model="form.count" type="number" min="1" max="200" />
      </label>
      <label class="remark-field">
        备注
        <input v-model="form.remark" placeholder="例如：线下活动赠送" />
      </label>
    </div>
    <div class="actions">
      <button :disabled="loading" @click="createCodes">
        {{ loading ? "生成中..." : "生成激活码" }}
      </button>
    </div>

    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="message" class="success">{{ message }}</p>

    <div v-if="generatedText" class="generated-box">
      <div class="records-head">
        <h3>本次生成</h3>
        <button class="ghost" @click="copyGenerated">复制全部</button>
      </div>
      <textarea :value="generatedText" rows="8" readonly></textarea>
    </div>

    <div class="records-head">
      <h3>全部激活码</h3>
      <span class="hint">共 {{ codes.length }} 条</span>
    </div>
    <div v-if="codes.length" class="code-list">
      <div v-for="item in codes" :key="item.id" class="code-row">
        <strong>{{ item.code }}</strong>
        <span>{{ item.days }} 天</span>
        <span :class="item.status === 'used' ? 'warn' : 'success'">
          {{ item.status === "used" ? "已使用" : "未使用" }}
        </span>
        <span class="hint">{{ item.used_by_email || item.remark || "--" }}</span>
        <span class="hint">{{ formatDate(item.used_at || item.created_at) }}</span>
      </div>
    </div>
    <p v-else class="hint">暂无激活码</p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import {
  createAdminActivationCodes,
  listAdminActivationCodes,
} from "../services/api/adminApi";

const loading = ref(false);
const error = ref("");
const message = ref("");
const codes = ref<any[]>([]);
const generated = ref<any[]>([]);
const form = ref({ days: 30, count: 1, remark: "" });

const generatedText = computed(() =>
  generated.value.map((item) => item.code).join("\n"),
);

/**
 * 读取全部激活码。
 * @returns {Promise<void>} 无返回值。
 */
async function load() {
  loading.value = true;
  error.value = "";
  try {
    codes.value = await listAdminActivationCodes();
  } catch (e: any) {
    error.value = e?.message || "读取激活码失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 批量生成激活码。
 * @returns {Promise<void>} 无返回值。
 */
async function createCodes() {
  loading.value = true;
  error.value = "";
  message.value = "";
  try {
    generated.value = await createAdminActivationCodes({
      days: Number(form.value.days || 0),
      count: Number(form.value.count || 0),
      remark: form.value.remark,
    });
    message.value = `已生成 ${generated.value.length} 个激活码`;
    await load();
  } catch (e: any) {
    error.value = e?.message || "生成激活码失败";
  } finally {
    loading.value = false;
  }
}

/**
 * 复制本次生成的激活码。
 * @returns {Promise<void>} 无返回值。
 */
async function copyGenerated() {
  try {
    await navigator.clipboard.writeText(generatedText.value);
    message.value = "已复制本次生成的激活码";
  } catch {
    error.value = "复制失败，请手动全选复制";
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
.remark-field {
  grid-column: 1 / -1;
}

.generated-box {
  margin-top: 14px;
}

.generated-box textarea {
  width: 100%;
}

.records-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin: 18px 0 8px;
}

.code-list {
  border: 1px solid var(--border);
  background: var(--bg-input);
}

.code-row {
  display: grid;
  grid-template-columns: 190px 70px 70px minmax(120px, 1fr) 170px;
  gap: 10px;
  align-items: center;
  padding: 10px;
  border-bottom: 1px solid var(--border);
}

.code-row:last-child {
  border-bottom: 0;
}

@media (max-width: 800px) {
  .code-row {
    grid-template-columns: 1fr;
  }
}
</style>
