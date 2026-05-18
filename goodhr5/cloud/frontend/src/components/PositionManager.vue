<template>
  <section class="panel">
    <div class="panel-header">
      <h2>岗位模板</h2>
      <button class="ghost" @click="positions.load">刷新</button>
    </div>

    <!-- 创建/编辑表单 -->
    <div class="form-grid">
      <label>岗位名称<input v-model="positions.form.value.name" placeholder="如: Java高级开发" /></label>
      <label>
        AND 匹配
        <select v-model="positions.form.value.isAndMode">
          <option :value="false">OR (任一匹配)</option>
          <option :value="true">AND (全部匹配)</option>
        </select>
      </label>
      <label>关键词(空格分隔)<input v-model="positions.form.value.keywords" placeholder="Java Spring Boot" /></label>
      <label>排除词(空格分隔)<input v-model="positions.form.value.excludeKeywords" placeholder="实习 应届" /></label>
    </div>
    <label>岗位描述<textarea v-model="positions.form.value.description" rows="3" placeholder="岗位说明" /></label>
    <label>默认问候语<textarea v-model="positions.form.value.greetMessage" rows="3" placeholder="默认打招呼文案" /></label>

    <p v-if="positions.error.value" class="error">{{ positions.error.value }}</p>

    <div class="actions">
      <button :disabled="positions.loading.value || !positions.form.value.name" @click="positions.save">
        {{ positions.loading.value ? '保存中...' : (positions.form.value.id ? '更新模板' : '保存模板') }}
      </button>
      <button class="ghost" :disabled="positions.loading.value" @click="positions.resetForm">清空</button>
    </div>

    <!-- 模板列表 -->
    <p v-if="positions.positions.value.length === 0" class="hint">暂无岗位模板</p>
    <div v-else class="card-list" style="margin-top:12px">
      <article v-for="pos in positions.positions.value" :key="pos.id" class="card">
        <div>
          <strong>{{ pos.name }}</strong>
          <p class="card-meta">{{ pos.is_and_mode ? 'AND 匹配' : 'OR 匹配' }}</p>
          <p class="card-meta">关键词：{{ (pos.keywords || []).join(' / ') || '无' }}</p>
          <p class="card-meta">排除词：{{ (pos.exclude_keywords || []).join(' / ') || '无' }}</p>
        </div>
        <div class="card-actions">
          <button class="ghost" @click="positions.edit(pos)">编辑</button>
          <button class="ghost danger" :disabled="positions.loading.value" @click="positions.remove(pos.id)">删除</button>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
defineProps({ positions: Object })
</script>
