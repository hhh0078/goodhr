/** 本文件提供后台新增、编辑和确认操作统一使用的弹框组件。 */
"use client";

import CloseRoundedIcon from "@mui/icons-material/CloseRounded";
import { Button, Dialog, DialogActions, DialogContent, DialogTitle, IconButton, Stack, Typography } from "@mui/material";
import type { ReactNode } from "react";

type AdminDialogProps = {
  open: boolean;
  title: string;
  description?: string;
  children: ReactNode;
  confirmText?: string;
  cancelText?: string;
  loading?: boolean;
  confirmDisabled?: boolean;
  maxWidth?: "xs" | "sm" | "md" | "lg" | "xl";
  onClose: () => void;
  onConfirm?: () => void;
  extraActions?: ReactNode;
};

/** AdminDialog 渲染统一的后台表单弹框和操作栏。 */
export default function AdminDialog({ open, title, description, children, confirmText = "保存", cancelText = "取消", loading = false, confirmDisabled = false, maxWidth = "sm", onClose, onConfirm, extraActions }: AdminDialogProps) {
  return <Dialog open={open} onClose={loading ? undefined : onClose} fullWidth maxWidth={maxWidth} slotProps={{ paper: { sx: { borderRadius: "8px", maxHeight: "calc(100vh - 40px)", "& .MuiButton-root": { minHeight: 38, px: 1.75 }, "& .MuiIconButton-root": { width: 38, height: 38 }, "& .MuiOutlinedInput-root": { minHeight: 46, borderRadius: "8px" }, "& .MuiOutlinedInput-root.MuiInputBase-multiline": { minHeight: "unset" }, "& .MuiInputLabel-root": { fontSize: 14 } } } }}>
    <DialogTitle sx={{ pr: 7, pb: description ? 0.75 : 2 }}><Typography component="span" sx={{ fontSize: 21, fontWeight: 780 }}>{title}</Typography><IconButton aria-label="关闭弹框" onClick={onClose} disabled={loading} sx={{ position: "absolute", right: 12, top: 12 }}><CloseRoundedIcon /></IconButton></DialogTitle>
    {description ? <Typography sx={{ px: 3, pb: 2, color: "text.secondary", fontSize: 14 }}>{description}</Typography> : null}
    <DialogContent dividers sx={{ p: { xs: 2, md: 3 } }}>{children}</DialogContent>
    <DialogActions sx={{ px: 3, py: 2, justifyContent: "space-between" }}><Stack direction="row" spacing={1}>{extraActions}</Stack><Stack direction="row" spacing={1}><Button color="secondary" onClick={onClose} disabled={loading}>{cancelText}</Button>{onConfirm ? <Button variant="contained" onClick={onConfirm} disabled={loading || confirmDisabled}>{loading ? "处理中" : confirmText}</Button> : null}</Stack></DialogActions>
  </Dialog>;
}
