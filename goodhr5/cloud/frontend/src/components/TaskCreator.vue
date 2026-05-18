<template>
  <section class="panel">
    <div class="panel-header">
      <h2>创建任务</h2>
    </div>

    <div class="form-grid">
      <label>平台
        <select v-model="tasks.form.value.platformId">
          <option value="boss">Boss直聘</option>
          <option value="zhaopin">智联招聘</option>
          <option value="liepin">猎聘</option>
        </select>
      </label>
      <label>账号
        <select v-model="tasks.form.value.platformAccountId">
          <option value="">请选择账号</option>
          <option v-for="acc in platformAccounts" :key="acc.id" :value="acc.id">
            {{ acc.display_name }}
          </option>
        </select>
      </label>
      <label>岗位模板
        <select v-model="tasks.form.value.positionId">
          <option value="">不使用模板</option>
          <option v-for="pos in positions" :key="pos.id" :value="pos.id">{{ pos.name }}</option>
        </select>
      </label>
      <label>筛选模式
        <select v-model="tasks.form.value.mode">
          <option value="keyword">关键词筛选</option>
          <option value="ai">AI筛选</option>
        </select>
      </label>
      <label>匹配上限
        <input v-model="tasks.form.value.matchLimit" type="number" min="1" />
      </label>
    </div>

    <!-- 岗位模板预览 -->
    <div v-if="selectedPosition" class="snapshot">
      <strong>{{ selectedPosition.name }}</strong>
      <p class="snapshot-meta">{{ selectedPosition.is_and_mode ? 'AND 匹配' : 'OR 匹配' }}</p>
      <p class="snapshot-meta">关键词：{{ (selectedPosition.keywords || []).join(' / ') || '无' }}</p>
      <p class="snapshot-meta">排除词：{{ (selectedPosition.exclude_keywords || []).join(' / ') || '无' }}</p>
    </div>

    <p v-if="tasks.error.value" class="error">{{ tasks.error.value }}</p>

    <div class="actions">
      <button :disabled="tasks.loading.value || !tasks.form.value.platformAccountId" @click="tasks.create">
        {{ tasks.loading.value ? '创建中...' : '创建任务' }}
      </button>
    </div>
  </section>
</template>

<script setup>
import { computed } from 'vue'

const props = defineProps({ tasks: Object, positions: Object, agent: Object })

const selectedPosition = computed(() =>
  props.positions.find(p => p.id === props.tasks.form.value.positionId) || null
)

const platformAccounts = computed(() => [])
</script>
