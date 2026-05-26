<template>
  <div class="terminal-login">
    <div class="keyword-wall" aria-hidden="true">
      <div
        v-for="(row, index) in keywordRows"
        :key="index"
        :class="['keyword-row', index % 2 === 0 ? 'left' : 'right']"
      >
        <span v-for="word in row" :key="word">{{ word }}</span>
      </div>
    </div>
    <section class="product-intro">
      <p class="intro-kicker">GoodHR</p>
      <h1>给招聘人员用的自动化工具</h1>
      <p>
        自动读取招聘平台候选人列表，根据岗位模板和 AI
        配置判断是否匹配，再自动查看详情、评分、筛选并打招呼、沟通确认、推送结果、复核信息、邀约面试。
      </p>
      <div class="intro-points">
        <span>减少重复点击</span>
        <span>优先跟进匹配候选人</span>
        <span>每天看清打招呼结果</span>
      </div>
      <p class="intro-note">
        准备好本地程序、平台账号、岗位模板和个人 AI 配置后，任务就能自动执行。
      </p>
    </section>
    <div class="terminal-window">
      <div class="terminal-bar">
        <span class="bar-btn bar-close"></span>
        <span class="bar-btn bar-min"></span>
        <span class="bar-btn bar-max"></span>
        <span class="bar-title">GoodHR</span>
      </div>
      <div class="terminal-body">
        <div class="line">
          <span class="prompt">$</span>
          <span class="cmd">Welcome to GoodHR </span>
        </div>
        <!-- <div class="line dim">Welcome to GoodHR 5 Cloud Console</div> -->
        <div class="line dim">────────────────────────────────────</div>
        <div class="line" style="margin-top: 12px">
          <span class="prompt">&gt;</span>
          <input
            ref="emailRef"
            v-model="auth.email.value"
            class="terminal-input"
            placeholder="you@example.com"
            @keydown.enter="focusCode"
          />
          <span class="field-label">邮箱</span>
        </div>
        <div class="line">
          <span class="prompt">&gt;</span>
          <input
            ref="codeRef"
            v-model="auth.code.value"
            class="terminal-input"
            placeholder="4位验证码"
            maxlength="4"
            @keydown.enter="auth.login"
          />
          <span class="field-label">验证码</span>
        </div>
        <div v-if="auth.devCode.value" class="line dim">
          [dev] {{ auth.devCode.value }}
        </div>
        <div v-if="auth.error.value" class="line error">
          {{ auth.error.value }}
        </div>
        <div class="line" style="margin-top: 16px">
          <button
            class="terminal-btn"
            :disabled="auth.loading.value || !auth.email.value"
            @click="auth.sendCode"
          >
            [ 发送验证码 ]
          </button>
          <button
            class="terminal-btn primary"
            :disabled="auth.loading.value || !auth.code.value"
            @click="auth.login"
          >
            [ 登录 ]
          </button>
        </div>
        <div v-if="auth.loading.value" class="line" style="margin-top: 8px">
          <span class="blink">▌</span> processing...
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from "vue";
const props = defineProps({ auth: Object });
const emailRef = ref(null);
const codeRef = ref(null);
const keywordRows = [
  ["招聘", "候选人", "简历", "打招呼", "沟通", "面试", "筛选", "匹配"],
  ["Boss直聘", "猎聘", "智联", "58同城", "HR", "岗位模板", "AI评分"],
  ["自动筛选", "自动打招呼", "人才库", "回复率", "复聊", "跟进", "Offer"],
  ["薪资", "经验", "学历", "城市", "活跃候选人", "高匹配", "已沟通"],
  ["今日打招呼", "跳过原因", "查看详情", "推荐列表", "招聘效率", "沟通记录"],
  ["AI判断", "匹配分", "已扫描", "已跳过", "已回复", "待跟进", "高意向"],
  ["成都招聘", "销售", "客服", "运营", "老师", "开发", "人事"],
  ["自动化", "批量沟通", "精准筛选", "快速开聊", "职位匹配", "人才发现"],
];
function focusCode() {
  codeRef.value?.focus();
}
watch(
  () => props.auth?.devCode?.value,
  (v) => {
    if (v && codeRef.value) codeRef.value.focus();
  },
);
</script>

<style scoped>
.terminal-login {
  position: fixed;
  inset: 0;
  z-index: 100;
  overflow: hidden;
  display: flex;
  justify-content: center;
  align-items: center;
  gap: 28px;
  min-height: 100vh;
  width: 100vw;
  padding: 24px;
  background: #020202;
}
.keyword-wall {
  position: absolute;
  inset: -38vh -18vw;
  z-index: 0;
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 18px;
  pointer-events: none;
  opacity: 0.22;
  transform: rotate(-8deg);
}
.keyword-row {
  display: flex;
  gap: 34px;
  white-space: nowrap;
  color: #174a17;
  font-size: clamp(34px, 7vw, 92px);
  line-height: 1;
  font-weight: 700;
  text-transform: uppercase;
  animation-duration: 12s;
  animation-timing-function: linear;
  animation-iteration-count: infinite;
}
.keyword-row.left {
  animation-name: drift-left;
}
.keyword-row.right {
  animation-name: drift-right;
  color: #2a332a;
  animation-duration: 14s;
}
.product-intro {
  position: relative;
  z-index: 1;
  width: 420px;
  max-width: 100%;
  border-left: 2px solid #0f0;
  padding: 8px 0 8px 18px;
}
.intro-kicker {
  color: #0f0;
  font-size: 13px;
  margin-bottom: 8px;
}
.product-intro h1 {
  color: #fff;
  font-size: 28px;
  line-height: 1.25;
  font-weight: normal;
  margin-bottom: 14px;
}
.product-intro p {
  color: #ddd;
  line-height: 1.8;
}
.intro-points {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 16px 0;
}
.intro-points span {
  border: 1px solid #333;
  color: #0f0;
  padding: 6px 10px;
  font-size: 12px;
  background: #050505;
}
.intro-note {
  color: #aaa;
  font-size: 13px;
}
.terminal-window {
  position: relative;
  z-index: 1;
  width: 480px;
  max-width: 100%;
  border: 1px solid #0f0;
  background: #050505;
}
.terminal-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-bottom: 1px solid #333;
  background: #0d0d0d;
}
.bar-title {
  flex: 1;
  text-align: center;
  font-size: 12px;
  color: #fff;
  margin-right: 40px;
}
.bar-btn {
  width: 12px;
  height: 12px;
  border-radius: 0;
  display: inline-block;
}
.bar-close {
  background: #e33;
}
.bar-min {
  background: #e83;
}
.bar-max {
  background: #3a3;
  opacity: 0.5;
}
.terminal-body {
  padding: 20px;
  min-height: 260px;
}
.line {
  font-size: 14px;
  line-height: 2;
  display: flex;
  align-items: center;
  gap: 4px;
}
.line.dim {
  color: #555;
  margin: 2px 0;
}
.line.error {
  color: #f33;
  margin-top: 8px;
}
.prompt {
  color: #0f0;
  margin-right: 4px;
  flex-shrink: 0;
}
.cmd {
  color: #0a0;
}
.placeholder {
  color: #fff; /* 灰色，可换成 #666、#fff 等 */
}
.terminal-input {
  .placeholder {
    color: #fff;
  }
  width: 180px;
  background: transparent;
  border: none;
  border-bottom: 1px dashed #b6b6b6;
  color: #0f0;
  font-family: inherit;
  font-size: 14px;
  padding: 2px 4px;
  outline: none;

  /* 提示文字颜色 白色 */
}
.terminal-input:focus {
  border-bottom-color: #0f0;
}
.terminal-input::placeholder {
  color: #333;
}
.field-label {
  color: #fff;
  font-size: 12px;
  margin-left: 4px;
}
.terminal-btn {
  background: transparent;
  border: 1px solid #333;
  color: #fff;
  font-family: inherit;
  font-size: 13px;
  padding: 6px 16px;
  cursor: pointer;
  margin-right: 8px;
  border-radius: 0;
}
.terminal-btn:hover:not(:disabled) {
  border-color: #0f0;
  color: #0f0;
}
.terminal-btn.primary {
  border-color: #0f0;
  color: #0f0;
}
.terminal-btn.primary:hover:not(:disabled) {
  background: #0f0;
  color: #000;
}
.terminal-btn:disabled {
  opacity: 0.3;
  cursor: not-allowed;
}
.blink {
  animation: blink 1s step-end infinite;
}
@keyframes blink {
  50% {
    opacity: 0;
  }
}
@keyframes drift-left {
  from {
    transform: translateX(18%);
  }
  to {
    transform: translateX(-46%);
  }
}
@keyframes drift-right {
  from {
    transform: translateX(-46%);
  }
  to {
    transform: translateX(18%);
  }
}
@media (max-width: 900px) {
  .terminal-login {
    flex-direction: column;
    align-items: stretch;
    overflow-y: auto;
  }
  .product-intro {
    width: 100%;
  }
  .terminal-window {
    width: 100%;
  }
}
</style>
