/** 文件作用说明：从真实简历与岗位开启独立的企业招聘进程，保留原有推荐和任务记录。 */
"use client";
import { Alert, Form, Select } from "antd";
import { useEffect, useRef, useState } from "react";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { cloudRequest } from "@/lib/admin-api";
import { isReadOnlyResume, type PositionOption, type Resume } from "@/lib/resume-types";
import { loadRecruitmentWorkflows, recruitmentMemberOptions, recruitmentRequest, type RecruitmentMember, type RecruitmentWorkflow } from "@/lib/recruitment-api";

/** StartRecruitmentDialog 可从岗位招聘页面或指定简历进入，人员来自现有团队。 */
export default function StartRecruitmentDialog({ open, candidate, positions, initialPositionID = "", onClose, onStarted }: { open: boolean; candidate?: Resume; positions: PositionOption[]; initialPositionID?: string; onClose: () => void; onStarted: (id: string) => void }) {

  const { notify, user } = useAdmin();
  const [candidateID, setCandidateID] = useState("");
  const [positionID, setPositionID] = useState("");
  const [workflowID, setWorkflowID] = useState("");
  const [owner, setOwner] = useState("");
  const [workflows, setWorkflows] = useState<RecruitmentWorkflow[]>([]);
  const [members, setMembers] = useState<RecruitmentMember[]>([]);
  const [candidates, setCandidates] = useState<Resume[]>([]);
  const [busy, setBusy] = useState(false);
  const [searchBusy, setSearchBusy] = useState(false);
  const searchGeneration = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const saving = useRef(false);
  /** searchCandidates 只读取真实候选人的搜索摘要，并忽略晚到的旧搜索结果。 */

  async function searchCandidates(query = "") {

    const current = ++searchGeneration.current;
    setSearchBusy(true);
    try {

      const data = await cloudRequest(`/api/candidates?${new URLSearchParams({ q: query, page_size: "50" })}`);
      if (current === searchGeneration.current) setCandidates((data.candidates || []).filter((item: Resume) => !isReadOnlyResume(item)));
    } catch (cause) {

      if (current === searchGeneration.current) notify(cause instanceof Error ? cause.message : "简历没读出来", "warning");
    } finally {

      if (current === searchGeneration.current) setSearchBusy(false);
    }
  }

  useEffect(() => {

    if (!open) return;
    setCandidateID(candidate?.id || "");
    setPositionID(initialPositionID);
    setOwner(user?.email || "");
    void loadRecruitmentWorkflows().then((data) => {

      setWorkflows(data.workflows);
      setMembers(data.members);
      setWorkflowID(data.workflows.find((item) => item.is_default)?.id || data.workflows[0]?.id || "");
    }).catch((cause) => notify(cause.message || "流程没读出来", "error"));
    if (!candidate) void searchCandidates();
    return () => {

      searchGeneration.current++;
      if (timer.current) clearTimeout(timer.current);
    };
  }, [open, candidate?.id]);
  /** queueSearch 为可搜索简历选项合并短时间输入，不触发自动选择或写入。 */

  function queueSearch(value: string) {

    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {

      void searchCandidates(value);
    }, 250);
  }
  /** start 建立岗位应聘进程，重复点击不会创建第二条，内置示例始终拒绝开启。 */

  async function start() {

    if (!candidateID || !positionID || !workflowID || saving.current || isReadOnlyResume(candidate || candidateID)) return;
    saving.current = true;
    setBusy(true);
    try {

      const data = await recruitmentRequest("/applications", "POST", {

        candidate_id: candidateID,
        position_id: positionID,
        workflow_id: workflowID,
        owner_email: owner
      });
      notify(data.existing ? "这个岗位已在推进，已打开已有进度。" : "招聘流程开启了，推荐记录仍然保留。", "success");
      onStarted(data.application.id);
      onClose();
    } catch (cause) {

      notify(cause instanceof Error ? cause.message : "流程没有开启，请再试一次。", "error");
    } finally {

      saving.current = false;
      setBusy(false);
    }
  }

  return <AdminDialog
    open={open}
    title="开启企业招聘"
    loading={busy}
    onClose={onClose}
    onConfirm={() => void start()}
    confirmText="开启招聘"
    confirmDisabled={!candidateID || !positionID || !workflowID}>

    <Alert type="info" title="这是候选人在该岗位下的面试与录用进程，原来的猎头推荐和自动任务记录会保留。" style={{ marginBottom: 12 }} />

    <Form layout="vertical">
      <Form.Item label="候选人" required>
        <Select
          aria-label="招聘候选人"
          disabled={Boolean(candidate)}
          showSearch
          filterOption={false}
          onSearch={queueSearch}
          loading={searchBusy}
          value={candidateID || undefined}
          onChange={setCandidateID}
          placeholder="按姓名、手机或关键词搜索"
          options={candidate ? [{ value: candidate.id, label: candidate.candidate_name }] : candidates.map((item) => ({ value: item.id, label: `${item.candidate_name} · ${item.phone || "未留手机号"}` }))} />
      </Form.Item>
      <Form.Item label="招聘岗位" required>
        <Select
          aria-label="招聘岗位"
          showSearch
          optionFilterProp="label"
          value={positionID || undefined}
          onChange={setPositionID}
          options={positions.map((item) => ({ value: item.id, label: item.name }))} />
      </Form.Item>
      <Form.Item label="招聘流程" required>
        <Select
          aria-label="招聘流程模板"
          showSearch
          optionFilterProp="label"
          value={workflowID || undefined}
          onChange={setWorkflowID}
          options={workflows.map((item) => ({ value: item.id, label: `${item.name}${item.is_default ? "（默认）" : ""}` }))} />
      </Form.Item>
      <Form.Item label="招聘负责人">
        <Select
          aria-label="招聘负责人"
          showSearch
          optionFilterProp="label"
          value={owner || undefined}
          onChange={setOwner}
          options={recruitmentMemberOptions(members)} />
      </Form.Item>
    </Form>

  </AdminDialog>;
}
