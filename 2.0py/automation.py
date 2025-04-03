import json
import os
import asyncio
import time
from pyppeteer import launch
from platform_configs import PLATFORM_CONFIGS
from candidate_handler import CandidateHandler
from utils import Utils

# 反检测代码（提取为常量）
ANTI_DETECTION_SCRIPT = """
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
"""

class CandidateProcessor:
    """候选人处理器"""

    def __init__(self, page, platform_config, config_data, log_callback, play_sound_callback=None, update_greet_count_callback=None):
        self.page = page
        self.platform_config = platform_config
        self.config_data = config_data
        self.log_callback = log_callback
        self.play_sound_callback = play_sound_callback
        self.update_greet_count_callback = update_greet_count_callback
        self.handler = CandidateHandler(page, log_callback, config_data, play_sound_callback, update_greet_count_callback)
        self.api_config = PLATFORM_CONFIGS.get("api", {})

    async def process_candidates(self):
        """处理候选人列表"""
        try:
            elements_config = self.platform_config["page_elements"]
            candidate_config = elements_config["candidate_list"]
            dialog_config = elements_config["candidate_dialog"]

            # 等待候选人列表加载
            self.log_callback(f"等待 {candidate_config['description']} 加载...")

            # 根据选择器类型构建选择器
            selector = self._build_selector(candidate_config)
            self.log_callback(f"使用选择器: {selector}")

            # 添加重试机制
            max_retries = 5
            retry_count = 0
            last_processed_index = 0  # 记录上次处理到的位置
            last_candidates_count = 0  # 记录上次的候选人数量

            while retry_count < max_retries:
                try:
                    # 获取当前页面上的所有候选人
                    candidates = await Utils.get_element_by_css_selector(
                        self.page, 
                        candidate_config["selector"], 
                        candidate_config.get("selector_type", "class")
                    )
                    
                    current_candidates_count = len(candidates)
                    
                    if candidates:
                        self.log_callback(f"成功加载候选人列表，找到 {current_candidates_count} 个候选人")
                        
                        # 检查是否有新的候选人加载
                        if current_candidates_count > last_candidates_count:
                            self.log_callback(f"检测到新加载的候选人，新增 {current_candidates_count - last_candidates_count} 个")
                            last_candidates_count = current_candidates_count
                            retry_count = 0  # 重置重试计数
                        else:
                            retry_count += 1
                            if retry_count >= max_retries:
                                self.log_callback("没有新的候选人加载，处理完成")
                                break
                            
                        # 从上次处理的位置继续处理
                        for index in range(last_processed_index, current_candidates_count):
                            try:
                                # 确保每次处理候选人时都使用最新的配置
                                self.handler.config_data = self.config_data
                                await self.handler.handle_candidate(candidates[index], candidate_config, dialog_config, index + 1)
                                last_processed_index = index + 1  # 更新处理位置
                                
                                # 检查是否需要等待新的候选人加载
                                if index == current_candidates_count - 1:
                                    self.log_callback("已处理到当前列表的最后一个候选人，等待新的候选人加载...")
                                    await self.page.waitFor(2000)  # 等待2秒，让页面有时间加载新内容
                                    
                            except Exception as e:
                                self.log_callback(f"处理第 {index + 1} 个候选人时出错: {str(e)}")
                                continue
                    else:
                        retry_count += 1
                        self.log_callback(f"第 {retry_count} 次尝试：未找到候选人列表，等待1秒后重试...")
                        await asyncio.sleep(1)
                        
                except Exception as e:
                    retry_count += 1
                    self.log_callback(f"第 {retry_count} 次尝试：加载候选人列表失败 ({str(e)})，等待1秒后重试...")
                    await asyncio.sleep(1)

            if not candidates:
                self.log_callback("无法加载候选人列表，请检查页面是否正确加载")
                return 0  # 返回打招呼计数为0
            
            # 返回打招呼计数
            greet_count = self.handler.greet_count
            self.log_callback(f"本次共打招呼 {greet_count} 次")
            return greet_count

        except Exception as e:
            self.log_callback(f"处理候选人列表时出错: {str(e)}")
            return 0  # 出错时返回打招呼计数为0

    def _build_selector(self, candidate_config):
        """根据配置构建选择器"""
        selector = candidate_config["selector"]
        selector_type = candidate_config.get("selector_type", "css")

        if selector_type == "role":
            return f'[role="{selector}"]'
        elif selector_type == "class":
            return "." + selector.replace(" ", ".")
        elif selector_type == "compound":
            return selector
        else:
            return selector


class AutomationProcess:
    def __init__(self, config_data, log_callback, play_sound_callback=None, update_greet_count_callback=None):
        self.config_data = config_data
        self.log_callback = log_callback
        self.play_sound_callback = play_sound_callback
        self.update_greet_count_callback = update_greet_count_callback
        self.is_running = False
        self.browser = None
        self.page = None
        self.platform_config = self.load_platform_config()
        self.handler = None  # 候选人处理器
        self.greet_count = 0  # 记录打招呼计数

    def load_platform_config(self):
        """加载平台配置"""
        platform = self.config_data.get("platform")
        if platform not in PLATFORM_CONFIGS:
            self.log_callback(f"未找到平台 {platform} 的配置信息")
            return {}
        return PLATFORM_CONFIGS[platform]

    async def start(self):
        """启动自动化流程"""
        if not self.is_running:
            self.is_running = True
            self.log_callback("自动化流程开始运行")
            greet_count = await self.open_web_page()
            return greet_count  # 返回打招呼计数
        return 0  # 如果已经在运行，返回0

    async def stop(self):
        """停止自动化流程"""
        self.is_running = False
        if self.browser:
            await self.close_browser()

    async def close_browser(self):
        """关闭浏览器"""
        try:
            if self.browser:
                await self.browser.close()
                self.browser = None
                self.page = None
                self.log_callback("浏览器已关闭")
        except Exception as e:
            self.log_callback(f"关闭浏览器出错: {str(e)}")

    async def wait_for_target_page(self, target_url, timeout=300):
        """等待直到页面URL匹配目标URL，最多循环100次，每次间隔1秒"""
        self.log_callback("等待用户登录并进入目标页面...")
        
        # 最多循环100次
        for i in range(1, 101):
            self.log_callback(f"检查第 {i}/100 次")
            
            try:
                # 检查当前页面的URL
                if self.page:
                    try:
                        # 通过JavaScript获取当前页面URL
                        current_url = await self.page.evaluate("window.location.href")
                        self.log_callback(f"当前页面URL: {current_url}")
                        
                        # 检查URL是否包含目标URL
                        if target_url in current_url:
                            self.log_callback(f"已找到目标页面: {current_url}")
                            return True
                    except Exception as e:
                        self.log_callback(f"获取当前页面URL时出错: {str(e)}")
                
                # 如果未找到目标URL，等待1秒后继续
                await asyncio.sleep(1)
                
            except Exception as e:
                self.log_callback(f"检查过程中出错: {str(e)}")
                await asyncio.sleep(1)
        
        # 循环结束仍未找到目标页面
        self.log_callback("检查100次后未找到目标页面")
        return False

    async def open_web_page(self):
        """打开网页"""
        try:
            # 设置信号处理，避免"signal only works in main thread"错误
            import signal
            original_handler = None
            try:
                # 保存原始的SIGINT处理器
                original_handler = signal.getsignal(signal.SIGINT)
                # 设置为SIG_DFL（默认行为）
                signal.signal(signal.SIGINT, signal.SIG_DFL)
            except ValueError:
                # 如果在非主线程中，会抛出ValueError
                self.log_callback("警告：在非主线程中无法设置信号处理器")
            
            platform = self.config_data.get("platform")
            if platform not in PLATFORM_CONFIGS:
                raise Exception(f"未找到平台 {platform} 的配置信息")

            platform_info = PLATFORM_CONFIGS[platform]
            url = platform_info["url"]
            target_url = platform_info.get("target_url", url)


            # 从配置中获取浏览器路径
            chrome_path = self.config_data.get("browser_path", "")
            
            # 如果配置中没有浏览器路径或路径不存在，尝试查找Chrome
            if not chrome_path or not os.path.exists(chrome_path):
                if chrome_path:
                    self.log_callback(f"配置的浏览器路径不存在: {chrome_path}")
                
                # 尝试查找Chrome浏览器
                chrome_path = self._find_chrome_browser()
                
                if chrome_path:
                    self.log_callback(f"找到Chrome浏览器: {chrome_path}")
                    
                    # 更新配置中的浏览器路径
                    self.config_data["browser_path"] = chrome_path
                    try:
                        from eel_app import save_config
                        save_config()
                        self.log_callback("已更新配置中的浏览器路径")
                    except Exception as e:
                        self.log_callback(f"更新配置中的浏览器路径失败: {str(e)}")
                else:
                    self.log_callback("未找到Chrome浏览器，尝试下载便携版Chrome")
                    
                    # 下载便携版Chrome浏览器
                    chrome_path = await self._download_portable_chrome()
                    
                    if not chrome_path:
                        self.log_callback("下载Chrome浏览器失败，无法继续")
                        return 0  # 返回打招呼计数为0
                    
                    # 更新配置中的浏览器路径
                    self.config_data["browser_path"] = chrome_path
                    try:
                        from eel_app import save_config
                        save_config()
                        self.log_callback("已更新配置中的浏览器路径")
                    except Exception as e:
                        self.log_callback(f"更新配置中的浏览器路径失败: {str(e)}")

            self.log_callback(f"使用Chrome浏览器: {chrome_path}")
            # 使用try-except块包装launch调用
            try:
                self.browser = await launch({
                    'headless': False,
                    'executablePath': chrome_path,
                    'args': [
                        '--no-sandbox',
                        '--start-maximized',
                        '--disable-infobars',
                        '--disable-blink-features=AutomationControlled',
                        '--window-size=1920,1080',
                        '--disable-gpu',
                        '--no-first-run',
                        '--password-store=basic',
                        '--disable-notifications',
                        '--disable-popup-blocking',
                        '--lang=zh-CN'
                    ],
                    'ignoreDefaultArgs': ['--enable-automation'],
                    'defaultViewport': None,
                    'handleSIGINT': False,  # 禁用Pyppeteer的SIGINT处理
                    'handleSIGTERM': False, # 禁用Pyppeteer的SIGTERM处理
                    'handleSIGHUP': False   # 禁用Pyppeteer的SIGHUP处理
                })
            except Exception as e:
                self.log_callback(f"启动浏览器失败: {str(e)}")
                # 如果是信号相关错误，提供更详细的信息
                if "signal" in str(e).lower():
                    self.log_callback("这可能是由于在非主线程中使用信号处理导致的。尝试在主线程中运行或使用其他方法启动浏览器。")
                return 0

            # 恢复原始的信号处理器
            if original_handler:
                try:
                    signal.signal(signal.SIGINT, original_handler)
                except ValueError:
                    pass

            self.page = await self.browser.newPage()
            
            # 注入反检测代码
            await Utils.inject_anti_detection_js(self.page)
            
            # 设置请求头
            await Utils.set_browser_headers(self.page)
            self.log_callback(f"正在打开{url}")
            
            # 修改等待条件和添加错误处理
            try:
                # 使用更宽松的等待条件
                await self.page.goto(url, {'waitUntil': 'domcontentloaded', 'timeout': 30000})
                self.log_callback(f"页面基本内容已加载")
            except Exception as e:
                # 即使超时，如果页面已经打开，我们也继续执行
                self.log_callback(f"页面加载可能不完整，但尝试继续: {str(e)}")
                # 再等待一段时间，让页面有机会进一步加载
                await asyncio.sleep(5)
            
            self.log_callback(f"等待打开{target_url}")

            if not await self.wait_for_target_page(target_url):
                self.log_callback("未能进入目标页面，停止自动化流程")
                await self.stop()
                return 0  # 返回打招呼计数为0

            self.log_callback("开始处理候选人...")
            processor = CandidateProcessor(
                self.page, 
                platform_info, 
                self.config_data, 
                self.log_callback, 
                self.play_sound_callback,
                self.update_greet_count_callback
            )
            greet_count = await processor.process_candidates()
            
            return greet_count  # 返回打招呼计数

        except Exception as e:
            self.log_callback(f"打开网页失败: {str(e)}")
            if self.browser:
                await self.close_browser()
            return 0  # 出错时返回打招呼计数为0

    def _find_chrome_browser(self):
        """查找Chrome浏览器安装位置"""
        # 可能的Chrome可执行文件名列表
        chrome_exe_names = [
            "chrome.exe",
            "chromee.exe",  # 用户自定义名称
            "googlechrome.exe",
            "chrome_browser.exe",
            "google-chrome.exe"
        ]
        
        # 可能的Chrome安装路径列表（按优先级排序）
        base_paths = [
            # 标准安装路径
            r"C:\Program Files\Google\Chrome\Application",
            r"C:\Program Files (x86)\Google\Chrome\Application",
            
            # 用户目录安装路径
            os.path.join(os.environ.get('LOCALAPPDATA', ''), r"Google\Chrome\Application"),
            os.path.join(os.environ.get('PROGRAMFILES', ''), r"Google\Chrome\Application"),
            os.path.join(os.environ.get('PROGRAMFILES(X86)', ''), r"Google\Chrome\Application"),
            
            # 其他可能的安装路径
            r"C:\Program Files\Google\Chrome Beta\Application",
            r"C:\Program Files (x86)\Google\Chrome Beta\Application",
            r"C:\Program Files\Google\Chrome Dev\Application",
            r"C:\Program Files (x86)\Google\Chrome Dev\Application",
            r"C:\Program Files\Google\Chrome SxS\Application",
            r"C:\Program Files (x86)\Google\Chrome SxS\Application",
            
            # 企业版Chrome
            r"C:\Program Files\Google\Chrome Enterprise\Application",
            r"C:\Program Files (x86)\Google\Chrome Enterprise\Application",
            
            # 便携版Chrome可能的位置
            os.path.join(os.getcwd(), "chrome-portable"),
            os.path.join(os.getcwd(), "chrome"),
            
            # 桌面和下载文件夹
            os.path.join(os.path.expanduser("~"), "Desktop"),
            os.path.join(os.path.expanduser("~"), "Downloads"),
            
            # 其他常见位置
            r"D:\Program Files\Google\Chrome\Application",
            r"D:\Program Files (x86)\Google\Chrome\Application",
            r"E:\Program Files\Google\Chrome\Application",
            r"E:\Program Files (x86)\Google\Chrome\Application",
        ]
        
        # 生成所有可能的Chrome路径
        possible_paths = []
        for base_path in base_paths:
            for exe_name in chrome_exe_names:
                possible_paths.append(os.path.join(base_path, exe_name))
        
        # 添加其他浏览器（基于Chromium的）
        other_browsers = [
            # Edge浏览器
            r"C:\Program Files\Microsoft\Edge\Application\msedge.exe",
            r"C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe",
            os.path.join(os.environ.get('LOCALAPPDATA', ''), r"Microsoft\Edge\Application\msedge.exe"),
            
            # 360浏览器
            r"C:\Program Files\360\360Chrome\Chrome\Application\360chrome.exe",
            r"C:\Program Files (x86)\360\360Chrome\Chrome\Application\360chrome.exe",
            
            # QQ浏览器
            r"C:\Program Files\Tencent\QQBrowser\QQBrowser.exe",
            r"C:\Program Files (x86)\Tencent\QQBrowser\QQBrowser.exe",
            
            # 搜狗浏览器
            r"C:\Program Files\Sogou\SogouExplorer\SogouExplorer.exe",
            r"C:\Program Files (x86)\Sogou\SogouExplorer\SogouExplorer.exe",
        ]
        
        possible_paths.extend(other_browsers)
        
        # 检查注册表中的Chrome安装路径
        try:
            import winreg
            for root_key in [winreg.HKEY_CURRENT_USER, winreg.HKEY_LOCAL_MACHINE]:
                for sub_key in [
                    r"Software\Google\Chrome\BLBeacon",
                    r"Software\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe"
                ]:
                    try:
                        with winreg.OpenKey(root_key, sub_key) as key:
                            chrome_path = winreg.QueryValue(key, None)
                            if chrome_path and os.path.exists(chrome_path):
                                return chrome_path
                    except:
                        pass
        except:
            pass
        
        # 检查环境变量PATH中的Chrome
        try:
            path_dirs = os.environ.get('PATH', '').split(os.pathsep)
            for path_dir in path_dirs:
                for exe_name in chrome_exe_names:
                    chrome_path = os.path.join(path_dir, exe_name)
                    if os.path.exists(chrome_path):
                        return chrome_path
        except:
            pass
        
        # 检查可能的安装路径
        for path in possible_paths:
            if os.path.exists(path):
                return path
        
        # 递归搜索常见目录
        common_dirs = [
            os.path.expanduser("~"),  # 用户主目录
            "C:\\",                   # C盘根目录
            "D:\\",                   # D盘根目录（如果存在）
            os.getcwd()               # 当前工作目录
        ]
        
        for dir_path in common_dirs:
            if os.path.exists(dir_path):
                try:
                    chrome_path = self._search_chrome_in_directory(dir_path)
                    if chrome_path:
                        return chrome_path
                except:
                    pass
        
        # 如果都没找到，返回None
        return None
        
    def _search_chrome_in_directory(self, directory, max_depth=3, current_depth=0):
        """递归搜索目录中的Chrome浏览器"""
        if current_depth > max_depth:
            return None
            
        chrome_exe_names = ["chrome.exe", "chromee.exe", "googlechrome.exe", "msedge.exe"]
        
        try:
            for item in os.listdir(directory):
                full_path = os.path.join(directory, item)
                
                # 检查是否是文件
                if os.path.isfile(full_path):
                    if item.lower() in chrome_exe_names:
                        return full_path
                
                # 检查是否是目录
                elif os.path.isdir(full_path):
                    # 跳过一些不太可能包含浏览器的目录
                    skip_dirs = ["Windows", "Program Files", "Program Files (x86)", "ProgramData", "System32", "SysWOW64"]
                    if item in skip_dirs and current_depth > 0:
                        continue
                        
                    # 递归搜索子目录
                    chrome_path = self._search_chrome_in_directory(full_path, max_depth, current_depth + 1)
                    if chrome_path:
                        return chrome_path
        except:
            pass
            
        return None

    async def _download_portable_chrome(self):
        """下载便携版Chrome浏览器"""
        try:
            import requests
            import zipfile
            import tempfile
            import shutil
            
            # 创建Chrome目录
            chrome_dir = os.path.join(os.getcwd(), "chrome-portable")
            if not os.path.exists(chrome_dir):
                os.makedirs(chrome_dir)
            
            # 检查是否已经有下载好的便携版Chrome
            existing_chrome = os.path.join(chrome_dir, "chrome.exe")
            if os.path.exists(existing_chrome):
                self.log_callback(f"已找到现有的便携版Chrome: {existing_chrome}")
                return existing_chrome
                
            # 便携版Chrome下载地址列表（按优先级排序）
            chrome_urls = [
                "https://goodhr.58it.cn/chrome-portable.zip",  # 主服务器
                "https://cdn.jsdelivr.net/gh/good-hr/chrome-portable/chrome-portable.zip",  # 备用CDN
                "https://raw.githubusercontent.com/good-hr/chrome-portable/main/chrome-portable.zip"  # GitHub备用
            ]
            
            # 尝试从不同的URL下载
            downloaded = False
            temp_path = None
            
            for url in chrome_urls:
                try:
                    self.log_callback(f"正在尝试从 {url} 下载Chrome便携版...")
                    
                    # 创建临时文件
                    with tempfile.NamedTemporaryFile(delete=False, suffix='.zip') as temp_file:
                        temp_path = temp_file.name
                    
                    # 下载文件
                    response = requests.get(url, stream=True, timeout=30)
                    if response.status_code != 200:
                        self.log_callback(f"从 {url} 下载失败，HTTP状态码: {response.status_code}")
                        continue
                        
                    total_size = int(response.headers.get('content-length', 0))
                    
                    # 检查文件大小是否合理（大约20MB左右）
                    if total_size > 0 and (total_size < 10 * 1024 * 1024 or total_size > 50 * 1024 * 1024):
                        self.log_callback(f"文件大小异常: {total_size / (1024 * 1024):.2f} MB，跳过此下载源")
                        continue
                    
                    downloaded = 0
                    
                    with open(temp_path, 'wb') as f:
                        for chunk in response.iter_content(chunk_size=8192):
                            if chunk:
                                f.write(chunk)
                                downloaded += len(chunk)
                                # 计算下载进度
                                progress = int(downloaded / total_size * 100) if total_size > 0 else 0
                                if progress % 10 == 0 and progress > 0:  # 每下载10%显示一次进度
                                    self.log_callback(f"下载进度: {progress}%")
                    
                    # 检查下载是否完成
                    if total_size > 0 and downloaded < total_size * 0.9:
                        self.log_callback(f"下载不完整: 已下载 {downloaded / (1024 * 1024):.2f} MB，预期 {total_size / (1024 * 1024):.2f} MB")
                        continue
                    
                    downloaded = True
                    self.log_callback(f"从 {url} 下载成功，文件大小: {downloaded / (1024 * 1024):.2f} MB")
                    break
                    
                except Exception as e:
                    self.log_callback(f"从 {url} 下载时出错: {str(e)}")
                    if temp_path and os.path.exists(temp_path):
                        try:
                            os.unlink(temp_path)
                        except:
                            pass
                    temp_path = None
            
            if not downloaded or not temp_path or not os.path.exists(temp_path):
                self.log_callback("所有下载源均失败")
                return None
            
            self.log_callback("下载完成，正在解压...")
            
            try:
                # 解压文件
                with zipfile.ZipFile(temp_path, 'r') as zip_ref:
                    zip_ref.extractall(chrome_dir)
                
                # 删除临时文件
                os.unlink(temp_path)
            except Exception as e:
                self.log_callback(f"解压文件时出错: {str(e)}")
                if temp_path and os.path.exists(temp_path):
                    try:
                        os.unlink(temp_path)
                    except:
                        pass
                return None
            
            # 查找chrome.exe
            chrome_exe_path = None
            
            # 首先检查根目录
            direct_chrome = os.path.join(chrome_dir, "chrome.exe")
            if os.path.exists(direct_chrome):
                chrome_exe_path = direct_chrome
            else:
                # 递归查找chrome.exe
                for root, dirs, files in os.walk(chrome_dir):
                    for file in files:
                        if file.lower() == "chrome.exe":
                            chrome_exe_path = os.path.join(root, file)
                            break
                    if chrome_exe_path:
                        break
            
            if not chrome_exe_path:
                self.log_callback("在解压后的文件中未找到chrome.exe")
                return None
            
            self.log_callback(f"成功下载并解压Chrome便携版: {chrome_exe_path}")
            return chrome_exe_path
            
        except Exception as e:
            self.log_callback(f"下载Chrome便携版失败: {str(e)}")
            return None

    async def process_candidate_list(self, candidates, candidate_config, dialog_config):
        """处理候选人列表"""
        try:
            # 创建候选人处理器
            self.handler = CandidateHandler(
                self.page, 
                self.log_callback, 
                self.config_data,
                play_sound_callback=self.play_sound_callback,
                update_greet_count_callback=self.update_greet_count_callback
            )
            
            # 设置最大打招呼数量限制
            max_greet_count = self.config_data.get("max_greet_count", 0)
            if max_greet_count > 0:
                self.handler.max_greet_count = max_greet_count
                self.log_callback(f"已设置最大打招呼数量限制为 {max_greet_count} 次")
            
            # 检查候选人列表是否为空
            if not candidates:
                self.log_callback("未找到候选人列表，请检查选择器是否正确")
                return 0  # 返回打招呼计数为0

            # 遍历处理每个候选人
            for index, candidate in enumerate(candidates, 1):
                try:
                    # 确保每次处理候选人时都使用最新的配置
                    self.handler.config_data = self.config_data
                    await self.handler.handle_candidate(candidate, candidate_config, dialog_config, index)
                except Exception as e:
                    self.log_callback(f"处理第 {index} 个候选人时出错: {str(e)}")
                    continue
            
            # 返回打招呼计数
            greet_count = self.handler.greet_count
            self.log_callback(f"本次共打招呼 {greet_count} 次")
            return greet_count

        except Exception as e:
            self.log_callback(f"处理候选人列表时出错: {str(e)}")
            return 0  # 出错时返回打招呼计数为0