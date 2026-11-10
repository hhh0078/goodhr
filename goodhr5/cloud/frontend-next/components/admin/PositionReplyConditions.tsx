/** 文件作用说明：让 HR 勾选岗位原文、常用条件或 AI 建议，保留老条件的编号与类型。 */
"use client";

import { DownOutlined, PlusOutlined, ThunderboltOutlined } from "@ant-design/icons";
import { Alert, Button, Checkbox, Flex, Select } from "antd";
import { useEffect, useRef, useState } from "react";
import { suggestPositionConditions, type ConditionSuggestion, type PositionReplyCondition } from "@/lib/auto-reply";
import { conditionContentKey } from "@/lib/auto-reply-rules";
import styles from "./position-auto-reply.module.css";

const typeOptions = [{ value: "required", label: "必须满足" }, { value: "confirm", label: "需要确认" }, { value: "bonus", label: "加分项" }];
type Props = {
  positionID: string;
  requirement: string;
  description: string;
  conditions: PositionReplyCondition[];
  presets: string[];
  onChange: (conditions: PositionReplyCondition[]) => void;
};

/** rawConditionOptions 按原岗位要求的行和分号拆分，不把普通文字或优先项直接设成必须满足。 */
function rawConditionOptions(requirement: string): ConditionSuggestion[] {
  return requirement.split(/[\r\n；;]+/).flatMap(line => {
    const content = line.replace(/^\s*(?:[-•·*]|\d+[、.)）])\s*/, "").trim();
    if (!content || !conditionContentKey(content) || [...content].length > 1000) return [];
    return [{ content, type: /优先|加分/.test(content) ? "bonus" : "confirm" }];
  });
}

/** PositionReplyConditions 把生成结果留在选择区，等待 HR 勾选；关闭或切岗会取消本次生成。 */
export default function PositionReplyConditions({ positionID, requirement, description, conditions, presets, onChange }: Props) {
  const [open, setOpen] = useState(false);
  const [generating, setGenerating] = useState(false);
  const [result, setResult] = useState<ConditionSuggestion[] | null>(null);
  const [error, setError] = useState("");
  const pending = useRef<AbortController | null>(null);
  useEffect(() => {
    setGenerating(false);
    setResult(null);
    setError("");
    return () => { pending.current?.abort(); pending.current = null; };
  }, [positionID, description, requirement]);
  const chosen = conditions.filter(row => row.enabled);
  // 先采用原岗位条件和 AI 给出的分类，重复出现的公共预设只占一个名额。
  const options = new Map<string, ConditionSuggestion & { group: string }>();
  const groups: [string, ConditionSuggestion[]][] = [
    ["AI 建议", result || []], ["岗位要求", rawConditionOptions(requirement)], ["常用条件", presets.map(content => ({ content, type: "confirm" }))],
    ["已配置过", conditions],
  ];
  for (const [group, groupRows] of groups) {
    for (const row of groupRows) {
      const key = conditionContentKey(row.content);
      if (key && !options.has(key)) options.set(key, { ...row, group });
    }
  }
  /** toggle 按内容匹配已有稳定编号；取消已保存条件时停用原记录，新草稿可直接撤回。 */
  function toggle(option: ConditionSuggestion, checked: boolean) {
    const key = conditionContentKey(option.content);
    const row = conditions.find(item => conditionContentKey(item.content) === key);
    if (row) {
      if (!checked && !row.id) onChange(conditions.filter(item => item !== row));
      else onChange(conditions.map(item => item === row ? { ...item, enabled: checked } : item));
    } else if (checked) {
      onChange([...conditions, { id: "", content: option.content, type: option.type, enabled: true, sort_order: conditions.length }]);
    }
  }
  /** generate 对同一次点击只发出一个请求，失败或取消都保留 HR 已经勾选的条件。 */
  async function generate() {
    if (pending.current) return;
    const controller = new AbortController();
    pending.current = controller;
    setOpen(true); setGenerating(true); setError("");
    try {
      const rows = await suggestPositionConditions(positionID, description, controller.signal);
      if (!controller.signal.aborted) setResult(rows);
    } catch (cause) {
      if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : "这次没生成出来，可以再试一次。");
    } finally {
      if (pending.current === controller) { pending.current = null; setGenerating(false); }
    }
  }
  return <section className={styles.section}>
    <div className={styles.heading}><h3>岗位条件</h3><Flex wrap gap={8} className={styles.actions}>
      <Button icon={open ? <DownOutlined /> : <PlusOutlined />} aria-expanded={open} onClick={() => setOpen(!open)}>添加条件</Button>
      <Button icon={<ThunderboltOutlined />} loading={generating} aria-busy={generating} onClick={() => void generate()}>根据 AI 生成</Button>
    </Flex></div>
    {chosen.length ? <div className={styles.selectedConditions}>{chosen.map(row =>
      <div key={conditionContentKey(row.content)} className={styles.conditionRow}>
        <Checkbox checked onChange={() => toggle(row, false)}>{row.content}</Checkbox>
        <Select aria-label={row.content + "的条件类型"} value={row.type} options={typeOptions}
          onChange={(type: PositionReplyCondition["type"]) => onChange(conditions.map(item => item === row ? { ...item, type } : item))} />
      </div>)}</div> : <span className={styles.muted}>尚未选择岗位条件</span>}
    {open && <div className={styles.picker}>
      {error && <Alert type="warning" showIcon title={error} />}
      {generating && <span className={styles.muted} role="status">正在根据岗位资料生成条件…</span>}
      {result?.length === 0 && !generating && !error && <p className={styles.muted} role="status">现有资料中没有可以建议的条件。</p>}
      {groups.map(([label]) => {
        const rows = [...options.values()].filter(row => row.group === label);
        if (!rows.length) return null;
        return <fieldset key={label} className={styles.optionGroup}><legend>{label}</legend>
          {rows.map(row => <Checkbox key={row.content} checked={chosen.some(chosenRow => conditionContentKey(chosenRow.content) === conditionContentKey(row.content))}
            onChange={event => toggle(row, event.target.checked)}>{row.content}</Checkbox>)}
        </fieldset>;
      })}
      {!options.size && !error && !generating && !result && <p className={styles.muted}>还没有可选条件，补充岗位要求后可用 AI 生成。</p>}
    </div>}
  </section>;
}
