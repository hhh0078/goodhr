import { BaseParser } from "./common.js";

class BossParser extends BaseParser {
  constructor() {
    super();
    // 定义完整的 class 名称
    //新牛人 recommend-card-list
    this.fullClasses = {
      container: "card-list",
      items: [
        "candidate-card-wrap",
        "geek-info-card",
        "card-container",
        "card-inner clear-fix",
      ],
      name: "name",
      age: "job-card-left_labels__wVUfs",
      education: "base-info join-text-wrap",
      university: "content join-text-wrap",
      description: "content",
      clickTarget: [
        "btn btn-greet",
        "btn btn-getcontact search-btn-tip btn-prop-common prop-card-chat",
      ],
    };
    this.urlInfo = {
      url: "/web/chat/recommend",
      site: "推荐牛人",
    };

    // 定义部分 class 名称（用于模糊匹配）
    this.selectors = {
      container: "card-list",
      items: [
        "candidate-card-wrap",
        "card-inner clear-fix",
        "card-container",
        "geek-info-card",
      ],
      name: "name",
      age: ["job-card-left"],
      education: ["base-info join-text-wrap", "geek-info-detail"],
      university: "content join-text-wrap",
      description: "content",
      clickTarget: [
        "btn btn-greet",
        "btn btn-getcontact search-btn-tip btn-prop-common prop-card-chat",
      ],
      activeText: "active-text",
      continueButton: ["btn btn-continue btn-outline"],
      phoneButton: "operate-item",

      extraSelectors: [
        { prefix: "salary-text", type: "薪资" },
        { prefix: "job-info-primary", type: "基本信息" },
        { prefix: "tags-wrap", type: "标签" },
        { prefix: "content join-text-wrap", type: "公司信息" },
      ],
      // 同事是否沟通过
      isContacted: ["colleague-collaboration"],
    };

    // BOSS特定的选择器
    this.detailSelectors = {
      detailLink: [
        "card-inner common-wrap",
        "card-inner clear-fix",
        "candidate-card-wrap",
        "card-inner blue-collar-wrap",
        "card-container",
        "geek-info-card",
        "card-inner new-geek-wrap",
      ],
      closeButton: [
        "boss-popup__close",
        "resume-custom-close",
        "boss-popup__close",
        "dialog-wrap active",
      ],
      closeButtonXpath: ['//*[@id="boss-dynamic-dialog-1j38fleo5"]/div/div[2]'],
      // 消息提示
      messageTip: "chat-global-entry",
      //消息列表元素
      messageListItem: "friend-list-item",
    };

    // 初始化数据拦截监听
    this.initDataInterceptor();
  }

  /**
   * 检查是否有消息提示，有就开始处理
   * @param {*} element
   * @returns
   */
  async checkMessageTip(element, phone, wechat, resume) {
    //暂时不处理
    return true;

    let messageTip = window.parent.document.getElementsByClassName(
      this.detailSelectors.messageTip
    );
    if (messageTip.length > 0) {
      messageTip = messageTip[0];
    }
    console.log("消息数量:", messageTip.textContent.trim());
    let messageCount = 0;
    try {
      messageCount = parseInt(messageTip.textContent.trim()) || 0;
    } catch (error) {
      console.error("解析消息数量失败:", error);
    }
    if (messageCount <= 0) {
      return false;
    }
    //开始处理消息
    // 1. 点击消息提示

    messageTip.click();
    await new Promise((resolve) => setTimeout(resolve, 2500));

    // 3. 查找出待处理的列表
    let messageListItems =
      window.parent.document.getElementsByClassName("friend-list-item");
    console.log("消息列表元素数量:", messageListItems);

    for (let i = 0; i < messageListItems.length; i++) {
      console.log(messageListItems[i].textContent.trim());
      let messageCount = 0;
      try {
        messageCount =
          parseInt(
            messageListItems[i]
              .getElementsByClassName("badge-count badge-count-common-less")[0]
              .textContent.trim()
          ) || 0;
      } catch (error) {}
      if (messageCount <= 0) {
        continue;
      }
      //点击消息
      messageListItems[i].click();
      await new Promise((resolve) => setTimeout(resolve, 1000));
      // 点击索要绑定手机号
      await this.clickPhoneButton(phone, wechat, resume);
    }

    // 3. 点击索要绑定手机号
    // 4. 关闭列表
    messageTip.click();

    return false;
  }

  /**
   * 点击索要绑定手机号按钮
   * @param {是否索要手机号} phone
   * @param {是否索要微信} wechat
   * @param {是否索要简历} resume
   */
  async clickPhoneButton(phone, wechat, resume) {
    const suoyaolist = window.parent.document.querySelectorAll(
      `[class^="${this.selectors.phoneButton}"]`
    );

    if (!suoyaolist) {
      console.error("未找到索要信息的按钮");
      return null;
    }

    if (suoyaolist.length <= 3) {
      console.error("未找到索要手机号按钮");
      return null;
    }

    console.log("索要手机号按钮:", suoyaolist);

    if (phone) {
      suoyaolist[3].click();
      await new Promise((resolve) => setTimeout(resolve, 200));
      const confirmphoneButton = document.querySelector(
        `[class^="boss-btn-primary boss-btn"]`
      );

      if (confirmphoneButton) {
        confirmphoneButton.click();
      }
    }
    if (wechat) {
      suoyaolist[4].click();
      await new Promise((resolve) => setTimeout(resolve, 200));

      const confirmphoneButton = document.querySelector(
        `[class^="boss-btn-primary boss-btn"]`
      );

      if (confirmphoneButton) {
        confirmphoneButton.click();
      }
    }
    if (resume) {
      suoyaolist[2].click();
      await new Promise((resolve) => setTimeout(resolve, 200));

      const confirmphoneButton = document.querySelector(
        `[class^="boss-btn-primary boss-btn"]`
      );
      if (confirmphoneButton) {
        confirmphoneButton.click();
      }
    }
  }

  //索要绑定手机号
  async collectPhoneWechatResume(phone, wechat, resume, candidate, element) {
    try {
      //等等1秒
      await new Promise((resolve) => setTimeout(resolve, 1000));

      for (let i = 0; i < this.selectors.continueButton.length; i++) {
        //先点击继续沟通
        const continueButton = element.querySelectorAll(
          `[class^="${this.selectors.continueButton[i]}"]`
        );
        for (let j = 0; j < continueButton.length; j++) {
          // console.log(continueButton[j].textContent.trim());
          // console.log(continueButton[j]);
          continueButton[j].click();
          await new Promise((resolve) => setTimeout(resolve, 500));
          //  console.log(continueButton[j]);
          continueButton[j].click();
        }
      }
      // await randomDelay("索要信息");
      await new Promise((resolve) => setTimeout(resolve, 1000));

      await this.clickPhoneButton(phone, wechat, resume);

      //等待2秒
      await new Promise((resolve) => setTimeout(resolve, 1000));
      // console.log('等待2秒');

      //点击确认按钮
      const closeButton = document.querySelector(
        `[class^="iboss iboss-close"]`
      );
      // console.log("关闭按钮",closeButton);

      if (closeButton) {
        closeButton.click();
      }

      const closeButton2 = document.querySelector(
        `[class^="km-icon sati ignore-im-widget-click sati-times"]`
      );
      // console.log("关闭按钮2",closeButton2);
      if (closeButton2) {
        closeButton2.click();
      }
    } catch (error) {
      console.error("索要手机号失败:", error);
      return null;
    }
  }

  // 初始化数据拦截监听
  initDataInterceptor() {
    // 监听来自boss_interceptor.js的拦截数据
    window.addEventListener("message", (event) => {
      if (event.source !== window) return;

      if (
        event.data &&
        event.data.source === "boss-plugin" &&
        event.data.type === "geek-list"
      ) {
        if (event.data.data) {
          this.processInterceptedData(
            event.data.data.zpData.geekList || event.data.data.zpData.geeks
          );
        }
      }
    });
  }

  // 处理拦截到的数据
  async processInterceptedData(apiData) {
    try {
      if (apiData) {
        const candidates = apiData;

        // 直接存储拦截到的数据
        await new Promise((resolve, reject) => {
          chrome.storage.local.set(
            {
              bossZhipinCandidates: candidates,
            },
            () => {
              if (chrome.runtime.lastError) {
                console.error("保存缓存数据失败:", chrome.runtime.lastError);
                reject(chrome.runtime.lastError);
              } else {
                console.log("候选人数据直接缓存完成");
                resolve();
              }
            }
          );
        });
      }
    } catch (error) {
      console.error("处理拦截数据失败:", error);
    }
  }

  // 删除不再需要的合并方法

  // 添加一个新的查找元素的方法
  findElements() {
    let items = [];

    // 1. 首先尝试使用完整的 class 名称
    // items = document.getElementsByClassName(this.fullClasses.items);
    for (const item of this.fullClasses.items) {
      items = document.getElementsByClassName(item);
      if (items.length > 0) {
        break;
      }
    }

    if (items.length === 0) {
      for (const item of this.selectors.items) {
        items = document.getElementsByClassName(item);
        if (items.length > 0) {
          break;
        }
      }
      // 2. 尝试使用简单的 class 名称
      // items = document.getElementsByClassName(this.selectors.items);
    }

    if (items.length === 0) {
      for (const item of this.fullClasses.items) {
        items = document.querySelectorAll(`[class*="${item}"]`);
        if (items.length > 0) {
          break;
        }
      }
      // 3. 尝试使用模糊匹配
    }

    if (items.length === 0) {
      for (const item of this.fullClasses.items) {
        items = document.querySelectorAll(
          `[class^="${item}"], [class*=" ${this.selectors.items}"]`
        );
        if (items.length > 0) {
          break;
        }
      }
      // 4. 尝试使用前缀匹配
    }

    return items;
  }

  // 从缓存中获取候选人数据
  async getCachedCandidateData() {
    return new Promise((resolve) => {
      chrome.storage.local.get(["bossZhipinCandidates"], (result) => {
        if (chrome.runtime.lastError) {
          console.error("获取缓存数据失败:", chrome.runtime.lastError);
          resolve([]);
          return;
        }
        resolve(result.bossZhipinCandidates || []);
      });
    });
  }

  // 根据候选人姓名从缓存中查找完整信息
  findCandidateFromCache(cachedData, candidateName) {
    if (!cachedData || !candidateName) return null;

    for (const candidate of cachedData) {
      console.log(candidate.geekCard);
      if (
        candidate.geekCard &&
        candidate.geekCard.geekName
          .toLowerCase()
          .includes(candidateName.toLowerCase())
      ) {
        return candidate;
      }
    }

    return null;
  }

  //第二次组装。无需
  async extractCandidates2(data) {
    return null;
  }

  //提取信息
  async extractCandidates(elements = null) {
    try {
      const candidates = [];
      let items = elements || (await this.findElements());

      if (!items || items.length === 0) {
        console.warn("未找到任何候选人元素");
        return [];
      }

      // 异步获取缓存数据
      const cachedData = await this.getCachedCandidateData();

      // 使用for循环顺序处理元素，确保所有异步操作完成
      for (const item of Array.from(items)) {
        try {
          // this.highlightElement(item, 'processing');

          // 提取候选人姓名
          const nameElement = await this.findNameElement(item);
          const candidateName = nameElement?.textContent?.trim() || "";
          if (!candidateName) {
            this.clearHighlight(item);
            continue;
          }
          console.log("查找:" + candidateName);

          // 查找缓存或创建新候选人
          const candidate = await this.processCandidate(
            item,
            candidateName,
            cachedData
          );
          if (candidate) {
            candidates.push(candidate);
            // this.highlightElement(item, 'matched');
          }
        } catch (error) {
          console.error("处理候选人元素失败:", error);
          this.clearHighlight(item);
        }
      }

      return candidates;
    } catch (error) {
      console.error("提取候选人失败:", error);
      return []; // 出错时返回空数组
    }
  }

  // 查找元素方法
  findElement(fullClass, partialClass) {
    return (item) => {
      if (Array.isArray(partialClass)) {
        for (const className of partialClass) {
          const element =
            item.getElementsByClassName(className)[0] ||
            item.querySelector(`[class*="${className}"]`);
          if (element) return element;
        }
      } else {
        return (
          item.getElementsByClassName(fullClass)[0] ||
          item.getElementsByClassName(partialClass)[0] ||
          item.querySelector(`[class*="${partialClass}"]`)
        );
      }
      return null;
    };
  }

  // 辅助方法：查找姓名元素
  async findNameElement(item) {
    return this.findElement(this.fullClasses.name, this.selectors.name)(item);
  }

  // 辅助方法：处理单个候选人
  // 从页面更新候选人信息
  async updateCandidateFromPage(candidate, item) {
    try {
      // 获取页面上的实时状态
      const pageActiveText =
        (await this.findElement(
          this.fullClasses.activeText,
          this.selectors.activeText
        )(item)?.textContent?.trim()) || "离线";

      if (pageActiveText && pageActiveText !== "离线") {
        candidate.activeText = pageActiveText;
      }
      return candidate;
    } catch (error) {
      console.error("更新候选人页面信息失败:", error);
      return candidate;
    }
  }

  // 创建新候选人
  async createNewCandidate(item, candidateName) {
    try {
      return {
        name: candidateName,
        age: this.extractAge(
          await this.findElement(this.fullClasses.age, this.selectors.age)(item)
            ?.textContent
        ),
        education:
          (await this.findElement(
            this.fullClasses.education,
            this.selectors.education
          )(item)?.textContent?.trim()) || "",
        university:
          (await this.findElement(
            this.fullClasses.university,
            this.selectors.university
          )(item)?.textContent?.trim()) || "",
        description:
          (await this.findElement(
            this.fullClasses.description,
            this.selectors.description
          )(item)?.textContent?.trim()) || "",
        activeText: await this.getActiveText(item),
        extraInfo: await this.extractExtraInfo(
          item,
          this.selectors.extraSelectors
        ),
        timestamp: Date.now(),
      };
    } catch (error) {
      console.error("创建新候选人失败:", error);
      return null;
    }
  }

  async processCandidate(item, candidateName, cachedData) {
    const cachedCandidate = this.findCandidateFromCache(
      cachedData,
      candidateName
    );
    if (cachedCandidate) {
      // console.log(`使用缓存数据: ${candidateName}`);
      // return this.updateCandidateFromPage(cachedCandidate, item);
      return cachedCandidate;
    }
    return this.createNewCandidate(item, candidateName);
  }

  // 获取在线状态文本
  getActiveText(item) {
    let activeTextElement = findElement(
      this.fullClasses.activeText,
      this.selectors.activeText
    );
    if (!activeTextElement) {
      let onlineMarker = findElement("online-marker", "online-marker");
      return onlineMarker ? "在线" : "离线";
    }
    return activeTextElement.textContent?.trim() || "离线";
  }

  extractAge(text) {
    if (!text) return 0;
    const matches = text.match(/(\d+)岁/);
    return matches ? parseInt(matches[1]) : 0;
  }

  async clickMatchedItem(element) {
    // console.log('打招呼:', element);
    try {
      let clickElement = null;
      // 使用多种方式查找点击目标

      //触发element 的鼠标移入事件
      element.dispatchEvent(
        new MouseEvent("mouseover", {
          view: window,
          bubbles: true,
          cancelable: true,
        })
      );

      //this.selectors.clickTarget 是数组

      for (const className of this.selectors.clickTarget) {
        let aaa =
          element.getElementsByClassName(className) ||
          element.getElementsByClassName(className) ||
          element.querySelector(`[class*="${className}"]`);
        if (aaa.length > 0) {
          for (let i = 0; i <= aaa.length; i++) {
            if (aaa[i]) {
              aaa[i].click();
            }
          }
        }
      }
      // 使用多种方式查找点击目标\
      //console.log('点击元素:', clickElement);
      if (clickElement) {
        clickElement.click();
        return true;
      }
      return false;
    } catch (error) {
      console.error("点击元素时出错:", error);
      return false;
    }
  }

  // 实现点击候选人详情方法
  async clickCandidateDetail(element) {
    try {
      let detailLink = null;
      //this.detailSelectors.detailLink 是数组
      for (const className of this.detailSelectors.detailLink) {
        let element2 = element.getElementsByClassName(className)[0];
        if (element2) {
          detailLink = element2;
        }
      }

      //console.log(detailLink);
      if (detailLink) {
        detailLink.click();
        // console.log('点击候选人详情成功');
        return true;
      } else {
        console.error("无法查找到详情class:", this.detailSelectors.detailLink);
      }
      return false;
    } catch (error) {
      console.error("点击候选人详情失败:", error);
      return false;
    }
  }

  // 实现关闭详情方法
  async closeDetail(maxRetries = 3) {
    try {
      // 递归深度限制，避免无限循环
      if (maxRetries <= 0) {
        console.warn("关闭详情已达到最大重试次数");
        // alert("Goodhr提醒您: 尝试三次关闭候选人弹框都失败了，请暂停使用并联系作者处理");
        return false;
      }

      // 尝试多种关闭方式
      for (const className of this.detailSelectors.closeButton) {
        const closeElements = document.getElementsByClassName(className);
        if (closeElements.length > 0) {
          closeElements[0].click();
          // 等待DOM更新
          await new Promise((resolve) => setTimeout(resolve, 500));
        }

        const closeElements2 =
          window.parent.document.getElementsByClassName(className);
        if (closeElements2.length > 0) {
          closeElements2[0].click();
          // 等待DOM更新
          await new Promise((resolve) => setTimeout(resolve, 500));
        }
      } // 检查元素是否在最顶层文档中

      // 检查是否还有未关闭的弹框
      await new Promise((resolve) => setTimeout(resolve, 500));
      const stillHasModals = this.checkForRemainingModals();

      if (stillHasModals) {
        console.log("检测到还有未关闭的弹框，继续尝试关闭");
        return await this.closeDetail(maxRetries - 1);
      }

      return true;
    } catch (error) {
      console.error("关闭详情失败:", error);
      // alert("Goodhr提醒您: 快停止使用插件以免封号。然后联系作者");
      return false;
    }
  }

  // 检查是否还有未关闭的弹框
  checkForRemainingModals() {
    // 检查所有可能的弹框类型
    for (const className of this.detailSelectors.closeButton) {
      if (document.getElementsByClassName(className).length > 0) {
        console.log(`发现未关闭的弹框: ${className}`);
        return true;
      }
    }

    for (const xpath of this.detailSelectors.closeButtonXpath) {
      const element = document.evaluate(
        xpath,
        document,
        null,
        XPathResult.FIRST_ORDERED_NODE_TYPE,
        null
      ).singleNodeValue;
      if (element) {
        console.log(`发现未关闭的弹框: ${xpath}`);
        return true;
      }
    }

    return false;
  }

  //查询同事沟通过候选人的信息
  async queryColleagueContactedInfo(candidate) {
    try {
      //参考boss_resume_downloader.js中的processNextCandidate方法
      for (let i = 0; i <= this.selectors.isContacted.length; i++) {
        let aaa = document.getElementsByClassName(
          this.selectors.isContacted[i]
        );
        if (aaa.length > 0) {
          return aaa[0].textContent.trim();
        } else {
        }
      }
    } catch (error) {
      console.error("查询同事沟通过候选人的信息失败:", error);
    }
  }

  // 获取候选人简单信息（用于AI决策）
  getSimpleCandidateInfo(candidate) {
    if (!candidate) return "候选人信息为空";

    const info = [];

    // 基本信息
    if (candidate.geekCard) {
      const gc = candidate.geekCard;
      info.push(`姓名: ${gc.geekName || "未知"}`);
      info.push(`年龄: ${gc.ageDesc || "未知"}`);
      info.push(`学历: ${gc.geekDegree || "未知"}`);
      info.push(`毕业院校: ${gc.geekEdu?.school || "未知"}`);
      info.push(`专业: ${gc.geekEdu?.major || "未知"}`);
      info.push(`工作年限: ${gc.geekWorkYear || "未知"}`);
      info.push(`当前状态: ${gc.applyStatusDesc || "未知"}`);
      info.push(`期望薪资: ${gc.salary || "未知"}`);
      info.push(`期望职位: ${gc.expectPositionName || "未知"}`);
      info.push(`期望地点: ${gc.expectLocationName || "未知"}`);

      if (gc.geekDesc?.content) {
        info.push(`自我介绍: ${gc.geekDesc.content}`);
      }
    }

    // 工作经历
    if (candidate.geekCard.geekWorks?.length > 0) {
      info.push("\n工作经历:");
      candidate.geekCard.geekWorks.forEach((work) => {
        info.push(
          `- ${work.company || "未知公司"} · ${work.positionName || "未知职位"}`
        );
        info.push(`  时间: ${work.startDate} 至 ${work.endDate || "至今"}`);
        info.push(`  职责: ${work.responsibility || "无描述"}`);
        //工作时长 workTime
        info.push(`  工作时长: ${work.workTime || "无描述"}`);
        info.push(`  关键词: ${work.workEmphasisList || "无描述"}`);
      });
    }

    // 教育经历
    if (candidate.geekCard.geekEdus?.length > 0) {
      info.push("\n教育经历:");
      candidate.geekCard.geekEdus.forEach((edu) => {
        info.push(`- ${edu.school || "未知学校"} · ${edu.major || "未知专业"}`);
        info.push(
          `  学历: ${edu.degreeName || "未知"} (${edu.startDate} 至 ${
            edu.endDate
          })`
        );
      });
    }

    // 其他信息
    info.push(`\n最后活跃: ${candidate.activeTimeDesc || "未知"}`);
    info.push(`当前日期: ${new Date().toLocaleDateString()}`);

    return info.join("\n");
  }
}

export { BossParser };
