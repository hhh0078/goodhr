/** 本文件负责新版后台帮助指南卡片、用户对话缓存和流式 AI 帮助助手。 */
"use client";

import DeleteSweepRoundedIcon from "@mui/icons-material/DeleteSweepRounded";
import SendRoundedIcon from "@mui/icons-material/SendRounded";
import { Box, Button, Stack, TextField, Typography } from "@mui/material";
import { useEffect, useMemo, useRef, useState } from "react";
import { CLOUD_API_BASE, cloudRequest, getToken } from "@/lib/admin-api";
import { PageHeader, RefreshButton, SectionPanel } from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";

type GuideCard = { id?: string; title?: string; summary?: string; content?: string };
type ChatMessage = { role: "user" | "assistant"; content: string };

/** HelpPage 展示系统指南并提供带本地缓存的流式 AI 问答。 */
export default function HelpPage() {
  const { user, notify } = useAdmin();
  const [guide, setGuide] = useState<any>({});
  const [activeIndex, setActiveIndex] = useState(0);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [guideLoading, setGuideLoading] = useState(false);
  const chatBody = useRef<HTMLDivElement | null>(null);
  const cards = useMemo<GuideCard[]>(() => Array.isArray(guide?.cards) ? guide.cards : [], [guide]);
  const activeCard = cards[activeIndex] || null;
  const cacheKey = `goodhr5_help_chat_${user?.email || "guest"}`;

  /** loadGuide 读取系统帮助指南。 */
  async function loadGuide() {
    setGuideLoading(true);
    try { const data = await cloudRequest("/api/help/guide", { auth: false }); setGuide(data.guide || data || {}); }
    catch (error) { notify(error instanceof Error ? error.message : "帮助指南读取失败", "error"); }
    finally { setGuideLoading(false); }
  }

  useEffect(() => { void loadGuide(); }, []);
  useEffect(() => {
    try {
      const cached = JSON.parse(localStorage.getItem(cacheKey) || "[]");
      setMessages(Array.isArray(cached) ? cached.filter((item) => item?.role && item?.content != null).slice(-40) : []);
    } catch { setMessages([]); }
  }, [cacheKey]);
  useEffect(() => { localStorage.setItem(cacheKey, JSON.stringify(messages.slice(-40))); chatBody.current?.scrollTo({ top: chatBody.current.scrollHeight }); }, [cacheKey, messages]);

  /** askCard 将当前指南内容作为问题发送给帮助助手。 */
  function askCard(card: GuideCard) {
    const question = `请解释一下：${card.title || ""}。${card.summary || card.content || ""}`.trim();
    setInput(question);
    void send(question);
  }

  /** send 向帮助助手提交问题并流式追加回答。 */
  async function send(value?: string) {
    const question = String(value ?? input).trim();
    if (!question || loading) return;
    const history = [...messages, { role: "user" as const, content: question }].slice(-39);
    setMessages([...history, { role: "assistant", content: "" }]);
    setInput("");
    setLoading(true);
    try {
      const response = await fetch(`${CLOUD_API_BASE}/api/help/chat`, { method: "POST", headers: { "Content-Type": "application/json", Authorization: `Bearer ${getToken()}` }, body: JSON.stringify({ messages: history }) });
      if (!response.ok || !response.body) throw new Error("帮助助手请求失败");
      const reader = response.body.getReader(); const decoder = new TextDecoder(); let answer = "";
      while (true) { const { value: chunk, done } = await reader.read(); if (done) break; answer += decoder.decode(chunk, { stream: true }); setMessages([...history, { role: "assistant", content: answer }]); }
    } catch (error) {
      setMessages([...history, { role: "assistant", content: "帮助助手暂时无法连接，请检查系统 AI 配置。" }]);
      notify(error instanceof Error ? error.message : "帮助助手请求失败", "error");
    } finally { setLoading(false); }
  }

  /** clearChat 清除当前用户的帮助对话。 */
  function clearChat() { setMessages([]); localStorage.removeItem(cacheKey); notify("帮助对话已清空", "success"); }

  return <><PageHeader title="常见问题" description="查看系统使用说明，或直接向帮助助手描述遇到的问题。" actions={<RefreshButton loading={guideLoading} onClick={() => void loadGuide()} />} />
    <Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", xl: "minmax(0, .9fr) minmax(420px, 1.1fr)" }, gap: 2 }}>
      <Stack spacing={2}><Box sx={{ display: "grid", gridTemplateColumns: { xs: "1fr", sm: "repeat(2, 1fr)" }, gap: 1.25 }}>{cards.map((card, index) => <Box component="button" type="button" key={card.id || `${card.title}-${index}`} onClick={() => setActiveIndex(index)} sx={{ minHeight: 128, p: 2, textAlign: "left", font: "inherit", color: "inherit", bgcolor: activeIndex === index ? "#e7f5ed" : "#f8faf9", border: "1px solid", borderColor: activeIndex === index ? "primary.main" : "divider", borderRadius: "8px", cursor: "pointer", transition: "160ms ease", "&:hover": { borderColor: "primary.main", transform: "translateY(-1px)" } }}><Typography color="primary.main" sx={{ fontSize: 12, fontWeight: 800 }}>{String(index + 1).padStart(2, "0")}</Typography><Typography component="h2" sx={{ mt: 1, fontSize: 17, fontWeight: 780 }}>{card.title || "使用指南"}</Typography><Typography sx={{ mt: 0.75, color: "text.secondary", fontSize: 13, lineHeight: 1.65 }}>{card.summary || "点击查看详细说明"}</Typography></Box>)}</Box>
        <SectionPanel>{activeCard ? <><Typography component="h2" sx={{ fontSize: 19, fontWeight: 780 }}>{activeCard.title}</Typography><Typography sx={{ mt: 1.25, color: "text.secondary", whiteSpace: "pre-wrap", lineHeight: 1.8 }}>{activeCard.content || activeCard.summary}</Typography><Button sx={{ mt: 2 }} onClick={() => askCard(activeCard)}>问 AI 这个问题</Button></> : <Typography color="text.secondary">帮助指南正在准备中。</Typography>}</SectionPanel></Stack>
      <SectionPanel><Stack direction="row" sx={{ justifyContent: "space-between", alignItems: "flex-start", gap: 2 }}><Box><Typography component="h2" sx={{ fontSize: 19, fontWeight: 780 }}>AI 帮助助手</Typography><Typography sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}>对话只缓存在当前浏览器，最多保留最近 20 轮。</Typography></Box><Button size="small" color="secondary" startIcon={<DeleteSweepRoundedIcon />} disabled={!messages.length || loading} onClick={clearChat}>清空</Button></Stack><Stack ref={chatBody} spacing={1.5} sx={{ mt: 2, minHeight: 360, maxHeight: 560, overflowY: "auto", pr: 0.5 }}>{messages.length ? messages.map((item, index) => <Box key={`${item.role}-${index}`} sx={{ alignSelf: item.role === "user" ? "flex-end" : "flex-start", maxWidth: "88%", p: 1.5, bgcolor: item.role === "user" ? "#e7f5ed" : "#f4f7f5", border: "1px solid", borderColor: "divider", borderRadius: "8px" }}><Typography sx={{ mb: 0.5, color: "text.secondary", fontSize: 11, fontWeight: 800 }}>{item.role === "user" ? "我" : "助手"}</Typography><Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.7 }}>{item.content || "正在思考..."}</Typography></Box>) : <Typography color="text.secondary">例如：本地程序连不上怎么办？任务参数分别是什么意思？</Typography>}</Stack><Stack direction="row" spacing={1} sx={{ mt: 2, alignItems: "flex-end" }}><TextField value={input} onChange={(event) => setInput(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); void send(); } }} placeholder="描述你遇到的问题" fullWidth multiline minRows={2} maxRows={5} /><Button variant="contained" aria-label="发送问题" disabled={loading || !input.trim()} onClick={() => void send()} sx={{ minWidth: 52, height: 52 }}><SendRoundedIcon /></Button></Stack></SectionPanel>
    </Box></>;
}
