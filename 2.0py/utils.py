import pyautogui
import time
import random
import asyncio
from pyppeteer import launch
import websockets.exceptions
import traceback
import logging

# 配置日志
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger('utils')

class Utils:
    # 添加静态变量跟踪重试次数
    _retry_attempts = 0
    _max_retry_attempts = 10

    @staticmethod
    async def handle_connection_error(func, page, *args, **kwargs):
        """
        处理连接错误的包装函数，尝试在不重启浏览器的情况下恢复操作
        
        Args:
            func: 要执行的异步函数
            page: 页面对象
            *args, **kwargs: 传递给func的参数
            
        Returns:
            执行结果
        """
        max_retries = 10  # 最大重试次数
        retry_delay_base = 0.5  # 基础重试延迟（秒）
        
        for retry in range(max_retries):
            try:
                # 尝试执行操作
                return await func(page, *args, **kwargs)
                
            except Exception as e:
                error_message = str(e)
                
                # 判断是否为连接相关错误
                is_connection_error = any(err in error_message.lower() for err in 
                                         ["connection", "closed", "disconnected", "timeout", 
                                          "websocket", "broken", "network"])
                
                if not is_connection_error:
                    # 不是连接错误，直接抛出
                    raise
                
                # 是连接错误，记录并尝试恢复
                Utils.log(f"连接错误 (第 {retry+1}/{max_retries} 次尝试): {error_message}")
                
                if retry < max_retries - 1:
                    # 计算指数退避延迟
                    delay = retry_delay_base * (2 ** retry)
                    # 添加随机因子(0.8-1.2)，避免同步问题
                    delay *= (0.8 + random.random() * 0.4)
                    # 最大延迟不超过10秒
                    delay = min(delay, 10)
                    
                    Utils.log(f"等待 {delay:.2f} 秒后重试...")
                    await asyncio.sleep(delay)
                    
                    # 尝试进行轻量级的连接恢复
                    try:
                        # 检查页面是否仍然可用
                        await page.evaluate("() => document.readyState")
                        Utils.log("页面连接仍然有效，继续执行")
                    except:
                        Utils.log("页面连接无效，无法恢复")
                        # 此处我们选择抛出错误，而不是重启浏览器
                        # 调用者应当决定如何处理这种情况
                        raise Exception("页面连接已断开，需要手动处理恢复")
                else:
                    Utils.log("已达到最大重试次数，操作失败")
                    raise
        
        # 不应该到达这里，但为了完整性添加
        raise Exception("重试次数已用尽，操作失败")

    @staticmethod
    def log(message):
        """打印带时间戳的日志"""
        current_time = time.strftime("%H:%M:%S", time.localtime())
        print(f"[{current_time}] {message}")
        logger.info(message)

    @staticmethod
    async def get_element_and_viewport_info(page, element):
        """
        获取元素、视口和浏览器窗口的详细信息
        Args:
            page: 页面对象
            element: 要检查的元素

        Returns:
            dict: 包含元素、视口和浏览器窗口的详细信息
        """
        try:
            # 使用JavaScript直接获取元素的位置和大小，确保获取的是最新的位置信息
            element_info = await page.evaluate('''
                (element) => {
                    if (!element) return null;
                    
            const rect = element.getBoundingClientRect();
                    const scrollY = window.pageYOffset || document.documentElement.scrollTop;
                    const scrollX = window.pageXOffset || document.documentElement.scrollLeft;
                    
                    // 计算元素相对于文档顶部的绝对位置
                    return {
                        top: rect.top + scrollY,
                        bottom: rect.bottom + scrollY,
                        left: rect.left + scrollX,
                        right: rect.right + scrollX,
                        width: rect.width,
                        height: rect.height,
                        // 添加相对于视口的位置
                        relativeTop: rect.top,
                        relativeBottom: rect.bottom
                    };
                }
            ''', element)
            
            if not element_info:
                return None

            # 获取视口和窗口信息，包括实际可见区域
            viewport_info = await page.evaluate('''
                () => {
                    // 获取文档的总高度
                    const docHeight = Math.max(
                        document.documentElement.scrollHeight,
                        document.body.scrollHeight
                    );
                    
                    // 获取准确的滚动位置
                    const scrollY = (window.pageYOffset || document.documentElement.scrollTop);
                    const scrollX = (window.pageXOffset || document.documentElement.scrollLeft);
                    
            return {
                        viewport: {
                            width: window.innerWidth,
                            height: window.innerHeight,
                            scrollX: scrollX,
                            scrollY: scrollY,
                            docHeight: docHeight,  // 添加文档总高度
                            visualTop: scrollY,  // 视觉可见区域的顶部就是滚动位置
                            visualBottom: scrollY + window.innerHeight,  // 视觉可见区域的底部
                            visualLeft: scrollX,
                            visualRight: scrollX + window.innerWidth
                        },
                        window: {
                screenX: window.screenX,
                screenY: window.screenY,
                            outerWidth: window.outerWidth,
                outerHeight: window.outerHeight
                        }
                    }
                }
            ''')

            # 整合所有信息
            return {
                'element': {
                    'top': element_info['top'],
                    'bottom': element_info['bottom'],
                    'left': element_info['left'],
                    'right': element_info['right'],
                    'width': element_info['width'],
                    'height': element_info['height'],
                    'relativeTop': element_info['relativeTop'],
                    'relativeBottom': element_info['relativeBottom']
                },
                'viewport': {
                    'top': viewport_info['viewport']['visualTop'],
                    'bottom': viewport_info['viewport']['visualBottom'],
                    'left': viewport_info['viewport']['visualLeft'],
                    'right': viewport_info['viewport']['visualRight'],
                    'width': viewport_info['viewport']['width'],
                    'height': viewport_info['viewport']['height'],
                    'scrollX': viewport_info['viewport']['scrollX'],
                    'scrollY': viewport_info['viewport']['scrollY'],
                    'docHeight': viewport_info['viewport']['docHeight'] 
                },
                'window': viewport_info['window']
            }

        except Exception as e:
            
            return None

    @staticmethod
    async def draw_element_box(page, element_absolute_top, element_absolute_bottom, element_index):
        """在页面上绘制元素位置的框"""
        height = element_absolute_bottom - element_absolute_top
        await page.evaluate('''
            (element) => {
                // 移除同一个元素的旧框框（如果存在）
                const existingBox = document.getElementById(`element-debug-box-${element.elementIndex}`);
                if (existingBox) {
                    existingBox.remove();
                }
                
                // 获取当前滚动位置
                const scrollY = window.pageYOffset || document.documentElement.scrollTop;
                
                // 创建新的框
                const box = document.createElement('div');
                box.id = `element-debug-box-${element.elementIndex}`;
                box.style.position = 'fixed';
                box.style.top = (element.top - scrollY) + 'px';  // 调整为相对于视口的位置
                box.style.height = element.height + 'px';
                box.style.left = '10px';
                box.style.width = 'calc(50% - 20px)';
                box.style.border = '3px solid blue';
                box.style.backgroundColor = 'rgba(0, 0, 255, 0.1)';
                box.style.pointerEvents = 'none';
                box.style.zIndex = '9999';
                
                // 添加顶部标签
                const topLabel = document.createElement('div');
                topLabel.style.position = 'absolute';
                topLabel.style.top = '0';
                topLabel.style.left = '0';
                topLabel.style.backgroundColor = 'blue';
                topLabel.style.color = 'white';
                topLabel.style.padding = '4px';
                topLabel.style.fontSize = '12px';
                topLabel.style.whiteSpace = 'nowrap';
                topLabel.textContent = `元素${element.elementIndex}上边: ${element.top}px`;
                box.appendChild(topLabel);
                
                // 添加底部标签
                const bottomLabel = document.createElement('div');
                bottomLabel.style.position = 'absolute';
                bottomLabel.style.bottom = '0';
                bottomLabel.style.left = '0';
                bottomLabel.style.backgroundColor = 'blue';
                bottomLabel.style.color = 'white';
                bottomLabel.style.padding = '4px';
                bottomLabel.style.fontSize = '12px';
                bottomLabel.style.whiteSpace = 'nowrap';
                bottomLabel.textContent = `元素${element.elementIndex}下边: ${element.bottom}px`;
                box.appendChild(bottomLabel);
                
                // 添加中间标签
                const centerLabel = document.createElement('div');
                centerLabel.style.position = 'absolute';
                centerLabel.style.top = '50%';
                centerLabel.style.left = '50%';
                centerLabel.style.transform = 'translate(-50%, -50%)';
                centerLabel.style.backgroundColor = 'blue';
                centerLabel.style.color = 'white';
                centerLabel.style.padding = '4px';
                centerLabel.style.fontSize = '14px';
                centerLabel.style.borderRadius = '4px';
                centerLabel.style.whiteSpace = 'nowrap';
                centerLabel.textContent = `元素 ${element.elementIndex}`;
                box.appendChild(centerLabel);
                
                document.body.appendChild(box);
                
                console.log(`已创建元素框框 ${element.elementIndex}，位置: ${element.top}px - ${element.bottom}px，滚动位置: ${scrollY}px`);
            }
        ''', {
            'top': element_absolute_top,
            'bottom': element_absolute_bottom,
            'height': height,
            'elementIndex': element_index
        })

    @staticmethod
    async def draw_viewport_box(page, viewport_absolute_top, viewport_absolute_bottom, element_index):
        """在页面上绘制视口位置的框"""
        height = viewport_absolute_bottom - viewport_absolute_top
        await page.evaluate('''
            (viewport) => {
                // 移除已存在的框
                const existingBox = document.getElementById('viewport-debug-box');
                if (existingBox) {
                    existingBox.remove();
                }
                
                // 获取当前滚动位置
                const scrollY = window.pageYOffset || document.documentElement.scrollTop;
                
                // 创建新的框
                const box = document.createElement('div');
                box.id = 'viewport-debug-box';
                box.style.position = 'fixed';
                box.style.top = '0px';  // 视口框始终从视口顶部开始
                box.style.height = viewport.height + 'px';  // 视口的高度
                box.style.right = '10px';
                box.style.width = 'calc(50% - 20px)';
                box.style.border = '3px solid red';
                box.style.backgroundColor = 'rgba(255, 0, 0, 0.1)';
                box.style.pointerEvents = 'none';
                box.style.zIndex = '10000';
                
                // 添加顶部标签
                const topLabel = document.createElement('div');
                topLabel.style.position = 'absolute';
                topLabel.style.top = '0';
                topLabel.style.right = '0';
                topLabel.style.backgroundColor = 'red';
                topLabel.style.color = 'white';
                topLabel.style.padding = '4px';
                topLabel.style.fontSize = '12px';
                topLabel.style.whiteSpace = 'nowrap';
                topLabel.textContent = `视口上边: ${viewport.top}px (滚动位置: ${scrollY}px)`;
                box.appendChild(topLabel);
                
                // 添加底部标签
                const bottomLabel = document.createElement('div');
                bottomLabel.style.position = 'absolute';
                bottomLabel.style.bottom = '0';
                bottomLabel.style.right = '0';
                bottomLabel.style.backgroundColor = 'red';
                bottomLabel.style.color = 'white';
                bottomLabel.style.padding = '4px';
                bottomLabel.style.fontSize = '12px';
                bottomLabel.style.whiteSpace = 'nowrap';
                bottomLabel.textContent = `视口下边: ${viewport.bottom}px`;
                box.appendChild(bottomLabel);
                
                // 添加中间标签
                const centerLabel = document.createElement('div');
                centerLabel.style.position = 'absolute';
                centerLabel.style.top = '50%';
                centerLabel.style.left = '50%';
                centerLabel.style.transform = 'translate(-50%, -50%)';
                centerLabel.style.backgroundColor = 'red';
                centerLabel.style.color = 'white';
                centerLabel.style.padding = '4px';
                centerLabel.style.fontSize = '14px';
                centerLabel.style.borderRadius = '4px';
                centerLabel.style.whiteSpace = 'nowrap';
                centerLabel.textContent = `当前处理元素: ${viewport.elementIndex}`;
                box.appendChild(centerLabel);
                
                document.body.appendChild(box);
                
                console.log(`已创建视口框框，位置: ${viewport.top}px - ${viewport.bottom}px，滚动位置: ${scrollY}px`);
            }
        ''', {
            'top': viewport_absolute_top,
            'bottom': viewport_absolute_bottom,
            'height': height,
            'elementIndex': element_index
        })

    @staticmethod
    async def is_element_in_viewport(page, element, max_scroll_attempts=20, scroll_step=500, element_index=None):
        """
        检查元素是否在视口可见区域内，如果不在则尝试使用鼠标滚轮滚动使其可见
        Args:
            page: 页面对象
            element: 要检查的元素
            max_scroll_attempts: 最大滚动尝试次数
            scroll_step: 每次滚动的像素数（正数向上滚动，负数向下滚动）
            element_index: 当前处理的元素索引（用于日志输出）

        Returns:
            bool: 元素是否在可见区域内
            dict: 元素和视口的位置信息
        """
        try:
            Utils.log(f"\n{'='*50}")
            Utils.log(f"开始处理第 {element_index} 个元素")
            Utils.log(f"{'='*50}")

            # 检查元素是否在视口内的函数
            async def is_fully_visible(info):
                # 实时获取最新的元素和视口信息
                latest_info = await Utils.get_element_and_viewport_info(page, element)
                if not latest_info:
                    return False, None

                # 获取当前滚动位置
                current_scroll = latest_info['viewport']['scrollY']
                
                # 使用直接获取的元素相对于视口的位置
                element_relative_top = latest_info['element']['relativeTop']
                element_relative_bottom = latest_info['element']['relativeBottom']
                
                # 判断元素是否完全在视口可见区域内
                is_visible = (
                    element_relative_top >= 0 and  # 元素顶部在可见区域内
                    element_relative_bottom <= latest_info['viewport']['height']  # 元素底部在可见区域内
                )

                return is_visible, latest_info

            # 获取初始信息
            info = await Utils.get_element_and_viewport_info(page, element)
            if not info:
                return False, None

            # 如果元素已经在视口内，直接返回
            is_visible, latest_info = await is_fully_visible(info)
            if is_visible:
                Utils.log(f"元素 {element_index} 已完全可见")
                return True, latest_info

            attempt = 0
            last_scroll_position = -1  # 记录上次滚动位置
            consecutive_same_position = 0  # 记录连续相同位置的次数

            while attempt < max_scroll_attempts:
                attempt += 1
                
                # 获取最新的元素和视口信息
                latest_info = await Utils.get_element_and_viewport_info(page, element)
                if not latest_info:
                    return False, None

                # 计算元素中心点相对于视口的位置
                element_relative_center = latest_info['element']['relativeTop'] + (latest_info['element']['height'] / 2)
                viewport_center = latest_info['viewport']['height'] / 2
                
                # 计算需要滚动的距离，目标是将元素放在视口中央
                scroll_distance = element_relative_center - viewport_center
                scroll_direction = -1 if scroll_distance > 0 else 1  # -1表示向下滚动，1表示向上滚动

                # 获取当前文档总高度和滚动位置
                doc_height = latest_info['viewport']['docHeight']
                current_scroll = latest_info['viewport']['scrollY']
                viewport_height = latest_info['viewport']['height']

                # 检查是否滚动位置没有变化
                if current_scroll == last_scroll_position:
                    consecutive_same_position += 1
                    if consecutive_same_position >= 3:
                        Utils.log("滚动位置连续3次未变化，可能已到达边界或出现问题")
                        await page.waitFor(1000)  # 等待页面可能的加载
                        consecutive_same_position = 0  # 重置计数
                else:
                    consecutive_same_position = 0
                    last_scroll_position = current_scroll

                # 根据文档高度调整滚动步长
                remaining_scroll = doc_height - (current_scroll + viewport_height) if scroll_direction < 0 else current_scroll
                adjusted_scroll_step = min(abs(scroll_distance), remaining_scroll, scroll_step)
                
                # 如果已经到达底部或顶部，等待新内容加载
                if (scroll_direction < 0 and current_scroll + viewport_height >= doc_height) or \
                   (scroll_direction > 0 and current_scroll <= 0):
                    Utils.log(f"已到达{'底部' if scroll_direction < 0 else '顶部'}，等待新内容加载...")
                    await page.waitFor(2000)  # 增加等待时间
                    continue

                # 将鼠标移动到视口中心位置
                viewport_center_x = latest_info['window']['screenX'] + latest_info['viewport']['width'] / 2
                viewport_center_y = latest_info['window']['screenY'] + latest_info['viewport']['height'] / 2
                
                # 使用pyautogui移动鼠标到视口中心
                pyautogui.moveTo(viewport_center_x, viewport_center_y)
                time.sleep(0.2)  # 等待鼠标移动完成

                # 执行滚动，使用更小的滚动步长
                actual_scroll_units = int(adjusted_scroll_step * scroll_direction / 2)  # 减小滚动步长
                pyautogui.scroll(actual_scroll_units)
                
                # 等待滚动完成和页面稳定（添加随机等待时间）
                random_wait = random.uniform(0.5, 1.0)  # 增加等待时间
                time.sleep(random_wait)
                
                # 检查元素是否可见
                is_visible, latest_info = await is_fully_visible(info)
                if is_visible:
                    Utils.log(f"元素 {element_index} 已可见")
                    return True, latest_info
            
            # 如果达到最大尝试次数仍未找到，返回最后一次检查的结果
            final_visible, final_info = await is_fully_visible(info)
            Utils.log(f"元素 {element_index} 处理完成: {'可见' if final_visible else '不可见'}")
            return final_visible, final_info
        
        except Exception as e:
            Utils.log(f"检查元素 {element_index} 可见性时出错: {str(e)}")
            return False, None

    @staticmethod
    async def inject_anti_detection_js(page):
        """
        注入反检测的JavaScript代码
        Args:
            page: 页面对象
        """
        try:
            # 注入反检测代码
            await page.evaluateOnNewDocument('''
                // 删除 webdriver 属性
                Object.defineProperty(navigator, 'webdriver', {
                    get: () => false
                });

                // 删除自动化相关标记
                delete window.cdc_adoQpoasnfa76pfcZLmcfl_Array;
                delete window.cdc_adoQpoasnfa76pfcZLmcfl_Promise;
                delete window.cdc_adoQpoasnfa76pfcZLmcfl_Symbol;

                // 修改 chrome.app
                window.chrome = {
                    app: {
                        isInstalled: false,
                        InstallState: {
                            DISABLED: "DISABLED",
                            INSTALLED: "INSTALLED",
                            NOT_INSTALLED: "NOT_INSTALLED"
                        },
                        RunningState: {
                            CANNOT_RUN: "CANNOT_RUN",
                            READY_TO_RUN: "READY_TO_RUN",
                            RUNNING: "RUNNING"
                        }
                    },
                    runtime: {}
                };

                // 修改 permissions
                const originalQuery = window.navigator.permissions.query;
                window.navigator.permissions.query = (parameters) => (
                    parameters.name === 'notifications' ?
                        Promise.resolve({ state: Notification.permission }) :
                        originalQuery(parameters)
                );

                // 添加插件
                Object.defineProperty(navigator, 'plugins', {
                    get: function() {
                        return [
                            {
                                0: {
                                    type: "application/x-google-chrome-pdf",
                                    suffixes: "pdf",
                                    description: "Portable Document Format",
                                    enabledPlugin: true
                                },
                                description: "Portable Document Format",
                                filename: "internal-pdf-viewer",
                                length: 1,
                                name: "Chrome PDF Plugin"
                            }
                        ];
                    }
                });

                // 添加语言
                Object.defineProperty(navigator, 'languages', {
                    get: () => ["zh-CN", "zh", "en-US", "en"]
                });

                // 添加 mimeTypes
                Object.defineProperty(navigator, 'mimeTypes', {
                    get: () => [
                        {
                            type: "application/pdf",
                            suffixes: "pdf",
                            description: "Portable Document Format",
                            enabledPlugin: true
                        }
                    ]
                });

                // 添加连接信息
                Object.defineProperty(navigator, 'connection', {
                    get: () => ({
                        rtt: 50,
                        downlink: 10,
                        effectiveType: "4g",
                        saveData: false
                    })
                });

                // 添加硬件信息
                Object.defineProperty(navigator, 'hardwareConcurrency', {
                    get: () => 8
                });

                Object.defineProperty(navigator, 'deviceMemory', {
                    get: () => 8
                });

                // 添加 WebGL 信息
                const getParameter = WebGLRenderingContext.prototype.getParameter;
                WebGLRenderingContext.prototype.getParameter = function(parameter) {
                    if (parameter === 37445) {
                        return 'Intel Inc.'
                    }
                    if (parameter === 37446) {
                        return 'Intel(R) Iris(TM) Graphics 6100'
                    }
                    return getParameter.apply(this, [parameter]);
                };
            ''')
        except Exception as e:
            print(f"注入反检测代码时出错: {str(e)}")

    @staticmethod
    async def set_browser_headers(page):
        """
        设置浏览器请求头
        Args:
            page: 页面对象
        """
        try:
            await page.setExtraHTTPHeaders({
                'Accept-Language': 'zh-CN,zh;q=0.9,en;q=0.8',
                'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
            })
        except Exception as e:
            print(f"设置浏览器请求头时出错: {str(e)}")

    @staticmethod
    async def _click_candidate(page, candidate, candidate_index):
        """
        点击候选人列表元素
        Args:
            page: 页面对象
            candidate: 候选人元素
            candidate_index: 候选人索引

        Returns:
            bool: 是否成功点击
        """
        try:
            # 获取候选人元素的绝对位置
            candidate_info = await Utils.get_element_and_viewport_info(page, candidate)
            if not candidate_info:
                Utils.log(f"获取候选人 {candidate_index} 元素位置信息失败")
                return False

            # 计算点击位置（元素中心）
            click_x = candidate_info['window']['screenX'] + candidate_info['element']['left'] + (candidate_info['element']['width'] / 2)
            click_y = candidate_info['window']['screenY'] + candidate_info['element']['relativeTop'] + (candidate_info['element']['height'] / 2)

            Utils.log(f"准备移动鼠标到候选人 {candidate_index}，目标位置: x={click_x}, y={click_y}")

            # 获取当前鼠标位置
            current_x, current_y = pyautogui.position()
            Utils.log(f"当前鼠标位置: x={current_x}, y={current_y}")
            
            # 计算移动距离
            distance = ((click_x - current_x) ** 2 + (click_y - current_y) ** 2) ** 0.5
            
            # 根据距离确定移动时间和步数
            duration = min(1.0, max(0.3, distance / 1000))  # 距离越远，移动时间越长，但最短0.3秒，最长1秒
            steps = int(max(10, min(50, distance / 20)))  # 距离越远，步数越多，但最少10步，最多50步
            
            Utils.log(f"鼠标移动: 距离={distance:.2f}px, 时间={duration:.2f}秒, 步数={steps}")
            
            # 平滑移动鼠标到点击位置
            pyautogui.moveTo(click_x, click_y, duration=duration, tween=pyautogui.easeOutQuad)
            
            # 随机等待一小段时间，模拟人类行为
            wait_time = 0.1 + random.random() * 0.2  # 0.1到0.4秒的随机等待
            time.sleep(wait_time)

            # 点击鼠标
            Utils.log(f"点击候选人 {candidate_index}")
            pyautogui.click()
            
            # 点击后随机等待
            wait_after_click = 0.1 + random.random() * 0.2  # 0.3到0.8秒的随机等待
            time.sleep(wait_after_click)

            Utils.log(f"已完成点击候选人 {candidate_index}")
            return True
            
        except Exception as e:
            Utils.log(f"点击候选人 {candidate_index} 元素时出错: {str(e)}")
            return False

    @staticmethod
    async def get_element_by_css_selector(page, selector, selector_type="class", timeout=5000):
        """
        通过CSS选择器获取元素，带有连接错误处理
        Args:
            page: 页面对象
            selector: 选择器字符串
            selector_type: 选择器类型，可选值：class, id, role, compound
            timeout: 等待超时时间（毫秒）

        Returns:
            list: 匹配的元素列表，如果没有找到则返回空列表
        """
        # 定义内部查找函数
        async def _find_elements(p, s, s_type, tout):
            try:
                # 使用build_selector方法构建完整的选择器
                full_selector = Utils.build_selector(s, s_type)

                Utils.log(f"正在查找元素: {full_selector}")

                try:
                    # 首先尝试检查元素是否已经存在，无需等待
                    Utils.log(f"标记8: {full_selector}")
                    elements = await p.querySelectorAll(full_selector)
                    if elements and len(elements) > 0:
                        Utils.log(f"立即找到元素: {full_selector}，共 {len(elements)} 个")
                        return elements
                    
                    Utils.log(f"标记9: {full_selector}")
                    # 如果不存在，等待元素出现
                    try:
                        # 使用较短的超时时间
                        await p.waitForSelector(full_selector, {'timeout': min(tout, 3000)})
                        Utils.log(f"标记10: {full_selector}")
                    except Exception as wait_error:
                        if "timeout" in str(wait_error).lower():
                            Utils.log(f"等待元素超时: {full_selector}")
                        else:
                            Utils.log(f"等待元素时出错: {str(wait_error)}")
                    
                    # 无论等待是否成功，都再次尝试获取元素
                    elements = await p.querySelectorAll(full_selector)
                    
                    if not elements or len(elements) == 0:
                        Utils.log(f"未找到匹配的元素: {full_selector}")
                        return []
                    
                    Utils.log(f"成功找到 {len(elements)} 个元素: {full_selector}")
                    return elements
                    
                except Exception as e:
                    Utils.log(f"查询元素时出错: {str(e)}")
                    raise
                    
            except Exception as e:
                Utils.log(f"获取元素过程中出现错误: {str(e)}")
                raise
        
        try:
            # 使用连接错误处理包装器执行查找
            return await Utils.handle_connection_error(_find_elements, page, selector, selector_type, timeout)
        except Exception as e:
            Utils.log(f"获取元素失败: {str(e)}")
            return []  # 最终失败返回空列表

    @staticmethod
    async def get_element_text(page, element):
        """
        获取元素的文本内容
        Args:
            page: 页面对象
            element: 要获取文本的元素

        Returns:
            str: 元素的文本内容，如果获取失败则返回空字符串
        """
        try:
            # 使用JavaScript获取元素的文本内容
            text = await page.evaluate('''
                (element) => {
                    if (!element) return '';
                    return element.innerText || element.textContent || '';
                }
            ''', element)
            
            if text:
                Utils.log(f"成功获取元素文本: {text} ")
            else:
                Utils.log("元素没有文本内容")
                
            return text
            
        except Exception as e:
            Utils.log(f"获取元素文本时出错: {str(e)}")
            return ''

    @staticmethod
    async def click_element(page, element, element_name="元素"):
        """
        通用的点击元素方法，移动鼠标并点击指定元素
        Args:
            page: 页面对象
            element: 要点击的元素
            element_name: 元素名称（用于日志输出）

        Returns:
            bool: 是否成功点击
        """
        try:
            # 使用JavaScript直接获取元素的位置和浏览器窗口信息
            position_info = await page.evaluate('''
                (element) => {
                    if (!element) return null;
                    
                    // 获取元素的位置信息
                    const rect = element.getBoundingClientRect();
                    
                    // 获取浏览器窗口信息
                    const windowInfo = {
                        screenX: window.screenX,
                        screenY: window.screenY,
                        outerWidth: window.outerWidth,
                        outerHeight: window.outerHeight,
                        innerWidth: window.innerWidth,
                        innerHeight: window.innerHeight,
                        scrollX: window.pageXOffset || document.documentElement.scrollLeft,
                        scrollY: window.pageYOffset || document.documentElement.scrollTop
                    };
                    
                    // 计算非内容区域高度（工具栏、标签栏等）
                    const nonContentHeight = windowInfo.outerHeight - windowInfo.innerHeight;
                    
                    // 计算元素中心点相对于屏幕的坐标
                    const centerX = windowInfo.screenX + rect.left + (rect.width / 2);
                    const centerY = windowInfo.screenY + nonContentHeight + rect.top + (rect.height / 2);
                    
                    return {
                        centerX: centerX,
                        centerY: centerY,
                        elementRect: {
                            left: rect.left,
                            top: rect.top,
                            width: rect.width,
                            height: rect.height
                        },
                        windowInfo: windowInfo,
                        nonContentHeight: nonContentHeight
                    };
                }
            ''', element)
            
            if not position_info:
                Utils.log(f"获取{element_name}位置信息失败")
                return False
                
            # 获取点击坐标
            click_x = position_info['centerX']
            click_y = position_info['centerY']
            
            Utils.log(f"准备移动鼠标到{element_name}，目标位置: x={click_x}, y={click_y}")
            Utils.log(f"浏览器信息: 窗口位置=({position_info['windowInfo']['screenX']}, {position_info['windowInfo']['screenY']}), 非内容区高度={position_info['nonContentHeight']}px")
            Utils.log(f"元素信息: 相对位置=({position_info['elementRect']['left']}, {position_info['elementRect']['top']}), 大小=({position_info['elementRect']['width']}x{position_info['elementRect']['height']})")

            # 获取当前鼠标位置
            current_x, current_y = pyautogui.position()
            Utils.log(f"当前鼠标位置: x={current_x}, y={current_y}")
            
            # 计算移动距离
            distance = ((click_x - current_x) ** 2 + (click_y - current_y) ** 2) ** 0.5
            
            # 根据距离确定移动时间
            duration = min(1.0, max(0.3, distance / 1000))  # 距离越远，移动时间越长，但最短0.3秒，最长1秒
            
            Utils.log(f"鼠标移动: 距离={distance:.2f}px, 时间={duration:.2f}秒")
            
            # 平滑移动鼠标到点击位置
            pyautogui.moveTo(click_x, click_y, duration=duration, tween=pyautogui.easeOutQuad)
            
            # 随机等待一小段时间，模拟人类行为
            wait_time = 0.1 + random.random() * 0.2  # 0.1到0.3秒的随机等待
            time.sleep(wait_time)

            # 点击鼠标
            Utils.log(f"点击{element_name}")
            pyautogui.click()
            
            # 点击后随机等待
            wait_after_click = 0.1 + random.random() * 0.2  # 0.1到0.3秒的随机等待
            time.sleep(wait_after_click)

            Utils.log(f"已完成点击{element_name}")
            return True
            
        except Exception as e:
            Utils.log(f"点击{element_name}时出错: {str(e)}")
            return False

    @staticmethod
    async def wait_for_element_state(page, selector, selector_type="class", expect_exist=True, max_attempts=5, wait_time=1):
        """
        等待元素达到预期状态（存在或不存在），带有自动重试功能
        
        Args:
            page: 页面对象
            selector: 选择器字符串
            selector_type: 选择器类型，可选值：class, id, role, compound
            expect_exist: 预期元素是否存在，True表示等待元素存在，False表示等待元素不存在
            max_attempts: 最大尝试次数
            wait_time: 每次尝试之间的等待时间（秒）
            
        Returns:
            bool: 是否达到预期状态
        """
        # 使用build_selector方法构建完整的选择器
        full_selector = Utils.build_selector(selector, selector_type)
        Utils.log(f"开始{'检测元素存在' if expect_exist else '检测元素不存在'}: {full_selector}")
        
        for attempt in range(1, max_attempts + 1):
            try:
                # 获取元素
                elements = await Utils.get_element_by_css_selector(page, selector, selector_type)
                
                # 根据预期状态判断结果
                if expect_exist:
                    # 预期存在：如果找到元素，返回成功
                    if elements and len(elements) > 0:
                        Utils.log(f"元素已找到: {full_selector}，共 {len(elements)} 个元素")
                        return True
                    else:
                        if attempt < max_attempts:
                            Utils.log(f"元素未找到: {full_selector}，第 {attempt}/{max_attempts} 次尝试，等待 {wait_time} 秒后重试...")
                            await asyncio.sleep(wait_time)
                        else:
                            Utils.log(f"元素未找到: {full_selector}，已达到最大尝试次数 {max_attempts}")
                            return False
                else:
                    # 预期不存在：如果没找到元素，返回成功
                    if not elements or len(elements) == 0:
                        Utils.log(f"元素已确认不存在: {full_selector}")
                        return True
                    else:
                        if attempt < max_attempts:
                            Utils.log(f"元素仍然存在: {full_selector}，找到 {len(elements)} 个元素，第 {attempt}/{max_attempts} 次尝试，等待 {wait_time} 秒后重试...")
                            await asyncio.sleep(wait_time)
                        else:
                            Utils.log(f"元素仍然存在: {full_selector}，已达到最大尝试次数 {max_attempts}")
                            return False
                            
            except Exception as e:
                error_message = str(e)
                Utils.log(f"等待元素状态时出现异常: {error_message}")
                
                if any(err in error_message.lower() for err in ["页面连接已断开", "需要手动处理恢复"]):
                    Utils.log("连接已断开且无法自动恢复，需要手动处理")
                    return False
                
                if attempt < max_attempts:
                    # 如果不是最后一次尝试，等待后重试
                    Utils.log(f"第 {attempt}/{max_attempts} 次尝试出错，等待 {wait_time} 秒后重试...")
                    try:
                        await asyncio.sleep(wait_time)
                    except:
                        Utils.log("等待时出错，可能连接已断开")
                        return False
                else:
                    Utils.log(f"已达到最大尝试次数 {max_attempts}，返回失败结果")
                    return False
        
        # 如果执行到这里，说明已经尝试了最大次数但仍未达到预期状态
        return False

    @staticmethod
    def build_selector(selector, selector_type="class"):
        """
        根据选择器类型构建完整的CSS选择器
        
        Args:
            selector: 选择器字符串
            selector_type: 选择器类型，可选值：class, id, role, tag, name, compound
            
        Returns:
            str: 构建好的CSS选择器
        """
        if selector_type == "class":
            # 检查是否已经以点号开头
            if selector.startswith('.'):
                return selector.replace(" ", ".")
            else:
                return "." + selector.replace(" ", ".")
        elif selector_type == "id":
            # 检查是否已经以#号开头
            if selector.startswith('#'):
                return selector
            else:
                return "#" + selector
        elif selector_type == "role":
            return f'[role="{selector}"]'
        elif selector_type == "tag":
            return selector
        elif selector_type == "name":
            return f'[name="{selector}"]'
        elif selector_type == "compound" or selector_type == "css":
            return selector
        else:
            # 默认情况下直接返回选择器
            return selector
