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

  <div class="view-tabs">
    <button
      type="button"
      :class="['view-tab', { active: ui.activeView === 'main' }]"
      @click="ui.activeView = 'main'"
    >
      主页
    </button>
    <button
      type="button"
      :class="['view-tab', { active: ui.activeView === 'logs' }]"
      @click="ui.activeView = 'logs'"
    >
      日志
      <span v-if="logs.length" class="log-badge">{{ logs.length }}</span>
    </button>
  </div>

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

      <section class="identity-strip card" @focusout.capture="requestAutoSave">
        <div class="identity-row">
          <div class="identity-inputs">
            <input
              v-model="ui.identityInput"
              class="text-input"
              placeholder="输入邮箱或手机号，点击后直接自动注册"
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

          <div>
            余额:
            <strong :style="{ color: balanceColor }">{{
              settings.aiBalanceText || "--"
            }}</strong>
            &nbsp;
            <a
              style="
                border: 1px solid #ccc;
                padding: 2px 4px;
                border-radius: 4px;
                text-decoration: none;
                color: #000;
              "
              href="https://ai.58it.cn"
              target="_blank"
              rel="noreferrer noopener"
              >ai充值(GoodAI)</a
            >

            &nbsp;&nbsp;
            <span
              style="
                cursor: pointer;
                border: 1px solid #ccc;
                padding: 2px 4px;
                border-radius: 4px;
              "
              @click="showPricingHint"
              >价格说明</span
            >
          </div>
        </div>
      </section>

      <section class="config-panel card">
        <button
          class="config-toggle"
          type="button"
          @click="ui.configExpanded = !ui.configExpanded"
        >
          <div>
            <h2 style="margin: 0px">运行参数/AI配置</h2>
          </div>
          <span class="config-arrow">{{
            ui.configExpanded ? "收起" : "展开"
          }}</span>
        </button>

        <div
          v-if="ui.configExpanded"
          class="config-body"
          @focusout.capture="requestAutoSave"
        >
          <div class="tabs small-tabs">
            <button
              class="tab-btn"
              :class="{ active: ui.configTab === 'runtime' }"
              type="button"
              @click="ui.configTab = 'runtime'"
            >
              运行配置
            </button>
            <button
              class="tab-btn"
              :class="{ active: ui.configTab === 'ai' }"
              type="button"
              @click="ui.configTab = 'ai'"
            >
              AI配置
            </button>
          </div>

          <div
            v-if="ui.configTab === 'runtime'"
            class="content-grid compact-grid"
          >
            <section class="span-7 field-stack">
              <div class="settings-grid">
                <label class="field-group">
                  <div style="display: flex; align-items: center; gap: 10px">
                    <span style="white-space: nowrap">打招呼暂停数</span>
                    <input
                      v-model.number="settings.matchLimit"
                      class="text-input"
                      type="number"
                      min="1"
                      style="width: 70px; flex-shrink: 0"
                    />
                  </div>
                  <span style="font-size: 11px; color: #9ca3af"
                    >累计打招呼达到此数量后自动暂停</span
                  >
                </label>
                <label class="field-group">
                  <div style="display: flex; align-items: center; gap: 10px">
                    <span style="white-space: nowrap">最小延迟秒数</span>
                    <input
                      v-model.number="settings.scrollDelayMin"
                      class="text-input"
                      type="number"
                      min="0"
                      style="width: 70px; flex-shrink: 0"
                    />
                  </div>
                  <span style="font-size: 11px; color: #9ca3af"
                    >每次操作的最小等待时间</span
                  >
                </label>
                <label class="field-group">
                  <div style="display: flex; align-items: center; gap: 10px">
                    <span style="white-space: nowrap">最大延迟秒数</span>
                    <input
                      v-model.number="settings.scrollDelayMax"
                      class="text-input"
                      type="number"
                      min="0"
                      style="width: 70px; flex-shrink: 0"
                    />
                  </div>
                  <span style="font-size: 11px; color: #9ca3af"
                    >每次操作的最大等待时间</span
                  >
                </label>
                <label class="field-group">
                  <div style="display: flex; align-items: center; gap: 10px">
                    <span style="white-space: nowrap">点击候选人频率</span>
                    <input
                      v-model.number="settings.clickFrequency"
                      class="text-input"
                      type="number"
                      min="0"
                      max="10"
                      style="width: 70px; flex-shrink: 0"
                    />
                  </div>
                  <span style="font-size: 11px; color: #9ca3af"
                    >每轮滚动点击多少个候选人</span
                  >
                </label>
              </div>
            </section>

            <section class="span-5 field-stack">
              <div
                class="checkbox-grid"
                style="flex-direction: column; flex-wrap: nowrap"
              >
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.isAndMode"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>全部关键词都要命中</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      开启后候选人需同时包含所有关键词才打招呼
                    </div>
                  </div>
                </label>
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.enableSound"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>启用提示音</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      匹配成功或出错时播放提示音
                    </div>
                  </div>
                </label>
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.runModeConfig.greetingEnabled"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>启用打招呼</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      匹配成功后自动发送打招呼消息
                    </div>
                  </div>
                </label>
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.runModeConfig.communicationEnabled"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>启用沟通处理</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      自动处理候选人回复的沟通消息
                    </div>
                  </div>
                </label>
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.communicationConfig.collectPhone"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>索要手机号</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      打招呼时自动询问候选人手机号
                    </div>
                  </div>
                </label>
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.communicationConfig.collectWechat"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>索要微信号</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      打招呼时自动询问候选人微信号
                    </div>
                  </div>
                </label>
                <label style="display: flex; align-items: flex-start; gap: 8px">
                  <input
                    v-model="settings.communicationConfig.collectResume"
                    type="checkbox"
                    style="margin-top: 3px"
                  />
                  <div>
                    <div>索要简历</div>
                    <div style="font-size: 11px; color: #9ca3af">
                      打招呼时自动询问候选人发送简历
                    </div>
                  </div>
                </label>
              </div>
            </section>
          </div>

          <div v-else class="content-grid compact-grid">
            <section class="span-6 field-stack">
              <div class="settings-grid">
                <label
                  class="field-group"
                  style="
                    flex-direction: row;
                    align-items: center;
                    justify-content: space-between;
                  "
                >
                  <span style="font-weight: 500">AI平台</span>
                  <a
                    href="https://www.ai.58it.cn/"
                    target="_blank"
                    style="
                      display: inline-flex;
                      align-items: center;
                      gap: 4px;
                      padding: 4px 12px;
                      border-radius: 8px;
                      background: linear-gradient(135deg, #6366f1, #8b5cf6);
                      color: #fff;
                      font-size: 12px;
                      font-weight: 600;
                      text-decoration: none;
                      white-space: nowrap;
                      transition: opacity 0.2s;
                    "
                  >
                    GoodAI
                    <svg
                      style="width: 12px; height: 12px"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2.5"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    >
                      <path
                        d="M18 13v6a2 2 0 01-2 2H5a2 2 0 01-2-2V8a2 2 0 012-2h6"
                      />
                      <polyline points="15 3 21 3 21 9" />
                      <line x1="10" y1="14" x2="21" y2="3" />
                    </svg>
                  </a>
                </label>
                <label class="field-group">
                  <span>模型选择</span>
                  <select v-model="settings.aiConfig.model" class="text-input">
                    <option value="">
                      系统默认 ({{ ui.systemConfig.default_model || "未配置" }})
                    </option>
                    <option
                      v-for="model in availableModels"
                      :key="model.model_id"
                      :value="model.model_id"
                    >
                      {{ model.model_id }} - {{ model.description }}
                    </option>
                  </select>
                </label>
              </div>
            </section>

            <section class="span-6 field-stack">
              <label class="field-group">
                <div
                  style="
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                  "
                >
                  <span>查看详情 Prompt</span>
                  <button
                    type="button"
                    style="
                      font-size: 11px;
                      padding: 1px 8px;
                      border: 1px solid #ddd;
                      border-radius: 3px;
                      background: #f5f5f5;
                      cursor: pointer;
                      color: #666;
                    "
                    @click.stop.prevent="resetClickPrompt"
                  >
                    重置
                  </button>
                </div>
                <textarea
                  v-model="settings.aiConfig.clickPrompt"
                  class="text-area prompt-area"
                  :placeholder="
                    ui.systemConfig.default_click_prompt
                      ? '留空则使用系统默认 Prompt'
                      : '请输入查看详情 Prompt'
                  "
                />
                <span style="font-size: 10px; color: #999; line-height: 1.4">
                  使用${候选人信息}和${岗位信息}作为标记符，系统会自动替换为实际内容，如果你不清楚，请不要修改。这里一般也不需要改动。
                </span>
              </label>
            </section>
          </div>
        </div>
      </section>
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
              <button
                class="btn btn-primary"
                type="button"
                @click="addPosition"
              >
                新增岗位
              </button>
            </div>

            <div class="position-list" style="margin-top: 10px">
              <button
                v-for="position in settings.positions"
                :key="position.name"
                class="position-item"
                :class="{
                  active: settings.currentPositionName === position.name,
                }"
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
                  <button
                    type="button"
                    @click="removeKeyword('include', keyword)"
                  >
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
                  <button
                    type="button"
                    @click="removeKeyword('exclude', keyword)"
                  >
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
                <span class="log-level" :class="entry.type">{{
                  entry.type
                }}</span>
                <span class="log-text">{{ entry.message }}</span>
              </div>
            </div>
          </section>
        </div>

        <div v-else class="content-grid" @focusout.capture="requestAutoSave">
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
              <button
                class="btn btn-secondary"
                type="button"
                @click="addPosition"
              >
                新增岗位
              </button>
            </div>

            <div
              class="position-list"
              style="margin-top: 5px; margin-bottom: 10px"
            >
              <button
                v-for="position in settings.positions"
                :key="position.name"
                class="position-item"
                :class="{
                  active: settings.currentPositionName === position.name,
                }"
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
                <span class="log-level" :class="entry.type">{{
                  entry.type
                }}</span>
                <span class="log-text">{{ entry.message }}</span>
              </div>
            </div>
          </section>
        </div>
      </section>

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

    <section v-if="ui.activeView === 'logs'" class="terminal-panel">
      <header class="terminal-header">
        <span class="terminal-title">● 运行日志</span>
        <button
          type="button"
          class="terminal-clear-btn"
          @click="logs.length = 0"
        >
          清空
        </button>
      </header>
      <div ref="terminalBody" class="terminal-body">
        <div
          v-for="(entry, index) in logs"
          :key="`${entry.time}-${index}`"
          class="terminal-line"
          :class="entry.type"
        >
          <span class="terminal-time">{{ entry.time }}</span>
          <span class="terminal-msg">{{ entry.message }}</span>
        </div>
        <div v-if="!logs.length" class="terminal-empty">
          暂无日志，等待操作...
        </div>
      </div>
    </section>

    <footer class="action-bar floating-action-bar">
      <div class="site-links footer-links">
        <a :href="ui.systemConfig.contact_url" target="_blank" rel="noreferrer"
          >联系我</a
        >
        <a :href="ui.systemConfig.donate_url" target="_blank" rel="noreferrer"
          >打赏我</a
        >
        <a :href="ui.systemConfig.website_url" target="_blank" rel="noreferrer"
          >前往官网</a
        >
      </div>
      <div class="header-actions footer-actions">
        <button
          v-if="!ui.running"
          class="btn btn-primary btn-large"
          type="button"
          @click="startRun"
        >
          开始运行
        </button>
        <button
          v-else
          class="btn btn-danger btn-large"
          type="button"
          @click="stopRun"
        >
          停止运行
        </button>
      </div>
    </footer>
  </main>
</template>

<script setup>
import { computed, ref, watch, nextTick } from "vue";
import { usePanelStore } from "./composables/usePanelStore.js";
import { APP_VERSION } from "./constants/appVersion.js";

const {
  settings,
  ui,
  logs,
  currentPosition,
  availableModels,
  addPosition,
  removePosition,
  addKeyword,
  removeKeyword,
  bindAccount,
  requestAutoSave,
  resetClickPrompt,
  optimizeJobDescription,
  pushLog,
  startRun,
  stopRun,
} = usePanelStore();

const terminalBody = ref(null);

watch(
  () => logs.length,
  async () => {
    await nextTick();
    if (terminalBody.value) {
      const el = terminalBody.value;
      const target = el.scrollHeight - el.clientHeight * 0.7;
      el.scrollTop = Math.max(0, target);
    }
  },
);

const ads = computed(() => {
  if (!Array.isArray(ui.systemConfig.ads)) {
    return [];
  }
  return ui.systemConfig.ads
    .filter((item) => item && item.title && item.url)
    .slice(0, 3);
});

const topAd = computed(() => ads.value[0] || null);
const balanceAd = computed(() => ads.value[1] || null);
const configAd = computed(() => ads.value[2] || null);
const balanceColor = computed(() => {
  const balance = Number(settings.aiBalance);
  if (!Number.isFinite(balance)) {
    return "#9ca3af";
  }
  if (balance < 0.1) {
    return "#ef4444";
  }
  if (balance > 3) {
    return "#22c55e";
  }
  return "#f59e0b";
});

function adStyle(ad) {
  return {
    background: ad.background_color || undefined,
    color: ad.text_color || undefined,
    borderColor: ad.border_color || ad.background_color || undefined,
  };
}

function confirmRemovePosition(name) {
  if (!globalThis.confirm(`确认删除岗位“${name}”吗？`)) {
    return;
  }
  removePosition(name);
}

function showPricingHint() {
  globalThis.alert(
    "价格跟当前使用的模型有非常大的关系。模型越好，价格就越贵，效果就越好，反之一样。\n\n不同的模型都是根据token消耗量计算价格。如果你不了解，可以直接运行。每筛选一个候选人都会显示消耗的金额。",
  );
}
</script>
