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
  openHelp: () => void;
  closeHelp: () => void;
  choosePlatform: (platformId: string) => void;
  openPlatform: () => void;
  downloadAgent: () => void;
  checkAgent: () => void;
  resume: () => void;
  finishClose: () => void;
  dismissCelebration: () => void;
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
  blocked: boolean;
  storageUnavailable: boolean;
  celebration: OnboardingCelebration | null;
  actions: OnboardingActions;
};

type StepCopy = { title: string; description: string; hint: string; estimate: string };
const stepCopy: Record<OnboardingStep, StepCopy> = {
  "agent-menu": { title: "先把本地小助手请到岗", description: "点击左侧“组件信息”。GoodHR 的浏览器操作在你的电脑上完成，需要先安装并启动本地程序。", hint: "装过也没关系，连接成功后我会自动跳过。", estimate: "首次安装约 3–8 分钟" },
  "agent-download": { title: "下载并安装本地程序", description: "点击“下载本地程序”，在下载页选择你的电脑系统。安装完成后，双击 GoodHR 图标启动，再回到这里。", hint: "下载页会另外打开，这里的教程进度会留着。", estimate: "约 3–8 分钟，取决于网络" },
  "agent-connect": { title: "我在等本地小助手打卡", description: "安装好了就双击 GoodHR 图标。点击“检查连接”，连接好后会自动往下走，不需要重新注册。", hint: "程序开着仍连不上？看看浏览器是否允许“设备上的应用”。", estimate: "启动后通常约 10–30 秒" },
  "runtime-install": { title: "把必要组件准备好", description: "点击现有弹框的“安装必要组件”。组件负责打开浏览器和读取页面，安装结束后我会自动接着带路。", hint: "如果还有 Windows 运行库提示，按弹框安装或修复后重新检查。", estimate: "约 2–8 分钟，取决于网络" },
  "platform-select": { title: "今天在哪个平台招人？", description: "选择你正在使用的招聘平台。我会直接打开对应平台，不用你在浏览器里到处找入口。", hint: "选择与你的招聘账号对应的平台端。", estimate: "约 10 秒" },
  "platform-open": { title: "打开招聘平台", description: "点击“打开招聘平台”，切换到本地程序打开的浏览器，在那里扫码或输入验证码登录。", hint: "要在 GoodHR 打开的浏览器里登录，才能接着运行。", estimate: "约 1–2 分钟" },
  "platform-login": { title: "请在招聘浏览器里登录", description: "完成平台登录后回到 GoodHR。我会沿用现有登录检测，确认通过后自动进入创建岗位。", hint: "登录和验证码由你亲自处理，我在这里等你。", estimate: "约 1–2 分钟，平台验证可能更久" },
  "position-menu": { title: "现在去创建第一个岗位", description: "点击左侧“岗位管理”。岗位用来告诉我：今天帮你找什么人，以及怎么判断是否合适。", hint: "已经在岗位管理页时，这一步会自动略过。", estimate: "约 5 秒" },
  "position-create": { title: "点击“新建岗位”", description: "点击高亮的新建按钮。教程里先填写最需要的几项，其他参数使用现有默认设置。", hint: "保存后的岗位也能编辑，不用一次把所有参数研究透。", estimate: "整个岗位填写约 2–3 分钟" },
  "position-name": { title: "先填岗位名称", description: "把招聘平台上的岗位名称原样复制过来，保持一致，我才能找到对应岗位。", hint: "填好后按回车或按 Tab 离开输入框，会自动引导下一项。", estimate: "约 20–30 秒" },
  "position-filter-mode": { title: "选择第一轮筛选方式", description: "关键词按你给的词快速判断；AI 会按岗位要求分析基础信息。点击你想用的方式。", hint: "AI 需要有效会员；使用内置 AI 还需要可用余额。关键词也可以先跑通。", estimate: "约 10–20 秒" },
  "position-detail-mode": { title: "选择简历详情识别方式", description: "选择打开候选人详情后如何读取内容。当前平台可用的方式会显示在这里，按说明选一项。", hint: "关键词配 OCR 或 DOM 可避免内置 AI 余额门槛；选择 AI 详情仍需要岗位要求。", estimate: "约 10–20 秒" },
  "position-requirement": { title: "把岗位要求告诉 AI", description: "粘贴真实 JD 或写清经验、技能、学历等要求。写能从简历里判断的条件，AI 才有明确标准。", hint: "填好后点一下这段说明，或按键盘 Tab 离开文本框，我就接着带路。", estimate: "约 1–2 分钟" },
  "position-keywords": { title: "写下需要命中的关键词", description: "输入能体现岗位要求的关键词，比如实际技能或相关经验。按字段说明分隔多个词。", hint: "填好后点一下这段说明，或按键盘 Tab 离开输入框，我就接着带路。", estimate: "约 30–60 秒" },
  "position-save": { title: "把这个岗位存下来", description: "点击“创建岗位”。我会等待实际保存成功，再把这一大步记成完成。", hint: "如果缺少必填内容，按表单提示补好即可，之前的步骤不用重来。", estimate: "约 5–10 秒" },
  "position-start": { title: "准备第一次运行", description: "点击刚创建岗位的“开始”。先查看启动确认，再到招聘平台设置基础筛选条件。", hint: "只会突出你的教程岗位，不会替你启动其他岗位。", estimate: "约 5 秒" },
  "platform-filter": { title: "先在平台选好筛选条件", description: "点击“打开平台，并筛选条件”，到招聘浏览器里设置地区、学历、经验等条件，再回到这里。", hint: "这些条件是在招聘平台中设置，我不会往平台页面添加遮罩或脚本。", estimate: "约 1–2 分钟" },
  "position-run": { title: "确认筛选后，开始运行", description: "确认你已经在招聘平台设置好条件，再点击“我已筛选好，立即开始”。开始后会按岗位设置筛选，匹配时会打招呼。", hint: "如果启动检查没通过，我会留在这里；按提示修复后可以再试。", estimate: "约 10–30 秒" },
  "first-result": { title: "等第一条真实筛选结果", description: "保持浏览器开着。我会展示当前执行步骤，等到 AI 或关键词真正给出判断，再庆祝跑通。", hint: "判定“不合适”也是正常结果；只有扫描数量增加还不算完成。", estimate: "通常约 1–3 分钟，平台或 AI 可能更久" },
  "complete": { title: "首轮跑通，今天你赢了", description: "从安装、登录到真实筛选，关键流程你已经走过一遍。接下来可以调整岗位条件，继续使用。", hint: "教程结束不会自动停止正在运行的岗位。", estimate: "已完成" },
};

/** onboardingStepCopy 返回当前小步骤的说明，统一复用在遮罩和帮助弹框里。 */
export function onboardingStepCopy(step: OnboardingStep): StepCopy {
  return stepCopy[step];
}
