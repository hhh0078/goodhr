<template>
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

      <div v-if="ui.configTab === 'runtime'" class="content-grid compact-grid">
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
</template>

<script setup lang="ts">
import { usePanelStore } from "../composables/usePanelStore";

const { settings, ui, availableModels, requestAutoSave, resetClickPrompt } =
  usePanelStore();
</script>
