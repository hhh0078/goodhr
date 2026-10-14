/** 本文件负责 帮助指南和 AI 问答 的 Ant Design 页面。 */
"use client";
import { Button, Card, Flex, Input, Typography } from "antd";
import { ClearOutlined, SendOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import { useEffect, useMemo, useRef, useState } from "react";
import { CLOUD_API_BASE, cloudRequest, getToken } from "@/lib/admin-api";
import {
  FormActionRow,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
type GuideCard = {
  id?: string;
  title?: string;
  summary?: string;
  content?: string;
};
type ChatMessage = {
  role: "user" | "assistant";
  content: string;
};
/** HelpPage 展示系统指南并提供带本地缓存的流式 AI 问答。 */
export default function HelpPage() {
  const { user, notify } = useAdmin();
  const [guide, setGuide] = useState<any>({});
  const [activeIndex, setActiveIndex] = useState(0);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [loading, setLoading] = useState(false);
  const chatBody = useRef<HTMLDivElement | null>(null);
  const cards = useMemo<GuideCard[]>(
    () => (Array.isArray(guide?.cards) ? guide.cards : []),
    [guide],
  );
  const activeCard = cards[activeIndex] || null;
  const cacheKey = `goodhr5_help_chat_${user?.email || "guest"}`;
  /** loadGuide 读取系统帮助指南。 */
  async function loadGuide() {
    try {
      const data = await cloudRequest("/api/help/guide", { auth: false });
      setGuide(data.guide || data || {});
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "帮助指南读取失败",
        "error",
      );
    }
  }
  useEffect(() => {
    void loadGuide();
  }, []);
  useEffect(() => {
    try {
      const cached = JSON.parse(localStorage.getItem(cacheKey) || "[]");
      setMessages(
        Array.isArray(cached)
          ? cached
              .filter((item) => item?.role && item?.content != null)
              .slice(-40)
          : [],
      );
    } catch {
      setMessages([]);
    }
  }, [cacheKey]);
  useEffect(() => {
    localStorage.setItem(cacheKey, JSON.stringify(messages.slice(-40)));
    chatBody.current?.scrollTo({ top: chatBody.current.scrollHeight });
  }, [cacheKey, messages]);
  /** askCard 将当前指南内容作为问题发送给帮助助手。 */
  function askCard(card: GuideCard) {
    const question =
      `请解释一下：${card.title || ""}。${card.summary || card.content || ""}`.trim();
    setInput(question);
    void send(question);
  }
  /** send 向帮助助手提交问题并流式追加回答。 */
  async function send(value?: string) {
    const question = String(value ?? input).trim();
    if (!question || loading) return;
    const history = [
      ...messages,
      { role: "user" as const, content: question },
    ].slice(-39);
    setMessages([...history, { role: "assistant", content: "" }]);
    setInput("");
    setLoading(true);
    try {
      const response = await fetch(`${CLOUD_API_BASE}/api/help/chat`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${getToken()}`,
        },
        body: JSON.stringify({ messages: history }),
      });
      if (!response.ok || !response.body) throw new Error("帮助助手请求失败");
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let answer = "";
      while (true) {
        const { value: chunk, done } = await reader.read();
        if (done) break;
        answer += decoder.decode(chunk, { stream: true });
        setMessages([...history, { role: "assistant", content: answer }]);
      }
    } catch (error) {
      setMessages([
        ...history,
        {
          role: "assistant",
          content: "帮助助手暂时无法连接，请检查系统 AI 配置。",
        },
      ]);
      notify(
        error instanceof Error ? error.message : "帮助助手请求失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }
  /** clearChat 清除当前用户的帮助对话。 */
  function clearChat() {
    setMessages([]);
    localStorage.removeItem(cacheKey);
    notify("帮助对话已清空", "success");
  }
  return (
    <>
      <PageHeader
        title="常见问题"
        description="查看系统使用说明，或直接向帮助助手描述遇到的问题。"
      />
      <div className={styles.layout18} style={{ display: "grid", gap: 16 }}>
        <Flex vertical={true} gap={2 * 8}>
          <div className={styles.layout5} style={{ display: "grid", gap: 10 }}>
            {cards.map((card, index) => (
              <Card
                hoverable
                size="small"
                key={card.id || card.title + "-" + index}
                onClick={() => setActiveIndex(index)}
                role="button"
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    setActiveIndex(index);
                  }
                }}
                style={{
                  minHeight: 128,
                  borderColor:
                    activeIndex === index
                      ? "var(--goodhr-primary)"
                      : "var(--goodhr-border)",
                  background:
                    activeIndex === index
                      ? "var(--goodhr-primary-soft)"
                      : "var(--goodhr-hover)",
                }}
              >
                <Typography.Text
                  style={{ color: "var(--goodhr-primary)", fontWeight: 800 }}
                >
                  {String(index + 1).padStart(2, "0")}
                </Typography.Text>
                <Typography.Title level={5} style={{ margin: "8px 0 6px" }}>
                  {card.title || "使用指南"}
                </Typography.Title>
                <Typography.Text type="secondary">
                  {card.summary || "点击查看详细说明"}
                </Typography.Text>
              </Card>
            ))}
          </div>
          <SectionPanel>
            {activeCard ? (
              <>
                <Typography.Title
                  level={2}
                  style={{
                    display: "block",
                    margin: 0,
                    fontSize: 19,
                    fontWeight: 780,
                  }}
                >
                  {activeCard.title}
                </Typography.Title>
                <Typography.Text
                  style={{
                    display: "block",
                    margin: 0,
                    marginTop: 10,
                    color: "var(--goodhr-text-secondary)",
                    whiteSpace: "pre-wrap",
                    lineHeight: 1.8,
                  }}
                >
                  {activeCard.content || activeCard.summary}
                </Typography.Text>
                <Button
                  style={{ marginTop: 16 }}
                  onClick={() => askCard(activeCard)}
                >
                  问 AI 这个问题
                </Button>
              </>
            ) : (
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  color: "var(--goodhr-text-secondary)",
                }}
              >
                帮助指南正在准备中。
              </Typography.Text>
            )}
          </SectionPanel>
        </Flex>
        <SectionPanel>
          <Flex
            style={{
              justifyContent: "space-between",
              alignItems: "flex-start",
              gap: 16,
            }}
          >
            <div>
              <Typography.Title
                level={2}
                style={{
                  display: "block",
                  margin: 0,
                  fontSize: 19,
                  fontWeight: 780,
                }}
              >
                AI 帮助助手
              </Typography.Title>
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  marginTop: 4,
                  color: "var(--goodhr-text-secondary)",
                  fontSize: 13,
                }}
              >
                对话只缓存在当前浏览器，最多保留最近 20 轮。
              </Typography.Text>
            </div>
            <Button
              icon={<ClearOutlined aria-hidden="true" />}
              size="small"
              disabled={!messages.length || loading}
              onClick={clearChat}
            >
              清空
            </Button>
          </Flex>
          <Flex
            vertical={true}
            gap={1.5 * 8}
            style={{
              marginTop: 16,
              minHeight: 360,
              maxHeight: 560,
              overflowY: "auto",
              paddingRight: 4,
            }}
            ref={chatBody}
          >
            {messages.length ? (
              messages.map((item, index) => (
                <div
                  style={{
                    alignSelf: item.role === "user" ? "flex-end" : "flex-start",
                    maxWidth: "88%",
                    padding: 12,
                    backgroundColor:
                      item.role === "user"
                        ? "var(--goodhr-primary-soft)"
                        : "var(--goodhr-hover)",
                    border: "1px solid",
                    borderColor: "var(--goodhr-border)",
                    borderRadius: "8px",
                  }}
                  key={`${item.role}-${index}`}
                >
                  <Typography.Text
                    style={{
                      display: "block",
                      margin: 0,
                      marginBottom: 4,
                      color: "var(--goodhr-text-secondary)",
                      fontSize: 11,
                      fontWeight: 800,
                    }}
                  >
                    {item.role === "user" ? "我" : "助手"}
                  </Typography.Text>
                  <Typography.Text
                    style={{
                      display: "block",
                      margin: 0,
                      whiteSpace: "pre-wrap",
                      lineHeight: 1.7,
                    }}
                  >
                    {item.content || "正在思考..."}
                  </Typography.Text>
                </div>
              ))
            ) : (
              <Typography.Text
                style={{
                  display: "block",
                  margin: 0,
                  color: "var(--goodhr-text-secondary)",
                }}
              >
                例如：本地程序连不上怎么办？岗位运行参数分别是什么意思？
              </Typography.Text>
            )}
          </Flex>
          <div style={{ marginTop: 16 }}>
            <FormActionRow
              maxWidth="100%"
              field={
                <Input.TextArea
                  autoSize={{ minRows: 2, maxRows: 5 }}
                  style={{ width: "100%" }}
                  value={input}
                  onChange={(event) => setInput(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === "Enter" && !event.shiftKey) {
                      event.preventDefault();
                      void send();
                    }
                  }}
                  placeholder="描述你遇到的问题"
                />
              }
              action={
                <Button
                  type={"primary"}
                  style={{ minWidth: 48 }}
                  aria-label="发送问题"
                  disabled={loading || !input.trim()}
                  onClick={() => void send()}
                >
                  <SendOutlined aria-hidden="true" />
                </Button>
              }
            />
          </div>
        </SectionPanel>
      </div>
    </>
  );
}
