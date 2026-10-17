/** 本文件提供管理员文章列表、纯文本编辑和全部公开文章链接的一键复制。 */
"use client";

import { Alert, Button, DatePicker, Flex, Form, Input, Select, Switch, Table, Tag, Typography } from "antd";
import { CopyOutlined, PlusOutlined } from "@ant-design/icons";
import dayjs, { type Dayjs } from "dayjs";
import { useCallback, useEffect, useRef, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import { useAdmin } from "@/components/admin/AdminApp";
import { EmptyState, SectionPanel } from "@/components/admin/AdminUI";
import { cloudRequest } from "@/lib/admin-api";
import { formatArticleDate } from "@/lib/article-public-utils";
import { articlePublicURL, fetchAllArticleLinks, saveArticle, type AdminArticle } from "@/lib/admin-articles";
import styles from "./page.module.css";

type EditorValues = { title: string; content: string; published_at: Dayjs; is_published: boolean };

/** ArticlesAdminPage 复用后台权限和表单管理官网文章，草稿及未来文章仅由管理端维护。 */
export default function ArticlesAdminPage() {
  const { user, notify, confirm } = useAdmin();
  const [articles, setArticles] = useState<AdminArticle[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(20);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<AdminArticle | null>(null);
  const [saving, setSaving] = useState(false);
  const [reading, setReading] = useState(false);
  const [copying, setCopying] = useState(false);
  const [manualCopy, setManualCopy] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [form] = Form.useForm<EditorValues>();
  const listRequest = useRef(0);
  const operationBusy = useRef(false);

  /** loadArticles 只让最新查询更新列表，避免快速筛选时较慢的旧请求覆盖结果。 */
  const loadArticles = useCallback(async () => {
    if (user?.role !== "super_admin") return;
    const request = ++listRequest.current;
    setLoading(true);
    setLoadError(false);
    try {
      const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) });
      if (query.trim()) params.set("q", query.trim());
      if (status) params.set("is_published", status);
      const data = await cloudRequest(`/api/admin/articles?${params}`);
      if (request !== listRequest.current) return;
      setArticles(Array.isArray(data.items) ? data.items : []);
      setTotal(Number(data.total) || 0);
      if (page > 1 && !data.items?.length && Number(data.total) <= (page - 1) * pageSize) setPage(Math.max(1, Math.ceil(Number(data.total) / pageSize)));
    } catch (error) {
      if (request === listRequest.current) {
        setLoadError(true);
        setArticles([]);
        setTotal(0);
        notify(error instanceof Error ? error.message : "文章暂时没读到，请重试。", "error");
      }
    } finally {
      if (request === listRequest.current) setLoading(false);
    }
  }, [user?.role, page, pageSize, query, status, notify]);

  useEffect(() => {
    void loadArticles();
    return () => { listRequest.current++; };
  }, [loadArticles]);

  /** openEditor 读取完整正文再编辑，列表摘要不会覆盖原文章内容。 */
  async function openEditor(article?: AdminArticle) {
    if (operationBusy.current) return;
    operationBusy.current = true;
    setReading(true);
    try {
      const full = article ? (await cloudRequest(`/api/admin/articles/${encodeURIComponent(article.id)}`)).article as AdminArticle : null;
      setEditing(full);
      form.setFieldsValue({ title: full?.title || "", content: full?.content || "", published_at: full ? dayjs(full.published_at) : dayjs(), is_published: full?.is_published || false });
      setEditorOpen(true);
    } catch (error) {
      notify(error instanceof Error ? error.message : "正文还没读到，请再试一次。", "error");
    } finally {
      operationBusy.current = false;
      setReading(false);
    }
  }

  /** submitArticle 在校验完成后保存正文与时间，失败时保留编辑草稿。 */
  async function submitArticle() {
    if (operationBusy.current) return;
    let values: EditorValues;
    try { values = await form.validateFields(); } catch { return; }
    if (operationBusy.current) return;
    operationBusy.current = true;
    setSaving(true);
    try {
      await saveArticle(editing?.id, { title: values.title.trim(), content: values.content, published_at: values.published_at.toISOString(), is_published: values.is_published });
      setEditorOpen(false);
      notify("文章已保存", "success");
      await loadArticles();
    } catch (error) {
      notify(error instanceof Error ? error.message : "没保存成功，正文还在，可以再试。", "error");
    } finally {
      operationBusy.current = false;
      setSaving(false);
    }
  }

  /** deleteArticle 确认后删除指定文章，并同步更新当前分页。 */
  async function deleteArticle(article: AdminArticle) {
    if (deleting || !(await confirm("删除文章", `确认删除《${article.title}》？官网链接也会一起下线。`))) return;
    setDeleting(article.id);
    try {
      await cloudRequest(`/api/admin/articles/${encodeURIComponent(article.id)}`, { method: "DELETE" });
      notify("文章已删除", "success");
      await loadArticles();
    } catch (error) {
      notify(error instanceof Error ? error.message : "没删成功，可以再试。", "error");
    } finally { setDeleting(null); }
  }

  /** copyAllLinks 一次导出全部公开文章链接；剪贴板不可用时保留文本供手动复制。 */
  async function copyAllLinks() {
    if (copying) return;
    setCopying(true);
    try {
      const links = await fetchAllArticleLinks();
      if (!links) { notify("还没有已公开的文章，发布一篇后再来复制。", "info"); return; }
      try {
        await navigator.clipboard.writeText(links);
        notify(`已复制 ${links.split("\n").length} 条文章链接，每行一条`, "success");
      } catch {
        setManualCopy(links);
        notify("剪贴板暂时没接住，可以在弹框里全选复制。", "info");
      }
    } catch (error) {
      notify(error instanceof Error ? error.message : "链接没读到，请再试一次。", "error");
    } finally { setCopying(false); }
  }

  if (user?.role !== "super_admin") return <SectionPanel><EmptyState text="只有超级管理员可以管理官网文章" /></SectionPanel>;

  return <>
    <SectionPanel className={styles.table}>
      <Flex className={`goodhr-compact-toolbar ${styles.toolbar}`} align="center" justify="space-between" gap={8}>
        <Flex className={styles.filters} gap={8} wrap>
          <Input.Search className={styles.search} aria-label="搜索文章标题" placeholder="搜索文章标题" maxLength={200} allowClear onSearch={(value) => { setPage(1); setQuery(value); }} enterButton="查询" />
          <Select className={styles.status} aria-label="文章发布状态" value={status} onChange={(value) => { setPage(1); setStatus(value); }} options={[{ value: "", label: "全部状态" }, { value: "true", label: "公开" }, { value: "false", label: "草稿" }]} />
        </Flex>
        <Flex className="goodhr-toolbar-actions" gap={8} wrap>
          <Button icon={<CopyOutlined aria-hidden="true" />} loading={copying} onClick={() => void copyAllLinks()}>复制全部文章链接</Button>
          <Button type="primary" icon={<PlusOutlined aria-hidden="true" />} loading={reading} onClick={() => void openEditor()}>新建文章</Button>
        </Flex>
      </Flex>
      {loadError ? <Alert type="error" title="文章列表暂时没读到" action={<Button onClick={() => void loadArticles()}>重试</Button>} /> : null}
      <Table<AdminArticle> rowKey="id" dataSource={articles} loading={loading} scroll={{ x: 700 }} pagination={{ current: page, pageSize, total, showSizeChanger: true, showTotal: (count) => `共 ${count} 篇`, onChange: (next, size) => { setPage(size !== pageSize ? 1 : next); setPageSize(size); } }} locale={{ emptyText: loadError ? "读取失败，请稍后重试。" : "还没有文章，点右上角写第一篇。" }} columns={[
        { title: "标题", dataIndex: "title", width: 320, render: (title: string, article) => <Button type="link" className={styles.title} disabled={reading} onClick={() => void openEditor(article)}>{title}</Button> },
        { title: "状态", width: 90, render: (_, article) => <Tag color={article.is_published ? "green" : "default"}>{!article.is_published ? "草稿" : Date.parse(article.published_at) > Date.now() ? "待发布" : "已发布"}</Tag> },
        { title: "发布时间", dataIndex: "published_at", width: 150, render: formatArticleDate },
        { title: "操作", width: 170, render: (_, article) => <Flex gap={8}>
          {article.is_published && Date.parse(article.published_at) <= Date.now() ? <Typography.Link href={articlePublicURL(article.id)} target="_blank" rel="noopener noreferrer">查看</Typography.Link> : null}
          <Button type="link" disabled={reading} onClick={() => void openEditor(article)}>编辑</Button>
          <Button type="link" danger loading={deleting === article.id} disabled={Boolean(deleting)} onClick={() => void deleteArticle(article)}>删除</Button>
        </Flex> },
      ]} />
    </SectionPanel>
    <AdminDialog open={editorOpen} title={editing ? "编辑文章" : "新建文章"} maxWidth="md" loading={saving} onClose={() => setEditorOpen(false)} onConfirm={() => void submitArticle()}>
      <Form form={form} layout="vertical" requiredMark={false}>
        <Form.Item name="title" label="标题" rules={[{ required: true, whitespace: true, message: "给文章起个标题吧" }]}><Input maxLength={200} showCount /></Form.Item>
        <Form.Item name="content" label="正文" rules={[{ required: true, whitespace: true, message: "正文还空着，写几句再保存" }]}><Input.TextArea rows={12} maxLength={200000} placeholder="直接输入正文，回车换行会保留。" /></Form.Item>
        <Flex gap={16} wrap>
          <Form.Item name="published_at" label="发布时间" rules={[{ required: true, message: "选一下发布时间" }]}><DatePicker showTime format="YYYY-MM-DD HH:mm" /></Form.Item>
          <Form.Item name="is_published" label="公开发布" valuePropName="checked" tooltip="开启后在官网公开，未来时间会到点可见。"><Switch checkedChildren="公开" unCheckedChildren="草稿" /></Form.Item>
        </Flex>
      </Form>
    </AdminDialog>
    <AdminDialog open={manualCopy !== null} title="全部公开文章链接" onClose={() => setManualCopy(null)} showCancel={false}>
      <Input.TextArea className={styles.copyText} aria-label="全部公开文章链接" readOnly rows={10} value={manualCopy || ""} onFocus={(event) => event.target.select()} />
    </AdminDialog>
  </>;
}
