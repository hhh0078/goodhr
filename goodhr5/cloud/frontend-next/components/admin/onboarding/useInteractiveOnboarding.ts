/** 本文件编排跨页面新手引导，复用实际登录、安装和岗位状态；仅微步骤保存在本机。 */
"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import { usePathname } from "next/navigation";
import { clearLocalAgentDetectCache, cloudRequest, localRequest } from "@/lib/admin-api";
import { hasMissingRequiredRuntime } from "@/lib/admin-runtime";
import { openPlatformPositionBrowser, pickPlatformAuthConfig, waitForPlatformLoggedIn } from "@/lib/platform-login";
import { isPlatformOpen, type PlatformConfigLike } from "@/lib/platform-open";
import { loadUserFlow } from "@/lib/user-flow";
import { publishOnboardingEvent, setOnboardingGuideContext, subscribeOnboardingEvents, type OnboardingEvent, type OnboardingScreeningResult } from "@/lib/onboarding-events";
import { createOnboardingState, nextOnboardingFormStep, onboardingDoneCount, onboardingRemainingTime, readOnboardingState, saveOnboardingState, type OnboardingState, type OnboardingStep } from "@/lib/onboarding-state";
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
  const [startDialogStatus, setStartDialogStatus] = useState<"ready" | "checking" | "error" | "update">("ready");
  const [taskStopped, setTaskStopped] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [statusText, setStatusText] = useState("");
  const [storageUnavailable, setStorageUnavailable] = useState(false);
  const [celebration, setCelebration] = useState<OnboardingCelebration | null>(null);
  const [cloudConfirmed, setCloudConfirmed] = useState(false);
  const aliveRef = useRef(true);
  const openingRef = useRef(false);
  const pendingStartBackRef = useRef("");
  const pendingRecoveryRef = useRef("");
  const loginRequestRef = useRef<AbortController | null>(null);
  const environmentRef = useRef(false);
  const phaseBeforeSkip = useRef<OnboardingPhase>("invite");

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

  /** returnToPlatformChoice 保留实际岗位数据，在所选平台关闭时明确返回选择，绝不静默换平台。 */
  const returnToPlatformChoice = useCallback((message: string) => {
    loginRequestRef.current?.abort();
    publishOnboardingEvent({ type: "position-form-close-requested" });
    publishOnboardingEvent({ type: "position-start-close-requested" });
    update((value) => ({ ...value, step: "platform-select", platformLoggedIn: false, positionId: "", positionStarted: false, completedFields: [], completionKind: undefined }));
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
        if (phaseRef.current === "active" && !isPlatformOpen(configs, current.platformId) && !current.step.startsWith("agent-") && !current.step.startsWith("runtime-") && current.step !== "platform-select") {
          returnToPlatformChoice("上次选择的招聘平台暂未开放，请重新选择可用平台。已经保存的岗位会保留，我不会替你换平台。");
        }
        return configs;
      } catch {
        if (aliveRef.current) setError("招聘平台的开放状态暂时没读到。点“重新读取平台”再试一次，我先不替你乱选。");
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
          setCloudConfirmed(true); changePhase("silent"); return;
        }
        if (runtimeRequest.status === "fulfilled" && runtimeRequest.value && !hasMissingRequiredRuntime(runtimeRequest.value)) {
          setConnectionAlive(true); setRuntimeReady(true); setEnvironmentChecked(true); environmentRef.current = true;
          update((current) => ({ ...current, environmentReady: true, step: current.step.startsWith("agent-") || current.step.startsWith("runtime-") ? "platform-select" : current.step }));
        }
        if (cached.positionId) {
          const data = await cloudRequest("/api/positions");
          if (!active) return;
          const exists = Array.isArray(data.positions) && data.positions.some((position: { id?: string }) => position.id === cached.positionId);
          if (!exists) update((current) => ({ ...current, positionId: "", positionStarted: false, completedFields: [], step: "position-menu", status: current.status === "completed" ? "active" : current.status, completionKind: undefined }));
        }
        changePhase(stateRef.current.status === "invited" ? "invite" : stateRef.current.status);
      } catch {
        if (active) { setError("上手进度暂时没读到。点“继续引导”可以恢复本机进度，也可以先正常使用。"); changePhase("paused"); }
      }
    }
    void initialize();
    return () => { active = false; aliveRef.current = false; loginRequestRef.current?.abort(); setOnboardingGuideContext({ active: false, platformId: "boss" }); };
  }, [identity, changePhase, update]);

  useEffect(() => {
    if (phase !== "active") return;
    let active = true;
    void readPlatformAvailability();
    const positionId = stateRef.current.positionId;
    /** validateTutorialPosition 恢复教程时核对岗位仍存在且开启，避免暂停期间删除后留下无法点击的目标。 */
    async function validateTutorialPosition() {
      if (!positionId) return;
      try {
        const data = await cloudRequest("/api/positions");
        if (!active || phaseRef.current !== "active" || stateRef.current.positionId !== positionId) return;
        const position = Array.isArray(data.positions) ? data.positions.find((item: { id?: string }) => item.id === positionId) : undefined;
        if (!position || position.is_open === false) {
          update((value) => ({ ...value, positionId: "", positionStarted: false, completedFields: [], step: "position-menu", completionKind: undefined }));
          setError(position ? "教程岗位已经关闭。请新建一个开启的岗位继续，原岗位会保留。" : "教程岗位已经删除。我们从新建岗位继续，其他进度会留着。");
        }
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
    const engaged = ["loading", "invite", "skip", "active", "paused", "completed"].includes(phase);
    onActivityChange({ engaged, awaitingChoice: ["loading", "invite", "skip"].includes(phase) });
    setOnboardingGuideContext({ active: phase === "active", platformId: state.platformId, positionId: state.positionId, step: state.step, browserOpening: busy && ["platform-open", "platform-login"].includes(state.step) });
  }, [phase, state.platformId, state.positionId, state.step, busy, onActivityChange]);
  useEffect(() => { if (phase !== "active" || blocked) loginRequestRef.current?.abort(); }, [phase, blocked]);

  /** finishResult 只有已启动教程岗位的终态筛选结果才结束教学，跳过和扫描数均不计。 */
  const finishResult = useCallback((result: OnboardingScreeningResult, positionId: string) => {
    const current = stateRef.current;
    if (phaseRef.current !== "active" || !current.positionStarted || current.positionId !== positionId) return;
    if (result.phase !== "result" || result.terminal !== true || typeof result.accepted !== "boolean" || !result.reason.trim()) return;
    // 旧本地程序会把混合模式的关键词通过提前标成终态；仍需 AI 详情时继续等待最终判断。
    if (current.filterMode === "keyword" && current.detailMode === "ai" && result.kind === "keyword" && result.accepted) return;
    update((value) => ({ ...value, status: "completed", step: "complete", completionKind: result.kind }));
    setStatusText(result.accepted ? "第一条真实筛选结果已经出来了，后续按岗位设置继续运行。" : "第一条真实判断：这位候选人暂不符合要求。筛选流程已经跑通。 ");
    setError(""); changePhase("completed");
    celebrate("complete", "首轮跑通，今天你赢了", "不是只点完按钮，是已经得到一条真实筛选结果。招聘小助手正式到岗。");
  }, [update, changePhase, celebrate]);

  /** openPlatform 只处理顶部入口的实际打开请求，登录等待由独立检测负责。 */
  const openPlatform = useCallback(async () => {
    if (openingRef.current || !agentBase || !environmentRef.current || phaseRef.current !== "active" || blocked || helpOpen) return;
    loginRequestRef.current?.abort();
    openingRef.current = true; setBusy(true); setError("");
    const request = new AbortController(); loginRequestRef.current = request;
    const platformId = stateRef.current.platformId;
    try {
      const data = await cloudRequest("/api/platforms/config/");
      if (request.signal.aborted || !aliveRef.current || phaseRef.current !== "active") return;
      const configs = data.configs || data.configurations || [];
      platformConfigsRef.current = configs; setPlatformConfigs(configs);
      if (!isPlatformOpen(configs, platformId)) { returnToPlatformChoice("刚选的平台暂未开放，请换一个可用平台。我不会打开别的平台来凑数。"); return; }
      const auth = pickPlatformAuthConfig(configs, platformId);
      await openPlatformPositionBrowser(agentBase, platformId, auth);
      if (request.signal.aborted || !aliveRef.current || phaseRef.current !== "active" || stateRef.current.platformId !== platformId) return;
      setStatusText("浏览器已经打开，请切过去登录招聘平台账号。");
      update((value) => ({ ...value, step: "platform-login" }));
    } catch (failure) {
      if (request.signal.aborted) return;
      if (aliveRef.current && phaseRef.current === "active") setError(failure instanceof Error ? failure.message : "浏览器暂时没打开，再点顶部按钮试试。");
    } finally {
      openingRef.current = false;
      if (loginRequestRef.current === request) loginRequestRef.current = null;
      if (aliveRef.current) setBusy(false);
    }
  }, [agentBase, blocked, helpOpen, update, returnToPlatformChoice]);

  useEffect(() => {
    if (phase !== "active" || state.step !== "platform-login" || busy || blocked || helpOpen || !connectionAlive || !runtimeReady || !agentBase) return;
    const request = new AbortController(); loginRequestRef.current = request;
    let active = true;
    const platformId = state.platformId;
    /** checkPlatformLogin 刷新或恢复后只继续检测，不重复打开窗口；旧请求不能推进新账号。 */
    async function checkPlatformLogin() {
      try {
        const data = await cloudRequest("/api/platforms/config/");
        if (!active || request.signal.aborted) return;
        const configs = data.configs || data.configurations || [];
        platformConfigsRef.current = configs; setPlatformConfigs(configs);
        if (!isPlatformOpen(configs, platformId)) { returnToPlatformChoice("这个招聘平台暂未开放，请重新选择可用平台。当前登录窗口不用关闭，已保存的岗位也会保留。"); return; }
        const auth = pickPlatformAuthConfig(configs, platformId);
        await waitForPlatformLoggedIn(agentBase, auth, (message) => {
          if (active && !request.signal.aborted) setStatusText(message);
        }, request.signal);
        if (!active || request.signal.aborted || phaseRef.current !== "active" || stateRef.current.platformId !== platformId) return;
        update((value) => ({ ...value, platformLoggedIn: true, step: value.positionId ? "position-start" : "position-menu" }));
        setError("");
        celebrate("platform", "平台登录好了，开工路又近一步", "现在复制平台岗位名称，建立你的第一个招聘岗位。");
      } catch (failure) {
        if (!active || request.signal.aborted) return;
        setError(`${failure instanceof Error ? failure.message : "平台登录暂时没确认"}。可以再点顶部“打开浏览器”，回到招聘窗口继续登录。`);
      }
    }
    void checkPlatformLogin();
    return () => { active = false; request.abort(); if (loginRequestRef.current === request) loginRequestRef.current = null; };
  }, [identity, phase, state.step, state.platformId, agentBase, connectionAlive, runtimeReady, busy, blocked, helpOpen, update, celebrate, returnToPlatformChoice]);

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
    if (event.type === "position-form-closed") setFormOpen(false);
    if (event.type === "start-dialog-closed") {
      setStartOpen(false); setStartDialogStatus("ready");
      if (pendingStartBackRef.current && event.positionId === pendingStartBackRef.current) {
        pendingStartBackRef.current = "";
        if (phaseRef.current === "active") {
          update((value) => ({ ...value, step: "position-start" }));
          setError("");
        }
      }
    }
    if (event.type === "guide-requested") {
      update((current) => ({ ...current, status: "active", step: current.status === "completed" ? "platform-select" : current.step }));
      changePhase("active"); setError(""); return;
    }
    if (phaseRef.current !== "active") return;
    const current = stateRef.current;
    if (event.type === "platform-unavailable") { returnToPlatformChoice(event.message || "这个招聘平台暂未开放，请重新选择可用平台。我不会替你换平台。"); void readPlatformAvailability(); return; }
    if (event.type === "start-dialog-status" && event.positionId === current.positionId) {
      setStartDialogStatus(event.startStatus || "ready");
      if (event.startStatus === "error" || event.startStatus === "update") setError(event.message || "请先按启动弹框里的提示处理，再继续。");
      else if (event.startStatus === "checking") { setError(""); setStatusText(event.message || "正在检查启动条件"); }
      return;
    }
    if (event.type === "platform-browser-open-requested" && event.platformId === current.platformId && ["platform-open", "platform-login"].includes(current.step)) { void openPlatform(); return; }
    if (event.type === "position-form-opened") { setFormOpen(true); update((value) => ({ ...value, completedFields: event.restored ? value.completedFields : [], step: event.restored ? nextOnboardingFormStep(value) : "position-name" })); }
    if (event.type === "position-form-closed") { setFormOpen(false); if (!current.positionId) update((value) => ({ ...value, step: "position-create" })); }
    if (event.type === "form-field-completed" && event.field) {
      update((value) => {
        const completedFields = value.completedFields.filter((field) => field !== event.field);
        if (event.completed) completedFields.push(event.field!);
        const next = { ...value, completedFields, ...(event.field === "filter-mode" ? { filterMode: event.mode === "keyword" ? "keyword" as const : "ai" as const } : {}), ...(event.field === "detail-mode" ? { detailMode: event.mode || value.detailMode } : {}) };
        return { ...next, step: nextOnboardingFormStep(next) };
      });
    }
    if (event.type === "position-created" && event.positionId) {
      setFormOpen(false); update((value) => ({ ...value, positionId: event.positionId!, platformId: event.platformId || value.platformId, step: "position-start" }));
      celebrate("position", "岗位建好了，方向也有了", "接下来设置平台筛选条件，让小助手开始干活。");
    }
    if (event.type === "start-dialog-opened" && event.positionId === current.positionId) { setStartOpen(true); setStartDialogStatus("ready"); setError(""); update((value) => ({ ...value, step: "platform-filter" })); }
    if (event.type === "platform-opened" && event.positionId === current.positionId && !pendingStartBackRef.current) update((value) => ({ ...value, step: "position-run" }));
    if (event.type === "position-started" && event.positionId === current.positionId) {
      pendingStartBackRef.current = "";
      setStartOpen(false); setTaskStopped(false); update((value) => ({ ...value, positionStarted: true, step: "first-result" }));
      celebrate("started", "小助手开工了", "先别关浏览器，咱们再看一条真实筛选结果。");
    }
    if (event.type === "screening-result" && event.positionId && event.result) finishResult(event.result, event.positionId);
  }), [update, changePhase, celebrate, finishResult, openPlatform, returnToPlatformChoice, readPlatformAvailability]);

  useEffect(() => {
    if (phase !== "active") return;
    let active = true; let timer: number | undefined;
    /** pollEnvironment 串行检查本地健康与真实结果，断线不删历史里程碑、不叠加慢请求。 */
    async function pollEnvironment() {
      try {
        if (!agentBase) {
          setConnectionAlive(false); setRuntimeReady(false); setEnvironmentChecked(agentDetected);
          await refreshAgent(); return;
        }
        const runtime = await localRequest(agentBase, "/api/v1/runtime/status", { timeoutMS: 3500 });
        if (!active) return;
        const ready = !hasMissingRequiredRuntime(runtime);
        const system = runtime?.system_runtime || runtime?.runtime?.system_runtime || {};
        setSystemRuntimeMissing(Boolean(system.required && !system.ready));
        setSystemDownloadAvailable(Boolean(String(system.download_url || "").trim()));
        setConnectionAlive(true); setRuntimeReady(ready); setEnvironmentChecked(true); environmentRef.current = ready;
        if (ready && !stateRef.current.environmentReady) {
          update((value) => ({ ...value, environmentReady: true, step: value.step.startsWith("agent-") || value.step.startsWith("runtime-") ? "platform-select" : value.step }));
          celebrate("environment", "本地小助手已到岗", "安装和连接都准备好了，现在去打开招聘平台。");
        }
        const current = stateRef.current;
        if (ready && current.positionId && current.positionStarted && current.step === "first-result") {
          try {
          const task = await localRequest(agentBase, `/api/v1/local/positions/${encodeURIComponent(current.positionId)}/status`, { timeoutMS: 5000 });
          if (!active) return;
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
            if (active) setError("本地程序已经连上，但运行记录暂时没读到。我会继续重试，你也可以暂停教程查看岗位日志。");
          }
        }
      } catch {
        if (active) { setConnectionAlive(false); environmentRef.current = false; setStatusText("本地小助手暂时掉线了。启动程序后点“检查连接”，我会接着带路。"); await refreshAgent().catch(() => undefined); }
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
    else if (step.startsWith("agent-") || step.startsWith("runtime-")) step = "platform-select";
    else if (step === "first-result" && taskStopped && !pathname.startsWith("/admin/positions")) step = "position-menu";
    else if (step.startsWith("position-") || step === "platform-filter") {
      if (!pathname.startsWith("/admin/positions")) step = "position-menu";
      else if (step === "position-menu") step = state.positionId ? "position-start" : "position-create";
      else if (["position-name", "position-filter-mode", "position-detail-mode", "position-requirement", "position-keywords", "position-save"].includes(step) && !formOpen) step = "position-create";
      else if ((step === "platform-filter" || step === "position-run") && !startOpen) step = "position-start";
      else if (startOpen && ["platform-filter", "position-run"].includes(step)) {
        if (startDialogStatus === "error") step = "position-start-error";
        else if (startDialogStatus === "update") step = "position-start-update";
        else if (startDialogStatus === "checking") step = "position-start-check";
      }
    }
  }
  const selector = onboardingTargetSelector(step, state.positionId);
  const { target, targetGeometry } = useOnboardingTarget(selector, phase === "active" && !blocked && !helpOpen);
  const copy = { ...onboardingStepCopy(step) };
  if (step === "position-menu" && state.step === "first-result") {
    copy.title = "先回岗位管理，接着处理这一轮";
    copy.description = "点击左侧“岗位管理”，回到教程岗位。上一轮已经结束但还没有筛选结果，回去后点“上一步”，我会先确认岗位停好，再带你重新开始。";
    copy.hint = "已经保存的岗位和教程进度都会留着，不需要重新创建。";
  }
  if (step === "runtime-system" && !systemDownloadAvailable) copy.description = "当前没有运行库下载地址。请到微软官网下载 Microsoft Visual C++ x64 运行库，或联系作者确认安装包。安装或修复完成后，回来点击“安装或修复后重新检查”。";

  /** choosePlatform 选择已经使用的平台，并等待用户亲自打开招聘浏览器。 */
  function choosePlatform(platformId: string) {
    if (!["boss", "zhaopin", "liepin", "hliepin"].includes(platformId)) return;
    if (platformsLoading || !isPlatformOpen(platformConfigsRef.current, platformId)) { setError("这个招聘平台暂未开放，请选择当前可用的平台。"); return; }
    update((value) => ({ ...value, platformId, platformLoggedIn: value.platformId === platformId && value.platformLoggedIn, ...(value.platformId !== platformId ? { positionId: "", positionStarted: false, completedFields: [] } : {}), step: "platform-open" }));
    setError("");
  }
  /** accept 接受引导邀请；保留当前真实事实，不把跳过当成已完成。 */
  function accept() { update((value) => ({ ...value, status: "active" })); changePhase("active"); setError(""); }
  /** pause 暂停遮罩但保留进度，恢复后重新检查当前环境。 */
  function pause() { update((value) => ({ ...value, status: "paused" })); changePhase("paused"); setHelpOpen(false); }
  /** resume 允许用户主动重新进入教程，已拒绝引导也能从帮助入口回来。 */
  function resume() { update((value) => ({ ...value, status: "active", step: value.status === "completed" ? "platform-select" : value.step })); changePhase("active"); setError(""); }
  /** back 回看前一个操作说明，不删除已经保存的岗位或真实完成事实。 */
  function back() {
    loginRequestRef.current?.abort();
    const current = stateRef.current;
    if (step === "first-result") {
      if (!taskStopped) { setHelpOpen(true); return; }
      if (pendingRecoveryRef.current) return;
      pendingRecoveryRef.current = current.positionId;
      setBusy(true); setError(""); setStatusText("上一轮已经结束，正在确认本地和云端都已停好，确认后再重新开始。");
      publishOnboardingEvent({ type: "position-recover-requested", positionId: current.positionId });
      return;
    }
    if (step === "position-name" && formOpen) publishOnboardingEvent({ type: "position-form-close-requested" });
    if (["platform-filter", "position-start-check", "position-start-error", "position-start-update"].includes(step) && startOpen) {
      pendingStartBackRef.current = current.positionId;
      publishOnboardingEvent({ type: "position-start-close-requested" });
      return;
    }
    const previous: Partial<Record<OnboardingStep, OnboardingStep>> = { "agent-connect": "agent-download", "platform-open": "platform-select", "platform-login": "platform-open", "position-create": "position-menu", "position-name": "position-create", "position-filter-mode": "position-name", "position-detail-mode": "position-filter-mode", "position-requirement": "position-detail-mode", "position-keywords": "position-detail-mode", "position-save": current.filterMode === "keyword" ? "position-keywords" : "position-requirement", "platform-filter": "position-start", "position-run": "platform-filter", "first-result": "position-start" };
    update((value) => ({ ...value, step: previous[step] || value.step })); setError("");
  }
  /** checkAgent 主动重新探测，复用已有绑定流程，不重复打开安装提示弹框。 */
  async function checkAgent() {
    if (busy) return; setBusy(true); clearLocalAgentDetectCache();
    try { await refreshAgent(); } catch { setError("还没连上。确认本地 GoodHR 已启动，再检查浏览器的设备应用权限。"); }
    finally { if (aliveRef.current) setBusy(false); }
  }
  return { phase, state, step, target, targetGeometry, ...copy, remaining: onboardingRemainingTime(state), doneCount: onboardingDoneCount(state), statusText, error, busy, helpOpen, blocked, storageUnavailable, availablePlatforms: ["boss", "zhaopin", "liepin", "hliepin"].filter((platformId) => isPlatformOpen(platformConfigs, platformId)), platformsLoading, celebration, actions: {
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
