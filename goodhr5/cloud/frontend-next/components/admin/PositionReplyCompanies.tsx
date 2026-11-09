/** 文件作用说明：展示团队公司单选卡，并仅在明确打开编辑器时修改共享公司资料。 */
"use client";

import { BankOutlined, EditOutlined, EnvironmentOutlined, PlusOutlined } from "@ant-design/icons";
import { Alert, Button, Flex, Form, Input, Radio } from "antd";
import { useId, useRef, useState } from "react";
import { deleteCompanyProfile, emptyCompanyProfile, loadCompanyProfiles, saveCompanyProfile, type CompanyProfile } from "@/lib/auto-reply";
import AdminDialog from "./AdminDialog";
import styles from "./position-auto-reply.module.css";

type Props = {
  profiles: CompanyProfile[];
  selected: string;
  onSelect: (id: string) => void;
  onProfiles: (profiles: CompanyProfile[]) => void;
  notify: (message: string, severity?: "success" | "error" | "warning" | "info") => void;
  confirm: (title: string, message: string) => Promise<boolean>;
};

/** PositionReplyCompanies 区分团队资料与岗位绑定；编辑或删除其它公司时不改变当前选择。 */
export default function PositionReplyCompanies({ profiles, selected, onSelect, onProfiles, notify, confirm }: Props) {
  const [editing, setEditing] = useState<CompanyProfile | null>(null);
  const [busy, setBusy] = useState(false);
  const [conflict, setConflict] = useState(false);
  const pending = useRef(false);
  const prefix = useId();
  /** save 保留相同公司的稳定编号，新增公司后由 HR 点击卡片决定是否绑定。 */
  async function save() {
    if (!editing || pending.current) return;
    if (!editing.name.trim()) { notify("请填写公司名称。", "warning"); return; }
    pending.current = true; setBusy(true);
    try {
      const saved = await saveCompanyProfile(editing);
      onProfiles(editing.id ? profiles.map(row => row.id === saved.id ? saved : row) : [saved, ...profiles]);
      setEditing(null); notify("公司档案已保存。", "success");
    } catch (cause) {
      if (cause && typeof cause === "object" && "status" in cause && cause.status === 409) setConflict(true);
      notify(cause instanceof Error ? cause.message : "公司资料没保存成功，可以再试一次。", "error");
    } finally { pending.current = false; setBusy(false); }
  }
  /** reload 由HR明确放弃旧草稿并载入最新共享资料，不能自动覆盖仍在编辑的内容。 */
  async function reload() {
    if (!editing?.id || pending.current) return;
    pending.current = true; setBusy(true);
    try {
      if (!await confirm("载入最新公司资料", "公司资料刚有更新。载入后会替换当前未保存的草稿。")) return;
      const rows = await loadCompanyProfiles(); const latest = rows.find(row => row.id === editing.id);
      if (!latest) throw new Error("这份公司档案已移走，请重新选择。");
      onProfiles(rows); setEditing(latest); setConflict(false);
    } catch (cause) { notify(cause instanceof Error ? cause.message : "公司资料没读出来，草稿还在。", "error"); }
    finally { pending.current = false; setBusy(false); }
  }
  /** remove 复核团队共享资料的删除意图；仅已删除的选中项需要解除本草稿的绑定。 */
  async function remove() {
    if (!editing?.id || pending.current) return;
    pending.current = true;
    try {
      if (!await confirm("删除公司档案", "确定删除「" + editing.name + "」？正在被岗位使用的档案会保留。")) return;
      setBusy(true);
      await deleteCompanyProfile(editing.id);
      onProfiles(profiles.filter(row => row.id !== editing.id));
      if (selected === editing.id) onSelect("");
      setEditing(null); notify("公司档案已删除。", "success");
    } catch (cause) {
      notify(cause instanceof Error ? cause.message : "删除没成功，可以再试一次。", "error");
    } finally { pending.current = false; setBusy(false); }
  }
  return <section className={styles.section}>
    <div className={styles.heading}><h3>岗位所属公司</h3><Flex gap={8}>
      {selected && <Button type="text" onClick={() => onSelect("")}>清除选择</Button>}
      <Button icon={<PlusOutlined />} onClick={() => {setConflict(false);setEditing(emptyCompanyProfile());}}>新建公司</Button>
    </Flex></div>
    {profiles.length > 0 ? <Radio.Group value={selected} aria-label="岗位所属公司" className={styles.companies} onChange={event => onSelect(event.target.value)}>
      {profiles.map(row => <div className={styles.company} data-selected={selected === row.id} key={row.id}>
        <Radio value={row.id} aria-label={row.name}>
          <span className={styles.companyName}><BankOutlined aria-hidden />{row.name}</span>
          {row.address && <span className={styles.address}><EnvironmentOutlined aria-hidden />{row.address}</span>}
        </Radio>
        <Button type="text" icon={<EditOutlined />} title="编辑公司" aria-label={'编辑公司：' + row.name} onClick={() => {setConflict(false);setEditing(row);}} />
      </div>)}
    </Radio.Group> : <p className={styles.muted}>团队还没有公司档案。新建一家公司后，即可在岗位中选用。</p>}
    {editing && <AdminDialog open title={editing.id ? "编辑公司档案" : "新建公司档案"} maxWidth="sm" loading={busy}
      onClose={() => setEditing(null)} onConfirm={() => void save()} confirmText="保存公司档案"
      extraActions={editing.id && <Button danger disabled={busy} onClick={() => void remove()}>删除档案</Button>}>
      <Form layout="vertical" component="div" disabled={busy}>
        {conflict && <Alert type="warning" showIcon title="公司资料刚有更新，草稿仍为你保留。" action={<Button disabled={busy} onClick={()=>void reload()}>载入最新资料</Button>} />}
        {([
          ["name", "公司名称"], ["address", "公司地址"], ["contact", "可公开的联系方式"], ["overview", "公司概况"], ["extra_info", "补充信息"],
        ] as const).map(([field, label]) => <Form.Item key={field} label={label} htmlFor={prefix + field} required={field === "name"}>
          {field === "overview" || field === "extra_info"
            ? <Input.TextArea id={prefix + field} value={editing[field]} autoSize={{ minRows: 2, maxRows: 6 }} onChange={event => setEditing({ ...editing, [field]: event.target.value })} />
            : <Input id={prefix + field} value={editing[field]} onChange={event => setEditing({ ...editing, [field]: event.target.value })} />}
        </Form.Item>)}
        <span className={styles.muted}>团队共用这份档案，保存后会同步到使用它的岗位。</span>
      </Form>
    </AdminDialog>}
  </section>;
}
