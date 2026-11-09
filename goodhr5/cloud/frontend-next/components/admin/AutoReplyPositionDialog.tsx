/** 文件作用说明：在岗位内配置对外资料、公司、候选人条件与回复话术，并按配置版本保存。 */
"use client";

import { Alert, Button, Form, Input, Skeleton, Switch } from "antd";
import { useEffect, useId, useRef, useState } from "react";
import { duplicateConditionContent, emptyPositionAutoReplyConfig, loadCompanyProfiles, loadPositionAutoReplyConfig, savePositionAutoReplyConfig,
  type CompanyProfile, type PositionAutoReplyConfig } from "@/lib/auto-reply";
import AdminDialog from "./AdminDialog";

import PositionReplyCompanies from "./PositionReplyCompanies";
import PositionReplyConditions from "./PositionReplyConditions";
import PositionReplyPhrases from "./PositionReplyPhrases";
import styles from "./position-auto-reply.module.css";

type Position = { id: string; name: string; requirement?: string };
type Props = {
  open: boolean;
  position: Position | null;
  allowAutoReply: boolean;
  notify: (message: string, severity?: "success" | "error" | "warning" | "info") => void;
  confirm: (title: string, message: string) => Promise<boolean>;
  onRequireMax: () => void;
  onClose: () => void;
  onSaved: (enabled: boolean) => void;
};

/** AutoReplyPositionDialog 每次打开独立建立草稿，关闭或换岗位将卸载正在请求的旧表单。 */
export default function AutoReplyPositionDialog({ open, position, ...props }: Props) {
  return open && position ? <PositionForm key={position.id} {...props} position={position} /> : null;
}

/** PositionForm 只保存本岗位的选择和话术；团队公司资料使用单独的保存入口。 */
function PositionForm({ position, allowAutoReply, notify, confirm, onRequireMax, onClose, onSaved }: Omit<Props, "open" | "position"> & { position: Position }) {
  const [config, setConfig] = useState<PositionAutoReplyConfig>(emptyPositionAutoReplyConfig(position.id));
  const [profiles, setProfiles] = useState<CompanyProfile[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [retry, setRetry] = useState(0);
  const [saving, setSaving] = useState(false);
  const [conflict, setConflict] = useState(false);
  const savingRef = useRef(false);
  const id = useId();
  useEffect(() => {
    let active = true;
    setLoading(true); setLoadError("");
    Promise.all([loadPositionAutoReplyConfig(position.id), loadCompanyProfiles()])
      .then(([loaded, companies]) => {
        if (!active) return;
        setConfig(loaded); setProfiles(companies); setConflict(false);
      })
      .catch(cause => { if (active) setLoadError(cause instanceof Error ? cause.message : "岗位配置没读出来，请再试一次。"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [position.id, retry]);
  /** save 校验岗位选择并按加载时的配置版本提交；重复点击只产生一个请求。 */
  async function save() {
    if (loading || loadError || savingRef.current) return;
    if (config.enabled && !allowAutoReply) { onRequireMax(); return; }
    if (config.enabled && !profiles.some(row => row.id === config.company_profile_id)) { notify("先给这个岗位选一家公司，再开启自动回复。", "warning"); return; }
    if (duplicateConditionContent(config.conditions)) { notify("有重复条件，请重新检查选择。", "warning"); return; }
    savingRef.current = true; setSaving(true);
    try {
      const saved = await savePositionAutoReplyConfig(config);
      onSaved(saved.enabled); notify("岗位自动回复配置已保存。", "success"); onClose();
    } catch (cause) {
      if (cause && typeof cause === "object" && "status" in cause && cause.status === 409) setConflict(true);
      notify(cause instanceof Error ? cause.message : "这次没保存成功，草稿还在。", "error");
    } finally { savingRef.current = false; setSaving(false); }
  }
  return <AdminDialog open title={<span className={styles.dialogTitle}>自动回复 · {position.name}</span>} onClose={onClose} onConfirm={() => void save()} confirmText="保存配置"
    loading={saving} confirmDisabled={loading || Boolean(loadError)} maxWidth="md">
    {loading ? <Skeleton active paragraph={{ rows: 6 }} /> : loadError ? <Alert type="warning" showIcon title={loadError}
      action={<Button onClick={() => setRetry(retry + 1)}>重试</Button>} /> :
      <Form layout="vertical" component="div" disabled={saving} className={styles.form}>
        {!allowAutoReply && <Alert type="warning" showIcon title="自动回复需要 Max 或全功能体验会员。您仍可查看、修改和关闭已有配置。" />}
        {conflict && <Alert type="warning" showIcon title="配置已更新，可以放弃当前草稿并重新载入。"
          action={<Button onClick={() => setRetry(retry + 1)}>载入最新配置</Button>} />}
        <section className={styles.section}>
          <div className={styles.heading}><h3>岗位自动回复</h3>
            <Switch checked={config.enabled} aria-label="开启岗位自动回复" onChange={enabled => {
              if (enabled && !allowAutoReply) onRequireMax();
              else setConfig({ ...config, enabled });
            }} />
          </div>
          <Form.Item label="对外岗位说明" extra="此处只填写岗位信息。公司信息在下面填写。" htmlFor={id} className={styles.description}>
            <Input.TextArea id={id} aria-label="自动回复岗位描述" value={config.position_description} autoSize={{ minRows: 2, maxRows: 8 }}
              placeholder="填写可以告诉候选人的职责、工作地点、薪资和时间。" onChange={event => setConfig({ ...config, position_description: event.target.value })} />
          </Form.Item>
        </section>
        <PositionReplyCompanies profiles={profiles} selected={config.company_profile_id} onProfiles={setProfiles}
          onSelect={company_profile_id => setConfig(current => ({ ...current, company_profile_id }))} notify={notify} confirm={confirm} />
        <PositionReplyConditions positionID={position.id} requirement={position.requirement || ""} description={config.position_description} conditions={config.conditions}
          presets={config.condition_options} onChange={conditions => setConfig(current => ({ ...current, conditions }))} />
        <PositionReplyPhrases config={config} onChange={phrase_overrides => setConfig(current => ({ ...current, phrase_overrides }))}
          onLegacyChange={resume_request_message => setConfig(current => ({ ...current, resume_request_message }))} />
        <p className={styles.foot}>岗位里将“是否生成简历”选为“需要”，资料和推荐报告才会进简历库。</p>
      </Form>}
  </AdminDialog>;
}
