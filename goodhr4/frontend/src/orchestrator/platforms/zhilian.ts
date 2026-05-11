/**
 * 智联招聘平台选择器配置
 *
 * 旧项目参考：content_scripts/sites/zhilian.js
 */

import type { PlatformConfig } from "./types.js";

export const zhilianConfig: PlatformConfig = {
  id: "zhilian",
  name: "智联招聘",
  domain: "zhaopin.com",

  card: {
    container: '[role="group"]',
    card: [".recommend-item__inner.recommend-resume-item__inner"],
    name: ".talent-basic-info__name--inner",
    basicInfo: [".talent-basic-info__basic"],
    education: [".resume-item__content.resume-card-exp"],
    university: "",
    description: ".resume-item__content",
  },

  actions: {
    greetBtn: [".small-screen-btn.is-mr-16"],
    continueBtn: [".small-screen-btn.is-mr-16.km-button.km-control.km-ripple-off.km-button--light.km-button--plain.resume-btn-small"],
    phoneBtn: [],
    wechatBtn: [],
    resumeBtn: [],
    confirmBtn: [],
  },

  detail: {
    openTarget: [".new-resume-detail--inner"],
    closeBtn: [],
    messageTip: "",
    messageItem: "",
  },

  extras: [
    { selector: ".talent-basic-info__extra--content", label: "薪资" },
  ],

  behavior: {
    needsDetailPage: true,
    supportsPaging: false,
    nextPageBtn: "",
    nextPageDisabledClass: "",
  },
};
