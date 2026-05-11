/**
 * 流程管理器（Orchestrator）— 扩展侧
 *
 * 运行在 Chrome 扩展侧（Vue 侧边栏 / background），不直接操作页面 DOM。
 * 所有页面操作通过 sendMessageToActiveTab 指挥注入侧 injector.js 执行。
 *
 * 核心设计：
 * 1. 流程编排在这里，具体操作在注入侧
 * 2. 平台差异由策略对象决定，流程中不出现 if(platform)
 * 3. 主循环统一调度"主动打招呼"和"被动回复"，消息优先
 *
 * 本质流程：扫描 → 筛选（粗筛→精筛）→ 行动
 */

import { sendMessageToActiveTab } from "../services/extension.js";
import { resolveStrategy } from "./strategies.js";

class Orchestrator {
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
   * @param {object} data - 运行参数
   * @param {object} parserInfo - 平台信息 { name }
   * @param {function} onLog - 日志回调
   */
  async startFreeMode(data, parserInfo, onLog) {
    if (this.isRunning) return;

    this.strategy = resolveStrategy(parserInfo.name, false);
    this.onLog = onLog;
    this.isRunning = true;
    this.matchCount = 0;
    this.matchLimit = data.matchLimit || 200;
    this.scrollDelayMin = data.scrollDelayMin || 3;
    this.scrollDelayMax = data.scrollDelayMax || 5;
    this.enableSound = data.enableSound || false;

    this._log("开始滚动（免费模式）", "info");
    await this._runLoop();
  }

  /**
   * 启动AI模式
   * @param {object} data - 运行参数
   * @param {object} parserInfo - 平台信息 { name }
   * @param {function} onLog - 日志回调
   */
  async startAIMode(data, parserInfo, onLog) {
    if (this.isRunning) return;

    if (!data.jobDescription || !data.jobDescription.trim()) {
      onLog("AI模式需要填写岗位说明", "error");
      return;
    }

    this.strategy = resolveStrategy(parserInfo.name, true);
    this.onLog = onLog;
    this.isRunning = true;
    this.matchCount = 0;
    this.matchLimit = data.matchLimit || 200;
    this.scrollDelayMin = data.scrollDelayMin || 3;
    this.scrollDelayMax = data.scrollDelayMax || 5;
    this.enableSound = data.enableSound || false;
    this.aiConfig = data.aiConfig;
    this.jobDescription = data.jobDescription;

    this._log("开始滚动（AI模式）", "info");
    await this._runLoop();
  }

  /**
   * 停止主循环
   */
  stop() {
    this.isRunning = false;
    this.matchCount = 0;
    this._sendCommand({ action: "STOP_SCROLL" });
    this._log("已停止", "warning");
  }

  /**
   * 发送日志
   * @param {string} message - 日志内容
   * @param {string} type - 日志类型
   */
  _log(message, type = "info") {
    if (this.onLog) this.onLog(message, type);
  }

  /**
   * 向注入侧发送指令并等待响应
   * @param {object} command - 指令对象
   * @returns {Promise<any>} 注入侧的响应
   */
  async _sendCommand(command) {
    try {
      return await sendMessageToActiveTab(command);
    } catch (error) {
      this._log(`指令发送失败: ${command.action} - ${error.message}`, "error");
      return null;
    }
  }

  /**
   * 随机等待（秒级，模拟人类间隔）
   */
  async _waitRandomDelay() {
    const delay = Math.floor(
      Math.random() * (this.scrollDelayMax - this.scrollDelayMin + 1) +
        this.scrollDelayMin,
    );
    await new Promise((resolve) => setTimeout(resolve, delay * 1000));
  }

  /**
   * 主循环：每轮完整地走一遍 "被动检测 → 扫描 → 筛选 → 行动"
   * 所有逻辑平铺在一个方法内，按顺序阅读即可理解完整流程
   */
  async _runLoop() {
    while (this.isRunning) {
      try {
        // ── 前置检查：是否达到限制 ──
        if (this.matchCount >= this.matchLimit) {
          this._log(`已达到匹配限制 ${this.matchLimit}，自动停止`, "warning");
          this.stop();
          return;
        }

        // ══════════════════════════════════════════
        // 第一步：检测被动消息（预留，暂不实现）
        // ══════════════════════════════════════════
        const messageCheck = await this._sendCommand({
          action: "CHECK_NEW_MESSAGE",
        });
        if (messageCheck?.hasMessage) {
          // TODO: 被动回复流程
          // 1. 点击消息入口 → 读历史 → AI生成回复 → 发送
          // 2. 返回候选人列表页
          continue;
        }

        // ══════════════════════════════════════════
        // 第二步：扫描 — 找下一个候选人
        // ══════════════════════════════════════════
        const scanResult = await this._sendCommand({
          action: "FIND_NEXT_CANDIDATE",
        });

        if (!scanResult?.found) {
          await this._waitRandomDelay();
          continue;
        }

        const candidate = scanResult.candidate;
        const strategy = this.strategy;
        const basicInfo = candidate.info || "";

        // ══════════════════════════════════════════
        // 第三步：粗筛 — 看卡片基本信息，决定是否值得打开详情
        // ══════════════════════════════════════════
        let shouldGreet = false;
        let greetReason = "";

        try {
          const coarse = await strategy.coarseFilter(this, basicInfo);

          if (!coarse.pass) {
            // 粗筛未通过，尝试兜底（如免费模式的关键词匹配）
            let fallbackPass = false;
            try {
              fallbackPass = await strategy.fallbackFilter(this, basicInfo);
            } catch (error) {
              this._log(`兜底筛选异常: ${error.message}`, "error");
            }

            if (fallbackPass) {
              shouldGreet = true;
              greetReason = coarse.reason;
            } else {
              await this._sendCommand({
                action: "MARK_ELEMENT",
                data: {
                  elementId: candidate.elementId,
                  reason: `未打招呼(${coarse.reason})`,
                  type: "rejected",
                },
              });
            }

            continue;
          }

          // ══════════════════════════════════════════
          // 第四步：获取详情 — 粗筛通过后，打开详情页获取完整信息
          // ══════════════════════════════════════════
          let detailedInfo = basicInfo;
          let detailOpened = false;

          if (strategy.needsDetailPage()) {
            try {
              const detailResponse = await this._sendCommand({
                action: "OPEN_CANDIDATE_DETAIL",
                data: { elementId: candidate.elementId },
              });

              if (detailResponse?.opened) {
                detailOpened = true;
                await this._waitRandomDelay();
                detailedInfo = detailResponse.detailedInfo || basicInfo;
              }
            } catch (error) {
              this._log(`打开详情页异常: ${error.message}`, "error");
            }
          }

          // ══════════════════════════════════════════
          // 第五步：精筛 — 看完整信息，决定是否打招呼
          // ══════════════════════════════════════════
          try {
            const fine = await strategy.fineFilter(this, detailedInfo);

            if (fine.pass) {
              shouldGreet = true;
              greetReason = fine.reason;
            } else {
              await this._sendCommand({
                action: "MARK_ELEMENT",
                data: {
                  elementId: candidate.elementId,
                  reason: `未打招呼(${fine.reason})`,
                  type: "rejected",
                },
              });
            }
          } catch (error) {
            this._log(`精筛异常: ${error.message}`, "error");
            await this._sendCommand({
              action: "MARK_ELEMENT",
              data: {
                elementId: candidate.elementId,
                reason: `精筛异常`,
                type: "error",
              },
            });
          }

          // ══════════════════════════════════════════
          // 第六步：关闭详情页（如果打开过）
          // ══════════════════════════════════════════
          if (detailOpened) {
            try {
              await this._sendCommand({ action: "CLOSE_DETAIL" });
              await new Promise((resolve) => setTimeout(resolve, 500));
            } catch (error) {
              this._log(`关闭详情页异常: ${error.message}`, "error");
            }
          }
        } catch (error) {
          this._log(`筛选流程异常: ${error.message}`, "error");
        }

        // ══════════════════════════════════════════
        // 第七步：行动 — 打招呼 + 索要联系方式 + 通知
        // ══════════════════════════════════════════
        if (!shouldGreet) continue;

        if (this.matchCount >= this.matchLimit) {
          this._log(`匹配成功但已达到限制 ${this.matchLimit}，停止`, "warning");
          this.stop();
          return;
        }

        // 标记为已匹配
        await this._sendCommand({
          action: "MARK_ELEMENT",
          data: {
            elementId: candidate.elementId,
            reason: `已打招呼(${greetReason})`,
            type: "matched",
          },
        });

        // 打招呼
        let greetSuccess = false;
        try {
          const greetResult = await this._sendCommand({
            action: "CLICK_GREET",
            data: { elementId: candidate.elementId },
          });
          greetSuccess = greetResult?.clicked || false;
        } catch (error) {
          this._log(`打招呼异常: ${error.message}`, "error");
        }

        if (!greetSuccess) continue;

        // 索要联系方式
        try {
          await this._sendCommand({
            action: "COLLECT_CONTACT",
            data: { elementId: candidate.elementId },
          });
        } catch (error) {
          this._log(`索要联系方式异常: ${error.message}`, "error");
        }

        // 计数 + 日志
        this.matchCount++;
        this._log(
          `打招呼成功 ${this.matchCount}/${this.matchLimit} - ${candidate.name || "未知"}`,
          "success",
        );

        // 播放提示音
        if (this.enableSound) {
          await this._sendCommand({
            action: "PLAY_SOUND",
            data: { sound: "notification2" },
          });
        }
      } catch (error) {
        this._log(`主循环异常: ${error.message}`, "error");
      }
    }
  }
}

const orchestrator = new Orchestrator();

/**
 * 启动免费模式运行
 * @param {object} data - 运行参数
 * @param {object} parserInfo - 平台信息
 * @param {function} onLog - 日志回调
 */
export async function startFreeRun(data, parserInfo, onLog) {
  await orchestrator.startFreeMode(data, parserInfo, onLog);
}

/**
 * 启动AI模式运行
 * @param {object} data - 运行参数
 * @param {object} parserInfo - 平台信息
 * @param {function} onLog - 日志回调
 */
export async function startAIRun(data, parserInfo, onLog) {
  await orchestrator.startAIMode(data, parserInfo, onLog);
}

/**
 * 停止运行
 */
export function stopRun() {
  orchestrator.stop();
}

export { Orchestrator };
