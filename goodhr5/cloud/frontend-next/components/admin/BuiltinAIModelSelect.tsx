/** 本文件提供个人配置和充值弹框共用的内置AI模型选择，统一展示说明与真实价格。 */
"use client";
import { Form, Select } from "antd";
import { useId } from "react";
import { formatBuiltinAIPrice, type BuiltinAIModel } from "@/lib/personal-ai-config";
import styles from "./BuiltinAIModelSelect.module.css";

/** BuiltinAIModelSelect 复用内置模型配置，不为不同入口另写模型名单或价格。 */
export default function BuiltinAIModelSelect({ models, value, onChange, loading = false, disabled = false }: {
  models: BuiltinAIModel[] | null;
  value: string;
  onChange: (model: string) => void;
  loading?: boolean;
  disabled?: boolean;
}) {
  const id = useId();
  const selected = models?.find(item => item.id === value);
  return <Form.Item layout="vertical" htmlFor={id} label="模型名称" style={{ marginBottom: 8 }} extra={selected ? <div className={styles.details}>
    {selected.description ? <span>{selected.description}</span> : null}
    <span>输入 {formatBuiltinAIPrice(selected.input_price_per_1m_cents)} / 输出 {formatBuiltinAIPrice(selected.output_price_per_1m_cents)} · 每百万 Token</span>
  </div> : loading ? "我正在读取内置模型列表……" : "请重新读取内置模型列表后再选择。"}>
    <Select id={id} value={selected ? value : undefined} onChange={onChange} showSearch optionFilterProp="searchText"
      style={{ width: "100%" }} classNames={{ popup: { root: styles.dropdown } }} virtual={false}
      loading={loading} disabled={disabled || loading || !models?.length} placeholder={loading ? "正在读取模型" : "暂无可选模型"}
      options={(models || []).map(model => ({ value: model.id, label: `${model.name} (${model.id})`, searchText: `${model.name} ${model.id} ${model.description}`, model }))}
      optionRender={option => <div className={styles.option}>
        <span className={styles.name}>{option.data.model.name}</span>
        <span className={styles.detail}>{option.data.model.id}</span>
        {option.data.model.description ? <span className={styles.detail}>{option.data.model.description}</span> : null}
        <span className={styles.detail}>输入 {formatBuiltinAIPrice(option.data.model.input_price_per_1m_cents)} / 输出 {formatBuiltinAIPrice(option.data.model.output_price_per_1m_cents)} · 每百万 Token</span>
      </div>} />
  </Form.Item>;
}
