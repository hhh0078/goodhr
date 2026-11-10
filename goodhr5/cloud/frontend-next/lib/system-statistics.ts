/** 文件作用：定义全站统计数据、中文指标和安全导出格式，金额展示保留服务器返回的精确字符串。 */
export type StatisticGroup = { name: string; count: number; amount?: string };
export type FunnelStep = { key: string; label: string; count: number };
export type ConsumptionDay = { date: string; amount: string; calls: number };
export type SystemStatistics = {
  start_date: string; end_date: string; as_of: string;
  metrics: Record<string, number>; amounts: Record<string, string>;
  current: Record<string, number>; current_amounts: Record<string, string>;
  funnel: FunnelStep[]; curve: ConsumptionDay[];
  platforms: StatisticGroup[]; models: StatisticGroup[]; sources: StatisticGroup[];
  versions: StatisticGroup[]; errors: StatisticGroup[]; blocked: StatisticGroup[];
  warnings: string[];
};
export const periodOptions = [
  { value: "today", label: "今天" }, { value: "yesterday", label: "昨天" },
  { value: "last7", label: "近7天" }, { value: "last30", label: "近30天" },
  { value: "custom", label: "自定义日期" },
];
export const metricLabels: Record<string, string> = {
  registered: "新注册用户", active_users: "活跃用户", new_positions: "新增岗位",
  active_teams: "活跃团队", screening_users: "使用筛选的用户",
  processed_resumes: "处理简历", greeted: "成功打招呼", new_resumes: "新增入库简历",
  charged_calls: "AI扣费笔数", consumers: "消费用户", prompt_tokens: "输入Token", completion_tokens: "输出Token",
  paid_orders: "成功支付订单", payers: "付款用户", refund_count: "成功退款笔数",
  screenings: "筛选次数", first_done: "初筛完成", first_pass: "初筛通过", second_done: "复筛完成", second_pass: "复筛通过",
  screening_failed: "筛选失败", average_screening_ms: "单轮平均耗时（毫秒）",
  recommendations: "新增猎头推荐", recommendation_updates: "推荐跟进记录", applications: "新增招聘流程",
  interviews: "面试场次", onboardings: "已记录入职", new_customers: "新增猎头客户", new_customer_jobs: "新增客户岗位",
  runtime_errors: "运行异常", affected_users: "异常影响用户",
  retention_1_eligible: "次日留存可观察用户", retention_1_returned: "次日回访用户",
  retention_7_eligible: "7日留存可观察用户", retention_7_returned: "第7日回访用户",
};
export const amountLabels: Record<string, string> = {
  consumption: "AI消费", principal_consumption: "本金消费", bonus_consumption: "赠送余额消费",
  receipts: "实付收款", ai_recharge: "其中AI余额充值", refunds: "成功退款", net_receipts: "净收款",
  gift: "新增赠送余额", admin_increase: "人工增加余额", admin_decrease: "人工扣减余额",
};
export const currentLabels: Record<string, string> = {
  users: "用户总数", positions: "岗位总数", running_positions: "运行中岗位", resumes: "简历库总量", teams: "团队总数", bound_users: "已绑定本地程序的用户",
};
export const currentAmountLabels: Record<string, string> = { balance: "剩余总余额", principal: "剩余本金", bonus: "剩余赠送余额" };
const dimensionLabels: Record<string, string> = {
  boss: "BOSS直聘", liepin: "猎聘", liepin_hr: "猎聘企业端", liepin_headhunter: "猎聘猎头端", zhaopin: "智联招聘", "51job": "前程无忧", job51: "前程无忧",
  automatic: "自动采集", manual: "手动添加", ai_import: "AI导入", browser_extension: "浏览器插件", unknown: "来源未记录",
  agent_detected: "启动本地程序", runtime_ready: "安装运行组件", position_created: "创建岗位", platform_login_verified: "登录招聘平台",
  position_started: "启动岗位", first_resume_processed: "处理首份简历", first_greet_success: "首次打招呼",
  "browser.exit": "浏览器异常退出", "worker.exit": "浏览器工具异常退出", "agent.exit": "本地程序退出", "agent.previous_exit": "上次本地程序异常退出",
};

/** dimensionLabel 将平台和有限事件标识转换为中文，未知值保留原名便于核对。 */
export function dimensionLabel(value: string): string { return dimensionLabels[value] || value || "未记录"; }

/** statisticRate 只在分母大于零时计算比例，缺少样本不会显示成0%。 */
export function statisticRate(numerator: number | undefined, denominator: number | undefined): string {
  return numerator === undefined || !denominator ? "—" : `${(numerator / denominator * 100).toFixed(1)}%`;
}

/** shanghaiDate 返回北京时间自然日，避免操作电脑的系统时区改变默认统计日期。 */
export function shanghaiDate(date = new Date()): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone: "Asia/Shanghai", year: "numeric", month: "2-digit", day: "2-digit" }).format(date);
}

/** statisticsCSV 导出全站汇总及图表数字，明确区分期间、当前快照和缺失值，并防止单元格公式注入。 */
export function statisticsCSV(stats: SystemStatistics): string {
  const rows: Array<Array<string | number>> = [["统计范围", stats.start_date, stats.end_date], ["数据截至", stats.as_of], ["时区", "Asia/Shanghai"], ["分类", "指标", "数值"]];
  for (const [key, label] of Object.entries(metricLabels)) rows.push(["期间汇总", label, stats.metrics[key] ?? "暂无完整记录"]);
  for (const [key, label] of Object.entries(amountLabels)) rows.push(["期间金额（元）", label, stats.amounts[key] ?? "暂无完整记录"]);
  for (const [key, label] of Object.entries(currentLabels)) rows.push(["当前快照", label, stats.current[key] ?? "暂无记录"]);
  for (const [key, label] of Object.entries(currentAmountLabels)) rows.push(["当前余额（元）", label, stats.current_amounts[key] ?? "暂无记录"]);
  for (const step of stats.funnel) rows.push(["所选注册批次漏斗", step.label, step.count]);
  rows.push(["消费曲线日期", "金额（元）", "扣费笔数"]);
  for (const day of stats.curve) rows.push([day.date, day.amount, day.calls]);
  for (const [label, groups] of [["平台筛选", stats.platforms], ["模型消费", stats.models], ["简历来源", stats.sources], ["当前客户端版本", stats.versions], ["运行异常类型", stats.errors], ["新用户卡点", stats.blocked]] as const)
    for (const group of groups) rows.push([label, dimensionLabel(group.name), group.amount ?? group.count]);
  for (const warning of stats.warnings) rows.push(["统计说明", warning]);
  return "\ufeff" + rows.map(csvRow).join("\r\n");
}

/** csvRow 对每个单元格转义引号和公式前缀，不改变服务器的金额精度。 */
function csvRow(row: Array<string | number>): string {
  return row.map(csvCell).join(",");
}

/** csvCell 防止外部模型名称等文本被电子表格当成公式执行。 */
function csvCell(value: string | number): string {
  const text = String(value);
  const safe = /^[\s]*[=+@\t\r]/.test(text) || /^\s*-(?!\d+(?:\.\d+)?$)/.test(text) ? `'${text}` : text;
  return `"${safe.replaceAll('"', '""')}"`;
}
