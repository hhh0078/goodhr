<template>
  <section class="panel">
    <div class="panel-header"><h2>岗位模板</h2><div style="display:flex;gap:8px"><button v-if="!showForm" class="ghost" @click="showForm=true">+ 新建模板</button><button v-else class="ghost" @click="showForm=false">收起</button><button class="ghost" @click="positions.load">刷新</button></div></div>
    <template v-if="showForm">
      <div class="form-grid"><label>岗位名称<input v-model="positions.form.value.name" placeholder="如: Java高级开发"/></label><label>AND<select v-model="positions.form.value.isAndMode"><option :value="false">OR</option><option :value="true">AND</option></select></label><label>关键词<input v-model="positions.form.value.keywords" placeholder="Java Spring"/></label><label>排除词<input v-model="positions.form.value.excludeKeywords" placeholder="实习 应届"/></label></div>
      <label>描述<textarea v-model="positions.form.value.description" rows="2"/></label>
      <label>问候语<textarea v-model="positions.form.value.greetMessage" rows="2"/></label>
      <p v-if="positions.error.value" class="error">{{positions.error.value}}</p>
      <div class="actions"><button :disabled="positions.loading.value||!positions.form.value.name" @click="positions.save">{{positions.loading.value?'保存中...':(positions.form.value.id?'更新':'保存')}}</button><button class="ghost" :disabled="positions.loading.value" @click="positions.resetForm">清空</button></div>
    </template>
    <p v-if="positions.positions.value.length===0" class="hint">暂无岗位模板</p>
    <div v-else class="card-list" style="margin-top:12px"><article v-for="pos in positions.positions.value" :key="pos.id" class="card"><div><strong>{{pos.name}}</strong><p class="card-meta">{{pos.is_and_mode?'AND':'OR'}} | 关键词:{{(pos.keywords||[]).join(' / ')||'无'}} | 排除:{{(pos.exclude_keywords||[]).join(' / ')||'无'}}</p></div><div class="card-actions"><button class="ghost" @click="edit(pos)">编辑</button><button class="ghost danger" :disabled="positions.loading.value" @click="positions.remove(pos.id)">删除</button></div></article></div>
  </section>
</template>
<script setup lang="ts">import {ref} from 'vue';const props=defineProps({positions:Object});const showForm=ref(false);function edit(pos:any){showForm.value=true;props.positions.edit(pos)}</script>
