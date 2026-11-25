"use client";

/**
 * 本文件负责 WorkBuddy 连接器的 OAuth 授权确认页：
 * 完成登录检查、本地程序在线状态展示、下载引导，用户同意后向云端换取授权码并跳回回调地址。
 */
import { useCallback, useEffect, useRef, useState } from "react";
import "@/components/public-site.css";
import Button from "antd/es/button";
import Paragraph from "antd/es/typography/Paragraph";
import Title from "antd/es/typography/Title";
import Alert from "antd/es/alert";
import Spin from "antd/es/spin";
import Checkbox from "antd/es/checkbox";
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import DownloadOutlined from "@ant-design/icons/lib/icons/DownloadOutlined";
import BrandMark from "@/components/BrandMark";
import LoginForm from "@/components/LoginForm";
import { apiRequest, TOKEN_KEY } from "@/lib/api";
import {
  APIRequestError,
  bindDetectedLocalAgent,
  clearLocalAgentDetectCache,
  cloudRequest,
  detectLocalAgent,
  syncCloudSessionToLocalAgent,
} from "@/lib/admin-api";

/** AuthorizeParams 表示 OAuth 授权请求里页面需要透传的参数。 */
type AuthorizeParams = {
  client_id: string;
  redirect_uri: string;
  state: string;
  scope: string;
  code_challenge: string;
  code_challenge_method: string;
};

/** AgentState 表示本地程序的连接情况。 */
type AgentState = "checking" | "online" | "offline" | "error";

/** AuthorizationTarget 保存服务器预检通过的客户端自报名称和已登记回调，不代表官方身份认证。 */
type AuthorizationTarget = { clientName: string; redirectURI: string; callbackOrigin: string };

const grantedItems = [
  "读取你的岗位清单、订阅状态和筛选结果",
  "在你确认后启动岗位筛选任务",
  "只在你同意授权的期间生效，随时可以在控制台解除",
];

/**
 * readAgentConnection 有界读取真实命令通道状态，兼容旧浏览器。
 * @param token - 本次认证固定的登录凭证，不能临时换成其他标签页的账号。
 * @returns 当前账号的命令通道是否在线。
 */
async function readAgentConnection(token: string) {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), 8000);
  try {
    const status = await cloudRequest("/api/agents/ws-status", { auth: false, headers: { Authorization: `Bearer ${token}` }, signal: controller.signal });
    if (typeof status.connected !== "boolean") throw new Error("连接状态没读完整，请重新检查。");
    return status.connected;
  } finally {
    window.clearTimeout(timer);
  }
}

/** AuthorizePage 展示授权确认卡片和本地程序引导。 */
export default function AuthorizePage() {
  const [phase, setPhase] = useState<"loading" | "error" | "account" | "authenticate" | "ready">("loading");
  const [accountIntent, setAccountIntent] = useState<"login" | "register" | null>(null);
  const [authBusy, setAuthBusy] = useState(false);
  const [authorizationTarget, setAuthorizationTarget] = useState<AuthorizationTarget | null>(null);
  const [authorizationConfirmed, setAuthorizationConfirmed] = useState(false);
  const [errorText, setErrorText] = useState("");
  const [params, setParams] = useState<AuthorizeParams | null>(null);
  const [agent, setAgent] = useState<AgentState>("checking");
  const [agentError, setAgentError] = useState("");
  const [submitting, setSubmitting] = useState<"approve" | "deny" | null>(null);
  const startedRef = useRef(false);
  const checkingRef = useRef(false);
  const activeRef = useRef(true);
  const authorizationConfirmedRef = useRef(false);
  const authenticatedTokenRef = useRef("");

  useEffect(() => {
    activeRef.current = true;
    return () => { activeRef.current = false; };
  }, []);

  /** resetAccountSelection 退出本次身份确认，不清理其他标签刚刚登录的新账号。 */
  const resetAccountSelection = useCallback((message: string) => {
    if (!activeRef.current) return;
    authenticatedTokenRef.current = "";
    authorizationConfirmedRef.current = false;
    setAuthorizationConfirmed(false);
    setAccountIntent(null);
    setAuthBusy(false);
    setSubmitting(null);
    setPhase("account");
    setErrorText(message);
  }, []);

  /** requireAuthentication 只清理本次已经过期的会话，随后留在本页重新选择账号。 */
  const requireAuthentication = useCallback((token: string) => {
    if (!activeRef.current || token !== authenticatedTokenRef.current) return;
    if (token === window.localStorage.getItem(TOKEN_KEY)) {
      window.localStorage.removeItem(TOKEN_KEY);
      resetAccountSelection("登录状态过期了，请重新选择账号并确认本次授权。");
    } else {
      resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。");
    }
  }, [resetAccountSelection]);

  /** submitConsent 把授权决定提交给云端并跳回回调地址。 */
  const submitConsent = useCallback(
    async (decision: "approve" | "deny") => {
      if (!params || !authorizationTarget) return;
      if (decision === "approve" && !authorizationConfirmedRef.current) {
        setErrorText("请先确认授权接收方、回调地址和以上权限。");
        return;
      }
      const token = authenticatedTokenRef.current;
      if (!token || token !== window.localStorage.getItem(TOKEN_KEY)) {
        resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。");
        return;
      }
      setSubmitting(decision);
      try {
        const data = await cloudRequest("/api/workbuddy/authorize", {
          auth: false,
          headers: { Authorization: `Bearer ${token}` },
          method: "POST",
          body: { ...params, decision },
        });
        if (!activeRef.current || token !== authenticatedTokenRef.current) return;
        if (token !== window.localStorage.getItem(TOKEN_KEY)) {
          resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。");
          return;
        }
        const redirect = String(data.redirect_url || "");
        if (!redirect) throw new Error("回调地址没有返回，请回到刚才的应用重新连接");
        window.location.assign(redirect);
      } catch (error) {
        if (!activeRef.current || token !== authenticatedTokenRef.current) return;
        if (error instanceof APIRequestError && error.status === 401) {
          requireAuthentication(token);
          setSubmitting(null);
          return;
        }
        setErrorText(error instanceof Error ? error.message : "授权没有完成，请重试");
        setSubmitting(null);
      }
    },
    [params, authorizationTarget, requireAuthentication, resetAccountSelection],
  );

  /** checkAgent 复用账号绑定与登录同步，再以云端命令通道的真实连接状态判断在线。 */
  const checkAgent = useCallback(async () => {
    if (checkingRef.current) return;
    const token = authenticatedTokenRef.current;
    if (!token || token !== window.localStorage.getItem(TOKEN_KEY)) {
      resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。");
      return;
    }
    checkingRef.current = true;
    setAgent("checking");
    setAgentError("");
    /** currentCheck 防止离开授权页或切换账号后回写旧检查结果。 */
    const currentCheck = () => {
      if (!activeRef.current || token !== authenticatedTokenRef.current) return false;
      if (token !== window.localStorage.getItem(TOKEN_KEY)) {
        resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。");
        return false;
      }
      return true;
    };
    try {
      // 先校验云端登录，再向本机同步；会话失效时留在本页重新选择账号。
      let connected = await readAgentConnection(token);
      if (!currentCheck()) return;
      clearLocalAgentDetectCache();
      const baseURL = await detectLocalAgent();
      if (!currentCheck()) return;
      if (baseURL) {
        await bindDetectedLocalAgent(baseURL, 8000, token);
        if (!currentCheck()) return;
        await syncCloudSessionToLocalAgent(baseURL, 8000, token);
        // 本地通道最多每五秒取一次新凭证；短暂等待实际回执，不把同步受理当成在线。
        const deadline = Date.now() + 6000;
        for (let attempt = 0; attempt < 6; attempt++) {
          if (!currentCheck()) return;
          connected = await readAgentConnection(token);
          if (connected || Date.now() >= deadline || attempt === 5) break;
          await new Promise<void>((resolve) => window.setTimeout(resolve, 1000));
        }
      }
      if (currentCheck()) setAgent(connected ? "online" : "offline");
    } catch (error) {
      if (!currentCheck()) return;
      if (error instanceof APIRequestError && error.status === 401) {
        requireAuthentication(token);
        return;
      }
      setAgent("error");
      setAgentError(error instanceof Error ? error.message : "暂时没查到连接状态，请重新检查。");
    } finally {
      checkingRef.current = false;
    }
  }, [requireAuthentication, resetAccountSelection]);

  /**
   * finishAuthentication 先有界同步本地程序，再继续OAuth；未安装或本机连接失败不阻断授权。
   * @param data - 当前登录或注册实际返回的认证结果。
   * @returns 本次本地同步与授权续接结束后完成的异步结果。
   */
  const finishAuthentication = useCallback(async (data: Record<string, unknown>) => {
    if (!activeRef.current || !authorizationConfirmedRef.current) return;
    const token = typeof data.access_token === "string" ? data.access_token : "";
    if (!token || token !== window.localStorage.getItem(TOKEN_KEY)) {
      resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。");
      return;
    }
    authenticatedTokenRef.current = token;
    setErrorText("");
    setAuthBusy(false);
    setPhase("ready");
    setSubmitting("approve");
    await checkAgent();
    if (!activeRef.current || token !== authenticatedTokenRef.current || token !== window.localStorage.getItem(TOKEN_KEY) || !authorizationConfirmedRef.current) {
      if (activeRef.current) setSubmitting(null);
      return;
    }
    await submitConsent("approve");
  }, [checkAgent, submitConsent, resetAccountSelection]);

  useEffect(() => {
    if (startedRef.current) return;
    startedRef.current = true;
    const search = window.location.search;
    const query = new URLSearchParams(search);
    const nextParams: AuthorizeParams = {
      client_id: query.get("client_id") || "",
      redirect_uri: query.get("redirect_uri") || "",
      state: query.get("state") || "",
      scope: query.get("scope") || "",
      code_challenge: query.get("code_challenge") || "",
      code_challenge_method: query.get("code_challenge_method") || "",
    };
    if (!nextParams.client_id || !nextParams.redirect_uri || !nextParams.code_challenge) {
      setErrorText("授权链接不完整，请回到刚才的应用重新发起连接。");
      setPhase("error");
      return;
    }
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 8000);
    /** prepareAuthorization 先让服务器校验客户端、回调和PKCE，失败时不展示认证入口。 */
    async function prepareAuthorization() {
      try {
        const data = await apiRequest(`/api/workbuddy/authorize?${new URLSearchParams(nextParams)}`, { signal: controller.signal });
        if (!activeRef.current) return;
        if (![data.client_name, data.redirect_uri, data.callback_origin].every((value) => typeof value === "string" && value.trim()) || data.redirect_uri !== nextParams.redirect_uri) {
          throw new Error("授权接收方信息没有读完整，请重新检查授权链接。");
        }
        setAuthorizationTarget({ clientName: data.client_name, redirectURI: data.redirect_uri, callbackOrigin: data.callback_origin });
        setParams(nextParams);
        // 这次绑定由用户选择账号入口，不能凭浏览器旧会话跳过身份确认。
        setPhase("account");
      } catch (cause) {
        if (!activeRef.current) return;
        setErrorText(cause instanceof Error ? cause.message : "授权链接暂时没查清楚，请重新检查。");
        setPhase("error");
      } finally {
        window.clearTimeout(timeout);
      }
    }
    void prepareAuthorization();
  }, []);

  return (
    <div className="public-login-1">
      <div className="public-container public-login-2">
        <div className="public-login-3">
          <BrandMark />
        </div>
      </div>
      <div className="public-container public-login-7">
        <div style={{ maxWidth: 520, margin: "0 auto" }}>
          <div className="public-panel public-login-17">
            <Title
              level={2}
              className="public-text public-login-18"
              style={{ margin: 0, color: "var(--goodhr-text, #17211c)", fontSize: 24, fontWeight: 750 }}
            >
              连接你的 GoodHR 账号
            </Title>
            <Paragraph
              className="public-text public-login-19"
              style={{ margin: "8px 0 20px", color: "var(--goodhr-text-secondary, #637069)" }}
            >
              {phase === "loading" && "我先看看授权链接……"}
              {phase === "error" && "这次授权没能继续"}
              {(phase === "account" || phase === "authenticate") && "先核对接收方和权限，再选择登录或注册。"}
              {phase === "ready" && "账号已经确认，我会按你的授权选择继续连接。"}
            </Paragraph>

            {phase === "loading" && (
              <div style={{ textAlign: "center", padding: "32px 0" }}>
                <Spin />
              </div>
            )}

            {phase === "error" && (
              <>
                <Alert type="warning" showIcon title={errorText} />
                <Button style={{ marginTop: 12 }} onClick={() => window.location.reload()}>重新检查授权链接</Button>
              </>
            )}

            {params && authorizationTarget && phase !== "loading" && phase !== "error" && (
              <>
                <Alert
                  type="info"
                  showIcon
                  title="请核对授权接收方"
                  description={<div style={{ overflowWrap: "anywhere" }}>
                    <Paragraph style={{ marginBottom: 8 }}><strong>授权接收方：</strong>{authorizationTarget.clientName}（客户端自报）</Paragraph>
                    <Paragraph style={{ marginBottom: 8 }}><strong>回调来源：</strong>{authorizationTarget.callbackOrigin}</Paragraph>
                    <Paragraph style={{ marginBottom: 0 }}><strong>回调地址：</strong>{authorizationTarget.redirectURI}</Paragraph>
                  </div>}
                  style={{ marginBottom: 16 }}
                />
                <div style={{ margin: "0 0 16px" }}>
                  {grantedItems.map((item) => (
                    <div key={item} style={{ display: "flex", gap: 8, alignItems: "flex-start", marginBottom: 10 }}>
                      <CheckCircleRoundedIcon style={{ color: "var(--goodhr-primary, #159a62)", marginTop: 3 }} aria-hidden="true" />
                      <Paragraph style={{ margin: 0, color: "var(--goodhr-text-secondary, #637069)", fontSize: 13 }}>
                        {item}
                      </Paragraph>
                    </div>
                  ))}
                </div>
                <div style={{ marginBottom: 16 }}>
                  <Checkbox checked={authorizationConfirmed} disabled={authBusy || submitting !== null} onChange={(event) => {
                    authorizationConfirmedRef.current = event.target.checked;
                    setAuthorizationConfirmed(event.target.checked);
                  }}>我确认上方应用和回调地址，并同意以上权限</Checkbox>
                </div>
              </>
            )}

            {phase === "account" && (
              <>
                {errorText && <Alert type="warning" showIcon title={errorText} style={{ marginBottom: 12 }} />}
                <Paragraph strong>你已经有 GoodHR 账号了吗？</Paragraph>
                <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
                  <Button type="primary" onClick={() => { setAccountIntent("login"); setPhase("authenticate"); }}>我有账号</Button>
                  <Button onClick={() => { setAccountIntent("register"); setPhase("authenticate"); }}>我还没有账号</Button>
                </div>
              </>
            )}

            {phase === "authenticate" && accountIntent && (
              <>
                <div style={{ display: "flex", justifyContent: "space-between", gap: 8, alignItems: "center", marginBottom: 12 }}>
                  <Paragraph strong style={{ margin: 0 }}>{accountIntent === "register" ? "注册 GoodHR 并绑定" : "登录 GoodHR 并绑定"}</Paragraph>
                  <Button type="text" disabled={authBusy} onClick={() => { setAccountIntent(null); setPhase("account"); }}>重新选择</Button>
                </div>
                {authorizationConfirmed ? <LoginForm key={accountIntent} intent={accountIntent} onAuthenticated={finishAuthentication} onBusyChange={setAuthBusy} onSessionChanged={() => resetAccountSelection("登录账号已经变化，请重新选择账号并确认授权。")} /> : <Alert type="info" showIcon title="请先确认上方授权接收方、回调地址和权限，再继续。" />}
              </>
            )}

            {phase === "ready" && params && (
              <>

                {agent === "checking" && (
                  <Paragraph style={{ margin: "0 0 12px", color: "var(--goodhr-text-secondary, #637069)", fontSize: 12 }}>
                    我在看看你的本地程序连上了没有……
                  </Paragraph>
                )}
                {agent === "online" && (
                  <Alert
                    type="success"
                    showIcon
                    title="本地程序已连接任务通道"
                    description="首次使用还要在控制台完成组件安装、创建岗位，并在招聘浏览器里登录和设置筛选条件。准备好后再回刚才的应用启动任务。"
                    style={{ marginBottom: 12 }}
                  />
                )}
                {(agent === "offline" || agent === "error") && (
                  <Alert
                    type={agent === "error" ? "warning" : "info"}
                    showIcon
                    title={agent === "error" ? "连接状态暂时没查到" : "本地程序还没连接"}
                    description={agent === "error" ? agentError : "还没安装 GoodHR 也可以先授权。请下载安装 GoodHR，启动后在控制台登录同一账号，完成组件安装、创建岗位和招聘平台登录。"}
                    style={{ marginBottom: 12 }}
                  />
                )}

                <div style={{ display: "flex", flexWrap: "wrap", gap: 8, marginBottom: 12 }}>
                  {agent !== "online" && <Button href="/download" target="_blank" rel="noopener noreferrer" icon={<DownloadOutlined aria-hidden="true" />}>下载安装 GoodHR</Button>}
                  <Button href="/admin/" target="_blank" rel="noopener noreferrer">打开控制台准备</Button>
                  <Button loading={agent === "checking"} disabled={submitting !== null} onClick={() => { void checkAgent(); }}>安装后重新检查</Button>
                </div>

                {errorText && (
                  <Alert type="error" showIcon title={errorText} style={{ marginBottom: 12 }} />
                )}

                <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 4 }}>
                  <Button
                    onClick={() => submitConsent("deny")}
                    disabled={submitting !== null}
                    style={{ borderRadius: 999 }}
                  >
                    暂不授权
                  </Button>
                  <Button
                    type="primary"
                    loading={submitting === "approve"}
                    disabled={submitting === "deny" || !authorizationConfirmed}
                    onClick={() => submitConsent("approve")}
                    style={{ borderRadius: 999 }}
                  >
                    同意授权
                  </Button>
                </div>
              </>
            )}
          </div>
          <Paragraph
            style={{ margin: "12px 4px 0", color: "var(--goodhr-text-secondary, #637069)", fontSize: 12 }}
          >
            授权会把筛选任务的启动和结果查询交给上方应用，招聘平台的登录状态和数据仍然只保存在你的电脑里。
          </Paragraph>
        </div>
      </div>
    </div>
  );
}
