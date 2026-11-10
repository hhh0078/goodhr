/** 文件作用：在岗位日志和分享页中展示同一条浏览器错误的列表进度、请求状态与运行环境。 */
"use client";

import { Collapse, Flex, Typography } from "antd";

type Fields = Record<string, unknown>;
type DiagnosticRow = { key: string; label: string; value: string };
type DiagnosticGroup = { title: string; rows: DiagnosticRow[] };

const listLabels: Record<string, string> = {
  platform: "招聘平台", operation: "列表操作", stage: "失败时的步骤", last_success_stage: "上次完成的步骤",
  elapsed_ms: "列表操作耗时", stage_elapsed_ms: "当前步骤耗时", scope_budget_ms: "步骤时限", scope_remaining_ms: "步骤剩余时间", parent_status: "上层任务状态",
  selector_key: "定位项", batch_start_index: "批次起始下标", max_items: "本批最多读取", read_concurrency: "并发读取数",
  window_phase: "窗口采样时点", total_matches: "页面匹配卡片数", before_total_matches: "滚动前匹配卡片数", after_total_matches: "滚动后匹配卡片数",
  field_count: "每张卡片字段数", cards_read: "已读取卡片数", missing_names: "缺少姓名数", read_attempt: "读取轮次",
  loading_visible: "可见加载提示", empty_visible: "可见空列表提示", first_card_count: "首次检查的卡片数",
  before_count: "推进前卡片数", next_index: "下一张卡片下标", scroll_round: "滚动轮次", poll_round: "检查轮次",
  tail_visible: "尾部锚点可见", tail_moved: "尾部锚点已移动", bottom_rounds: "触底轮次", blocked_rounds: "未推进轮次",
  new_cards: "新增卡片数", head_changed: "首张卡片变化",
};
const requestLabels: Record<string, string> = {
  request_id: "请求编号", action: "浏览器动作", phase: "请求中断位置", reason: "请求结果",
  error_code: "错误码", worker_step: "执行端报错步骤", original_error: "底层错误",
  elapsed_ms: "请求耗时", go_timeout_ms: "本地请求时限", worker_timeout_ms: "浏览器操作时限",
  read_value_timeout_ms: "已定位元素取值时限",
  parent_remaining_ms: "发起请求时上层剩余时间", response_received: "收到响应头", http_status: "HTTP 状态",
  connection_acquired: "已取得连接", request_written: "请求已写入连接",
  worker_reachable: "执行端有响应", worker_phase: "执行端最后观测步骤", progress_age_ms: "距最后进度",
  item_index: "最后观测卡片下标（从 0 起）", item_count: "卡片数", field_key: "最后观测字段",
  queue_pending: "未结束请求数（含执行中）", queue_blocked: "队列被阻塞", active_action: "正在执行的动作", active_request_id: "正在执行的请求编号",
  page_closed: "页面已关闭", browser_connected: "浏览器仍连接", frame_count: "页面框架数", client_version: "请求端版本", worker_version: "执行端版本",
  node_version: "Node.js 版本", browser_version: "浏览器版本",
};
const environmentLabels = { agent_version: "本地程序版本", os: "操作系统", arch: "处理器架构" };
const valueLabels: Record<string, string> = {
  before_scroll: "滚动前", after_scroll: "滚动后",
  "context canceled": "已取消", "context deadline exceeded": "已超时",
  name: "姓名", card_text: "卡片文本", basic_info: "基本信息", education: "学历",
  university: "毕业院校", description: "描述", platform_candidate_id: "平台候选人编号",
  "51job": "前程无忧", windows: "Windows", darwin: "macOS",
  find_candidates: "查找候选人", advance_candidate_list: "推进候选人列表",
  before_request: "发送请求之前", WORKER_REQUEST_NOT_SENT: "请求未发出",
  connect_worker: "连接浏览器执行端", send_request: "发送请求", list_resolved: "已定位列表",
  worker_deadline: "执行端操作超时", request_cancelled: "请求已取消", client_disconnected: "请求端连接断开",
  native_input_deadline: "系统输入操作超时", queue_blocked_by_prior_request: "队列被前一个请求阻塞",
  wait_response_headers: "等待响应头", read_response_body: "读取响应内容", worker_response: "处理执行端响应",
  parent_cancelled: "上层任务取消", parent_deadline: "上层任务超时", request_deadline: "本地请求超时",
  worker_error: "执行端返回错误", connection_refused: "连接被拒绝", connection_reset: "连接被重置",
  connection_closed: "连接已关闭", transport_error: "传输错误", invalid_response: "响应格式有误",
  received: "已收到请求", queued: "排队中", executing: "执行中", require_page: "获取页面",
  resolve_list: "定位列表", read_card: "读取卡片", locate_field: "定位字段", read_field: "读取字段",
  read_value_timeout: "读取已定位元素的内容超时", playwright_timeout: "页面操作等待超时",
  locate_read_target: "定位要读取的元素", read_value: "已定位元素，正在读取内容",
  screenshot_prepare: "准备截图区域", screenshot_capture: "截取页面画面", screenshot_top: "滚动到详情顶部",
  screenshot_save: "保存截图分段", screenshot_advance: "滚动到下一段",
  scroll_target: "定位滚动区域", scroll_anchor: "定位滚动锚点", scroll_move: "移动鼠标",
  scroll_wheel: "滚动列表", scroll_verify: "检查滚动结果",
};

/** asFields 仅接受普通对象，兼容旧日志中缺失或无效的诊断字段。 */
function asFields(value: unknown): Fields {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Fields : {};
}

/** rowsFor 按已知字段提取能展示的证据，保留数字零和布尔假。 */
function rowsFor(fields: Fields, labels: Record<string, string>): DiagnosticRow[] {
  return Object.entries(labels).flatMap(([key, label]) => {
    const raw = fields[key];
    let value: string;
    if (key === "worker_reachable" && raw === false) value = "状态探测未收到有效响应";
    else if (typeof raw === "boolean") value = raw ? "是" : "否";
    else if (typeof raw === "number" && Number.isFinite(raw)) {
      value = String(raw);
      if (key.endsWith("_ms")) value = raw >= 1000 ? `${raw / 1000} 秒（${raw} ms）` : `${raw} ms`;
    }
    else if (typeof raw === "string" && raw.trim()) value = Object.hasOwn(valueLabels, raw) ? `${valueLabels[raw]}（${raw}）` : raw;
    else return [];
    return [{ key, label, value }];
  });
}

/** browserDiagnosticFromOutput 从已脱敏出参取出摘要、分组详情和按时间记录的最近步骤。 */
function browserDiagnosticFromOutput(output: unknown) {
  const raw = asFields(output).browser_diagnostic;
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const fields = asFields(raw);
  const list = asFields(fields.list);
  const request = asFields(fields.request);
  const requestRows = rowsFor(request, requestLabels);
  if (requestRows.length && !requestRows.some(row => row.key === "worker_phase") && !(Array.isArray(request.pending_reads) && request.pending_reads.length)) {
    requestRows.push({ key: "worker_phase", label: requestLabels.worker_phase, value: "未收到本次请求的执行进度" });
  }
  const pending = Array.isArray(request.pending_reads) ? request.pending_reads.slice(0, 4) : [];
  const pendingRows = pending.flatMap((read, index) => {
    const rows = rowsFor(asFields(read), {
      worker_phase: "执行步骤", item_index: "卡片下标（从 0 起）", field_key: "字段", elapsed_ms: "已耗时",
    });
    return rows.length ? [{ key: String(index), label: `读取 ${index + 1}`, value: rows.map(row => `${row.label}：${row.value}`).join(" · ") }] : [];
  });
  const groups: DiagnosticGroup[] = [
    { title: "候选人列表", rows: rowsFor(list, listLabels) },
    { title: "浏览器请求", rows: requestRows },
    { title: "采样时仍未返回的读取", rows: pendingRows },
    { title: "运行环境", rows: rowsFor(fields, environmentLabels) },
  ].filter(group => group.rows.length);
  const timeline = Array.isArray(list.timeline) ? list.timeline.filter((line): line is string => typeof line === "string" && Boolean(line.trim())) : [];
  if (!groups.length && !timeline.length) return null;
  const summary = [
    ...rowsFor(list, { stage: listLabels.stage, last_success_stage: listLabels.last_success_stage }),
    ...rowsFor(request, { reason: requestLabels.reason, worker_phase: requestLabels.worker_phase }),
  ].map(row => `${row.label}：${row.value.replace(/（[a-z][a-z_]*）$/, "")}`).join(" · ");
  return { groups, timeline, summary };
}

/** browserDiagnosticText 将各项证据和最近步骤转成能搜索、能复制的文字。 */
export function browserDiagnosticText(output: unknown): string {
  const diagnostic = browserDiagnosticFromOutput(output);
  if (!diagnostic) return "";
  return [
    ...diagnostic.groups.flatMap(group => [group.title, ...group.rows.map(row => `${row.label}：${row.value}`)]),
    ...diagnostic.timeline,
  ].join("\n");
}

/** BrowserDiagnosticDetails 直接展示错误摘要，展开后按来源逐项显示实际采集到的证据。 */
export default function BrowserDiagnosticDetails({ output }: { output: unknown }) {
  const diagnostic = browserDiagnosticFromOutput(output);
  if (!diagnostic) return null;
  return (
    <Flex vertical gap={4} style={{ minWidth: 0, overflowWrap: "anywhere" }}>
      {diagnostic.summary && <Typography.Text>{diagnostic.summary}</Typography.Text>}
      <Collapse ghost size="small" items={[{
        key: "browser", label: "查看浏览器排障详情",
        children: <Flex vertical gap={12}>
          {diagnostic.timeline.length > 0 && <div>
            <Typography.Text strong>最近执行步骤</Typography.Text>
            <ol style={{ margin: "4px 0 0", paddingInlineStart: 22 }}>
              {diagnostic.timeline.map((line, index) => <li key={index}>{line}</li>)}
            </ol>
          </div>}
          {diagnostic.groups.map(group => <div key={group.title}>
            <Typography.Text strong>{group.title}</Typography.Text>
            <dl style={{ margin: "4px 0 0", display: "grid", gridTemplateColumns: "minmax(0, min(9.5em, 42%)) minmax(0, 1fr)", gap: "4px 8px" }}>
              {group.rows.map(row => <div key={row.key} style={{ display: "contents" }}>
                <dt>{row.label}</dt><dd style={{ margin: 0 }}>{row.value}</dd>
              </div>)}
            </dl>
          </div>)}
        </Flex>,
      }]} />
    </Flex>
  );
}
