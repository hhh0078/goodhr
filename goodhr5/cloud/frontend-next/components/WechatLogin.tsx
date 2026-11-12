/** 本文件展示公众号免密登录二维码和一次性验证码，并串行等待当前页面的微信确认结果。 */
"use client";

import { Alert, Button, Flex, Image, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { apiRequest, INVITE_CACHE_KEY } from "@/lib/api";
import styles from "./WechatLogin.module.css";

type Challenge = { ticket: string; login_code: string; qr_image_url: string; expires_at: string };
type Props = { agreementAccepted: boolean; onAgreement: () => void; onSuccess: (data: Record<string, unknown>) => void };

const CHALLENGE_CACHE_KEY = "goodhr.wechat.login.challenge";
let pendingCreation: Promise<Challenge> | null = null;

/** validChallenge 接受四位新码及尚未过期的八位旧码，公开短码不能代替页面独占凭据。 */
function validChallenge(value: Challenge | null): value is Challenge {
  return Boolean(value && /^[a-f0-9]{64}$/.test(value.ticket) && /^(?:\d{4}|\d{8})$/.test(value.login_code)
    && typeof value.qr_image_url === "string" && Number.isFinite(Date.parse(value.expires_at)));
}

/** cachedChallenge 只恢复当前标签页仍有效的登录码；浏览器禁用缓存时仍允许正常登录。 */
function cachedChallenge(): Challenge | null {
  try {
    const value = JSON.parse(sessionStorage.getItem(CHALLENGE_CACHE_KEY) || "null");
    if (validChallenge(value) && Date.parse(value.expires_at) > Date.now()) return value;
    sessionStorage.removeItem(CHALLENGE_CACHE_KEY);
  } catch { /* 缓存不可用或损坏时重新申请，不影响邮箱入口。 */ }
  return null;
}

/** forgetChallenge 仅清理本轮凭据，迟到的旧轮询不能删除刚生成的新码。 */
function forgetChallenge(ticket: string) {
  try { if (cachedChallenge()?.ticket === ticket) sessionStorage.removeItem(CHALLENGE_CACHE_KEY); } catch { /* 缓存不可用时无需清理。 */ }
}

/** createChallenge 合并在途生成请求，切换页面时仍保存响应，避免丢码后重复生成触发限流。 */
function createChallenge(previousTicket: string): Promise<Challenge> {
  if (pendingCreation) return pendingCreation;
  const controller = new AbortController();
  const timeout = window.setTimeout(() => controller.abort(), 10000);
  pendingCreation = apiRequest("/api/auth/wechat/create", { method: "POST", body: JSON.stringify({ previous_ticket: previousTicket }), signal: controller.signal })
    .then((data: Challenge) => {
      if (!validChallenge(data) || Date.parse(data.expires_at) <= Date.now()) throw new Error("登录码没读完整，请重新生成");
      try { sessionStorage.setItem(CHALLENGE_CACHE_KEY, JSON.stringify(data)); } catch { /* 无缓存也可使用当前页面登录码。 */ }
      return data;
    }).finally(() => { window.clearTimeout(timeout); pendingCreation = null; });
  return pendingCreation;
}

/** WechatLogin 恢复当前标签页的有效码；换码失败保留原码，收到正式登录态才跳转。 */
export default function WechatLogin({ agreementAccepted, onAgreement, onSuccess }: Props) {
  const [challenge, setChallenge] = useState<Challenge | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [pollError, setPollError] = useState("");
  const [status, setStatus] = useState("waiting");
  const [remaining, setRemaining] = useState(0);
  const [generation, setGeneration] = useState(0);
  const agreement = useRef(agreementAccepted);
  const success = useRef(onSuccess);
  const previousTicket = useRef("");
  agreement.current = agreementAccepted;
  success.current = onSuccess;

  useEffect(() => {
    let active = true;
    const stored = cachedChallenge();
    if (generation === 0 && stored) {
      previousTicket.current = stored.ticket;
      setChallenge(stored); setRemaining(Math.max(0, Math.ceil((Date.parse(stored.expires_at) - Date.now()) / 1000))); setLoading(false);
      return;
    }
    setLoading(true); setError("");
    createChallenge(previousTicket.current || stored?.ticket || "")
      .then((data) => {
        if (!active) return;
        previousTicket.current = data.ticket;
        setStatus("waiting"); setPollError("");
        setRemaining(Math.max(0, Math.ceil((Date.parse(data.expires_at) - Date.now()) / 1000)));
        setChallenge(data);
      })
      .catch((cause: unknown) => { if (active) setError(cause instanceof Error ? cause.message : "登录码没准备好，可以重试或先用邮箱登录"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [generation]);

  useEffect(() => {
    if (!challenge) return;
    /** updateRemaining 以服务端截止时间计算剩余秒数，后台标签页恢复后不沿用过期倒计时。 */
    const updateRemaining = () => setRemaining(Math.max(0, Math.ceil((Date.parse(challenge.expires_at) - Date.now()) / 1000)));
    updateRemaining();
    const timer = window.setInterval(updateRemaining, 1000);
    return () => window.clearInterval(timer);
  }, [challenge]);

  useEffect(() => {
    if (!challenge) return;
    let controller: AbortController | undefined;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    /** poll 等待本次请求完成后再安排下一次，防止慢请求叠加或离开页面后恢复旧登录。 */
    async function poll() {
      if (!active) return;
      if (Date.now() >= Date.parse(challenge!.expires_at)) { setStatus("expired"); return; }
      controller = new AbortController();
      const requestTimeout = window.setTimeout(() => controller?.abort(), 10000);
      try {
        const result = await apiRequest("/api/auth/wechat/poll", {
          method: "POST",
          body: JSON.stringify({ ticket: challenge!.ticket, agreement_accepted: agreement.current, inviter_id: localStorage.getItem(INVITE_CACHE_KEY) || "" }),
          signal: controller.signal,
        });
        if (!active) return;
        failures = 0; setPollError("");
        if (result.access_token) { forgetChallenge(challenge!.ticket); active = false; success.current(result); return; }
        setStatus(String(result.status || "waiting"));
        if (result.status === "expired") { forgetChallenge(challenge!.ticket); return; }
      } catch (cause) {
        if (!active) return;
        failures++;
        setPollError(cause instanceof Error ? cause.message : "连接有点慢，我再等等。也可以重新生成登录码。");
      } finally {
        window.clearTimeout(requestTimeout);
      }
      if (active) timer = setTimeout(() => void poll(), Math.min(10000, 1500 * Math.max(1, failures)));
    }
    void poll();
    return () => { active = false; controller?.abort(); clearTimeout(timer); };
  }, [challenge]);

  return (
    <Flex vertical align="center" gap={16} className={styles.login}>
      <Typography.Text strong style={{ fontSize: 18 }}>微信扫码登录</Typography.Text>
      {!challenge && loading ? <div className={styles.qr}><Spin description="正在准备登录码" /></div> : challenge ? (
        <div className={styles.qr}>
          <Image width={208} height={208} src={challenge.qr_image_url} alt="GoodHR 公众号二维码，请用微信扫码" preview={false} />
          {status === "expired" || remaining === 0 ? <div className={styles.expired}><Typography.Text strong>登录码过期了</Typography.Text></div> : null}
        </div>
      ) : null}
      {challenge ? <Flex vertical align="center" gap={8}>
        <Typography.Text>扫码进入公众号，把下面的数字发给我</Typography.Text>
        <Typography.Text className={styles.code} style={{ fontSize: "clamp(20px, 5vw, 24px)", fontWeight: 700 }} aria-label="微信登录码">{challenge.login_code}</Typography.Text>
      </Flex> : null}
      {challenge ? <div className={styles.actions}>
        <Typography.Text type="secondary" aria-label="登录码倒计时">{status === "expired" || remaining === 0 ? "已过期" : `${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, "0")} 后过期`}</Typography.Text>
        <Button type="link" aria-label="重新生成" loading={loading} onClick={() => setGeneration(value => value + 1)}>重新生成</Button>
      </div> : null}
      <div role="status" aria-live="polite" className={styles.status}>
        {status === "agreement_required" ? <Button type="link" onClick={onAgreement}>微信已确认，读完并同意协议就能进入</Button> : challenge && status !== "expired" && remaining > 0 ? <><Spin size="small" /> 正在等你发验证码</> : null}
      </div>
      {error || pollError ? <Alert type="warning" showIcon title={error || pollError} action={<Button size="small" loading={loading} onClick={() => setGeneration(value => value + 1)}>重试</Button>} /> : null}
      <Typography.Paragraph type="secondary" className={styles.hint}>首次使用会自动注册，无需邮箱和密码。已绑定公众号的邮箱账号会继续使用原账号；还没绑定的，请先用邮箱登录。</Typography.Paragraph>
    </Flex>
  );
}
