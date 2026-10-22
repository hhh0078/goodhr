/** 文件作用说明：提供猎头客户与甲方岗位的分页查询、权限内编辑和现有平台岗位绑定。 */
"use client";
import { Alert, Button, Flex, Form, Input, Pagination, Select, Table, Tag, Typography } from "antd";
import { PlusOutlined, SearchOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import Link from "next/link";
import AdminDialog from "../AdminDialog";
import { useAdmin } from "../AdminApp";
import { SectionPanel } from "../AdminUI";
import CustomerEditor from "./CustomerEditor";
import JobEditor from "./JobEditor";
import { cloudRequest } from "@/lib/admin-api";
import { customerStatuses, jobStatuses, jobPriorities, loadHeadhunterCustomers, loadHeadhunterMetadata, saveHeadhunterEntity, type HeadhunterCustomer, type HeadhunterJob, type HeadhunterMetadata } from "@/lib/headhunter-api";
import styles from "./headhunter.module.css";

type Entity = HeadhunterCustomer | HeadhunterJob;
/** HeadhunterPage 按客户或岗位展示列表，筛选写入地址且保留编辑失败草稿。 */
export default function HeadhunterPage({ kind }: { kind: "customers" | "jobs" }) {
  const { user, notify } = useAdmin();
  const params = useSearchParams(); const router = useRouter(); const queryText = params.toString();
  const account = `${user?.tenant_id || ""}:${user?.id || ""}`;
  const isCustomer = kind === "customers";
  const [items, setItems] = useState<Entity[]>([]); const [total, setTotal] = useState(0);
  const [q, setQ] = useState(params.get("q") || ""); const [status, setStatus] = useState(params.get("status") || "");
  const [customerID, setCustomerID] = useState(params.get("customer_id") || "");
  const [owner, setOwner] = useState(params.get("owner_email") || "");
  const [loading, setLoading] = useState(false); const [error, setError] = useState("");
  const [metadata, setMetadata] = useState<HeadhunterMetadata | null>(null);
  const [customers, setCustomers] = useState<HeadhunterCustomer[]>([]); const [optionError, setOptionError] = useState("");
  const [entity, setEntity] = useState<Entity | null>(null); const [readonly, setReadonly] = useState(false);
  const [openingID, setOpeningID] = useState(""); const [saving, setSaving] = useState(false); const [saveError, setSaveError] = useState("");
  const [customerForm] = Form.useForm<HeadhunterCustomer>(); const [jobForm] = Form.useForm<HeadhunterJob>();
  const generation = useRef(0); const optionsGeneration = useRef(0); const editGeneration = useRef(0); const saveInFlight = useRef(false);
  const page = Math.max(1, Number.isSafeInteger(Number(params.get("page"))) ? Number(params.get("page")) : 1);
  const pageSize = [10, 20, 50, 100].includes(Number(params.get("page_size"))) ? Number(params.get("page_size")) : 20;
  const statuses = isCustomer ? customerStatuses : jobStatuses;
  const teamRole = metadata?.members.find((member) => member.email.toLowerCase() === String(user?.email || "").toLowerCase())?.role;
  const admin = ["super_admin", "tenant_admin", "admin", "owner"].includes(user?.role) || ["owner", "admin"].includes(user?.tenant_role) || ["owner", "admin"].includes(teamRole || "");

  /** load 查询服务端当前权限内的分页，旧响应不会覆盖新条件。 */
  async function load() {
    const current = ++generation.current; setLoading(true); setError("");
    const query = new URLSearchParams(queryText); query.set("page", String(page)); query.set("page_size", String(pageSize));
    try {
      const result = await cloudRequest(`/api/headhunter/${kind}?${query}`);
      if (current !== generation.current) return;
      if (!Array.isArray(result.items) || !Number.isFinite(Number(result.total))) throw new Error("列表没读完整，请重试。");
      setItems(result.items); setTotal(Number(result.total));
    } catch (cause) { if (current === generation.current) setError(cause instanceof Error ? cause.message : "列表没读出来，请重试。"); }
    finally { if (current === generation.current) setLoading(false); }
  }
  /** loadOptions 独立读取有效团队成员、平台岗位与客户，缺项时不使用猜测数据。 */
  async function loadOptions() {
    const current = ++optionsGeneration.current; setOptionError("");
    try {
      const [nextMetadata, nextCustomers] = await Promise.all([loadHeadhunterMetadata(), isCustomer ? Promise.resolve([]) : loadHeadhunterCustomers()]);
      if (current !== optionsGeneration.current) return;
      setMetadata(nextMetadata); setCustomers(nextCustomers);
    } catch (cause) { if (current === optionsGeneration.current) setOptionError(cause instanceof Error ? cause.message : "选项没读出来，请重试。"); }
  }
  useEffect(() => { setItems([]); setEntity(null); setMetadata(null); setCustomers([]); void loadOptions(); return () => { generation.current += 1; optionsGeneration.current += 1; editGeneration.current += 1; }; }, [account, kind]);
  useEffect(() => { setQ(params.get("q") || ""); setStatus(params.get("status") || ""); setOwner(params.get("owner_email") || ""); setCustomerID(params.get("customer_id") || ""); void load(); return () => { generation.current += 1; }; }, [queryText, account, kind]);

  /** navigateFilters 保存筛选和分页，重新查询不需要独立刷新按钮。 */
  function navigateFilters(nextPage = 1, nextSize = pageSize, reset = false) {
    const query = new URLSearchParams({ page: String(nextPage), page_size: String(nextSize) });
    if (!reset) { if (q.trim()) query.set("q", q.trim()); if (status) query.set("status", status); if (owner) query.set("owner_email", owner); if (!isCustomer && customerID) query.set("customer_id", customerID); }
    const next = query.toString(); if (next === queryText) void load(); else router.replace(`/admin/headhunter/${kind}?${next}`, { scroll: false });
  }

  /** canManage 使用服务端权限优先，本地只在缺失标记时按负责人或管理员收紧入口。 */
  function canManage(item: Entity) { return item.can_manage ?? (admin || item.owner_email === user?.email); }

  /** openCreate 使用明确默认值创建草稿，普通成员默认本人负责。 */
  function openCreate() {
    if (!metadata) return;
    editGeneration.current += 1; setSaveError(""); setReadonly(false);
    const next: Entity = isCustomer
      ? { id: "", name: "", short_name: "", code: "", industry: "", scale: "", city: "", status: "potential", source: "", owner_email: user?.email || "", contacts: [], contracts: [], remark: "", revision: 0 }
      : { id: "", customer_id: customerID || "", name: "", city: "", department: "", headcount: 1, salary_min: 0, salary_max: 0, education: "", experience: "", status: "open", priority: "normal", owner_email: user?.email || "", description: "", requirements: "", remark: "", position_ids: [], revision: 0 };
    setEntity(next); if (isCustomer) { customerForm.resetFields(); customerForm.setFieldsValue(next as HeadhunterCustomer); } else { jobForm.resetFields(); jobForm.setFieldsValue(next as HeadhunterJob); }
  }
  /** openEntity 先读取详情和最新版本；联系人、合同权限由服务端决定，不从列表补全。 */
  async function openEntity(item: Entity) {
    const current = ++editGeneration.current; setOpeningID(item.id); setSaveError("");
    try {
      const result = await cloudRequest(`/api/headhunter/${kind}/${encodeURIComponent(item.id)}`);
      if (current !== editGeneration.current) return;
      const next: Entity = isCustomer ? result.customer : result.job;
      if (!next?.id) throw new Error("详情没读完整，请再试一次。");
      setReadonly(!canManage(next)); setEntity(next);
      if (isCustomer) { customerForm.resetFields(); customerForm.setFieldsValue({ ...next, contacts: (next as HeadhunterCustomer).contacts || [], contracts: (next as HeadhunterCustomer).contracts || [] }); }
      else { jobForm.resetFields(); jobForm.setFieldsValue(next as HeadhunterJob); }
    } catch (cause) { if (current === editGeneration.current) notify(cause instanceof Error ? cause.message : "详情没读到，请重试。", "warning"); }
    finally { if (current === editGeneration.current) setOpeningID(""); }
  }
  /** save 保留服务端版本；权限或版本冲突时留住草稿，不把失败伪装成已保存。 */
  async function save() {
    if (!entity || readonly || saveInFlight.current) return;
    const current = editGeneration.current;
    let fields: Partial<Entity>;
    try { fields = isCustomer ? await customerForm.validateFields() : await jobForm.validateFields(); } catch { return; }
    saveInFlight.current = true; setSaving(true); setSaveError("");
    try {
      await saveHeadhunterEntity(kind, { ...entity, ...fields });
      if (current !== editGeneration.current) return;
      setEntity(null); notify(isCustomer ? "客户资料记好了。" : "甲方岗位和平台绑定已保存。", "success"); void load(); void loadOptions();
    } catch (cause) { if (current === editGeneration.current) setSaveError(cause instanceof Error ? cause.message : "没保存成功，草稿还在。请核对后重试。"); }
    finally { saveInFlight.current = false; if (current === editGeneration.current) setSaving(false); }
  }

  return <>
    <Flex gap={8} className={`goodhr-compact-toolbar ${styles.toolbar}`} data-testid={`headhunter-${kind}-toolbar`}>
      <Input aria-label={isCustomer ? "搜索客户" : "搜索甲方岗位"} prefix={<SearchOutlined aria-hidden="true" />} placeholder={isCustomer ? "公司名称、简称或编号" : "岗位名称、职责或客户"} value={q} onChange={(event) => setQ(event.target.value)} onPressEnter={() => navigateFilters()} className={styles.keyword} />
      {!isCustomer ? <Select aria-label="筛选甲方客户" allowClear showSearch optionFilterProp="label" placeholder="全部客户" value={customerID || undefined} onChange={(value) => setCustomerID(value || "")} options={customers.map((customer) => ({ value: customer.id, label: customer.name }))} className={styles.select} /> : null}
      <Select aria-label="筛选合作状态" allowClear showSearch optionFilterProp="label" placeholder="全部状态" value={status || undefined} onChange={(value) => setStatus(value || "")} options={statuses} className={styles.status} />
      <Select aria-label="筛选负责人" allowClear showSearch optionFilterProp="label" placeholder="全部负责人" value={owner || undefined} onChange={(value) => setOwner(value || "")} options={(metadata?.members || []).map((member) => ({ value: member.email, label: member.email }))} className={styles.select} />
      <Button type="primary" loading={loading} onClick={() => navigateFilters()}>查询</Button><Button onClick={() => navigateFilters(1, pageSize, true)}>重置</Button>
      <Flex gap={8} className="goodhr-toolbar-actions"><Button type="primary" icon={<PlusOutlined aria-hidden="true" />} disabled={!metadata} onClick={openCreate}>{isCustomer ? "新建客户" : "新建甲方岗位"}</Button></Flex>
    </Flex>
    {error ? <Alert type="warning" title={error} action={<Button onClick={() => void load()}>重试</Button>} style={{ marginBottom: 12 }} /> : null}
    {optionError ? <Alert type="warning" title={optionError} action={<Button onClick={() => void loadOptions()}>重试读取选项</Button>} style={{ marginBottom: 12 }} /> : null}
    {!isCustomer && metadata && !customers.length ? <Alert type="info" title={<>先建一个甲方客户，再记录客户的招聘岗位。<Link href="/admin/headhunter/customers">去客户管理</Link></>} style={{ marginBottom: 12 }} /> : null}
    <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
      <Table<Entity> rowKey="id" loading={loading} dataSource={items} pagination={false} scroll={{ x: isCustomer ? 940 : 1120 }} locale={{ emptyText: isCustomer ? "这里还没有客户，先把第一位甲方记下来。" : "还没有甲方岗位，先选客户再写招聘要求。" }} columns={[
        { title: isCustomer ? "甲方公司" : "甲方岗位", width: 210, fixed: "left", render: (_, item) => <Flex vertical gap={2}><Button type="link" className={styles.name} loading={openingID === item.id} onClick={() => void openEntity(item)}>{item.name}</Button><Typography.Text type="secondary" className={styles.meta}>{isCustomer ? (item as HeadhunterCustomer).short_name || (item as HeadhunterCustomer).code : (item as HeadhunterJob).customer_name || customers.find((customer) => customer.id === (item as HeadhunterJob).customer_id)?.name || "客户资料待核对"}</Typography.Text></Flex> },
        ...(isCustomer ? [
          { title: "行业 / 规模", width: 180, render: (_: unknown, item: Entity) => [(item as HeadhunterCustomer).industry, (item as HeadhunterCustomer).scale].filter(Boolean).join(" / ") || "待补充" },
          { title: "来源", width: 130, render: (_: unknown, item: Entity) => (item as HeadhunterCustomer).source || "未记录" },
        ] : [
          { title: "部门 / 人数", width: 140, render: (_: unknown, item: Entity) => `${(item as HeadhunterJob).department || "部门待补充"} · ${(item as HeadhunterJob).headcount || 1} 人` },
          { title: "年薪（万元）", width: 145, render: (_: unknown, item: Entity) => { const job = item as HeadhunterJob; return job.salary_min || job.salary_max ? `${job.salary_min || "待定"}–${job.salary_max || "待定"}` : "待沟通"; } },
          { title: "绑定平台岗位", width: 170, render: (_: unknown, item: Entity) => <Flex gap={4} wrap>{(item as HeadhunterJob).position_ids?.length ? (item as HeadhunterJob).position_ids.map((id) => <Tag key={id}>{metadata?.positions.find((position) => position.id === id)?.name || "已绑定岗位"}</Tag>) : <Typography.Text type="secondary">尚未绑定</Typography.Text>}</Flex> },
        ]),
        { title: "城市", dataIndex: "city", width: 110, render: (value: string) => value || "待补充" },
        { title: "状态", width: 100, render: (_, item) => <Tag>{statuses.find((option) => option.value === item.status)?.label || item.status}</Tag> },
        ...(!isCustomer ? [{ title: "优先级", width: 90, render: (_: unknown, item: Entity) => jobPriorities.find((option) => option.value === (item as HeadhunterJob).priority)?.label || "正常" }] : []),
        { title: "负责人", dataIndex: "owner_email", width: 180, render: (value: string) => <Typography.Text className={styles.meta}>{value || "未分配"}</Typography.Text> },
        { title: "操作", width: 80, fixed: "right", render: (_, item) => <Button type="link" size="small" loading={openingID === item.id} onClick={() => void openEntity(item)}>{canManage(item) ? "编辑" : "查看"}</Button> },
      ]} />
      <Flex gap={8} wrap justify="space-between" align="center" className="goodhr-table-pagination"><Typography.Text type="secondary">共 {total} {isCustomer ? "位客户" : "个岗位"}</Typography.Text><Pagination current={page} pageSize={pageSize} total={total} showSizeChanger pageSizeOptions={[10, 20, 50, 100]} onChange={(next, size) => navigateFilters(size !== pageSize ? 1 : next, size)} /></Flex>
    </SectionPanel>
    <AdminDialog open={Boolean(entity)} title={`${readonly ? "查看" : entity?.id ? "编辑" : "新建"}${isCustomer ? "客户" : "甲方岗位"}`} maxWidth="md" loading={saving} onClose={() => { editGeneration.current += 1; setEntity(null); }} onConfirm={readonly ? undefined : () => void save()} confirmText="保存资料" cancelText={readonly ? "关闭" : "取消"}>
      {saveError ? <Alert type="warning" title={saveError} style={{ marginBottom: 12 }} /> : null}
      {readonly ? <Alert type="info" title="这里展示你有权查看的资料；维护由负责人或团队管理员处理。" style={{ marginBottom: 12 }} /> : null}
      {entity && metadata ? isCustomer ? <CustomerEditor key={editGeneration.current} form={customerForm} initialValues={entity as HeadhunterCustomer} metadata={metadata} readonly={readonly || saving} ownerEditable={admin} /> : <JobEditor key={editGeneration.current} form={jobForm} initialValues={entity as HeadhunterJob} metadata={metadata} customers={customers} readonly={readonly || saving} ownerEditable={admin} /> : null}
    </AdminDialog>
  </>;
}
