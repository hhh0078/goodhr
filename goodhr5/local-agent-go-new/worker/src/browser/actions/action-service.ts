// 文件作用说明：统一组装 Worker 会话、原子能力和封装能力，并作为 HTTP 层唯一可调用的浏览器入口。

import { existsSync } from "node:fs";
import type {
  BrowserStartRequest,
  CookieSetRequest,
  DownloadConfigureRequest,
  ElementClickRequest,
  ElementFindAllRequest,
  ElementFindRequest,
  ElementInputRequest,
  ElementReadRequest,
  KeyboardPressRequest,
  LongScreenshotRequest,
  PageOpenRequest,
  PageUseRequest,
  ScreenshotRequest,
  ScrollRequest,
} from "../../contracts/actions.js";
import { binaryInfo } from "cloakbrowser";
import type { ActionContext, JsonObject } from "../../contracts/common.js";
import { WorkerLogger } from "../../logging/logger.js";
import { KeyboardPrimitive } from "../primitives/keyboard.js";
import { LocatorPrimitive } from "../primitives/locator.js";
import { MousePrimitive } from "../primitives/mouse.js";
import { BrowserSession } from "../session/browser-session.js";
import { ClickAction } from "./click.js";
import { FindAction } from "./find.js";
import { InputAction } from "./input.js";
import { KeyboardAction } from "./keyboard.js";
import { MoveAction } from "./move.js";
import { ReadAction } from "./read.js";
import { ScreenshotAction } from "./screenshot.js";
import { ScrollAction } from "./scroll.js";

/** ActionService 只暴露完整封装能力，原子能力不会越过本类进入 HTTP 层。 */
export class ActionService {
  private readonly logger = new WorkerLogger();
  private readonly session = new BrowserSession(this.logger);
  private readonly locator = new LocatorPrimitive();
  private readonly mouse = new MousePrimitive();
  private readonly keyboardPrimitive = new KeyboardPrimitive();
  private readonly findAction = new FindAction(
    this.session,
    this.locator,
    this.logger,
  );
  private readonly moveAction = new MoveAction(this.mouse, this.logger);
  private readonly scrollAction = new ScrollAction(
    this.session,
    this.findAction,
    this.moveAction,
    this.locator,
    this.mouse,
    this.logger,
  );
  private readonly clickAction = new ClickAction(
    this.findAction,
    this.scrollAction,
    this.moveAction,
    this.locator,
    this.mouse,
    this.logger,
  );
  private readonly inputAction = new InputAction(
    this.findAction,
    this.scrollAction,
    this.moveAction,
    this.mouse,
    this.keyboardPrimitive,
    this.logger,
  );
  private readonly readAction = new ReadAction(this.findAction, this.logger);
  private readonly screenshotAction = new ScreenshotAction(
    this.session,
    this.findAction,
    this.moveAction,
    this.mouse,
    this.logger,
  );
  private readonly keyboardAction = new KeyboardAction(
    this.session,
    this.keyboardPrimitive,
    this.logger,
  );

  /** startBrowser 启动或复用 CloakBrowser 会话。 */
  startBrowser(request: BrowserStartRequest, context: ActionContext) {
    return this.session.start(request, context);
  }

  /** stopBrowser 关闭浏览器会话。 */
  stopBrowser(context: ActionContext) {
    return this.session.stop(context);
  }

  /** browserStatus 返回浏览器当前状态。 */
  browserStatus() {
    return this.session.status(false);
  }

  /** runtimeStatus 返回 CloakBrowser 增强二进制安装状态。 */
  runtimeStatus() {
    const info = binaryInfo();
    const configuredPath = process.env.CLOAKBROWSER_BINARY_PATH?.trim();
    const binaryPath = configuredPath || info.binaryPath;
    return {
      cloakbrowser_version: info.version,
      platform: info.platform,
      binary_path: binaryPath,
      installed: existsSync(binaryPath),
    };
  }

  /** openPage 打开或新建标签页。 */
  openPage(request: PageOpenRequest, context: ActionContext) {
    return this.session.open(request, context);
  }

  /** listPages 返回当前全部标签页。 */
  listPages() {
    return this.session.listPages();
  }

  /** usePage 切换当前标签页。 */
  usePage(request: PageUseRequest, context: ActionContext) {
    return this.session.usePage(request.page_id, context);
  }

  /** closePage 关闭当前标签页。 */
  closePage(context: ActionContext) {
    return this.session.closeCurrentPage(context);
  }

  /** currentURL 返回当前页面地址。 */
  async currentURL(): Promise<JsonObject> {
    const status = await this.session.status(false);
    return { url: status.current_url };
  }

  /** findElement 查找一个元素并返回短生命周期引用。 */
  async findElement(request: ElementFindRequest, context: ActionContext) {
    return (await this.findAction.one(request.selector, context)).result;
  }

  /** findElements 查找元素列表并读取配置字段。 */
  findElements(request: ElementFindAllRequest, context: ActionContext) {
    return this.findAction.all(
      request.selector,
      request.max_items ?? 100,
      request.fields ?? {},
      context,
      !request.expected_missing,
    );
  }

  /** readElement 查找并读取元素内容。 */
  readElement(request: ElementReadRequest, context: ActionContext) {
    return this.readAction.execute(request, context);
  }

  /** clickElement 执行完整封装点击。 */
  clickElement(request: ElementClickRequest, context: ActionContext) {
    return this.clickAction.execute(request, context);
  }

  /** inputElement 执行完整封装输入。 */
  inputElement(request: ElementInputRequest, context: ActionContext) {
    return this.inputAction.execute(request, context);
  }

  /** scroll 执行页面或元素真实滚轮滚动。 */
  scroll(request: ScrollRequest, context: ActionContext) {
    return this.scrollAction.execute(request, context);
  }

  /** pressKey 在当前页面执行通用按键。 */
  pressKey(request: KeyboardPressRequest, context: ActionContext) {
    return this.keyboardAction.press(request, context);
  }

  /** screenshot 保存页面或元素截图。 */
  screenshot(request: ScreenshotRequest, context: ActionContext) {
    return this.screenshotAction.execute(request, context);
  }

  /** screenshotLong 使用真实鼠标滚轮分段截取长元素。 */
  screenshotLong(request: LongScreenshotRequest, context: ActionContext) {
    return this.screenshotAction.long(request, context);
  }

  /** listCookies 读取当前浏览器 Cookie。 */
  async listCookies(context: ActionContext) {
    const cookies = await this.session.cookies(context);
    return { cookies, count: cookies.length };
  }

  /** setCookies 导入浏览器 Cookie。 */
  async setCookies(request: CookieSetRequest, context: ActionContext) {
    await this.session.setCookies(request.cookies, context);
    return { saved: request.cookies.length };
  }

  /** listDownloads 返回浏览器会话中的下载记录。 */
  listDownloads() {
    return this.session.listDownloads();
  }

  /** configureDownloads 切换后续浏览器下载的保存目录。 */
  configureDownloads(request: DownloadConfigureRequest, context: ActionContext) {
    return this.session.configureDownloads(request.directory, context);
  }

  /** clearDownloads 清空内存中的下载记录，不删除本地文件。 */
  clearDownloads() {
    return this.session.clearDownloads();
  }

}
