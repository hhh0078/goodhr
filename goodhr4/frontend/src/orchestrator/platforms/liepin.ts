/**
 * 猎聘网(lpt.liepin.com)平台选择器配置
 *
 * 旧项目参考：content_scripts/sites/liepin.js
 */

import type { PlatformConfig } from "./types.js";

export const liepinConfig: PlatformConfig = {
  id: "liepin",
  name: "猎聘网",
  domain: "lpt.liepin.com",
  pages: [
    { url: "lpt.liepin.com/recommend", title: "人才推荐" },
  ],

  card: {
    container: ".recommandResumes--",
    card: [".newResumeItemWrap--"],
    name: ".nest-resume-personal-name",
    basicInfo: [".personal-detail-age"],
    education: [".personal-detail-edulevel"],
    university: ".resume-university",
    description: ".resume-description",
  },

  actions: {
    greetBtn: [".ant-lpt-btn.ant-lpt-btn-primary.ant-lpt-teno-btn.ant-lpt-teno-btn-secondary"],
    continueBtn: [".ant-lpt-btn.ant-lpt-btn-primary.ant-lpt-teno-btn.ant-lpt-teno-btn-secondary"],
    phoneBtn: [".im-ui-action-button.action-item.action-phone"],
    wechatBtn: [".im-ui-action-button.action-item.action-wechat"],
    resumeBtn: [".im-ui-action-button.action-item.action-resume"],
    confirmBtn: [".ant-im-btn.ant-im-btn-primary"],
  },

  detail: {
    openTarget: [".newResumeItem"],
    closeBtn: [".closeBtn--"],
    messageTip: "",
    messageItem: "",
  },

  extras: [
    { selector: ".nest-resume-personal-skills", label: "技能" },
    { selector: ".personal-expect-content", label: "薪资" },
    { selector: ".personal-detail-location", label: "地点" },
  ],

  behavior: {
    needsDetailPage: true,
    supportsPaging: false,
    nextPageBtn: "",
    nextPageDisabledClass: "",
  },
};
