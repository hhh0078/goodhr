/** 文件作用：用原生SVG和后台主题绘制消费曲线、整体漏斗及聚合分布，不增加图表依赖。 */
"use client";
import { Empty, Tooltip, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { dimensionLabel, statisticRate, type ConsumptionDay, type FunnelStep, type StatisticGroup } from "@/lib/system-statistics";
import styles from "./page.module.css";

/** StatisticsMetrics 展示紧凑汇总数字，未知与实际零严格区分。 */
export function StatisticsMetrics({ items }: { items: Array<{ label: string; value: string | number | undefined; note?: string }> }) {
  return <div className={styles.metrics}>{items.map((item) => <div className={styles.metric} key={item.label}>
    <Tooltip title={item.note}><span className={styles.metricLabel}>{item.label}</span></Tooltip>
    <strong className={styles.metricValue}>{item.value === undefined ? "—" : typeof item.value === "number" ? item.value.toLocaleString("zh-CN") : item.value}</strong>
    {item.note && <span className={styles.metricNote}>{item.note}</span>}
  </div>)}</div>;
}

/** ConsumptionChart 按真实日消费金额绘制折线，包含零值与单点，精确金额通过提示和下方数字展示。 */
export function ConsumptionChart({ days, today }: { days: ConsumptionDay[]; today: string }) {
  const svgRef = useRef<SVGSVGElement>(null);
  const [width, setWidth] = useState(720);
  useEffect(() => {
    if (!svgRef.current) return;
    const observer = new ResizeObserver((entries) => setWidth(Math.max(280, Math.round(entries[0].contentRect.width))));
    observer.observe(svgRef.current);
    return () => observer.disconnect();
  }, []);
  const height = width < 400 ? 220 : 250, left = 62, right = 22, top = 20, bottom = 40;
  const values = days.map((day) => Number(day.amount));
  const max = Math.max(...values, 0) || 1;
  const points = days.map((day, index) => ({ ...day, x: days.length === 1 ? width / 2 : left + index / (days.length - 1) * (width - left - right), y: height - bottom - values[index] / max * (height - top - bottom) }));
  const stride = Math.max(1, Math.ceil(days.length / 7));
  const totalLabel = days.length === 7 ? "近7天 · 每天一个点" : `${days.length}天 · 每天一个点`;
  return <figure className={styles.figure} data-testid="consumption-chart">
    <figcaption className={styles.chartHeading}><Typography.Title level={5}>AI消耗金额</Typography.Title><Typography.Text type="secondary">{totalLabel}</Typography.Text></figcaption>
    <Typography.Paragraph type="secondary" className={styles.chartDescription}>{days[0]?.date} 至 {days.at(-1)?.date} · 单位：元</Typography.Paragraph>
    <svg ref={svgRef} viewBox={`0 0 ${width} ${height}`} className={styles.lineChart} role="img" aria-label="每天AI消耗金额曲线">
      <title>每天AI消耗金额曲线，精确金额见下方每日数据</title>
      {[0, 0.25, 0.5, 0.75, 1].map((fraction) => {
        const y = height - bottom - fraction * (height - top - bottom);
        return <g key={fraction}><line x1={left} x2={width - right} y1={y} y2={y} className={styles.gridLine} /><text x={left - 10} y={y + 4} textAnchor="end" className={styles.axis}>{max >= 10000 ? new Intl.NumberFormat("zh-CN", { notation: "compact", maximumFractionDigits: 1 }).format(max * fraction) : (max * fraction).toFixed(max < 0.01 ? 4 : max < 1 ? 3 : 2)}</text></g>;
      })}
      <polyline fill="none" points={points.map((point) => `${point.x},${point.y}`).join(" ")} className={styles.line} />
      {points.map((point, index) => <g key={point.date}>
        <Tooltip title={`${point.date}${point.date === today ? "（截至当前）" : ""} · ¥${point.amount} · ${point.calls}笔`}>
          <circle cx={point.x} cy={point.y} r={days.length > 60 ? 3 : 5} className={styles.point} tabIndex={0} aria-label={`${point.date} 消耗${point.amount}元，${point.calls}笔`} />
        </Tooltip>
        {(index % stride === 0 || index === points.length - 1) && <text x={point.x} y={height - 12} textAnchor="middle" className={styles.axis}>{point.date.slice(5)}</text>}
      </g>)}
    </svg>
    {values.every((value) => value === 0) && <Typography.Text type="secondary">这段时间还没有消费记录，账本先歇一会儿。</Typography.Text>}
    <details className={styles.dailyDetails}><summary>查看每天金额</summary><div className={styles.dailyRows}>{days.map((day) => <div key={day.date}><span>{day.date}{day.date === today ? "（截至当前）" : ""}</span><strong>¥{day.amount}</strong><span>{day.calls}笔</span></div>)}</div></details>
  </figure>;
}

/** StatisticsFunnel 显示所选注册批次逐步完成的人数，零分母不制造转化率，不展示个人名单。 */
export function StatisticsFunnel({ steps, start, end }: { steps: FunnelStep[]; start: string; end: string }) {
  const registered = steps[0]?.count || 0;
  return <figure className={styles.figure} data-testid="statistics-funnel">
    <figcaption className={styles.chartHeading}><Typography.Title level={5}>新用户使用漏斗</Typography.Title><Typography.Text type="secondary">全站汇总</Typography.Text></figcaption>
    <Typography.Paragraph type="secondary" className={styles.chartDescription}>{start === end ? start : `${start} 至 ${end}`}注册的同一批用户，截至当前</Typography.Paragraph>
    {!registered && <Typography.Paragraph type="secondary">这段时间没有新注册用户；下方各步骤人数均为0。</Typography.Paragraph>}
    <ol className={styles.funnel}>{steps.map((step, index) => <li key={step.key}>
      <div className={styles.funnelLabel}><span>{step.label}</span><span><strong>{step.count.toLocaleString("zh-CN")}</strong> 人</span></div>
      <div className={styles.funnelLine}>
        <div className={styles.funnelTrack}><div className={styles.funnelBar} style={{ width: registered ? `${Math.min(100, step.count / registered * 100)}%` : "0%", opacity: 1 - index * 0.055 }} /></div>
        <Tooltip title={index ? `相对上一步：${statisticRate(step.count, steps[index - 1]?.count)}` : "相对所选注册用户"}><span className={styles.funnelRate}>{statisticRate(step.count, registered)}</span></Tooltip>
      </div>
    </li>)}</ol>
    <Typography.Paragraph type="secondary" className={styles.explanation}>人数按账号去重、按步骤依次统计。右侧为相对注册人数的转化率，悬停可看上一步转化率。</Typography.Paragraph>
  </figure>;
}

/** StatisticsGroups 展示单一维度聚合分布，模型消费同时显示金额与扣费笔数。 */
export function StatisticsGroups({ title, groups, money = false }: { title: string; groups: StatisticGroup[]; money?: boolean }) {
  const max = Math.max(...groups.map((group) => money ? Number(group.amount || 0) : group.count), 0) || 1;
  return <div className={styles.group}><Typography.Title level={5}>{title}</Typography.Title>
    {!groups.length ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这段时间暂无记录" /> : <ul className={styles.groupList}>{groups.map((group) => <li key={group.name}>
      <div className={styles.groupLabel}><span>{dimensionLabel(group.name)}</span><strong>{money ? `¥${group.amount}` : group.count.toLocaleString("zh-CN")}{money && <small> · {group.count}笔</small>}</strong></div>
      <div className={styles.groupTrack}><div style={{ width: `${(money ? Number(group.amount || 0) : group.count) / max * 100}%` }} /></div>
    </li>)}</ul>}
  </div>;
}
