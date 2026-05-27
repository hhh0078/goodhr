<template>
  <main class="container">
    <header class="header">
      <h1>Good!HR 助手{{ state.version }}</h1>
      <span>免费的HR助手，帮你打招呼</span>
      <span>作者接小程序、app、后台管理系统。定制开发</span>
    </header>

    <TabSwitcher v-model="state.currentTab" />

    <section class="panel-section">
      <button
        v-if="state.currentTab === 'ai'"
        class="ai-config-btn"
        type="button"
        @click="ui.showAiConfig = true"
      >
        AI配置
      </button>

      <FreePanel
        v-if="state.currentTab === 'free'"
        :state="state"
        :ui="ui"
        @update:phone="(value) => (state.phone = value)"
        @bind-phone="bindPhone(state.phone, 'free')"
        @update:position-draft="(value) => (ui.positionDraft = value)"
        @add-position="addPosition(false)"
        @select-position="selectPosition"
        @remove-position="removePosition"
      >
        <template #rest>
          <div class="filter-group">
            <label class="checkbox-wrapper">
              <input v-model="state.isAndMode" type="checkbox" />
              <span>全部关键词都必须匹配</span>
            </label>
            <div class="keyword-input-group">
              <input
                v-model="ui.keywordDraft"
                class="keyword-input"
                placeholder="输入关键词"
                @keydown.enter.prevent="addKeyword('include')"
              />
              <button class="keyword-btn danger" type="button" @click="addKeyword('exclude')">排除</button>
              <button class="keyword-btn" type="button" @click="addKeyword('include')">添加</button>
            </div>
            <div class="keyword-list">
              <div v-for="keyword in freeKeywords" :key="keyword" class="keyword-tag">
                {{ keyword }}
                <button class="chip-remove" type="button" @click="removeKeyword(keyword, 'include')">&times;</button>
              </div>
            </div>
            <div class="keyword-list" style="margin-top: 6px">
              <div v-for="keyword in freeExcludeKeywords" :key="keyword" class="keyword-tag">
                {{ keyword }}
                <button class="chip-remove" type="button" @click="removeKeyword(keyword, 'exclude')">&times;</button>
              </div>
            </div>
            <div class="inline-options" style="margin-top: 8px">
              <label class="checkbox-wrapper"><input v-model="state.communicationConfig.collectPhone" type="checkbox" />索要手机号</label>
              <label class="checkbox-wrapper"><input v-model="state.communicationConfig.collectWechat" type="checkbox" />索要微信号</label>
              <label class="checkbox-wrapper"><input v-model="state.communicationConfig.collectResume" type="checkbox" />索要简历</label>
            </div>
          </div>

          <div class="filter-group">
            <div style="margin-bottom: 8px">
              <span class="group-title">打招呼暂停数</span>
              <label class="input-wrapper">
                <input v-model.number="state.matchLimit" type="number" class="number-input" min="1" />
                <span>* 打招呼成功多少后停止</span>
              </label>
            </div>
            <div style="margin-bottom: 8px">
              <span class="group-title">随机延迟秒数</span>
              <label class="input-wrapper">
                <input v-model.number="state.scrollDelayMin" type="number" class="number-input" min="1" max="9" />
                到
                <input v-model.number="state.scrollDelayMax" type="number" class="number-input" min="1" max="10" />
                <span>* 随机模拟停止的时间段 0-10秒，默认3-5</span>
              </label>
            </div>
            <div style="margin-bottom: 8px">
              <span class="group-title">点击候选人频率</span>
              <label class="input-wrapper">
                <input v-model.number="state.clickFrequency" type="number" class="number-input" min="0" max="10" />
                <span>* 每浏览10个候选人中随机点击查看的数量</span>
              </label>
            </div>
            <label class="checkbox-wrapper">
              <span>启用提示音</span>
              <input v-model="state.enableSound" type="checkbox" />
            </label>
          </div>

          <div class="filter-group">
            <label class="title">使用说明</label>
            <div class="section-help">
              <p>案例：招聘一个英语老师、本科、英语4级、40岁以下、女</p>
              <p>1. 在推荐牛人页面先筛好基础条件。</p>
              <p>2. 添加岗位和关键词，排除不合适条件。</p>
              <p>3. 点击开始，运行时注意观察并及时调整关键词。</p>
              <p>4. 刷新浏览器可以暂停。</p>
            </div>
          </div>

          <div class="filter-group">
            <label class="title">打赏记录</label>
            <RankingList :items="rankings" :loading="ui.loadingRanking" :error="ui.rankingError" />
          </div>
        </template>
      </FreePanel>

      <AIPanel
        v-if="state.currentTab === 'ai'"
        :state="state"
        :ui="ui"
        @update:phone="(value) => (state.phone = value)"
        @bind-phone="bindPhone(state.phone, 'ai')"
        @update:position-draft="(value) => (ui.aiPositionDraft = value)"
        @add-position="addPosition(true)"
        @select-position="selectPosition"
        @remove-position="removePosition"
      >
        <template #status>
          <div class="inline-options">
            <label class="checkbox-wrapper"><input v-model="state.communicationConfig.collectPhone" type="checkbox" />索要手机号</label>
            <label class="checkbox-wrapper"><input v-model="state.communicationConfig.collectWechat" type="checkbox" />索要微信号</label>
            <label class="checkbox-wrapper"><input v-model="state.communicationConfig.collectResume" type="checkbox" />索要简历</label>
          </div>
        </template>

        <template #rest>
          <div class="filter-group" v-if="currentPosition">
            <div style="display: flex; align-items: center; justify-content: space-between">
              <label class="title">岗位要求</label>
              <button
                class="keyword-btn"
                type="button"
                style="font-size: 12px; padding: 4px 8px"
                @click="pushLog('AI优化入口已预留，后续接接口', 'info')"
              >
                AI优化
              </button>
            </div>
            <div style="font-size: 11px; color: #d90000; margin-bottom: 10px">
              * 你可以把平台的岗位信息复制进来。点击AI优化后。
            </div>
            <textarea
              v-model="currentPosition.description"
              class="job-description"
              placeholder="请详细描述岗位要求"
            ></textarea>
          </div>

          <div class="filter-group">
            <label class="title">设置说明</label>
            <div class="section-help">
              * AI版本使用免费版的设置：随机延迟、打招呼暂停数、点击候选人频率、启用提示音
            </div>
          </div>

          <div class="filter-group">
            <label class="title">AI提示语</label>
            <textarea v-model="state.aiConfig.clickPrompt" class="job-description"></textarea>
          </div>

          <div class="filter-group">
            <label class="title">使用说明</label>
            <div class="section-help">
              <p>1. 首先配置AI设置。</p>
              <p>2. 添加岗位并详细描述岗位要求。</p>
              <p>3. 在招聘平台筛选好基本条件。</p>
              <p>4. 点击开始，AI将智能判断候选人是否合适。</p>
            </div>
          </div>

          <div class="filter-group">
            <label class="title">打赏记录</label>
            <RankingList :items="rankings" :loading="ui.loadingRanking" :error="ui.rankingError" />
          </div>
        </template>
      </AIPanel>
    </section>

    <section class="bottom-sticky">
      <div class="bottom-links">
        <a href="http://58it.cn" target="_blank">联系我</a>
        <a href="http://58it.cn" target="_blank">打赏我</a>
        <a href="http://goodhr.58it.cn" target="_blank">前往官网</a>
        <a href="http://goodhr.58it.cn" target="_blank" class="accent">分享给另一个HR</a>
      </div>
      <div class="bottom-controls">
        <div v-if="!ui.isRunning" class="initial-buttons">
          <button class="btn primary-btn" type="button" @click="startRun">开始</button>
        </div>
        <div v-else class="stop-buttons">
          <button class="btn danger-btn" type="button" @click="stopRun">停止</button>
        </div>
      </div>
      <RunLogPanel
        :logs="logs"
        :expanded="ui.logExpanded"
        :status-text="ui.isRunning ? '运行中' : '待机中'"
        @toggle-expand="ui.logExpanded = !ui.logExpanded"
        @clear="clearLogs"
      />
    </section>
  </main>

  <div v-if="ui.showAiConfig" class="ai-config-modal" @click.self="ui.showAiConfig = false">
    <div class="ai-config-content">
      <button class="close-btn" type="button" @click="ui.showAiConfig = false">&times;</button>
      <h3>选择模型</h3>
      <div class="model-cards-container">
        <div
          v-for="model in ui.modelCards"
          :key="model.name"
          class="model-card"
          :class="{ selected: state.aiConfig.model === model.name }"
          @click="state.aiConfig.model = model.name"
        >
          <div class="model-card-name">{{ model.name }}</div>
          <div class="model-card-description">{{ model.description }}</div>
          <div class="model-card-prices">
            <span>{{ model.inputPrice }}</span>
            <span>{{ model.outputPrice }}</span>
          </div>
        </div>
      </div>
      <button class="keyword-btn" type="button" @click="saveAiConfig">应用配置</button>
    </div>
  </div>
</template>

<script setup>
import { onMounted } from "vue";
import TabSwitcher from "./components/TabSwitcher.vue";
import FreePanel from "./components/FreePanel.vue";
import AIPanel from "./components/AIPanel.vue";
import RankingList from "./components/RankingList.vue";
import RunLogPanel from "./components/RunLogPanel.vue";
import { useGoodHrPanel } from "./composables/useGoodHrPanel.js";

const {
  state,
  ui,
  logs,
  rankings,
  currentPosition,
  freeKeywords,
  freeExcludeKeywords,
  hydrate,
  addPosition,
  removePosition,
  selectPosition,
  addKeyword,
  removeKeyword,
  pushLog,
  clearLogs,
  bindPhone,
  saveAiConfig,
  startRun,
  stopRun,
} = useGoodHrPanel();

onMounted(async () => {
  await hydrate();
});
</script>

<style>
@import "./styles/panel.css";

html,
body,
#app {
  width: 100%;
  height: 100%;
  margin: 0;
}
</style>
