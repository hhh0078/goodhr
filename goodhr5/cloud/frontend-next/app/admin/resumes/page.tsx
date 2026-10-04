/** 本文件负责新版后台简历库的搜索、分页、详情跳转、备注和清空。 */
"use client";

import DeleteSweepRoundedIcon from "@mui/icons-material/DeleteSweepRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
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
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import {
  EmptyState,
  PageHeader,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import { cloudAssetURL, cloudRequest, formatDate } from "@/lib/admin-api";
import {
  normalizeCandidate,
  periodText,
  scoreText,
  type NormalizedCandidate,
  type NormalizedExperience,
  type NormalizedNote,
} from "@/lib/candidate-normalize";
import {
  DEFAULT_RESUME_FILTERS,
  resumeFiltersFromParams,
  resumeListQuery,
  resumePageNumber,
  type ResumeFilters,
} from "@/lib/resume-filters";

/** ResumesPage 展示云端保存的候选人简历列表。 */
export default function ResumesPage() {
  const params = useSearchParams();
  const router = useRouter();
  const { notify, confirm } = useAdmin();
  const paramsQuery = params.toString();
  const initialFilters = resumeFiltersFromParams(params);
  const initialPage = resumePageNumber(params.get("page"), 1);
  const initialPageSize = resumePageNumber(params.get("page_size"), 10, 100);
  const [items, setItems] = useState<any[]>([]);
  const [keyword, setKeyword] = useState(initialFilters.keyword);
  const [positions, setPositions] = useState<any[]>([]);
  const [selectedPosition, setSelectedPosition] = useState(initialFilters.positionID);
  const [platformID, setPlatformID] = useState(initialFilters.platformID);
  const [phoneStatus, setPhoneStatus] = useState(initialFilters.phoneStatus);
  const [conditionStatus, setConditionStatus] = useState(initialFilters.conditionStatus);
  const [sort, setSort] = useState(initialFilters.sort);
  const [pageSize, setPageSize] = useState(initialPageSize);
  const [page, setPage] = useState(initialPage);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [deletingID, setDeletingID] = useState("");
  const [noteCandidate, setNoteCandidate] =
    useState<NormalizedCandidate | null>(null);
  const [notes, setNotes] = useState<NormalizedNote[]>([]);
  const [noteContent, setNoteContent] = useState("");
  const [noteLoading, setNoteLoading] = useState(false);
  const requestSequence = useRef(0);
  const candidates = useMemo(() => items.map(normalizeCandidate), [items]);

  /** activeFilters 返回当前筛选表单值，供查询和地址同步共用。 */
  function activeFilters(): ResumeFilters {
    return {
      keyword,
      positionID: selectedPosition,
      platformID,
      phoneStatus,
      conditionStatus,
      sort,
    };
  }

  /** load 读取简历分页列表。 */
  async function load(
    nextPage = page,
    filters: ResumeFilters = activeFilters(),
    nextPageSize = pageSize,
  ) {
    const requestID = ++requestSequence.current;
    setLoading(true);
    try {
      const query = resumeListQuery(filters, nextPage, nextPageSize);
      const data = await cloudRequest(`/api/candidates?${query}`);
      if (requestID !== requestSequence.current) return;
      setItems(data.candidates || data.items || []);
      setTotal(Number(data.total || 0));
      setPage(Number(data.page || nextPage));
    } catch (error) {
      if (requestID === requestSequence.current) {
        notify(error instanceof Error ? error.message : "简历读取失败", "error");
      }
    } finally {
      if (requestID === requestSequence.current) setLoading(false);
    }
  }

  /** showList 同步简历库地址；地址未变化时直接刷新当前列表。 */
  async function showList(
    nextPage = page,
    filters: ResumeFilters = activeFilters(),
    nextPageSize = pageSize,
  ) {
    const query = resumeListQuery(filters, nextPage, nextPageSize).toString();
    if (query === paramsQuery) {
      await load(nextPage, filters, nextPageSize);
      return;
    }
    requestSequence.current += 1;
    setLoading(true);
    router.replace(`/admin/resumes?${query}`, { scroll: false });
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
    const nextParams = new URLSearchParams(paramsQuery);
    const nextFilters = resumeFiltersFromParams(nextParams);
    const nextPage = resumePageNumber(nextParams.get("page"), 1);
    const nextPageSize = resumePageNumber(nextParams.get("page_size"), 10, 100);
    const canonicalQuery = resumeListQuery(
      nextFilters,
      nextPage,
      nextPageSize,
    ).toString();
    if (canonicalQuery !== paramsQuery) {
      requestSequence.current += 1;
      setLoading(true);
      router.replace(`/admin/resumes?${canonicalQuery}`, { scroll: false });
      return;
    }
    setKeyword(nextFilters.keyword);
    setSelectedPosition(nextFilters.positionID);
    setPlatformID(nextFilters.platformID);
    setPhoneStatus(nextFilters.phoneStatus);
    setConditionStatus(nextFilters.conditionStatus);
    setSort(nextFilters.sort);
    setPage(nextPage);
    setPageSize(nextPageSize);
    void load(nextPage, nextFilters, nextPageSize);
  }, [paramsQuery, router]);

  /** resetFilters 清空简历筛选条件。 */
  function resetFilters() {
    const reset = { ...DEFAULT_RESUME_FILTERS };
    setKeyword(reset.keyword);
    setSelectedPosition(reset.positionID);
    setPlatformID(reset.platformID);
    setPhoneStatus(reset.phoneStatus);
    setConditionStatus(reset.conditionStatus);
    setSort(reset.sort);
    void showList(1, reset);
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
      const deleted = Number(data.deleted || 0);
      const cleanupFailed = Number(data.cleanup_failed || 0);
      notify(
        cleanupFailed > 0
          ? `已删除 ${deleted} 份简历，不过有 ${cleanupFailed} 个文件没清干净，我先小声记下了`
          : `已删除 ${deleted} 份简历`,
        cleanupFailed > 0 ? "warning" : "success",
      );
      await showList(1);
    } catch (error) {
      notify(error instanceof Error ? error.message : "清空失败", "error");
    }
  }

  /** deleteCandidate 二次确认后删除单个候选人及其全部关联资料。 */
  async function deleteCandidate(candidate: NormalizedCandidate) {
    if (
      !(await confirm(
        "删除简历",
        `我小声确认一下，删除“${candidate.name}”后，简历、附件、沟通记录和 AI 记录都会一起清掉，之后找不回来了。继续吗？`,
      ))
    )
      return;
    setDeletingID(candidate.id);
    try {
      const query = candidate.engagementId
        ? `?engagement_id=${encodeURIComponent(candidate.engagementId)}`
        : "";
      const data = await cloudRequest(
        `/api/candidates/${encodeURIComponent(candidate.id)}${query}`,
        { method: "DELETE" },
      );
      const cleanupFailed = Number(data.cleanup_failed || 0);
      notify(
        cleanupFailed > 0
          ? `简历已删除，不过有 ${cleanupFailed} 个附件文件没清干净，我已经记下了`
          : "简历和相关记录都清掉了",
        cleanupFailed > 0 ? "warning" : "success",
      );
      const nextPage = candidates.length === 1 && page > 1 ? page - 1 : page;
      await showList(nextPage);
    } catch (error) {
      notify(error instanceof Error ? error.message : "这份简历暂时没删成功", "error");
    } finally {
      setDeletingID("");
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
          selectedPosition
            ? "当前显示指定岗位运行产生的简历。"
            : "目前已经默认不生成简历、降低AI消耗，如果需要生成简历，请在岗位设置->高级设置 中开启。"
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
            sm: "repeat(2, minmax(0, 1fr))",
            md: "repeat(4, minmax(0, 1fr))",
          },
          "@media (min-width: 1350px)": {
            gridTemplateColumns:
              "minmax(210px, 1.55fr) minmax(135px, 1fr) minmax(96px, .7fr) minmax(100px, .72fr) minmax(110px, .8fr) minmax(160px, 1.15fr) 72px 64px",
          },
          gap: 1,
          alignItems: "center",
          mb: 1.5,
          "& .MuiOutlinedInput-root": {
            minHeight: 40,
            height: 40,
          },
        }}
      >
        <TextField
          size='small'
          value={keyword}
          onChange={(event) => setKeyword(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === "Enter") void showList(1, activeFilters());
          }}
          placeholder='搜索姓名、岗位、公司或关键词'
          sx={{
            gridColumn: { sm: "span 2", md: "span 1" },
            "@media (min-width: 1350px)": { gridColumn: "span 1" },
          }}
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
          label='岗位'
          value={selectedPosition}
          onChange={(event) => {
            const value = event.target.value;
            const next = { ...activeFilters(), positionID: value };
            setSelectedPosition(value);
            void showList(1, next);
          }}
        >
          <MenuItem value=''>全部岗位</MenuItem>
          {positions.map((item) => (
            <MenuItem key={item.id} value={item.id}>
              {item.name}
            </MenuItem>
          ))}
        </TextField>

        <TextField
          select
          size='small'
          label='平台'
          value={platformID}
          onChange={(event) => {
            const value = event.target.value;
            const next = { ...activeFilters(), platformID: value };
            setPlatformID(value);
            void showList(1, next);
          }}
        >
          <MenuItem value=''>全部平台</MenuItem>
          <MenuItem value='boss'>BOSS直聘</MenuItem>
          <MenuItem value='zhaopin'>智联招聘</MenuItem>
          <MenuItem value='hliepin'>猎聘猎头端</MenuItem>
          <MenuItem value='liepin'>猎聘企业端</MenuItem>
        </TextField>

        <TextField
          select
          size='small'
          label='手机号'
          value={phoneStatus}
          onChange={(event) => {
            const value = event.target.value;
            const next = { ...activeFilters(), phoneStatus: value };
            setPhoneStatus(value);
            void showList(1, next);
          }}
        >
          <MenuItem value='has'>有手机号</MenuItem>
          <MenuItem value='none'>无手机号</MenuItem>
          <MenuItem value='all'>全部</MenuItem>
        </TextField>

        <TextField
          select
          size='small'
          label='条件状态'
          value={conditionStatus}
          onChange={(event) => {
            const value = event.target.value;
            const next = { ...activeFilters(), conditionStatus: value };
            setConditionStatus(value);
            void showList(1, next);
          }}
        >
          <MenuItem value='all_matched'>全部满足</MenuItem>
          <MenuItem value='pending'>待确认</MenuItem>
          <MenuItem value='unmatched'>有未满足</MenuItem>
          <MenuItem value='untracked'>未跟踪</MenuItem>
          <MenuItem value='all'>全部</MenuItem>
        </TextField>

        <TextField
          select
          size='small'
          label='排序'
          value={sort}
          onChange={(event) => {
            const value = event.target.value;
            const next = { ...activeFilters(), sort: value };
            setSort(value);
            void showList(1, next);
          }}
        >
          <MenuItem value='second_score_desc'>第二次分数从高到低</MenuItem>
          <MenuItem value='recent'>最近入库</MenuItem>
        </TextField>

        <Button
          size='small'
          variant='contained'
          disabled={loading}
          onClick={() => void showList(1, activeFilters())}
          sx={{ whiteSpace: "nowrap" }}
        >
          查询
        </Button>
        <Button
          size='small'
          color='secondary'
          onClick={resetFilters}
          sx={{ whiteSpace: "nowrap" }}
        >
          重置
        </Button>
      </Box>
      <SectionPanel sx={{ p: 0, overflow: "hidden" }}>
        {candidates.length ? (
          <>
            <Box
              sx={{
                display: { xs: "none", md: "grid" },
                gridTemplateColumns: "1.05fr 1.25fr .8fr .85fr 92px",
                px: 2,
                py: 1.5,
                bgcolor: "action.hover",
                borderBottom: "1px solid",
                borderColor: "divider",
                "& p": { fontWeight: 800 },
              }}
            >
              <Typography>候选人</Typography>
              <Typography>经历</Typography>
              <Typography>AI分析</Typography>
              <Typography>备注</Typography>
              <Typography>操作</Typography>
            </Box>
            <Stack>
              {candidates.map((item) => (
                <ResumeRow
                  key={`${item.id}-${item.engagementId}`}
                  item={item}
                  onOpenNotes={openNotes}
                  onDelete={deleteCandidate}
                  deleting={deletingID === item.id}
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
            onChange={(_, value) => void showList(value, activeFilters())}
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
  onDelete,
  deleting,
}: {
  item: NormalizedCandidate;
  onOpenNotes: (item: NormalizedCandidate) => void;
  onDelete: (item: NormalizedCandidate) => void;
  deleting: boolean;
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
  const contacts = [
    item.phone ? `手机：${item.phone}` : "",
    item.email ? `邮箱：${item.email}` : "",
    item.wechat ? `微信：${item.wechat}` : "",
  ].filter(Boolean);
  const experiences = [...item.workExperiences, ...item.educations].slice(0, 3);
  return (
    <Box
      sx={{
        display: "grid",
        gridTemplateColumns: {
          xs: "1fr",
          md: "1.05fr 1.25fr .8fr .85fr 92px",
        },
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
          <Avatar src={cloudAssetURL(item.avatarUrl)}>{item.name.slice(0, 1)}</Avatar>
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
            {contacts.map((contact) => (
              <Typography
                key={contact}
                noWrap
                title={contact}
                sx={{ mt: 0.35, color: "text.secondary", fontSize: 12 }}
              >
                {contact}
              </Typography>
            ))}
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
      <NotePreview
        notes={item.notes}
        createdAt={item.createdAt}
        creatorEmail={item.creatorEmail}
        onClick={() => onOpenNotes(item)}
      />
      <Button
        color='error'
        variant='outlined'
        size='small'
        startIcon={<DeleteOutlineRoundedIcon />}
        disabled={deleting}
        onClick={() => onDelete(item)}
        sx={{ justifySelf: { md: "start" } }}
      >
        {deleting ? "删除中" : "删除"}
      </Button>
    </Box>
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
  createdAt,
  creatorEmail,
  onClick,
}: {
  notes: NormalizedNote[];
  createdAt: string;
  creatorEmail: string;
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
        sx={{ mb: 0.6, color: "primary.main", fontSize: 12, fontWeight: 820 }}
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
      {createdAt || creatorEmail ? (
        <Stack
          spacing={0.35}
          sx={{
            mt: 1,
            pt: 0.8,
            borderTop: "1px solid",
            borderColor: "divider",
          }}
        >
          {createdAt ? (
            <Typography sx={{ color: "text.secondary", fontSize: 11 }}>
              创建时间：{formatDate(createdAt)}
            </Typography>
          ) : null}
          {creatorEmail ? (
            <Typography sx={{ color: "text.secondary", fontSize: 11 }}>
              创建人：{creatorEmail}
            </Typography>
          ) : null}
        </Stack>
      ) : null}
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
                  bgcolor: "action.hover",
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
      <Box component='span' sx={{ color: "primary.main", fontWeight: 800 }}>
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
