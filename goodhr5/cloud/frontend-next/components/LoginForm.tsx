/** 本文件负责新版登录页的邮箱验证码登录流程和首次协议确认。 */
"use client";

import ArrowForwardRoundedIcon from "@mui/icons-material/ArrowForwardRounded";
import MailOutlineRoundedIcon from "@mui/icons-material/MailOutlineRounded";
import VerifiedRoundedIcon from "@mui/icons-material/VerifiedRounded";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  InputAdornment,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { useEffect, useMemo, useState, type UIEvent } from "react";
import {
  apiRequest,
  INVITE_CACHE_KEY,
  legacyAdminURL,
  SESSION_EMAIL_KEY,
  TOKEN_KEY,
} from "@/lib/api";
import { captureLocalAgentPortFromURL } from "@/lib/admin-api";

const AGREEMENT_MARKDOWN = `## GoodHR 使用协议与隐私说明

**请先小声看完：GoodHR 是效率工具，不是平台安全保险。继续使用前，请确认你能接受下面这些边界。**

### **1. 招聘平台账号风险自担**

GoodHR 会模拟人工操作招聘平台，但不保证账号永远安全、不被限制、不被封禁。

如果你认为招聘平台账号被限制、封号、降权、风控的后果自己承担不起，请不要使用本软件。

因招聘平台规则变化、风控策略、用户使用频率、账号状态等原因造成的账号异常、封禁、损失，由使用者自行承担。

### **2. 仅可用于合法招聘工作**

GoodHR 只能用于合法、合规的招聘、候选人沟通和团队协作。

不得用于诈骗、骚扰、倒卖简历、爬取数据、侵犯隐私、恶意营销、违法犯罪或其它不正当用途。

如果因为你的使用方式产生纠纷、投诉、处罚或法律后果，由你自行承担。

### **3. 简历与候选人信息属于高度敏感信息**

使用过程中，GoodHR 可能会读取或处理候选人的姓名、手机号、微信、工作经历、教育经历、求职意向、简历内容、沟通记录等信息。

这些信息仅用于帮助你完成招聘筛选、AI 判断、AI 回复、候选人跟进和团队内协作。

除岗位创建人、所属团队成员及你授权的成员外，其他用户不可见。

### **4. AI 会使用候选人信息辅助回复**

当你开启 AI 筛选、AI 回复、简历结构化等功能时，系统可能会把必要的候选人信息发送给 AI 服务，用来生成更准确的分析、评分、回复建议或结构化结果。

我们不会把这些信息用于与招聘无关的用途。

### **5. 营销与服务通知**

你同意我们通过你提供的邮箱发送必要的服务通知，例如验证码、订单、会员到期、本地程序更新、风险提醒等。

我们也可能发送少量营销邮件，例如套餐优惠、功能更新、召回提醒等。你可以联系我取消接收营销类邮件。

### **6. 请自行控制使用频率**

GoodHR 提供了模拟休息、打开概率、操作间隔等配置，但这些配置不能保证完全规避招聘平台风控。

请根据自己的账号情况谨慎设置，不要高频、异常、批量地使用。

### **7. 数据可见范围**

你的岗位、任务、团队、简历库数据，只对你和你所在团队内有权限的成员可见。

本地浏览器数据、招聘平台登录状态、截图缓存等敏感数据主要保存在你的本地电脑中。

### **8. 继续使用代表你已理解并同意**

如果你勾选并继续登录，表示你已经阅读、理解并同意以上内容。

如果你不同意，或者无法承担相关风险，请停止使用 GoodHR。`;

/** LoginForm 提供验证码发送、倒计时、协议确认和登录状态保存。 */
export default function LoginForm() {
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [cooldown, setCooldown] = useState(0);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [agreementOpen, setAgreementOpen] = useState(false);
  const [agreementScrolled, setAgreementScrolled] = useState(false);
  const [agreementChecked, setAgreementChecked] = useState(false);
  const [acceptedEmail, setAcceptedEmail] = useState("");

  const normalizedEmail = useMemo(() => email.trim().toLowerCase(), [email]);
  const agreementReady = acceptedEmail === normalizedEmail || agreementChecked;

  useEffect(() => {
    captureLocalAgentPortFromURL();
    const cachedEmail = (localStorage.getItem(SESSION_EMAIL_KEY) || "")
      .trim()
      .toLowerCase();
    if (cachedEmail) setEmail(cachedEmail);
    const token = localStorage.getItem(TOKEN_KEY) || "";
    if (token) window.location.replace(resolveNextPath());
  }, []);

  useEffect(() => {
    if (cooldown <= 0) return undefined;
    const timer = window.setInterval(
      () => setCooldown((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearInterval(timer);
  }, [cooldown]);

  useEffect(() => {
    setAgreementChecked(false);
    setAgreementScrolled(false);
    setAcceptedEmail((current) => (current === normalizedEmail ? current : ""));
  }, [normalizedEmail]);

  /** sendCode 请求向当前邮箱发送登录验证码。 */
  async function sendCode() {
    setError("");
    setMessage("");
    if (!normalizedEmail) return setError("请先填写邮箱");
    setLoading(true);
    try {
      await apiRequest("/api/auth/send-code", {
        method: "POST",
        body: JSON.stringify({ email: normalizedEmail }),
      });
      setCooldown(60);
      setMessage("验证码已发送，请查看邮箱");
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }

  /** ensureAgreementAccepted 确认当前邮箱是否已经完成协议确认。 */
  async function ensureAgreementAccepted() {
    if (acceptedEmail === normalizedEmail || agreementChecked) return true;
    const data = await apiRequest(
      `/api/auth/agreement-status?email=${encodeURIComponent(normalizedEmail)}`,
      { method: "GET" },
    );
    if (data.agreement_accepted) {
      setAcceptedEmail(normalizedEmail);
      return true;
    }
    setAgreementOpen(true);
    setError("我小声提醒一下：首次登录前需要先读完并同意协议。");
    return false;
  }

  /** login 提交邮箱验证码并保存登录凭证。 */
  async function login() {
    setError("");
    setMessage("");
    if (!normalizedEmail) return setError("请先填写邮箱");
    if (code.trim().length !== 4) return setError("请输入 4 位验证码");
    setLoading(true);
    try {
      if (!(await ensureAgreementAccepted())) return;
      const inviterID = localStorage.getItem(INVITE_CACHE_KEY) || "";
      const data = await apiRequest("/api/auth/login", {
        method: "POST",
        body: JSON.stringify({
          email: normalizedEmail,
          code: code.trim(),
          inviter_id: inviterID,
          agreement_accepted: agreementChecked,
        }),
      });
      const token = String(data.access_token || "");
      if (!token) throw new Error("登录成功但未返回登录凭证");
      localStorage.setItem(TOKEN_KEY, token);
      localStorage.setItem(SESSION_EMAIL_KEY, normalizedEmail);
      localStorage.removeItem(INVITE_CACHE_KEY);
      setMessage("登录成功，正在进入控制台");
      window.location.assign(resolveNextPath());
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }

  /** handleAgreementScroll 判断协议内容是否已经滚动到底部。 */
  function handleAgreementScroll(event: UIEvent<HTMLDivElement>) {
    const target = event.currentTarget;
    const reachedBottom =
      target.scrollTop + target.clientHeight >= target.scrollHeight - 8;
    if (reachedBottom) setAgreementScrolled(true);
  }

  /** acceptAgreement 在弹框内确认协议勾选。 */
  function acceptAgreement() {
    setAgreementChecked(true);
    setAcceptedEmail(normalizedEmail);
    setAgreementOpen(false);
    setError("");
  }

  return (
    <Box
      component="form"
      onSubmit={(event) => {
        event.preventDefault();
        void login();
      }}
      noValidate
    >
      <Stack spacing={2.25}>
        <TextField
          label="邮箱"
          placeholder="请输入邮箱(12242993@qq.com 为示例)"
          type="email"
          autoComplete="email"
          value={email}
          onChange={(event) => setEmail(event.target.value)}
          disabled={loading}
          fullWidth
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <MailOutlineRoundedIcon color="action" />
                </InputAdornment>
              ),
            },
          }}
        />
        <TextField
          label="验证码"
          inputMode="numeric"
          placeholder="请输入4位验证码"
          autoComplete="one-time-code"
          value={code}
          onChange={(event) =>
            setCode(event.target.value.replace(/\D/g, "").slice(0, 4))
          }
          disabled={loading}
          fullWidth
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position="start">
                  <VerifiedRoundedIcon color="action" />
                </InputAdornment>
              ),
              endAdornment: (
                <InputAdornment position="end">
                  <Button
                    onClick={() => void sendCode()}
                    disabled={loading || cooldown > 0 || !email.trim()}
                    size="small"
                  >
                    {cooldown > 0 ? `${cooldown}s 后重试` : "发送验证码"}
                  </Button>
                </InputAdornment>
              ),
            },
          }}
        />
        <Stack
          direction={{ xs: "column", sm: "row" }}
          spacing={1}
          sx={{ alignItems: { sm: "center" }, justifyContent: "space-between" }}
        >
          <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
            同一账号支持多处登录，不会再互相挤下线。
          </Typography>
          <Button
            size="small"
            onClick={() => {
              setAgreementOpen(true);
              setAgreementScrolled(false);
            }}
          >
            阅读协议
          </Button>
        </Stack>
        {error ? <Alert severity="error">{error}</Alert> : null}
        {message ? <Alert severity="success">{message}</Alert> : null}
        <Button
          type="submit"
          variant="contained"
          size="large"
          disabled={loading || !email.trim() || code.length !== 4}
          endIcon={
            loading ? (
              <CircularProgress size={18} color="inherit" />
            ) : (
              <ArrowForwardRoundedIcon />
            )
          }
        >
          {loading ? "正在处理" : "登录并进入控制台"}
        </Button>
      </Stack>
      <Typography sx={{ mt: 2.5, color: "text.secondary", fontSize: 13, lineHeight: 1.7 }}>
        未注册的邮箱首次登录后会自动创建账号。首次使用需要阅读并同意 GoodHR 使用协议。
      </Typography>
      <Dialog
        open={agreementOpen}
        onClose={() => setAgreementOpen(false)}
        fullWidth
        maxWidth="md"
      >
        <DialogTitle>GoodHR 使用协议与隐私说明</DialogTitle>
        <DialogContent
          dividers
          onScroll={handleAgreementScroll}
          sx={{ maxHeight: { xs: "62vh", md: "68vh" } }}
        >
          <AgreementMarkdown markdown={AGREEMENT_MARKDOWN} />
        </DialogContent>
        <DialogActions
          sx={{
            px: 3,
            py: 2,
            alignItems: { xs: "stretch", sm: "center" },
            flexDirection: { xs: "column", sm: "row" },
          }}
        >
          <FormControlLabel
            control={
              <Checkbox
                checked={agreementReady}
                disabled={!agreementScrolled && acceptedEmail !== normalizedEmail}
                onChange={(event) => setAgreementChecked(event.target.checked)}
              />
            }
            label={
              agreementScrolled || acceptedEmail === normalizedEmail
                ? "我已阅读并同意"
                : "请先滚动到最下面，我再让你勾"
            }
            sx={{ mr: "auto" }}
          />
          <Button onClick={() => setAgreementOpen(false)}>先不登录</Button>
          <Button
            variant="contained"
            disabled={!agreementReady}
            onClick={acceptAgreement}
          >
            同意并继续
          </Button>
        </DialogActions>
      </Dialog>
    </Box>
  );
}

/** AgreementMarkdown 将协议 Markdown 转换为轻量展示内容。 */
function AgreementMarkdown({ markdown }: { markdown: string }) {
  return (
    <Stack spacing={1.4}>
      {markdown.split("\n").map((line, index) => {
        const text = line.trim();
        if (!text) return <Box key={index} sx={{ height: 4 }} />;
        if (text.startsWith("## ")) {
          return (
            <Typography key={index} component="h2" sx={{ fontSize: 22, fontWeight: 820 }}>
              {stripMarkdown(text.replace(/^##\s+/, ""))}
            </Typography>
          );
        }
        if (text.startsWith("### ")) {
          return (
            <Typography key={index} component="h3" sx={{ mt: 1.4, fontSize: 17, fontWeight: 800 }}>
              {stripMarkdown(text.replace(/^###\s+/, ""))}
            </Typography>
          );
        }
        const strong = text.match(/^\*\*(.*)\*\*$/);
        return (
          <Typography
            key={index}
            sx={{
              color: strong ? "text.primary" : "text.secondary",
              fontWeight: strong ? 760 : 400,
              lineHeight: 1.75,
            }}
          >
            {stripMarkdown(strong?.[1] || text)}
          </Typography>
        );
      })}
    </Stack>
  );
}

/** stripMarkdown 去掉协议展示里少量 Markdown 强调符号。 */
function stripMarkdown(text: string) {
  return text.replace(/\*\*/g, "");
}

/** resolveNextPath 返回登录完成后的安全站内跳转地址。 */
function resolveNextPath() {
  const nextPath = new URLSearchParams(window.location.search).get("next");
  return nextPath?.startsWith("/") && !nextPath.startsWith("//")
    ? nextPath
    : legacyAdminURL();
}

/** errorMessage 从未知异常中提取可展示的信息。 */
function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "操作失败，请稍后重试";
}
