<template>
  <section class="mode-panel">
    <a
      v-if="balanceAd"
      class="ad-card inline-ad"
      :href="balanceAd.url"
      :style="adStyle(balanceAd)"
      target="_blank"
      rel="noreferrer"
    >
      <strong>{{ balanceAd.title }}</strong>
      <span>{{ balanceAd.subtitle }}</span>
    </a>
    <div class="tabs">
      <button
        class="tab-btn"
        :class="{ active: settings.runMode === 'free' }"
        type="button"
        @click="settings.runMode = 'free'"
      >
        免费版
      </button>
      <button
        class="tab-btn"
        :class="{ active: settings.runMode === 'ai' }"
        type="button"
        @click="settings.runMode = 'ai'"
      >
        AI版
      </button>
    </div>

    <div
      v-if="settings.runMode === 'free'"
      class="content-grid"
      @focusout.capture="requestAutoSave"
    >
      <section class="card" :class="ui.running ? 'span-8' : 'span-12'">
        <div class="section-heading">
          <div style="color: black; margin-bottom: 10px">岗位与关键词</div>
        </div>

        <div class="position-toolbar">
          <input
            v-model="ui.positionDraft"
            class="text-input"
            placeholder="新增岗位名称 例如：销售顾问"
            @keydown.enter.prevent="addPosition"
          />
          <button class="btn btn-primary" type="button" @click="addPosition">
            新增岗位
          </button>
        </div>

        <div class="position-list" style="margin-top: 10px">
          <button
            v-for="position in settings.positions"
            :key="position.name"
            class="position-item"
            :class="{ active: settings.currentPositionName === position.name }"
            type="button"
            @click="settings.currentPositionName = position.name"
          >
            <span>{{ position.name }}</span>
            <span
              class="position-remove"
              @click.stop="confirmRemovePosition(position.name)"
              >×</span
            >
          </button>
        </div>

        <div v-if="currentPosition" class="keyword-manager">
          <div class="keyword-row">
            <input
              v-model="ui.includeDraft"
              class="text-input"
              placeholder="输入后回车或点击添加"
              @keydown.enter.prevent="addKeyword('include')"
            />
            <button
              class="btn btn-primary"
              type="button"
              @click="addKeyword('include')"
            >
              添加
            </button>
          </div>
          <span class="section-note"
            >当候选人的所有信息里出现这个关键词就会打招呼</span
          >

          <div class="chip-list compact-chips">
            <span
              v-for="keyword in currentPosition.keywords"
              :key="keyword"
              class="chip include"
            >
              {{ keyword }}
              <button type="button" @click="removeKeyword('include', keyword)">
                ×
              </button>
            </span>
          </div>

          <div class="keyword-row">
            <input
              v-model="ui.excludeDraft"
              class="text-input"
              placeholder="输入后回车或点击排除"
              @keydown.enter.prevent="addKeyword('exclude')"
            />
            <button
              class="btn btn-danger"
              type="button"
              @click="addKeyword('exclude')"
            >
              排除
            </button>
          </div>
          <span class="section-note"
            >当候选人的所有信息里出现这个关键词就会排除</span
          >
          <div class="chip-list compact-chips">
            <span
              v-for="keyword in currentPosition.excludeKeywords"
              :key="keyword"
              class="chip exclude"
            >
              {{ keyword }}
              <button type="button" @click="removeKeyword('exclude', keyword)">
                ×
              </button>
            </span>
          </div>
        </div>
      </section>

      <section v-if="ui.running" class="card span-4">
        <div class="section-heading">
          <div>
            <span class="section-tag">日志</span>
            <h2>免费版日志</h2>
          </div>
        </div>
        <div class="log-list short">
          <div
            v-for="(entry, index) in logs.slice().reverse()"
            :key="`${entry.time}-${index}`"
            class="log-item compact"
          >
            <span class="log-time">{{ entry.time }}</span>
            <span class="log-level" :class="entry.type">{{ entry.type }}</span>
            <span class="log-text">{{ entry.message }}</span>
          </div>
        </div>
      </section>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { usePanelStore } from "../composables/usePanelStore";
import type { AdItem } from "../constants/defaults";

const {
  settings,
  ui,
  logs,
  currentPosition,
  addPosition,
  addKeyword,
  removeKeyword,
  requestAutoSave,
} = usePanelStore();

const ads = computed<AdItem[]>(() => {
  if (!Array.isArray(ui.systemConfig.ads)) return [];
  return ui.systemConfig.ads
    .filter((item: AdItem) => item && item.title && item.url)
    .slice(0, 3);
});
const balanceAd = computed(() => ads.value[1] || null);

function adStyle(ad: AdItem) {
  return {
    background: ad.background_color || undefined,
    color: ad.text_color || undefined,
    borderColor: ad.border_color || ad.background_color || undefined,
  };
}

function confirmRemovePosition(name: string) {
  if (!globalThis.confirm(`确认删除岗位"${name}"吗？`)) return;
  const { removePosition } = usePanelStore();
  removePosition(name);
}
</script>
