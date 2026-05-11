/**
 * 流程管理器（Orchestrator）— 扩展侧
 *
 * 运行在 Chrome 扩展侧（Vue 侧边栏 / background），不直接操作页面 DOM。
 * 所有页面操作通过 bridge 统一调度，bridge 根据平台配置翻译为 common.js 的原子操作。
 *
 * 核心设计：
 * 1. 流程编排在这里 → 具体操作在 bridge → 原子执行在 common.js
 * 2. 平台差异由策略对象决定，流程中不出现 if(platform)
 * 3. 主循环统一调度"主动打招呼"和"被动回复"，消息优先
 *
 * 本质流程：扫描 → 筛选（粗筛→精筛）→ 行动
 */

import { resolveStrategy } from "./strategies.js";
import type { Strategy, FilterResult, RunData } from "./strategies.js";
export type { RunData } from "./strategies.js";
import * as bridge from "./bridge.js";
import type { AIConfig } from "../constants/defaults.js";

/** 日志回调类型 */
export type LogCallback = (message: string, type: string) => void;

class Orchestrator {
  isRunning: boolean;
  strategy: Strategy | null;
  matchCount: number;
  matchLimit: number;
  scrollDelayMin: number;
  scrollDelayMax: number;
  enableSound: boolean;
  onLog: LogCallback | null;
  aiConfig?: AIConfig;
  jobDescription?: string;

  constructor() {
    this.isRunning = false;
    this.strategy = null;
    this.matchCount = 0;
    this.matchLimit = 200;
    this.scrollDelayMin = 3;
    this.scrollDelayMax = 5;
    this.enableSound = false;
    this.onLog = null;
  }

  /**
   * 启动免费模式
   * @param data - 运行参数
   * @param onLog - 日志回调
   */
  async startFreeMode(data: RunData, onLog: LogCallback): Promise<void> {
    if (this.isRunning) return;

    const platform = await bridge.detectCurrentPlatform();
    if (!platform) {
      onLog("未识别当前招聘平台，请确认已打开招聘网站", "error");
      return;
    }

    const alive = await bridge.ping();
    if (!alive) {
      onLog("注入脚本未就绪，请刷新页面后重试", "error");
      return;
    }

    this.strategy = resolveStrategy(platform.id, false);
    this.onLog = onLog;
    this.isRunning = true;
    this.matchCount = 0;
    this.matchLimit = data.matchLimit || 200;
    this.scrollDelayMin = data.scrollDelayMin || 3;
    this.scrollDelayMax = data.scrollDelayMax || 5;
    this.enableSound = data.enableSound || false;

    bridge.resetCandidateIndex();
    this._log(`已识别平台: ${platform.name}，免费模式启动`, "info");
    await this._runLoop();
  }

  /**
   * 启动AI模式
   * @param data - 运行参数
   * @param onLog - 日志回调
   */
  async startAIMode(data: RunData, onLog: LogCallback): Promise<void> {
    if (this.isRunning) return;

    if (!data.jobDescription || !data.jobDescription.trim()) {
      onLog("AI模式需要填写岗位说明", "error");
      return;
    }

    const platform = await bridge.detectCurrentPlatform();
    if (!platform) {
      onLog("未识别当前招聘平台，请确认已打开招聘网站", "error");
      return;
    }

    const alive = await bridge.ping();
    if (!alive) {
      onLog("注入脚本未就绪，请刷新页面后重试", "error");
      return;
    }

    this.strategy = resolveStrategy(platform.id, true);
    this.onLog = onLog;
    this.isRunning = true;
    this.matchCount = 0;
    this.matchLimit = data.matchLimit || 200;
    this.scrollDelayMin = data.scrollDelayMin || 3;
    this.scrollDelayMax = data.scrollDelayMax || 5;
    this.enableSound = data.enableSound || false;
    this.aiConfig = data.aiConfig;
    this.jobDescription = data.jobDescription;

    bridge.resetCandidateIndex();
    this._log(`已识别平台: ${platform.name}，AI模式启动`, "info");
    await this._runLoop();
  }

  /**
   * 停止主循环
   */
  stop(): void {
    this.isRunning = false;
    this.matchCount = 0;
    this._log("已停止", "warning");
  }

  /**
   * 发送日志
   * @param message - 日志内容
   * @param type - 日志类型
   */
  _log(message: string, type = "info"): void {
    if (this.onLog) this.onLog(message, type);
  }

  /**
   * 随机等待（秒级，模拟人类间隔）
   */
  async _waitRandomDelay(): Promise<void> {
    const delay = Math.floor(
      Math.random() * (this.scrollDelayMax - this.scrollDelayMin + 1) +
        this.scrollDelayMin,
    );
    this._log(`等待 ${delay} 秒...`, "info");
    await new Promise((resolve) => setTimeout(resolve, delay * 1000));
  }

  /**
   * 主循环：每轮完整地走一遍 "被动检测 → 扫描 → 筛选 → 行动"
   * 所有逻辑平铺在一个方法内，按顺序阅读即可理解完整流程
   */
  async _runLoop(): Promise<void> {
    while (this.isRunning) {
      try {
        if (this.matchCount >= this.matchLimit) {
          this._log(`已达到匹配限制 ${this.matchLimit}，自动停止`, "warning");
          this.stop();
          return;
        }

        const hasMessage = await bridge.checkNewMessage();
        if (hasMessage) {
          this._log("检测到新消息，优先处理", "info");
          continue;
        }

        const candidate = await bridge.findNextCandidate();
        if (!candidate) {
          await this._waitRandomDelay();
          continue;
        }

        const info = await bridge.extractCandidateInfo(candidate.elementId);
        const candidateInfo = info.info || candidate.info;
        const candidateName = info.name || "";
        candidate.name = candidateName;

        const strategy = this.strategy!;
        let shouldGreet = false;
        let greetReason = "";

        try {
          const coarse = await strategy.coarseFilter(this, candidateInfo);

          if (!coarse.pass) {
            let fallbackPass = false;
            try {
              fallbackPass = await strategy.fallbackFilter(this, candidateInfo);
            } catch (error: any) {
              this._log(`兜底筛选异常: ${error.message}`, "error");
            }

            if (fallbackPass) {
              shouldGreet = true;
              greetReason = coarse.reason;
            } else {
              await bridge.markElement(
                candidate.elementId,
                `未打招呼(${coarse.reason})`,
                "rejected",
              );
              continue;
            }
          }

          let detailedInfo = candidateInfo;
          let detailOpened = false;
          const platform = bridge.getCurrentPlatform();
          const needsDetail = strategy.needsDetailPage();

          if (needsDetail && platform) {
            try {
              const detailResponse = await bridge.openCandidateDetail(
                candidate.elementId,
              );

              if (detailResponse.opened) {
                detailOpened = true;
                await this._waitRandomDelay();
                detailedInfo = detailResponse.detailedInfo || candidateInfo;
              }
            } catch (error: any) {
              this._log(`打开详情页异常: ${error.message}`, "error");
            }
          }

          try {
            const fine = await strategy.fineFilter(this, detailedInfo);

            if (fine.pass) {
              shouldGreet = true;
              greetReason = fine.reason;
            } else {
              await bridge.markElement(
                candidate.elementId,
                `未打招呼(${fine.reason})`,
                "rejected",
              );
            }
          } catch (error: any) {
            this._log(`精筛异常: ${error.message}`, "error");
            await bridge.markElement(candidate.elementId, "精筛异常", "error");
          }

          if (detailOpened) {
            try {
              await bridge.closeCandidateDetail();
            } catch (error: any) {
              this._log(`关闭详情页异常: ${error.message}`, "error");
            }
          }
        } catch (error: any) {
          this._log(`筛选流程异常: ${error.message}`, "error");
        }

        if (!shouldGreet) continue;

        if (this.matchCount >= this.matchLimit) {
          this._log(`匹配成功但已达到限制 ${this.matchLimit}，停止`, "warning");
          this.stop();
          return;
        }

        await bridge.markElement(
          candidate.elementId,
          `已打招呼(${greetReason})`,
          "matched",
        );

        let greetSuccess = false;
        try {
          greetSuccess = await bridge.clickGreet(candidate.elementId);
        } catch (error: any) {
          this._log(`打招呼异常: ${error.message}`, "error");
        }

        if (!greetSuccess) continue;

        try {
          await bridge.collectContact(candidate.elementId);
        } catch (error: any) {
          this._log(`索要联系方式异常: ${error.message}`, "error");
        }

        this.matchCount++;
        this._log(
          `打招呼成功 ${this.matchCount}/${this.matchLimit} - ${candidateName || "未知"}`,
          "success",
        );

        if (this.enableSound) {
          this._playSound("notification2");
        }
      } catch (error: any) {
        this._log(`主循环异常: ${error.message}`, "error");
      }
    }
  }

  /**
   * 在扩展侧播放提示音
   * @param soundName - 音频文件名（不含扩展名）
   */
  _playSound(soundName: string): void {
    try {
      const audio = new Audio(chrome.runtime.getURL(`sounds/${soundName}.mp3`));
      audio.volume = 0.5;
      audio.play().catch(() => {});
    } catch {
      // ignore
    }
  }
}

const orchestrator = new Orchestrator();

/**
 * 启动免费模式运行
 * @param data - 运行参数
 * @param onLog - 日志回调
 */
export async function startFreeRun(
  data: RunData,
  onLog: LogCallback,
): Promise<void> {
  await orchestrator.startFreeMode(data, onLog);
}

/**
 * 启动AI模式运行
 * @param data - 运行参数
 * @param onLog - 日志回调
 */
export async function startAIRun(
  data: RunData,
  onLog: LogCallback,
): Promise<void> {
  await orchestrator.startAIMode(data, onLog);
}

/**
 * 停止运行
 */
export function stopRun(): void {
  orchestrator.stop();
}

export { Orchestrator };
