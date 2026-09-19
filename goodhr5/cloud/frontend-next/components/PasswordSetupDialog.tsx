/** 本文件负责强制历史账号设置登录密码，设置完成前不允许关闭弹框。 */
"use client";

import LockOutlineRoundedIcon from "@mui/icons-material/LockOutlineRounded";
import {
  Alert,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  InputAdornment,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { useState } from "react";

type PasswordSetupDialogProps = {
  open: boolean;
  email?: string;
  onSubmit: (password: string) => Promise<void>;
};

/** PasswordSetupDialog 收集并确认历史账号的新密码。 */
export default function PasswordSetupDialog({ open, email, onSubmit }: PasswordSetupDialogProps) {
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  /** submit 校验两次输入并保存密码。 */
  async function submit() {
    setError("");
    if ([...password].length < 2) return setError("密码至少需要 2 个字符");
    if (password !== confirmation) return setError("两次密码不一样，我先不敢保存");
    setLoading(true);
    try {
      await onSubmit(password);
      setPassword("");
      setConfirmation("");
    } catch (submitError) {
      setError(submitError instanceof Error ? submitError.message : "密码没保存成功，请再试一次");
    } finally {
      setLoading(false);
    }
  }

  return (
    <Dialog open={open} onClose={() => undefined} fullWidth maxWidth="xs">
      <DialogTitle>给老账号补一个密码</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2}>
          <Typography color="text.secondary" sx={{ lineHeight: 1.7 }}>
            我小声提醒一下：{email || "这个账号"} 以前只用验证码，还没有密码。设置好以后，登录会快很多。
          </Typography>
          <TextField
            label="新密码"
            type="password"
            autoComplete="new-password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            disabled={loading}
            helperText="至少 2 个字符"
            slotProps={{ input: { startAdornment: <InputAdornment position="start"><LockOutlineRoundedIcon color="action" /></InputAdornment> } }}
            fullWidth
          />
          <TextField
            label="再输入一次"
            type="password"
            autoComplete="new-password"
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value)}
            disabled={loading}
            onKeyDown={(event) => {
              if (event.key === "Enter") void submit();
            }}
            fullWidth
          />
          {error ? <Alert severity="error">{error}</Alert> : null}
        </Stack>
      </DialogContent>
      <DialogActions sx={{ px: 3, py: 2 }}>
        <Button
          variant="contained"
          onClick={() => void submit()}
          disabled={loading || [...password].length < 2 || !confirmation}
          startIcon={loading ? <CircularProgress size={16} color="inherit" /> : undefined}
        >
          {loading ? "正在保存" : "设置密码并继续"}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
