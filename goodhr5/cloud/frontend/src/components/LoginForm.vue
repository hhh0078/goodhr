<template>
  <form class="panel" @submit.prevent="auth.login">
    <h2>登录 GoodHR 5</h2>
    <div class="form-grid">
      <label>邮箱<input v-model="auth.email.value" type="email" placeholder="you@example.com" /></label>
      <label>验证码<input v-model="auth.code.value" placeholder="4位验证码" maxlength="4" /></label>
    </div>
    <p v-if="auth.devCode.value" class="hint">开发模式验证码: {{ auth.devCode.value }}</p>
    <p v-if="auth.error.value" class="error">{{ auth.error.value }}</p>
    <div class="actions">
      <button type="button" :disabled="auth.loading.value || !auth.email.value" @click="auth.sendCode">发送验证码</button>
      <button type="submit" :disabled="auth.loading.value || !auth.code.value">登录</button>
    </div>
  </form>
</template>

<script setup>
defineProps({ auth: Object })
</script>
