/**
 * 拉勾网平台选择器配置
 *
 * 旧项目参考：content_scripts/sites/lagou.js
 * 拉勾网显示职位而非候选人姓名
 */

import type { PlatformConfig } from "./types.js";

export const lagouConfig: PlatformConfig = {
  id: "lagou",
  name: "拉勾网",
  domain: "lagou.com",
  pages: [],

  card: {
    container: ".position-list",
    card: [".position-item"],
    name: ".position-name",
    basicInfo: [".age-info"],
    education: [".edu-background"],
    university: ".school-name",
    description: ".position-detail",
  },

  actions: {
    greetBtn: [".position-title"],
    continueBtn: [],
    phoneBtn: [],
    wechatBtn: [],
    resumeBtn: [],
    confirmBtn: [],
  },

  detail: {
    openTarget: [".position-item"],
    closeBtn: [],
    messageTip: "",
    messageItem: "",
  },

  extras: [
    { selector: ".salary", label: "薪资" },
    { selector: ".company-name", label: "公司" },
    { selector: ".industry-field", label: "行业" },
    { selector: ".position-address", label: "地点" },
  ],

  behavior: {
    needsDetailPage: true,
    supportsPaging: false,
    nextPageBtn: "",
    nextPageDisabledClass: "",
  },
};
