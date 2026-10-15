/** 本文件负责新手邀请、真实操作遮罩、阶段进度、针对性帮助和轻量完成鼓励。 */
"use client";

import { Alert, Button, Flex, Image, Input, Modal, Progress, Spin, Tour, Typography } from "antd";
import { CheckCircleFilled, ClockCircleOutlined, PauseOutlined, QuestionCircleOutlined, TrophyOutlined } from "@ant-design/icons";
import confetti from "canvas-confetti";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { OnboardingExperienceProps } from "@/lib/onboarding-view";
import { ONBOARDING_SKIP_PHRASE, ONBOARDING_STAGES, onboardingStage } from "@/lib/onboarding-state";
import ChoiceCards from "../ChoiceCards";
import ClickableImagePreview from "../ClickableImagePreview";
import styles from "./OnboardingExperience.module.css";

const platformChoices = [
  { value: "boss", label: "BOSS 直聘", description: "使用 BOSS 直聘招聘账号" },
  { value: "zhaopin", label: "智联招聘", description: "使用智联招聘企业账号" },
  { value: "liepin", label: "猎聘企业端", description: "使用猎聘企业招聘账号" },
  { value: "hliepin", label: "猎聘猎头端", description: "使用猎聘猎头账号" },
];
const permissionImage = "/assets/help/local-agent-device-app-permission.png";
const operationSteps = ["platform-select", "agent-download", "agent-connect", "first-result"];

/** OnboardingExperience 只展示控制器状态，推进步骤必须由真实操作结果决定。 */
export default function OnboardingExperience(props: OnboardingExperienceProps) {
  const { phase, state, step, target, targetGeometry, title, description, hint, estimate, remaining, doneCount, statusText, error, busy, helpOpen, blocked, storageUnavailable, celebration, actions } = props;
  const [skipText, setSkipText] = useState("");
  const [mobileMenuPlacement, setMobileMenuPlacement] = useState<"top" | "bottom" | null>(null);
  const actionsRef = useRef(actions);
  const progressBarRef = useRef<HTMLElement | null>(null);
  actionsRef.current = actions;
  const stage = onboardingStage(step);
  const active = phase === "active";
  const stageDone = [state.environmentReady, state.platformLoggedIn, Boolean(state.positionId), state.positionStarted, state.status === "completed"];

  useLayoutEffect(() => {
    if (!active || !target || !step.endsWith("menu")) { setMobileMenuPlacement(null); return; }
    /** positionMobileMenu 在控制台手机菜单目标的上下空白中放置说明，避免提示卡压住真实点击位置。 */
    function positionMobileMenu() {
      if (window.innerWidth >= 900) { setMobileMenuPlacement(null); return; }
      const box = target!.getBoundingClientRect();
      const below = window.innerHeight - box.bottom;
      const above = box.top - (progressBarRef.current?.getBoundingClientRect().height || 0);
      setMobileMenuPlacement(below >= 260 || below >= above ? "bottom" : "top");
    }
    positionMobileMenu();
    window.addEventListener("resize", positionMobileMenu);
    window.addEventListener("scroll", positionMobileMenu, true);
    window.addEventListener("transitionend", positionMobileMenu, true);
    return () => {
      window.removeEventListener("resize", positionMobileMenu);
      window.removeEventListener("scroll", positionMobileMenu, true);
      window.removeEventListener("transitionend", positionMobileMenu, true);
    };
  }, [active, target, step]);

  useLayoutEffect(() => {
    if (!active) return;
    const root = document.querySelector<HTMLElement>("[data-admin-root]");
    const progress = progressBarRef.current;
    if (!root || !progress) return;
    const previousActive = root.getAttribute("data-onboarding-active");
    const previousHeight = root.style.getPropertyValue("--goodhr-onboarding-height");
    const hadLayoutClass = root.classList.contains(styles.activeLayout);
    const overlayRoot = document.body;
    const previousOverlayTop = overlayRoot.style.getPropertyValue("--goodhr-onboarding-overlay-top");
    const hadOverlayClass = overlayRoot.classList.contains(styles.activeOverlayLayout);
    root.setAttribute("data-onboarding-active", "true");
    root.classList.add(styles.activeLayout);
    overlayRoot.classList.add(styles.activeOverlayLayout);
    /** measureProgressHeight 只测量当前控制台进度条，让菜单工具栏和正文避让真实高度。 */
    function measureProgressHeight() {
      root!.style.setProperty("--goodhr-onboarding-height", `${Math.ceil(progress!.getBoundingClientRect().height)}px`);
      overlayRoot.style.setProperty("--goodhr-onboarding-overlay-top", `${Math.ceil(progress!.getBoundingClientRect().bottom) + 12}px`);
    }
    measureProgressHeight();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measureProgressHeight);
    observer?.observe(progress);
    window.addEventListener("resize", measureProgressHeight);
    return () => {
      observer?.disconnect();
      window.removeEventListener("resize", measureProgressHeight);
      if (previousActive === null) root.removeAttribute("data-onboarding-active");
      else root.setAttribute("data-onboarding-active", previousActive);
      if (previousHeight) root.style.setProperty("--goodhr-onboarding-height", previousHeight);
      else root.style.removeProperty("--goodhr-onboarding-height");
      if (!hadLayoutClass) root.classList.remove(styles.activeLayout);
      if (previousOverlayTop) overlayRoot.style.setProperty("--goodhr-onboarding-overlay-top", previousOverlayTop);
      else overlayRoot.style.removeProperty("--goodhr-onboarding-overlay-top");
      if (!hadOverlayClass) overlayRoot.classList.remove(styles.activeOverlayLayout);
    };
  }, [active]);

  useEffect(() => {
    if (phase !== "skip") setSkipText("");
  }, [phase]);

  useEffect(() => {
    if (!celebration) return;
    if (!window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      const themeRoot = document.querySelector("[data-admin-root]") || document.documentElement;
      const primary = getComputedStyle(themeRoot).getPropertyValue("--goodhr-primary").trim() || "#159a62";
      void confetti({ particleCount: 42, spread: 62, startVelocity: 23, gravity: 1.05, ticks: 125, origin: { x: 0.7, y: 0.35 }, colors: [primary, "#d6b66e", "#c6d9cc"], disableForReducedMotion: true, zIndex: 1800 });
    }
    const timer = window.setTimeout(() => actionsRef.current.dismissCelebration(), 1800);
    return () => window.clearTimeout(timer);
  }, [celebration?.id]);

  if (phase === "loading" || phase === "silent") return null;
  const operationPanel = active && !blocked && operationSteps.includes(step) ? <OperationPanel {...props} /> : null;

  return (
    <div data-testid="onboarding-experience" data-step={step}>
      {active ? (
        <section ref={progressBarRef} className={styles.progressBar} style={blocked ? { zIndex: 900 } : undefined} aria-label="新手教程总进度">
          <Flex gap={10} align="center" justify="space-between" wrap>
            <div className={styles.progressTitle}>
              <Typography.Text strong>上手进度 {doneCount}/5</Typography.Text>
              <Typography.Text className={styles.currentStep}>{blocked ? "先处理当前必要检查，进度已保存" : title}</Typography.Text>
            </div>
            <Flex gap={6} align="center" wrap>
              <Typography.Text type="secondary" className={styles.remaining}><ClockCircleOutlined aria-hidden="true" /> 预计还需 {remaining}</Typography.Text>
              <Button type="text" icon={<QuestionCircleOutlined />} disabled={blocked} onClick={actions.openHelp}>遇到问题</Button>
              <Button type="text" icon={<PauseOutlined />} onClick={actions.pause}>稍后继续</Button>
            </Flex>
          </Flex>
          <Progress percent={Math.max(0, Math.min(100, doneCount * 20))} showInfo={false} size="small" strokeColor="var(--goodhr-primary)" className={styles.progressLine} />
          <ol className={styles.stageList}>
            {ONBOARDING_STAGES.map((name, index) => (
              <li key={name} className={stageDone[index] ? styles.stageCompleted : index === stage ? styles.stageCurrent : undefined} aria-current={index === stage ? "step" : undefined}>
                {stageDone[index] ? <CheckCircleFilled aria-hidden="true" /> : <span className={styles.stageNumber}>{index + 1}</span>}
                <span>{name}</span>
              </li>
            ))}
          </ol>
        </section>
      ) : null}

      {operationPanel}
      {active && !operationPanel && (!target || blocked) ? (
        <section className={`${styles.operationPanel} ${styles.passivePanel}`} data-testid="onboarding-target-loading" aria-live="polite">
          <Typography.Text strong>{blocked ? "先处理当前必要检查，进度已保存" : "正在定位操作位置"}</Typography.Text>
          <Typography.Paragraph type="secondary">{blocked ? "完成当前弹框里的检查后，我会从这一步继续带路。" : error || statusText || "当前页面准备好后，我会继续高亮操作位置。你也可以先按说明操作。"}</Typography.Paragraph>
          <Flex gap={8} wrap><Button disabled={blocked} onClick={actions.openHelp}>遇到问题</Button><Button onClick={actions.pause}>稍后继续</Button></Flex>
        </section>
      ) : null}

      {active && storageUnavailable ? <div className={styles.storageNotice} role="status">这次浏览器没存住进度，当前页面可以继续；刷新后可能需要重新带路。</div> : null}

      <Tour
        key={`${step}:${targetGeometry}`}
        rootClassName={styles.tourPopup}
        open={active && Boolean(target) && !helpOpen && !blocked}
        current={0}
        steps={[{
          target,
          title: <span data-testid="onboarding-tour" data-step={step}>{title}</span>,
          description: <div><Typography.Paragraph>{description}</Typography.Paragraph>{step === "platform-login" ? <Flex gap={8} align="center" className={styles.loginStatus} role="status"><Image src="/assets/platforms/camoufox.png" alt="橙色狐狸浏览器图标" width={28} height={28} preview={false} /><Typography.Text type="secondary"><Spin size="small" /> {statusText || "我在等你完成平台登录。"}</Typography.Text></Flex> : null}<Typography.Paragraph type="secondary" className={styles.tourHint}>{hint}</Typography.Paragraph>{error && !["position-start-error", "position-start-update"].includes(step) ? <Alert type="warning" showIcon title={error} className={styles.helpAlert} /> : null}<Typography.Text type="secondary" className={styles.tourEstimate}>{estimate} · 完成操作后，我会自动接着带路。</Typography.Text></div>,
          placement: operationPanel ? "top" : step.endsWith("menu") ? mobileMenuPlacement || "right" : "bottom",
        }]}
        disabledInteraction={false}
        keyboard={false}
        mask={{ color: "rgba(20, 31, 25, .4)" }}
        gap={{ offset: 7, radius: 8 }}
        zIndex={1500}
        scrollIntoViewOptions={{ block: "nearest", inline: "nearest", behavior: "instant" }}
        onClose={actions.pause}
        indicatorsRender={() => null}
        actionsRender={() => <Flex gap={6} wrap><Button type="text" onClick={actions.back}>上一步</Button><Button type="text" onClick={actions.pause}>稍后继续</Button><Button onClick={actions.openHelp}>遇到问题</Button></Flex>}
        styles={{ section: { maxWidth: "min(390px, calc(100vw - 28px))" }, title: { color: "var(--goodhr-text)" } }}
      />

      <Modal
        open={phase === "invite" && !blocked}
        title={<span data-testid="onboarding-welcome">新同事，欢迎上岗。</span>}
        width={520}
        closable={false}
        keyboard={false}
        mask={{ closable: false }}
        footer={<Flex justify="flex-end" gap={8} wrap><Button onClick={actions.requestSkip}>我非新用户，无需教程</Button><Button type="primary" onClick={actions.accept}>好，带我跑一遍</Button></Flex>}
      >
        <Typography.Paragraph className={styles.welcomeCopy}>强烈建议跟我跑一遍：从打开浏览器到筛完第一位候选人，有我带路，少一点“接下来点哪儿”。</Typography.Paragraph>
        <div className={styles.welcomeFacts}><span>5 个阶段 · 进度自动保存</span><span>预计 {remaining}</span></div>
      </Modal>

      <Modal
        open={phase === "skip" && !blocked}
        title="我再小声挽留一下"
        width={500}
        onCancel={actions.cancelSkip}
        footer={<Flex justify="flex-end" gap={8} wrap><Button onClick={actions.cancelSkip}>还是跟着教程走</Button><Button type="primary" disabled={skipText !== ONBOARDING_SKIP_PHRASE} onClick={actions.confirmSkip}>确认跳过</Button></Flex>}
      >
        <Typography.Paragraph>教程会带你跑到第一条真实筛选结果。确定不用带路的话，请完整输入下面这句话：</Typography.Paragraph>
        <Typography.Paragraph strong className={styles.skipPhrase}>{ONBOARDING_SKIP_PHRASE}</Typography.Paragraph>
        <Input aria-label="输入跳过教程确认文字" value={skipText} onChange={(event) => setSkipText(event.target.value)} placeholder={ONBOARDING_SKIP_PHRASE} autoComplete="off" onPressEnter={() => { if (skipText === ONBOARDING_SKIP_PHRASE) actions.confirmSkip(); }} />
      </Modal>

      <Modal
        open={active && helpOpen && !blocked}
        zIndex={1700}
        title={`这一步怎么做：${title}`}
        width={560}
        onCancel={actions.closeHelp}
        footer={<Button type="primary" onClick={actions.closeHelp}>我再试试</Button>}
      >
        <Typography.Paragraph>{description}</Typography.Paragraph>
        <Typography.Paragraph type="secondary">{hint}</Typography.Paragraph>
        {error || blocked ? <Alert type="warning" showIcon title="我目前看到的卡点" description={error || statusText || "先按当前页面提示完成检查，再回来继续。"} className={styles.helpAlert} /> : null}
        {step === "agent-connect" ? <div><Typography.Paragraph>启动本地 GoodHR 程序后，如果还连不上，点击地址栏左侧的网站设置，允许“设备上的应用”，再检查一次连接。</Typography.Paragraph><ClickableImagePreview src={permissionImage} alt="Chrome 设备上的应用权限开关位置" /></div> : null}
        {step === "first-result" ? <Typography.Paragraph>“不合适，已跳过”也是筛选结果。还没判断完、只扫描到了候选人，都不会算教程完成。岗位失败时，可以先看日志里的具体提示。</Typography.Paragraph> : null}
      </Modal>

      {phase === "paused" || phase === "declined" ? (
        <div className={styles.resumeEntry}><Typography.Text type="secondary">{phase === "paused" ? "教程进度给你留着了" : "需要带路时，我还在"}</Typography.Text><Button type="primary" onClick={actions.resume}>{phase === "paused" ? "继续教程" : "开启引导"}</Button></div>
      ) : null}

      {celebration && phase !== "completed" ? <div className={styles.celebrationToast} role="status" aria-live="polite"><CheckCircleFilled aria-hidden="true" /><div><strong>{celebration.title}</strong><span>{celebration.description}</span></div></div> : null}

      <Modal open={phase === "completed" && !blocked} width={500} title={<Flex gap={10} align="center"><TrophyOutlined className={styles.trophy} />首轮跑通，今天你赢了</Flex>} onCancel={actions.finishClose} footer={<Button type="primary" onClick={actions.finishClose}>开始正式使用</Button>}>
        <Typography.Paragraph>第一条真实筛选结果已经出来了。从本地程序、平台登录，到创建岗位和开始筛选，关键流程你已经走过一遍。</Typography.Paragraph>
        {statusText ? <Alert type="success" showIcon title={statusText} className={styles.helpAlert} /> : null}
        {error ? <Alert type="warning" showIcon title={error} className={styles.helpAlert} /> : null}
        <Typography.Paragraph type="secondary">接下来可以调整岗位条件，继续招人。正在运行的岗位会继续工作，你随时可以在岗位管理中停止。</Typography.Paragraph>
        <Progress percent={100} strokeColor="var(--goodhr-primary)" />
      </Modal>
    </div>
  );
}

/** OperationPanel 展示当前步骤需要的真实操作按钮，不提供跳过业务判断的下一步按钮。 */
function OperationPanel({ step, state, title, description, statusText, error, busy, availablePlatforms, platformsLoading, actions }: OnboardingExperienceProps) {
  if (!operationSteps.includes(step)) return null;
  return (
    <section className={styles.operationPanel} aria-label="当前教程操作" aria-live="polite">
      <Typography.Title level={5} className={styles.panelTitle}>{title}</Typography.Title>
      <Typography.Paragraph type="secondary" className={styles.panelDescription}>{description}</Typography.Paragraph>
      {step === "platform-select" ? <div data-onboarding="onboarding-platform-choice"><ChoiceCards label="请选择你使用的平台" value="" options={platformChoices.map((choice) => ({ ...choice, disabled: platformsLoading || !availablePlatforms.includes(choice.value), description: platformsLoading ? "正在读取开放状态" : availablePlatforms.includes(choice.value) ? choice.description : "暂未开放，请选择其他可用平台" }))} compact columns={2} onChange={(value) => actions.choosePlatform(String(value))} />{error ? <Alert type="warning" showIcon title={error} className={styles.panelStatus} /> : null}{error || (!platformsLoading && availablePlatforms.length === 0) ? <Button loading={platformsLoading} onClick={actions.refreshPlatforms} style={{ marginTop: 8 }}>重新读取平台</Button> : null}</div> : null}
      {step === "agent-download" ? <Button data-onboarding="onboarding-agent-download" type="primary" onClick={actions.downloadAgent}>下载本地程序</Button> : null}
      {step === "agent-connect" ? <div><Button data-onboarding="onboarding-agent-connect" type="primary" loading={busy} onClick={actions.checkAgent}>检查连接</Button><Typography.Paragraph type="secondary" className={styles.connectHint}>程序已经启动仍连不上？点击“遇到问题”查看浏览器权限图。</Typography.Paragraph></div> : null}
      {step === "first-result" ? <div data-onboarding="onboarding-result-wait"><Flex gap={8} align="center"><Spin size="small" /><Typography.Text>{statusText || "等待第一条真实筛选判断"}</Typography.Text></Flex><Typography.Paragraph type="secondary" className={styles.connectHint}>筛掉不合适的人也算正常结果，进度会按真实判断更新。</Typography.Paragraph></div> : null}
      {statusText && !["platform-login", "first-result"].includes(step) ? <Typography.Paragraph type="secondary" className={styles.panelStatus}>{statusText}</Typography.Paragraph> : null}
      {error && step !== "platform-select" ? <Alert type="warning" showIcon title={error} className={styles.panelStatus} /> : null}
    </section>
  );
}
