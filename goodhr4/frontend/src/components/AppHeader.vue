<template>
  <section
    class="hero card"
    style="background-color: var(--primary); color: #fff"
  >
    <div class="hero-main">
      <div>
        <h1 style="margin: 0px; margin-bottom: 10px; text-align: center">
          GoodHR {{ APP_VERSION }}
        </h1>
        <div
          v-for="value in ui.systemConfig.announcement"
          style="text-align: center"
        >
          {{ value }}
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { usePanelStore } from "../composables/usePanelStore";
import { APP_VERSION } from "../constants/appVersion";

const { ui } = usePanelStore();
</script>
