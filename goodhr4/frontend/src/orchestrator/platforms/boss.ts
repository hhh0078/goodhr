/**
 * Boss直聘平台选择器配置
 *
 * 旧项目参考：content_scripts/sites/boss.js
 * Boss直聘是最完整的平台，包括：
 * - API 拦截器获取候选人详细数据
 * - 多种候选卡类型（推荐牛人、搜索结果）
 * - 粗筛通过后直接打招呼（AI模式跳过精筛）
 */

import type { PlatformConfig } from "./types.js";

export const bossConfig: PlatformConfig = {
  id: "boss",
  name: "Boss直聘",
  urlPattern: /zhipin\.com/,

  card: {
    container: ".card-list",
    card: [
      ".candidate-card-wrap",
      ".geek-info-card",
      ".card-container",
    ],
    name: ".name",
    basicInfo: [".job-card-left"],
    education: [".base-info.join-text-wrap", ".geek-info-detail"],
    university: ".content.join-text-wrap",
    description: ".content",
  },

  actions: {
    greetBtn: [".btn.btn-greet", ".btn.btn-getcontact"],
    continueBtn: [".btn.btn-continue.btn-outline"],
    phoneBtn: [".operate-item"],
    wechatBtn: [],
    resumeBtn: [],
    confirmBtn: [],
  },

  detail: {
    openTarget: [
      ".card-inner.common-wrap",
      ".card-inner.clear-fix",
      ".candidate-card-wrap",
    ],
    closeBtn: [".boss-popup__close", ".resume-custom-close"],
    messageTip: ".chat-global-entry",
    messageItem: ".friend-list-item",
  },

  extras: [
    { selector: ".salary-text", label: "薪资" },
    { selector: ".job-info-primary", label: "基本信息" },
    { selector: ".tags-wrap", label: "标签" },
    { selector: ".content.join-text-wrap", label: "公司信息" },
  ],

  behavior: {
    needsDetailPage: false,
    supportsPaging: false,
    nextPageBtn: "",
    nextPageDisabledClass: "",
  },
};
