/** 本文件展示公众号免密登录二维码和一次性验证码，并串行等待当前页面的微信确认结果。 */
"use client";

import { Alert, Button, Checkbox, Flex, Image, Spin, Typography } from "antd";
import { useEffect, useRef, useState } from "react";
import { apiRequest, INVITE_CACHE_KEY } from "@/lib/api";
import styles from "./WechatLogin.module.css";

type Challenge = { ticket: string; login_code: string; qr_image_url: string; expires_at: string };
type Props = { agreementAccepted: boolean; onAgreementChange: (checked: boolean) => void; onAgreement: () => void; onSuccess: (data: Record<string, unknown>) => void };

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

/** ConsentQRPlaceholder 仅绘制不可用于登录的装饰二维码，不读取真实二维码或发起网络请求。 */
function ConsentQRPlaceholder() {
  return <svg aria-hidden="true" focusable="false" viewBox="0 0 25 25" className={styles.placeholder}><path d="M2 2h1v1h-1zM2 3h1v1h-1zM2 4h1v1h-1zM2 5h1v1h-1zM2 6h1v1h-1zM2 7h1v1h-1zM2 8h1v1h-1zM2 10h1v1h-1zM2 12h1v1h-1zM2 13h1v1h-1zM2 16h1v1h-1zM2 17h1v1h-1zM2 18h1v1h-1zM2 19h1v1h-1zM2 20h1v1h-1zM2 21h1v1h-1zM2 22h1v1h-1zM3 2h1v1h-1zM3 8h1v1h-1zM3 10h1v1h-1zM3 13h1v1h-1zM3 16h1v1h-1zM3 22h1v1h-1zM4 2h1v1h-1zM4 4h1v1h-1zM4 5h1v1h-1zM4 6h1v1h-1zM4 8h1v1h-1zM4 13h1v1h-1zM4 14h1v1h-1zM4 16h1v1h-1zM4 18h1v1h-1zM4 19h1v1h-1zM4 20h1v1h-1zM4 22h1v1h-1zM5 2h1v1h-1zM5 4h1v1h-1zM5 5h1v1h-1zM5 6h1v1h-1zM5 8h1v1h-1zM5 16h1v1h-1zM5 18h1v1h-1zM5 19h1v1h-1zM5 20h1v1h-1zM5 22h1v1h-1zM6 2h1v1h-1zM6 4h1v1h-1zM6 5h1v1h-1zM6 6h1v1h-1zM6 8h1v1h-1zM6 16h1v1h-1zM6 18h1v1h-1zM6 19h1v1h-1zM6 20h1v1h-1zM6 22h1v1h-1zM7 2h1v1h-1zM7 8h1v1h-1zM7 14h1v1h-1zM7 16h1v1h-1zM7 22h1v1h-1zM8 2h1v1h-1zM8 3h1v1h-1zM8 4h1v1h-1zM8 5h1v1h-1zM8 6h1v1h-1zM8 7h1v1h-1zM8 8h1v1h-1zM8 13h1v1h-1zM8 14h1v1h-1zM8 16h1v1h-1zM8 17h1v1h-1zM8 18h1v1h-1zM8 19h1v1h-1zM8 20h1v1h-1zM8 21h1v1h-1zM8 22h1v1h-1zM9 12h1v1h-1zM9 13h1v1h-1zM10 4h1v1h-1zM10 7h1v1h-1zM10 9h1v1h-1zM10 10h1v1h-1zM10 12h1v1h-1zM10 15h1v1h-1zM10 18h1v1h-1zM10 20h1v1h-1zM10 21h1v1h-1zM11 3h1v1h-1zM11 5h1v1h-1zM11 8h1v1h-1zM11 10h1v1h-1zM11 12h1v1h-1zM11 14h1v1h-1zM11 16h1v1h-1zM11 19h1v1h-1zM11 21h1v1h-1zM12 3h1v1h-1zM12 6h1v1h-1zM12 8h1v1h-1zM12 10h1v1h-1zM12 12h1v1h-1zM12 14h1v1h-1zM12 17h1v1h-1zM12 19h1v1h-1zM12 21h1v1h-1zM13 2h1v1h-1zM13 4h1v1h-1zM13 7h1v1h-1zM13 10h1v1h-1zM13 12h1v1h-1zM13 13h1v1h-1zM13 15h1v1h-1zM13 18h1v1h-1zM13 21h1v1h-1zM14 2h1v1h-1zM14 5h1v1h-1zM14 6h1v1h-1zM14 9h1v1h-1zM14 10h1v1h-1zM14 13h1v1h-1zM14 16h1v1h-1zM14 17h1v1h-1zM14 20h1v1h-1zM14 21h1v1h-1zM15 13h1v1h-1zM15 14h1v1h-1zM15 15h1v1h-1zM15 19h1v1h-1zM15 20h1v1h-1zM16 2h1v1h-1zM16 3h1v1h-1zM16 4h1v1h-1zM16 5h1v1h-1zM16 6h1v1h-1zM16 7h1v1h-1zM16 8h1v1h-1zM16 15h1v1h-1zM16 16h1v1h-1zM16 17h1v1h-1zM16 18h1v1h-1zM16 19h1v1h-1zM17 2h1v1h-1zM17 8h1v1h-1zM18 2h1v1h-1zM18 4h1v1h-1zM18 5h1v1h-1zM18 6h1v1h-1zM18 8h1v1h-1zM18 14h1v1h-1zM18 15h1v1h-1zM18 16h1v1h-1zM18 17h1v1h-1zM18 18h1v1h-1zM19 2h1v1h-1zM19 4h1v1h-1zM19 5h1v1h-1zM19 6h1v1h-1zM19 8h1v1h-1zM19 13h1v1h-1zM19 14h1v1h-1zM19 18h1v1h-1zM19 19h1v1h-1zM19 20h1v1h-1zM20 2h1v1h-1zM20 4h1v1h-1zM20 5h1v1h-1zM20 6h1v1h-1zM20 8h1v1h-1zM20 12h1v1h-1zM20 13h1v1h-1zM20 16h1v1h-1zM20 17h1v1h-1zM20 20h1v1h-1zM21 2h1v1h-1zM21 8h1v1h-1zM21 10h1v1h-1zM21 12h1v1h-1zM21 15h1v1h-1zM21 18h1v1h-1zM21 20h1v1h-1zM21 21h1v1h-1zM22 2h1v1h-1zM22 3h1v1h-1zM22 4h1v1h-1zM22 5h1v1h-1zM22 6h1v1h-1zM22 7h1v1h-1zM22 8h1v1h-1zM22 10h1v1h-1zM22 12h1v1h-1zM22 14h1v1h-1zM22 16h1v1h-1zM22 19h1v1h-1zM22 21h1v1h-1z" /></svg>;
}

/** WechatLogin 恢复当前标签页的有效码；换码失败保留原码，收到正式登录态才跳转。 */
export default function WechatLogin({ agreementAccepted, onAgreementChange, onAgreement, onSuccess }: Props) {
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
  const forceRefresh = useRef(false);
  agreement.current = agreementAccepted;
  success.current = onSuccess;

  useEffect(() => {
    let active = true;
    if (!agreementAccepted) { setLoading(false); return; }
    const stored = cachedChallenge();
    const refresh = forceRefresh.current;
    forceRefresh.current = false;
    if (!refresh && stored) {
      previousTicket.current = stored.ticket;
      setStatus("waiting"); setError(""); setPollError("");
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
  }, [generation, agreementAccepted]);

  useEffect(() => {
    if (!challenge) return;
    /** updateRemaining 以服务端截止时间计算剩余秒数，后台标签页恢复后不沿用过期倒计时。 */
    const updateRemaining = () => setRemaining(Math.max(0, Math.ceil((Date.parse(challenge.expires_at) - Date.now()) / 1000)));
    updateRemaining();
    const timer = window.setInterval(updateRemaining, 1000);
    return () => window.clearInterval(timer);
  }, [challenge]);

  useEffect(() => {
    if (!challenge || !agreementAccepted) return;
    let controller: AbortController | undefined;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    /** poll 等待本次请求完成后再安排下一次，防止慢请求叠加或离开页面后恢复旧登录。 */
    async function poll() {
      if (!active || !agreement.current) return;
      if (Date.now() >= Date.parse(challenge!.expires_at)) { setStatus("expired"); return; }
      controller = new AbortController();
      const requestTimeout = window.setTimeout(() => controller?.abort(), 10000);
      try {
        const result = await apiRequest("/api/auth/wechat/poll", {
          method: "POST",
          body: JSON.stringify({ ticket: challenge!.ticket, agreement_accepted: agreement.current, inviter_id: localStorage.getItem(INVITE_CACHE_KEY) || "" }),
          signal: controller.signal,
        });
        if (!active || !agreement.current) return;
        failures = 0; setPollError("");
        if (result.access_token) { forgetChallenge(challenge!.ticket); active = false; success.current(result); return; }
        setStatus(String(result.status || "waiting"));
        if (result.status === "expired") { forgetChallenge(challenge!.ticket); return; }
      } catch (cause) {
        if (!active || !agreement.current) return;
        failures++;
        setPollError(cause instanceof Error ? cause.message : "连接有点慢，我再等等。也可以重新生成登录码。");
      } finally {
        window.clearTimeout(requestTimeout);
      }
      if (active) timer = setTimeout(() => void poll(), Math.min(10000, 1500 * Math.max(1, failures)));
    }
    void poll();
    return () => { active = false; controller?.abort(); clearTimeout(timer); };
  }, [challenge, agreementAccepted]);

  /** regenerate 仅在本人同意后主动换码，普通恢复与重新勾选继续复用有效码。 */
  function regenerate() {
    if (!agreementAccepted || loading) return;
    forceRefresh.current = true;
    setGeneration(value => value + 1);
  }

  return (
    <Flex vertical align="center" gap={10} className={styles.login}>
      <Typography.Text strong className={styles.heading}>微信扫码，发送下方数字登录</Typography.Text>
      <div className={styles.qr}>
        {agreementAccepted && challenge ? <Image width={160} height={160} src={challenge.qr_image_url} alt="GoodHR 公众号二维码，请用微信扫码" preview={false} /> : null}
        {!agreementAccepted ? <>
          <ConsentQRPlaceholder />
          <div className={`${styles.cover} ${styles.consent}`} aria-label="请先同意协议">
            <Button type="primary" className={styles.consentButton} style={{ borderRadius: 6 }} onClick={() => onAgreementChange(true)}>同意协议</Button>
            <Button type="link" aria-label="查看二维码蒙板协议" onClick={onAgreement}>使用说明与隐私协议</Button>
          </div>
        </>
          : loading && !challenge ? <Spin aria-label="正在准备登录码" />
          : challenge && (status === "expired" || remaining === 0) ? <div className={styles.cover}><Typography.Text strong>登录码过期了</Typography.Text></div> : null}
      </div>
      {agreementAccepted && challenge ? <div className={styles.codeRow}>
        <Typography.Text className={styles.code} style={{ fontSize: 24, fontWeight: 700 }} aria-label="微信登录码">{challenge.login_code}</Typography.Text>
        <span className={styles.countdown}>
          {status !== "expired" && remaining > 0 ? <Spin size="small" aria-label="正在等待微信确认" /> : null}
          <Typography.Text type="secondary" aria-label="登录码倒计时">{status === "expired" || remaining === 0 ? "已过期" : `${Math.floor(remaining / 60)}:${String(remaining % 60).padStart(2, "0")} 后过期`}</Typography.Text>
        </span>
      </div> : null}
      <div className={styles.actions}>
        <span className={styles.agreement}>
          <Checkbox aria-label="我已阅读并同意" checked={agreementAccepted} onChange={event => { agreement.current = event.target.checked; onAgreementChange(event.target.checked); }}>同意</Checkbox>
          <Button type="link" aria-label="查看使用协议与隐私说明" onClick={onAgreement}>使用说明与隐私协议</Button>
        </span>
        <Button type="link" aria-label="重新生成" disabled={!agreementAccepted} loading={loading} onClick={regenerate}>重新生成</Button>
      </div>
      {agreementAccepted && (error || pollError) ? <Alert type="warning" showIcon title={error || pollError} action={<Button size="small" loading={loading} onClick={regenerate}>重试</Button>} /> : null}
      <Typography.Paragraph type="secondary" className={styles.hint} style={{ fontSize: 12 }}>首次使用会自动注册，无需邮箱和密码。已绑定公众号的邮箱账号会继续使用原账号；还没绑定的，请先用邮箱登录。</Typography.Paragraph>
    </Flex>
  );
}
