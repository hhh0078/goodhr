<!-- 本文件负责展示全局右上角轻提醒和居中确认弹框。 -->
<template>
  <div class="toast-stack" aria-live="polite">
    <article
      v-for="item in notifyState.toasts"
      :key="item.id"
      :class="['toast-item', item.kind]"
    >
      <strong>{{ item.kind === "success" ? "成功" : "错误" }}</strong>
      <span>{{ item.message }}</span>
      <button type="button" @click="removeToast(item.id)">x</button>
    </article>
  </div>

  <div v-if="notifyState.confirm.visible" class="notify-mask">
    <section :class="['notify-dialog', notifyState.confirm.kind]">
      <div class="notify-title">
        <strong>{{ notifyState.confirm.title }}</strong>
      </div>
      <p>{{ notifyState.confirm.message }}</p>
      <div class="notify-actions">
        <button
          v-if="notifyState.confirm.showCancel"
          type="button"
          class="ghost"
          @click="closeConfirm(false)"
        >
          {{ notifyState.confirm.cancelText }}
        </button>
        <button type="button" class="ghost primary" @click="closeConfirm(true)">
          {{ notifyState.confirm.confirmText }}
        </button>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { closeConfirm, notifyState, removeToast } from "../services/notify";
</script>

<style scoped>
.toast-stack {
  position: fixed;
  top: 18px;
  right: 18px;
  z-index: 3000;
  display: grid;
  gap: 10px;
  width: min(360px, calc(100vw - 36px));
}

.toast-item {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr) auto;
  gap: 10px;
  align-items: start;
  border: 1px solid var(--border);
  background: var(--bg-panel);
  color: var(--fg);
  padding: 12px;
  box-shadow: 0 12px 30px rgba(0, 0, 0, 0.24);
}

.toast-item.success {
  border-color: var(--accent);
}

.toast-item.error {
  border-color: #e15c5c;
}

.toast-item strong {
  color: var(--accent);
}

.toast-item.error strong {
  color: #ff8a8a;
}

.toast-item span {
  line-height: 1.5;
  word-break: break-word;
}

.toast-item button {
  border: 0;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
}

.notify-mask {
  position: fixed;
  inset: 0;
  z-index: 3100;
  display: grid;
  place-items: center;
  padding: 18px;
  background: rgba(0, 0, 0, 0.48);
}

.notify-dialog {
  width: min(420px, 100%);
  border: 1px solid var(--border);
  background: var(--bg-panel);
  color: var(--fg);
  padding: 18px;
  box-shadow: 0 18px 60px rgba(0, 0, 0, 0.36);
}

.notify-dialog.error {
  border-color: #e15c5c;
}

.notify-title {
  color: var(--accent);
  font-size: 16px;
  margin-bottom: 10px;
}

.notify-dialog.error .notify-title {
  color: #ff8a8a;
}

.notify-dialog p {
  margin: 0;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}

.notify-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 18px;
}

.notify-actions .primary {
  border-color: var(--accent);
  color: var(--accent);
}
</style>
