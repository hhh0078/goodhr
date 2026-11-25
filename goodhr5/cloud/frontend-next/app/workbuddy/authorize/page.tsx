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
import CheckCircleRoundedIcon from "@ant-design/icons/lib/icons/CheckCircleOutlined";
import DownloadOutlined from "@ant-design/icons/lib/icons/DownloadOutlined";
import BrandMark from "@/components/BrandMark";
import { TOKEN_KEY } from "@/lib/api";
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

const grantedItems = [
  "读取你的岗位清单、订阅状态和筛选结果",
  "在你确认后启动岗位筛选任务",
  "只在你同意授权的期间生效，随时可以在控制台解除",
];

/** readAgentConnection 有界读取真实命令通道状态，沿用旧浏览器支持的 AbortController。 */
async function readAgentConnection() {
  const controller = new AbortController();
  const timer = window.setTimeout(() => controller.abort(), 8000);
  try {
    const status = await cloudRequest("/api/agents/ws-status", { signal: controller.signal });
    if (typeof status.connected !== "boolean") throw new Error("连接状态没读完整，请重新检查。");
    return status.connected;
  } finally {
    window.clearTimeout(timer);
  }
}

/** AuthorizePage 展示授权确认卡片和本地程序引导。 */
export default function AuthorizePage() {
  const [phase, setPhase] = useState<"loading" | "error" | "ready">("loading");
  const [errorText, setErrorText] = useState("");
  const [params, setParams] = useState<AuthorizeParams | null>(null);
  const [agent, setAgent] = useState<AgentState>("checking");
  const [agentError, setAgentError] = useState("");
  const [submitting, setSubmitting] = useState<"approve" | "deny" | null>(null);
  const startedRef = useRef(false);
  const checkingRef = useRef(false);
  const activeRef = useRef(true);

  useEffect(() => {
    activeRef.current = true;
    return () => { activeRef.current = false; };
  }, []);

  /** redirectLogin 未登录时跳登录页，登录后回到本授权页。 */
  const redirectLogin = useCallback((search: string) => {
    const next = `/workbuddy/authorize${search}`;
    window.location.assign(`/login?next=${encodeURIComponent(next)}`);
  }, []);

  /** submitConsent 把授权决定提交给云端并跳回回调地址。 */
  const submitConsent = useCallback(
    async (decision: "approve" | "deny") => {
      if (!params) return;
      setSubmitting(decision);
      try {
        const data = await cloudRequest("/api/workbuddy/authorize", {
          method: "POST",
          body: { ...params, decision },
        });
        const redirect = String(data.redirect_url || "");
        if (!redirect) throw new Error("回调地址没有返回，请回到 WorkBuddy 重新连接");
        window.location.assign(redirect);
      } catch (error) {
        setErrorText(error instanceof Error ? error.message : "授权没有完成，请重试");
        setSubmitting(null);
      }
    },
    [params],
  );

  /** checkAgent 复用账号绑定与登录同步，再以云端命令通道的真实连接状态判断在线。 */
  const checkAgent = useCallback(async () => {
    if (checkingRef.current) return;
    checkingRef.current = true;
    setAgent("checking");
    setAgentError("");
    const token = window.localStorage.getItem(TOKEN_KEY) || "";
    /** currentCheck 防止离开授权页或切换账号后回写旧检查结果。 */
    const currentCheck = () => activeRef.current && token === window.localStorage.getItem(TOKEN_KEY);
    try {
      // 先校验云端登录，再向本机同步；失效凭证由共享请求保留完整 OAuth next 后转到登录页。
      let connected = await readAgentConnection();
      if (!currentCheck()) return;
      if (!connected) {
        clearLocalAgentDetectCache();
        const baseURL = await detectLocalAgent();
        if (!currentCheck()) return;
        if (baseURL) {
          await bindDetectedLocalAgent(baseURL);
          if (!currentCheck()) return;
          await syncCloudSessionToLocalAgent(baseURL);
          // 本地通道最多每五秒取一次新凭证；短暂等待实际回执，不把同步受理当成在线。
          for (let attempt = 0; attempt < 6; attempt++) {
            if (!currentCheck()) return;
            connected = await readAgentConnection();
            if (connected || attempt === 5) break;
            await new Promise<void>((resolve) => window.setTimeout(resolve, 1000));
          }
        }
      }
      if (currentCheck()) setAgent(connected ? "online" : "offline");
    } catch (error) {
      if (!currentCheck()) return;
      if (error instanceof APIRequestError && error.status === 401) {
        window.localStorage.removeItem(TOKEN_KEY);
        redirectLogin(window.location.search);
        return;
      }
      setAgent("error");
      setAgentError(error instanceof Error ? error.message : "暂时没查到连接状态，请重新检查。");
    } finally {
      checkingRef.current = false;
      // 安装时另开控制台登录可能更换令牌；旧检查退出后必须让用户能用新账号重查。
      if (activeRef.current && token !== window.localStorage.getItem(TOKEN_KEY)) {
        setAgent("error");
        setAgentError("登录状态已经更新，点“安装后重新检查”，我再确认一次连接。");
      }
    }
  }, [redirectLogin]);

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
      setErrorText("授权链接不完整：请回到 WorkBuddy 重新发起连接。");
      setPhase("error");
      return;
    }
    const token = window.localStorage.getItem(TOKEN_KEY) || "";
    if (!token) {
      redirectLogin(search);
      return;
    }
    setParams(nextParams);
    setPhase("ready");
  }, [redirectLogin]);

  // 安装包和控制台在新标签打开，授权页保留原始回调参数，回来后可以重新检查。
  useEffect(() => {
    if (phase !== "ready" || !params) return;
    void checkAgent();
  }, [phase, params, checkAgent]);

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
              WorkBuddy 请求连接你的 GoodHR
            </Title>
            <Paragraph
              className="public-text public-login-19"
              style={{ margin: "8px 0 20px", color: "var(--goodhr-text-secondary, #637069)" }}
            >
              {phase === "loading" && "我先看看授权链接……"}
              {phase === "error" && "这次授权没能继续"}
              {phase === "ready" && "授权之后，你可以在 WorkBuddy 里直接聊着管理招聘任务。"}
            </Paragraph>

            {phase === "loading" && (
              <div style={{ textAlign: "center", padding: "32px 0" }}>
                <Spin />
              </div>
            )}

            {phase === "error" && (
              <Alert type="warning" showIcon title={errorText} />
            )}

            {phase === "ready" && params && (
              <>
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

                {agent === "checking" && (
                  <Paragraph style={{ margin: "0 0 12px", color: "var(--goodhr-text-secondary, #637069)", fontSize: 12 }}>
                    我在看看你的本地程序连上了没有……
                  </Paragraph>
                )}
                {agent === "online" && (
                  <Alert
                    type="success"
                    showIcon
                    title="本地程序已连接 WorkBuddy 任务通道"
                    description="首次使用还要在控制台完成组件安装、创建岗位，并在招聘浏览器里登录和设置筛选条件。准备好后再回 WorkBuddy 启动任务。"
                    style={{ marginBottom: 12 }}
                  />
                )}
                {(agent === "offline" || agent === "error") && (
                  <Alert
                    type={agent === "error" ? "warning" : "info"}
                    showIcon
                    title={agent === "error" ? "连接状态暂时没查到" : "本地程序还没连接"}
                    description={agent === "error" ? agentError : "只有 WorkBuddy 也可以先授权。请下载安装 GoodHR，启动后在控制台登录同一账号，完成组件安装、创建岗位和招聘平台登录。"}
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
                    disabled={submitting === "deny"}
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
            授权只把筛选任务的启动和结果查询交给 WorkBuddy，招聘平台的登录状态和数据仍然只保存在你的电脑里。
          </Paragraph>
        </div>
      </div>
    </div>
  );
}
