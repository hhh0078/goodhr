/** 本文件负责新版后台帮助指南和 AI 帮助助手。 */
"use client";

import SendRoundedIcon from "@mui/icons-material/SendRounded";
import { Box, Button, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { CLOUD_API_BASE, cloudRequest, getToken } from "@/lib/admin-api";
import { PageHeader, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

/** HelpPage 展示系统指南并提供流式 AI 问答。 */
export default function HelpPage() {
  const { notify } = useAdmin();
  const [guide, setGuide] = useState<any>({});
  const [messages, setMessages] = useState<any[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => { cloudRequest("/api/help/guide", { auth: false }).then((data) => setGuide(data.guide || {})).catch(() => undefined); }, []);

  /** send 向帮助助手提交问题并流式追加回答。 */
  async function send() {
    const question = input.trim(); if (!question || loading) return;
    const nextMessages = [...messages, { role: "user", content: question }]; setMessages([...nextMessages, { role: "assistant", content: "" }]); setInput(""); setLoading(true);
    try {
      const response = await fetch(`${CLOUD_API_BASE}/api/help/chat`, { method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${getToken()}` }, body: JSON.stringify({ messages: nextMessages }) });
      if (!response.ok || !response.body) throw new Error("帮助助手请求失败");
      const reader = response.body.getReader(); const decoder = new TextDecoder(); let answer = "";
      while (true) { const { value, done } = await reader.read(); if (done) break; answer += decoder.decode(value, { stream: true }); setMessages([...nextMessages, { role: "assistant", content: answer }]); }
    } catch (error) { notify(error instanceof Error ? error.message : "帮助助手请求失败", "error"); }
    finally { setLoading(false); }
  }

  return <><PageHeader title="常见问题" description="查看系统使用说明，或直接向帮助助手描述遇到的问题。" /><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", lg: "minmax(0, .8fr) minmax(0, 1.2fr)" }, gap: 2 }}><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>系统指南</Typography><Box component="pre" sx={{ mt: 2, whiteSpace: "pre-wrap", fontFamily: "inherit", lineHeight: 1.8, color: "text.secondary" }}>{guide.content || guide.text || JSON.stringify(guide, null, 2) || "帮助指南正在准备中。"}</Box></SectionPanel><SectionPanel><Typography component="h2" sx={{ fontSize: 19, fontWeight: 760 }}>AI 帮助助手</Typography><Stack spacing={1.5} sx={{ mt: 2, minHeight: 340, maxHeight: 520, overflowY: "auto" }}>{messages.length ? messages.map((item, index) => <Box key={`${item.role}-${index}`} sx={{ alignSelf: item.role === "user" ? "flex-end" : "flex-start", maxWidth: "88%", p: 1.5, bgcolor: item.role === "user" ? "#e7f5ed" : "#f4f7f5", borderRadius: "8px" }}><Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.7 }}>{item.content || "正在思考..."}</Typography></Box>) : <Typography color="text.secondary">例如：为什么本地程序显示未连接？</Typography>}</Stack><Stack direction="row" spacing={1} sx={{ mt: 2 }}><TextField value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void send(); } }} placeholder="描述你遇到的问题" fullWidth multiline maxRows={4} /><Button variant="contained" disabled={loading || !input.trim()} onClick={() => void send()}><SendRoundedIcon /></Button></Stack></SectionPanel></Box></>;
}
