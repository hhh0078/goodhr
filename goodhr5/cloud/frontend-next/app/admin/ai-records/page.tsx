/** 本文件负责展示内置 AI 余额变动、调用扣费和充值调整记录。 */
"use client";

import ArrowDownwardRoundedIcon from "@mui/icons-material/ArrowDownwardRounded";
import ArrowUpwardRoundedIcon from "@mui/icons-material/ArrowUpwardRounded";
import {
  Box,
  Chip,
  Pagination,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";

const pageSize = 20;

/** AIRecordsPage 展示当前用户或超管指定用户的 AI 使用记录。 */
export default function AIRecordsPage() {
  const { user, notify } = useAdmin();
  const searchParams = useSearchParams();
  const requestedEmail = String(searchParams.get("email") || "").trim();
  const canViewTarget = user?.role === "super_admin" && requestedEmail;
  const [records, setRecords] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const query = useMemo(() => {
    const params = new URLSearchParams({
      page: String(page),
      page_size: String(pageSize),
    });
    if (canViewTarget) params.set("email", requestedEmail);
    return params.toString();
  }, [canViewTarget, page, requestedEmail]);

  /** load 读取 AI 余额流水记录。 */
  async function load() {
    setLoading(true);
    try {
      const data = await cloudRequest(`/api/ai-wallet/records?${query}`);
      setRecords(data.records || []);
      setTotal(Number(data.total || 0));
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "AI 使用记录读取失败，我再试也得先缓缓。",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, [query]);

  const title = canViewTarget ? "AI 使用记录" : "我的 AI 使用记录";
  const description = canViewTarget
    ? `正在查看 ${requestedEmail} 的内置 AI 余额流水。`
    : "这里记录内置 AI 的充值、赠送、调整和调用扣费。";

  return (
    <>
      <PageHeader
        title={title}
        description={description}
        actions={
          <RefreshButton loading={loading} onClick={() => void load()} />
        }
      />
      <SectionPanel>
        {records.length ? (
          <>
            <TableContainer sx={{ display: { xs: "none", md: "block" } }}>
              <Table size='small'>
                <TableHead>
                  <TableRow sx={{ bgcolor: "#f6faf7" }}>
                    <TableCell>类型</TableCell>
                    <TableCell>金额</TableCell>
                    <TableCell>余额</TableCell>
                    <TableCell>模型 / Token</TableCell>
                    <TableCell>说明</TableCell>
                    <TableCell>时间</TableCell>
                  </TableRow>
                </TableHead>
                <TableBody>
                  {records.map((record) => (
                    <RecordRow
                      key={record.id || `${record.created_at}-${record.reason}`}
                      record={record}
                    />
                  ))}
                </TableBody>
              </Table>
            </TableContainer>
            <Stack spacing={1.25} sx={{ display: { xs: "flex", md: "none" } }}>
              {records.map((record) => (
                <RecordCard
                  key={record.id || `${record.created_at}-${record.reason}`}
                  record={record}
                />
              ))}
            </Stack>
            <Pagination
              page={page}
              count={Math.max(1, Math.ceil(total / pageSize))}
              onChange={(_, value) => setPage(value)}
              sx={{ mt: 2, display: "flex", justifyContent: "center" }}
            />
          </>
        ) : (
          <EmptyState
            text={
              loading
                ? "正在读取 AI 使用记录"
                : "这里暂时空空的，等你开始使用内置 AI 后我再认真记账。"
            }
          />
        )}
      </SectionPanel>
    </>
  );
}

/** RecordRow 展示桌面端单条 AI 记录。 */
function RecordRow({ record }: { record: any }) {
  return (
    <TableRow hover>
      <TableCell>
        <CategoryChip category={record.category} />
      </TableCell>
      <TableCell>
        <AmountText
          units={Number(record.change_units || 0)}
          value={record.change}
        />
      </TableCell>
      <TableCell>￥{record.balance_after || "0.00"}</TableCell>
      <TableCell>
        <TokenInfo record={record} />
      </TableCell>
      <TableCell>
        <ReasonText record={record} />
      </TableCell>
      <TableCell>{formatDate(record.created_at)}</TableCell>
    </TableRow>
  );
}

/** RecordCard 展示移动端单条 AI 记录。 */
function RecordCard({ record }: { record: any }) {
  return (
    <Box
      sx={{
        p: 1.5,
        border: "1px solid",
        borderColor: "divider",
        borderRadius: "8px",
        bgcolor: "#fbfdfc",
      }}
    >
      <Stack direction='row' sx={{ justifyContent: "space-between", gap: 1 }}>
        <CategoryChip category={record.category} />
        <AmountText
          units={Number(record.change_units || 0)}
          value={record.change}
        />
      </Stack>
      <Typography sx={{ mt: 1, color: "text.secondary", fontSize: 13 }}>
        <ReasonText record={record} />
      </Typography>
      <Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 12 }}>
        <TokenInfo record={record} /> · 余额 ￥{record.balance_after || "0.00"}{" "}
        · {formatDate(record.created_at)}
      </Typography>
    </Box>
  );
}

/** CategoryChip 展示流水类别。 */
function CategoryChip({ category }: { category: string }) {
  const labelMap: Record<string, string> = {
    ai_usage: "AI调用",
    recharge: "充值",
    signup_bonus: "注册赠送",
    admin_adjust: "后台调整",
  };
  const color =
    category === "ai_usage"
      ? "warning"
      : category === "admin_adjust"
        ? "info"
        : "success";
  return (
    <Chip
      size='small'
      color={color as any}
      variant='outlined'
      label={labelMap[category] || category || "记录"}
    />
  );
}

/** AmountText 展示余额变动金额。 */
function AmountText({ units, value }: { units: number; value: string }) {
 const positive = units ? units > 0 : !String(value || "").trim().startsWith("-");
  const Icon = positive ? ArrowUpwardRoundedIcon : ArrowDownwardRoundedIcon;
  return (
    <Stack
      direction='row'
      spacing={0.4}
      sx={{
        alignItems: "center",
        color: positive ? "#15804f" : "#b45309",
        fontWeight: 800,
      }}
    >
      <Icon sx={{ fontSize: 16 }} />
      <Typography sx={{ fontWeight: 800 }}>
        {positive ? "+" : ""}￥{value || "0.00"}
      </Typography>
    </Stack>
  );
}

/** TokenInfo 展示模型和 token 用量。 */
function TokenInfo({ record }: { record: any }) {
  const tokens = Number(record.total_tokens || 0);
  return (
    <>
      {record.model_id || "无模型"}
      {tokens ? ` / ${tokens} tokens` : ""}
    </>
  );
}

/** ReasonText 展示记录说明和关联订单号。 */
function ReasonText({ record }: { record: any }) {
  return (
    <>
      {record.reason || "暂无说明"}
      {record.related_order_no ? `（订单 ${record.related_order_no}）` : ""}
    </>
  );
}
