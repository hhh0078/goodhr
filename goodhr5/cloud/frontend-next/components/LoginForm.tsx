/** 本文件负责新版登录页的邮箱验证码登录流程。 */
"use client";

import ArrowForwardRoundedIcon from "@mui/icons-material/ArrowForwardRounded";
import MailOutlineRoundedIcon from "@mui/icons-material/MailOutlineRounded";
import VerifiedRoundedIcon from "@mui/icons-material/VerifiedRounded";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  InputAdornment,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { useEffect, useState } from "react";
import {
  apiRequest,
  INVITE_CACHE_KEY,
  legacyAdminURL,
  SESSION_EMAIL_KEY,
  TOKEN_KEY,
} from "@/lib/api";

/** LoginForm 提供验证码发送、倒计时和登录状态保存。 */
export default function LoginForm() {
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [cooldown, setCooldown] = useState(0);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    if (cooldown <= 0) return undefined;
    const timer = window.setInterval(
      () => setCooldown((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearInterval(timer);
  }, [cooldown]);

  /** sendCode 请求向当前邮箱发送登录验证码。 */
  async function sendCode() {
    const normalizedEmail = email.trim().toLowerCase();
    if (!normalizedEmail) {
      setError("请先填写邮箱");
      return;
    }
    setLoading(true);
    setError("");
    setMessage("");
    try {
      const data = await apiRequest("/api/auth/send-code", {
        method: "POST",
        body: JSON.stringify({ email: normalizedEmail }),
      });
      if (data.debug_code) setCode(String(data.debug_code));
      setCooldown(30);
      setMessage("验证码已发送，请查收邮箱");
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }

  /** login 使用验证码登录并保存与旧前端兼容的 Token。 */
  async function login() {
    const normalizedEmail = email.trim().toLowerCase();
    if (!normalizedEmail || code.trim().length !== 4) {
      setError("请填写邮箱和 4 位验证码");
      return;
    }
    setLoading(true);
    setError("");
    setMessage("");
    try {
      const inviterID = localStorage.getItem(INVITE_CACHE_KEY) || "";
      const data = await apiRequest("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({
          email: normalizedEmail,
          code: code.trim(),
          inviter_id: inviterID,
        }),
      });
      const token = String(data.access_token || "");
      if (!token) throw new Error("登录成功但未返回登录凭证");
      localStorage.setItem(TOKEN_KEY, token);
      localStorage.setItem(SESSION_EMAIL_KEY, normalizedEmail);
      setMessage("登录成功，正在进入控制台");
      const nextPath = new URLSearchParams(window.location.search).get("next");
      const safeNextPath = nextPath?.startsWith("/") && !nextPath.startsWith("//") ? nextPath : legacyAdminURL();
      window.location.assign(safeNextPath);
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }

  return (
    <Box
      component='form'
      onSubmit={(event) => {
        event.preventDefault();
        void login();
      }}
      noValidate
    >
      <Stack spacing={2.25}>
        <TextField
          label='邮箱'
          placeholder='请输入邮箱(12242993@qq.com 为示例)'
          type='email'
          autoComplete='email'
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          disabled={loading}
          fullWidth
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position='start'>
                  <MailOutlineRoundedIcon color='action' />
                </InputAdornment>
              ),
            },
          }}
        />
        <TextField
          label='验证码'
          inputMode='numeric'
          placeholder='请输入4位验证码'
          autoComplete='one-time-code'
          value={code}
          onChange={(event) =>
            setCode(event.target.value.replace(/\D/g, "").slice(0, 4))
          }
          disabled={loading}
          fullWidth
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position='start'>
                  <VerifiedRoundedIcon color='action' />
                </InputAdornment>
              ),
              endAdornment: (
                <InputAdornment position='end'>
                  <Button
                    onClick={() => void sendCode()}
                    disabled={loading || cooldown > 0 || !email.trim()}
                    size='small'
                  >
                    {cooldown > 0 ? `${cooldown}s 后重试` : "发送验证码"}
                  </Button>
                </InputAdornment>
              ),
            },
          }}
        />
        {error ? <Alert severity='error'>{error}</Alert> : null}
        {message ? <Alert severity='success'>{message}</Alert> : null}
        <Button
          type='submit'
          variant='contained'
          size='large'
          disabled={loading || !email.trim() || code.length !== 4}
          endIcon={
            loading ? (
              <CircularProgress size={18} color='inherit' />
            ) : (
              <ArrowForwardRoundedIcon />
            )
          }
        >
          {loading ? "正在处理" : "登录并进入控制台"}
        </Button>
      </Stack>
      <Typography
        sx={{ mt: 2.5, color: "text.secondary", fontSize: 13, lineHeight: 1.7 }}
      >
        未注册的邮箱首次登录后会自动创建账号。登录即表示你同意仅将 GoodHR
        用于合法招聘工作。
      </Typography>
    </Box>
  );
}

/** errorMessage 从未知异常中提取可展示的信息。 */
function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "操作失败，请稍后重试";
}
