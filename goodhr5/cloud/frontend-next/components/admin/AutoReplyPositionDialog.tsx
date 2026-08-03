/** 本文件负责岗位自动回复配置、团队公司档案和多条件编辑弹框。 */
"use client";

import AddRoundedIcon from "@mui/icons-material/AddRounded";
import DeleteOutlineRoundedIcon from "@mui/icons-material/DeleteOutlineRounded";
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Divider,
  FormControlLabel,
  MenuItem,
  Stack,
  Switch,
  TextField,
  Typography,
} from "@mui/material";
import { useEffect, useState } from "react";
import {
  deleteCompanyProfile,
  duplicateConditionContent,
  emptyCompanyProfile,
  emptyPositionAutoReplyConfig,
  loadCompanyProfiles,
  loadPositionAutoReplyConfig,
  saveCompanyProfile,
  savePositionAutoReplyConfig,
  type AutoReplyConditionType,
  type CompanyProfile,
  type PositionAutoReplyConfig,
} from "@/lib/auto-reply";
import AdminDialog from "./AdminDialog";

type AutoReplyPositionDialogProps = {
  open: boolean;
  position: { id: string; name: string } | null;
  allowAutoReply: boolean;
  notify: (
    message: string,
    severity?: "success" | "error" | "warning" | "info",
  ) => void;
  confirm: (title: string, message: string) => Promise<boolean>;
  onRequireMax: () => void;
  onClose: () => void;
  onSaved: (enabled: boolean) => void;
};

const conditionTypeOptions: Array<{
  value: AutoReplyConditionType;
  label: string;
}> = [
  { value: "required", label: "必须满足" },
  { value: "confirm", label: "需要确认" },
  { value: "bonus", label: "加分项" },
];

/** AutoReplyPositionDialog 编辑一个岗位的自动回复配置和团队公司资料。 */
export default function AutoReplyPositionDialog({
  open,
  position,
  allowAutoReply,
  notify,
  confirm,
  onRequireMax,
  onClose,
  onSaved,
}: AutoReplyPositionDialogProps) {
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [profileSaving, setProfileSaving] = useState(false);
  const [config, setConfig] = useState<PositionAutoReplyConfig>(
    emptyPositionAutoReplyConfig(""),
  );
  const [profiles, setProfiles] = useState<CompanyProfile[]>([]);
  const [companyForm, setCompanyForm] = useState<CompanyProfile>(
    emptyCompanyProfile(),
  );

  useEffect(() => {
    if (!open || !position?.id) return;
    void load(position.id);
  }, [open, position?.id]);

  /** load 同时读取岗位自动回复配置和团队公司档案。 */
  async function load(positionID: string) {
    setLoading(true);
    try {
      const [nextConfig, nextProfiles] = await Promise.all([
        loadPositionAutoReplyConfig(positionID),
        loadCompanyProfiles(),
      ]);
      setConfig(nextConfig);
      setProfiles(nextProfiles);
      const selected =
        nextProfiles.find(
          (profile) => profile.id === nextConfig.company_profile_id,
        ) || nextProfiles[0];
      setCompanyForm(selected || emptyCompanyProfile());
      if (!nextConfig.company_profile_id && selected) {
        setConfig({ ...nextConfig, company_profile_id: selected.id });
      }
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "自动回复配置没读出来，我先不乱填。",
        "error",
      );
    } finally {
      setLoading(false);
    }
  }

  /** selectCompanyProfile 切换岗位使用的公司档案并载入编辑内容。 */
  function selectCompanyProfile(profileID: string) {
    const selected = profiles.find((profile) => profile.id === profileID);
    setConfig((current) => ({
      ...current,
      company_profile_id: profileID,
    }));
    setCompanyForm(selected || emptyCompanyProfile());
  }

  /** startNewCompanyProfile 清空公司表单并准备新增档案。 */
  function startNewCompanyProfile() {
    setCompanyForm(emptyCompanyProfile());
    setConfig((current) => ({ ...current, company_profile_id: "" }));
  }

  /** persistCompanyProfile 保存当前公司档案并返回服务端正式记录。 */
  async function persistCompanyProfile() {
    if (!companyForm.name.trim()) {
      notify("公司档案至少要有个名字，不然我后面容易认错。", "warning");
      return null;
    }
    setProfileSaving(true);
    try {
      const saved = await saveCompanyProfile(companyForm);
      setProfiles((current) => {
        const exists = current.some((profile) => profile.id === saved.id);
        return exists
          ? current.map((profile) =>
              profile.id === saved.id ? saved : profile,
            )
          : [saved, ...current];
      });
      setCompanyForm(saved);
      setConfig((current) => ({
        ...current,
        company_profile_id: saved.id,
      }));
      notify("公司档案已经记好，团队里都能用。", "success");
      return saved;
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "公司档案没保存成功。",
        "error",
      );
      return null;
    } finally {
      setProfileSaving(false);
    }
  }

  /** removeCompanyProfile 二次确认后删除当前未被岗位使用的公司档案。 */
  async function removeCompanyProfile() {
    if (!companyForm.id) return;
    const approved = await confirm(
      "删除公司档案",
      `公主请确认要删除“${companyForm.name}”吗？正在被岗位使用的话，我会老实拦住。`,
    );
    if (!approved) return;
    setProfileSaving(true);
    try {
      await deleteCompanyProfile(companyForm.id);
      const remaining = profiles.filter(
        (profile) => profile.id !== companyForm.id,
      );
      const next = remaining[0] || emptyCompanyProfile();
      setProfiles(remaining);
      setCompanyForm(next);
      setConfig((current) => ({
        ...current,
        company_profile_id: next.id,
      }));
      notify("公司档案已删除，我没碰别的资料。", "success");
    } catch (error) {
      notify(
        error instanceof Error ? error.message : "公司档案没删除成功。",
        "error",
      );
    } finally {
      setProfileSaving(false);
    }
  }

  /** addCondition 新增一条默认的待确认条件。 */
  function addCondition() {
    setConfig((current) => ({
      ...current,
      conditions: [
        ...current.conditions,
        {
          id: "",
          type: "confirm",
          content: "",
          sort_order: current.conditions.length,
          enabled: true,
        },
      ],
    }));
  }

  /** updateCondition 更新指定序号的岗位条件。 */
  function updateCondition(
    index: number,
    patch: Partial<PositionAutoReplyConfig["conditions"][number]>,
  ) {
    setConfig((current) => ({
      ...current,
      conditions: current.conditions.map((condition, conditionIndex) =>
        conditionIndex === index ? { ...condition, ...patch } : condition,
      ),
    }));
  }

  /** removeCondition 删除指定序号的岗位条件。 */
  function removeCondition(index: number) {
    setConfig((current) => ({
      ...current,
      conditions: current.conditions.filter(
        (_, conditionIndex) => conditionIndex !== index,
      ),
    }));
  }

  /** save 保存公司档案后再原子保存岗位配置和全部条件。 */
  async function save() {
    if (config.enabled && !allowAutoReply) {
      onRequireMax();
      return;
    }
    const emptyCondition = config.conditions.find(
      (condition) => !condition.content.trim(),
    );
    if (emptyCondition) {
      notify("有一条岗位条件还是空的，补上或删掉都行，我不记仇。", "warning");
      return;
    }
    const duplicate = duplicateConditionContent(config.conditions);
    if (duplicate) {
      notify(`岗位条件“${duplicate}”重复了，留一条就够。`, "warning");
      return;
    }
    setSaving(true);
    try {
      let companyProfileID = config.company_profile_id;
      if (companyForm.name.trim()) {
        const savedCompany = await saveCompanyProfile(companyForm);
        companyProfileID = savedCompany.id;
        setCompanyForm(savedCompany);
        setProfiles((current) => {
          const exists = current.some(
            (profile) => profile.id === savedCompany.id,
          );
          return exists
            ? current.map((profile) =>
                profile.id === savedCompany.id ? savedCompany : profile,
              )
            : [savedCompany, ...current];
        });
      }
      if (config.enabled && !companyProfileID) {
        notify("开启自动回复前要先选一份公司档案。", "warning");
        return;
      }
      const saved = await savePositionAutoReplyConfig({
        ...config,
        company_profile_id: companyProfileID,
      });
      setConfig(saved);
      onSaved(saved.enabled);
      notify(
        saved.enabled
          ? "自动回复配置保存好了，我会按这份内容老实回答。"
          : "自动回复配置已保存，开关先保持关闭。",
        "success",
      );
      onClose();
    } catch (error) {
      notify(
        error instanceof Error
          ? error.message
          : "自动回复配置没保存成功，我们再试一次。",
        "error",
      );
    } finally {
      setSaving(false);
    }
  }

  const busy = loading || saving || profileSaving;
  return (
    <AdminDialog
      open={open}
      title={position ? `自动回复 · ${position.name}` : "自动回复设置"}
      description='岗位描述、条件和公司资料会提供给 AI；不确定的事我不会瞎编。'
      confirmText='保存配置'
      loading={saving}
      loadingText='保存中'
      confirmDisabled={loading}
      maxWidth='md'
      onClose={onClose}
      onConfirm={() => void save()}
    >
      {loading ? (
        <Stack spacing={1.25} sx={{ py: 8, alignItems: "center" }}>
          <CircularProgress size={26} />
          <Typography sx={{ color: "text.secondary" }}>
            正在读取自动回复配置
          </Typography>
        </Stack>
      ) : (
        <Stack spacing={2.25}>
          {!allowAutoReply ? (
            <Alert severity='warning'>
              自动回复属于 Max 全能版。你仍然可以查看和关闭旧配置，重新开启需要 Max 或全功能体验会员。
            </Alert>
          ) : null}
          <FormControlLabel
            control={
              <Switch
                checked={config.enabled}
                disabled={busy || (!allowAutoReply && !config.enabled)}
                onChange={(event) => {
                  if (event.target.checked && !allowAutoReply) {
                    onRequireMax();
                    return;
                  }
                  setConfig({ ...config, enabled: event.target.checked });
                }}
              />
            }
            label={config.enabled ? "自动回复已开启" : "自动回复暂未开启"}
          />
          <TextField
            label='自动回复岗位描述'
            value={config.position_description}
            onChange={(event) =>
              setConfig({
                ...config,
                position_description: event.target.value,
              })
            }
            multiline
            minRows={4}
            helperText='只写这个岗位真实、稳定的信息，比如工作内容、薪资范围、工作时间和招聘重点。'
          />
          <TextField
            label='没有简历时发送的话术'
            value={config.resume_request_message}
            onChange={(event) =>
              setConfig({
                ...config,
                resume_request_message: event.target.value,
              })
            }
            multiline
            minRows={2}
            helperText='默认：你好，能发一份简历吗？'
          />

          <Divider />
          <Stack
            direction={{ xs: "column", sm: "row" }}
            sx={{ alignItems: { sm: "center" }, justifyContent: "space-between" }}
          >
            <Box>
              <Typography sx={{ fontWeight: 760 }}>岗位条件</Typography>
              <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
                必须满足权重最高，需要确认会主动追问，加分项只做辅助判断。
              </Typography>
            </Box>
            <Button startIcon={<AddRoundedIcon />} onClick={addCondition}>
              添加条件
            </Button>
          </Stack>
          {config.conditions.length ? (
            <Stack spacing={1.25}>
              {config.conditions.map((condition, index) => (
                <Box
                  key={`${condition.id || "new"}-${index}`}
                  sx={{
                    display: "grid",
                    gridTemplateColumns: {
                      xs: "1fr",
                      md: "150px minmax(0, 1fr) auto auto",
                    },
                    gap: 1,
                    alignItems: "center",
                  }}
                >
                  <TextField
                    select
                    label='类型'
                    value={condition.type}
                    onChange={(event) =>
                      updateCondition(index, {
                        type: event.target.value as AutoReplyConditionType,
                      })
                    }
                  >
                    {conditionTypeOptions.map((option) => (
                      <MenuItem key={option.value} value={option.value}>
                        {option.label}
                      </MenuItem>
                    ))}
                  </TextField>
                  <TextField
                    label={`条件 ${index + 1}`}
                    value={condition.content}
                    onChange={(event) =>
                      updateCondition(index, { content: event.target.value })
                    }
                    placeholder='例如：必须统招本科、需要确认能否接受出差'
                  />
                  <FormControlLabel
                    control={
                      <Checkbox
                        checked={condition.enabled}
                        onChange={(event) =>
                          updateCondition(index, {
                            enabled: event.target.checked,
                          })
                        }
                      />
                    }
                    label='启用'
                    sx={{ mr: 0 }}
                  />
                  <Button
                    color='error'
                    startIcon={<DeleteOutlineRoundedIcon />}
                    onClick={() => removeCondition(index)}
                  >
                    删除
                  </Button>
                </Box>
              ))}
            </Stack>
          ) : (
            <Alert severity='info'>
              这里暂时空空的。没有条件也能保存，只是我后面会更依赖岗位描述。
            </Alert>
          )}

          <Divider />
          <Stack
            direction={{ xs: "column", sm: "row" }}
            spacing={1}
            sx={{ alignItems: { sm: "center" }, justifyContent: "space-between" }}
          >
            <Box>
              <Typography sx={{ fontWeight: 760 }}>团队公司档案</Typography>
              <Typography sx={{ color: "text.secondary", fontSize: 13 }}>
                同一份公司资料可以给多个岗位复用，团队成员都能编辑。
              </Typography>
            </Box>
            <Button startIcon={<AddRoundedIcon />} onClick={startNewCompanyProfile}>
              新建档案
            </Button>
          </Stack>
          {profiles.length ? (
            <TextField
              select
              label='当前岗位使用的公司档案'
              value={config.company_profile_id}
              onChange={(event) => selectCompanyProfile(event.target.value)}
            >
              {profiles.map((profile) => (
                <MenuItem key={profile.id} value={profile.id}>
                  {profile.name}
                </MenuItem>
              ))}
            </TextField>
          ) : null}
          <Box
            sx={{
              display: "grid",
              gridTemplateColumns: { xs: "1fr", md: "1fr 1fr" },
              gap: 1.25,
            }}
          >
            <TextField
              label='公司档案名称'
              value={companyForm.name}
              onChange={(event) =>
                setCompanyForm({ ...companyForm, name: event.target.value })
              }
              placeholder='例如：德阳总部'
            />
            <TextField
              label='公司地址'
              value={companyForm.address}
              onChange={(event) =>
                setCompanyForm({ ...companyForm, address: event.target.value })
              }
            />
            <TextField
              label='联系方式'
              value={companyForm.contact}
              onChange={(event) =>
                setCompanyForm({ ...companyForm, contact: event.target.value })
              }
            />
            <TextField
              label='公司概况'
              value={companyForm.overview}
              onChange={(event) =>
                setCompanyForm({ ...companyForm, overview: event.target.value })
              }
              multiline
              minRows={3}
            />
            <TextField
              label='其他公司信息'
              value={companyForm.extra_info}
              onChange={(event) =>
                setCompanyForm({ ...companyForm, extra_info: event.target.value })
              }
              multiline
              minRows={3}
              sx={{ gridColumn: { md: "1 / -1" } }}
            />
          </Box>
          <Stack direction='row' spacing={1} sx={{ flexWrap: "wrap" }}>
            <Button
              variant='outlined'
              disabled={profileSaving}
              onClick={() => void persistCompanyProfile()}
            >
              {profileSaving ? "保存中" : "单独保存公司档案"}
            </Button>
            {companyForm.id ? (
              <Button
                color='error'
                disabled={profileSaving}
                startIcon={<DeleteOutlineRoundedIcon />}
                onClick={() => void removeCompanyProfile()}
              >
                删除这份档案
              </Button>
            ) : null}
          </Stack>
        </Stack>
      )}
    </AdminDialog>
  );
}
