<template>
  <section class="panel">
    <div class="panel-header">
      <h2>岗位模板</h2>
      <div style="display:flex;gap:8px">
        <button v-if="!showForm" class="ghost" @click="showForm=true">+ 新建模板</button>
        <button v-else class="ghost" @click="showForm=false">收起</button>
        <button class="ghost" @click="positions.load">刷新</button>
      </div>
    </div>

    <template v-if="showForm">
      <h3>基础信息</h3>
      <div class="form-grid">
        <label>岗位名称<input v-model="positions.form.value.name" placeholder="如: Java高级开发"/></label>
        <label>默认模式
          <select v-model="positions.form.value.modeDefault">
            <option value="ai">AI筛选</option>
            <option value="keyword">关键词筛选</option>
          </select>
        </label>
        <label>问候语<textarea v-model="positions.form.value.greetMessage" rows="2"/></label>
        <label>描述<textarea v-model="positions.form.value.description" rows="2"/></label>
      </div>

      <h3>公共参数</h3>
      <div class="form-grid">
        <label>提示音<select v-model="positions.form.value.enableSound"><option :value="true">开启</option><option :value="false">关闭</option></select></label>
        <label>点击频率(%)<input v-model="positions.form.value.clickFrequency" type="number" min="0" max="100"/></label>
        <label>滚动延迟最小(秒)<input v-model="positions.form.value.scrollDelayMin" type="number" min="0"/></label>
        <label>滚动延迟最大(秒)<input v-model="positions.form.value.scrollDelayMax" type="number" min="0"/></label>
        <label>列表查看最小(秒)<input v-model="positions.form.value.listViewDelayMin" type="number" min="0" step="0.1"/></label>
        <label>列表查看最大(秒)<input v-model="positions.form.value.listViewDelayMax" type="number" min="0" step="0.1"/></label>
        <label>详情查看最小(秒)<input v-model="positions.form.value.detailViewDelayMin" type="number" min="0" step="0.1"/></label>
        <label>详情查看最大(秒)<input v-model="positions.form.value.detailViewDelayMax" type="number" min="0" step="0.1"/></label>
        <label>打招呼延迟最小(秒)<input v-model="positions.form.value.greetDelayMin" type="number" min="0" step="0.1"/></label>
        <label>打招呼延迟最大(秒)<input v-model="positions.form.value.greetDelayMax" type="number" min="0" step="0.1"/></label>
        <label>处理后休息阈值最小(人)<input v-model="positions.form.value.restAfterCandidatesMin" type="number" min="0"/></label>
        <label>处理后休息阈值最大(人)<input v-model="positions.form.value.restAfterCandidatesMax" type="number" min="0"/></label>
        <label>单次任务休息次数最小<input v-model="positions.form.value.restTimesMin" type="number" min="0"/></label>
        <label>单次任务休息次数最大<input v-model="positions.form.value.restTimesMax" type="number" min="0"/></label>
        <label>每次休息时长最小(分钟)<input v-model="positions.form.value.restDurationMin" type="number" min="0" step="0.1"/></label>
        <label>每次休息时长最大(分钟)<input v-model="positions.form.value.restDurationMax" type="number" min="0" step="0.1"/></label>
      </div>

      <h3>AI 模式专属</h3>
      <div class="form-grid">
        <label>模型<input v-model="positions.form.value.aiModel" placeholder="如: gpt-4.1-mini"/></label>
        <label>岗位要求<textarea v-model="positions.form.value.aiPositionRequirement" rows="2"/></label>
        <label>AI提示词<textarea v-model="positions.form.value.aiClickPrompt" rows="2"/></label>
      </div>

      <h3>关键词模式专属</h3>
      <div class="form-grid">
        <label>AND/OR<select v-model="positions.form.value.isAndMode"><option :value="false">OR</option><option :value="true">AND</option></select></label>
        <label>关键词<input v-model="positions.form.value.keywords" placeholder="Java Spring"/></label>
        <label>排除词<input v-model="positions.form.value.excludeKeywords" placeholder="实习 应届"/></label>
        <label>关键词模式详情打开概率(%)<input v-model="positions.form.value.keywordDetailOpenProbability" type="number" min="0" max="100"/></label>
        <label>详情模式<select v-model="positions.form.value.keywordDetailMode"><option value="dom">DOM</option><option value="ocr">OCR</option></select></label>
      </div>

      <p v-if="positions.error.value" class="error">{{positions.error.value}}</p>
      <div class="actions">
        <button :disabled="positions.loading.value||!positions.form.value.name" @click="positions.save">{{positions.loading.value?'保存中...':(positions.form.value.id?'更新':'保存')}}</button>
        <button class="ghost" :disabled="positions.loading.value" @click="positions.resetForm">清空</button>
      </div>
    </template>

    <p v-if="positions.positions.value.length===0" class="hint">暂无岗位模板</p>
    <div v-else class="card-list" style="margin-top:12px">
      <article v-for="pos in positions.positions.value" :key="pos.id" class="card">
        <div>
          <strong>{{pos.name}}</strong>
          <p class="card-meta">默认模式: {{pos.common_config?.mode_default === 'keyword' ? '关键词' : 'AI'}} | 关键词:{{(pos.keywords||[]).join(' / ')||'无'}} | 排除:{{(pos.exclude_keywords||[]).join(' / ')||'无'}}</p>
        </div>
        <div class="card-actions">
          <button class="ghost" @click="edit(pos)">编辑</button>
          <button class="ghost danger" :disabled="positions.loading.value" @click="positions.remove(pos.id)">删除</button>
        </div>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import {ref} from 'vue'
const props=defineProps({positions:Object})
const showForm=ref(false)
function edit(pos:any){showForm.value=true;props.positions.edit(pos)}
</script>
