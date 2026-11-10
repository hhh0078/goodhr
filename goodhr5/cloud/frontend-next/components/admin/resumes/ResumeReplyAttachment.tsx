/** 本文件在获授权的简历会话中打开或下载真实附件，临时预览地址仅保留在当前浏览器。 */
"use client";

import { Button, Flex, Typography } from "antd";
import { DownloadOutlined, FilePdfOutlined, PaperClipOutlined } from "@ant-design/icons";
import { useEffect, useRef, useState } from "react";
import { fetchCandidateReplyAttachment } from "@/lib/admin-api";

export type ReplyAttachment = {
  id: string;
  file_name: string;
  size_bytes?: number;
  source_message_id: string;
};

type Action = "open" | "download";

/** ResumeReplyAttachment 仅展示服务端已核实对应消息的文件，读取失败时保留原操作入口。 */
export default function ResumeReplyAttachment({ attachment, candidateID, threadID }: {
  attachment: ReplyAttachment;
  candidateID: string;
  threadID: string;
}) {
  const pending = useRef<AbortController | null>(null);
  const [busy, setBusy] = useState<Action | "">("");
  const [error, setError] = useState("");
  const filename = attachment.file_name?.split(/[\\/]/).pop()?.replace(/[\u0000-\u001f\u007f]/g,"").trim() || "简历附件";
  const isPDF = /\.pdf$/i.test(filename);

  useEffect(() => () => {
    pending.current?.abort();
    pending.current = null;
  }, []);

  /** read 根据当前会话重新鉴权，只把服务端识别为 PDF 的内容交给浏览器预览。 */
  async function read(action: Action) {
    if (pending.current) return;
    const preview = action === "open" ? window.open("about:blank", "_blank") : null;
    if (action === "open" && !preview) {
      setError("浏览器拦住了预览，可点下载或允许弹窗后重试。");
      return;
    }
    if (preview) {
      preview.opener = null;
      preview.document.title = "正在打开附件";
      preview.document.body.textContent = "正在读取简历附件…";
    }
    const controller = new AbortController();
    pending.current = controller;
    setBusy(action);
    setError("");
    const timeout = window.setTimeout(() => controller.abort(), 45000);
    try {
      const blob = await fetchCandidateReplyAttachment(candidateID, threadID, attachment.id, action === "open", controller.signal);
      if (!blob.size || (attachment.size_bytes && blob.size !== attachment.size_bytes)) {
        throw new Error("附件未读取完整，请重试。");
      }
      if (preview && (blob.type.split(";")[0].trim().toLowerCase() !== "application/pdf" || (await blob.slice(0, 5).text()) !== "%PDF-")) {
        throw new Error("这份文件暂时不能预览，可以下载原件。");
      }
      controller.signal.throwIfAborted();
      if (preview?.closed) return;
      const url = URL.createObjectURL(blob);
      // 浏览器下载和 PDF 阅读器在页面之外载入文件，接收文件后再释放临时地址。
      window.setTimeout(() => URL.revokeObjectURL(url), 60000);
      if (preview) {
        preview.location.replace(url);
      } else {
        const link = document.createElement("a");
        link.href = url;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        link.remove();
      }
    } catch (cause) {
      if (preview && !preview.closed) preview.close();
      if (pending.current === controller) {
        setError(controller.signal.aborted ? "读取附件超时，可以重试。" : cause instanceof Error ? cause.message : "附件没读出来，可以重试。");
      }
    } finally {
      window.clearTimeout(timeout);
      if (pending.current === controller) {
        pending.current = null;
        setBusy("");
      }
    }
  }

  return <Flex vertical align="start" gap={4} style={{maxWidth:"100%"}}>
    <Typography.Text style={{overflowWrap:"anywhere"}}><PaperClipOutlined aria-hidden="true" /> {filename}</Typography.Text>
    <Flex gap={8} wrap>
      {isPDF && <Button icon={<FilePdfOutlined />} loading={busy === "open"} disabled={Boolean(busy)} onClick={() => void read("open")}>打开附件</Button>}
      <Button icon={<DownloadOutlined />} loading={busy === "download"} disabled={Boolean(busy)} onClick={() => void read("download")}>下载附件</Button>
    </Flex>
    {error && <Typography.Text type="danger" role="alert">{error}</Typography.Text>}
  </Flex>;
}
