/** 本文件负责展示岗位自动回复的 AI 总记录、工具调用和待审核配置建议。 */
"use client";

import {
  Accordion,
  AccordionDetails,
  AccordionSummary,
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Divider,
  Stack,
  Typography,
} from "@mui/material";
import ExpandMoreRoundedIcon from "@mui/icons-material/ExpandMoreRounded";
import { useEffect, useState } from "react";
import {
  loadAutoReplyAudit,
  loadAutoReplySuggestions,
  reviewAutoReplySuggestion,
  type AutoReplyAuditRecord,
  type AutoReplyConfigSuggestion,
} from "@/lib/auto-reply";
import AdminDialog from "./AdminDialog";
import JsonTree from "./JsonTree";

type AutoReplyAuditDialogProps = {
  open: boolean;
  position: { id: string; name: string } | null;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
  onClose: () => void;
};

/** AutoReplyAuditDialog 展示指定岗位最近的 AI 判断和工具执行全记录。 */
export default function AutoReplyAuditDialog({
  open,
  position,
  notify,
  onClose,
}: AutoReplyAuditDialogProps) {
  const [loading, setLoading] = useState(false);
  const [reviewingID, setReviewingID] = useState("");
  const [records, setRecords] = useState<AutoReplyAuditRecord[]>([]);
  const [suggestions, setSuggestions] = useState<
    AutoReplyConfigSuggestion[]
  >([]);

  useEffect(() => {
    if (!open || !position?.id) return;
    void load(position.id);
  }, [open, position?.id]);

  /** load 同时读取当前岗位 AI 总记录和团队待审核建议。 */
  async function load(positionID: string) {
    setLoading(true);
    try {
      const [nextRecords, nextSuggestions] = await Promise.all([
        loadAutoReplyAudit(positionID),
        loadAutoReplySuggestions("pending"),
      ]);
      setRecords(nextRecords);
      setSuggestions(
        nextSuggestions.filter(
          (suggestion) =>
            !suggestion.position_id || suggestion.position_id === positionID,
        ),
      );
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "AI 总记录没读出来，我先小声停一下。",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  /** reviewSuggestion 标记一条配置建议为采纳或拒绝，但不直接修改原配置。 */
  async function reviewSuggestion(
    item: AutoReplyConfigSuggestion,
    status: "approved" | "rejected",
  ) {
    setReviewingID(item.id);
    try {
      await reviewAutoReplySuggestion(item.id, status);
      setSuggestions((current) =>
        current.filter((suggestion) => suggestion.id !== item.id),
      );
      notify(
        status === "approved"
          ? "这条建议已标记采纳，请按建议手动更新岗位或公司资料。"
          : "这条建议已放回抽屉，不会改动现有资料。",
        "success",
      );
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "配置建议没审核成功。",
        "error",
      );
    } finally {
      setReviewingID("");
    }
  }

  return (
    <AdminDialog
      open={open}
      title={position ? `AI 总记录 · ${position.name}` : "AI 总记录"}
      description='这里会保存 AI 收到的内容、返回结果和工具调用，方便你核对它有没有偷偷犯迷糊。'
      cancelText='关闭'
      maxWidth='lg'
      onClose={onClose}
    >
      {loading ? (
        <Stack spacing={1.25} sx={{ py: 8, alignItems: "center" }}>
          <CircularProgress size={26} />
          <Typography sx={{ color: "text.secondary" }}>
            正在整理 AI 总记录
          </Typography>
        </Stack>
      ) : (
        <Stack spacing={2.25}>
          <Box>
            <Typography sx={{ fontWeight: 760 }}>待审核建议</Typography>
            <Typography sx={{ mt: 0.35, color: "text.secondary", fontSize: 13 }}>
              AI 只能提建议，不能直接修改岗位或公司资料。采纳后仍需要你手动更新。
            </Typography>
          </Box>
          {suggestions.length ? (
            <Stack spacing={1}>
              {suggestions.map((suggestion) => (
                <Box
                  key={suggestion.id}
                  sx={{
                    p: 1.5,
                    border: "1px solid",
                    borderColor: "divider",
                    borderRadius: "8px",
                    bgcolor: "background.paper",
                  }}
                >
                  <Stack
                    direction={{ xs: "column", md: "row" }}
                    spacing={1}
                    sx={{ justifyContent: "space-between" }}
                  >
                    <Box sx={{ minWidth: 0 }}>
                      <Typography sx={{ fontWeight: 700 }}>
                        {suggestionTypeLabel(suggestion.suggestion_type)} · {" "}
                        {suggestionOperationLabel(suggestion.operation)}
                      </Typography>
                      <Typography sx={{ mt: 0.45, color: "text.secondary" }}>
                        {suggestion.reason || "AI 没有写原因，我先不替它圆。"}
                      </Typography>
                    </Box>
                    <Stack direction='row' spacing={1}>
                      <Button
                        variant='outlined'
                        disabled={reviewingID === suggestion.id}
                        onClick={() =>
                          void reviewSuggestion(suggestion, "approved")
                        }
                      >
                        标记采纳
                      </Button>
                      <Button
                        color='error'
                        disabled={reviewingID === suggestion.id}
                        onClick={() =>
                          void reviewSuggestion(suggestion, "rejected")
                        }
                      >
                        拒绝
                      </Button>
                    </Stack>
                  </Stack>
                  <Box
                    sx={{
                      mt: 1,
                      p: 1.25,
                      borderRadius: "8px",
                      bgcolor: "action.hover",
                    }}
                  >
                    <JsonTree value={suggestion.proposed_value} />
                  </Box>
                </Box>
              ))}
            </Stack>
          ) : (
            <Alert severity='info'>
              目前没有待审核建议，AI 暂时还算老实。
            </Alert>
          )}

          <Divider />
          <Typography sx={{ fontWeight: 760 }}>最近 AI 记录</Typography>
          {records.length ? (
            <Stack spacing={1}>
              {records.map((record) => (
                <AuditRecordAccordion key={record.id} record={record} />
              ))}
            </Stack>
          ) : (
            <Alert severity='info'>
              这里暂时空空的，自动回复开始工作后我再认真记账。
            </Alert>
          )}
        </Stack>
      )}
    </AdminDialog>
  );
}

/** AuditRecordAccordion 展示一条可展开的 AI 输入、返回和工具记录。 */
function AuditRecordAccordion({ record }: { record: AutoReplyAuditRecord }) {
  return (
    <Accordion
      disableGutters
      elevation={0}
      sx={{ border: "1px solid", borderColor: "divider", borderRadius: "8px" }}
    >
      <AccordionSummary expandIcon={<ExpandMoreRoundedIcon />}>
        <Stack
          direction={{ xs: "column", md: "row" }}
          spacing={1}
          sx={{ width: "100%", alignItems: { md: "center" } }}
        >
          <Typography sx={{ flex: 1, fontWeight: 720 }}>
            {record.candidate_name || "暂时没认出候选人"}
            {record.gender ? ` · ${record.gender}` : ""}
          </Typography>
          <Chip
            size='small'
            color={auditStatusColor(record.status)}
            variant='outlined'
            label={auditStatusLabel(record.status)}
          />
          <Typography sx={{ color: "text.secondary", fontSize: 12.5 }}>
            {formatDateTime(record.started_at)}
          </Typography>
        </Stack>
      </AccordionSummary>
      <AccordionDetails>
        <Stack spacing={1.5}>
          {record.error_message ? (
            <Alert severity='error'>{record.error_message}</Alert>
          ) : null}
          <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
            模型：{record.model || "未记录"} · Token：{record.token_usage} ·
            追踪编号：{record.trace_id || "未记录"}
          </Typography>
          <AuditJSONBlock title='发给 AI 的内容' value={record.input_messages} />
          <AuditJSONBlock title='AI 完整返回' value={record.output_message} />
          <Box>
            <Typography sx={{ mb: 0.75, fontWeight: 700 }}>
              工具调用（{record.tool_calls.length}）
            </Typography>
            {record.tool_calls.length ? (
              <Stack spacing={1}>
                {record.tool_calls.map((tool) => (
                  <Box
                    key={tool.id}
                    sx={{
                      p: 1.25,
                      borderRadius: "8px",
                      bgcolor: "action.hover",
                    }}
                  >
                    <Typography sx={{ fontWeight: 700 }}>
                      {tool.sequence_no}. {tool.tool_name || "未命名工具"} · {" "}
                      {auditStatusLabel(tool.status)}
                    </Typography>
                    {tool.error_message ? (
                      <Typography sx={{ mt: 0.5, color: "error.main" }}>
                        {tool.error_message}
                      </Typography>
                    ) : null}
                    <Box sx={{ mt: 0.75 }}>
                      <JsonTree
                        label='参数'
                        value={tool.arguments_json}
                      />
                      <JsonTree label='结果' value={tool.result_json} />
                    </Box>
                  </Box>
                ))}
              </Stack>
            ) : (
              <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
                这次没有调用工具。
              </Typography>
            )}
          </Box>
        </Stack>
      </AccordionDetails>
    </Accordion>
  );
}

/** AuditJSONBlock 展示 AI 审计中的一块 JSON 数据。 */
function AuditJSONBlock({ title, value }: { title: string; value: unknown }) {
  return (
    <Box>
      <Typography sx={{ mb: 0.75, fontWeight: 700 }}>{title}</Typography>
      <Box
        sx={{
          p: 1.25,
          maxHeight: 320,
          overflow: "auto",
          borderRadius: "8px",
          bgcolor: "action.hover",
        }}
      >
        <JsonTree value={value} />
      </Box>
    </Box>
  );
}

/** auditStatusLabel 返回 AI 或工具状态的中文名称。 */
function auditStatusLabel(status: string) {
  const labels: Record<string, string> = {
    running: "处理中",
    completed: "已完成",
    failed: "失败",
    skipped: "已跳过",
  };
  return labels[status] || status || "未记录";
}

/** auditStatusColor 返回 AI 或工具状态对应的 MUI 颜色。 */
function auditStatusColor(status: string): "success" | "error" | "warning" {
  if (status === "completed") return "success";
  if (status === "failed") return "error";
  return "warning";
}

/** suggestionTypeLabel 返回建议目标的中文名称。 */
function suggestionTypeLabel(value: string) {
  return value === "company" ? "公司资料" : "岗位资料";
}

/** suggestionOperationLabel 返回建议操作的中文名称。 */
function suggestionOperationLabel(value: string) {
  const labels: Record<string, string> = {
    create: "建议新增",
    update: "建议修改",
    delete: "建议删除",
  };
  return labels[value] || "建议调整";
}

/** formatDateTime 把审计时间转换为用户电脑的本地时间。 */
function formatDateTime(value: string) {
  if (!value) return "时间未记录";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? "时间未记录" : date.toLocaleString("zh-CN");
}
