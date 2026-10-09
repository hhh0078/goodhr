/** 本文件负责 简历库与候选人备注 的 Ant Design 页面。 */
"use client";
import {
  Avatar,
  Button,
  Flex,
  Form,
  Input,
  Pagination,
  Select,
  Table,
  Typography,
} from "antd";
import { ClearOutlined, SearchOutlined } from "@ant-design/icons";
import styles from "./page.module.css";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudRequest, formatDate } from "@/lib/admin-api";
import {
  normalizeCandidate,
  periodText,
  scoreText,
  type NormalizedCandidate,
  type NormalizedExperience,
  type NormalizedNote,
} from "@/lib/candidate-normalize";
/** ResumesPage 展示云端保存的候选人简历列表。 */
export default function ResumesPage() {
  const params = useSearchParams();
  const { notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [keyword, setKeyword] = useState("");
  const [positions, setPositions] = useState<any[]>([]);
  const [selectedPosition, setSelectedPosition] = useState(
    params.get("position_id") || "",
  );
  const [pageSize, setPageSize] = useState(10);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [noteCandidate, setNoteCandidate] =
    useState<NormalizedCandidate | null>(null);
  const [notes, setNotes] = useState<NormalizedNote[]>([]);
  const [noteContent, setNoteContent] = useState("");
  const [noteLoading, setNoteLoading] = useState(false);
  const positionID = params.get("position_id") || "";
  const candidates = useMemo(() => items.map(normalizeCandidate), [items]);
  /** load 读取简历分页列表。 */
  async function load(
    nextPage = page,
    nextKeyword = keyword,
    nextPosition = selectedPosition,
  ) {
    setLoading(true);
    try {
      const query = new URLSearchParams({
        page: String(nextPage),
        page_size: String(pageSize),
      });
      if (nextPosition) query.set("position_id", nextPosition);
      if (nextKeyword.trim()) query.set("q", nextKeyword.trim());
      const data = await cloudRequest(`/api/candidates?${query}`);
      setItems(data.candidates || data.items || []);
      setTotal(Number(data.total || 0));
      setPage(Number(data.page || nextPage));
    } catch (error) {
      notify(error instanceof Error ? error.message : "简历读取失败", "error");
    } finally {
      setLoading(false);
    }
  }
  /** loadFilters 读取岗位运行和岗位筛选项。 */
  async function loadFilters() {
    const [positionData] = await Promise.allSettled([
      cloudRequest("/api/positions"),
    ]);
    if (positionData.status === "fulfilled")
      setPositions(positionData.value.positions || []);
  }
  useEffect(() => {
    void loadFilters();
  }, []);
  useEffect(() => {
    void load(1);
  }, [selectedPosition, pageSize]);
  /** resetFilters 清空简历筛选条件。 */
  function resetFilters() {
    setKeyword("");
    setSelectedPosition("");
    void load(1, "", "");
  }
  /** clearAll 清空当前团队简历库。 */
  async function clearAll() {
    try {
      if (
        !(await confirm(
          "清空简历库",
          "我小声确认一下，清空后这些简历记录就找不回来了。继续吗？",
        ))
      )
        return;
      const data = await cloudRequest("/api/candidates", { method: "DELETE" });
      notify(`已删除 ${Number(data.deleted || 0)} 份简历`, "success");
      await load(1);
    } catch (error) {
      notify(error instanceof Error ? error.message : "清空失败", "error");
    }
  }
  /** openNotes 打开候选人备注弹框并读取完整备注。 */
  async function openNotes(candidate: NormalizedCandidate) {
    setNoteCandidate(candidate);
    setNotes(candidate.notes || []);
    setNoteContent("");
    setNoteLoading(true);
    try {
      const data = await cloudRequest(
        `/api/candidates/${encodeURIComponent(candidate.id)}/notes`,
      );
      setNotes((data.notes || []).map(normalizeNote));
    } catch (error) {
      notify(error instanceof Error ? error.message : "备注读取失败", "error");
    } finally {
      setNoteLoading(false);
    }
  }
  /** addNote 新增候选人备注。 */
  async function addNote() {
    if (!noteCandidate || !noteContent.trim()) return;
    setNoteLoading(true);
    try {
      const data = await cloudRequest(
        `/api/candidates/${encodeURIComponent(noteCandidate.id)}/notes`,
        { method: "POST", body: { content: noteContent.trim() } },
      );
      const nextNote = normalizeNote(data.note);
      setNotes((current) => [nextNote, ...current]);
      setItems((current) =>
        current.map((item) =>
          item.id === noteCandidate.id
            ? { ...item, notes: [data.note, ...(item.notes || [])].slice(0, 2) }
            : item,
        ),
      );
      setNoteContent("");
      notify("备注已记上，打工小本本更新了", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "备注保存失败", "error");
    } finally {
      setNoteLoading(false);
    }
  }
  return (
    <>
      <PageHeader
        title="简历库"
        description={
          selectedPosition
            ? "当前显示指定岗位运行产生的简历。"
            : "目前已经默认不生成简历、降低AI消耗，如果需要生成简历，请在岗位设置->高级设置 中开启。"
        }
        actions={
          <Button
            danger={true}
            icon={<ClearOutlined aria-hidden="true" />}
            onClick={() => void clearAll()}
          >
            清空简历库
          </Button>
        }
      />
      <div
        className={styles.layout1}
        style={{ display: "grid", gap: 10, marginBottom: 12 }}
      >
        <Input
          prefix={
            <span>
              <SearchOutlined aria-hidden="true" />
            </span>
          }
          size="small"
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") void load(1);
          }}
          placeholder="搜索姓名、岗位、公司或关键词"
        />
        <Form.Item
          htmlFor="goodhr-resumes-field-1"
          layout="vertical"
          style={{ marginBottom: 0, minWidth: 0 }}
          label={"岗位"}
        >
          <Select
            id="goodhr-resumes-field-1"
            onChange={(value) => setSelectedPosition(value)}
            style={{ width: "100%" }}
            size="small"
            value={selectedPosition}
          >
            <Select.Option value="">全部岗位</Select.Option>
            {positions.map((item) => (
              <Select.Option key={item.id} value={item.id}>
                {item.name}
              </Select.Option>
            ))}
          </Select>
        </Form.Item>
        <Button
          type={"primary"}
          disabled={loading}
          onClick={() => void load(1)}
        >
          查询
        </Button>
        <Button onClick={resetFilters}>重置</Button>
      </div>
      <SectionPanel style={{ padding: 0, overflow: "hidden" }}>
        <Table<NormalizedCandidate>
          rowKey={(item) => item.id + "-" + item.engagementId}
          dataSource={candidates}
          loading={loading}
          size="small"
          pagination={false}
          scroll={{ x: 1120 }}
          locale={{ emptyText: <EmptyState text="暂无简历" /> }}
          columns={[
            {
              key: "candidate",
              title: "候选人",
              width: 280,
              render: (_, item) => <CandidateSummary item={item} />,
            },
            {
              key: "experience",
              title: "经历",
              width: 320,
              render: (_, item) => (
                <Flex vertical gap={6}>
                  {[...item.workExperiences, ...item.educations]
                    .slice(0, 3)
                    .map((experience, index) => (
                      <ExperienceSummary key={index} item={experience} />
                    ))}
                  {!item.workExperiences.length && !item.educations.length && (
                    <Typography.Text type="secondary">暂无经历</Typography.Text>
                  )}
                </Flex>
              ),
            },
            {
              key: "ai",
              title: "AI分析",
              width: 250,
              render: (_, item) => (
                <Flex vertical gap={6}>
                  <AIText
                    label="第一次"
                    score={item.aiFirstAnalysis.score}
                    reason={item.aiFirstAnalysis.reason}
                  />
                  <AIText
                    label="第二次"
                    score={item.aiSecondAnalysis.score}
                    reason={item.aiSecondAnalysis.reason}
                  />
                </Flex>
              ),
            },
            {
              key: "notes",
              title: "备注",
              width: 250,
              render: (_, item) => (
                <NotePreview
                  notes={item.notes}
                  onClick={() => void openNotes(item)}
                />
              ),
            },
          ]}
        />
        <Flex
          wrap
          gap={12}
          justify="space-between"
          align="center"
          style={{ padding: 16 }}
        >
          <Typography.Text type="secondary">共 {total} 份简历</Typography.Text>
          <Pagination
            current={page}
            total={total}
            pageSize={pageSize}
            showSizeChanger
            pageSizeOptions={[10, 20, 50, 100]}
            onChange={(next, size) => {
              if (size !== pageSize) {
                setPageSize(size);
                setPage(1);
              } else void load(next);
            }}
          />
        </Flex>
      </SectionPanel>
      <NoteDialog
        candidate={noteCandidate}
        notes={notes}
        content={noteContent}
        loading={noteLoading}
        onContent={setNoteContent}
        onClose={() => setNoteCandidate(null)}
        onAdd={addNote}
      />
    </>
  );
}
/** ResumeRow 展示一行简历库候选人。 */
/** CandidateSummary 展示候选人的基础信息并保留详情入口。 */
function CandidateSummary({ item }: { item: NormalizedCandidate }) {
  const href =
    "/admin/resumes/detail?candidate_id=" +
    encodeURIComponent(item.id) +
    (item.engagementId
      ? "&engagement_id=" + encodeURIComponent(item.engagementId)
      : "");
  const facts = [
    item.workRegion,
    item.age ? item.age + "岁" : "",
    item.gender,
    item.workYears,
    item.educationLevel,
  ]
    .filter(Boolean)
    .join(" / ");
  return (
    <Link href={href} style={{ color: "inherit", display: "block" }}>
      <Flex align="center" gap={12}>
        <Avatar src={item.avatarUrl}>{item.name.slice(0, 1)}</Avatar>
        <Flex vertical style={{ minWidth: 0 }} gap={4}>
          <Typography.Text strong>{item.name}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {facts || "暂无基础信息"}
          </Typography.Text>
          <Typography.Text>
            {item.expectedPosition || "暂无期望职位"}
          </Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {item.creatorEmail ? "创建人：" + item.creatorEmail : ""}
            {item.createdAt ? " · " + formatDate(item.createdAt) : ""}
          </Typography.Text>
        </Flex>
      </Flex>
    </Link>
  );
}
/** ExperienceSummary 展示简历库列表中的经历摘要。 */
function ExperienceSummary({ item }: { item: NormalizedExperience }) {
  const mainText =
    item.companyName || item.schoolName || item.projectName || "";
  const detailText = [
    item.positionName || item.majorName || item.roleName || item.educationLevel,
    periodText(item),
  ]
    .filter(Boolean)
    .join(" / ");
  if (!mainText && !detailText) return null;
  return (
    <Typography.Text
      ellipsis={true}
      style={{ display: "block", margin: 0, fontSize: 14 }}
    >
      {item.companyName ? (
        <span style={{ fontWeight: 820 }}>{item.companyName}</span>
      ) : (
        mainText
      )}
      {detailText ? `${mainText ? " / " : ""}${detailText}` : ""}
    </Typography.Text>
  );
}
/** NotePreview 展示候选人最新两条备注入口。 */
function NotePreview({
  notes,
  onClick,
}: {
  notes: NormalizedNote[];
  onClick: () => void;
}) {
  return (
    <Button
      type="text"
      style={{
        display: "block",
        height: "auto",
        width: "100%",
        whiteSpace: "normal",
        minWidth: 0,
        padding: 0,
        textAlign: "left",
      }}
      onClick={onClick}
    >
      <Typography.Text
        style={{
          display: "block",
          margin: 0,
          marginBottom: 4.8,
          color: "var(--goodhr-primary)",
          fontSize: 12,
          fontWeight: 820,
        }}
      >
        备注
      </Typography.Text>
      {notes.length ? (
        <Flex vertical={true} gap={0.5 * 8}>
          {notes.slice(0, 2).map((note) => (
            <Typography.Text
              style={{
                margin: 0,
                color: "var(--goodhr-text-secondary)",
                display: "-webkit-box",
                fontSize: 12,
                lineHeight: 1.55,
                overflow: "hidden",
                overflowWrap: "anywhere",
                WebkitBoxOrient: "vertical",
                WebkitLineClamp: 2,
                whiteSpace: "normal",
              }}
              key={note.id || note.createdAt}
              title={note.content}
            >
              {note.content}
            </Typography.Text>
          ))}
        </Flex>
      ) : (
        <Typography.Text
          style={{
            display: "block",
            margin: 0,
            color: "var(--goodhr-text-secondary)",
            fontSize: 12,
          }}
        >
          这里暂时没备注
        </Typography.Text>
      )}
    </Button>
  );
}
/** NoteDialog 展示候选人备注记录和新增表单。 */
function NoteDialog({
  candidate,
  notes,
  content,
  loading,
  onContent,
  onClose,
  onAdd,
}: {
  candidate: NormalizedCandidate | null;
  notes: NormalizedNote[];
  content: string;
  loading: boolean;
  onContent: (value: string) => void;
  onClose: () => void;
  onAdd: () => void;
}) {
  return (
    <AdminDialog
      open={Boolean(candidate)}
      title="备注记录"
      description={candidate ? `候选人：${candidate.name}` : ""}
      maxWidth="lg"
      confirmText="新增备注"
      confirmDisabled={!content.trim()}
      loading={loading}
      onClose={onClose}
      onConfirm={onAdd}
    >
      <div className={styles.layout26} style={{ display: "grid", gap: 20 }}>
        <Flex
          vertical={true}
          gap={1.25 * 8}
          style={{ maxHeight: 420, overflowY: "auto", paddingRight: 4 }}
        >
          {notes.length ? (
            notes.map((note) => (
              <div
                style={{
                  padding: 12,
                  border: "1px solid",
                  borderColor: "var(--goodhr-border)",
                  borderRadius: "8px",
                  backgroundColor: "var(--goodhr-hover)",
                }}
                key={note.id || note.createdAt}
              >
                <Typography.Text
                  style={{
                    display: "block",
                    margin: 0,
                    whiteSpace: "pre-wrap",
                    lineHeight: 1.75,
                  }}
                >
                  {note.content}
                </Typography.Text>
                <Typography.Text
                  style={{
                    display: "block",
                    margin: 0,
                    marginTop: 8,
                    color: "var(--goodhr-text-secondary)",
                    fontSize: 12,
                  }}
                >
                  备注人：{note.authorEmail || "暂时没记上"}· 备注时间：
                  {formatDate(note.createdAt)}
                </Typography.Text>
              </div>
            ))
          ) : (
            <EmptyState
              text={
                loading
                  ? "正在读取备注"
                  : "这里暂时没备注，先写一条也行，我不挑"
              }
            />
          )}
        </Flex>
        <Form.Item
          htmlFor="goodhr-resumes-field-2"
          layout="vertical"
          style={{ marginBottom: 0, minWidth: 0 }}
          label={"新增备注"}
          extra={`${content.length}/1000`}
        >
          <Input.TextArea
            id="goodhr-resumes-field-2"
            autoSize={{ minRows: 8 }}
            maxLength={1000}
            style={{ width: "100%" }}
            value={content}
            onChange={(event) => onContent(event.target.value)}
            placeholder="比如：候选人意向不错，下午再约一次。"
          />
        </Form.Item>
      </div>
    </AdminDialog>
  );
}
/** AIText 展示一次 AI 判断结果。 */
function AIText({
  label,
  score,
  reason,
}: {
  label: string;
  score: unknown;
  reason: string;
}) {
  if (!reason && scoreText(score) === "无") return null;
  const text = `${label} ${scoreText(score)}${reason ? `：${reason}` : ""}`;
  return (
    <Typography.Text
      style={{
        margin: 0,
        color: "var(--goodhr-text-secondary)",
        display: "-webkit-box",
        fontSize: 12,
        lineHeight: 1.6,
        overflow: "hidden",
        overflowWrap: "anywhere",
        WebkitBoxOrient: "vertical",
        WebkitLineClamp: 2,
        whiteSpace: "normal",
      }}
      title={text}
    >
      <span style={{ color: "var(--goodhr-primary)", fontWeight: 800 }}>
        {label}
        {scoreText(score)}
      </span>
      {reason ? `：${reason}` : ""}
    </Typography.Text>
  );
}
/** normalizeNote 归一化备注接口数据。 */
function normalizeNote(input: any): NormalizedNote {
  return {
    id: String(input?.id || ""),
    content: String(input?.content || ""),
    authorEmail: String(input?.author_email || ""),
    createdAt: String(input?.created_at || ""),
  };
}
