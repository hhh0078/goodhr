/** 本文件定义新手遮罩的展示合同和分步说明，界面只呈现状态，不代替用户操作。 */
import type { OnboardingState, OnboardingStep } from "./onboarding-state";

export type OnboardingPhase = "loading" | "invite" | "skip" | "active" | "paused" | "declined" | "completed" | "silent";
export type OnboardingCelebration = { id: string; title: string; description: string };
export type OnboardingActions = {
  accept: () => void;
  pause: () => void;
  requestSkip: () => void;
  cancelSkip: () => void;
  confirmSkip: () => void;
  back: () => void;
  requestRestart: () => void;
  cancelRestart: () => void;
  confirmRestart: () => void;
  openHelp: () => void;
  closeHelp: () => void;
  choosePlatform: (platformId: string) => void;
  openPlatform: () => void;
  downloadAgent: () => void;
  checkAgent: () => void;
  resume: () => void;
  finishClose: () => void;
  dismissCelebration: () => void;
  refreshPlatforms: () => void;
};
export type OnboardingExperienceProps = {
  phase: OnboardingPhase;
  state: OnboardingState;
  step: OnboardingStep;
  target: HTMLElement | null;
  targetGeometry: string;
  title: string;
  description: string;
  hint: string;
  estimate: string;
  remaining: string;
  doneCount: number;
  statusText: string;
  error: string;
  busy: boolean;
  helpOpen: boolean;
  restartOpen: boolean;
  restartBlockedReason: string;
  /** 教程自己的表单或开始弹框保留时，让暂停入口位于其遮罩上方；必要检查仍优先。 */
  resumeAboveDialog: boolean;
  formSaving: boolean;
  blocked: boolean;
  storageUnavailable: boolean;
  availablePlatforms: string[];
  platformsLoading: boolean;
  celebration: OnboardingCelebration | null;
  actions: OnboardingActions;
};

type StepCopy = { title: string; description: string; hint: string; estimate: string };
// 教程与普通岗位表单共用费用和适用场景，避免两处说明不一致。
export const ONBOARDING_FILTER_MODE_OPTIONS = [
  { value: "keyword", label: "关键词筛选（免费模式）", description: "适合单一、简单的条件，按技能词、排除词匹配，不判断复杂要求。基础筛选免费。" },
  { value: "ai", label: "AI 筛选（付费模式）", description: "适合多项、复杂要求，结合经历和岗位要求判断。需有效会员；内置 AI 按用量扣余额，自定义接口按服务商规则计费。", memberOnly: true },
];
const stepCopy: Record<OnboardingStep, StepCopy> = {
  "agent-menu": { title: "先把本地小助手请到岗", description: "点击左侧“组件信息”。GoodHR 的浏览器操作在你的电脑上完成，需要先安装并启动本地程序。", hint: "装过也没关系，连接成功后我会自动跳过。", estimate: "首次安装约 3–8 分钟" },
  "agent-download": { title: "下载并安装本地程序", description: "点击“下载本地程序”，在下载页选择你的电脑系统。安装完成后，双击 GoodHR 图标启动，再回到这里。", hint: "下载页会另外打开，这里的教程进度会留着。", estimate: "约 3–8 分钟，取决于网络" },
  "agent-connect": { title: "我在等本地小助手打卡", description: "安装好了就双击 GoodHR 图标。点击“检查连接”，连接好后会自动往下走，不需要重新注册。", hint: "程序开着仍连不上？看看浏览器是否允许“设备上的应用”。", estimate: "启动后通常约 10–30 秒" },
  "runtime-install": { title: "把必要组件准备好", description: "点击现有弹框的“安装必要组件”。组件负责打开浏览器和读取页面，安装结束后我会自动接着带路。", hint: "如果还有 Windows 运行库提示，按弹框安装或修复后重新检查。", estimate: "约 2–8 分钟，取决于网络" },
  "runtime-system": { title: "先把 Windows 运行库补齐", description: "点击“下载微软运行库”，打开安装包并选择安装；如果已经装过，就选择修复。完成后回来点击“安装或修复后重新检查”。", hint: "运行库检查通过后，再安装剩余必要组件。我会接着带路，进度不用重来。", estimate: "约 1–3 分钟，取决于安装情况" },
  "platform-select": { title: "先去创建第一个岗位", description: "平台会在岗位表单里选择。先点击左侧“岗位管理”，把这次要招的岗位建好。", hint: "岗位保存成功后，我再带你打开对应的招聘平台。", estimate: "约 5 秒" },
  "platform-open": { title: "从岗位“开始”进入准备弹框", description: "点击这个岗位的“开始”，再在弹框里打开对应招聘平台，登录并设置筛选条件。", hint: "这时还不会运行任务，先把平台里的准备工作做好。", estimate: "约 5 秒" },
  "platform-login": { title: "去招聘浏览器登录，再设好筛选条件", description: "切到刚打开的招聘浏览器，登录这个岗位对应的平台账号，再设置年龄、学历、地区等基础筛选条件。完成后回到当前弹框确认开始。", hint: "扫码、验证码需要你亲自完成。没看到窗口？点弹框里的“打开平台，并筛选条件”，或在任务栏、程序坞找橙色狐狸图标。", estimate: "约 1–2 分钟，平台验证可能更久" },
  "position-menu": { title: "现在去创建第一个岗位", description: "点击左侧“岗位管理”。岗位用来告诉我：今天帮你找什么人，以及怎么判断是否合适。", hint: "已经在岗位管理页时，这一步会自动略过。", estimate: "约 5 秒" },
  "position-create": { title: "点击“新建岗位”", description: "点击高亮的新建按钮。教程里先填写最需要的几项，其他参数使用现有默认设置。", hint: "保存后的岗位也能编辑，不用一次把所有参数研究透。", estimate: "整个岗位填写约 2–3 分钟" },
  "position-edit": { title: "回头检查刚才的岗位", description: "点击这个岗位的“编辑”，回到刚才的填写步骤。已经保存的内容会带回来，修改后仍保存到同一个岗位。", hint: "不用再建一份。想从第一步重新听一遍，点顶部“从头开始”。", estimate: "约 5 秒" },
  "position-platform": { title: "这个岗位在哪个平台招人？", description: "选择岗位实际所在的招聘平台。先把岗位建好，保存后我再带你打开招聘浏览器登录。", hint: "猎聘企业端和猎头端要选对；暂未开放的平台不能选择。", estimate: "约 10 秒" },
  "position-name": { title: "把平台里的完整岗位名搬过来", description: "到你选择的招聘平台“岗位管理”复制完整岗位名称，原样粘贴到这里，保持一致，我才能找到对应岗位。", hint: "填好后按回车或按 Tab 离开输入框，会自动引导下一项。", estimate: "约 20–30 秒" },
  "position-filter-mode": { title: "选筛选方式，先把费用说清楚", description: "按条件复杂程度选，简单条件可以先用免费模式跑一遍。", hint: "想先免费跑通，可选关键词 + OCR / DOM；下一步若选 AI 详情，仍有 AI 调用费用。", estimate: "约 10–20 秒" },
  "position-detail-mode": { title: "选择简历详情识别方式", description: "选择打开候选人详情后如何读取内容。OCR / DOM 免费；AI 详情是付费能力，需要有效会员，并按 AI 配置计费。", hint: "关键词配 OCR 或 DOM 可以先免费跑通；选择 AI 详情还需要填写岗位要求。", estimate: "约 10–20 秒" },
  "position-requirement": { title: "把岗位要求告诉 AI", description: "粘贴真实 JD 或写清经验、技能、学历等要求。写能从简历里判断的条件，AI 才有明确标准。", hint: "填好后点一下这段说明，或按键盘 Tab 离开文本框，我就接着带路。", estimate: "约 1–2 分钟" },
  "position-keywords": { title: "写下需要命中的关键词", description: "输入能体现岗位要求的关键词，比如实际技能或相关经验。按字段说明分隔多个词。", hint: "填好后点一下这段说明，或按键盘 Tab 离开输入框，我就接着带路。", estimate: "约 30–60 秒" },
  "position-save": { title: "把这个岗位存下来", description: "点击“创建岗位”。保存成功后，我会带你点这个岗位的“开始”，在弹框里准备平台登录和筛选条件。", hint: "如果缺少必填内容，按表单提示补好即可，之前的步骤不用重来。", estimate: "约 5–10 秒" },
  "position-start": { title: "岗位存好了，点这里“开始”", description: "点击这个岗位的“开始”，打开准备弹框。下一步在弹框里打开对应平台、登录并设置基础筛选条件。", hint: "这一步只打开确认弹框，确认准备好后才会运行。", estimate: "约 5 秒" },
  "platform-filter": { title: "从这个弹框打开招聘平台", description: "点击“打开平台，并筛选条件”。在打开的招聘浏览器里登录账号，并设置年龄、学历、地区等基础筛选条件。", hint: "准备好后回到这个弹框，再确认开始；这些平台条件需要你亲自设置。", estimate: "约 1–2 分钟" },
  "position-run": { title: "登录已确认，筛选条件也设好了吗？", description: "请确认你已在招聘平台设置好年龄、学历、地区等条件，再点“我已筛选好，立即开始”。任务开始后会按岗位设置筛选，匹配时会打招呼。", hint: "我能检查登录，平台里的筛选条件还得请你确认；没设好可以先回招聘浏览器调整。", estimate: "约 10–30 秒" },
  "position-start-check": { title: "启动前，先让我检查一下", description: "正在打开平台或检查登录、会员、余额和本地程序。请留意当前弹框里的进度，检查通过且岗位启动成功后，我才会继续。", hint: "这会儿不用重复点击。想返回的话，点“上一步”，当前检查结束后会安全返回。", estimate: "约 10–30 秒，平台响应可能更久" },
  "position-start-error": { title: "先处理这个提示，再继续开工", description: "岗位还没启动，具体原因就在高亮弹框里。先读提示，再点“我知道了”返回；处理好后可以重新点岗位的“开始”。", hint: "教程进度和已经创建的岗位都会留着。也可以点“稍后继续”，先去处理余额、登录或其他提示。", estimate: "处理好提示后即可重试" },
  "position-start-update": { title: "本地程序得先更新一下", description: "当前版本还不能启动这个岗位。请按高亮弹框提示点击“立即更新”，页面会重新检查版本，并接上现有更新流程。", hint: "岗位还没开跑，进度已经保存；更新并重新连接成功后，再点岗位的“开始”。", estimate: "更新约 1–3 分钟，取决于网络" },
  "first-result": { title: "等第一条真实筛选结果", description: "保持浏览器开着。我会展示当前执行步骤，等到 AI 或关键词真正给出判断，再庆祝跑通。", hint: "判定“不合适”也是正常结果；只有扫描数量增加还不算完成。", estimate: "通常约 1–3 分钟，平台或 AI 可能更久" },
  "complete": { title: "首轮跑通，今天你赢了", description: "从准备本地程序、创建岗位，到平台登录和真实筛选，关键流程你已经走过一遍。接下来可以调整岗位条件，继续使用。", hint: "教程结束不会自动停止正在运行的岗位。", estimate: "已完成" },
};

/** onboardingStepCopy 返回当前小步骤的说明，统一复用在遮罩和帮助弹框里。 */
export function onboardingStepCopy(step: OnboardingStep): StepCopy {
  return stepCopy[step];
}
