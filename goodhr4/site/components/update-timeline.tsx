import type { UpdateRecord } from "@/lib/api";

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleDateString("zh-CN");
}

export function UpdateTimeline({ updates }: { updates: UpdateRecord[] }) {
  if (!updates.length) {
    return <p className="empty">暂无更新记录</p>;
  }

  return (
    <div className="timeline">
      {updates.map((item) => (
        <article key={item.id} className="timeline-item">
          <div className="timeline-meta">
            <strong>v{item.version}</strong>
            <span>{formatDate(item.published_at)}</span>
          </div>
          <h3>{item.title || `版本 ${item.version}`}</h3>
          <p>{item.content}</p>
          {item.force_update ? <em>该版本为强制更新</em> : null}
        </article>
      ))}
    </div>
  );
}
