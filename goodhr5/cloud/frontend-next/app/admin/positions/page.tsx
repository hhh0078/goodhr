/** 本文件负责岗位模板完整的新增、编辑、会员校验、模式联动和提示词管理。 */
"use client";

import AddRoundedIcon from "@mui/icons-material/AddRounded";
import AutoFixHighRoundedIcon from "@mui/icons-material/AutoFixHighRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import EditRoundedIcon from "@mui/icons-material/EditRounded";
import RestartAltRoundedIcon from "@mui/icons-material/RestartAltRounded";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  Divider,
  Stack,
  TextField,
  Typography,
} from "@mui/material";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import AdminDialog from "@/components/admin/AdminDialog";
import ChoiceCards from "@/components/admin/ChoiceCards";
import {
  EmptyState,
  PageHeader,
  RefreshButton,
  SectionPanel,
} from "@/components/admin/AdminUI";
import { useAdmin } from "@/components/admin/AdminApp";
import PlatformLogo, {
  platformIconSrc,
  platformLabel,
} from "@/components/admin/PlatformLogo";
import { cloudRequest } from "@/lib/admin-api";

type PositionForm = ReturnType<typeof createEmptyForm>;

/** PositionsPage 管理岗位筛选、详情识别和 AI 提示词配置。 */
export default function PositionsPage() {
  const router = useRouter();
  const { subscription, notify, confirm } = useAdmin();
  const [items, setItems] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [optimizing, setOptimizing] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [form, setForm] = useState<PositionForm>(createEmptyForm());
  const [defaults, setDefaults] = useState({
    filter_prompt: "",
    open_detail_prompt: "",
    review_prompt: "",
  });

  /** load 读取岗位模板和系统默认提示词。 */
  async function load() {
    setLoading(true);
    try {
      const [positions, prompts] = await Promise.all([
        cloudRequest("/api/positions"),
        cloudRequest("/api/system/default-prompts"),
      ]);
      setItems(positions.positions || []);
      setDefaults(normalizePrompts(prompts.prompts || prompts || {}));
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "岗位模板读取失败",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  /** openCreate 使用免费版可用配置打开新增弹框。 */
  function openCreate() {
    setForm(fillPrompts(createEmptyForm(), defaults));
    setDialogOpen(true);
  }

  /** openEdit 将岗位完整字段写入弹框并校验会员功能。 */
  async function openEdit(item: any) {
    const next = formFromItem(item, defaults);
    if (
      !subscription.active &&
      (next.mode_default === "ai" || next.detail_mode === "ai")
    ) {
      const go = await confirm(
        "会员功能",
        "该岗位使用了 AI 筛选或 AI 详情识别。当前会员已到期，是否前往订阅页面？",
      );
      if (go) router.push("/admin/subscription");
    }
    setForm(next);
    setDialogOpen(true);
  }

  /** save 保存岗位模板并保留旧后端所需字段结构。 */
  async function save() {
    if (!form.name.trim()) return notify("请填写岗位名称", "warning");
    if (
      !subscription.active &&
      (form.mode_default === "ai" || form.detail_mode === "ai")
    )
      return requireMembership();
    setLoading(true);
    try {
      await cloudRequest("/api/positions", {
        method: "POST",
        body: {
          id: form.id,
          platform_id: form.platform_id,
          name: form.name.trim(),
          keywords: splitKeywords(form.keywords),
          exclude_keywords: splitKeywords(form.exclude_keywords),
          description: form.description.trim(),
          greet_message: form.greet_message.trim(),
          is_and_mode: form.is_and_mode,
          common_config: {
            mode_default: form.mode_default,
            detail_mode: normalizeDetailMode(
              form.platform_id,
              form.detail_mode,
            ),
          },
          ai_config: {
            position_requirement: form.position_requirement,
            filter_prompt: form.filter_prompt || defaults.filter_prompt,
            greet_prompt: form.filter_prompt || defaults.filter_prompt,
            click_prompt: form.filter_prompt || defaults.filter_prompt,
            open_detail_prompt:
              form.open_detail_prompt || defaults.open_detail_prompt,
            review_prompt: normalizePrompt(form.review_prompt),
            detail_score_threshold: Number(form.detail_score_threshold || 60),
            greet_score_threshold: Number(form.greet_score_threshold || 70),
          },
          keyword_config: {},
        },
      });
      notify(form.id ? "岗位模板已更新" : "岗位模板已创建", "success");
      setDialogOpen(false);
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "保存岗位失败", "error");
    } finally {
      setLoading(false);
    }
  }

  /** remove 删除指定岗位模板。 */
  async function remove(item: any) {
    if (!(await confirm("删除岗位模板", `确认删除“${item.name}”吗？`))) return;
    try {
      await cloudRequest(`/api/positions/${item.id}`, { method: "DELETE" });
      notify("岗位模板已删除", "success");
      await load();
    } catch (error) {
      notify(error instanceof Error ? error.message : "删除失败", "error");
    }
  }

  /** optimizeRequirement 调用用户 AI 配置整理岗位要求。 */
  async function optimizeRequirement() {
    if (!form.position_requirement.trim())
      return notify("请先填写岗位要求", "warning");
    setOptimizing(true);
    try {
      const data = await cloudRequest("/api/positions/optimize-requirement", {
        method: "POST",
        body: { text: form.position_requirement },
      });
      setForm((current) => ({
        ...current,
        position_requirement:
          data.optimized || data.text || current.position_requirement,
      }));
      notify("岗位要求已优化", "success");
    } catch (error) {
      notify(error instanceof Error ? error.message : "AI 优化失败", "error");
    } finally {
      setOptimizing(false);
    }
  }

  /** selectMode 选择筛选模式并执行会员提醒。 */
  async function selectMode(value: string) {
    if (value === "ai" && !subscription.active) return requireMembership();
    setForm((current) => ({ ...current, mode_default: value }));
  }

  /** selectDetailMode 选择详情模式并执行平台与会员联动。 */
  async function selectDetailMode(value: string) {
    if (form.platform_id === "boss" && value === "dom")
      return notify("Boss直聘不支持 DOM 详情识别", "warning");
    if (isDOMOnlyPlatform(form.platform_id) && value !== "dom")
      return notify(`${platformLabel(form.platform_id)}只能用 DOM 详情识别`, "warning");
    if (value === "ai" && !subscription.active) return requireMembership();
    setForm((current) => ({ ...current, detail_mode: value }));
  }

  /** selectPlatform 切换平台并修正平台不支持的详情模式。 */
  function selectPlatform(value: string) {
    setForm((current) => ({
      ...current,
      platform_id: value,
      detail_mode: normalizeDetailMode(value, current.detail_mode),
    }));
  }

  /** requireMembership 引导免费用户前往订阅页面。 */
  async function requireMembership() {
    const go = await confirm(
      "该功能需要订阅会员",
      "AI 筛选和 AI 详情识别属于会员功能，是否前往订阅页面？",
    );
    if (go) router.push("/admin/subscription");
  }

  return (
    <>
      <PageHeader
        title='岗位管理'
        description='岗位模板决定首次筛选、详情识别和最终打招呼判断。'
        actions={
          <>
            <Button
              variant='contained'
              startIcon={<AddRoundedIcon />}
              disabled={loading}
              onClick={openCreate}
            >
              新建岗位
            </Button>
            <RefreshButton loading={loading} onClick={() => void load()} />
          </>
        }
      />
      <SectionPanel>
        {items.length ? (
          <Stack>
            {items.map((item) => (
              <Stack
                key={item.id}
                direction={{ xs: "column", md: "row" }}
                spacing={2}
                sx={{
                  py: 2,
                  borderBottom: "1px solid",
                  borderColor: "divider",
                  alignItems: { md: "center" },
                }}
              >
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography sx={{ fontWeight: 760 }}>{item.name}</Typography>
                  <Stack
                    direction='row'
                    spacing={0.75}
                    sx={{
                      mt: 0.5,
                      alignItems: "center",
                      color: "text.secondary",
                      flexWrap: "wrap",
                    }}
                  >
                    <PlatformLogo platformID={item.platform_id} size={20} />
                    <Typography sx={{ fontSize: 13 }}>
                      {platformLabel(item.platform_id)} ·{" "}
                      {item.common_config?.mode_default === "ai"
                        ? "AI 筛选"
                        : "关键词筛选"}{" "}
                      · 详情：
                      {detailModeLabel(item.common_config?.detail_mode)} ·
                      关键词：{(item.keywords || []).join(" / ") || "无"}
                    </Typography>
                  </Stack>
                </Box>
                <Stack direction='row' spacing={1}>
                  <Button
                    startIcon={<EditRoundedIcon />}
                    onClick={() => void openEdit(item)}
                  >
                    编辑
                  </Button>
                  <Button
                    color='error'
                    startIcon={<DeleteOutlineRoundedIcon />}
                    onClick={() => void remove(item)}
                  >
                    删除
                  </Button>
                </Stack>
              </Stack>
            ))}
          </Stack>
        ) : (
          <EmptyState text='暂无岗位模板' />
        )}
      </SectionPanel>
      <AdminDialog
        open={dialogOpen}
        title={form.id ? "编辑岗位模板" : "新建岗位模板"}
        description='按运行顺序填写。只有当前选择模式需要的字段会显示。'
        maxWidth='md'
        confirmText={form.id ? "保存修改" : "创建岗位"}
        loading={loading}
        confirmDisabled={!form.name.trim()}
        onClose={() => setDialogOpen(false)}
        onConfirm={() => void save()}
      >
        <Stack spacing={3}>
          <Alert severity='info' variant='outlined'>
            运行时先读取候选人基础信息，完成第一次筛选并决定是否打开详情；读取详情后再进行第二次分析，决定是否打招呼。请按这个顺序配置下面的内容。
          </Alert>
          <Box>
            <Typography
              component='h3'
              sx={{ mb: 1.5, fontSize: 17, fontWeight: 780 }}
            >
              基础信息
            </Typography>
            <TextField
              label='岗位名称'
              value={form.name}
              onChange={(event) =>
                setForm({ ...form, name: event.target.value })
              }
              fullWidth
              placeholder='例如：服装带货主播'
              helperText='建议与招聘平台中的岗位名称保持一致，任务会根据名称自动切换岗位。'
            />
          </Box>
          <ChoiceCards
            label='招聘平台'
            value={form.platform_id}
            columns={3}
            onChange={(value) => selectPlatform(String(value))}
            options={[
              {
                value: "boss",
                label: "Boss直聘",
                description: "支持 OCR 和 AI 详情识别。",
                iconSrc: platformIconSrc("boss"),
              },
            {
              value: "zhaopin",
              label: "智联招聘",
              description: "只支持 DOM 详情识别。",
              iconSrc: platformIconSrc("zhaopin"),
            },
            {
              value: "hliepin",
              label: "猎聘猎头端",
              description: "只支持 DOM 详情识别。",
              iconSrc: platformIconSrc("hliepin"),
            },
            {
              value: "liepin",
              label: "猎聘企业端",
              description: "只支持 DOM 详情识别。",
              iconSrc: platformIconSrc("liepin"),
            },
            ]}
          />
          <ChoiceCards
            label='基础信息筛选模式   (决定是否打开查看详情)'
            value={form.mode_default}
            onChange={(value) => void selectMode(String(value))}
            options={[
              {
                value: "keyword",
                label: "关键词筛选",
                description: "按关键词和排除词判断，永久免费且速度快。",
              },
              {
                value: "ai",
                label: "AI 筛选（会员功能）",
                description: "AI 先根据基础信息判断是否值得打开详情。",
                memberOnly: true,
              },
            ]}
          />
          <Typography sx={{ mt: -2, color: "text.secondary", fontSize: 13 }}>
            选择哪种详情方式就只使用哪一种：DOM 最快，OCR 在本地识别截图文字，AI
            能理解完整页面但耗时更长。
          </Typography>
          <ChoiceCards
            label='详情信息筛选模式  (决定是否打招呼)'
            value={form.detail_mode}
            columns={3}
            onChange={(value) => void selectDetailMode(String(value))}
            options={[
          {
            value: "dom",
            label: "DOM 识别",
            description: "速度最快，适合可直接读取文字的平台。",
            disabled: form.platform_id === "boss",
          },
          {
            value: "ocr",
            label: "OCR 识别",
            description: "离线识别截图文字，速度快。电脑配置低就别选这个。",
            disabled: isDOMOnlyPlatform(form.platform_id),
          },
          {
            value: "ai",
            label: "AI 识别（会员功能）",
            description: "直接理解完整详情截图，效果最好但更慢。",
            disabled: isDOMOnlyPlatform(form.platform_id),
            memberOnly: true,
          },
            ]}
          />
          {form.mode_default === "keyword" ? (
            <>
              <Divider />
              <Box>
                <Typography
                  component='h3'
                  sx={{ mb: 1.5, fontSize: 17, fontWeight: 780 }}
                >
                  关键词筛选
                </Typography>
                <Stack spacing={2}>
                  <ChoiceCards
                    label='匹配方式'
                    value={form.is_and_mode}
                    onChange={(value) =>
                      setForm({ ...form, is_and_mode: Boolean(value) })
                    }
                    options={[
                      {
                        value: false,
                        label: "满足任一关键词",
                        description: "命中一个关键词即可通过，适合放宽筛选。",
                      },
                      {
                        value: true,
                        label: "必须同时满足",
                        description: "需要命中全部关键词，适合严格筛选。",
                      },
                    ]}
                  />
                  <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
                    关键词模式是否打开详情，由“个人配置”中的详情查看概率控制。满足任一关键词更宽松，必须同时满足则更严格。
                  </Typography>
                  <Box
                    sx={{
                      display: "grid",
                      gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                      gap: 2,
                    }}
                  >
                    <TextField
                      label='关键词'
                      value={form.keywords}
                      onChange={(event) =>
                        setForm({ ...form, keywords: event.target.value })
                      }
                      multiline
                      minRows={3}
                      helperText='支持空格、中文逗号、英文逗号或换行分隔。'
                    />
                    <TextField
                      label='排除词'
                      value={form.exclude_keywords}
                      onChange={(event) =>
                        setForm({
                          ...form,
                          exclude_keywords: event.target.value,
                        })
                      }
                      multiline
                      minRows={3}
                      helperText='命中排除词后直接跳过。'
                    />
                  </Box>
                </Stack>
              </Box>
            </>
          ) : null}
          {form.mode_default === "ai" || form.detail_mode === "ai" ? (
            <>
              <Divider />
              <Box>
                <Stack
                  direction={{ xs: "column", sm: "row" }}
                  sx={{ mb: 1.5, justifyContent: "space-between", gap: 1 }}
                >
                  <Box>
                    <Typography
                      component='h3'
                      sx={{ fontSize: 17, fontWeight: 780 }}
                    >
                      AI 配置
                    </Typography>
                    <Typography
                      sx={{ mt: 0.5, color: "text.secondary", fontSize: 13 }}
                    >
                      请将JD岗位要求复制到“岗位要求”中，点击“AI
                      优化岗位要求”按钮，AI会自动优化。
                    </Typography>
                  </Box>
                  <Button
                    startIcon={
                      optimizing ? (
                        <CircularProgress size={16} color='inherit' />
                      ) : (
                        <AutoFixHighRoundedIcon />
                      )
                    }
                    disabled={optimizing || !form.position_requirement.trim()}
                    onClick={() => void optimizeRequirement()}
                  >
                    {optimizing ? "优化中..." : "AI 优化岗位要求"}
                  </Button>
                </Stack>
                <Stack spacing={2}>
                  <TextField
                    label='岗位要求'
                    value={form.position_requirement}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        position_requirement: event.target.value,
                      })
                    }
                    multiline
                    fullWidth
                    placeholder='必须有3年以上教学经验，必须有教师资格证，学历年龄 等基础条件可以在平台提前筛选好，更不要写跟岗位要求无关的 比如 岗位福利，工作环境等。'
                    minRows={7}
                    helperText='建议写清学历、经验、技能、行业、城市、到岗状态和明确的淘汰条件；不要填写“有上进心”等无法从简历判断的内容。'
                  />
                  <Box
                    sx={{
                      p: 1.5,
                      borderLeft: "3px solid",
                      borderColor: "primary.main",
                      bgcolor: "#f5f8f6",
                    }}
                  >
                    <Typography sx={{ fontSize: 13, fontWeight: 760 }}>
                      岗位要求示例
                    </Typography>
                    <Typography
                      sx={{
                        mt: 0.5,
                        color: "text.secondary",
                        fontSize: 13,
                        lineHeight: 1.75,
                      }}
                    >
                      求职意向必须匹配目标岗位；具备 3
                      年以上相关经验；拥有岗位要求的证书或技能；当前状态满足到岗要求。薪资越高或岗位越重要，条件应写得越明确。
                    </Typography>
                  </Box>
                  <PromptField
                    label='打开详情提示词'
                    value={form.open_detail_prompt}
                    defaultValue={defaults.open_detail_prompt}
                    description='只用于第一次分析，判断候选人是否值得打开详情。普通岗位可以宽松一些，高级岗位可以更严格。'
                    onChange={(value) =>
                      setForm({ ...form, open_detail_prompt: value })
                    }
                  />
                  <TextField
                    label='看详情阈值分'
                    type='number'
                    value={form.detail_score_threshold}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        detail_score_threshold: Number(event.target.value),
                      })
                    }
                    slotProps={{ htmlInput: { min: 0, max: 100 } }}
                    helperText='首次评分大于等于该值时打开候选人详情。'
                  />
                  <PromptField
                    label='打招呼提示词'
                    value={form.filter_prompt}
                    defaultValue={defaults.filter_prompt}
                    description='用于详情分析并决定候选人的最终分数，直接影响是否执行打招呼。'
                    onChange={(value) =>
                      setForm({ ...form, filter_prompt: value })
                    }
                  />
                  <TextField
                    label='打招呼阈值分'
                    type='number'
                    value={form.greet_score_threshold}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        greet_score_threshold: Number(event.target.value),
                      })
                    }
                    slotProps={{ htmlInput: { min: 0, max: 100 } }}
                    helperText='详情评分大于等于该值时执行打招呼。'
                  />
                  <PromptField
                    label='复核提示词（可选）'
                    value={form.review_prompt}
                    defaultValue=''
                    defaultActionLabel='清空'
                    emptyPlaceholder='可留空，不填写则不会触发复核'
                    description='当详情分数接近打招呼阈值时执行二次复核；留空则不会触发复核。'
                    onChange={(value) =>
                      setForm({ ...form, review_prompt: value })
                    }
                  />
                </Stack>
              </Box>
            </>
          ) : null}
          <Divider />
          <Box>
            <Typography
              component='h3'
              sx={{ mb: 1.5, fontSize: 17, fontWeight: 780 }}
            >
              可选信息
            </Typography>
            <Box
              sx={{
                display: "grid",
                gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
                gap: 2,
              }}
            >
              <TextField
                label='问候语'
                value={form.greet_message}
                onChange={(event) =>
                  setForm({ ...form, greet_message: event.target.value })
                }
                multiline
                minRows={3}
              />
              <TextField
                label='岗位描述'
                value={form.description}
                onChange={(event) =>
                  setForm({ ...form, description: event.target.value })
                }
                multiline
                minRows={3}
              />
            </Box>
          </Box>
          {!subscription.active &&
          (form.mode_default === "ai" || form.detail_mode === "ai") ? (
            <Alert severity='warning'>
              当前会员已到期，AI 选项无法保存。可以改为关键词筛选和 OCR 识别。
            </Alert>
          ) : null}
        </Stack>
      </AdminDialog>
    </>
  );
}

/** PromptField 输出带恢复系统默认按钮的提示词输入框。 */
function PromptField({
  label,
  value,
  defaultValue,
  defaultActionLabel = "设为系统默认",
  emptyPlaceholder = "系统暂未配置默认提示词",
  description,
  onChange,
}: {
  label: string;
  value: string;
  defaultValue: string;
  defaultActionLabel?: string;
  emptyPlaceholder?: string;
  description: string;
  onChange: (value: string) => void;
}) {
  return (
    <Box>
      <Stack
        direction='row'
        sx={{ mb: 0.75, justifyContent: "space-between", alignItems: "center" }}
      >
        <Typography sx={{ fontSize: 13, fontWeight: 700 }}>{label}</Typography>
        <Button
          size='small'
          startIcon={<RestartAltRoundedIcon />}
          onClick={() => onChange(defaultValue)}
        >
          {defaultActionLabel}
        </Button>
      </Stack>
      <TextField
        value={value}
        onChange={(event) => onChange(event.target.value)}
        multiline
        minRows={6}
        fullWidth
        placeholder={defaultValue ? "已加载系统默认提示词" : emptyPlaceholder}
      />
      <Typography
        sx={{
          mt: 0.75,
          color: "text.secondary",
          fontSize: 12.5,
          lineHeight: 1.6,
        }}
      >
        {description}
      </Typography>
    </Box>
  );
}

/** createEmptyForm 返回免费版可用的岗位默认表单。 */
function createEmptyForm() {
  return {
    id: "",
    name: "",
    platform_id: "boss",
    mode_default: "keyword",
    detail_mode: "ocr",
    keywords: "",
    exclude_keywords: "",
    is_and_mode: false,
    position_requirement: "",
    open_detail_prompt: "",
    filter_prompt: "",
    review_prompt: "",
    detail_score_threshold: 60,
    greet_score_threshold: 70,
    greet_message: "",
    description: "",
  };
}

/** formFromItem 将后端岗位数据转换为编辑表单。 */
function formFromItem(
  item: any,
  defaults: ReturnType<typeof normalizePrompts>,
): PositionForm {
  const common = item.common_config || {};
  const ai = item.ai_config || {};
  return fillPrompts(
    {
      id: item.id || "",
      name: item.name || "",
      platform_id: item.platform_id || "boss",
      mode_default: common.mode_default || "keyword",
      detail_mode: normalizeDetailMode(
        item.platform_id,
        common.detail_mode || "ocr",
      ),
      keywords: (item.keywords || []).join(" "),
      exclude_keywords: (item.exclude_keywords || []).join(" "),
      is_and_mode: Boolean(item.is_and_mode),
      position_requirement: ai.position_requirement || "",
      open_detail_prompt: normalizePrompt(ai.open_detail_prompt),
      filter_prompt: normalizePrompt(
        ai.greet_prompt || ai.filter_prompt || ai.click_prompt,
      ),
      review_prompt: normalizePrompt(ai.review_prompt),
      detail_score_threshold: Number(ai.detail_score_threshold ?? 60),
      greet_score_threshold: Number(ai.greet_score_threshold ?? 70),
      greet_message: item.greet_message || "",
      description: item.description || "",
    },
    defaults,
  );
}

/** fillPrompts 为岗位空提示词补充系统默认值。 */
function fillPrompts(
  form: PositionForm,
  defaults: ReturnType<typeof normalizePrompts>,
) {
  return {
    ...form,
    open_detail_prompt: form.open_detail_prompt || defaults.open_detail_prompt,
    filter_prompt: form.filter_prompt || defaults.filter_prompt,
    review_prompt: form.review_prompt || "",
  };
}

/** normalizePrompts 统一系统默认提示词字段。 */
function normalizePrompts(value: any) {
  return {
    filter_prompt: normalizePrompt(value?.filter_prompt),
    open_detail_prompt: normalizePrompt(value?.open_detail_prompt),
    review_prompt: normalizePrompt(value?.review_prompt),
  };
}

/** normalizePrompt 还原历史数据中的字面换行。 */
function normalizePrompt(value: unknown) {
  return String(value || "").replace(/\\n/g, "\n");
}

/** normalizeDetailMode 修正平台不支持的详情模式。 */
function normalizeDetailMode(platformID: string, mode: string) {
  if (isDOMOnlyPlatform(platformID)) return "dom";
  if (platformID === "boss" && mode === "dom") return "ocr";
  return ["dom", "ocr", "ai"].includes(mode) ? mode : "ocr";
}

/** isDOMOnlyPlatform 判断平台是否只支持 DOM 详情识别。 */
function isDOMOnlyPlatform(platformID: string) {
  return ["hliepin", "liepin", "zhaopin"].includes(platformID);
}

/** splitKeywords 将多种分隔符转换成忽略大小写的去重关键词数组。 */
function splitKeywords(value: string) {
  const seen = new Set<string>();
  return String(value || "")
    .split(/[,\s，、；;]+/)
    .map((item) => item.trim())
    .filter((item) => {
      const key = item.toLowerCase();
      if (!item || seen.has(key)) return false;
      seen.add(key);
      return true;
    });
}

/** detailModeLabel 返回详情模式中文名称。 */
function detailModeLabel(value: string) {
  return value === "dom" ? "DOM识别" : value === "ai" ? "AI识别" : "OCR识别";
}
