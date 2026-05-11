<template>
  <AppHeader />

  <ViewTabs />

  <main class="app-shell">
    <div
      v-if="ui.activeView === 'main'"
      style="display: flex; flex-direction: column; gap: 14px"
    >
      <a
        v-if="topAd"
        class="ad-card hero-ad"
        :href="topAd.url"
        :style="adStyle(topAd)"
        target="_blank"
        rel="noreferrer"
      >
        <strong>{{ topAd.title }}</strong>
        <span>{{ topAd.subtitle }}</span>
      </a>

      <IdentitySection />

      <ConfigPanel />

      <div class="asdfasdf">
        <div class="tabs2">
          <button
            class="tab-btn"
            :class="{ active: settings.runMode === 'free' }"
            type="button"
            style="flex: 1"
            @click="settings.runMode = 'free'"
          >
            免费版
          </button>
          <button
            class="tab-btn"
            :class="{ active: settings.runMode === 'ai' }"
            type="button"
            style="flex: 1"
            @click="settings.runMode = 'ai'"
          >
            AI收费版
          </button>
        </div>

        <div style="display: block; margin-top: 5px">
          <FreeModePanel v-if="settings.runMode === 'free'" />
          <AIModePanel v-if="settings.runMode === 'ai'" />
        </div>
      </div>

      <a
        v-if="configAd"
        class="ad-card config-ad"
        :href="configAd.url"
        :style="adStyle(configAd)"
        target="_blank"
        rel="noreferrer"
      >
        <strong>{{ configAd.title }}</strong>
        <span>{{ configAd.subtitle }}</span>
      </a>
    </div>

    <LogTerminal />

    <ActionBar />
  </main>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { usePanelStore } from "./composables/usePanelStore";
import type { AdItem } from "./constants/defaults";
import AppHeader from "./components/AppHeader.vue";
import ViewTabs from "./components/ViewTabs.vue";
import IdentitySection from "./components/IdentitySection.vue";
import ConfigPanel from "./components/ConfigPanel.vue";
import FreeModePanel from "./components/FreeModePanel.vue";
import AIModePanel from "./components/AIModePanel.vue";
import LogTerminal from "./components/LogTerminal.vue";
import ActionBar from "./components/ActionBar.vue";

const { settings, ui } = usePanelStore();

const ads = computed<AdItem[]>(() => {
  if (!Array.isArray(ui.systemConfig.ads)) return [];
  return ui.systemConfig.ads
    .filter((item: AdItem) => item && item.title && item.url)
    .slice(0, 3);
});

const topAd = computed(() => ads.value[0] || null);
const configAd = computed(() => ads.value[2] || null);

function adStyle(ad: AdItem) {
  return {
    background: ad.background_color || undefined,
    color: ad.text_color || undefined,
    borderColor: ad.border_color || ad.background_color || undefined,
  };
}
</script>
