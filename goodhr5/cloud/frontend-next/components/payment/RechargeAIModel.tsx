/** 本文件在充值时读取并保存当前账号的内置AI模型，保存失败保留原配置，充值与模型切换分别记账。 */
"use client";
import { Alert, Button, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import BuiltinAIModelSelect from "@/components/admin/BuiltinAIModelSelect";
import { cloudRequest, getToken } from "@/lib/admin-api";
import { isOfficialAIURL, normalizeBuiltinAIModels, type BuiltinAIModel } from "@/lib/personal-ai-config";

/** RechargeAIModel 仅修改已确认使用内置接口的模型，不能借充值替用户静默切换自定义接口。 */
export default function RechargeAIModel({ onBusyChange, onSaved }: {
  onBusyChange: (busy: boolean) => void;
  onSaved?: (config: Record<string, unknown>) => void;
}) {
  const [models, setModels] = useState<BuiltinAIModel[] | null>(null);
  const [config, setConfig] = useState<Record<string, unknown> | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const [retry, setRetry] = useState(0);
  const savingRef = useRef(false);
  const ownerToken = useRef(getToken());
  useEffect(() => {
    let active = true;
    setLoading(true);
    setError("");
    Promise.all([cloudRequest("/api/config/user-ai"), cloudRequest("/api/ai-wallet")]).then(([ai, wallet]) => {
      if (!active || getToken() !== ownerToken.current) return;
      const choices = normalizeBuiltinAIModels((wallet.wallet || wallet).models);
      if (!ai.config || !choices.length) throw new Error("模型配置暂时没读完整，请重试。");
      setConfig(ai.config);
      setModels(choices);
    }).catch(cause => { if (active) setError(cause instanceof Error ? cause.message : "模型配置暂时没读到，请重试。"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [retry]);
  /** saveModel 串行保存选择，失败时保留原模型，防止快速点击覆盖较新的配置。 */
  async function saveModel(model: string) {
    if (!config || savingRef.current || !models?.some(item => item.id === model)) return;
    if (getToken() !== ownerToken.current) return setError("账号已变化，请重新打开充值弹框。");
    savingRef.current = true;
    setSaving(true);
    onBusyChange(true);
    setError("");
    try {
      const data = await cloudRequest("/api/config/user-ai", { method: "PUT", body: {
        base_url: config.base_url, model, api_key: config.api_key || "", temperature: config.temperature ?? 0,
        prompt_template: config.prompt_template || "", enabled: config.enabled !== false,
      } });
      if (!data.config || data.config.model !== model) throw new Error("模型切换还没确认，请重新读取后再试。");
      setConfig(data.config);
      onSaved?.(data.config);
    } catch (cause) { setError(cause instanceof Error ? cause.message : "模型没保存成功，请重试。"); }
    finally { savingRef.current = false; setSaving(false); onBusyChange(false); }
  }
  const official = config && isOfficialAIURL(String(config.base_url || ""));
  return <div style={{ marginTop: 12 }}>
    <BuiltinAIModelSelect models={models} value={String(config?.model || "")} loading={loading} disabled={!official || saving} onChange={value => void saveModel(value)} />
    {error ? <Alert type="warning" showIcon title={error} action={<Button disabled={saving || loading} onClick={() => setRetry(value => value + 1)}>重新读取模型</Button>} /> : null}
    {!loading && config && !official ? <Typography.Paragraph type="secondary">当前是自定义接口，请先到<a href="/admin/personal-config">个人配置</a>切换内置 AI。</Typography.Paragraph> : null}
    {official ? <Typography.Text type="secondary">{saving ? "正在保存模型……" : "选好即保存，下次 AI 调用使用新模型；充值金额不与模型绑定。"}</Typography.Text> : null}
  </div>;
}
