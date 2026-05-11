/**
 * 猎聘网(h.liepin.com)平台选择器配置
 *
 * 旧项目参考：content_scripts/sites/hliepin.js
 * 特点：支持翻页功能
 */

import type { PlatformConfig } from "./types.js";

export const hliepinConfig: PlatformConfig = {
  id: "hliepin",
  name: "猎聘网(h)",
  domain: "h.liepin.com",
  pages: [
    { url: "h.liepin.com/search/getConditionItem", title: "找人" },
  ],

  card: {
    container: ".recommandResumes--",
    card: [".no-hover-tr"],
    name: ".new-resume-personal-name",
    basicInfo: [".personal-detail-age"],
    education: [".J1lRR"],
    university: ".J1lRR",
    description: ".new-resume-personal-expect",
  },

  actions: {
    greetBtn: [".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"],
    continueBtn: [".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"],
    phoneBtn: [".ant-btn.ant-btn-primary.__im_basic__basic-input-action", ".im-ui-action-button.action-item"],
    wechatBtn: [],
    resumeBtn: [],
    confirmBtn: [".ant-btn.ant-btn-link.ant-btn-lg.btn-cancel.directly-open-chat-btn"],
  },

  detail: {
    openTarget: [".tlog-common-resume-card"],
    closeBtn: [".closeBtn--"],
    messageTip: "",
    messageItem: "",
  },

  extras: [
    { selector: ".J1lRR", label: "详情" },
    { selector: ".new-resume-offline", label: "在线状态" },
  ],

  behavior: {
    needsDetailPage: true,
    supportsPaging: true,
    nextPageBtn: ".ant-pagination-next",
    nextPageDisabledClass: "ant-pagination-disabled",
  },
};
