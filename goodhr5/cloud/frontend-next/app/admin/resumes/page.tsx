/** 本文件负责新版后台简历库的搜索、分页、详情跳转、备注和清空。 */
"use client";

import DeleteSweepRoundedIcon from "@mui/icons-material/DeleteSweepRounded";
import SearchRoundedIcon from "@mui/icons-material/SearchRounded";
import {
  Avatar,
  Box,
  Button,
  InputAdornment,
  MenuItem,
  Pagination,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
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
  const [tasks, setTasks] = useState<any[]>([]);
  const [positions, setPositions] = useState<any[]>([]);
  const [selectedTask, setSelectedTask] = useState(params.get("task_id") || "");
  const [selectedPosition, setSelectedPosition] = useState("");
  const [pageSize, setPageSize] = useState(10);
  const [page, setPage] = useState(1);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [noteCandidate, setNoteCandidate] =
    useState<NormalizedCandidate | null>(null);
  const [notes, setNotes] = useState<NormalizedNote[]>([]);
  const [noteContent, setNoteContent] = useState("");
  const [noteLoading, setNoteLoading] = useState(false);
  const taskID = params.get("task_id") || "";
  const candidates = useMemo(() => items.map(normalizeCandidate), [items]);

  /** load 读取简历分页列表。 */
  async function load(nextPage = page) {
    setLoading(true);
    try {
      const query = new URLSearchParams({
        page: String(nextPage),
        page_size: String(pageSize),
      });
      if (selectedTask || taskID) query.set("task_id", selectedTask || taskID);
      if (selectedPosition) query.set("position_id", selectedPosition);
      if (keyword.trim()) query.set("q", keyword.trim());
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

  /** loadFilters 读取任务和岗位筛选项。 */
  async function loadFilters() {
    const [taskData, positionData] = await Promise.allSettled([
      cloudRequest("/api/tasks"),
      cloudRequest("/api/positions"),
    ]);
    if (taskData.status === "fulfilled") setTasks(taskData.value.tasks || []);
    if (positionData.status === "fulfilled")
      setPositions(positionData.value.positions || []);
  }

  useEffect(() => {
    void loadFilters();
  }, []);
  useEffect(() => {
    void load(1);
  }, [selectedTask, selectedPosition, pageSize]);

  /** resetFilters 清空简历筛选条件。 */
  function resetFilters() {
    setKeyword("");
    setSelectedTask(taskID);
    setSelectedPosition("");
    void load(1);
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
        title='简历库'
        description={
          selectedTask
            ? "当前显示指定任务产生的简历。"
            : "简易的简历库，后续会持续升级功能。"
        }
        actions={
          <Button
            color='error'
            startIcon={<DeleteSweepRoundedIcon />}
            onClick={() => void clearAll()}
          >
            清空简历库
          </Button>
        }
      />
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: {
            xs: "1fr",
            lg: "minmax(240px,1fr) 220px 220px 100px auto auto",
          },
          gap: 1.25,
          mb: 1.5,
        }}
      >
        <TextField
          size='small'
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") void load(1);
          }}
          placeholder='搜索姓名、岗位、公司或关键词'
          slotProps={{
            input: {
              startAdornment: (
                <InputAdornment position='start'>
                  <SearchRoundedIcon />
                </InputAdornment>
              ),
            },
          }}
        />
        <TextField
          select
          size='small'
          label='任务'
          value={selectedTask}
          onChange={(event) => setSelectedTask(event.target.value)}
        >
          <MenuItem value=''>全部任务</MenuItem>
          {tasks.map((item) => (
            <MenuItem key={item.id} value={item.id}>
              {item.name}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size='small'
          label='岗位'
          value={selectedPosition}
          onChange={(event) => setSelectedPosition(event.target.value)}
        >
          <MenuItem value=''>全部岗位</MenuItem>
          {positions.map((item) => (
            <MenuItem key={item.id} value={item.id}>
              {item.name}
            </MenuItem>
          ))}
        </TextField>

        <Button
          variant='contained'
          disabled={loading}
          onClick={() => void load(1)}
        >
          查询
        </Button>
        <Button color='secondary' onClick={resetFilters}>
          重置
        </Button>
      </Box>
      <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
        {candidates.length ? (
          <>
            <Box
              sx={{
                display: { xs: "none", md: "grid" },
                gridTemplateColumns: "1.1fr 1.35fr .85fr .9fr",
                px: 2,
                py: 1.5,
                bgcolor: "#fafbfa",
                borderBottom: "1px solid",
                borderColor: "divider",
                "& p": { fontWeight: 800 },
              }}
            >
              <Typography>候选人</Typography>
              <Typography>经历</Typography>
              <Typography>AI分析</Typography>
              <Typography>备注</Typography>
            </Box>
            <Stack>
              {candidates.map((item) => (
                <ResumeRow
                  key={`${item.id}-${item.engagementId}`}
                  item={item}
                  onOpenNotes={openNotes}
                />
              ))}
            </Stack>
          </>
        ) : (
          <EmptyState text={loading ? "正在读取简历" : "暂无简历"} />
        )}
        <Stack
          direction={{ xs: "column", sm: "row" }}
          spacing={2}
          sx={{
            p: 2,
            justifyContent: "space-between",
            alignItems: "center",
            borderTop: "1px solid",
            borderColor: "divider",
          }}
        >
          <Typography color='text.secondary'>共 {total} 份简历</Typography>
          <Pagination
            page={page}
            count={Math.max(1, Math.ceil(total / pageSize))}
            onChange={(_, value) => void load(value)}
            color='primary'
          />
        </Stack>
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
function ResumeRow({
  item,
  onOpenNotes,
}: {
  item: NormalizedCandidate;
  onOpenNotes: (item: NormalizedCandidate) => void;
}) {
  const href = `/admin/resumes/detail?candidate_id=${encodeURIComponent(item.id)}${item.engagementId ? `&engagement_id=${encodeURIComponent(item.engagementId)}` : ""}`;
  const facts = [
    item.workRegion,
    item.age ? `${item.age}岁` : "",
    item.gender,
    item.workYears,
    item.educationLevel,
  ]
    .filter(Boolean)
    .join(" / ");
  const ownerLine = [
    item.creatorEmail ? `创建人：${item.creatorEmail}` : "",
    item.createdAt ? `创建时间：${formatDate(item.createdAt)}` : "",
  ]
    .filter(Boolean)
    .join("  ");
  const experiences = [...item.workExperiences, ...item.educations].slice(0, 3);
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: { xs: "1fr", md: "1.1fr 1.35fr .85fr .9fr" },
        gap: { xs: 1.25, md: 2 },
        alignItems: "center",
        width: "100%",
        px: 2,
        py: 2,
        borderBottom: "1px solid",
        borderColor: "divider",
      }}
    >
      <Button
        component={Link}
        href={href}
        color='secondary'
        sx={{
          justifyContent: "flex-start",
          p: 0,
          textAlign: "left",
          minWidth: 0,
        }}
      >
        <Stack
          direction='row'
          spacing={1.5}
          sx={{ minWidth: 0, alignItems: "center" }}
        >
          <Avatar src={item.avatarUrl}>{item.name.slice(0, 1)}</Avatar>
          <Box sx={{ minWidth: 0 }}>
            <Typography noWrap sx={{ fontWeight: 820 }}>
              {item.name}
            </Typography>
            <Typography
              noWrap
              sx={{ mt: 0.4, color: "text.secondary", fontSize: 13 }}
            >
              {facts || "暂无基础信息"}
            </Typography>
            <Typography noWrap sx={{ mt: 0.6 }}>
              {item.expectedPosition || "暂无期望职位"}
            </Typography>
            {ownerLine ? (
              <Typography
                noWrap
                sx={{ mt: 0.5, color: "text.secondary", fontSize: 12 }}
              >
                {ownerLine}
              </Typography>
            ) : null}
          </Box>
        </Stack>
      </Button>
      <Stack spacing={0.6} sx={{ minWidth: 0 }}>
        {experiences.length ? (
          experiences.map((experience, index) => (
            <ExperienceSummary
              key={`${experience.companyName || experience.schoolName || experience.projectName || index}-${index}`}
              item={experience}
            />
          ))
        ) : (
          <Typography color='text.secondary'>暂无经历</Typography>
        )}
      </Stack>
      <Stack spacing={0.8} sx={{ minWidth: 0 }}>
        <AIText
          label='第一次'
          score={item.aiFirstAnalysis.score}
          reason={item.aiFirstAnalysis.reason}
        />
        <AIText
          label='第二次'
          score={item.aiSecondAnalysis.score}
          reason={item.aiSecondAnalysis.reason}
        />
      </Stack>
      <NotePreview notes={item.notes} onClick={() => onOpenNotes(item)} />
    </Box>
  );
}

/** ExperienceSummary 展示简历库列表中的经历摘要。 */
function ExperienceSummary({ item }: { item: NormalizedExperience }) {
  const mainText = item.companyName || item.schoolName || item.projectName || "";
  const detailText = [
    item.positionName || item.majorName || item.roleName || item.educationLevel,
    periodText(item),
  ]
    .filter(Boolean)
    .join(" / ");

  if (!mainText && !detailText) return null;

  return (
    <Typography noWrap sx={{ fontSize: 14 }}>
      {item.companyName ? (
        <Box component='span' sx={{ fontWeight: 820 }}>
          {item.companyName}
        </Box>
      ) : (
        mainText
      )}
      {detailText ? `${mainText ? " / " : ""}${detailText}` : ""}
    </Typography>
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
      color='secondary'
      onClick={onClick}
      sx={{
        display: "block",
        minWidth: 0,
        p: 0,
        textAlign: "left",
        bgcolor: "transparent",
      }}
    >
      <Typography
        sx={{ mb: 0.6, color: "#16724c", fontSize: 12, fontWeight: 820 }}
      >
        备注
      </Typography>
      {notes.length ? (
        <Stack spacing={0.5}>
          {notes.slice(0, 2).map((note) => (
            <Typography
              key={note.id || note.createdAt}
              title={note.content}
              sx={{
                color: "text.secondary",
                display: "-webkit-box",
                fontSize: 12,
                lineHeight: 1.55,
                overflow: "hidden",
                overflowWrap: "anywhere",
                WebkitBoxOrient: "vertical",
                WebkitLineClamp: 2,
                whiteSpace: "normal",
              }}
            >
              {note.content}
            </Typography>
          ))}
        </Stack>
      ) : (
        <Typography sx={{ color: "text.secondary", fontSize: 12 }}>
          这里暂时没备注
        </Typography>
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
      title='备注记录'
      description={candidate ? `候选人：${candidate.name}` : ""}
      maxWidth='lg'
      confirmText='新增备注'
      confirmDisabled={!content.trim()}
      loading={loading}
      onClose={onClose}
      onConfirm={onAdd}
    >
      <Box
        sx={{
          display: "grid",
          gridTemplateColumns: { xs: "1fr", md: "minmax(0, 1fr) 360px" },
          gap: 2.5,
        }}
      >
        <Stack
          spacing={1.25}
          sx={{ maxHeight: 420, overflowY: "auto", pr: 0.5 }}
        >
          {notes.length ? (
            notes.map((note) => (
              <Box
                key={note.id || note.createdAt}
                sx={{
                  p: 1.5,
                  border: "1px solid",
                  borderColor: "divider",
                  borderRadius: "8px",
                  bgcolor: "#fbfdfc",
                }}
              >
                <Typography sx={{ whiteSpace: "pre-wrap", lineHeight: 1.75 }}>
                  {note.content}
                </Typography>
                <Typography
                  sx={{ mt: 1, color: "text.secondary", fontSize: 12 }}
                >
                  备注人：{note.authorEmail || "暂时没记上"} · 备注时间：
                  {formatDate(note.createdAt)}
                </Typography>
              </Box>
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
        </Stack>
        <TextField
          label='新增备注'
          value={content}
          onChange={(event) => onContent(event.target.value)}
          placeholder='比如：候选人意向不错，下午再约一次。'
          multiline
          minRows={8}
          slotProps={{ htmlInput: { maxLength: 1000 } }}
          helperText={`${content.length}/1000`}
          fullWidth
        />
      </Box>
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
    <Typography
      title={text}
      sx={{
        color: "text.secondary",
        display: "-webkit-box",
        fontSize: 12,
        lineHeight: 1.6,
        overflow: "hidden",
        overflowWrap: "anywhere",
        WebkitBoxOrient: "vertical",
        WebkitLineClamp: 2,
        whiteSpace: "normal",
      }}
    >
      <Box component='span' sx={{ color: "#16724c", fontWeight: 800 }}>
        {label} {scoreText(score)}
      </Box>
      {reason ? `：${reason}` : ""}
    </Typography>
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
