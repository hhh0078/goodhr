/** 文件作用说明：免登录读取公开简历资料，提供当前链接二维码和手机适配的只读页面。 */
"use client";
import { Alert, Button, Flex, QRCode, Skeleton, Typography } from "antd";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { cloudRequest } from "@/lib/admin-api";
import type { Resume } from "@/lib/resume-types";
import ResumeProfile from "@/components/admin/resumes/ResumeProfile";
import styles from "./share.module.css";

/** ResumeShareView 只调用公开接口，不发送登录凭据，也不读取备注、推荐或自动任务。 */
export default function ResumeShareView() {
  const params = useParams<{ token: string }>();
  const token = String(params.token || "");
  const [candidate, setCandidate] = useState<Resume | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [shareURL, setShareURL] = useState("");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let active = true;
    setCandidate(null); setError(""); setLoading(true);
    setShareURL(new URL(`/resumes/share/${encodeURIComponent(token)}`, window.location.origin).href);
    if (!/^[a-f0-9]{64}$/.test(token)) { setError("这个链接没有复制完整，请向分享人重新要一份。"); setLoading(false); return; }
    cloudRequest(`/api/public/resume-shares/${encodeURIComponent(token)}`, { auth: false })
      .then((data) => { if (active) setCandidate(data.candidate || null); })
      .catch((reason) => { if (active) setError(reason instanceof Error ? reason.message : "这份简历暂时没读出来，请稍后再试。"); })
      .finally(() => { if (active) setLoading(false); });
    return () => { active = false; };
  }, [token, attempt]);

  return <main className={styles.page}>
    <Flex justify="space-between" align="center" gap={12} wrap className={styles.heading}><Typography.Title level={4} style={{ margin: 0 }}>GoodHR · 简历分享</Typography.Title><Typography.Text type="secondary">无需登录，直接查看</Typography.Text></Flex>
    {loading ? <section className={styles.profile}><Skeleton active paragraph={{ rows: 8 }} /></section> : error || !candidate ? <Alert type="warning" showIcon title={error || "这份分享已取消，或简历已删除。"} action={<Button onClick={() => setAttempt((current) => current + 1)}>重新读取</Button>} /> : <div className={styles.content}>
      <section className={styles.profile} aria-label="公开简历资料"><ResumeProfile candidate={candidate} /></section>
      <aside className={styles.qr} aria-label="简历二维码"><Typography.Title level={5} style={{ margin: 0 }}>手机也能看</Typography.Title>{shareURL ? <QRCode value={shareURL} size={180} color="#183d31" /> : null}<Typography.Text type="secondary">扫一扫，打开同一份简历</Typography.Text></aside>
    </div>}
  </main>;
}
