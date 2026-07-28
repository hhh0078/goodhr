// 文件作用说明：实现封装好的页面或元素截图能力，统一路径安全、查找、保存和错误日志。

import path from "node:path";
import type {
  ScreenshotRequest,
  ScreenshotResult,
} from "../../contracts/actions.js";
import type { ActionContext } from "../../contracts/common.js";
import { normalizeWorkerError } from "../../errors/worker-error.js";
import { WorkerLogger } from "../../logging/logger.js";
import { ScreenshotPrimitive } from "../primitives/screenshot.js";
import { BrowserSession } from "../session/browser-session.js";
import { FindAction } from "./find.js";

/** ScreenshotAction 实现页面和元素截图封装能力。 */
export class ScreenshotAction {
  private readonly primitive = new ScreenshotPrimitive();

  /** 创建截图封装能力。 */
  constructor(
    private readonly session: BrowserSession,
    private readonly find: FindAction,
    private readonly logger: WorkerLogger,
  ) {}

  /** execute 平铺执行路径校验、可选元素查找和截图保存。 */
  async execute(
    request: ScreenshotRequest,
    actionContext: ActionContext,
  ): Promise<ScreenshotResult> {
    const step = "screenshot";
    const filename = safePNGFilename(request.filename);
    const filePath = path.join(request.directory, filename);
    this.logger.info(actionContext, step, "start", {
      target_description: request.target?.description ?? "当前页面",
      filename,
      full_page: request.full_page ?? false,
    });
    try {
      const page = await this.session.requirePage(actionContext, step);
      const size = request.target
        ? await this.primitive.element(
            (
              await this.find.one(
                request.target,
                actionContext,
                true,
              )
            ).resolved.locator,
            filePath,
          )
        : await this.primitive.page(
            page,
            filePath,
            request.full_page ?? false,
          );
      const result: ScreenshotResult = {
        path: filePath,
        filename,
        size,
      };
      this.logger.info(actionContext, step, "success", {
        filename,
        size,
      });
      return result;
    } catch (error) {
      const normalized = normalizeWorkerError(error, {
        code: "SCREENSHOT_FAILED",
        message: "截图没保存成功，我已经把原因记下来了",
        action: actionContext.action,
        step,
        trace_id: actionContext.trace_id,
        retryable: true,
        details: { filename },
      });
      this.logger.error(actionContext, step, "failed", normalized.details);
      throw normalized;
    }
  }
}

/** safePNGFilename 清理路径字符并确保使用 PNG 后缀。 */
function safePNGFilename(rawName: string): string {
  const base = path
    .basename(rawName || "screenshot.png")
    .replace(/[<>:"/\\|?*\x00-\x1F]/g, "_")
    .trim();
  const name = base || "screenshot.png";
  return name.toLowerCase().endsWith(".png") ? name : `${name}.png`;
}
