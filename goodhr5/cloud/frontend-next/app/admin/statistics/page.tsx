/** 文件作用：展示超级管理员全站统计、整体新用户漏斗和每日消费曲线，不展示逐人列表。 */
"use client";
import { Alert, Button, DatePicker, Flex, Select, Skeleton, Tabs, Typography } from "antd";
import dayjs from "dayjs";
import { useEffect, useRef, useState } from "react";
import { useAdmin } from "@/components/admin/AdminApp";
import { EmptyState, PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { cloudRequest } from "@/lib/admin-api";
import { amountLabels, currentAmountLabels, currentLabels, metricLabels, periodOptions, shanghaiDate, statisticRate, statisticsCSV, type SystemStatistics } from "@/lib/system-statistics";
import { ConsumptionChart, StatisticsFunnel, StatisticsGroups, StatisticsMetrics } from "./statistics-charts";
import styles from "./page.module.css";

/** SystemStatisticsPage 以今天为默认范围读取全站数据，筛选变更时丢弃过期响应。 */
export default function SystemStatisticsPage() {
  const { user } = useAdmin();
  const [period, setPeriod] = useState("today");
  const [range, setRange] = useState<[string, string] | null>(null);
  const [stats, setStats] = useState<SystemStatistics | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const requestID = useRef(0);
  const query = new URLSearchParams({ period });
  if (period === "custom" && range) { query.set("start_date", range[0]); query.set("end_date", range[1]); }
  const queryString = query.toString();
  const isAdmin = user?.role === "super_admin";

  useEffect(() => {
    const id = ++requestID.current;
    if (!isAdmin || (period === "custom" && !range)) { setLoading(false); setStats(null); setError(""); return; }
    setLoading(true); setError(""); setStats(null);
    cloudRequest(`/api/admin/statistics?${queryString}`).then((data) => {
      if (id !== requestID.current) return;
      if (!data.stats?.metrics || !Array.isArray(data.stats.funnel) || !Array.isArray(data.stats.curve)) throw new Error("统计数据没读完整，请重试");
      setStats(data.stats);
    }).catch((reason) => {
      if (id === requestID.current) setError(reason instanceof Error ? reason.message : "这次统计没读成功，稍后再试试");
    }).finally(() => { if (id === requestID.current) setLoading(false); });
    return () => { requestID.current++; };
  }, [isAdmin, queryString, retry]);

  /** exportSummary 下载当前已成功读取的全站汇总，保留原始金额精度。 */
  function exportSummary() {
    if (!stats || loading) return;
    const url = URL.createObjectURL(new Blob([statisticsCSV(stats)], { type: "text/csv;charset=utf-8" }));
    const link = document.createElement("a"); link.href = url; link.download = `全站统计_${stats.start_date}_${stats.end_date}.csv`; link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }

  if (!isAdmin) return <SectionPanel><EmptyState text="只有超级管理员可以查看全站统计" /></SectionPanel>;
  return <>
    <PageHeader title="系统统计" description="看看今天忙了多少、花了多少，以及新用户卡在哪一步。" />
    <Flex className="goodhr-compact-toolbar" data-testid="statistics-toolbar">
      <Select aria-label="统计时间" value={period} options={periodOptions} onChange={setPeriod} showSearch optionFilterProp="label" style={{ width: 140 }} />
      {period === "custom" && <DatePicker.RangePicker aria-label="自定义统计日期" value={range ? [dayjs(range[0]), dayjs(range[1])] : null}
        allowClear onChange={(dates) => setRange(dates?.[0] && dates?.[1] ? [dates[0].format("YYYY-MM-DD"), dates[1].format("YYYY-MM-DD")] : null)}
        disabledDate={(date) => date.format("YYYY-MM-DD") > shanghaiDate()} className={styles.rangePicker} />}
      <Typography.Text type="secondary" className={styles.rangeLabel}>{stats ? `${stats.start_date} 至 ${stats.end_date}` : "默认统计今天"} · 北京时间</Typography.Text>
      <Flex className="goodhr-toolbar-actions"><Button onClick={exportSummary} disabled={!stats || loading}>导出汇总</Button></Flex>
    </Flex>
    {loading ? <SectionPanel><Skeleton active paragraph={{ rows: 12 }} /></SectionPanel> : error ?
      <Alert type="error" showIcon title="统计暂时没读成功" description={error} action={<Button onClick={() => setRetry(retry + 1)}>重试</Button>} /> : !stats ?
        <SectionPanel><EmptyState text="选好开始和结束日期，我就把这段时间的账和进度一起算好。" /></SectionPanel> : <>
          {stats.warnings.map((warning) => <Alert key={warning} type="warning" showIcon title={warning} className={styles.warning} />)}
          <SectionPanel className={styles.summary}>
            <StatisticsMetrics items={[
              { label: "AI消费", value: `¥${stats.amounts.consumption}`, note: "用户余额实际扣费" },
              { label: "实付收款", value: `¥${stats.amounts.receipts}`, note: `${stats.metrics.paid_orders}笔成功支付` },
              { label: "新注册用户", value: stats.metrics.registered, note: "按注册时间统计" },
              { label: "活跃用户", value: stats.metrics.active_users, note: "登录、筛选、扣费或启动本地程序的去重人数" },
              { label: "处理简历", value: stats.metrics.processed_resumes, note: "沿用全站已处理计数" },
              { label: "成功打招呼", value: stats.metrics.greeted, note: "岗位上报的成功次数" },
            ]} />
          </SectionPanel>
          <div className={styles.charts}>
            <SectionPanel><ConsumptionChart days={stats.curve} today={shanghaiDate()} /></SectionPanel>
            <SectionPanel><StatisticsFunnel steps={stats.funnel} start={stats.start_date} end={stats.end_date} /></SectionPanel>
          </div>
          <SectionPanel className={styles.details}>
            <Tabs items={[
              { key: "growth", label: "用户增长", children: <>
                <StatisticsMetrics items={["registered", "active_users", "active_teams", "screening_users", "consumers", "payers"].map((key) => ({ label: metricLabels[key], value: stats.metrics[key] }))} />
                <div className={styles.rates}>{[1, 7].map((offset) => <span key={offset}>{offset === 1 ? "次日留存" : "7日留存"} <strong>{statisticRate(stats.metrics[`retention_${offset}_returned`], stats.metrics[`retention_${offset}_eligible`])}</strong>（{stats.metrics[`retention_${offset}_eligible`] || 0}人可观察）</span>)}</div>
                <Typography.Paragraph type="secondary" className={styles.explanation}>留存按同一批注册用户在次日或第7日是否回来使用计算。观察日需完整结束且有完整采集记录；未到观察日、历史缺失均不算流失。</Typography.Paragraph>
              </> },
              { key: "usage", label: "招聘与使用", children: <>
                <StatisticsMetrics items={["new_positions", "screenings", "first_done", "first_pass", "second_done", "second_pass", "screening_failed", "new_resumes", "recommendations", "recommendation_updates", "applications", "interviews", "onboardings", "new_customers", "new_customer_jobs"].map((key) => ({ label: metricLabels[key], value: stats.metrics[key] }))} />
                <div className={styles.rates}><span>初筛通过率 <strong>{statisticRate(stats.metrics.first_pass, stats.metrics.first_done)}</strong></span><span>复筛通过率 <strong>{statisticRate(stats.metrics.second_pass, stats.metrics.second_done)}</strong></span><span>单轮平均耗时 <strong>{stats.metrics.average_screening_ms === undefined ? "—" : `${(stats.metrics.average_screening_ms / 1000).toFixed(2)}秒`}</strong></span></div>
                <div className={styles.groups}><StatisticsGroups title="各平台筛选次数" groups={stats.platforms} /><StatisticsGroups title="新增简历来源" groups={stats.sources} /></div>
                <Typography.Paragraph type="secondary" className={styles.explanation}>筛选按每次尝试去重；简历按入库主体计数，多个来源不重复累加，内置示例不计入。面试按排期日期，已取消场次不计；入职只统计已明确记录并完成流程的记录。</Typography.Paragraph>
              </> },
              { key: "finance", label: "财务对账", children: <>
                <StatisticsMetrics items={Object.entries(amountLabels).map(([key, label]) => ({ label, value: stats.amounts[key] === undefined ? undefined : `¥${stats.amounts[key]}` }))} />
                <StatisticsMetrics items={["charged_calls", "consumers", "prompt_tokens", "completion_tokens", "paid_orders", "payers", "refund_count"].map((key) => ({ label: metricLabels[key], value: stats.metrics[key] }))} />
                <StatisticsGroups title="各模型消费" groups={stats.models} money />
                <Typography.Paragraph type="secondary" className={styles.explanation}>实付收款按支付成功时间，退款按退款成功时间；净收款＝实付收款－成功退款。赠送、人工调整和AI余额消费分开统计，AI充值属于实付收款的一部分。</Typography.Paragraph>
              </> },
              { key: "runtime", label: "运行与卡点", children: <>
                <StatisticsMetrics items={["runtime_errors", "affected_users"].map((key) => ({ label: metricLabels[key], value: stats.metrics[key] }))} />
                <div className={styles.groups}><StatisticsGroups title="运行异常类型" groups={stats.errors} /><StatisticsGroups title="所选新用户当前卡点" groups={stats.blocked} /></div>
                <Typography.Paragraph type="secondary" className={styles.explanation}>这里只汇总已上报的运行异常，不把正常停止当成失败。卡点反映所选注册用户截至当前的状态。</Typography.Paragraph>
              </> },
              { key: "current", label: "当前总量", children: <>
                <StatisticsMetrics items={Object.entries(currentLabels).map(([key, label]) => ({ label, value: stats.current[key] }))} />
                <StatisticsMetrics items={Object.entries(currentAmountLabels).map(([key, label]) => ({ label, value: `¥${stats.current_amounts[key]}` }))} />
                <StatisticsGroups title="当前已绑定客户端版本" groups={stats.versions} />
                <Typography.Paragraph type="secondary" className={styles.explanation}>本栏为当前快照，不随日期筛选变化。绑定不等于在线；每个账号只统计最近有效绑定的一个版本。</Typography.Paragraph>
              </> },
            ]} />
          </SectionPanel>
          <Typography.Paragraph type="secondary" className={styles.timestamp}>数据截至 {new Intl.DateTimeFormat("zh-CN", { timeZone: "Asia/Shanghai", dateStyle: "short", timeStyle: "medium" }).format(new Date(stats.as_of))}（北京时间）</Typography.Paragraph>
        </>}
  </>;
}
