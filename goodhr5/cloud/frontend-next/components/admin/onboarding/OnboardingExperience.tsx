/** 本文件负责新手邀请、真实操作遮罩、阶段进度、针对性帮助和轻量完成鼓励。 */
"use client";

import { Alert, Button, Flex, Image, Modal, Progress, Spin, Tag, Tour, Typography } from "antd";
import { CheckCircleFilled, ClockCircleOutlined, ExclamationCircleOutlined, QuestionCircleOutlined, TrophyOutlined } from "@ant-design/icons";
import confetti from "canvas-confetti";
import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { OnboardingExperienceProps } from "@/lib/onboarding-view";
import { ONBOARDING_STAGES, onboardingStage } from "@/lib/onboarding-state";
import { AUTHOR_CONTACT } from "@/lib/author-contact";
import ClickableImagePreview from "../ClickableImagePreview";
import styles from "./OnboardingExperience.module.css";

const permissionImage = "/assets/help/local-agent-device-app-permission.png";
const operationSteps = ["agent-download", "agent-connect", "first-result"];

/** OnboardingExperience 只展示控制器状态，推进步骤必须由真实操作结果决定。 */
export default function OnboardingExperience(props: OnboardingExperienceProps) {
  const { phase, state, step, target, targetGeometry, title, description, hint, estimate, remaining, doneCount, statusText, error, busy, helpOpen, restartOpen, restartBlockedReason, formSaving, blocked, storageUnavailable, celebration, actions } = props;
  const [mobileMenuPlacement, setMobileMenuPlacement] = useState<"top" | "bottom" | null>(null);
  const [modePopupLayout, setModePopupLayout] = useState<{ placement: "top" | "bottom"; maxHeight: number } | null>(null);
  const actionsRef = useRef(actions);
  const progressBarRef = useRef<HTMLElement | null>(null);
  actionsRef.current = actions;
  const stage = onboardingStage(step);
  const active = phase === "active";
  const stageDone = [state.environmentReady, Boolean(state.positionId) && !state.reviewPosition, state.platformLoggedIn && !state.reviewPosition, state.positionStarted, state.status === "completed"];

  useLayoutEffect(() => {
    if (!active || !target || !["position-filter-mode", "first-result"].includes(step)) { setModePopupLayout(null); return; }
    let frame = 0;
    /** measureModePopup 按目标和固定进度条的真实空位限制费用及结果气泡，标题和操作始终留在滚动区外。 */
    function measureModePopup() {
      const box = target!.getBoundingClientRect();
      const progressBottom = progressBarRef.current?.getBoundingClientRect().bottom || 0;
      // 为遮罩孔边距、气泡箭头及视口边缘留出空间，不能只靠 Tour 的视口翻转避让进度条。
      const gap = 24;
      const above = Math.max(0, box.top - progressBottom - gap);
      const below = Math.max(0, window.innerHeight - box.bottom - gap);
      const placement = above > below ? "top" : "bottom";
      const maxHeight = Math.floor(Math.min(520, placement === "top" ? above : below));
      setModePopupLayout((current) => current?.placement === placement && current.maxHeight === maxHeight ? current : { placement, maxHeight });
    }
    /** scheduleModePopup 合并滚动、弹框动画和尺寸通知，避免同一帧重复测量引导气泡。 */
    function scheduleModePopup() { cancelAnimationFrame(frame); frame = requestAnimationFrame(measureModePopup); }
    measureModePopup();
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(scheduleModePopup);
    observer?.observe(target);
    if (progressBarRef.current) observer?.observe(progressBarRef.current);
    window.addEventListener("resize", scheduleModePopup);
    window.addEventListener("scroll", scheduleModePopup, true);
    window.addEventListener("transitionend", scheduleModePopup, true);
    return () => {
      cancelAnimationFrame(frame); observer?.disconnect();
      window.removeEventListener("resize", scheduleModePopup);
      window.removeEventListener("scroll", scheduleModePopup, true);
      window.removeEventListener("transitionend", scheduleModePopup, true);
    };
  }, [active, target, targetGeometry, step]);

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
    if (!celebration) return;
    if (!window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      const themeRoot = document.querySelector("[data-admin-root]") || document.documentElement;
      const primary = getComputedStyle(themeRoot).getPropertyValue("--goodhr-primary").trim() || "#159a62";
      void confetti({ particleCount: 42, spread: 62, startVelocity: 23, gravity: 1.05, ticks: 125, origin: { x: 0.7, y: 0.35 }, colors: [primary, "#d6b66e", "#c6d9cc"], disableForReducedMotion: true, zIndex: 1800 });
    }
    const timer = window.setTimeout(() => actionsRef.current.dismissCelebration(), 1800);
    return () => window.clearTimeout(timer);
  }, [celebration?.id]);

  if (phase === "loading" || (phase === "silent" && !restartOpen)) return null;
  const operationPanel = active && !blocked && !props.resultLogsOpen && operationSteps.includes(step) ? <OperationPanel {...props} /> : null;
  const simulationNotice = process.env.NEXT_PUBLIC_ONBOARDING_PREVIEW_MODE === "simulation" ? <Alert data-testid="onboarding-simulation-notice" type="warning" showIcon title="这是自动化模拟环境，不会打开真实招聘浏览器。" description={<Typography.Link href="http://127.0.0.1:5174/admin">打开真实浏览器验收</Typography.Link>} className={styles.helpAlert} /> : null;

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
              <Button type="text" disabled={blocked} onClick={actions.requestRestart}>从头开始</Button>
              <Button type="text" icon={<QuestionCircleOutlined />} disabled={blocked} onClick={actions.openHelp}>遇到问题</Button>
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
      {active && !operationPanel && !helpOpen && !restartOpen && !props.resultLogsOpen && (!target || blocked) ? (
        <section className={`${styles.operationPanel} ${styles.passivePanel}`} data-testid="onboarding-target-loading" aria-live="polite">
          <Typography.Text strong>{blocked ? "先处理当前必要检查，进度已保存" : "正在定位操作位置"}</Typography.Text>
          <Typography.Paragraph type="secondary">{blocked ? "完成当前弹框里的检查后，我会从这一步继续带路。" : error || statusText || "当前页面准备好后，我会继续高亮操作位置。你也可以先按说明操作。"}</Typography.Paragraph>
          <Flex gap={8} wrap><Button disabled={blocked} onClick={actions.openHelp}>遇到问题</Button></Flex>
        </section>
      ) : null}

      {active && storageUnavailable ? <div className={styles.storageNotice} role="status">这次浏览器没存住进度，当前页面可以继续；刷新后可能需要重新带路。</div> : null}

      <Tour
        key={`${step}:${targetGeometry}`}
        rootClassName={styles.tourPopup}
        open={active && Boolean(target) && !helpOpen && !restartOpen && !blocked && !props.resultLogsOpen}
        current={0}
        steps={[{
          target,
          title: <span data-testid="onboarding-tour" data-step={step}>{title}</span>,
          description: <div tabIndex={modePopupLayout ? 0 : undefined} data-testid={step === "position-filter-mode" ? "onboarding-mode-description" : undefined}>{step === "platform-login" ? simulationNotice : null}<Typography.Paragraph>{description}</Typography.Paragraph>{step === "position-filter-mode" ? <FilterModeExplanation /> : null}{step === "platform-login" && !error ? <Flex gap={8} align="center" className={styles.loginStatus} role="status"><Image src="/assets/platforms/camoufox.png" alt="橙色狐狸浏览器图标" width={28} height={28} preview={false} /><Typography.Text type="secondary"><Spin size="small" /> {statusText || "我在等你完成平台登录。"}</Typography.Text></Flex> : null}<Typography.Paragraph type="secondary" className={styles.tourHint}>{hint}</Typography.Paragraph>{error && !["position-start-error", "position-start-update", "first-result"].includes(step) ? <Alert type="warning" showIcon title={error} className={styles.helpAlert} /> : null}{step !== "first-result" || props.resultState === "waiting" ? <Typography.Text type="secondary" className={styles.tourEstimate}>{estimate} · 完成操作后，我会自动接着带路。</Typography.Text> : null}</div>,
          placement: modePopupLayout?.placement || (operationPanel ? "top" : step.endsWith("menu") ? mobileMenuPlacement || "right" : "bottom"),
        }]}
        disabledInteraction={false}
        keyboard={false}
        mask={{ color: "rgba(20, 31, 25, .4)" }}
        gap={{ offset: 7, radius: 8 }}
        zIndex={1500}
        scrollIntoViewOptions={{ block: "nearest", inline: "nearest", behavior: "instant" }}
        closable={false}
        indicatorsRender={() => null}
        actionsRender={() => <Flex gap={6} wrap>{step !== "first-result" ? <Button type="text" disabled={formSaving} onClick={actions.back}>上一步</Button> : null}<Button onClick={actions.openHelp}>遇到问题</Button></Flex>}
        classNames={modePopupLayout ? { section: styles.modeTourSection, header: styles.modeTourHeader, description: styles.modeTourDescription, footer: styles.modeTourFooter } : undefined}
        styles={{ section: { maxWidth: "min(390px, calc(100vw - 28px))", ...(modePopupLayout ? { maxHeight: modePopupLayout.maxHeight } : {}) }, title: { color: "var(--goodhr-text)" } }}
      />

      <Modal
        open={phase === "invite" && !blocked}
        title={<span data-testid="onboarding-welcome">新同事，欢迎上岗。</span>}
        width={520}
        closable={false}
        keyboard={false}
        mask={{ closable: false }}
        footer={<Flex justify="flex-end"><Button type="primary" onClick={actions.accept}>好，带我跑一遍</Button></Flex>}
      >
        {simulationNotice}
        <Typography.Paragraph className={styles.welcomeCopy}>强烈建议跟我跑一遍：先建岗位，再从“开始”弹框打开平台、登录并设好筛选条件，直到筛完第一位候选人。有我带路，少一点“接下来点哪儿”。</Typography.Paragraph>
        <div className={styles.welcomeFacts}><span>5 个阶段 · 进度自动保存</span><span>预计 {remaining}</span></div>
      </Modal>

      <Modal
        open={active && helpOpen && !blocked}
        zIndex={1700}
        title={`这一步怎么做：${title}`}
        width={560}
        centered
        styles={{ body: { maxHeight: "min(560px, calc(100dvh - 220px))", overflowY: "auto", overscrollBehavior: "contain" } }}
        onCancel={actions.closeHelp}
        footer={<Flex justify="flex-end" gap={8} wrap><Button disabled={formSaving} onClick={actions.back}>上一步</Button><Button onClick={actions.requestRestart}>从头开始</Button><Button type="primary" onClick={actions.closeHelp}>我再试试</Button></Flex>}
      >
        <Typography.Paragraph>{description}</Typography.Paragraph>
        {step === "position-filter-mode" ? <FilterModeExplanation /> : null}
        <Typography.Paragraph type="secondary">{hint}</Typography.Paragraph>
        {error || blocked ? <Alert type="warning" showIcon title="我目前看到的卡点" description={error || statusText || "先按当前页面提示完成检查，再回来继续。"} className={styles.helpAlert} /> : null}
        {step === "agent-connect" ? <div><Typography.Paragraph>启动本地 GoodHR 程序后，如果还连不上，点击地址栏左侧的网站设置，允许“设备上的应用”，再检查一次连接。</Typography.Paragraph><ClickableImagePreview src={permissionImage} alt="Chrome 设备上的应用权限开关位置" /></div> : null}
        {step === "first-result" ? <div className={styles.helpResultActions}><Typography.Paragraph>“不合适，已跳过”也是筛选结果。还没判断完、只扫描到候选人，都不会算教程完成。</Typography.Paragraph><ResultActions {...props} /></div> : null}
        <AuthorContact />
      </Modal>

      <Modal
        open={restartOpen && !blocked}
        zIndex={1700}
        title="从第一步再走一遍？"
        width={520}
        onCancel={actions.cancelRestart}
        footer={<Flex justify="flex-end" gap={8} wrap><Button onClick={actions.cancelRestart}>返回教程</Button>{state.positionStarted && props.resultState === "waiting" && restartBlockedReason ? <Button danger disabled={busy} onClick={actions.stopResult}>停止本轮</Button> : null}<Button type="primary" disabled={Boolean(restartBlockedReason)} onClick={actions.confirmRestart}>从头带我一遍</Button></Flex>}
      >
        <Typography.Paragraph>已保存的岗位和招聘数据都会保留；这次未保存的教程草稿会清空。已经建好的岗位，我带你重新检查，不用再建一份。</Typography.Paragraph>
        {restartBlockedReason ? <Alert type="warning" showIcon title={restartBlockedReason} /> : <Typography.Paragraph type="secondary">从邀请开始重新带路，已经准备好的本地组件会自动略过。</Typography.Paragraph>}
      </Modal>

      {celebration && phase !== "completed" ? <div className={styles.celebrationToast} role="status" aria-live="polite"><CheckCircleFilled aria-hidden="true" /><div><strong>{celebration.title}</strong><span>{celebration.description}</span></div></div> : null}

      <Modal open={phase === "completed" && !blocked} width={500} title={<Flex gap={10} align="center"><TrophyOutlined className={styles.trophy} />首轮跑通，今天你赢了</Flex>} onCancel={actions.finishClose} footer={<Button type="primary" onClick={actions.finishClose}>开始正式使用</Button>}>
        <Typography.Paragraph>第一条真实筛选结果已经出来了。从准备本地程序、创建岗位，到打开招聘浏览器登录和开始筛选，关键流程你已经走过一遍。</Typography.Paragraph>
        {statusText ? <Alert type="success" showIcon title={statusText} className={styles.helpAlert} /> : null}
        {error ? <Alert type="warning" showIcon title={error} className={styles.helpAlert} /> : null}
        <Typography.Paragraph type="secondary">接下来可以调整岗位条件，继续招人。正在运行的岗位会继续工作，你随时可以在岗位管理中停止。</Typography.Paragraph>
        <Progress percent={100} strokeColor="var(--goodhr-primary)" />
      </Modal>
    </div>
  );
}

/** FilterModeExplanation 在遮罩和帮助中突出免费、付费及对应场景，避免费用提示埋在灰字里。 */
function FilterModeExplanation() {
  return (
    <div className={styles.modeComparison} data-testid="onboarding-filter-mode-costs">
      <div><Tag color="success">基础筛选免费</Tag><Typography.Text strong>关键词筛选</Typography.Text><p>适合单一、简单的条件，按技能词、排除词匹配。</p></div>
      <div><Tag color="gold">付费模式</Tag><Typography.Text strong>AI 筛选</Typography.Text><p>适合多项、复杂要求，结合经历和岗位要求判断。</p><p className={styles.modeFee}>需有效会员；内置 AI 按用量扣余额，自定义接口按服务商规则计费。</p></div>
    </div>
  );
}

/** OperationPanel 展示当前步骤需要的真实操作按钮，不提供跳过业务判断的下一步按钮。 */
function OperationPanel(props: OnboardingExperienceProps) {
  const { step, title, description, statusText, error, busy, actions, resultState } = props;
  if (!operationSteps.includes(step)) return null;
  if (step === "first-result") return (
    <section className={`${styles.operationPanel} ${styles.resultPanel}`} data-onboarding="onboarding-result-wait" data-testid="onboarding-result-state" data-result-state={resultState} aria-label="当前教程操作" aria-live="polite">
      <Flex gap={8} align="center" className={styles.resultStatus}>
        {resultState === "waiting" ? <Spin size="small" /> : <ExclamationCircleOutlined className={styles.resultWarning} aria-hidden="true" />}
        <Typography.Text strong>{resultState === "ended" ? statusText || "本轮已结束，还没得到筛选结果" : resultState === "unavailable" ? "暂时没读到任务状态" : statusText || "等待第一条真实筛选判断"}</Typography.Text>
      </Flex>
      {error ? <Alert type="warning" showIcon title={error} className={styles.resultError} /> : <Typography.Paragraph type="secondary" className={styles.connectHint}>{resultState === "waiting" ? "筛掉不合适的人也算结果，进度按真实判断更新。" : resultState === "ended" ? "先看日志处理原因，再重试或修改这个岗位。" : "先重新检查，确认上一轮结束后再重试。"}</Typography.Paragraph>}
      <ResultActions {...props} />
    </section>
  );
  return (
    <section className={styles.operationPanel} aria-label="当前教程操作" aria-live="polite">
      <Typography.Title level={5} className={styles.panelTitle}>{title}</Typography.Title>
      <Typography.Paragraph type="secondary" className={styles.panelDescription}>{description}</Typography.Paragraph>
      {step === "agent-download" ? <Button data-onboarding="onboarding-agent-download" type="primary" onClick={actions.downloadAgent}>下载本地程序</Button> : null}
      {step === "agent-connect" ? <div><Button data-onboarding="onboarding-agent-connect" type="primary" loading={busy} onClick={actions.checkAgent}>检查连接</Button><Typography.Paragraph type="secondary" className={styles.connectHint}>程序已经启动仍连不上？点击“遇到问题”查看浏览器权限图。</Typography.Paragraph></div> : null}
      {statusText && !["platform-login", "first-result"].includes(step) ? <Typography.Paragraph type="secondary" className={styles.panelStatus}>{statusText}</Typography.Paragraph> : null}
      {error ? <Alert type="warning" showIcon title={error} className={styles.panelStatus} /> : null}
    </section>
  );
}

/** ResultActions 让任务结束、状态失联和仍在运行的情形分别使用真实恢复动作，不能跳过真实筛选结果。 */
function ResultActions({ resultState, busy, actions }: OnboardingExperienceProps) {
  return <Flex gap={8} wrap className={styles.resultActions}>
    {resultState === "ended" ? <><Button type="primary" loading={busy} onClick={actions.recoverResult}>处理好，重新开始</Button><Button disabled={busy} onClick={actions.reviewResultPosition}>修改岗位</Button></> : null}
    {resultState === "unavailable" ? <Button type="primary" loading={busy} onClick={actions.checkResult}>重新检查</Button> : null}
    <Button disabled={busy} onClick={actions.viewResultLogs}>查看错误日志</Button>
    {resultState !== "ended" ? <Button danger disabled={busy} onClick={actions.stopResult}>{resultState === "unavailable" ? "停止并确认状态" : "停止本轮"}</Button> : null}
  </Flex>;
}

/** AuthorContact 在每一步帮助中复用官网作者信息，微信二维码使用既有真实图片并支持放大。 */
function AuthorContact() {
  return <section className={styles.authorContact} data-testid="onboarding-author-contact" aria-label="联系作者">
    <div className={styles.authorQrCode}>
      <Image src={AUTHOR_CONTACT.wechatQrCode} alt={AUTHOR_CONTACT.wechatQrCodeAlt} width="100%" preview={{ mask: "放大二维码", zIndex: 1900 }} />
      <Typography.Text type="secondary">点图放大，微信找我</Typography.Text>
    </div>
    <div className={styles.authorDetails}>
      <Typography.Text strong>作者：{AUTHOR_CONTACT.name}</Typography.Text>
      <Typography.Text>微信 / 手机：<Typography.Link href={`tel:${AUTHOR_CONTACT.phone}`}>{AUTHOR_CONTACT.phone}</Typography.Link></Typography.Text>
      <Typography.Text>邮箱：<Typography.Link href={`mailto:${AUTHOR_CONTACT.email}`}>{AUTHOR_CONTACT.email}</Typography.Link></Typography.Text>
      <Typography.Paragraph type="secondary">把报错截图和岗位日志发给我，我来帮你看看。</Typography.Paragraph>
    </div>
  </section>;
}
