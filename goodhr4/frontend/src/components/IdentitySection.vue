<template>
  <section class="identity-strip card" @focusout.capture="requestAutoSave">
    <div class="identity-row">
      <div class="identity-inputs">
        <input
          v-model="ui.identityInput"
          class="text-input"
          placeholder="输入邮箱，便于保存数据云端"
          @keydown.enter.prevent="bindAccount"
        />
        <button
          class="btn btn-primary"
          type="button"
          :disabled="ui.binding"
          @click="bindAccount"
        >
          {{ ui.binding ? "绑定中..." : "绑定" }}
        </button>
      </div>
      <!-- <div style="color: #999; font-size: 12px">*填写邮箱</div> -->
    </div>
  </section>
</template>

<script setup lang="ts">
import { usePanelStore } from "../composables/usePanelStore";

const { ui, bindAccount, requestAutoSave } = usePanelStore();
</script>
