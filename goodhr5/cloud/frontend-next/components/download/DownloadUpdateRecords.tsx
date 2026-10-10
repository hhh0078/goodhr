/** 本文件负责下载页更新记录的近期展示与完整历史展开，首次渲染保留服务端正文。 */
"use client";

import Button from "antd/es/button";
import { useId, useState } from "react";
import type { LocalAgentUpdate } from "@/lib/public-data";
import styles from "./download.module.css";

const RECENT_UPDATE_COUNT = 3;

/** DownloadUpdateRecords 默认展示最近三条更新，展开时展示后台提供的全部历史。 */
export default function DownloadUpdateRecords({ updates }: { updates: LocalAgentUpdate[] }) {
  const [expanded, setExpanded] = useState(false);
  const recordsId = useId();
  const visibleUpdates = expanded ? updates : updates.slice(0, RECENT_UPDATE_COUNT);

  /** toggleRecords 切换完整更新历史的展示状态，不改变更新记录或下载配置。 */
  function toggleRecords() {
    setExpanded(!expanded);
  }

  return (
    <section className="public-download-26" aria-labelledby={`${recordsId}-heading`}>
      <h2 id={`${recordsId}-heading`} className="public-text public-download-27">本地程序更新记录</h2>
      {updates.length > 0 ? (
        <div id={recordsId} className="public-download-28">
          {visibleUpdates.map((item, index) => (
            <article key={`${item.version}-${index}`} className="public-download-29">
              <div className="public-download-30">
                <h3 className="public-text public-download-31">{item.version || "未标版本"}</h3>
                {index === 0 ? <p className="public-text public-download-32">最新安装包</p> : null}
              </div>
              <p className="public-text public-download-33">
                {item.note || "这版比较低调，暂时没有写更新说明。"}
              </p>
            </article>
          ))}
        </div>
      ) : (
        <p className="public-text public-download-34">这里暂时空空的，安装包还在路上。</p>
      )}
      {updates.length > RECENT_UPDATE_COUNT ? (
        <div className={styles.updateActions}>
          <Button onClick={toggleRecords} aria-expanded={expanded} aria-controls={recordsId}>
            {expanded ? "收起" : "展示更多"}
          </Button>
        </div>
      ) : null}
    </section>
  );
}
