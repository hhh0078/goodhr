/** 文件作用说明：展示候选人针对岗位的独立招聘进度，安排面试、收集评价、审核、推进和发送邮件。 */
"use client";
import { Alert, Button, Collapse, Descriptions, Empty, Flex, Form, Input, InputNumber, Select, Space, Steps, Table, Tag, Timeline, Typography } from "antd";
import Link from "next/link";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { formatDate } from "@/lib/admin-api";
import { actRecruitmentApplication, loadRecruitmentApplication, loadRecruitmentWorkflows, recruitmentMemberOptions, recruitmentEventText, recruitmentRequest, recruitmentStatusLabel, type RecruitmentApplication, type RecruitmentDelivery, type RecruitmentInterview, type RecruitmentMailTemplate, type RecruitmentMember, type RecruitmentNode } from "@/lib/recruitment-api";
import RecruitmentMailDialog from "./RecruitmentMailDialog";
import CandidateProjection from "./CandidateProjection";

/** localInputDate 将有效日期换成当前浏览器可编辑的本地时间，坏值安全回退为空。 */
function localInputDate(value?: string) {

  const date = value ? new Date(value) : new Date(Date.now() + 86400000);
  if (Number.isNaN(date.getTime())) return "";
  const pad = (number: number) => String(number).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}
/** ApplicationDialog 按服务端权限展示操作，不复写原有推荐与自动任务记录。 */
export default function ApplicationDialog({ applicationID, onClose, onChanged }: { applicationID: string; onClose: () => void; onChanged?: () => void }) {

  const { notify, confirm, user } = useAdmin();
  const [application, setApplication] = useState<RecruitmentApplication | null>(null);
  const [members, setMembers] = useState<RecruitmentMember[]>([]);
  const [templates, setTemplates] = useState<RecruitmentMailTemplate[]>([]);
  const [history, setHistory] = useState<RecruitmentDelivery[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [editor, setEditor] = useState<"schedule" | "feedback" | "approve" | "return" | "end" | "assign" | "entry" | "interviewers" | "">("");
  const [interview, setInterview] = useState<RecruitmentInterview | null>(null);
  const [starts, setStarts] = useState("");
  const [duration, setDuration] = useState(60);
  const [mode, setMode] = useState("video");
  const [location, setLocation] = useState("");
  const [meetingURL, setMeetingURL] = useState("");
  const [interviewers, setInterviewers] = useState<string[]>([]);
  const [content, setContent] = useState("");
  const [decision, setDecision] = useState("pass");
  const [target, setTarget] = useState(0);
  const [entryDate, setEntryDate] = useState("");
  const [assignment, setAssignment] = useState<RecruitmentNode | null>(null);
  const [mailOpen, setMailOpen] = useState(false);
  const [mailInterviewID, setMailInterviewID] = useState("");
  const generation = useRef(0);
  const saving = useRef(false);
  /** load 获取最新流程、人员和邮件结果，旧候选人的晚回包不会落到新对话中。 */

  async function load() {

    if (!applicationID) return;
    const current = ++generation.current;
    setLoading(true);
    setError("");
    const results = await Promise.allSettled([
      loadRecruitmentApplication(applicationID),
      loadRecruitmentWorkflows(),
      recruitmentRequest("/mail-templates")
    ]);
    if (current !== generation.current) return;
    const [detail, people, mail] = results;
    if (detail.status === "fulfilled") setApplication(detail.value); else setError(detail.reason?.message || "招聘进度没读出来");
    if (people.status === "fulfilled") setMembers(people.value.members);
    if (mail.status === "fulfilled") setTemplates(mail.value.templates || []);
    if (detail.status === "fulfilled" && detail.value.permissions?.can_manage) {

      try {

        const deliveries = await recruitmentRequest(`/mail-history?application_id=${encodeURIComponent(applicationID)}`);
        if (current === generation.current) setHistory(deliveries.deliveries || []);
      } catch (cause) {

        if (current === generation.current) notify(cause instanceof Error ? cause.message : "邮件记录没读出来", "warning");
      }
    } else setHistory([]);
    if (current === generation.current) setLoading(false);
  }

  useEffect(() => {

    setApplication(null);
    setEditor("");
    setMailOpen(false);
    void load();
    return () => {

      generation.current++;
    };
  }, [applicationID]);
  /** act 串行提交带版本的操作，冲突时重新读取并保留待填写内容。 */

  async function act(action: string, payload: Record<string, unknown> = {}) {

    if (!application || saving.current) return false;
    saving.current = true;
    setBusy(true);
    try {

      const next = await actRecruitmentApplication(application, action, payload);
      setApplication({

        ...next,
        candidate: application.candidate,
        permissions: { ...next.permissions, can_view_resume: application.permissions.can_view_resume }
      });
      setEditor("");
      notify("这一步已经记好。", "success");
      onChanged?.();
      return true;
    } catch (cause) {

      notify(cause instanceof Error ? cause.message : "这一步没有完成，请再试一次。", "error");
      await load();
      return false;
    } finally {

      saving.current = false;
      setBusy(false);
    }
  }

  const node = application?.nodes?.[application.current_node];
  const permissions = application?.permissions;
  const active = application?.status === "active";
  const peopleOptions = recruitmentMemberOptions(members);
  const unreadNotifications = (application?.events || []).filter((event) => event.notify_emails?.includes(user?.email) && !event.read_by?.includes(user?.email));
  /** openSchedule 初始化新面试或改期草稿，人员始终可从团队选择。 */

  function openSchedule(item?: RecruitmentInterview) {

    setInterview(item || null);
    setStarts(localInputDate(item?.starts_at));
    setDuration(item?.duration_minutes || 60);
    setMode(item?.mode || "video");
    setLocation(item?.location || "");
    setMeetingURL(item?.meeting_url || "");
    setInterviewers(item?.interviewer_emails || node?.interviewer_emails || []);
    setEditor("schedule");
  }
  /** openOpinion 初始化本次评价或审核，避免带入上次操作的结论。 */

  function openOpinion(kind: "feedback" | "approve", item?: RecruitmentInterview) {

    setInterview(item || null);
    setContent("");
    setDecision(kind === "feedback" ? "pass" : "approved");
    setEditor(kind);
  }
  /** submitEditor 根据弹框提交具体动作，缺失时间、评价或原因时保留草稿。 */

  async function submitEditor() {

    if (!application) return;
    if (editor === "schedule") {

      const date = new Date(starts);
      if (!starts || Number.isNaN(date.getTime()) || duration < 5 || !interviewers.length) return notify("先选面试时间和团队面试官。", "warning");
      if (mode === "onsite" && !location.trim()) return notify("现场面试还需要填写地址。", "warning");
      if (mode === "video" && !/^https?:\/\//i.test(meetingURL.trim())) return notify("视频面试需要填写完整的会议链接。", "warning");
      await act(interview ? "reschedule" : "schedule", {

        node_id: node?.id,
        interview_id: interview?.id,
        starts_at: date.toISOString(),
        duration_minutes: duration,
        mode,
        location,
        meeting_url: meetingURL,
        interviewer_emails: interviewers
      });
    } else if (editor === "interviewers") {

      if (!interviewers.length) return notify("至少选择一位团队面试官。", "warning");
      await act("assign_interviewers", { interview_id: interview?.id, interviewer_emails: interviewers });
    } else if (editor === "feedback") {

      if (!content.trim()) return notify("写一句面试评价，后面的同事才好判断。", "warning");
      await act("feedback", { interview_id: interview?.id, conclusion: decision, content: content.trim() });
    } else if (editor === "approve") {

      if (decision === "rejected" && !content.trim()) return notify("写一下未通过的原因吧。", "warning");
      await act("approve", { decision, content: content.trim() });
    } else if (editor === "return" || editor === "end") {

      if (!content.trim()) return notify("先写下原因，我才能把记录说明白。", "warning");
      await act(editor === "return" ? "return" : "end", { target_node_index: target, status: decision, note: content.trim() });
    } else if (editor === "entry") {

      if (!entryDate) return notify("先选入职日期。", "warning");
      await act("set_entry_date", { entry_date: entryDate });
    } else if (editor === "assign" && assignment) await act("assign", {

      node_id: node?.id,
      owner_email: assignment.owner_email,
      interviewer_emails: assignment.interviewer_emails,
      approver_emails: assignment.approver_emails,
      cc_emails: assignment.cc_emails,
      approval_mode: assignment.approval_mode
    });
  }
  /** confirmAction 在取消面试或完成节点前明确确认对应动作。 */

  async function confirmAction(action: string, title: string, description: string, payload: Record<string, unknown> = {}) {

    if (await confirm(title, description)) await act(action, payload);
  }

  return <><AdminDialog
    open={Boolean(applicationID)}
    title={application ? `${application.candidate_name} · ${application.position_name}` : "招聘进度"}
    maxWidth="lg"
    loading={loading}
    onClose={onClose}
    cancelText="关闭">
    {error ? <Alert type="warning" title={error} action={<Button onClick={() => void load()}>重试</Button>} /> : null}
    {application ? <><Descriptions
      size="small"
      column={{ xs: 1, sm: 2, lg: 3 }}
      items={[
        {

          key: "status",
          label: "状态",
          children: <Tag>
            {recruitmentStatusLabel(application.status)}
          </Tag>
        },
        {

          key: "workflow",
          label: "流程",
          children: `${application.workflow_name} · v${application.workflow_version}`
        },
        { key: "owner", label: "招聘负责人", children: application.owner_email || "待分配" },
        { key: "stage", label: "当前节点", children: node?.name || application.stage || "已结束" },
        {

          key: "email",
          label: "候选人邮箱",
          children: application.candidate?.email || "暂无可用邮箱，请先补充简历资料"
        },
        {

          key: "resume",
          label: "简历",
          children: permissions?.can_view_resume ? <Link href={`/admin/resumes/detail?candidate_id=${encodeURIComponent(application.candidate_id)}`}>查看完整简历</Link> : "下方招聘资料可查看"
        }
      ]} />
      <Flex justify="space-between" gap={8} wrap style={{ marginBlock: 8 }}>
        <Typography.Text type="secondary">
          {unreadNotifications.length ? `有 ${unreadNotifications.length} 条发给我的新进度，展开操作记录可查看。` : "我的进度通知已读完。"}
        </Typography.Text>
        {unreadNotifications.length ? <Button size="small" disabled={busy} onClick={() => void act("read_notifications")}>标记通知已读</Button> : null}
      </Flex><Steps
        size="small"
        current={application.current_node}
        style={{ marginBlock: 16 }}
        items={(application.nodes || []).map((item) => ({ title: item.name, status: item.status === "completed" ? "finish" : undefined }))} />
      <Flex gap={8} wrap style={{ marginBottom: 16 }}>
        {active && permissions?.can_schedule && node?.type === "interview" ? <Button type="primary" disabled={busy} onClick={() => openSchedule()}>安排面试</Button> : null}
        {active && permissions?.can_approve ? <Button disabled={busy} onClick={() => openOpinion("approve")}>提交审核</Button> : null}
        {active && permissions?.can_advance ? <Button
          type="primary"
          disabled={busy}
          onClick={() => void confirmAction("advance", "推进招聘节点", `确认完成“${node?.name || application.stage}”并推进吗？`)}>推进下一步</Button> : null}
        {active && permissions?.can_manage ? <>{permissions.can_assign ? <Button
          disabled={busy}
          onClick={() => {

            setAssignment(node ? { ...node } : null);
            setEditor("assign");
          }}>调整节点人员</Button> : null}{application.current_node > 0 ? <Button
            disabled={busy}
            onClick={() => {

              setTarget(application.current_node - 1);
              setContent("");
              setEditor("return");
            }}>退回节点</Button> : null}<Button
              disabled={busy}
              onClick={() => {

                setEntryDate(application.entry_date || "");
                setEditor("entry");
              }}>登记入职日期</Button><Button
                danger
                disabled={busy}
                onClick={() => {

                  setDecision("rejected");
                  setContent("");
                  setEditor("end");
                }}>结束招聘</Button></> : null}
        {(permissions?.can_manage || permissions?.can_schedule || permissions?.can_advance) ? <Button
          disabled={busy || !templates.length}
          onClick={() => {

            setMailInterviewID("");
            setMailOpen(true);
          }}>预览并发送邮件</Button> : null}
      </Flex>
      {node ? <Alert
        type="info"
        title={`负责人：${node.owner_email || application.owner_email || "待分配"}；审核人：${node.approver_emails?.join("、") || (node.type === "approval" ? "负责人审核" : "无需额外审核")}${node.approver_emails?.length ? node.approval_mode === "any" ? "（任一通过）" : "（全部通过）" : ""}；内部抄送：${node.cc_emails?.join("、") || "无"}`}
        style={{ marginBottom: 12 }} /> : null}
      <Collapse
        defaultActiveKey={["interviews"]}
        items={[
          {

            key: "candidate",
            label: "招聘资料",
            children: <CandidateProjection candidate={application.candidate} />
          },
          {

            key: "interviews",
            label: `面试安排 · ${application.interviews?.length || 0}`,
            children: (application.interviews || []).length ? <Flex vertical gap={12}>
              {application.interviews.map((item) => <div
                key={item.id}
                style={{ paddingBottom: 12, borderBottom: "1px solid var(--ant-color-border-secondary, #eee)" }}>
                <Flex justify="space-between" gap={8} wrap>
                  <Space wrap>
                    <Typography.Text strong>
                      {formatDate(item.starts_at)} ·
                      {item.duration_minutes} 分钟
                    </Typography.Text>
                    <Tag>
                      {recruitmentStatusLabel(item.status)}
                    </Tag>
                    <Tag color={item.confirmation === "confirmed" ? "success" : undefined}>
                      {recruitmentStatusLabel(item.confirmation)}
                    </Tag>
                  </Space>
                  <Space wrap>
                    {active && permissions?.can_schedule && item.node_id === node?.id && item.attempt === node?.attempt && item.status !== "cancelled" && item.status !== "completed" ? <><Button size="small" disabled={busy} onClick={() => openSchedule(item)}>改期</Button><Button
                      size="small"
                      disabled={busy || new Date(item.starts_at).getTime() > Date.now()}
                      title="面试开始后才可登记完成"
                      onClick={() => void confirmAction("complete_interview", "登记面试完成", "确认这场面试已经完成吗？候选人确认参加和实际面试完成会分开记录。", { interview_id: item.id })}>完成面试</Button><Button
                        size="small"
                        danger
                        disabled={busy}
                        onClick={() => void confirmAction("cancel_interview", "取消面试", "取消这场面试后，旧确认链接将不能继续使用。需要另发取消通知时，可以选择取消邮件模板。", { interview_id: item.id, note: "HR 取消面试" })}>取消面试</Button></> : null}
                    {active && permissions?.can_manage && item.node_id === node?.id && item.attempt === node?.attempt && item.status !== "cancelled" ? <Button
                      size="small"
                      disabled={busy}
                      onClick={() => {

                        setInterview(item);
                        setInterviewers(item.interviewer_emails || []);
                        setEditor("interviewers");
                      }}>转交面试官</Button> : null}
                    {active && permissions?.can_feedback && item.node_id === node?.id && item.attempt === node?.attempt && item.interviewer_emails?.includes(user?.email) && item.status !== "cancelled" ? <Button
                      size="small"
                      disabled={busy || new Date(item.starts_at).getTime() > Date.now()}
                      title="面试开始后才可填写评价"
                      onClick={() => openOpinion("feedback", item)}>写面试评价</Button> : null}
                    {(permissions?.can_manage || permissions?.can_schedule) ? <Button
                      size="small"
                      disabled={busy || !templates.length}
                      onClick={() => {

                        setMailInterviewID(item.id);
                        setMailOpen(true);
                      }}>发送通知</Button> : null}
                  </Space>
                </Flex>
                <Typography.Paragraph type="secondary" style={{ marginBlock: 6 }}>面试官：
                  {item.interviewer_emails?.join("、") || "未分配"} ·
                  {item.mode === "onsite" ? `现场：${item.location}` : item.mode === "phone" ? "电话面试" : `视频：${item.meeting_url || "待补充"}`}
                </Typography.Paragraph>
                {(item.feedback || []).map((feedback, index) => <Typography.Paragraph key={index} style={{ marginBottom: 4, whiteSpace: "pre-wrap" }}>
                  {feedback.author_email} ·
                  {recruitmentStatusLabel(feedback.conclusion)}：
                  {feedback.content}
                </Typography.Paragraph>)}
              </div>)}
            </Flex> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="还没安排面试。时间确认后，再给候选人发邀请。" />
          },

          {

            key: "approvals",
            label: "节点审核",
            children: <Flex vertical gap={8}>
              {application.nodes.flatMap((item) => (item.approvals || []).map((approval, index) => <Typography.Text key={`${item.id}-${index}`}>
                {item.name} ·
                {approval.author_email} ·
                {recruitmentStatusLabel(approval.decision)}
                {approval.content}
              </Typography.Text>))}
              {!application.nodes.some((item) => item.approvals?.length) ? <Typography.Text type="secondary">还没有审核记录。</Typography.Text> : null}
            </Flex>
          },

          {

            key: "mail",
            label: `邮件记录 · ${history.length}`,
            children: <Table
              size="small"
              rowKey="id"
              dataSource={history}
              pagination={false}
              scroll={{ x: 680 }}
              columns={[
                { title: "时间", dataIndex: "created_at", render: (value) => formatDate(value), width: 170 },
                { title: "收件人", dataIndex: "recipient" },
                { title: "标题", dataIndex: "subject", ellipsis: true },
                {

                  title: "发送结果",
                  dataIndex: "status",
                  render: (value, row) => <Space direction="vertical" size={0}>
                    <Tag color={value === "sent" ? "success" : value === "failed" ? "error" : "warning"}>
                      {recruitmentStatusLabel(value)}
                    </Tag>
                    {row.error_message ? <Typography.Text type="secondary">
                      {row.error_message}
                    </Typography.Text> : null}
                  </Space>
                }
              ]} />
          },

          {

            key: "history",
            label: `操作记录 · ${application.events?.length || 0}`,
            children: <Timeline
              items={(application.events || []).map((event) => ({

                children: <><Typography.Text>
                  {recruitmentEventText(event)}
                </Typography.Text><div>
                    <Typography.Text type="secondary">
                      {event.author_email === "candidate" ? "候选人" : event.author_email} ·
                      {formatDate(event.created_at || "")}
                      {event.notify_emails?.length ? ` · 通知：${event.notify_emails.join("、")}` : ""}
                    </Typography.Text>
                  </div></>
              }))} />
          }
        ]} />
    </> : null}
  </AdminDialog>
    <AdminDialog
      open={Boolean(editor)}
      title={({

        schedule: interview ? "调整面试安排" : "安排面试",
        feedback: "面试评价",
        approve: "节点审核",
        return: "退回节点",
        end: "结束招聘",
        assign: "调整当前节点人员",
        entry: "登记入职日期",
        interviewers: "转交面试官"
      } as Record<string, string>)[editor] || "招聘操作"}
      maxWidth="sm"
      loading={busy}
      onClose={() => setEditor("")}
      onConfirm={() => void submitEditor()}>

      <Form layout="vertical">
        {editor === "schedule" ? <><Form.Item label="面试时间" required>
          <Input
            aria-label="面试时间"
            type="datetime-local"
            value={starts}
            onChange={(event) => setStarts(event.target.value)} />
        </Form.Item><Flex gap={12}>
            <Form.Item label="时长（分钟）" style={{ flex: 1 }}>
              <InputNumber
                aria-label="面试时长"
                min={5}
                max={480}
                value={duration}
                onChange={(value) => setDuration(value || 60)}
                style={{ width: "100%" }} />
            </Form.Item>
            <Form.Item label="面试方式" style={{ flex: 1 }}>
              <Select
                aria-label="面试方式"
                value={mode}
                onChange={setMode}
                options={[{ value: "video", label: "视频" }, { value: "onsite", label: "现场" }, { value: "phone", label: "电话" }]} />
            </Form.Item>
          </Flex><Form.Item label="面试官" required>
            <Select
              aria-label="面试安排面试官"
              mode="multiple"
              showSearch
              optionFilterProp="label"
              value={interviewers}
              onChange={setInterviewers}
              options={peopleOptions} />
          </Form.Item>{mode === "onsite" ? <Form.Item label="面试地址" required>
            <Input aria-label="面试地址" value={location} onChange={(event) => setLocation(event.target.value)} />
          </Form.Item> : null}{mode === "video" ? <Form.Item label="会议链接" required>
            <Input
              aria-label="会议链接"
              value={meetingURL}
              onChange={(event) => setMeetingURL(event.target.value)}
              placeholder="https://" />
          </Form.Item> : null}<Alert type="info" title="保存安排后，再预览并发送邀请。改期后需要发新的通知。" /></> : null}

        {editor === "feedback" || editor === "approve" || editor === "end" ? <Form.Item label={editor === "end" ? "结束原因类型" : "结论"}>
          <Select
            aria-label="招聘操作结论"
            value={decision}
            onChange={setDecision}
            options={editor === "feedback" ? [{ value: "pass", label: "通过" }, { value: "fail", label: "不通过" }, { value: "hold", label: "待定" }] : editor === "approve" ? [{ value: "approved", label: "通过" }, { value: "rejected", label: "未通过" }] : [{ value: "rejected", label: "公司暂不推进" }, { value: "withdrawn", label: "候选人退出" }]} />
        </Form.Item> : null}

        {editor === "return" ? <Form.Item label="退回到">
          <Select
            aria-label="退回目标节点"
            showSearch
            optionFilterProp="label"
            value={target}
            onChange={setTarget}
            options={(application?.nodes || []).slice(0, application?.current_node || 0).map((item, index) => ({ value: index, label: item.name }))} />
        </Form.Item> : null}

        {["feedback", "approve", "return", "end"].includes(editor) ? <Form.Item
          label={editor === "feedback" ? "面试评价" : "说明"}
          required={editor !== "approve" || decision === "rejected"}>
          <Input.TextArea
            aria-label="招聘操作说明"
            value={content}
            onChange={(event) => setContent(event.target.value)}
            maxLength={editor === "return" || editor === "end" ? 2000 : 4000}
            autoSize={{ minRows: 3, maxRows: 8 }} />
        </Form.Item> : null}

        {editor === "interviewers" ? <><Form.Item label="接手的面试官" required>
          <Select
            aria-label="接手面试官"
            mode="multiple"
            showSearch
            optionFilterProp="label"
            value={interviewers}
            onChange={setInterviewers}
            options={peopleOptions} />
        </Form.Item><Alert type="info" title="原有面评会保留。转交后按新人员检查面评，旧邀请链接会失效，需要重新发送通知。" /></> : null}
        {editor === "entry" ? <Form.Item label="计划入职日期" required>
          <Input
            aria-label="计划入职日期"
            type="date"
            value={entryDate}
            onChange={(event) => setEntryDate(event.target.value)} />
        </Form.Item> : null}

        {editor === "assign" && assignment ? <>{([
          { key: "owner_email", label: "负责人", multiple: false },
          { key: "interviewer_emails", label: "面试官", multiple: true },
          { key: "approver_emails", label: "审核人", multiple: true },
          { key: "cc_emails", label: "内部抄送人", multiple: true }
        ] as const).map((field) => <Form.Item key={field.key} label={field.label}>
          <Select
            aria-label={`当前节点${field.label}`}
            mode={field.multiple ? "multiple" : undefined}
            showSearch
            allowClear
            optionFilterProp="label"
            options={peopleOptions}
            value={assignment[field.key] || (field.multiple ? [] : undefined)}
            onChange={(value) => setAssignment((current) => current ? { ...current, [field.key]: value || (field.multiple ? [] : "") } : null)} />
        </Form.Item>)}<Form.Item label="审核方式">
            <Select
              aria-label="当前节点审核方式"
              value={assignment.approval_mode}
              options={[{ value: "all", label: "所有审核人通过" }, { value: "any", label: "任一审核人通过" }]}
              onChange={(value) => setAssignment((current) => current ? { ...current, approval_mode: value } : null)} />
          </Form.Item></> : null}

      </Form>

    </AdminDialog>{application ? <RecruitmentMailDialog
      open={mailOpen}
      application={application}
      templates={templates}
      history={history}
      initialInterviewID={mailInterviewID}
      onClose={() => setMailOpen(false)}
      onSent={() => {

        void load();
        onChanged?.();
      }} /> : null}
  </>;
}
