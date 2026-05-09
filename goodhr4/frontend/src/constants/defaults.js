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

export const DEFAULT_SETTINGS = {
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
    token: "",
    model: "gpt-5.1-chat",
    clickPrompt: "",
    contactPrompt: null,
    platform: "siliconflow",
  },
  aiExpireTime: "2099-10-30",
  aiBalance: null,
  aiBalanceText: "",
  authUser: null,
  authApiKey: null,
};

export function createDefaultSettings() {
  return deepClone(DEFAULT_SETTINGS);
}

export function createEmptyPosition(name = "") {
  return {
    name,
    keywords: [],
    excludeKeywords: [],
    description: "",
  };
}

export const DEFAULT_LOGS = [
  {
    type: "info",
    message: "系统待机，等待绑定或启动。",
    time: "",
  },
];
