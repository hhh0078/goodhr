<template>
  <section class="hero card" style="background-color: var(--primary); color: #fff;">
    <div class="hero-main">
      <div>
        <h1 style="margin: 0px; margin-bottom: 10px;text-align: center;">goodHR {{ APP_VERSION }}</h1>
        <div v-for="value in ui.systemConfig.announcement" style="text-align: center;">{{ value }}</div>
      </div>
    </div>
   
  </section>
  
  <main class="app-shell">
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
          <button class="btn btn-primary" type="button" :disabled="ui.binding" @click="bindAccount">
            {{ ui.binding ? "绑定中..." : "绑定" }}
          </button>
        </div>

        <div>
          余额:¥
          <strong :style="{ color: balanceColor }">{{ settings.aiBalanceText || "--" }}</strong>
          &nbsp;
                    <a style="border: 1px solid #ccc; padding: 2px 4px; border-radius: 4px; text-decoration: none;color: #000;" href="https://ai.58it.cn" target="_blank" rel="noreferrer noopener">ai充值(58AI)</a>

          &nbsp;&nbsp;
          <span style="cursor: pointer; border: 1px solid #ccc; padding: 2px 4px;border-radius: 4px;" @click="showPricingHint" >价格说明</span>
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
      <div class="tabs" >
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

      <div v-if="settings.runMode === 'free'" class="content-grid" @focusout.capture="requestAutoSave">
        <section class="card" :class="ui.running ? 'span-8' : 'span-12'">
          <div class="section-heading">
            <div style="color: black;margin-bottom: 10px;">
              岗位与关键词
            </div>
          </div>

          <div class="position-toolbar">
            <input
              v-model="ui.positionDraft"
              class="text-input"
              placeholder="新增岗位名称 例如：销售顾问"
              @keydown.enter.prevent="addPosition"
            />
            <button class="btn btn-primary" type="button" @click="addPosition">新增岗位</button>
          </div>


          <div class="position-list" style="margin-top: 10px;">
            <button
              v-for="position in settings.positions"
              :key="position.name"
              class="position-item"
              :class="{ active: settings.currentPositionName === position.name }"
              type="button"
              @click="settings.currentPositionName = position.name"
            >
              <span>{{ position.name }}</span>
              <span class="position-remove" @click.stop="confirmRemovePosition(position.name)">×</span>
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
              <button class="btn btn-primary" type="button" @click="addKeyword('include')">添加</button>
            </div>
            <span class="section-note" >当候选人的所有信息里出现这个关键词就会打招呼</span>

            <div class="chip-list compact-chips">
              <span v-for="keyword in currentPosition.keywords" :key="keyword" class="chip include">
                {{ keyword }}
                <button type="button" @click="removeKeyword('include', keyword)">×</button>
              </span>
            </div>

            <div class="keyword-row">
              <input
                v-model="ui.excludeDraft"
                class="text-input"
                placeholder="输入后回车或点击排除"
                @keydown.enter.prevent="addKeyword('exclude')"
              />
              <button class="btn btn-danger" type="button" @click="addKeyword('exclude')">排除</button>
            </div>
            <span class="section-note">当候选人的所有信息里出现这个关键词就会排除</span>
            <div class="chip-list compact-chips">
              <span v-for="keyword in currentPosition.excludeKeywords" :key="keyword" class="chip exclude">
                {{ keyword }}
                <button type="button" @click="removeKeyword('exclude', keyword)">×</button>
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
            <div v-for="(entry, index) in logs.slice().reverse()" :key="`${entry.time}-${index}`" class="log-item compact">
              <span class="log-time">{{ entry.time }}</span>
              <span class="log-level" :class="entry.type">{{ entry.type }}</span>
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
            <button class="btn btn-secondary" type="button" @click="addPosition">新增岗位</button>
          </div>

          <div class="position-list" style="margin-top: 5px;margin-bottom: 10px;">
            <button
              v-for="position in settings.positions"
              :key="position.name"
              class="position-item"
              :class="{ active: settings.currentPositionName === position.name }"
              type="button"
              @click="settings.currentPositionName = position.name"
            >
              <span>{{ position.name }}</span>
              <span class="position-remove" @click.stop="confirmRemovePosition(position.name)">x</span>
            </button>
          </div>

          <div v-if="currentPosition" class="field-group">
            <label>岗位说明</label>
            <textarea
              v-model="currentPosition.description"
              class="text-area ai-compact"
              placeholder="请填写完整岗位描述、要求、班次、学历、经验等条件"
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
            <div v-for="(entry, index) in logs.slice().reverse()" :key="`${entry.time}-${index}`" class="log-item compact">
              <span class="log-time">{{ entry.time }}</span>
              <span class="log-level" :class="entry.type">{{ entry.type }}</span>
              <span class="log-text">{{ entry.message }}</span>
            </div>
          </div>
        </section>
      </div>
    </section>

    <section class="config-panel card">
      <button
        class="config-toggle"
        type="button"
        @click="ui.configExpanded = !ui.configExpanded"
      >
        <div>
          <div style="color: black;">运行参数/AI配置</div>
        </div>
        <span class="config-arrow">{{ ui.configExpanded ? "收起" : "展开" }}</span>
      </button>

      <div v-if="ui.configExpanded" class="config-body" @focusout.capture="requestAutoSave">
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

        <div v-if="ui.configTab === 'runtime'" class="content-grid compact-grid">
          <section class="span-7 field-stack">
            <div class="settings-grid">
              <label class="field-group">
                <span>打招呼暂停数</span>
                <input v-model.number="settings.matchLimit" class="text-input" type="number" min="1" />
              </label>
              <label class="field-group">
                <span>最小延迟秒数</span>
                <input v-model.number="settings.scrollDelayMin" class="text-input" type="number" min="0" />
              </label>
              <label class="field-group">
                <span>最大延迟秒数</span>
                <input v-model.number="settings.scrollDelayMax" class="text-input" type="number" min="0" />
              </label>
              <label class="field-group">
                <span>点击候选人频率</span>
                <input v-model.number="settings.clickFrequency" class="text-input" type="number" min="0" max="10" />
              </label>
            </div>
          </section>

          <section class="span-5 field-stack">
            <div class="checkbox-grid">
              <label><input v-model="settings.isAndMode" type="checkbox" /> 全部关键词都要命中</label>
              <label><input v-model="settings.enableSound" type="checkbox" /> 启用提示音</label>
              <label><input v-model="settings.runModeConfig.greetingEnabled" type="checkbox" /> 启用打招呼</label>
              <label><input v-model="settings.runModeConfig.communicationEnabled" type="checkbox" /> 启用沟通处理</label>
              <label><input v-model="settings.communicationConfig.collectPhone" type="checkbox" /> 索要手机号</label>
              <label><input v-model="settings.communicationConfig.collectWechat" type="checkbox" /> 索要微信号</label>
              <label><input v-model="settings.communicationConfig.collectResume" type="checkbox" /> 索要简历</label>
            </div>
          </section>
        </div>

        <div v-else class="content-grid compact-grid">
          <section class="span-6 field-stack">
            <div class="settings-grid">
              <label class="field-group">
                <span>平台</span>
                <select v-model="settings.aiConfig.platform" class="text-input">
                  <option value="siliconflow">siliconflow</option>
                </select>
              </label>
              <label class="field-group">
                <span>模型</span>
                <input v-model="settings.aiConfig.model" class="text-input" type="text" />
              </label>
              <label class="field-group">
                <span>主 Token</span>
                <input v-model="settings.aiConfig.token" class="text-input" type="text" />
              </label>
            </div>
          </section>

          <section class="span-6 field-stack">
            <label class="field-group">
              <span>查看详情 Prompt</span>
              <textarea
                v-model="settings.aiConfig.clickPrompt"
                class="text-area prompt-area"
                :placeholder="
                  ui.systemConfig.default_click_prompt
                    ? '留空则使用系统默认 Prompt'
                    : '请输入查看详情 Prompt'
                "
              />
            </label>
          </section>
        </div>
       
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

    <footer class="action-bar floating-action-bar">
      <div class="site-links footer-links">
        <a :href="ui.systemConfig.contact_url" target="_blank" rel="noreferrer">联系我</a>
        <a :href="ui.systemConfig.donate_url" target="_blank" rel="noreferrer">打赏我</a>
        <a :href="ui.systemConfig.website_url" target="_blank" rel="noreferrer">前往官网</a>
      </div>
      <div class="header-actions footer-actions">
        <button v-if="!ui.running" class="btn btn-primary btn-large" type="button" @click="startRun">开始运行</button>
        <button v-else class="btn btn-danger btn-large" type="button" @click="stopRun">停止运行</button>
      </div>
    </footer>
  </main>
</template>

<script setup>
import { computed } from "vue";
import { usePanelStore } from "./composables/usePanelStore.js";
import { APP_VERSION } from "./constants/appVersion.js";

const {
  settings,
  ui,
  logs,
  currentPosition,
  addPosition,
  removePosition,
  addKeyword,
  removeKeyword,
  bindAccount,
  requestAutoSave,
  startRun,
  stopRun,
} = usePanelStore();

const ads = computed(() => {
  if (!Array.isArray(ui.systemConfig.ads)) {
    return [];
  }
  return ui.systemConfig.ads.filter((item) => item && item.title && item.url).slice(0, 3);
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
