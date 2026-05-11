/**
 * 平台选择器和配置的类型定义
 *
 * 每个平台定义自己的 CSS 选择器和行为配置，
 * 供 bridge.ts 和 orchestrator.ts 使用。
 *
 * 选择器统一使用 CSS 选择器格式（如 ".card-list"），
 * 旧项目中的 className（如 "card-list"）需加上 "." 前缀。
 */

/** 候选卡列表选择器配置 */
export interface CardSelectors {
  /** 候选人卡片列表容器 */
  container: string;
  /** 单个候选人卡片（支持多个备选） */
  card: string[];
  /** 候选人姓名 */
  name: string;
  /** 候选人年龄/基本信息 */
  basicInfo: string[];
  /** 候选人学历 */
  education: string[];
  /** 候选人学校 */
  university: string;
  /** 候选人描述/自我介绍 */
  description: string;
}

/** 操作按钮选择器配置 */
export interface ActionSelectors {
  /** 打招呼按钮（支持多个备选） */
  greetBtn: string[];
  /** 继续沟通按钮 */
  continueBtn: string[];
  /** 索要手机号按钮 */
  phoneBtn: string[];
  /** 索要微信按钮 */
  wechatBtn: string[];
  /** 索要简历按钮 */
  resumeBtn: string[];
  /** 确认按钮 */
  confirmBtn: string[];
}

/** 详情面板选择器配置 */
export interface DetailSelectors {
  /** 打开详情的点击目标 */
  openTarget: string[];
  /** 关闭详情面板按钮 */
  closeBtn: string[];
  /** 消息提示入口 */
  messageTip: string;
  /** 消息列表项 */
  messageItem: string;
}

/** 额外信息提取配置 */
export interface ExtraInfoSelector {
  /** CSS 选择器 */
  selector: string;
  /** 信息类型标签（如"薪资"、"标签"） */
  label: string;
}

/** 平台行为配置 */
export interface PlatformBehavior {
  /** 是否需要打开详情页 */
  needsDetailPage: boolean;
  /** 是否支持翻页 */
  supportsPaging: boolean;
  /** 翻页按钮选择器（支持翻页时） */
  nextPageBtn: string;
  /** 翻页禁用态 class（支持翻页时） */
  nextPageDisabledClass: string;
}

/** 平台配置完整类型 */
export interface PlatformConfig {
  /** 平台唯一标识 */
  id: string;
  /** 平台显示名称 */
  name: string;
  /** URL 匹配域名（如 "zhipin.com"），前端用 includes() 匹配 */
  domain: string;
  /** 候选卡选择器 */
  card: CardSelectors;
  /** 操作按钮选择器 */
  actions: ActionSelectors;
  /** 详情面板选择器 */
  detail: DetailSelectors;
  /** 额外信息选择器 */
  extras: ExtraInfoSelector[];
  /** 平台行为配置 */
  behavior: PlatformBehavior;
}

/** common.js 返回的序列化元素信息 */
export interface SerializedElement {
  __id: string;
  index: number;
  tagName: string;
  text: string;
  className: string;
  rect: {
    top: number;
    left: number;
    width: number;
    height: number;
  };
}

/** bridge.find 返回的候选卡信息 */
export interface CandidateCard {
  elementId: string;
  index: number;
  name: string;
  info: string;
}
