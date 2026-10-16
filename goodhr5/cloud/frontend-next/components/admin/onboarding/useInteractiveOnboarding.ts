/** 本文件编排跨页面新手引导，复用实际登录、安装和岗位状态；仅微步骤保存在本机。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { clearLocalAgentDetectCache, cloudRequest, localRequest } from "@/lib/admin-api";
import { hasMissingRequiredRuntime, requiredRuntimeComponents } from "@/lib/admin-runtime";
import { pickPlatformAuthConfig, waitForPlatformLoggedIn } from "@/lib/platform-login";
import { isPlatformOpen, type PlatformConfigLike } from "@/lib/platform-open";
import { loadUserFlow } from "@/lib/user-flow";
import { publishOnboardingEvent, setOnboardingGuideContext, subscribeOnboardingEvents, type OnboardingEvent, type OnboardingScreeningResult } from "@/lib/onboarding-events";
import { createOnboardingState, restartOnboardingState, needsPositionRequirement, nextOnboardingFormStep, normalizeOnboardingPosition, onboardingDoneCount, onboardingRemainingTime, readOnboardingState, saveOnboardingState, type OnboardingState, type OnboardingStep } from "@/lib/onboarding-state";
import { clearOnboardingPositionDraft } from "@/lib/onboarding-position-draft";
import { onboardingStepCopy, type OnboardingCelebration, type OnboardingExperienceProps, type OnboardingPhase } from "@/lib/onboarding-view";
import { onboardingTargetSelector, useOnboardingTarget } from "./useOnboardingTarget";

export type OnboardingActivity = { engaged: boolean; awaitingChoice: boolean };
export type InteractiveOnboardingOptions = {
  identity: string;
  agentBase: string;
  agentDetected: boolean;
  refreshAgent: () => Promise<void>;
  blocked: boolean;
  onActivityChange: (activity: OnboardingActivity) => void;
};

/** useInteractiveOnboarding 根据真实成功事件和页面状态推进教程，按钮说明不冒充业务完成。 */
export function useInteractiveOnboarding(options: InteractiveOnboardingOptions): OnboardingExperienceProps {
  const { identity, agentBase, agentDetected, refreshAgent, blocked, onActivityChange } = options;
  const pathname = usePathname();
  const router = useRouter();
  const [state, setState] = useState<OnboardingState>(createOnboardingState);
  const stateRef = useRef(state);
  const [phase, setPhase] = useState<OnboardingPhase>("loading");
  const phaseRef = useRef(phase);
  const [environmentChecked, setEnvironmentChecked] = useState(false);
  const [connectionAlive, setConnectionAlive] = useState(Boolean(agentBase));
  const [runtimeReady, setRuntimeReady] = useState(false);
  const [systemRuntimeMissing, setSystemRuntimeMissing] = useState(false);
  const [systemDownloadAvailable, setSystemDownloadAvailable] = useState(true);
  const [platformConfigs, setPlatformConfigs] = useState<PlatformConfigLike[]>([]);
  const [platformsLoading, setPlatformsLoading] = useState(true);
  const platformConfigsRef = useRef<PlatformConfigLike[]>([]);
  const platformReadRef = useRef<Promise<PlatformConfigLike[] | null> | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [startOpen, setStartOpen] = useState(false);
  const [platformOpenGeneration, setPlatformOpenGeneration] = useState(0);
  const [startDialogStatus, setStartDialogStatus] = useState<"ready" | "opening" | "checking" | "error" | "update">("ready");
  const [taskStopped, setTaskStopped] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [restartOpen, setRestartOpen] = useState(false);
  const [formSaving, setFormSaving] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [statusText, setStatusText] = useState("");
  const [storageUnavailable, setStorageUnavailable] = useState(false);
  const [celebration, setCelebration] = useState<OnboardingCelebration | null>(null);
  const [cloudConfirmed, setCloudConfirmed] = useState(false);
  const aliveRef = useRef(true);
  const pendingStartBackRef = useRef("");
  const pendingRecoveryRef = useRef("");
  const loginRequestRef = useRef<AbortController | null>(null);
  const environmentRef = useRef(false);
  const phaseBeforeSkip = useRef<OnboardingPhase>("invite");
  const sessionEpochRef = useRef(0);

  /** changePhase 立即更新运行保护，防止同一轮异步响应在暂停或换号后继续推进。 */
  const changePhase = useCallback((next: OnboardingPhase) => {
    if (next !== "active") pendingStartBackRef.current = "";
    phaseRef.current = next; setPhase(next);
  }, []);
  /** update 合并本机微步骤，真实业务字段只由事件或状态检查写入。 */
  const update = useCallback((transform: (current: OnboardingState) => OnboardingState) => {
    const next = { ...transform(stateRef.current), updatedAt: Date.now() };
    stateRef.current = next; setState(next);
  }, []);
  /** celebrate 每个真实里程碑只鼓励一次，刷新或回看不会重复放礼炮。 */
  const celebrate = useCallback((id: string, title: string, description: string) => {
    if (stateRef.current.celebrated.includes(id)) return;
    update((current) => ({ ...current, celebrated: [...current.celebrated, id] }));
    setCelebration({ id, title, description });
  }, [update]);

  /** returnToPlatformChoice 保留实际岗位数据，平台关闭时回到岗位创建入口重新选择，不自动改动原岗位。 */
  const returnToPlatformChoice = useCallback((message: string) => {
    loginRequestRef.current?.abort();
    publishOnboardingEvent({ type: "position-form-close-requested" });
    publishOnboardingEvent({ type: "position-start-close-requested" });
    update((value) => ({ ...value, step: "position-create", platformLoggedIn: false, positionStarted: false, completedFields: [], completionKind: undefined }));
    setError(message); setStatusText("");
  }, [update]);

  /** readPlatformAvailability 复用现有平台开放配置，合并重复读取并允许失败后重新检查。 */
  const readPlatformAvailability = useCallback((): Promise<PlatformConfigLike[] | null> => {
    if (platformReadRef.current) return platformReadRef.current;
    setPlatformsLoading(true);
    const request = (async () => {
      try {
        const data = await cloudRequest("/api/platforms/config/");
        const source = data.configs || data.configurations;
        const configs: PlatformConfigLike[] = Array.isArray(source) ? source : [];
        if (!aliveRef.current) return null;
        platformConfigsRef.current = configs; setPlatformConfigs(configs);
        const current = stateRef.current;
        if (phaseRef.current === "active" && current.positionId && !isPlatformOpen(configs, current.platformId) && ["platform-open", "platform-login", "position-start", "platform-filter", "position-run"].includes(current.step)) {
          returnToPlatformChoice("上次选择的招聘平台暂未开放，请重新选择可用平台。已经保存的岗位会保留，我不会替你换平台。");
        }
        return configs;
      } catch {
        if (aliveRef.current) setError("招聘平台的开放状态暂时没读到。请刷新页面后再试一次，填写的岗位草稿会留着。");
        return null;
      } finally {
        platformReadRef.current = null;
        if (aliveRef.current) setPlatformsLoading(false);
      }
    })();
    platformReadRef.current = request;
    return request;
  }, [returnToPlatformChoice]);

  useEffect(() => {
    aliveRef.current = true;
    sessionEpochRef.current += 1;
    setHelpOpen(false); setRestartOpen(false); setFormSaving(false);
    const cached = readOnboardingState(identity);
    stateRef.current = cached; setState(cached);
    let active = true;
    /** initialize 先读数据库完成事实，再恢复当前账号缓存；不能因缓存缺失强迫老用户再学。 */
    async function initialize() {
      try {
        const [flowRequest, runtimeRequest] = await Promise.allSettled([
          loadUserFlow(),
          agentBase ? localRequest(agentBase, "/api/v1/runtime/status", { timeoutMS: 3500 }) : Promise.resolve(null),
        ]);
        if (flowRequest.status === "rejected") throw flowRequest.reason;
        const flow = flowRequest.value;
        if (!active) return;
        if (flow?.tutorial_completed === true || flow?.state === "completed") {
          setCloudConfirmed(true);
          if (!cached.replaying) { changePhase("silent"); return; }
        }
        if (runtimeRequest.status === "fulfilled" && runtimeRequest.value && !hasMissingRequiredRuntime(runtimeRequest.value)) {
          setConnectionAlive(true); setRuntimeReady(true); setEnvironmentChecked(true); environmentRef.current = true;
          update((current) => ({ ...current, environmentReady: true, step: current.step.startsWith("agent-") || current.step.startsWith("runtime-") ? current.positionId ? current.reviewPosition ? "position-edit" : "position-start" : "position-menu" : current.step }));
        }
        if (cached.positionId) {
          const data = await cloudRequest("/api/positions");
          if (!active) return;
          const position = Array.isArray(data.positions) ? data.positions.find((item: { id?: string }) => item.id === cached.positionId) : null;
          update((current) => normalizeOnboardingPosition(current, position || null));
        }
        changePhase(stateRef.current.status === "invited" ? "invite" : stateRef.current.status);
      } catch {
        if (active) { setError("上手进度暂时没读到。点“继续引导”可以恢复本机进度，也可以先正常使用。"); changePhase("paused"); }
      }
    }
    void initialize();
    return () => { active = false; aliveRef.current = false; sessionEpochRef.current += 1; loginRequestRef.current?.abort(); setOnboardingGuideContext({ active: false, platformId: "boss" }); };
  }, [identity, changePhase, update]);

  useEffect(() => {
    if (phase !== "active") return;
    let active = true;
    const positionId = stateRef.current.positionId;
    /** validateTutorialPosition 先核对实际岗位和平台，再读取开放状态，避免旧平台缓存抢先改变恢复路线。 */
    async function validateTutorialPosition() {
      if (!positionId) { await readPlatformAvailability(); return; }
      try {
        const data = await cloudRequest("/api/positions");
        if (!active || phaseRef.current !== "active" || stateRef.current.positionId !== positionId) return;
        const position = Array.isArray(data.positions) ? data.positions.find((item: { id?: string }) => item.id === positionId) : undefined;
        if (!position || position.is_open === false) {
          update((value) => normalizeOnboardingPosition(value, null));
          setError(position ? "教程岗位已经关闭。请新建一个开启的岗位继续，原岗位会保留。" : "教程岗位已经删除。我们从新建岗位继续，其他进度会留着。");
        } else update((value) => normalizeOnboardingPosition(value, position));
        if (active) await readPlatformAvailability();
      } catch {
        if (active) setError("教程岗位暂时没读到。可以稍后继续再试，已保存的进度会留着。");
      }
    }
    void validateTutorialPosition();
    return () => { active = false; };
  }, [phase, readPlatformAvailability, update]);

  useEffect(() => {
    if (phase === "loading") return;
    if (!saveOnboardingState(identity, state)) setStorageUnavailable(true);
  }, [identity, state, phase]);
  useEffect(() => {
    // 暂停仍属于尚未结束的教程，普通欢迎、公告和小补丁不能挡住“继续教程”入口。
    const engaged = restartOpen || ["loading", "invite", "skip", "active", "paused", "completed"].includes(phase);
    onActivityChange({ engaged, awaitingChoice: ["loading", "invite", "skip"].includes(phase) });
    setOnboardingGuideContext({ active: phase === "active", platformId: state.platformId, positionId: state.positionId, step: state.step, reviewPosition: state.reviewPosition, browserOpening: false });
  }, [phase, restartOpen, state.platformId, state.positionId, state.step, state.reviewPosition, busy, onActivityChange]);
  useEffect(() => { if (phase !== "active" || blocked) loginRequestRef.current?.abort(); }, [phase, blocked]);

  /** finishResult 只有已启动教程岗位的终态筛选结果才结束教学，跳过和扫描数均不计。 */
  const finishResult = useCallback((result: OnboardingScreeningResult, positionId: string) => {
    const current = stateRef.current;
    if (phaseRef.current !== "active" || !current.positionStarted || current.positionId !== positionId || current.step !== "first-result" || current.reviewPosition) return;
    if (result.phase !== "result" || result.terminal !== true || typeof result.accepted !== "boolean" || !result.reason.trim()) return;
    // 旧本地程序会把混合模式的关键词通过提前标成终态；仍需 AI 详情时继续等待最终判断。
    if (current.filterMode === "keyword" && current.detailMode === "ai" && result.kind === "keyword" && result.accepted) return;
    update((value) => ({ ...value, status: "completed", step: "complete", replaying: false, completionKind: result.kind }));
    setStatusText(result.accepted ? "第一条真实筛选结果已经出来了，后续按岗位设置继续运行。" : "第一条真实判断：这位候选人暂不符合要求。筛选流程已经跑通。 ");
    setError(""); changePhase("completed");
    celebrate("complete", "首轮跑通，今天你赢了", "不是只点完按钮，是已经得到一条真实筛选结果。招聘小助手正式到岗。");
  }, [update, changePhase, celebrate]);

  /** openPlatform 保留旧展示层动作合同，只指明现有开始弹框入口，不另行打开或导航招聘页。 */
  const openPlatform = useCallback(() => {
    setError("请从这个岗位的“开始”进入，再点弹框里的“打开平台，并筛选条件”。");
  }, []);

  useEffect(() => {
    if (phase !== "active" || state.step !== "platform-login" || !startOpen || startDialogStatus !== "ready" || !state.positionId || state.reviewPosition || busy || blocked || helpOpen || restartOpen || !connectionAlive || !runtimeReady || !agentBase) return;
    const epoch = sessionEpochRef.current;
    const request = new AbortController(); loginRequestRef.current = request;
    let active = true;
    const platformId = state.platformId;
    const positionId = state.positionId;
    /** checkPlatformLogin 仅在原开始弹框内继续三次登录确认，不打开窗口，也不替用户确认平台筛选条件。 */
    async function checkPlatformLogin() {
      try {
        const saved = await cloudRequest("/api/positions");
        if (!active || request.signal.aborted || epoch !== sessionEpochRef.current || stateRef.current.positionId !== positionId) return;
        const position = Array.isArray(saved.positions) ? saved.positions.find((item: { id?: string }) => item.id === positionId) : null;
        const verified = normalizeOnboardingPosition(stateRef.current, position || null);
        if (!verified.positionId || verified.platformId !== platformId) {
          publishOnboardingEvent({ type: "position-start-close-requested", positionId, refreshPositions: true });
          update(() => verified); setError(verified.positionId ? "岗位的平台已调整，请打开该岗位对应的招聘平台再登录。" : "教程岗位已删除或关闭，我们先回去创建岗位。"); return;
        }
        const data = await cloudRequest("/api/platforms/config/");
        if (!active || request.signal.aborted || epoch !== sessionEpochRef.current) return;
        const configs = data.configs || data.configurations || [];
        platformConfigsRef.current = configs; setPlatformConfigs(configs);
        if (!isPlatformOpen(configs, platformId)) { returnToPlatformChoice("这个招聘平台暂未开放，请重新选择可用平台。当前登录窗口不用关闭，已保存的岗位也会保留。"); return; }
        const auth = pickPlatformAuthConfig(configs, platformId);
        await waitForPlatformLoggedIn(agentBase, auth, (message) => {
          if (active && !request.signal.aborted && epoch === sessionEpochRef.current) setStatusText(message);
        }, request.signal);
        if (!active || request.signal.aborted || epoch !== sessionEpochRef.current || phaseRef.current !== "active" || stateRef.current.step !== "platform-login" || stateRef.current.positionId !== positionId || stateRef.current.platformId !== platformId) return;
        update((value) => ({ ...value, platformLoggedIn: true, step: "position-run" }));
        setError("");
        setStatusText("平台登录已确认。年龄、学历、地区等条件请在招聘浏览器里亲自设置；设置好后，回来点“我已筛选好，立即开始”。");
        celebrate("platform", "登录确认好了，筛选条件交给你", "请在招聘浏览器里设置年龄、学历、地区等条件，回来确认后我才开工。");
      } catch (failure) {
        if (!active || request.signal.aborted || epoch !== sessionEpochRef.current) return;
        setError(`${failure instanceof Error ? failure.message : "平台登录暂时没确认"}。可以再点弹框里的“打开平台，并筛选条件”，回到招聘窗口继续登录。`);
      }
    }
    void checkPlatformLogin();
    return () => { active = false; request.abort(); if (loginRequestRef.current === request) loginRequestRef.current = null; };
  }, [identity, phase, state.step, state.platformId, state.positionId, state.reviewPosition, startOpen, startDialogStatus, platformOpenGeneration, agentBase, connectionAlive, runtimeReady, busy, blocked, helpOpen, restartOpen, update, celebrate, returnToPlatformChoice]);

  useEffect(() => subscribeOnboardingEvents((event: OnboardingEvent) => {
    if (event.type === "position-recovered" && event.positionId === pendingRecoveryRef.current) {
      pendingRecoveryRef.current = ""; setBusy(false);
      if (phaseRef.current === "active" && stateRef.current.positionId === event.positionId) {
        if (event.recovered) {
          setTaskStopped(false); setError(""); setStatusText("上一轮已经确认停好，可以重新开始。");
          update((value) => ({ ...value, step: "position-start" }));
        } else setError(event.message || "停止状态还没确认，这次先不重复启动。可以再点“上一步”重试，或稍后继续查看岗位日志。");
      }
      return;
    }
    if (event.type === "position-form-saving") { setFormSaving(event.saving === true); return; }
    if (event.type === "position-form-closed") setFormOpen(false);
    if (event.type === "start-dialog-closed") {
      loginRequestRef.current?.abort();
      setStartOpen(false); setStartDialogStatus("ready");
      if (phaseRef.current === "active" && event.positionId === stateRef.current.positionId && ["platform-filter", "platform-login", "position-run"].includes(stateRef.current.step)) update((value) => ({ ...value, step: "position-start" }));
      if (pendingStartBackRef.current && event.positionId === pendingStartBackRef.current) {
        pendingStartBackRef.current = "";
        if (phaseRef.current === "active") {
          update((value) => ({ ...value, step: "position-start" }));
          setError("");
        }
      }
    }
    if (event.type === "guide-requested") {
      if (stateRef.current.status === "completed" || cloudConfirmed) { requestRestart(); return; }
      update((current) => ({ ...current, status: "active", step: current.status === "completed" ? "position-menu" : current.step }));
      changePhase("active"); setError(""); return;
    }
    // 暂停时弹框仍可重查，只同步展示事实；恢复教程不能继续显示已经解决的旧错误。
    if (event.type === "start-dialog-status" && event.positionId === stateRef.current.positionId) {
      setStartDialogStatus(event.startStatus || "ready");
      if (event.startStatus === "error" || event.startStatus === "update") setError(event.message || "请先按启动弹框里的提示处理，再继续。");
      else if (event.startStatus === "checking" || event.startStatus === "opening") { setError(""); setStatusText(event.message || "正在处理当前操作"); }
      else if (event.startStatus === "ready") { setError(""); setStatusText(event.message || ""); }
      return;
    }
    if (phaseRef.current !== "active") return;
    const current = stateRef.current;
    if (event.type === "platform-unavailable") { returnToPlatformChoice(event.message || "这个招聘平台暂未开放，请重新选择可用平台。我不会替你换平台。"); void readPlatformAvailability(); return; }
    if (event.type === "position-form-opened") {
      setFormOpen(true);
      update((value) => {
        const next = { ...value, platformId: event.platformId || "", positionId: event.positionId || "", reviewPosition: Boolean(event.positionId), platformLoggedIn: false, positionStarted: false, completedFields: event.restored ? value.completedFields : [] };
        return { ...next, step: event.restored ? nextOnboardingFormStep(next) : "position-platform" };
      });
      setError(""); setStatusText("");
    }
    if (event.type === "position-form-closed") {
      setFormOpen(false);
      if (!current.positionId || current.reviewPosition) update((value) => ({ ...value, step: value.positionId ? "position-edit" : "position-create" }));
    }
    if (event.type === "form-field-completed" && event.field) {
      update((value) => {
        const completedFields = value.completedFields.filter((field) => field !== event.field);
        if (event.completed) completedFields.push(event.field!);
        const platformChanged = event.field === "platform" && event.platformId !== value.platformId;
        const fields = platformChanged ? completedFields.filter((field) => field !== "detail-mode" && field !== "name") : completedFields;
        const next = { ...value, completedFields: fields, ...(event.field === "platform" ? { platformId: event.platformId || "", platformLoggedIn: false } : {}), ...(event.field === "filter-mode" ? { filterMode: event.mode === "keyword" ? "keyword" as const : "ai" as const } : {}), ...(event.field === "detail-mode" ? { detailMode: event.mode || value.detailMode } : {}) };
        return { ...next, step: nextOnboardingFormStep(next) };
      });
    }
    if (event.type === "position-created" && event.positionId) {
      setFormOpen(false); update((value) => ({ ...value, positionId: event.positionId!, reviewPosition: false, platformId: event.platformId || value.platformId, platformLoggedIn: false, positionStarted: false, step: "position-start" }));
      setError(""); setStatusText("");
      celebrate("position", "岗位建好了，方向也有了", "点这个岗位的“开始”，再从弹框打开招聘平台，登录并设置筛选条件。");
    }
    if (event.type === "start-dialog-opened" && event.positionId === current.positionId && !current.reviewPosition) { setStartOpen(true); setStartDialogStatus("ready"); setError(""); update((value) => ({ ...value, platformLoggedIn: false, step: "platform-filter" })); }
    if (event.type === "platform-opened" && event.positionId === current.positionId && event.platformId === current.platformId && !current.reviewPosition && ["platform-filter", "platform-login", "position-run"].includes(current.step) && !pendingStartBackRef.current) {
      sessionEpochRef.current += 1;
      loginRequestRef.current?.abort(); setError("");
      setPlatformOpenGeneration((value) => value + 1);
      setStatusText("请切到刚打开的招聘浏览器，登录账号并设置年龄、学历、地区等筛选条件。");
      update((value) => ({ ...value, platformLoggedIn: false, step: "platform-login" }));
    }
    if (event.type === "position-started" && event.positionId === current.positionId) {
      pendingStartBackRef.current = "";
      setStartOpen(false); setTaskStopped(false); update((value) => ({ ...value, positionStarted: true, step: "first-result" }));
      celebrate("started", "小助手开工了", "先别关浏览器，咱们再看一条真实筛选结果。");
    }
    if (event.type === "screening-result" && event.positionId && event.result) finishResult(event.result, event.positionId);
  }), [update, changePhase, celebrate, finishResult, openPlatform, returnToPlatformChoice, readPlatformAvailability, cloudConfirmed, busy, taskStopped, agentBase]);

  useEffect(() => {
    if (phase !== "active") return;
    let active = true; let timer: number | undefined; let runtimeSignature = "";
    /** pollEnvironment 串行检查本地健康与真实结果，断线不删历史里程碑、不叠加慢请求。 */
    async function pollEnvironment() {
      const epoch = sessionEpochRef.current;
      try {
        if (!agentBase) {
          setConnectionAlive(false); setRuntimeReady(false); setEnvironmentChecked(agentDetected);
          await refreshAgent(); return;
        }
        const runtime = await localRequest(agentBase, "/api/v1/runtime/status", { timeoutMS: 3500 });
        if (!active || epoch !== sessionEpochRef.current) return;
        const ready = !hasMissingRequiredRuntime(runtime);
        const system = runtime?.system_runtime || runtime?.runtime?.system_runtime || {};
        const signature = JSON.stringify([requiredRuntimeComponents(runtime).map((item) => [item.key, item.installed]), runtime?.platform || "", system.download_url || ""]);
        if (signature !== runtimeSignature) {
          runtimeSignature = signature;
          // 复用本轮真实检测结果，让强制安装弹框和教程看到同一份组件事实；不新增就绪用户轮询。
          publishOnboardingEvent({ type: "runtime-status-changed", agentBase, runtimeStatus: runtime || {} });
        }
        setSystemRuntimeMissing(Boolean(system.required && !system.ready));
        setSystemDownloadAvailable(Boolean(String(system.download_url || "").trim()));
        setConnectionAlive(true); setRuntimeReady(ready); setEnvironmentChecked(true); environmentRef.current = ready;
        if (ready && !stateRef.current.environmentReady) {
          update((value) => ({ ...value, environmentReady: true, step: value.step.startsWith("agent-") || value.step.startsWith("runtime-") ? value.positionId ? value.reviewPosition ? "position-edit" : "position-start" : "position-menu" : value.step }));
          celebrate("environment", "本地小助手已到岗", "安装和连接都准备好了，先把要招聘的岗位建起来。");
        }
        const current = stateRef.current;
        if (ready && current.positionId && current.positionStarted && current.step === "first-result") {
          try {
          const task = await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(current.positionId)}/status`, { timeoutMS: 5000 });
          if (!active || epoch !== sessionEpochRef.current) return;
          const result = parseScreeningResult(task.analysis);
          const ended = ["failed", "stopped", "completed"].includes(String(task.status));
          setTaskStopped(ended);
          if (pendingRecoveryRef.current === current.positionId) return;
          if (result) finishResult(result, current.positionId);
          else {
            setStatusText(String(task.current_step || task.message || "正在等待第一条真实筛选结果"));
            if (ended) setError((currentError) => currentError || "这一轮已经结束，还没有真实筛选结果。可能暂时没有候选人，可以点“上一步”重新开始，或查看岗位日志。");
          }
          } catch {
            if (active && epoch === sessionEpochRef.current) setError("本地程序已经连上，但运行记录暂时没读到。我会继续重试，你也可以暂停教程查看岗位日志。");
          }
        }
      } catch {
        if (active && epoch === sessionEpochRef.current) { setConnectionAlive(false); environmentRef.current = false; setStatusText("本地小助手暂时掉线了。启动程序后点“检查连接”，我会接着带路。"); await refreshAgent().catch(() => undefined); }
      } finally {
        if (active) timer = window.setTimeout(() => void pollEnvironment(), 1800);
      }
    }
    void pollEnvironment();
    return () => { active = false; if (timer !== undefined) window.clearTimeout(timer); };
  }, [phase, agentBase, agentDetected, refreshAgent, update, celebrate, finishResult]);

  useEffect(() => {
    if (state.status !== "completed" || cloudConfirmed || !state.completionKind || !state.positionId) return;
    let active = true; let timer: number | undefined;
    /** syncCompletion 兼容旧本地程序，仅把无敏感详情的真实结果事实同步到既有云端接口。 */
    async function syncCompletion() {
      try {
        await cloudRequest("/api/user-flow", { method: "POST", body: { step: "first_screening_result", status: "completed", source: "interactive_tutorial", position_id: state.positionId, metadata: { kind: state.completionKind, phase: "result", terminal: true } } });
        if (active) { setCloudConfirmed(true); setError(""); }
      } catch {
        if (active) { setError("流程已经跑通，完成记录正在重试同步。你的本机进度已经保留。"); timer = window.setTimeout(() => void syncCompletion(), 4000); }
      }
    }
    void syncCompletion();
    return () => { active = false; if (timer !== undefined) window.clearTimeout(timer); };
  }, [state.status, cloudConfirmed, state.completionKind, state.positionId]);

  let step = state.step;
  if (phase === "active") {
    if (environmentChecked && !connectionAlive) step = pathname.startsWith("/admin/agent-download") ? state.step === "agent-connect" ? "agent-connect" : "agent-download" : "agent-menu";
    else if (connectionAlive && !runtimeReady) step = systemRuntimeMissing ? "runtime-system" : "runtime-install";
      else if (step.startsWith("agent-") || step.startsWith("runtime-") || step === "platform-select") step = state.reviewPosition && state.positionId ? "position-edit" : "position-menu";
    else if (step === "platform-open") step = state.positionId ? "position-start" : "position-menu";
    else if (!state.positionId && step === "platform-login") step = "position-menu";
    else if (step === "first-result" && taskStopped && !pathname.startsWith("/admin/positions")) step = "position-menu";
    else if (step.startsWith("position-") || ["platform-filter", "platform-login"].includes(step)) {
      if (!pathname.startsWith("/admin/positions")) step = "position-menu";
      else if (step === "position-menu") step = state.positionId ? state.reviewPosition ? "position-edit" : "position-start" : "position-create";
      else if (["position-platform", "position-name", "position-filter-mode", "position-detail-mode", "position-requirement", "position-keywords", "position-save"].includes(step) && !formOpen) step = state.reviewPosition && state.positionId ? "position-edit" : "position-create";
      else if (["platform-filter", "platform-login", "position-run"].includes(step) && !startOpen) step = "position-start";
      else if (startOpen && ["platform-filter", "platform-login", "position-run"].includes(step)) {
        if (startDialogStatus === "error") step = "position-start-error";
        else if (startDialogStatus === "update") step = "position-start-update";
        else if (startDialogStatus === "checking") step = "position-start-check";
      }
    }
  }
  const selector = onboardingTargetSelector(step, state.positionId);
  const { target, targetGeometry } = useOnboardingTarget(selector, phase === "active" && !blocked && !helpOpen && !restartOpen);
  const copy = { ...onboardingStepCopy(step) };
  const platformName = ({ boss: "BOSS 直聘", zhaopin: "智联招聘", liepin: "猎聘企业端", hliepin: "猎聘猎头端" } as Record<string, string>)[state.platformId] || "招聘平台";
  if (step === "platform-login" && state.positionId) copy.description = `请切到刚打开的招聘浏览器，在${platformName}里登录招聘账号，并亲自设置年龄、学历、地区等条件。登录确认后，回来点击“我已筛选好，立即开始”。`;
  if (step === "position-menu" && state.positionId) {
    copy.title = state.reviewPosition ? "回到岗位管理，检查刚才的岗位" : "回到岗位管理，准备开工";
    copy.description = state.reviewPosition ? "点击左侧“岗位管理”，我会带你编辑已经保存的教程岗位，不用再建一份。" : "点击左侧“岗位管理”，回到已经保存的教程岗位。登录后再设置筛选条件，接着开始第一轮。";
    copy.hint = "岗位已经保存，不用再建一遍。";
  }
  if (step === "position-menu" && state.step === "first-result") {
    copy.title = "先回岗位管理，接着处理这一轮";
    copy.description = "点击左侧“岗位管理”，回到教程岗位。上一轮已经结束但还没有筛选结果，回去后点“上一步”，我会先确认岗位停好，再带你重新开始。";
    copy.hint = "已经保存的岗位和教程进度都会留着，不需要重新创建。";
  }
  if (step === "runtime-system" && !systemDownloadAvailable) copy.description = "当前没有运行库下载地址。请到微软官网下载 Microsoft Visual C++ x64 运行库，或联系作者确认安装包。安装或修复完成后，回来点击“安装或修复后重新检查”。";
  if (step === "position-save" && state.reviewPosition) {
    copy.title = "把检查好的岗位存下来";
    copy.description = "点击“保存修改”。这次更新的还是刚才那个岗位，保存成功后，点岗位“开始”，再打开招聘平台登录和筛选。";
    copy.hint = "想检查前面的字段，继续点“上一步”；不会重复创建岗位。";
  }

  const restartBlockedReason = blocked || busy || formSaving || (startOpen && ["checking", "opening"].includes(startDialogStatus))
    ? "当前操作还在处理中，等它结束后就能重新带路，免得保存或启动到一半。"
    : state.positionStarted && !taskStopped
      ? "这个岗位还在运行。先到岗位管理停止它，再从头带路，避免同时开两轮。"
      : "";

  /** requestRestart 先解释仅重置教程的范围，登录检测在弹框期间暂停，不能越过用户选择。 */
  function requestRestart() {
    loginRequestRef.current?.abort();
    setHelpOpen(false); setRestartOpen(true);
    const current = stateRef.current;
    if (current.positionStarted && !taskStopped && agentBase && !busy) void checkRestartTask(current.positionId);
  }
  /** checkRestartTask 核对暂停期间用户是否已停止原任务，只读取状态，不替用户停止或重新启动。 */
  async function checkRestartTask(positionId: string) {
    const epoch = sessionEpochRef.current;
    setBusy(true);
    try {
      const task = await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(positionId)}/status`, { timeoutMS: 5000 });
      if (aliveRef.current && epoch === sessionEpochRef.current && stateRef.current.positionId === positionId) setTaskStopped(["stopped", "completed", "failed"].includes(String(task.status)));
    } catch {
      // 读不到时保留运行保护，不能把失联当成已经安全停止。
    } finally {
      if (aliveRef.current && epoch === sessionEpochRef.current) setBusy(false);
    }
  }
  /** cancelRestart 取消重走后保留原微步骤，现有登录检测会继续。 */
  function cancelRestart() { setRestartOpen(false); }
  /** confirmRestart 清除本轮未保存草稿和微步骤，保留岗位及云端完成事实，不自动启停任务。 */
  function confirmRestart() {
    if (restartBlockedReason) return;
    const current = stateRef.current;
    sessionEpochRef.current += 1;
    loginRequestRef.current?.abort(); loginRequestRef.current = null;
    publishOnboardingEvent({ type: "position-form-close-requested" });
    publishOnboardingEvent({ type: "position-start-close-requested" });
    clearOnboardingPositionDraft(identity, undefined, current.positionId || undefined);
    pendingStartBackRef.current = ""; pendingRecoveryRef.current = "";
    setBusy(false); setFormOpen(false); setFormSaving(false); setStartOpen(false);
    setTaskStopped(false); setHelpOpen(false); setRestartOpen(false); setCelebration(null);
    setError(""); setStatusText("");
    update(() => restartOnboardingState(current, connectionAlive && runtimeReady));
    changePhase("invite");
  }

  /** choosePlatform 兼容旧界面入口，仅确认岗位表单的平台，不提前打开或检测浏览器。 */
  function choosePlatform(platformId: string) {
    if (!["boss", "zhaopin", "liepin", "hliepin"].includes(platformId)) return;
    if (platformsLoading || !isPlatformOpen(platformConfigsRef.current, platformId)) { setError("这个招聘平台暂未开放，请选择当前可用的平台。"); return; }
    update((value) => ({ ...value, platformId, platformLoggedIn: false, completedFields: [...value.completedFields.filter((field) => field !== "platform"), "platform"], step: formOpen ? "position-name" : "position-create" }));
    setError("");
  }
  /** accept 接受引导邀请；保留当前真实事实，不把跳过当成已完成。 */
  function accept() { update((value) => ({ ...value, status: "active" })); changePhase("active"); setError(""); }
  /** pause 暂停遮罩但保留进度，恢复后重新检查当前环境。 */
  function pause() { if (formSaving) return; update((value) => ({ ...value, status: "paused" })); changePhase("paused"); setHelpOpen(false); setRestartOpen(false); }
  /** resume 允许用户主动重新进入教程，已拒绝引导也能从帮助入口回来。 */
  function resume() { update((value) => ({ ...value, status: "active", step: value.status === "completed" ? "position-menu" : value.step })); changePhase("active"); setError(""); }
  /** back 回看前一个操作说明，不删除已经保存的岗位或真实完成事实。 */
  function back() {
    if (formSaving || pendingRecoveryRef.current) return;
    sessionEpochRef.current += 1;
    loginRequestRef.current?.abort();
    setBusy(false);
    setHelpOpen(false); setRestartOpen(false); setStatusText(""); setError("");
    const current = stateRef.current;
    if (step === "first-result") {
      if (!taskStopped) { requestRestart(); return; }
      if (pendingRecoveryRef.current) return;
      pendingRecoveryRef.current = current.positionId;
      setBusy(true); setError(""); setStatusText("上一轮已经结束，正在确认本地和云端都已停好，确认后再重新开始。");
      publishOnboardingEvent({ type: "position-recover-requested", positionId: current.positionId });
      return;
    }
    if (["platform-open", "position-start"].includes(step) && current.positionId) {
      update((value) => ({ ...value, reviewPosition: true, platformLoggedIn: false, positionStarted: false, step: "position-edit" }));
      return;
    }
    if (step === "position-edit" || step === "position-create") {
      update((value) => ({ ...value, step: "position-menu" })); router.push("/admin"); return;
    }
    if (step === "position-menu") { changePhase("invite"); return; }
    if (step === "position-platform" && formOpen) {
      publishOnboardingEvent({ type: "position-form-close-requested" });
      update((value) => ({ ...value, step: value.reviewPosition && value.positionId ? "position-edit" : "position-create" }));
      return;
    }
    if ((["platform-filter", "position-start-check", "position-start-error", "position-start-update"].includes(step) || (startDialogStatus === "opening" && ["platform-login", "position-run"].includes(step))) && startOpen) {
      pendingStartBackRef.current = current.positionId;
      publishOnboardingEvent({ type: "position-start-close-requested" });
      return;
    }
    const previous: Partial<Record<OnboardingStep, OnboardingStep>> = { "agent-connect": "agent-download", "platform-login": "platform-filter", "position-create": "position-menu", "position-platform": "position-create", "position-name": "position-platform", "position-filter-mode": "position-name", "position-detail-mode": "position-filter-mode", "position-requirement": "position-detail-mode", "position-keywords": needsPositionRequirement(current) ? "position-requirement" : "position-detail-mode", "position-save": current.filterMode === "keyword" ? "position-keywords" : "position-requirement", "platform-filter": "position-start", "position-run": "platform-filter", "first-result": "position-start" };
    update((value) => ({ ...value, step: previous[step] || value.step })); setError("");
  }
  /** checkAgent 主动重新探测，复用已有绑定流程，不重复打开安装提示弹框。 */
  async function checkAgent() {
    if (busy) return; setBusy(true); clearLocalAgentDetectCache();
    try { await refreshAgent(); } catch { setError("还没连上。确认本地 GoodHR 已启动，再检查浏览器的设备应用权限。"); }
    finally { if (aliveRef.current) setBusy(false); }
  }
  return { phase, state, step, target, targetGeometry, ...copy, remaining: onboardingRemainingTime(state), doneCount: onboardingDoneCount(state), statusText, error, busy, helpOpen, restartOpen, restartBlockedReason, resumeAboveDialog: !blocked && (formOpen || startOpen), formSaving, blocked, storageUnavailable, availablePlatforms: ["boss", "zhaopin", "liepin", "hliepin"].filter((platformId) => isPlatformOpen(platformConfigs, platformId)), platformsLoading, celebration, actions: {
    requestRestart, cancelRestart, confirmRestart,
    accept, pause, resume, back, choosePlatform, openPlatform: () => void openPlatform(), checkAgent: () => void checkAgent(),
    requestSkip: () => { phaseBeforeSkip.current = phaseRef.current; changePhase("skip"); },
    cancelSkip: () => changePhase(phaseBeforeSkip.current),
    confirmSkip: () => { update((value) => ({ ...value, status: "declined" })); changePhase("declined"); },
    openHelp: () => setHelpOpen(true), closeHelp: () => setHelpOpen(false),
    downloadAgent: () => { window.open("/download", "_blank", "noopener,noreferrer"); update((value) => ({ ...value, step: "agent-connect" })); },
    finishClose: () => changePhase("silent"), dismissCelebration: () => setCelebration(null),
    refreshPlatforms: () => void readPlatformAvailability(),
  } };
}

/** parseScreeningResult 只接受具有明确判断的真实终态结果，异常与扫描计数均返回空。 */
function parseScreeningResult(value: unknown): OnboardingScreeningResult | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const result = value as Record<string, unknown>;
  if (result.phase !== "result" || result.terminal !== true || typeof result.accepted !== "boolean" || typeof result.reason !== "string" || !result.reason.trim()) return null;
  if (result.kind !== "ai" && result.kind !== "keyword") return null;
  return { kind: result.kind, phase: "result", terminal: true, accepted: result.accepted, reason: result.reason, ...(typeof result.score === "number" ? { score: result.score } : {}) };
}
