/** 本文件负责岗位自动回复配置、团队公司档案和多条件编辑弹框。 */
"use client";

import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import { Alert, Button, Checkbox, Divider, Flex, Form, Input, Select, Skeleton, Switch, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import {
  deleteCompanyProfile,
  duplicateConditionContent,
  emptyCompanyProfile,
  emptyPositionAutoReplyConfig,
  loadCompanyProfiles,
  loadPositionAutoReplyConfig,
  saveCompanyProfile,
  savePositionAutoReplyConfig,
  type AutoReplyConditionType,
  type CompanyProfile,
  type PositionAutoReplyConfig,
} from "@/lib/auto-reply";
import AdminDialog from "./AdminDialog";

type AutoReplyPositionDialogProps = {
  open: boolean;
  position: { id: string; name: string } | null;
  allowAutoReply: boolean;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
  confirm: (title: string, message: string) => Promise<boolean>;
  onRequireMax: () => void;
  onClose: () => void;
  onSaved: (enabled: boolean) => void;
};

const conditionTypeOptions: Array<{
  value: AutoReplyConditionType;
  label: string;
}> = [
  { value: "required", label: "必须满足" },
  { value: "confirm", label: "需要确认" },
  { value: "bonus", label: "加分项" },
];

/** AutoReplyPositionDialog 编辑一个岗位的自动回复配置和团队公司资料。 */
export default function AutoReplyPositionDialog({
  open,
  position,
  allowAutoReply,
  notify,
  confirm,
  onRequireMax,
  onClose,
  onSaved,
}: AutoReplyPositionDialogProps) {
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState("");
  const loadEpoch = useRef(0);
  const [saving, setSaving] = useState(false);
  const [profileSaving, setProfileSaving] = useState(false);
  const [config, setConfig] = useState<PositionAutoReplyConfig>(
    emptyPositionAutoReplyConfig(""),
  );
  const [profiles, setProfiles] = useState<CompanyProfile[]>([]);
  const [companyForm, setCompanyForm] = useState<CompanyProfile>(
    emptyCompanyProfile(),
  );

  useEffect(() => {
    if (!open || !position?.id) return;
    void load(position.id);
    return () => { loadEpoch.current += 1; };
  }, [open, position?.id]);

  /** load 同时读取岗位自动回复配置和团队公司档案。 */
  async function load(positionID: string) {
    const epoch = ++loadEpoch.current;
    setLoading(true);
    setLoadError("");
    try {
      const [nextConfig, nextProfiles] = await Promise.all([
        loadPositionAutoReplyConfig(positionID),
        loadCompanyProfiles(),
      ]);
      if (epoch !== loadEpoch.current) return;
      setConfig(nextConfig);
      setProfiles(nextProfiles);
      const selected =
        nextProfiles.find(
          (profile) => profile.id === nextConfig.company_profile_id,
        ) || nextProfiles[0];
      setCompanyForm(selected || emptyCompanyProfile());
      if (!nextConfig.company_profile_id && selected) {
        setConfig({ ...nextConfig, company_profile_id: selected.id });
      }
    } catch (error) {
      if (epoch !== loadEpoch.current) return;
      setLoadError(error instanceof Error ? error.message : "配置没读出来，请再试一次。");
    } finally {
      if (epoch === loadEpoch.current) setLoading(false);
    }
  }

  /** selectCompanyProfile 切换岗位使用的公司档案并载入编辑内容。 */
  function selectCompanyProfile(profileID: string) {
    const selected = profiles.find((profile) => profile.id === profileID);
    setConfig((current) => ({
      ...current,
      company_profile_id: profileID,
    }));
    setCompanyForm(selected || emptyCompanyProfile());
  }

  /** startNewCompanyProfile 清空公司表单并准备新增档案。 */
  function startNewCompanyProfile() {
    setCompanyForm(emptyCompanyProfile());
    setConfig((current) => ({ ...current, company_profile_id: "" }));
  }

  /** persistCompanyProfile 保存当前公司档案并返回服务端正式记录。 */
  async function persistCompanyProfile() {
    if (!companyForm.name.trim()) {
      notify("公司档案至少要有个名字，不然我后面容易认错。", "warning");
      return null;
    }
    setProfileSaving(true);
    try {
      const saved = await saveCompanyProfile(companyForm);
      setProfiles((current) => {
        const exists = current.some((profile) => profile.id === saved.id);
        return exists
          ? current.map((profile) =>
              profile.id === saved.id ? saved : profile,
            )
          : [saved, ...current];
      });
      setCompanyForm(saved);
      setConfig((current) => ({
        ...current,
        company_profile_id: saved.id,
      }));
      notify("公司档案已经记好，团队里都能用。", "success");
      return saved;
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "公司档案没保存成功。",
        "error",
      );
      return null;
    } finally {
      setProfileSaving(false);
    }
  }

  /** removeCompanyProfile 二次确认后删除当前未被岗位使用的公司档案。 */
  async function removeCompanyProfile() {
    if (!companyForm.id) return;
    const approved = await confirm(
      "删除公司档案",
      `公主请确认要删除“${companyForm.name}”吗？正在被岗位使用的话，我会老实拦住。`,
    );
    if (!approved) return;
    setProfileSaving(true);
    try {
      await deleteCompanyProfile(companyForm.id);
      const remaining = profiles.filter(
        (profile) => profile.id !== companyForm.id,
      );
      const next = remaining[0] || emptyCompanyProfile();
      setProfiles(remaining);
      setCompanyForm(next);
      setConfig((current) => ({
        ...current,
        company_profile_id: next.id,
      }));
      notify("公司档案已删除，我没碰别的资料。", "success");
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "公司档案没删除成功。",
        "error",
      );
    } finally {
      setProfileSaving(false);
    }
  }

  /** addCondition 新增一条默认的待确认条件。 */
  function addCondition() {
    setConfig((current) => ({
      ...current,
      conditions: [
        ...current.conditions,
        {
          id: "",
          type: "confirm",
          content: "",
          sort_order: current.conditions.length,
          enabled: true,
        },
      ],
    }));
  }

  /** updateCondition 更新指定序号的岗位条件。 */
  function updateCondition(
    index: number,
    patch: Partial<PositionAutoReplyConfig["conditions"][number]>,
  ) {
    setConfig((current) => ({
      ...current,
      conditions: current.conditions.map((condition, conditionIndex) =>
        conditionIndex === index ? { ...condition, ...patch } : condition,
      ),
    }));
  }

  /** removeCondition 删除指定序号的岗位条件。 */
  function removeCondition(index: number) {
    setConfig((current) => ({
      ...current,
      conditions: current.conditions.filter(
        (_, conditionIndex) => conditionIndex !== index,
      ),
    }));
  }

  /** save 保存公司档案后再原子保存岗位配置和全部条件。 */
  async function save() {
    if (!position || config.position_id !== position.id || loading || loadError || saving || profileSaving) return;
    if (config.enabled && !allowAutoReply) {
      onRequireMax();
      return;
    }
    const emptyCondition = config.conditions.find(
      (condition) => !condition.content.trim(),
    );
    if (emptyCondition) {
      notify("有一条岗位条件还是空的，补上或删掉都行，我不记仇。", "warning");
      return;
    }
    const duplicate = duplicateConditionContent(config.conditions);
    if (duplicate) {
      notify(`岗位条件“${duplicate}”重复了，留一条就够。`, "warning");
      return;
    }
    setSaving(true);
    try {
      let companyProfileID = config.company_profile_id;
      if (companyForm.name.trim()) {
        const savedCompany = await saveCompanyProfile(companyForm);
        companyProfileID = savedCompany.id;
        setCompanyForm(savedCompany);
        setProfiles((current) => {
          const exists = current.some(
            (profile) => profile.id === savedCompany.id,
          );
          return exists
            ? current.map((profile) =>
                profile.id === savedCompany.id ? savedCompany : profile,
              )
            : [savedCompany, ...current];
        });
      }
      if (config.enabled && !companyProfileID) {
        notify("开启自动回复前要先选一份公司档案。", "warning");
        return;
      }
      const saved = await savePositionAutoReplyConfig({
        ...config,
        company_profile_id: companyProfileID,
      });
      setConfig(saved);
      onSaved(saved.enabled);
      notify(
        saved.enabled
          ? "自动回复配置保存好了，我会按这份内容老实回答。"
          : "自动回复配置已保存，开关先保持关闭。",
        "success",
      );
      onClose();
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "自动回复配置没保存成功，我们再试一次。",
        "error",
      );
    } finally {
      setSaving(false);
    }
  }

  const busy = loading || saving || profileSaving;
  return <AdminDialog open={open} title={position ? `自动回复 · ${position.name}` : "自动回复设置"}
    description="本地 AI 按岗位与公司资料回复；不确定的事，我会请你接手。"
    confirmText="保存配置" loading={saving} loadingText="保存中" confirmDisabled={busy || Boolean(loadError)}
    maxWidth="md" onClose={onClose} onConfirm={() => void save()}>
    {loading ? <Skeleton active paragraph={{ rows: 8 }} /> : loadError ? <Alert type="warning" showIcon title={loadError}
      action={<Button onClick={() => position && void load(position.id)}>重试</Button>} /> :
      <Form layout="vertical" disabled={busy}>
        {!allowAutoReply ? <Alert type="warning" showIcon title="自动回复需要 Max 或全功能体验会员。你仍可查看和关闭已有配置。" style={{ marginBottom: 12 }} /> : null}
        <Form.Item label="岗位自动回复">
          <Flex align="center" gap={8}><Switch checked={config.enabled} aria-label="开启岗位自动回复"
            onChange={(enabled) => { if (enabled && !allowAutoReply) onRequireMax(); else setConfig({ ...config, enabled }); }} />
            <Typography.Text>{config.enabled ? "已开启" : "暂未开启"}</Typography.Text></Flex>
        </Form.Item>
        <Form.Item label="自动回复岗位描述" extra="写真实、稳定的工作内容、薪资、地点和时间，资料不够时我会请你接手。">
          <Input.TextArea aria-label="自动回复岗位描述" value={config.position_description} autoSize={{ minRows: 3, maxRows: 8 }}
            onChange={(event) => setConfig({ ...config, position_description: event.target.value })} />
        </Form.Item>
        <Form.Item label="没有简历时发送的话术">
          <Input.TextArea aria-label="索要简历话术" value={config.resume_request_message} autoSize={{ minRows: 2, maxRows: 4 }}
            onChange={(event) => setConfig({ ...config, resume_request_message: event.target.value })} />
        </Form.Item>
        <Divider />
        <Flex justify="space-between" align="center" gap={8} wrap style={{ marginBottom: 12 }}>
          <Typography.Text strong>岗位条件</Typography.Text><Button icon={<PlusOutlined aria-hidden="true" />} onClick={addCondition}>添加条件</Button>
        </Flex>
        <Typography.Paragraph type="secondary">必须满足和需要确认会逐项核对，加分项不阻止推荐。</Typography.Paragraph>
        {config.conditions.length ? <Flex vertical gap={8}>{config.conditions.map((condition, index) =>
          <Flex key={condition.id || `new-${index}`} align="center" gap={8} wrap>
            <Select aria-label={`条件 ${index + 1} 类型`} value={condition.type} options={conditionTypeOptions} style={{ width: 120 }}
              onChange={(type: AutoReplyConditionType) => updateCondition(index, { type })} />
            <Input aria-label={`条件 ${index + 1}`} value={condition.content} placeholder="例如：能否接受出差"
              style={{ flex: "1 1 220px" }} onChange={(event) => updateCondition(index, { content: event.target.value })} />
            <Checkbox checked={condition.enabled} onChange={(event) => updateCondition(index, { enabled: event.target.checked })}>启用</Checkbox>
            <Button danger aria-label={`删除条件 ${index + 1}`} icon={<DeleteOutlined aria-hidden="true" />} onClick={() => removeCondition(index)} />
          </Flex>)}</Flex> : <Alert type="info" showIcon title="还没有条件，也可以先保存，之后再补。" />}
        <Divider />
        <Flex justify="space-between" align="center" gap={8} wrap style={{ marginBottom: 12 }}>
          <Typography.Text strong>团队公司档案</Typography.Text><Button icon={<PlusOutlined aria-hidden="true" />} onClick={startNewCompanyProfile}>新建档案</Button>
        </Flex>
        <Form.Item label="当前岗位使用的公司档案">
          <Select aria-label="公司档案" showSearch optionFilterProp="label" allowClear value={config.company_profile_id || undefined}
            placeholder="选择已有档案，或新建一份" options={profiles.map((profile) => ({ value: profile.id, label: profile.name }))}
            onChange={(id) => selectCompanyProfile(id || "")} />
        </Form.Item>
        <Form.Item label="公司档案名称"><Input aria-label="公司档案名称" value={companyForm.name} onChange={(event) => setCompanyForm({ ...companyForm, name: event.target.value })} /></Form.Item>
        <Form.Item label="公司地址"><Input aria-label="公司地址" value={companyForm.address} onChange={(event) => setCompanyForm({ ...companyForm, address: event.target.value })} /></Form.Item>
        <Form.Item label="可向候选人提供的联系方式"><Input aria-label="公司联系方式" value={companyForm.contact} onChange={(event) => setCompanyForm({ ...companyForm, contact: event.target.value })} /></Form.Item>
        <Form.Item label="公司概况"><Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} aria-label="公司概况" value={companyForm.overview} onChange={(event) => setCompanyForm({ ...companyForm, overview: event.target.value })} /></Form.Item>
        <Form.Item label="其他公司信息"><Input.TextArea autoSize={{ minRows: 2, maxRows: 6 }} aria-label="其他公司信息" value={companyForm.extra_info} onChange={(event) => setCompanyForm({ ...companyForm, extra_info: event.target.value })} /></Form.Item>
        <Flex gap={8} wrap><Button loading={profileSaving} onClick={() => void persistCompanyProfile()}>保存公司档案</Button>
          {companyForm.id ? <Button danger icon={<DeleteOutlined aria-hidden="true" />} onClick={() => void removeCompanyProfile()}>删除档案</Button> : null}</Flex>
        <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>聊天和附件留本机；岗位里将“是否生成简历”选为“需要”，资料和推荐报告才会进简历库。</Typography.Paragraph>
      </Form>}
  </AdminDialog>;
}
