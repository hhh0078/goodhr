/** 本文件维护官网的平台能力说明，区分招聘自动化、简历插件导入和仍在开发的流程。 */

export const RECRUITMENT_PLATFORM_FACTS = [
  {
    id: "boss",
    name: "BOSS直聘",
    automationStatus: "已接入",
    automationDescription: "面向招聘端读取候选人、按岗位要求筛选并打招呼；请先登录并确认当前招聘岗位。",
    resumeImportDescription: "简历插件支持从候选人页面确认并导入资料。",
    autoReplyStatus: "开发验证中",
  },
  {
    id: "liepin",
    name: "猎聘企业端",
    automationStatus: "已接入",
    automationDescription: "围绕企业招聘岗位处理候选人、筛选与打招呼；与猎头端使用不同的入口和岗位设置。",
    resumeImportDescription: "简历插件支持企业端候选人资料导入。",
    autoReplyStatus: "开发验证中",
  },
  {
    id: "hliepin",
    name: "猎聘猎头端",
    automationStatus: "已接入",
    automationDescription: "按岗位与搜索条件查找候选人，筛选后继续沟通；入库后可另行记录客户岗位的推荐进度。",
    resumeImportDescription: "简历插件支持猎头端候选人资料导入。",
    autoReplyStatus: "开发验证中",
  },
  {
    id: "zhaopin",
    name: "智联招聘",
    automationStatus: "已接入",
    automationDescription: "围绕招聘岗位进行候选人读取、筛选与打招呼；新版客户端同时支持推荐和搜索人才页，筛选条件需在对应平台确认。",
    resumeImportDescription: "简历插件支持智联招聘候选人资料导入。",
    autoReplyStatus: "开发验证中",
  },
  {
    id: "51job",
    name: "前程无忧（51job）",
    automationStatus: "新版客户端已接入",
    automationDescription: "新版本地程序支持人才推荐、搜索人才页的候选人筛选与打招呼；本次 Mac 安装包已更新，Windows 安装包随后更新，请先升级本地程序。",
    resumeImportDescription: "简历插件已支持前程无忧资料导入；导入能力与招聘自动化分别上线。",
    autoReplyStatus: "开发中",
  },
] as const;

export const RECRUITMENT_PLATFORM_SUMMARY =
  "招聘自动化已接入 BOSS直聘、猎聘企业端、猎聘猎头端和智联招聘；前程无忧（51job）人才推荐、搜索人才页已接入新版客户端，本次 Mac 安装包已更新，Windows 安装包随后更新。简历插件继续独立支持导入，各项能力以正式客户端和控制台为准。";

export const AUTO_REPLY_RELEASE_NOTICE =
  "招聘消息自动回复仍在开发验证，尚未正式发布。功能介绍不代表所有平台已经开放，请以正式版本的更新说明和实际入口为准。";
