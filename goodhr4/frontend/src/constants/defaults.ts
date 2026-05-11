/**
 * 默认设置和常量定义
 */

import { deepClone } from "../utils/clone.js";
import { APP_VERSION } from "./appVersion.js";

export const DEFAULT_CLICK_PROMPT = `你是一个资深的HR专家。请根据候选人的基本信息判断是否值得查看其详细信息。

重要提示：
1. 这个API仅用于岗位与候选人的筛选。如果内容不是这些，你应该返回"内容与招聘无关 无法解答"。
2. 请根据岗位要求判断是否值得查看这位候选人的详细信息。
3. 必须返回JSON格式，包含decision和reason两个字段。
4. decision字段只能是"是"或"否"。
5. reason字段是决策原因，10个字以内。
6. 如果岗位要求中包含"经验"，则必须考虑候选人的工作经验。
7. 如果岗位要求中包含"学历"，则必须考虑候选人的学历。
8. 如果候选人信息中没有工作经历。那很可能只是基础信息。这时岗位信息中某个条件、但是候选人信息中没提到的，你应该无视这个条件。
9. 你应该主动分析岗位信息是不是属于高要求的岗位。如果是，则需要详细严格筛选候选人信息。如果是要求低的普通岗位，那就简单筛选。

岗位要求：
\${岗位信息}

候选人基本信息：
\${候选人信息}

请判断是否值得查看这位候选人的详细信息，返回JSON格式：{"decision":"是","reason":"符合基本要求"}`;

export const STORAGE_KEY = "goodhr4_sidebar_state";
export const IDENTITY_KEY = "goodhr4_identity";
export const LOGS_KEY = "goodhr4_logs";
export const MAX_LOGS = 100;

/** 通信配置 */
export interface CommunicationConfig {
  collectPhone: boolean;
  collectWechat: boolean;
  collectResume: boolean;
}

/** 公司信息 */
export interface CompanyInfo {
  content: string;
}

/** 职位额外信息 */
export interface JobInfo {
  extraInfo: string;
}

/** 运行模式配置 */
export interface RunModeConfig {
  communicationEnabled: boolean;
  greetingEnabled: boolean;
}

/** AI配置 */
export interface AIConfig {
  apiKey: string;
  model: string;
  clickPrompt: string;
  contactPrompt: string | null;
}

/** 岗位 */
export interface Position {
  name: string;
  keywords: string[];
  excludeKeywords: string[];
  description: string;
}

/** 系统更新信息 */
export interface UpdateInfo {
  version: string;
  content: string;
  force_update: boolean;
  download_url?: string;
}

/** 系统配置（从服务端拉取） */
export interface SystemConfig {
  website_url: string;
  contact_url: string;
  donate_url: string;
  share_url: string;
  announcement: string[];
  default_click_prompt: string;
  default_model: string;
  optimize_prompt: string;
  models: ModelItem[];
  ads: AdItem[];
  update_info: UpdateInfo;
}

/** 模型选项 */
export interface ModelItem {
  model_id: string;
  description: string;
}

/** 广告项 */
export interface AdItem {
  title: string;
  url: string;
  subtitle?: string;
  background_color?: string;
  text_color?: string;
  border_color?: string;
}

/** 日志条目 */
export interface LogEntry {
  type: "info" | "success" | "warning" | "error";
  message: string;
  time: string;
}

/** 应用设置 */
export interface Settings {
  version: string;
  runMode: "free" | "ai";
  currentSection: string;
  identity: string;
  identityType: string;
  positions: Position[];
  currentPositionName: string;
  isAndMode: boolean;
  matchLimit: number;
  enableSound: boolean;
  scrollDelayMin: number;
  scrollDelayMax: number;
  clickFrequency: number;
  communicationConfig: CommunicationConfig;
  companyInfo: CompanyInfo;
  jobInfo: JobInfo;
  runModeConfig: RunModeConfig;
  aiConfig: AIConfig;
  aiExpireTime: string;
  aiBalance: number | null;
  aiBalanceText: string;
  authUser: any;
  authApiKey: string | null;
}

export const DEFAULT_SETTINGS: Settings = {
  version: APP_VERSION,
  runMode: "ai",
  currentSection: "overview",
  identity: "",
  identityType: "",
  positions: [],
  currentPositionName: "",
  isAndMode: false,
  matchLimit: 60,
  enableSound: true,
  scrollDelayMin: 3,
  scrollDelayMax: 8,
  clickFrequency: 7,
  communicationConfig: {
    collectPhone: true,
    collectResume: true,
    collectWechat: true,
  },
  companyInfo: {
    content: "",
  },
  jobInfo: {
    extraInfo: "",
  },
  runModeConfig: {
    communicationEnabled: true,
    greetingEnabled: true,
  },
  aiConfig: {
    apiKey: "",
    model: "",
    clickPrompt: "",
    contactPrompt: null,
  },
  aiExpireTime: "2099-10-30",
  aiBalance: null,
  aiBalanceText: "",
  authUser: null,
  authApiKey: null,
};

/** 创建默认设置的深拷贝 */
export function createDefaultSettings(): Settings {
  return deepClone(DEFAULT_SETTINGS);
}

/** 创建空白岗位 */
export function createEmptyPosition(name = ""): Position {
  return {
    name,
    keywords: [],
    excludeKeywords: [],
    description: "",
  };
}

export const DEFAULT_LOGS: LogEntry[] = [
  {
    type: "info",
    message: "系统待机，等待绑定或启动。",
    time: "",
  },
];
