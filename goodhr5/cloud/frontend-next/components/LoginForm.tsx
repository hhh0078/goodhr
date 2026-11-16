/** 本文件负责登录页的邮箱、登录方式偏好、密码或验证码登录及首次协议确认。 */
"use client";

import {
  ArrowRightOutlined,
  LockOutlined,
  MailOutlined,
  SafetyCertificateOutlined,
} from "@ant-design/icons";
import { Alert, Button, Checkbox, Form, Input, Modal, Typography } from "antd";
import "@/components/public-site.css";
import { useEffect, useMemo, useRef, useState } from "react";
import { classifyAuthFailure, markPendingAdminArrival, trackConversion } from "@/lib/conversion-analytics";
import {
  apiRequest,
  INVITE_CACHE_KEY,
  legacyAdminURL,
  SESSION_EMAIL_KEY,
  TOKEN_KEY,
} from "@/lib/api";
import { captureLocalAgentPortFromURL } from "@/lib/admin-api";
import WechatLogin from "@/components/WechatLogin";
import PasswordSetupDialog from "@/components/PasswordSetupDialog";

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

### **8. 网站使用情况统计**

官网和登录页使用百度统计与 Microsoft Clarity，帮助我们了解访问、点击、页面滚动和注册步骤中的问题。Clarity 会生成热力图与操作回放，输入框内容会被遮盖。

我们不向统计工具发送你的密码、验证码或登录令牌，不采集后台简历、聊天、私人分享和诊断页面的操作回放。微软隐私说明：https://privacy.microsoft.com/privacystatement

### **9. 继续使用代表你已理解并同意**

如果你勾选并继续登录，表示你已经阅读、理解并同意以上内容。

如果你不同意，或者无法承担相关风险，请停止使用 GoodHR。`;

const LOGIN_MODE_CACHE_KEY = "goodhr.login.mode";
const AGREEMENT_CACHE_KEY = "goodhr.login.agreement";

/** LoginForm 提供验证码发送、倒计时、协议确认和登录状态保存。 */
export default function LoginForm() {
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [password, setPassword] = useState("");
  const [passwordConfirmation, setPasswordConfirmation] = useState("");
  const [loginMode, setLoginMode] = useState<"wechat" | "code" | "password">("wechat");
  const [isNewAccount, setIsNewAccount] = useState<boolean | null>(null);
  const [passwordSetupRequired, setPasswordSetupRequired] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  const [loading, setLoading] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [agreementOpen, setAgreementOpen] = useState(false);
  const [continueAfterAgreement, setContinueAfterAgreement] = useState(false);
  const [agreementChecked, setAgreementChecked] = useState(false);
  const [acceptedEmail, setAcceptedEmail] = useState("");
  const [loginPreferencesLoaded, setLoginPreferencesLoaded] = useState(false);
  const [resetOpen, setResetOpen] = useState(false);
  const [resetCode, setResetCode] = useState("");
  const [resetPassword, setResetPassword] = useState("");
  const [resetConfirmation, setResetConfirmation] = useState("");
  const [resetCooldown, setResetCooldown] = useState(0);
  const [resetBusy, setResetBusy] = useState(false);
  const [resetMessage, setResetMessage] = useState("");
  const loginBusy = useRef(false);

  const normalizedEmail = useMemo(() => email.trim().toLowerCase(), [email]);
  const agreementAcceptedForEmail = Boolean(
    normalizedEmail && acceptedEmail === normalizedEmail,
  );
  const agreementReady = agreementAcceptedForEmail || agreementChecked;
  const authFlow = loginMode === "password" ? "existing_account" : isNewAccount === null ? "unknown" : isNewAccount ? "new_account" : "existing_account";

  useEffect(() => {
    captureLocalAgentPortFromURL();
    try { setAgreementChecked(localStorage.getItem(AGREEMENT_CACHE_KEY) === "accepted"); } catch { /* 缓存不可用时由本人重新勾选。 */ }
    const cachedMode = localStorage.getItem(LOGIN_MODE_CACHE_KEY);
    if (cachedMode === "wechat" || cachedMode === "code" || cachedMode === "password") setLoginMode(cachedMode);
    const cachedEmail = (localStorage.getItem(SESSION_EMAIL_KEY) || "")
      .trim()
      .toLowerCase();
    if (cachedEmail && !cachedEmail.endsWith("@wechat.goodhr.invalid")) setEmail(cachedEmail);
    setLoginPreferencesLoaded(true);
    const token = localStorage.getItem(TOKEN_KEY) || "";
    if (token) window.location.replace(resolveNextPath());
  }, []);

  useEffect(() => {
    if (!loginPreferencesLoaded) return;
    localStorage.setItem(LOGIN_MODE_CACHE_KEY, loginMode);
    if (normalizedEmail) {
      localStorage.setItem(SESSION_EMAIL_KEY, normalizedEmail);
    } else {
      localStorage.removeItem(SESSION_EMAIL_KEY);
    }
  }, [loginMode, loginPreferencesLoaded, normalizedEmail]);

  useEffect(() => {
    if (cooldown <= 0) return undefined;
    const timer = window.setInterval(
      () => setCooldown((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearInterval(timer);
  }, [cooldown]);

  useEffect(() => {
    if (resetCooldown <= 0) return undefined;
    const timer = window.setInterval(
      () => setResetCooldown((value) => Math.max(0, value - 1)),
      1000,
    );
    return () => window.clearInterval(timer);
  }, [resetCooldown]);

  useEffect(() => {
    setContinueAfterAgreement(false);
    setAcceptedEmail((current) => (current === normalizedEmail ? current : ""));
  }, [normalizedEmail]);

  useEffect(() => {
    setIsNewAccount(null);
    setCode("");
    setPassword("");
    setPasswordConfirmation("");
  }, [normalizedEmail]);

  /** sendCode 请求向当前邮箱发送登录验证码。 */
  async function sendCode() {
    if (loading || cooldown > 0) return;
    setError("");
    setMessage("");
    if (!normalizedEmail) return setError("请先填写邮箱");
    setLoading(true);
    trackConversion("code_request", { method: "code" });
    try {
      const data = await apiRequest("/api/auth/send-code", {
        method: "POST",
        body: JSON.stringify({ email: normalizedEmail }),
      });
      setIsNewAccount(Boolean(data.is_new_account));
      trackConversion("code_sent", { method: "code", flow: data.is_new_account ? "new_account" : "existing_account" });
      if (data.debug_code) setCode(String(data.debug_code));
      setCooldown(60);
      setMessage(
        data.debug_code
          ? "本地开发验证码已自动填好"
          : "验证码已发送，请查看邮箱",
      );
    } catch (requestError) {
      trackConversion("code_failed", { reason: classifyAuthFailure(requestError), method: "code" });
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }

  /** openReset 打开忘记密码弹框并清空上一次的输入。 */
  function openReset() {
    setResetCode("");
    setResetPassword("");
    setResetConfirmation("");
    setResetMessage("");
    setResetOpen(true);
  }

  /** sendResetCode 请求向当前邮箱发送重置密码验证码。 */
  async function sendResetCode() {
    setResetMessage("");
    if (!normalizedEmail) {
      setResetOpen(false);
      return setError("请先填写邮箱");
    }
    setResetBusy(true);
    try {
      const data = await apiRequest("/api/auth/password/reset-code", {
        method: "POST",
        body: JSON.stringify({ email: normalizedEmail }),
      });
      setResetCooldown(60);
      if (data.debug_code) setResetCode(String(data.debug_code));
      setResetMessage(
        data.debug_code
          ? "本地开发验证码已自动填好"
          : "验证码已发送，请查看邮箱",
      );
    } catch (requestError) {
      setResetMessage(errorMessage(requestError));
    } finally {
      setResetBusy(false);
    }
  }

  /** submitReset 用验证码重设密码，成功后回到密码登录。 */
  async function submitReset() {
    if (resetBusy) return;
    setResetMessage("");
    if (resetCode.trim().length !== 4)
      return setResetMessage("请输入 4 位验证码");
    if ([...resetPassword].length < 2)
      return setResetMessage("新密码至少需要 2 个字符");
    if (resetPassword !== resetConfirmation)
      return setResetMessage("两次新密码不一样，再核对一下");
    setResetBusy(true);
    try {
      await apiRequest("/api/auth/password/reset", {
        method: "POST",
        body: JSON.stringify({
          email: normalizedEmail,
          code: resetCode.trim(),
          password: resetPassword,
        }),
      });
      setResetOpen(false);
      setLoginMode("password");
      setPassword(resetPassword);
      setError("");
      setMessage("密码已经重设好了，直接用新密码登录就行");
    } catch (requestError) {
      setResetMessage(errorMessage(requestError));
    } finally {
      setResetBusy(false);
    }
  }
  /** ensureAgreementAccepted 确认当前邮箱是否已经完成协议确认。 */
  async function ensureAgreementAccepted(confirmed: boolean) {
    if (confirmed || acceptedEmail === normalizedEmail || agreementChecked) return true;
    const data = await apiRequest(
      `/api/auth/agreement-status?email=${encodeURIComponent(normalizedEmail)}`,
      { method: "GET" },
    );
    if (data.agreement_accepted) {
      setAcceptedEmail(normalizedEmail);
      return true;
    }
    setAgreementOpen(true);
    setContinueAfterAgreement(true);
    trackConversion("agreement_opened", { flow: authFlow, method: loginMode });
    return false;
  }

  /** finishLogin 保存登录凭证，并在需要时先要求历史账号设置密码。 */
  function finishLogin(data: any) {
    const token = String(data.access_token || "");
    if (!token) throw new Error("登录成功但未返回登录凭证");
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(SESSION_EMAIL_KEY, String(data.user?.email || normalizedEmail));
    localStorage.removeItem(INVITE_CACHE_KEY);
    trackConversion(loginMode === "code" && isNewAccount ? "register_success" : "login_success", { flow: authFlow, method: loginMode });
    markPendingAdminArrival(authFlow);
    if (data.requires_password_setup) {
      trackConversion("password_setup_required", { flow: authFlow, method: loginMode });
      setPasswordSetupRequired(true);
      setMessage("验证码通过了，再补一个密码就能继续");
      return;
    }
    setMessage("登录成功，正在进入控制台");
    window.location.assign(resolveNextPath());
  }

  /** login 根据当前方式提交验证码或密码登录。 */
  async function login(confirmed = false) {
    if (loading || loginBusy.current) return;
    setError("");
    setMessage("");
    if (!normalizedEmail) return validationError("请先填写邮箱", "email");
    if (loginMode === "password" && !password) return validationError("请输入密码", "password");
    if (loginMode === "code" && code.trim().length !== 4)
      return validationError("请输入 4 位验证码", "code");
    if (loginMode === "code" && isNewAccount === null)
      return validationError("请先发送验证码", "send_code_first");
    if (loginMode === "code" && isNewAccount && [...password].length < 2)
      return validationError("首次注册需要设置至少 2 个字符的密码", "password");
    if (
      loginMode === "code" &&
      isNewAccount &&
      password !== passwordConfirmation
    )
      return validationError("两次密码不一样，我先不敢注册", "password_confirmation");
    loginBusy.current = true;
    setLoading(true);
    if (!confirmed) trackConversion("login_submit", { flow: authFlow, method: loginMode });
    try {
      if (!(await ensureAgreementAccepted(confirmed))) return;
      const inviterID = localStorage.getItem(INVITE_CACHE_KEY) || "";
      const data = await apiRequest(
        loginMode === "password"
          ? "/api/auth/password-login"
          : "/api/auth/login",
        {
          method: "POST",
          body: JSON.stringify(
            loginMode === "password"
              ? {
                  email: normalizedEmail,
                  password,
                  agreement_accepted: confirmed || agreementChecked,
                }
              : {
                  email: normalizedEmail,
                  code: code.trim(),
                  password: isNewAccount ? password : "",
                  inviter_id: inviterID,
                  agreement_accepted: confirmed || agreementChecked,
                },
          ),
        },
      );
      finishLogin(data);
    } catch (requestError) {
      trackConversion("login_failed", { flow: authFlow, method: loginMode, reason: classifyAuthFailure(requestError) });
      setError(errorMessage(requestError));
    } finally {
      loginBusy.current = false;
      setLoading(false);
    }
  }

  /** validationError 显示表单问题并记录固定字段分类，不上传用户填写内容。 */
  function validationError(message: string, reason: "email" | "code" | "password" | "send_code_first" | "password_confirmation") {
    setError(message);
    trackConversion("validation_failed", { flow: authFlow, method: loginMode, reason });
  }

  /** changeAgreement 保存本人勾选偏好；主动取消时清除缓存，不把缓存当作云端登录或协议记录。 */
  function changeAgreement(checked: boolean) {
    setAgreementChecked(checked);
    if (!checked) setAcceptedEmail("");
    try {
      if (checked) localStorage.setItem(AGREEMENT_CACHE_KEY, "accepted");
      else localStorage.removeItem(AGREEMENT_CACHE_KEY);
    } catch { /* 浏览器禁用存储时仅保留当前页面勾选，不阻断登录。 */ }
    if (checked) trackConversion("agreement_accepted", { flow: authFlow, method: loginMode });
  }

  /** acceptAgreement 确认协议并自动继续待完成的登录，不再要求用户重复点击。 */
  function acceptAgreement() {
    changeAgreement(true);
    setAcceptedEmail(normalizedEmail);
    setAgreementOpen(false);
    setError("");
    setContinueAfterAgreement(false);
    if (continueAfterAgreement) void login(true);
  }

  /** openAgreement 单独预览协议，不在用户只阅读协议时发起登录。 */
  function openAgreement() {
    setContinueAfterAgreement(false);
    setAgreementOpen(true);
    trackConversion("agreement_opened", { flow: authFlow, method: loginMode });
  }

  /** cancelAgreement 关闭协议并取消待续登录，保留用户已填的表单。 */
  function cancelAgreement() {
    setContinueAfterAgreement(false);
    setAgreementOpen(false);
    trackConversion("agreement_cancelled", { flow: authFlow, method: loginMode });
  }

  return (
    <>
      <Form
        layout="vertical"
        requiredMark={false}
        onFinish={() => void login()}
        className="public-login-form"
      >
        <div className="public-login-tabs" style={{ gridTemplateColumns: "repeat(3, minmax(0, 1fr))" }}>
          <Button block type={loginMode === "wechat" ? "primary" : "default"} onClick={() => setLoginMode("wechat")} disabled={loading}>微信扫码</Button>
          <Button
            block
            type={loginMode === "code" ? "primary" : "default"}
            onClick={() => setLoginMode("code")}
            disabled={loading}
          >
            邮箱验证码
          </Button>
          <Button
            block
            type={loginMode === "password" ? "primary" : "default"}
            onClick={() => setLoginMode("password")}
            disabled={loading}
          >
            邮箱密码
          </Button>
        </div>
        {!loginPreferencesLoaded ? null : loginMode === "wechat" ? <WechatLogin agreementAccepted={agreementChecked} onAgreementChange={changeAgreement} onAgreement={openAgreement} onSuccess={finishLogin} /> : <>
        <Form.Item label="邮箱" htmlFor="login-email">
          <Input
            id="login-email"
            placeholder="请输入邮箱(12242993@qq.com 为示例)"
            type="email"
            autoComplete="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            disabled={loading}
            prefix={<MailOutlined aria-hidden="true" />}
          />
        </Form.Item>
        {loginMode === "code" ? (
          <Form.Item label="验证码" htmlFor="login-code">
            <Input
              id="login-code"
              inputMode="numeric"
              placeholder="请输入4位验证码"
              autoComplete="one-time-code"
              value={code}
              onChange={(event) =>
                setCode(event.target.value.replace(/\D/g, "").slice(0, 4))
              }
              disabled={loading}
              prefix={<SafetyCertificateOutlined aria-hidden="true" />}
              suffix={
                <Button
                  type="text"
                  size="small"
                  onClick={() => void sendCode()}
                  disabled={loading || cooldown > 0 || !email.trim()}
                >
                  {cooldown > 0 ? cooldown + "s 后重试" : "发送验证码"}
                </Button>
              }
            />
          </Form.Item>
        ) : null}
        {loginMode === "password" || isNewAccount ? (
          <Form.Item
            label={
              isNewAccount && loginMode === "code" ? "设置登录密码" : "密码"
            }
            htmlFor="login-password"
            extra={
              isNewAccount && loginMode === "code"
                ? "首次注册必须设置至少 2 个字符的密码"
                : undefined
            }
          >
            <Input.Password
              id="login-password"
              autoComplete={isNewAccount ? "new-password" : "current-password"}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              disabled={loading}
              prefix={<LockOutlined aria-hidden="true" />}
            />
          </Form.Item>
        ) : null}
        {loginMode === "code" && isNewAccount ? (
          <Form.Item
            label="再输入一次密码"
            htmlFor="login-password-confirmation"
          >
            <Input.Password
              id="login-password-confirmation"
              autoComplete="new-password"
              value={passwordConfirmation}
              onChange={(event) => setPasswordConfirmation(event.target.value)}
              disabled={loading}
            />
          </Form.Item>
        ) : null}
        <div className="public-login-inline">
          <Typography.Paragraph
            className="public-login-hint"
            style={{
              margin: 0,
              fontSize: 12,
              color: "var(--goodhr-text-secondary, #637069)",
            }}
          >
            同一账号支持多处登录，不会再互相挤下线。
          </Typography.Paragraph>
        </div>
        <div className="public-login-agreement">
          <div className="public-login-agreement-label">
          <Checkbox
            checked={agreementReady}
            disabled={loading}
            onChange={(event) => changeAgreement(event.target.checked)}
          >我已阅读并同意</Checkbox>
          <Button type="link" className="public-agreement-link" onClick={openAgreement} disabled={loading}>
            《使用协议与隐私说明》
          </Button>
          </div>
          <Button type="text" size="small" className="public-login-forgot" onClick={openReset} disabled={loading}>
            忘记密码
          </Button>
        </div>
        {error ? (
          <Alert
            type="error"
            title={error}
            showIcon
            style={{ marginBottom: 16 }}
          />
        ) : null}
        {message ? (
          <Alert
            type="success"
            title={message}
            showIcon
            style={{ marginBottom: 16 }}
          />
        ) : null}
        <Button
          htmlType="submit"
          type="primary"
          size="large"
          block
          loading={loading}
          disabled={
            loading ||
            !email.trim() ||
            (loginMode === "code" ? code.length !== 4 : !password)
          }
          icon={<ArrowRightOutlined aria-hidden="true" />}
          iconPlacement="end"
        >
          {loading
            ? "正在处理"
            : loginMode === "password"
              ? "密码登录"
              : isNewAccount
                ? "注册并进入控制台"
                : "登录并进入控制台"}
        </Button>
        </>}
      </Form>
      <Typography.Paragraph
        className="public-login-note"
        style={{
          margin: "20px 0 16px",
          fontSize: 13,
          lineHeight: 1.7,
          color: "var(--goodhr-text-secondary, #637069)",
        }}
      >
        {loginMode === "wechat" ? "登录即将使用当前微信对应的 GoodHR 账号。请只发送自己页面上的验证码。" : "新邮箱注册需要验证码和密码。老账号没有密码也别慌，先用验证码登录，我会带你补上。"}
      </Typography.Paragraph>
      <Modal
        rootClassName="public-theme-portal"
        open={agreementOpen}
        onCancel={cancelAgreement}
        title="GoodHR 使用协议与隐私说明"
        width={900}
        footer={
          <div className="public-agreement-footer">
            <div className="public-login-actions">
              <Button onClick={cancelAgreement}>先不登录</Button>
              <Button
                type="primary"
                onClick={acceptAgreement}
              >
                我已阅读
              </Button>
            </div>
          </div>
        }
      >
        <div
          className="public-agreement-content"
          tabIndex={0}
          aria-label="使用协议内容"
        >
          <AgreementMarkdown markdown={AGREEMENT_MARKDOWN} />
        </div>
      </Modal>
      <Modal
        rootClassName="public-theme-portal"
        open={resetOpen}
        onCancel={() => {
          if (!resetBusy) setResetOpen(false);
        }}
        title="忘记密码"
        width={440}
        mask={{ closable: !resetBusy }}
        keyboard={!resetBusy}
        closable={!resetBusy}
        footer={
          <div className="public-login-actions">
            <Button onClick={() => setResetOpen(false)} disabled={resetBusy}>
              先不换了
            </Button>
            <Button
              type="primary"
              onClick={() => void submitReset()}
              loading={resetBusy}
              disabled={resetBusy}
            >
              重设密码
            </Button>
          </div>
        }
      >
        <Typography.Paragraph
          className="public-login-note"
          style={{
            margin: "20px 0 16px",
            fontSize: 13,
            lineHeight: 1.7,
            color: "var(--goodhr-text-secondary, #637069)",
          }}
        >
          别慌，用 {normalizedEmail || "你的邮箱"}{" "}
          收一个验证码，就能设一个新密码。
        </Typography.Paragraph>
        <Form layout="vertical" requiredMark={false}>
          <Form.Item label="验证码" htmlFor="reset-password-code">
            <Input
              id="reset-password-code"
              inputMode="numeric"
              placeholder="请输入4位验证码"
              autoComplete="one-time-code"
              value={resetCode}
              onChange={(event) =>
                setResetCode(event.target.value.replace(/\D/g, "").slice(0, 4))
              }
              disabled={resetBusy}
              suffix={
                <Button
                  type="text"
                  size="small"
                  onClick={() => void sendResetCode()}
                  disabled={resetBusy || resetCooldown > 0 || !email.trim()}
                >
                  {resetCooldown > 0
                    ? resetCooldown + "s 后重试"
                    : "发送验证码"}
                </Button>
              }
            />
          </Form.Item>
          <Form.Item
            label="新密码"
            htmlFor="reset-new-password"
            extra="至少 2 个字符。"
          >
            <Input.Password
              id="reset-new-password"
              autoComplete="new-password"
              value={resetPassword}
              onChange={(event) => setResetPassword(event.target.value)}
              disabled={resetBusy}
            />
          </Form.Item>
          <Form.Item label="再输入一次新密码" htmlFor="reset-confirm-password">
            <Input.Password
              id="reset-confirm-password"
              autoComplete="new-password"
              value={resetConfirmation}
              onChange={(event) => setResetConfirmation(event.target.value)}
              disabled={resetBusy}
              onPressEnter={(event) => {
                event.preventDefault();
                void submitReset();
              }}
            />
          </Form.Item>
          {resetMessage ? (
            <Alert type="info" title={resetMessage} showIcon />
          ) : null}
        </Form>
      </Modal>
      <PasswordSetupDialog
        open={passwordSetupRequired}
        email={normalizedEmail}
        onSubmit={async (nextPassword) => {
          const token = localStorage.getItem(TOKEN_KEY) || "";
          await apiRequest("/api/auth/password/setup", {
            method: "POST",
            headers: { Authorization: "Bearer " + token },
            body: JSON.stringify({ password: nextPassword }),
          });
          setPasswordSetupRequired(false);
          trackConversion("password_setup_completed", { flow: authFlow, method: loginMode });
          window.location.assign(resolveNextPath());
        }}
      />
    </>
  );
}

/** AgreementMarkdown 将协议 Markdown 转换为轻量展示内容。 */
function AgreementMarkdown({ markdown }: { markdown: string }) {
  return (
    <div className="public-agreement-markdown">
      {markdown.split("\n").map((line, index) => {
        const text = line.trim();
        if (!text) return <div key={index} style={{ height: 4 }} />;
        if (text.startsWith("## ")) {
          return (
            <Typography.Title
              key={index}
              level={2}
              style={{ margin: 0, fontSize: 22, fontWeight: 820 }}
            >
              {stripMarkdown(text.replace(/^##\s+/, ""))}
            </Typography.Title>
          );
        }
        if (text.startsWith("### ")) {
          return (
            <Typography.Title
              key={index}
              level={3}
              style={{ margin: "12px 0 0", fontSize: 17, fontWeight: 800 }}
            >
              {stripMarkdown(text.replace(/^###\s+/, ""))}
            </Typography.Title>
          );
        }
        const strong = text.match(/^\*\*(.*)\*\*$/);
        return (
          <Typography.Paragraph
            key={index}
            style={{
              margin: 0,
              color: strong
                ? "var(--goodhr-text, #17211c)"
                : "var(--goodhr-text-secondary, #637069)",
              fontWeight: strong ? 760 : 400,
              lineHeight: 1.75,
            }}
          >
            {stripMarkdown(strong?.[1] || text)}
          </Typography.Paragraph>
        );
      })}
    </div>
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
