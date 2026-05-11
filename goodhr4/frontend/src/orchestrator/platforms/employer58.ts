/**
 * 58同城平台选择器配置
 *
 * 旧项目参考：content_scripts/sites/employer58.js
 * 特点：不做筛选，直接打招呼
 */

import type { PlatformConfig } from "./types.js";

export const employer58Config: PlatformConfig = {
  id: "employer58",
  name: "58同城",
  urlPattern: /employer\.58\.com|58\.com/,

  card: {
    container: ".recommandResumes--",
    card: [".recommend-list.recommendList"],
    name: ".trueName.mycustomf",
    basicInfo: [".mycustomfontgobp1qai0ai2y2.resumeFont2"],
    education: [".hover-wrapper"],
    university: ".hover-wrapper",
    description: ".mycustomfont0zfunx8fq907",
  },

  actions: {
    greetBtn: [".el-button.chat-btn.el-button--primary"],
    continueBtn: [],
    phoneBtn: [],
    wechatBtn: [],
    resumeBtn: [],
    confirmBtn: [".ant-btn.ant-btn-link.ant-btn-lg.btn-cancel.directly-open-chat-btn"],
  },

  detail: {
    openTarget: [".recommend-list.recommendList"],
    closeBtn: [".closeBtn--"],
    messageTip: "",
    messageItem: "",
  },

  extras: [
    { selector: ".J1lRR", label: "详情" },
    { selector: ".recommend-status", label: "在线状态" },
  ],

  behavior: {
    needsDetailPage: false,
    supportsPaging: true,
    nextPageBtn: ".ant-pagination-next",
    nextPageDisabledClass: "ant-pagination-disabled",
  },
};
