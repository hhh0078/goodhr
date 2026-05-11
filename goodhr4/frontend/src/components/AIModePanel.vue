<template>
  <section class="mode-panel">
    <div class="content-grid" @focusout.capture="requestAutoSave">
      <section class="card" :class="ui.running ? 'span-8' : 'span-12'">
        <div class="section-heading">
          <div>
            <h2>岗位与岗位说明</h2>
          </div>
        </div>

        <div class="position-toolbar">
          <input
            v-model="ui.positionDraft"
            class="text-input"
            placeholder="新增岗位名称,例如：销售顾问"
            @keydown.enter.prevent="addPosition"
          />
          <button class="btn btn-secondary" type="button" @click="addPosition">
            新增岗位
          </button>
        </div>

        <div class="position-list" style="margin-top: 5px; margin-bottom: 10px">
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
              >x</span
            >
          </button>
        </div>

        <div v-if="currentPosition" class="field-group">
          <div
            style="
              display: flex;
              align-items: center;
              justify-content: space-between;
            "
          >
            <label style="margin: 0">岗位说明</label>
            <button
              type="button"
              :disabled="ui.optimizing"
              @click.stop.prevent="optimizeJobDescription"
              style="
                display: inline-flex;
                align-items: center;
                gap: 4px;
                padding: 3px 10px;
                border: 1px solid var(--line-strong);
                border-radius: 8px;
                background: var(--surface);
                color: var(--text);
                font-size: 12px;
                font-weight: 500;
                cursor: pointer;
                white-space: nowrap;
                transition: opacity 0.2s;
              "
              :style="{
                opacity: ui.optimizing ? 0.6 : 1,
                cursor: ui.optimizing ? 'not-allowed' : 'pointer',
              }"
            >
              <svg
                v-if="!ui.optimizing"
                style="width: 12px; height: 12px"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              >
                <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
              </svg>
              {{ ui.optimizing ? "AI优化中..." : "AI优化" }}
            </button>
          </div>
          <textarea
            v-model="currentPosition.description"
            class="text-area ai-compact"
            placeholder="请详细描述岗位要求，比如课程顾问：&#10;1. 3年以上销售经验&#10;2. 有过学科销售经验&#10;3 . 同事没有联系过。最近活跃的。&#10;AI将根据这些信息智能判断候选人是否合适&#10;重点：&#10;1. 学历、年龄、性别等请在平台提前筛选号。AI优化会自动去除，如果你坚持 可以在AI优化完后再加上。&#10;2. 尽量不要写 候选人简历上没有的信息。比如能加班、人品好、爱微笑。这会给AI带来困惑&#10;"
          />
        </div>
      </section>

      <section v-if="ui.running" class="card span-4">
        <div class="section-heading">
          <div>
            <span class="section-tag">日志</span>
            <h2>AI版日志</h2>
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
import { usePanelStore } from "../composables/usePanelStore";

const {
  settings,
  ui,
  logs,
  currentPosition,
  addPosition,
  optimizeJobDescription,
  requestAutoSave,
} = usePanelStore();

function confirmRemovePosition(name: string) {
  if (!globalThis.confirm(`确认删除岗位"${name}"吗？`)) return;
  const { removePosition } = usePanelStore();
  removePosition(name);
}
</script>
