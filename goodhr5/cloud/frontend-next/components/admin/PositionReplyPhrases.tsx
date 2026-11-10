/** 文件作用说明：在岗位设置中按场景收纳话术，编辑岗位覆盖并允许恢复后台默认。 */
"use client";

import { DeleteOutlined, DownOutlined, EditOutlined, PlusOutlined, UndoOutlined } from "@ant-design/icons";
import { Button, Flex, Input } from "antd";
import { useState } from "react";
import type { PositionAutoReplyConfig, ReplyPhrase, ReplyPhraseOverride } from "@/lib/auto-reply";
import { effectiveReplyPhrases, replacePhraseOverride } from "@/lib/auto-reply-phrases";
import ReplyPhraseEditor from "./ReplyPhraseEditor";
import styles from "./position-auto-reply.module.css";

type Props = {
  config: PositionAutoReplyConfig;
  onChange: (overrides: ReplyPhraseOverride[]) => void;
  onLegacyChange: (message: string) => void;
};

/** PositionReplyPhrases 保持可见列表与待保存覆盖一致，单独收纳岗位已移除的默认条目。 */
export default function PositionReplyPhrases({ config, onChange, onLegacyChange }: Props) {
  const [editing, setEditing] = useState<ReplyPhrase | null>(null);
  const defaults = new Map(config.default_phrases.map(row => [row.id, row]));
  const overrides = config.phrase_overrides;
  const phrases = effectiveReplyPhrases(config.default_phrases, overrides);
  const categories = Array.from(new Set(phrases.map(row => row.category)));
  const hidden = config.default_phrases.filter(row => overrides.some(item => item.id === row.id && item.deleted === true));
  /** reset 移除岗位覆盖，重新继承当前的后台默认话术。 */
  function reset(id: string) {
    onChange(replacePhraseOverride(overrides, id));
  }
  /** save 在内容与默认值相同时恢复继承，避免日后错过默认话术更新。 */
  function save(phrase: ReplyPhrase) {
    const base = defaults.get(phrase.id);
    if (base && base.category === phrase.category && base.title === phrase.title && base.scene === phrase.scene && base.content === phrase.content) reset(phrase.id);
    else onChange(replacePhraseOverride(overrides, phrase.id, phrase));
    setEditing(null);
  }
  return <section className={styles.section}>
    <details className={styles.library}>
      <summary><span className={styles.sectionTitle}>沟通话术</span><span className={styles.count}>{config.phrases_version ? phrases.length : 1} 条</span><DownOutlined className={styles.chevron} aria-hidden /></summary>
      {config.phrases_version ? <>
        <p className={styles.muted}>内容留空时，该场景不发文字；原生资料请求仍按现有规则处理。</p>
        <div className={styles.tools}><Button icon={<PlusOutlined />} disabled={phrases.length >= 64}
          onClick={() => setEditing({ id: "job_" + crypto.randomUUID(), title: "", category: "", scene: "", content: "" })}>新增话术</Button></div>
        {categories.map(category => <section className={styles.category} key={category}>
          <h4>{category}</h4>
          {phrases.filter(row => row.category === category).map(row => {
            const changed = overrides.some(item => item.id === row.id);
            return <div key={row.id} className={styles.phraseRow}>
              <div className={styles.phraseBody}><strong>{row.title}</strong><p>{row.content.trim() ? row.content : "留空，不发文字"}</p></div>
              <Flex align="center" gap={4}>
                <Button type="text" icon={<EditOutlined />} title="编辑" aria-label={'编辑话术：' + row.title} onClick={() => setEditing(row)} />
                {changed && defaults.has(row.id) && <Button type="text" icon={<UndoOutlined />} title="恢复默认" aria-label={'恢复默认：' + row.title} onClick={() => reset(row.id)} />}
                <Button type="text" icon={<DeleteOutlined />} title="从本岗位移除" aria-label={'移除话术：' + row.title}
                  onClick={() => onChange(replacePhraseOverride(overrides, row.id, { ...row, content: "", deleted: true }))} />
              </Flex>
            </div>;
          })}
        </section>)}
        {!phrases.length && <p className={styles.muted}>还没有话术，可以添加本岗位常用的回复。</p>}
        {hidden.length > 0 && <details className={styles.removed}><summary>已移除的默认话术（{hidden.length}）</summary>
          {hidden.map(row => <div className={styles.phraseRow} key={row.id}><span>{row.title}</span><Button type="link" onClick={() => reset(row.id)} aria-label={'恢复话术：' + row.title}>恢复</Button></div>)}
        </details>}
      </> : <label className={styles.legacy}>索要简历<Input.TextArea aria-label="索要简历话术" value={config.resume_request_message} autoSize={{ minRows: 2, maxRows: 5 }}
        onChange={event => onLegacyChange(event.target.value)} /></label>}
    </details>
    {editing && <ReplyPhraseEditor key={editing.id} phrase={editing} categories={Array.from(new Set([...config.default_phrases.map(row => row.category), ...categories]))}
      onClose={() => setEditing(null)} onSave={save} />}
  </section>;
}
