/** 文件作用说明：提供手动维护和 AI 草稿共用的简历表单，执行手机号校验、查重及字段差异确认。 */
"use client";
import { Alert, Button, Checkbox, Collapse, Flex, Form, Input, InputNumber, Select, Space, Table, Typography } from "antd";
import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { addRecommendation, addResumeNote, checkResumeDuplicates, loadResume, saveResume } from "@/lib/resume-api";
import { nonEmptyChanges, normalizePhone, validPhone, type DuplicateResult, type PositionOption, type Resume, type ResumeDraft } from "@/lib/resume-types";
import styles from "./resumes.module.css";

export type ResumeDraftSubmission = { candidate: ResumeDraft; candidate_id?: string; note: string; position_ids: string[] };
type Props = {
  open: boolean; candidate?: Resume | null; draft?: ResumeDraft; positions: PositionOption[];
  onClose: () => void; onSaved: (candidate: Resume) => void;
  onConfirmDraft?: (submission: ResumeDraftSubmission) => Promise<Resume>;
  persistedID?: string;
};
const fieldLabels: Record<string, string> = {
  candidate_name: "姓名", phone: "手机号", email: "邮箱", gender: "性别", birth_ym: "出生年月",
  current_region: "当前地区", expected_region: "期望地区", work_years: "工作年限", education_level: "学历",
  work_status: "求职状态", expected_position: "期望职位", expected_salary_min: "最低月薪（K）",
  expected_salary_max: "最高月薪（K）", expected_salary_text: "期望薪资原文", current_salary_text: "当前薪资原文", personal_description: "个人优势", raw_text: "简历正文", tags: "标签",
  work_experiences: "工作经历", educations: "教育经历", project_experiences: "项目经历", certificates: "证书", honors: "荣誉",
};
/** clearableFields 只包含手动表单可维护的非身份字段，防止清空来源、版本或姓名手机号。 */
const clearableFields = ["email", "gender", "birth_ym", "education_level", "current_region", "expected_region", "work_years", "work_status", "expected_position", "tags", "expected_salary_min", "expected_salary_max", "work_experiences", "educations", "project_experiences", "certificates", "honors", "personal_description", "raw_text"] as const;

/** ResumeEditor 保留已创建简历 ID，使备注或关联失败后的重试不会再次创建简历。 */
export default function ResumeEditor({ open, candidate, draft, positions, onClose, onSaved, onConfirmDraft, persistedID = "" }: Props) {
  const { notify } = useAdmin();
  const [form] = Form.useForm<ResumeDraft & { first_note?: string; position_ids?: string[] }>();
  const [busy, setBusy] = useState(false);
  const [checking, setChecking] = useState(false);
  const [duplicates, setDuplicates] = useState<DuplicateResult | null>(null);
  const [target, setTarget] = useState<Resume | null>(candidate || null);
  const [submission, setSubmission] = useState<ResumeDraftSubmission | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [clearFields, setClearFields] = useState<string[]>([]);
  const [allowPossible, setAllowPossible] = useState(false);
  const [error, setError] = useState("");
  const saveKey = useRef("");
  const savedCandidate = useRef<Resume | null>(null);
  const noteDone = useRef(false);
  const linkedPositions = useRef(new Set<string>());
  const duplicateGeneration = useRef(0);
  useEffect(() => {
    if (!open) return;
    form.resetFields();
    form.setFieldsValue({ source_type: "manual", ...(candidate || draft || { candidate_name: "", phone: "" }) });
    setTarget(candidate || null); setDuplicates(null); setAllowPossible(false); setSubmission(null); setClearFields([]); setError("");
    savedCandidate.current = null; noteDone.current = false; linkedPositions.current = new Set(); duplicateGeneration.current += 1;
    saveKey.current = crypto.randomUUID();
  }, [open, candidate, draft, form]);

  /** collectDraft 校验并规范化输入，移除动态列表中完全空白的行。 */
  async function collectDraft(): Promise<ResumeDraftSubmission> {
    const validated = await form.validateFields();
    const values = { ...form.getFieldsValue(true), ...validated };
    const { first_note, position_ids, ...fields } = values;
    const normalized = { ...fields, candidate_name: fields.candidate_name.trim(), phone: normalizePhone(fields.phone) };
    for (const key of ["work_experiences", "educations", "project_experiences", "certificates", "honors"] as const) {
      const rows = normalized[key];
      if (Array.isArray(rows)) (normalized as Record<string, unknown>)[key] = rows.filter((item) => item && Object.values(item).some((value) => value != null && String(value).trim()));
    }
    return { candidate: normalized, candidate_id: target?.id || persistedID || undefined, note: first_note?.trim() || "", position_ids: position_ids || [] };
  }
  /** checkDuplicates 仅传送身份线索并显示匹配原因，不自动合并候选人。 */
  async function checkDuplicates() {
    const current = ++duplicateGeneration.current;
    setChecking(true); setError("");
    try {
      const values = form.getFieldsValue(true);
      const result = await checkResumeDuplicates({ ...values, phone: normalizePhone(values.phone) }, target?.id || persistedID);
      if (current !== duplicateGeneration.current) return;
      setDuplicates(result); setAllowPossible(false);
      return result;
    } catch (cause) { if (current === duplicateGeneration.current) setError(cause instanceof Error ? cause.message : "查重没成功，请再试一次"); }
    finally { if (current === duplicateGeneration.current) setChecking(false); }
  }
  /** chooseExisting 读取已有简历并切换为比较更新，已有有效字段仍由后端保护。 */
  async function chooseExisting(id: string) {
    duplicateGeneration.current += 1; setChecking(false);
    setBusy(true); setError("");
    try { setTarget(await loadResume(id)); setDuplicates(null); setAllowPossible(false); }
    catch (cause) { setError(cause instanceof Error ? cause.message : "已有简历没读出来，请重试"); }
    finally { setBusy(false); }
  }
  /** previewSubmission 在保存前检查重复并展示实际将修改的字段。 */
  async function previewSubmission() {
    setError("");
    try {
      const next = await collectDraft();
      duplicateGeneration.current += 1; setChecking(false);
      setBusy(true);
      const result = await checkResumeDuplicates(next.candidate, target?.id || persistedID);
      setDuplicates(result);
      if (result.exact.length) { setError("这个手机号已有简历，请先选择已有记录，再比较更新。"); return; }
      if (result.hidden_match) { setError("团队内已有可能相同的简历，请管理员帮忙核对后再入库。"); return; }
      if (result.possible.length && !allowPossible) { setError("我找到几份可能重复的简历，请先核对下面的提示。"); return; }
      setSubmission(next); setClearFields([]); setPreviewOpen(true);
    } catch (cause) {
      if (!(cause && typeof cause === "object" && "errorFields" in cause)) setError(cause instanceof Error ? cause.message : "请检查填写内容后重试");
    } finally { setBusy(false); }
  }
  /** persistSubmission 分步保存简历、备注和岗位关联，每步成功结果均保留供安全重试。 */
  async function persistSubmission() {
    if (!submission) return;
    setBusy(true); setError("");
    try {
      let result = savedCandidate.current;
      const changes = nonEmptyChanges(submission.candidate, target);
      if (target) delete changes.source_type;
      if (onConfirmDraft && !result) {
        result = await onConfirmDraft({ ...submission, candidate_id: target?.id || persistedID || undefined, candidate: {
          ...changes, candidate_name: submission.candidate.candidate_name, phone: submission.candidate.phone,
          ...(target ? { row_version: target.row_version } : {}),
        } });
        savedCandidate.current = result; noteDone.current = true;
        submission.position_ids.forEach((id) => linkedPositions.current.add(id));
      } else if (!result) {
        const confirmedClears = clearCandidates.filter((field) => clearFields.includes(field));
        result = await saveResume(target?.id || "", changes, saveKey.current, target?.row_version, confirmedClears);
        savedCandidate.current = result;
      }
      if (!result?.id) throw new Error("简历保存结果没返回编号，请刷新核对，避免重复添加。");
      if (submission.note && !noteDone.current) {
        await addResumeNote(result.id, submission.note, `${saveKey.current}:note`); noteDone.current = true;
      }
      for (const id of submission.position_ids) {
        if (linkedPositions.current.has(id)) continue;
        await addRecommendation(result.id, id, "draft", "", `${saveKey.current}:position:${id}`);
        linkedPositions.current.add(id);
      }
      setPreviewOpen(false); notify(target ? "简历更新好了，原有记录也留着。" : "简历已入库，我认真记下了。", "success"); onSaved(result);
    } catch (cause) {
      setError(`${savedCandidate.current ? "简历已保存，剩余记录没记全；点保存只会重试剩余部分。 " : ""}${cause instanceof Error ? cause.message : "保存没成功，请重试"}`);
      setPreviewOpen(false);
    } finally { setBusy(false); }
  }
  const matches = [...(duplicates?.exact || []), ...(duplicates?.possible || [])];
  const changes = submission ? nonEmptyChanges(submission.candidate, target) : {};
  const clearCandidates = submission && candidate && target?.id === candidate.id && !onConfirmDraft
    ? clearableFields.filter((field) => !isBlankMaintenanceValue(target[field]) && isBlankMaintenanceValue(submission.candidate[field]))
    : [];
  return <>
    <AdminDialog open={open} title={target ? `维护简历 · ${target.candidate_name}` : onConfirmDraft ? "确认解析草稿" : "手动添加简历"} maxWidth="md"
      description="姓名和手机号填好就能入库，其它信息可以慢慢补。选择岗位只建立关联。"
      onClose={onClose} onConfirm={() => void (savedCandidate.current ? persistSubmission() : previewSubmission())} loading={busy} confirmText={savedCandidate.current ? "重试剩余记录" : "检查并保存"}>
      <Flex vertical gap={14}>
        {error ? <Alert type="warning" title={error} showIcon /> : null}
        {persistedID ? <Alert type="info" title="简历主体已保存，继续确认只会补齐尚未完成的记录。" showIcon /> : null}
        {target && !candidate ? <Alert type="info" title={`将比较更新已有简历：${target.candidate_name} · ${target.phone || "未留手机号"}`} showIcon /> : null}
        <Form form={form} layout="vertical" className={styles.editorForm} disabled={busy || Boolean(savedCandidate.current) || Boolean(persistedID)} onValuesChange={(changed) => { if (changed.phone != null || changed.candidate_name != null) { duplicateGeneration.current += 1; setChecking(false); setDuplicates(null); setAllowPossible(false); } }}>
          <div className={styles.fieldGrid}>
            <Form.Item name="candidate_name" label="姓名" rules={[{ required: true, whitespace: true, message: "姓名补一下，我好认真记账。" }]}><Input maxLength={80} /></Form.Item>
            <Form.Item name="phone" label="手机号" rules={[{ required: true, message: "手机号补一下，我再帮你入库。" }, { validator: (_, value) => !value || validPhone(value) ? Promise.resolve() : Promise.reject(new Error("请填写完整手机号，掩码号码暂时用不了。")) }]}><Input maxLength={24} onBlur={() => { if (form.getFieldValue("phone")) void checkDuplicates(); }} /></Form.Item>
            <Form.Item name="email" label="邮箱" rules={[{ type: "email", message: "邮箱看起来少了点什么，请检查一下。" }]}><Input /></Form.Item>
            <Form.Item name="gender" label="性别"><Select allowClear options={[{ value: "男", label: "男" }, { value: "女", label: "女" }, { value: "未知", label: "未说明" }]} /></Form.Item>
            <Form.Item name="birth_ym" label="出生年月" rules={[{ pattern: /^(?:19|20)\d{2}-(?:0[1-9]|1[0-2])$/, message: "填成 1995-06 这样的年月就好。" }]}><Input placeholder="例如 1995-06" /></Form.Item>
            <Form.Item name="education_level" label="最高学历"><Select allowClear showSearch options={["博士", "硕士", "本科", "大专", "高中", "中专", "初中及以下"].map((value) => ({ value, label: value }))} /></Form.Item>
            <Form.Item name="current_region" label="当前地区"><Input /></Form.Item>
            <Form.Item name="expected_region" label="期望地区"><Input /></Form.Item>
            <Form.Item name="work_years" label="工作年限"><Input placeholder="例如 5年 / 应届生" /></Form.Item>
            <Form.Item name="work_status" label="求职状态"><Select allowClear showSearch options={["在职，考虑机会", "离职，可随时到岗", "暂不考虑", "应届毕业生"].map((value) => ({ value, label: value }))} /></Form.Item>
            <Form.Item name="expected_position" label="期望职位"><Input /></Form.Item>
            <Form.Item name="tags" label="标签"><Select mode="tags" tokenSeparators={[",", "，"]} options={[]} /></Form.Item>
            <Form.Item name="expected_salary_min" label="最低月薪（K）"><InputNumber min={0} precision={2} style={{ width: "100%" }} /></Form.Item>
            <Form.Item name="expected_salary_max" label="最高月薪（K）" dependencies={["expected_salary_min"]} rules={[({ getFieldValue }) => ({ validator(_, value) { const low = getFieldValue("expected_salary_min"); return value == null || low == null || value >= low ? Promise.resolve() : Promise.reject(new Error("最高月薪应不小于最低月薪。")); } })]}><InputNumber min={0} precision={2} style={{ width: "100%" }} /></Form.Item>
            {!candidate ? <Form.Item name="position_ids" label="关联岗位" className={styles.wideField}><Select mode="multiple" showSearch optionFilterProp="label" options={positions.map((item) => ({ value: item.id, label: item.name }))} placeholder="可选多个，只关联、不启动任务" /></Form.Item> : null}
          </div>
          <Collapse items={[
            { key: "work", label: "工作经历", children: <ExperienceFields name="work_experiences" fields={[["company_name", "公司"], ["position_name", "职位"]]} /> },
            { key: "education", label: "教育经历", children: <ExperienceFields name="educations" fields={[["school_name", "学校"], ["major_name", "专业"], ["education_level", "学历"]]} /> },
            { key: "project", label: "项目经历", children: <ExperienceFields name="project_experiences" fields={[["project_name", "项目"], ["role_name", "角色"]]} /> },
            { key: "certificate", label: "证书与荣誉", children: <><ExperienceFields name="certificates" fields={[["certificate_name", "证书名称"]]} simple /><ExperienceFields name="honors" fields={[["honor_name", "荣誉名称"]]} simple /></> },
            { key: "description", label: "个人优势与简历正文", children: <><Form.Item name="personal_description" label="个人优势"><Input.TextArea autoSize={{ minRows: 3, maxRows: 8 }} /></Form.Item><Form.Item name="raw_text" label="简历正文"><Input.TextArea autoSize={{ minRows: 4, maxRows: 10 }} /></Form.Item></> },
          ]} />
          {!candidate ? <Form.Item name="first_note" label="首条备注" style={{ marginTop: 14 }}><Input.TextArea maxLength={1000} showCount autoSize={{ minRows: 2, maxRows: 5 }} placeholder="比如：下午可联系，对这个岗位有兴趣。" /></Form.Item> : null}
        </Form>
        <Flex align="center" justify="space-between"><Typography.Text type="secondary">查重仅提醒，可能重复的简历不会自动合并。</Typography.Text><Button onClick={() => void checkDuplicates()} loading={checking} disabled={busy}>再查一次</Button></Flex>
        {matches.length ? <>
          <Table size="small" rowKey={(item) => item.candidate.id} dataSource={matches} pagination={false} columns={[
            { title: "匹配简历", render: (_, item) => <Space direction="vertical" size={0}><Typography.Text strong>{item.candidate.candidate_name}</Typography.Text><Typography.Text type="secondary">{item.candidate.phone || "未留手机号"}</Typography.Text></Space> },
            { title: "原因", render: (_, item) => `${item.match_type === "exact" ? "确定重复" : "可能重复"} · ${(item.reasons || []).join("、") || item.reason || "身份信息相似"}` },
            { title: "操作", width: 112, render: (_, item) => <Button size="small" onClick={() => void chooseExisting(item.candidate.id)}>比较更新</Button> },
          ]} />
          {!duplicates?.exact.length ? <Checkbox checked={allowPossible} onChange={(event) => setAllowPossible(event.target.checked)}>我核对过了，是另一位候选人，继续新建。</Checkbox> : null}
        </> : duplicates ? <Typography.Text type="secondary">{duplicates.hidden_match ? "团队内可能已有简历，请管理员协助核对。" : "暂未找到相同简历。"}</Typography.Text> : null}
      </Flex>
    </AdminDialog>
    <AdminDialog open={previewOpen} title={target ? "确认更新内容" : "确认入库内容"} maxWidth="sm" onClose={() => setPreviewOpen(false)} onConfirm={() => void persistSubmission()} loading={busy} confirmText="确认保存">
      <Alert type="info" showIcon title={target ? clearCandidates.length ? "有值的改动会更新；留空的字段只有勾选确认才会清空。" : "只更新下面有值的字段；没识别到的信息会保留已有内容。" : "保存后可继续补充备注和岗位推荐。"} style={{ marginBottom: 12 }} />
      <Table size="small" pagination={false} rowKey="field" dataSource={Object.entries(changes).filter(([field]) => field !== "source_type").map(([field, value]) => ({ field, value }))} columns={[
        { title: "字段", width: 120, render: (_, row) => fieldLabels[row.field] || row.field },
        ...(target ? [{ title: "已有内容", render: (_: unknown, row: { field: string; value: unknown }) => displayValue(target[row.field as keyof Resume]) }] : []),
        { title: target ? "更新为" : "保存内容", render: (_, row) => displayValue(row.value) },
      ]} />
      {clearCandidates.length ? <>
        <Typography.Paragraph style={{ margin: "14px 0 8px" }}>这些内容刚被留空了。不勾选就继续保留已有内容。</Typography.Paragraph>
        <Table size="small" pagination={false} rowKey="field" dataSource={clearCandidates.map((field) => ({ field, previous: target?.[field] }))} columns={[
          { title: "字段", width: 110, render: (_, row) => fieldLabels[row.field] },
          { title: "已有内容", render: (_, row) => displayValue(row.previous) },
          { title: "确认清空", width: 150, render: (_, row) => <Checkbox disabled={busy || Boolean(savedCandidate.current)} checked={clearFields.includes(row.field)} onChange={(event) => setClearFields((current) => event.target.checked ? [...current, row.field] : current.filter((field) => field !== row.field))}>确认清空{fieldLabels[row.field]}</Checkbox> },
        ]} />
      </> : null}
      {submission?.note ? <Typography.Paragraph style={{ marginTop: 12 }}>新增备注：{submission.note}</Typography.Paragraph> : null}
      {submission?.position_ids.length ? <Typography.Text>关联 {submission.position_ids.length} 个岗位，阶段为“待推荐”。</Typography.Text> : null}
    </AdminDialog>
  </>;
}
/** isBlankMaintenanceValue 判断人工维护字段是否留空，零薪资属于有效值，空数组属于清空。 */
function isBlankMaintenanceValue(value: unknown) {
  return value == null || (typeof value === "string" && !value.trim()) || (Array.isArray(value) && value.length === 0);
}
/** displayValue 以便于核对的短文本展示字段变化。 */
function displayValue(value: unknown) {
  if (Array.isArray(value)) return value.map((item) => typeof item === "string" ? item : Object.values(item || {}).filter(Boolean).join(" · ")).join("；") || "未填写";
  return value == null || value === "" ? "未填写" : String(value);
}
/** ExperienceFields 复用动态表单维护工作、教育、项目、证书与荣誉条目。 */
function ExperienceFields({ name, fields, simple = false }: { name: string; fields: string[][]; simple?: boolean }) {
  return <Form.List name={name}>{(rows, { add, remove }) => <Flex vertical gap={12}>
    {rows.map((row) => <div key={row.key} className={styles.experienceEditor}>
      <div className={styles.fieldGrid}>
        {fields.map(([field, label]) => <Form.Item key={field} name={[row.name, field]} label={label}><Input /></Form.Item>)}
        {!simple ? <><Form.Item name={[row.name, "start_ym"]} label="开始年月" rules={[{ pattern: /^(?:19|20)\d{2}-(?:0[1-9]|1[0-2])$/, message: "请填 YYYY-MM 格式" }]}><Input placeholder="2020-01" /></Form.Item><Form.Item name={[row.name, "end_ym"]} label="结束年月" rules={[{ pattern: /^(?:(?:19|20)\d{2}-(?:0[1-9]|1[0-2])|至今)$/, message: "请填 YYYY-MM 或至今" }]}><Input placeholder="留空表示至今" /></Form.Item><Form.Item name={[row.name, "content"]} label="内容" className={styles.wideField}><Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} /></Form.Item></> : null}
      </div>
      <Button type="text" danger size="small" icon={<DeleteOutlined />} onClick={() => remove(row.name)}>移除这段</Button>
    </div>)}
    <Button type="dashed" icon={<PlusOutlined />} onClick={() => add({})}>添加一条</Button>
  </Flex>}</Form.List>;
}
