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
/** 日志追加回调类型（追加到最后一行末尾） */
export type AppendLogCallback = (suffix: string, type?: string) => void;

class Orchestrator {
  isRunning: boolean;
  strategy: Strategy | null;
  matchCount: number;
  matchLimit: number;
  scrollDelayMin: number;
  scrollDelayMax: number;
  enableSound: boolean;
  onLog: LogCallback | null;
  onAppendLog: AppendLogCallback | null;
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
    this.onAppendLog = null;
  }

  /**
   * 启动免费模式
   * @param data - 运行参数
   * @param onLog - 日志回调
   * @param onAppendLog - 日志追加回调
   */
  async startFreeMode(
    data: RunData,
    onLog: LogCallback,
    onAppendLog: AppendLogCallback,
  ): Promise<void> {
    if (this.isRunning) return;

    const platform = await bridge.detectCurrentPlatform();
    if (!platform) {
      throw new Error("未识别当前招聘平台，请确认已打开招聘网站");
    }

    const pageCheck = await bridge.checkPageValidity();
    if (!pageCheck.valid && pageCheck.page) {
      throw new Error(
        `请前往${platform.name}的「${pageCheck.page.title}」页面使用插件`,
      );
    }

    const alive = await bridge.ping();
    if (!alive) {
      throw new Error("注入脚本未就绪，请刷新页面后重试");
    }

    this.strategy = resolveStrategy(platform.id, false);
    this.onLog = onLog;
    this.onAppendLog = onAppendLog;
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
   * @param onAppendLog - 日志追加回调
   */
  async startAIMode(
    data: RunData,
    onLog: LogCallback,
    onAppendLog: AppendLogCallback,
  ): Promise<void> {
    if (this.isRunning) return;

    if (!data.jobDescription || !data.jobDescription.trim()) {
      throw new Error("AI模式需要填写岗位说明");
    }

    const platform = await bridge.detectCurrentPlatform();
    if (!platform) {
      throw new Error("未识别当前招聘平台，请确认已打开招聘网站");
    }

    const pageCheck = await bridge.checkPageValidity();
    if (!pageCheck.valid && pageCheck.page) {
      throw new Error(
        `请前往${platform.name}的「${pageCheck.page.title}」页面使用插件`,
      );
    }

    const alive = await bridge.ping();
    if (!alive) {
      throw new Error("注入脚本未就绪，请刷新页面后重试");
    }

    this.strategy = resolveStrategy(platform.id, true);
    this.onLog = onLog;
    this.onAppendLog = onAppendLog;
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
   * 发送日志（新增一行）
   * @param message - 日志内容
   * @param type - 日志类型
   */
  _log(message: string, type = "info"): void {
    if (this.onLog) this.onLog(message, type);
  }

  /**
   * 追加文本到最后一行日志末尾（用于同一条日志的进度更新）
   * @param suffix - 追加文本
   * @param type - 可选，同时更新日志类型
   */
  _appendLog(suffix: string, type?: string): void {
    if (this.onAppendLog) this.onAppendLog(suffix, type);
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
        // 检查是否已达到匹配上限，达到则自动停止
        if (this.matchCount >= this.matchLimit) {
          this._log(`已达到匹配限制 ${this.matchLimit}，自动停止`, "warning");
          this.stop();
          return;
        }

        // 检查是否有新消息（如 Boss 直聘的聊天回复），优先处理新消息
        const hasMessage = await bridge.checkNewMessage();
        // if (hasMessage) {
        //   this._log("检测到新消息，优先处理", "info");
        //   continue;
        // }

        // 在当前页面查找下一个候选人卡片，找不到则等待后重试
        const candidate = await bridge.findNextCandidate();
        if (!candidate) {
          await this._waitRandomDelay();
          continue;
        }

        // 提取候选人信息（姓名、简介等），用于后续筛选判断
        const info = await bridge.extractCandidateInfo(candidate.elementId);
        const candidateInfo = info.info || candidate.info;
        const candidateName = info.name || "";
        candidate.name = candidateName;

        // 输出筛选起始日志，后续结果会追加到这一行末尾
        this._log(`正在筛选 ${candidateName || "未知"}`, "info");

        const strategy = this.strategy!;
        let shouldGreet = false;
        let greetReason = "";

        try {
          // 粗筛：基于关键词等快速判断候选人是否符合要求
          const coarse = await strategy.coarseFilter(this, candidateInfo);

          if (!coarse.pass) {
            // 粗筛未通过，尝试兜底筛选（如特殊规则放宽条件）
            let fallbackPass = false;
            try {
              fallbackPass = await strategy.fallbackFilter(this, candidateInfo);
            } catch (error: any) {
              this._log(`兜底筛选异常: ${error.message}`, "error");
            }

            if (fallbackPass) {
              // 兜底筛选通过，继续后续流程
              shouldGreet = true;
              greetReason = coarse.reason;
            } else {
              // 粗筛和兜底都不通过，追加结果到当前日志行
              this._appendLog(` → 未通过(${coarse.reason})`, "warning");
              continue;
            }
          }

          // 精筛前的数据准备：判断策略是否需要打开候选人详情页获取更多信息
          let detailedInfo = candidateInfo;
          let detailOpened = false;
          const platform = bridge.getCurrentPlatform();
          const needsDetail = strategy.needsDetailPage();

          // 如果策略需要详情页数据，则打开候选人详情页并提取详细信息
          if (needsDetail && platform) {
            try {
              const detailResponse = await bridge.openCandidateDetail(
                candidate.elementId,
              );

              if (detailResponse.opened) {
                detailOpened = true;
                // 等待详情页加载完成，模拟人类浏览行为
                await this._waitRandomDelay();
                detailedInfo = detailResponse.detailedInfo || candidateInfo;
              }
            } catch (error: any) {
              this._log(`打开详情页异常: ${error.message}`, "error");
            }
          }

          try {
            // 精筛：基于完整信息（含详情页数据）做最终判断
            const fine = await strategy.fineFilter(this, detailedInfo);

            if (fine.pass) {
              // 精筛通过，追加结果到当前日志行
              shouldGreet = true;
              greetReason = fine.reason;
              this._appendLog(` → 筛选通过(${greetReason})`, "success");
            } else {
              // 精筛未通过，追加结果到当前日志行
              this._appendLog(` → 未通过(${fine.reason})`, "warning");
            }
          } catch (error: any) {
            this._log(`精筛异常: ${error.message}`, "error");
          }

          // 如果打开了详情页，精筛结束后关闭它，回到候选人列表
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

        // 筛选未通过，继续处理下一位候选人
        if (!shouldGreet) continue;

        // 再次检查匹配上限（精筛期间匹配数可能已变化）
        if (this.matchCount >= this.matchLimit) {
          this._log(`匹配成功但已达到限制 ${this.matchLimit}，停止`, "warning");
          this.stop();
          return;
        }

        // 追加打招呼状态到当前筛选日志行
        this._appendLog(` → 已打招呼(${greetReason})`, "success");

        // 点击打招呼按钮，向候选人发送问候
        let greetSuccess = false;
        try {
          greetSuccess = await bridge.clickGreet(candidate.elementId);
        } catch (error: any) {
          this._log(`打招呼异常: ${error.message}`, "error");
        }

        // 打招呼失败，跳过后续操作
        if (!greetSuccess) continue;

        // 打招呼成功后，尝试索要候选人联系方式（微信/手机号）
        try {
          await bridge.collectContact(candidate.elementId);
        } catch (error: any) {
          this._log(`索要联系方式异常: ${error.message}`, "error");
        }

        // 匹配计数加一，记录打招呼成功日志
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
 * @param onAppendLog - 日志追加回调
 */
export async function startFreeRun(
  data: RunData,
  onLog: LogCallback,
  onAppendLog: AppendLogCallback,
): Promise<void> {
  await orchestrator.startFreeMode(data, onLog, onAppendLog);
}

/**
 * 启动AI模式运行
 * @param data - 运行参数
 * @param onLog - 日志回调
 * @param onAppendLog - 日志追加回调
 */
export async function startAIRun(
  data: RunData,
  onLog: LogCallback,
  onAppendLog: AppendLogCallback,
): Promise<void> {
  await orchestrator.startAIMode(data, onLog, onAppendLog);
}

/**
 * 停止运行
 */
export function stopRun(): void {
  orchestrator.stop();
}

export { Orchestrator };
