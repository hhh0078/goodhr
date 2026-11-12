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
import { TOKEN_KEY, cloudAPIBase } from "@/lib/api";
import { detectLocalAgent } from "@/lib/admin-api";

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
type AgentState = "checking" | "online" | "offline";

const grantedItems = [
  "读取你的岗位清单、订阅状态和筛选结果",
  "在你确认后启动岗位筛选任务",
  "只在你同意授权的期间生效，随时可以在控制台解除",
];

/** AuthorizePage 展示授权确认卡片和本地程序引导。 */
export default function AuthorizePage() {
  const [phase, setPhase] = useState<"loading" | "error" | "ready">("loading");
  const [errorText, setErrorText] = useState("");
  const [params, setParams] = useState<AuthorizeParams | null>(null);
  const [agent, setAgent] = useState<AgentState>("checking");
  const [submitting, setSubmitting] = useState<"approve" | "deny" | null>(null);
  const startedRef = useRef(false);

  /** cloudPost 调用云端带登录态的接口。 */
  const cloudPost = useCallback(async (path: string, body: unknown) => {
    const token = window.localStorage.getItem(TOKEN_KEY) || "";
    const response = await fetch(`${cloudAPIBase()}${path}`, {
      method: "POST",
      cache: "no-store",
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${token}`,
      },
      body: JSON.stringify(body),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || data.ok === false) {
      throw new Error(String(data.error || "云端没有处理成功，请稍后再试"));
    }
    return data;
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
        const data = await cloudPost("/api/workbuddy/authorize", {
          ...params,
          decision,
        });
        const redirect = String(data.redirect_url || "");
        if (!redirect) throw new Error("回调地址没有返回，请回到 WorkBuddy 重新连接");
        window.location.assign(redirect);
      } catch (error) {
        setErrorText(error instanceof Error ? error.message : "授权没有完成，请重试");
        setSubmitting(null);
      }
    },
    [cloudPost, params],
  );

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

  // 登录后检查本地程序：先探测本机端口并同步云端令牌，再查云端绑定状态。
  useEffect(() => {
    if (phase !== "ready" || !params) return;
    let active = true;
    (async () => {
      try {
        const baseURL = await detectLocalAgent();
        if (baseURL) {
          const token = window.localStorage.getItem(TOKEN_KEY) || "";
          // 把云端令牌同步给本地程序，供它连云端命令通道；失败不阻塞授权。
          void fetch(`${baseURL}/api/v1/cloud/session`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ token }),
          }).catch(() => {});
        }
      } catch {
        // 本机探测失败不阻塞，交给云端状态判断。
      }
      const token = window.localStorage.getItem(TOKEN_KEY) || "";
      try {
        const response = await fetch(`${cloudAPIBase()}/api/agents/current`, {
          cache: "no-store",
          headers: { Authorization: `Bearer ${token}` },
        });
        const data = await response.json().catch(() => ({}));
        if (active) setAgent(data?.ok && data?.data?.machine_id ? "online" : "offline");
      } catch {
        if (active) setAgent("offline");
      }
    })();
    return () => {
      active = false;
    };
  }, [phase, params]);

  return (
    <div className="public-login-1">
      <div className="public-container public-login-2">
        <div className="public-login-3">
          <BrandMark />
        </div>
      </div>
      <div className="public-container public-login-7">
        <div style={{ maxWidth: 520, margin: "0 auto" }}>
          <div className="public-panel public-login-17" style={{ padding: 28 }}>
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
              <Alert type="warning" showIcon message={errorText} />
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
                    message="本地程序已连接，任务可以直接下发到你电脑上运行。"
                    style={{ marginBottom: 12 }}
                  />
                )}
                {agent === "offline" && (
                  <Alert
                    type="info"
                    showIcon
                    message="本地程序还没连接"
                    description="不用现在处理，授权完成后第一次启动任务前装好就行。没安装的话，先去下载页装一个。"
                    action={
                      <Button
                        size="small"
                        href="/download"
                        icon={<DownloadOutlined aria-hidden="true" />}
                      >
                        去下载
                      </Button>
                    }
                    style={{ marginBottom: 12 }}
                  />
                )}

                {errorText && (
                  <Alert type="error" showIcon message={errorText} style={{ marginBottom: 12 }} />
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
