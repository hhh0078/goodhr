<template>
  <div class="terminal-login">
    <div class="terminal-window">
      <div class="terminal-bar">
        <span class="bar-btn bar-close"></span>
        <span class="bar-btn bar-min"></span>
        <span class="bar-btn bar-max"></span>
        <span class="bar-title">GoodHR</span>
      </div>
      <div class="terminal-body">
        <div class="line">
          <span class="prompt">$</span>
          <span class="cmd">Welcome to GoodHR </span>
        </div>
        <!-- <div class="line dim">Welcome to GoodHR 5 Cloud Console</div> -->
        <div class="line dim">────────────────────────────────────</div>
        <div class="line" style="margin-top: 12px">
          <span class="prompt">&gt;</span>
          <input
            ref="emailRef"
            v-model="auth.email.value"
            class="terminal-input"
            placeholder="you@example.com"
            @keydown.enter="focusCode"
          />
          <span class="field-label">邮箱</span>
        </div>
        <div class="line">
          <span class="prompt">&gt;</span>
          <input
            ref="codeRef"
            v-model="auth.code.value"
            class="terminal-input"
            placeholder="4位验证码"
            maxlength="4"
            @keydown.enter="auth.login"
          />
          <span class="field-label">验证码</span>
        </div>
        <div v-if="auth.devCode.value" class="line dim">
          [dev] {{ auth.devCode.value }}
        </div>
        <div v-if="auth.error.value" class="line error">
          {{ auth.error.value }}
        </div>
        <div class="line" style="margin-top: 16px">
          <button
            class="terminal-btn"
            :disabled="auth.loading.value || !auth.email.value"
            @click="auth.sendCode"
          >
            [ 发送验证码 ]
          </button>
          <button
            class="terminal-btn primary"
            :disabled="auth.loading.value || !auth.code.value"
            @click="auth.login"
          >
            [ 登录 ]
          </button>
        </div>
        <div v-if="auth.loading.value" class="line" style="margin-top: 8px">
          <span class="blink">▌</span> processing...
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
const props = defineProps({ auth: Object });
const emailRef = ref(null);
const codeRef = ref(null);
function focusCode() {
  codeRef.value?.focus();
}
watch(
  () => props.auth?.devCode?.value,
  (v) => {
    if (v && codeRef.value) codeRef.value.focus();
  },
);
</script>

<style scoped>
.terminal-login {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 80vh;
}
.terminal-window {
  width: 480px;
  max-width: 100%;
  border: 1px solid var(--fg);
  background: #050505;
}
.terminal-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid #333;
  background: #0d0d0d;
}
.bar-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  color: #555;
  margin-right: 40px;
}
.bar-btn {
  width: 12px;
  height: 12px;
  border-radius: 0;
  display: inline-block;
}
.bar-close {
  background: #e33;
}
.bar-min {
  background: #e83;
}
.bar-max {
  background: #3a3;
  opacity: 0.5;
}
.terminal-body {
  padding: 20px;
  min-height: 260px;
}
.line {
  font-size: 14px;
  line-height: 2;
  display: flex;
  align-items: center;
  gap: 4px;
}
.line.dim {
  color: #555;
  margin: 2px 0;
}
.line.error {
  color: #f33;
  margin-top: 8px;
}
.prompt {
  color: var(--fg);
  margin-right: 4px;
  flex-shrink: 0;
}
.cmd {
  color: #0a0;
}
.terminal-input {
  width: 180px;
  background: transparent;
  border: none;
  border-bottom: 1px dashed #333;
  color: var(--fg);
  font-family: inherit;
  font-size: 14px;
  padding: 2px 4px;
  outline: none;
}
.terminal-input:focus {
  border-bottom-color: var(--fg);
}
.terminal-input::placeholder {
  color: #333;
}
.field-label {
  color: #555;
  font-size: 12px;
  margin-left: 4px;
}
.terminal-btn {
  background: transparent;
  border: 1px solid #333;
  color: #555;
  font-family: inherit;
  font-size: 13px;
  padding: 6px 16px;
  cursor: pointer;
  margin-right: 8px;
  border-radius: 0;
}
.terminal-btn:hover:not(:disabled) {
  border-color: var(--fg);
  color: var(--fg);
}
.terminal-btn.primary {
  border-color: var(--fg);
  color: var(--fg);
}
.terminal-btn.primary:hover:not(:disabled) {
  background: var(--fg);
  color: #000;
}
.terminal-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}
.blink {
  animation: blink 1s step-end infinite;
}
@keyframes blink {
  50% {
    opacity: 0;
  }
}
</style>
