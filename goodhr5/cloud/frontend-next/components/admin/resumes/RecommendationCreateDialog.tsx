/** 文件作用说明：从已有真实简历和开启岗位新增猎头推荐，复用原推荐接口并保护提交重试。 */
"use client";
import { Alert, Form, Input, Select } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { cloudRequest } from "@/lib/admin-api";
import { addRecommendation, loadResumePositions } from "@/lib/resume-api";
import { isReadOnlyResume, recommendationStages, type PositionOption, type Resume } from "@/lib/resume-types";

/** RecommendationCreateDialog 仅选择已有候选人和真实岗位，不新建简历或启动自动任务。 */
export default function RecommendationCreateDialog({ open, onClose, onSaved }: { open: boolean; onClose: () => void; onSaved: () => void }) {
  const { notify } = useAdmin();
  const [form] = Form.useForm<{ candidate_id: string; position_id: string; stage: string; note: string }>();
  const [candidates, setCandidates] = useState<Resume[]>([]);
  const [positions, setPositions] = useState<PositionOption[]>([]);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const generation = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saveKey = useRef("");
  const saving = useRef(false);
  /** searchCandidates 搜索可访问的真实简历，旧请求返回时不会替换新选择列表。 */
  async function searchCandidates(q = "") {
    const current = ++generation.current; setLoading(true);
    try {
      const data = await cloudRequest(`/api/candidates?${new URLSearchParams({ q, page: "1", page_size: "50" })}`);
      if (current === generation.current) setCandidates((data.candidates || []).filter((candidate: Resume) => !isReadOnlyResume(candidate)));
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "候选人没读出来，请重新搜索。"); }
    finally { if (current === generation.current) setLoading(false); }
  }
  useEffect(() => {
    if (!open) return;
    form.resetFields(); form.setFieldsValue({ stage: "recommended", note: "" });
    setError(""); setCandidates([]); setPositions([]); saveKey.current = crypto.randomUUID();
    void searchCandidates();
    let active = true;
    void loadResumePositions().then((items) => { if (active) setPositions(items); }).catch((cause) => { if (active) setError(cause.message || "岗位没读出来，请关闭后重试。"); });
    return () => { active = false; generation.current += 1; if (timer.current) clearTimeout(timer.current); };
  }, [open, form]);
  /** queueSearch 合并连续输入的查询，候选人必须从搜索结果中明确选中。 */
  function queueSearch(text: string) {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => { setError(""); void searchCandidates(text); }, 250);
  }
  /** saveRecommendation 显式创建或推进关联，忙时防止重复提交，失败保留草稿和请求编号。 */
  async function saveRecommendation() {
    if (saving.current) return;
    try {
      const values = await form.validateFields();
      if (!positions.some((position) => position.id === values.position_id)) { setError("这个岗位当前不可推荐，换一个开启的岗位吧。"); return; }
      saving.current = true; setBusy(true); setError("");
      await addRecommendation(values.candidate_id, values.position_id, values.stage, values.note?.trim() || "", saveKey.current);
      notify("推荐记好了，候选人和岗位都还在原处。", "success"); onSaved(); onClose();
    } catch (cause) { if (!(cause && typeof cause === "object" && "errorFields" in cause)) setError(cause instanceof Error ? cause.message : "推荐没保存成功，请重试。"); }
    finally { saving.current = false; setBusy(false); }
  }
  return <AdminDialog open={open} title="新增猎头推荐" maxWidth="sm" description="先选简历，再选开启的岗位；这里只记录推荐和跟进。"
    onClose={onClose} onConfirm={() => void saveRecommendation()} loading={busy} confirmText="保存推荐">
    {error ? <Alert type="warning" showIcon title={error} style={{ marginBottom: 10 }} /> : null}
    <Form form={form} layout="vertical" disabled={busy} onValuesChange={() => { saveKey.current = crypto.randomUUID(); }}>
      <Form.Item name="candidate_id" label="候选人" rules={[{ required: true, message: "先选一位已有候选人，我再记推荐。" }]}><Select showSearch filterOption={false} loading={loading} onSearch={queueSearch} placeholder="搜索姓名、手机号、公司或职位" options={candidates.map((candidate) => ({ value: candidate.id, label: `${candidate.candidate_name || "未命名候选人"} · ${candidate.phone || "手机号待补充"}` }))} notFoundContent={loading ? "正在搜索" : "暂时没有匹配简历，可以换个词试试。"} /></Form.Item>
      <Form.Item name="position_id" label="推荐岗位" rules={[{ required: true, message: "选一个开启的岗位就好。" }]}><Select showSearch optionFilterProp="label" placeholder="选择开启的岗位" options={positions.map((position) => ({ value: position.id, label: position.name }))} /></Form.Item>
      <Form.Item name="stage" label="推荐阶段" rules={[{ required: true }]}><Select options={recommendationStages} /></Form.Item>
      <Form.Item name="note" label="本次备注"><Input.TextArea maxLength={1000} showCount autoSize={{ minRows: 2, maxRows: 5 }} placeholder="比如：候选人已确认有兴趣，下午方便联系。" /></Form.Item>
    </Form>
  </AdminDialog>;
}
