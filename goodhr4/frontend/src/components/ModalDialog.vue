<template>
  <Teleport to="body">
    <div v-if="topModal" class="modal-overlay" @click.self="onOverlayClick">
      <div class="modal-box">
        <div class="modal-header">
          <h3>{{ topModal.title }}</h3>
        </div>
        <div class="modal-body">
          <p v-for="(line, i) in topModal.lines" :key="i">{{ line }}</p>
        </div>
        <div class="modal-footer">
          <button
            v-if="topModal.showCancel"
            class="btn btn-secondary"
            type="button"
            @click="onCancel"
          >
            取消
          </button>
          <button class="btn btn-primary" type="button" @click="onConfirm">
            确认
          </button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { usePanelStore } from "../composables/usePanelStore";

const { modals, dismissModal, confirmModal } = usePanelStore();

const topModal = computed(() => {
  return modals.length > 0 ? modals[modals.length - 1] : null;
});

function onConfirm() {
  if (modals.length === 0) return;
  confirmModal(modals.length - 1);
}

function onCancel() {
  if (modals.length === 0) return;
  dismissModal(modals.length - 1);
}

function onOverlayClick() {
  if (!topModal.value || topModal.value.forceUpdate) return;
  onCancel();
}
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.45);
  backdrop-filter: blur(4px);
}

.modal-box {
  background: #fff;
  border-radius: 12px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.18);
  width: 90%;
  max-width: 380px;
  overflow: hidden;
}

.modal-header {
  padding: 16px 20px 8px;
}

.modal-header h3 {
  margin: 0;
  font-size: 16px;
  font-weight: 700;
  color: #1e293b;
}

.modal-body {
  padding: 8px 20px 16px;
  max-height: 260px;
  overflow-y: auto;
}

.modal-body p {
  margin: 4px 0;
  font-size: 13px;
  line-height: 1.6;
  color: #475569;
}

.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 10px;
  padding: 12px 20px 16px;
}
</style>
