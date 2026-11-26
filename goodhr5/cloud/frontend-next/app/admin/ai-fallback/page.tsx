/** 文件作用：让超级管理员单独管理备用AI，密钥不回显，读取失败或版本冲突时禁止覆盖配置。 */
"use client";

import { useEffect, useState } from "react";
import { Alert, Button, Flex, Form, Input, Spin, Switch, Typography } from "antd";
import { SaveOutlined } from "@ant-design/icons";
import { useAdmin } from "@/components/admin/AdminApp";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { APIRequestError, cloudRequest } from "@/lib/admin-api";

type FallbackDraft = { enabled: boolean; base_url: string; api_key: string; model: string };
type FallbackSnapshot = { config: Omit<FallbackDraft, "api_key"> & { api_key_set: boolean }; revision: string };

/** fallbackSnapshot 校验完整读取结果，只取密钥设置状态，不接收服务端可能附带的密钥正文。 */
function fallbackSnapshot(value: unknown): FallbackSnapshot {
  const data = value as Partial<FallbackSnapshot> | null;
  if (!data?.config || typeof data.revision !== "string" || !data.revision.trim() ||
    typeof data.config.enabled !== "boolean" || typeof data.config.base_url !== "string" ||
    typeof data.config.model !== "string" || typeof data.config.api_key_set !== "boolean") {
    throw new Error("备用AI配置没读完整，请重新打开此页后再试");
  }
  return { config: { enabled: data.config.enabled, base_url: data.config.base_url, model: data.config.model, api_key_set: data.config.api_key_set }, revision: data.revision };
}

/** AIFallbackPage 读取并保存独立备用配置，保留原密钥与服务端版本保护，不发起模型测试调用。 */
export default function AIFallbackPage() {
  const { user, notify } = useAdmin();
  const [form] = Form.useForm<FallbackDraft>();
  const enabled = Form.useWatch("enabled", form);
  const [loading, setLoading] = useState(true);
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);
  const [revision, setRevision] = useState("");
  const [apiKeySet, setAPIKeySet] = useState(false);
  const [error, setError] = useState("");
  const [conflict, setConflict] = useState(false);
  const isSuperAdmin = user?.role === "super_admin";

  useEffect(() => {
    if (!isSuperAdmin) return;
    let active = true;
    const controller = new AbortController();
    setLoading(true);
    setLoaded(false);
    setConflict(false);
    setError("");
    cloudRequest("/api/admin/system/ai-fallback", { signal: controller.signal })
      .then(fallbackSnapshot)
      .then((snapshot) => {
        if (!active) return;
        form.setFieldsValue({ enabled: snapshot.config.enabled, base_url: snapshot.config.base_url, model: snapshot.config.model, api_key: "" });
        setRevision(snapshot.revision);
        setAPIKeySet(snapshot.config.api_key_set);
        setLoaded(true);
        setError("");
      })
      .catch((cause) => {
        if (active) setError(cause instanceof Error ? cause.message : "备用AI暂时没读出来，请重新打开此页后再试");
      })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; controller.abort(); };
  }, [form, isSuperAdmin, user?.id]);

  /** save 带读取版本提交完整三项配置；密钥留空保留旧值，冲突后必须重新核对服务端内容。 */
  async function save(values: FallbackDraft) {
    if (!loaded || !isSuperAdmin || conflict || saving) return;
    setSaving(true);
    setError("");
    try {
      const snapshot = fallbackSnapshot(await cloudRequest("/api/admin/system/ai-fallback", {
        method: "PUT", body: { enabled: values.enabled, base_url: values.base_url.trim(), api_key: values.api_key.trim(), model: values.model.trim(), revision },
      }));
      form.setFieldsValue({ enabled: snapshot.config.enabled, base_url: snapshot.config.base_url, model: snapshot.config.model, api_key: "" });
      setRevision(snapshot.revision);
      setAPIKeySet(snapshot.config.api_key_set);
      notify("备用AI已保存", "success");
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "备用AI没保存成功，可以稍后重试";
      if (cause instanceof APIRequestError && cause.status === 409) setConflict(true);
      setError(message);
      notify(message, "error");
    } finally {
      setSaving(false);
    }
  }

  if (!isSuperAdmin) return <SectionPanel><EmptyState text="只有超级管理员可以查看和设置备用AI" /></SectionPanel>;
  return <>
    <PageHeader title="备用AI" description="主用暂时接不上时，让备用接个班。" actions={
      <Button type="primary" htmlType="submit" form="ai-fallback-form" icon={<SaveOutlined aria-hidden="true" />} loading={saving} disabled={!loaded || loading || conflict}>保存配置</Button>
    } />
    <SectionPanel>
      <Flex vertical gap={12} style={{ width: "100%", maxWidth: 640, minWidth: 0 }}>
        <Typography.Paragraph type="secondary" style={{ margin: 0, fontSize: 12 }}>
          主用请求失败且允许重试时，后续尝试会一起切换到备用接口、密钥和模型。未启用或配置不完整时不会切换；备用沿用当前模型的计费规则。
        </Typography.Paragraph>
        {loading ? <Flex gap={8} align="center" role="status"><Spin size="small" /><span>正在读取备用配置…</span></Flex> : null}
        {error ? <Alert type="error" showIcon title={error} description={!loaded || conflict ? <Button type="link" href="/admin/ai-fallback" style={{ padding: 0 }}>重新打开配置</Button> : undefined} /> : null}
        <Form<FallbackDraft> id="ai-fallback-form" form={form} layout="vertical" initialValues={{ enabled: false, base_url: "", api_key: "", model: "" }} onFinish={save} disabled={!loaded || loading || saving || conflict} scrollToFirstError validateTrigger="onBlur">
          <Form.Item name="enabled" label="启用备用AI" valuePropName="checked" style={{ marginBottom: 12 }}>
            <Switch aria-label="启用备用AI" checkedChildren="已启用" unCheckedChildren="未启用" />
          </Form.Item>
          <Form.Item name="base_url" label="API 地址" style={{ marginBottom: 12 }} rules={[
            { required: Boolean(enabled), whitespace: true, message: "启用前请填好备用接口地址" },
            { validator: async (_, value: string) => {
              if (!value?.trim()) return;
              try {
                const url = new URL(value.trim());
                if (!["http:", "https:"].includes(url.protocol) || !url.hostname || url.username || url.password || url.search || url.hash) throw new Error();
              } catch { throw new Error("请填完整的HTTP或HTTPS地址，不要带账号、密钥或查询参数"); }
            } },
          ]}>
            <Input autoComplete="off" inputMode="url" maxLength={2048} placeholder="https://api.example.com/v1/chat/completions" />
          </Form.Item>
          <Form.Item name="api_key" label="API 密钥" style={{ marginBottom: 12 }} extra={<span style={{ fontSize: 12 }}>{apiKeySet ? "已配置。留空会保留原密钥，填写后才会替换。" : "尚未配置。启用前请填写密钥，保存后不会回显。"}</span>} rules={[{ required: Boolean(enabled) && !apiKeySet, whitespace: true, message: "启用前请填好备用密钥" }]}>
            <Input.Password autoComplete="new-password" maxLength={8192} placeholder={apiKeySet ? "留空保留已配置密钥" : "填写备用接口密钥"} />
          </Form.Item>
          <Form.Item name="model" label="模型名称" style={{ marginBottom: 0 }} rules={[{ required: Boolean(enabled), whitespace: true, message: "启用前请填好备用模型名称" }]}>
            <Input autoComplete="off" maxLength={200} placeholder="填写备用接口支持的模型名称" />
          </Form.Item>
        </Form>
      </Flex>
    </SectionPanel>
  </>;
}
