/** 本文件负责展示服务端准备好的官网公开统计数据。 */

import type { PublicStatsData } from "@/lib/public-data";

/** PublicStats 展示已处理简历和今日新注册数量。 */
export default function PublicStats({
  stats,
  compact = false,
  mobile = false,
}: {
  stats: PublicStatsData;
  compact?: boolean;
  mobile?: boolean;
}) {
  return (
    <div
      style={
        {
          "--public-publicstats-1-0":
            (mobile ? 0.75 : compact ? 1.5 : 3) * 8 + "px",
        } as import("react").CSSProperties
      }
      className="public-publicstats-1"
    >
      <StatValue
        label="今日处理简历"
        value={stats.processedResumeCount}
        suffix="份"
        compact={compact}
        mobile={mobile}
      />
      <div
        style={
          {
            "--public-publicstats-3-0":
              typeof (mobile ? 22 : compact ? 26 : 32) === "number"
                ? (mobile ? 22 : compact ? 26 : 32) + "px"
                : mobile
                  ? 22
                  : compact
                    ? 26
                    : 32,
          } as import("react").CSSProperties
        }
        className="public-publicstats-3"
      />
      <StatValue
        label="今日注册"
        value={stats.todayRegisteredCount}
        suffix="人"
        compact={compact}
        mobile={mobile}
      />
    </div>
  );
}

/** StatValue 展示一个统计数字及说明。 */
function StatValue({
  label,
  value,
  suffix,
  compact,
  mobile,
}: {
  label: string;
  value: number | null;
  suffix: string;
  compact: boolean;
  mobile: boolean;
}) {
  return (
    <div>
      <p
        style={
          {
            "--public-publicstats-6-0":
              typeof (mobile ? 12 : compact ? 15 : 22) === "number"
                ? (mobile ? 12 : compact ? 15 : 22) + "px"
                : mobile
                  ? 12
                  : compact
                    ? 15
                    : 22,
          } as import("react").CSSProperties
        }
        className="public-text public-publicstats-6"
      >
        {value === null ? "--" : value.toLocaleString("zh-CN")}
        <span
          style={
            {
              "--public-publicstats-7-0":
                typeof (mobile ? 8 : compact ? 11 : 13) === "number"
                  ? (mobile ? 8 : compact ? 11 : 13) + "px"
                  : mobile
                    ? 8
                    : compact
                      ? 11
                      : 13,
            } as import("react").CSSProperties
          }
          className="public-text public-publicstats-7"
        >
          {suffix}
        </span>
      </p>
      <p
        style={
          {
            "--public-publicstats-8-0": (compact ? 0.15 : 0.5) * 8 + "px",
            "--public-publicstats-8-1":
              typeof (mobile ? 8 : compact ? 10 : 13) === "number"
                ? (mobile ? 8 : compact ? 10 : 13) + "px"
                : mobile
                  ? 8
                  : compact
                    ? 10
                    : 13,
          } as import("react").CSSProperties
        }
        className="public-text public-publicstats-8"
      >
        {label}
      </p>
    </div>
  );
}
