import eel
import eel.browsers  # 导入browsers模块，用于设置浏览器路径
import json
import os
import asyncio
import threading
import time
from datetime import datetime, timedelta
import requests
import tkinter as tk
from tkinter import filedialog
from automation import AutomationProcess
import pygame  # 导入pygame用于播放声音
import webbrowser  # 导入webbrowser用于打开网页
import sys
import re
import platform
import random
import traceback
import subprocess
import logging
from api_client import ApiClient  # 导入API客户端
from platform_configs import PLATFORM_CONFIGS, refresh_platform_configs

# 初始化 Eel
eel.init('web')

# 初始化pygame混音器
pygame.mixer.init()

# 创建全局API客户端实例
api_client = ApiClient("https://goodhr.58it.cn/api/service/api/", logger=logging.getLogger("ApiClient"))

# 定义日志函数
def add_log(message):
    """
    添加日志（同时输出到控制台和前端）
    Args:
        message: 日志消息
    """
    try:
        # 获取当前时间
        now = datetime.now().strftime("%H:%M:%S")
        
        # 格式化日志消息
        log_message = f"[{now}] {message}"
        
        # 输出到控制台
        print(log_message)
        
        # 输出到前端
        try:
            eel.addLogFromPython(log_message)
        except:
            pass
            
        # 保存到日志文件
        try:
            today = datetime.now().strftime("%Y-%m-%d")
            log_file = os.path.join(log_dir, f"log_{today}.txt")
            
            with open(log_file, "a", encoding="utf-8") as f:
                f.write(log_message + "\n")
        except Exception as e:
            print(f"保存日志到文件失败: {str(e)}")
    except Exception as e:
        print(f"记录日志时出错: {str(e)}")

# 从线程中添加日志
def add_log_from_thread(message):
    """从线程中安全添加日志"""
    # 使用 eel.sleep 确保在主线程中调用
    eel.sleep(0)
    add_log(message)

# 全局变量
config_data = {}
automation = None
is_running = False
log_dir = "运行日志logs"
version_data_file = "version_data.json"
sound_enabled = True  # 声音开关默认开启
CURRENT_VERSION = "2.0"  # 当前程序版本号
UPDATE_URL = "https://goodhr.58it.cn"  # 更新网址

# 确保日志目录存在
if not os.path.exists(log_dir):
    os.makedirs(log_dir)

# 初始化ConfigManager
try:
    from config_manager import ConfigManager
    # 检查配置文件是否存在
    config_file_path = "config.json"
    if not os.path.exists(config_file_path):
        add_log(f"配置文件 {config_file_path} 不存在，将创建默认配置")
    
    config_manager = ConfigManager(config_file_path)
    # 设置日志函数
    from config_manager import add_log_func
    add_log_func = add_log
    add_log("ConfigManager初始化成功")
except ImportError as e:
    error_msg = f"导入ConfigManager模块失败: {str(e)}"
    print(error_msg)
    add_log(error_msg)
    config_manager = None
except Exception as e:
    error_msg = f"初始化ConfigManager失败: {str(e)}"
    print(error_msg)
    add_log(error_msg)
    # 打印详细的错误堆栈
    traceback.print_exc()
    config_manager = None

@eel.expose
def check_version_and_announcement():
    """检查版本更新和公告"""
    try:
        # 请求版本信息
        response = requests.get("https://goodhr.58it.cn/ai-v.json")
        if response.status_code == 200:
            data = response.json()
            server_version = data.get("version")
            release_notes = data.get("releaseNotes", "")
            announcement = data.get("gonggao")
            
            update_info = {
                "needUpdate": server_version != CURRENT_VERSION,
                "currentVersion": CURRENT_VERSION,  # 添加当前版本
                "version": server_version,
                "releaseNotes": release_notes,
                "forceUpdate": "必须更新" in release_notes if release_notes else False,
                "announcement": announcement,
                "updateUrl": UPDATE_URL
            }
            
            add_log(f"版本检查结果: 当前版本={CURRENT_VERSION}, 服务器版本={server_version}")
            if update_info["needUpdate"]:
                add_log(f"发现新版本: {server_version}, 更新说明: {release_notes}")
            if announcement:
                add_log(f"收到公告: {announcement}")
                
            return update_info
    except Exception as e:
        add_log(f"检查版本更新失败: {str(e)}")
        return {
            "needUpdate": False,
            "currentVersion": CURRENT_VERSION,  # 添加当前版本
            "version": CURRENT_VERSION,
            "releaseNotes": "",
            "forceUpdate": False,
            "announcement": None,
            "updateUrl": UPDATE_URL
        }

@eel.expose
def open_update_url():
    """打开更新网址"""
    try:
        webbrowser.open(UPDATE_URL)
        add_log(f"已打开更新网址: {UPDATE_URL}")
        return True
    except Exception as e:
        add_log(f"打开更新网址失败: {str(e)}")
        return False

# 播放声音函数
def play_sound(sound_file):
    if sound_enabled:
        try:
            # 确保声音文件存在
            sound_path = os.path.join("web", "sounds", sound_file)
            if not os.path.exists(sound_path):
                sound_path = os.path.join("sounds", sound_file)
                if not os.path.exists(sound_path):
                    sound_path = sound_file
                    if not os.path.exists(sound_path):
                        add_log(f"找不到声音文件: {sound_file}")
                        return
            
            # 播放声音
            pygame.mixer.music.load(sound_path)
            pygame.mixer.music.play()
            add_log(f"播放声音: {sound_file}")
        except Exception as e:
            add_log(f"播放声音出错: {str(e)}")

# 设置声音开关
@eel.expose
def set_sound_enabled(enabled):
    global sound_enabled
    sound_enabled = enabled
    save_config_item("sound_enabled", enabled, "声音开关")
    add_log(f"声音已{'开启' if enabled else '关闭'}")
    return sound_enabled

# 设置打招呼数量限制
@eel.expose
def set_max_greet_count(count):
    try:
        count = int(count)
        save_config_item("max_greet_count", count, "打招呼数量限制")
        add_log(f"已设置打招呼数量限制为 {count} 次")
        return True
    except Exception as e:
        add_log(f"设置打招呼数量限制出错: {str(e)}")
        return False

@eel.expose
def get_config():
    """
    获取完整配置数据
    Returns:
        Dict: 配置数据
    """
    try:
        if config_manager:
            return config_manager.load_config()
        else:
            # 如果ConfigManager初始化失败，尝试手动加载配置文件
            add_log("ConfigManager初始化失败，尝试手动加载配置文件")
            try:
                if os.path.exists("config.json"):
                    with open("config.json", 'r', encoding='utf-8') as f:
                        config = json.load(f)
                    add_log("成功手动加载config.json")
                    return config
                else:
                    add_log("配置文件不存在，返回空配置")
                    return {}
            except Exception as e:
                add_log(f"手动加载配置文件失败: {str(e)}")
                return {}
    except Exception as e:
        add_log(f"获取配置出错: {str(e)}")
        return {}

@eel.expose
def get_config_item(key):
    """
    获取指定配置项
    Args:
        key: 配置项键名
    Returns:
        Any: 配置项的值
    """
    try:
        if config_manager:
            return config_manager.get_config_item(key)
        else:
            # 如果ConfigManager初始化失败，尝试手动获取配置项
            add_log(f"ConfigManager初始化失败，尝试手动获取配置项: {key}")
            try:
                if os.path.exists("config.json"):
                    with open("config.json", 'r', encoding='utf-8') as f:
                        config = json.load(f)
                    if key in config:
                        return config[key]
                    else:
                        add_log(f"配置项 {key} 不存在")
                        return None
                else:
                    add_log("配置文件不存在")
                    return None
            except Exception as e:
                add_log(f"手动获取配置项失败: {str(e)}")
                return None
    except Exception as e:
        add_log(f"获取配置项{key}出错: {str(e)}")
        return None

# 加载配置文件
def load_config():
    """
    兼容旧版本的配置加载函数，仅在ConfigManager初始化失败时使用
    """
    global config_data
    
    # 尝试查找系统Chrome浏览器路径
    default_chrome_paths = [
        r"C:\Program Files\Google\Chrome\Application\chrome.exe",
        r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe"
    ]
    
    # 查找已安装的Chrome浏览器
    system_chrome_path = None
    for path in default_chrome_paths:
        if os.path.exists(path):
            system_chrome_path = path
            break
            
    # 如果没有找到系统Chrome，使用下载的浏览器
    default_browser_path = system_chrome_path if system_chrome_path else r"chromee.exe"
    
    # 获取今天的日期
    today = datetime.now().date()
    
    # 初始化默认配置
    default_config = {
        "version": "",
        "platform": "",
        "username": "",
        "selected_job_id": "",
        "userID": "",
        "browser_path": default_browser_path,
        "jobsData": [],
        "keywordsData": {},
        "selectedJob": "",
        # 为每个版本创建独立的配置对象
        "versions": {
            "free": {
                "greetCount": 0,
                "remainingQuota": 100,
                "expiryDate": "永久有效",
                "lastResetDate": today.strftime("%Y-%m-%d")
            },
            "donation": {
                "greetCount": 0,
                "remainingQuota": 0,
                "expiryDate": (today + timedelta(days=30)).strftime("%Y-%m-%d"),
                "lastResetDate": today.strftime("%Y-%m-%d")
            },
            "enterprise": {
                "greetCount": 0,
                "remainingQuota": 0,
                "expiryDate": "余额不足",
                "lastResetDate": today.strftime("%Y-%m-%d")
            }
        }
    }
    
    try:
        if os.path.exists("config.json"):
            with open("config.json", "r", encoding="utf-8") as f:
                loaded_config = json.load(f)
                config_data = loaded_config
        else:
            config_data = default_config
            with open("config.json", "w", encoding="utf-8") as f:
                json.dump(config_data, f, ensure_ascii=False, indent=4)
    except Exception as e:
        add_log(f"加载配置文件失败，使用默认配置: {str(e)}")
        config_data = default_config

# 获取版本信息
@eel.expose
def get_version_info():
    """
    获取当前版本信息
    Returns:
        Dict: 版本信息
    """
    try:
        global config_data
        
        # 获取当前用户手机号
        phone = config_data.get("username", "")
        if not phone or not isinstance(phone, str) or len(phone) != 11:
            add_log("未找到有效的用户手机号，无法获取版本信息")
            return {}
        
        # 从最新的用户配置中获取版本信息
        user_config = api_client.get_user_config(phone)
        if user_config:
            # 更新全局配置
            config_data = user_config
            add_log("已从服务器获取最新用户配置")
        else:
            add_log("从服务器获取配置失败，使用本地配置")
        
        # 获取当前版本
        current_version = config_data.get("version", "free")
        versions = config_data.get("versions", {})
        
        # 从versions字典中获取当前版本的数据
        current_version_data = {}
        if current_version in versions:
            current_version_data = versions[current_version]
        else:
            add_log(f"未找到当前版本({current_version})的数据")
            
        # 构造版本信息
        version_info = {
            "version": current_version,
            "greetCount": current_version_data.get("greetCount", 0),
            "remainingQuota": current_version_data.get("remainingQuota", 0),
            "expiryDate": current_version_data.get("expiryDate", "")
        }
        
        add_log(f"向前端发送版本信息: {version_info}")
        
        # 通知前端更新版本信息
        eel.updateVersionInfoFromPython(version_info)
        
        return version_info
    except Exception as e:
        add_log(f"获取版本信息出错: {str(e)}")
        return {}

# 保存配置项
@eel.expose
def save_config_item(key, value, display_name=None):
    """
    保存单个配置项
    Args:
        key: 配置项键名
        value: 配置项的值
        display_name: 显示名称
    Returns:
        bool: 是否保存成功
    """
    try:
        global config_data
        
        # 更新全局配置
        config_data[key] = value
        
        # 尝试通过ConfigManager保存配置
        if config_manager:
            add_log(f"保存配置项: {display_name or key}")
            result = config_manager.save_config_item(key, value, display_name)
            
            # 同步到服务器
            phone = config_data.get("username", "")
            if phone and isinstance(phone, str) and len(phone) == 11:
                # 确保配置中包含phone字段
                config_to_update = dict(config_data)
                config_to_update["phone"] = phone
                
                # 通过API更新服务器配置
                if api_client.update_user_config(config_to_update):
                    add_log("配置已同步到服务器")
                else:
                    add_log("同步到服务器失败")
            
            return result
        else:
            # 如果ConfigManager初始化失败，尝试手动保存配置项
            add_log(f"ConfigManager初始化失败，尝试手动保存配置项: {display_name or key}")
            try:
                # 保存配置
                with open("config.json", 'w', encoding='utf-8') as f:
                    json.dump(config_data, f, ensure_ascii=False, indent=4)
                
                # 同步到服务器
                phone = config_data.get("username", "")
                if phone and isinstance(phone, str) and len(phone) == 11:
                    # 确保配置中包含phone字段
                    config_to_update = dict(config_data)
                    config_to_update["phone"] = phone
                    
                    # 通过API更新服务器配置
                    if api_client.update_user_config(config_to_update):
                        add_log("配置已同步到服务器")
                    else:
                        add_log("同步到服务器失败")
                
                add_log(f"成功手动保存配置项: {display_name or key}")
                return True
            except Exception as e:
                add_log(f"手动保存配置项失败: {str(e)}")
                return False
    except Exception as e:
        add_log(f"保存配置项{display_name or key}出错: {str(e)}")
        return False

# 更新打招呼计数
def update_greet_count(count=1, tokens_used=0):
    """
    更新打招呼计数
    Args:
        count: 增加的次数，默认为1。如果为0，则只更新token使用情况
        tokens_used: 使用的token数量，默认为0
    Returns:
        Dict: 更新后的版本信息
    """
    try:
        global config_data
        
        # 获取当前用户手机号
        phone = config_data.get("username", "")
        if not phone or not isinstance(phone, str) or len(phone) != 11:
            add_log("未找到有效的用户手机号，无法更新打招呼计数")
            return {}
        
        # 记录token使用情况（如果有）
        if tokens_used > 0:
            add_log(f"本次分析消耗了 {tokens_used} 个tokens")
            
            # 注：token上传已经由AIAnalyzer处理，这里不再重复上传
        
        # 如果count为0，只更新token使用情况，不增加打招呼次数
        if count == 0:
            return {}
        
        # 先尝试通过API更新打招呼计数
        result = api_client.update_greet_count(phone, tokens_used)
        
        if result:
            add_log(f"打招呼计数更新成功: {result}")
            
            # 更新本地版本信息
            if config_data.get("versions") and config_data.get("version"):
                version = config_data.get("version")
                if version in config_data.get("versions"):
                    config_data["versions"][version]["greetCount"] = result.get("greetCount", 0)
                    config_data["versions"][version]["remainingQuota"] = result.get("remainingQuota", 0)
                    
                    # 如果服务器返回了token使用情况，也更新到本地
                    if "totalTokensUsed" in result:
                        config_data["versions"][version]["totalTokensUsed"] = result.get("totalTokensUsed", 0)
            
            # 通知前端更新版本信息
            eel.updateVersionInfoFromPython(result)
            
            return result
        
        # 如果API更新失败，尝试通过ConfigManager更新
        if config_manager:
            version_info = config_manager.update_greet_count(count, tokens_used)
            
            # 通知前端更新版本信息
            eel.updateVersionInfoFromPython(version_info)
            
            return version_info
        
        return {}
    except Exception as e:
        add_log(f"更新打招呼计数出错: {str(e)}")
        return {}

# 获取岗位列表
@eel.expose
def fetch_job_list(phone):
    """
    从服务器获取岗位列表
    Args:
        phone: 手机号
    Returns:
        Dict: 岗位列表数据
    """
    try:
        # 验证手机号格式
        if not phone or not isinstance(phone, str) or len(phone) != 11 or not phone.isdigit() or not phone.startswith('1'):
            add_log(f"手机号 {phone} 格式不正确，请输入正确的11位手机号码")
            return {"success": False, "message": "手机号格式不正确，请输入正确的11位手机号码"}
            
        
        # 首先尝试通过API获取用户配置
        user_config = api_client.get_user_config(phone)
        
        if user_config:
            # 更新全局配置
            global config_data
            config_data = user_config
            
            # 保存到本地配置文件（可选）
            with open("config.json", "w", encoding="utf-8") as f:
                json.dump(config_data, f, ensure_ascii=False, indent=4)
            
            add_log(f"成功获取用户 {phone} 的配置")
            
            # 构造返回数据
            result = {
                "success": True,
                "jobs": user_config.get("jobsData", []),
                "keywords": user_config.get("keywordsData", {})
            }
            
            # 记录返回的数据
            add_log(f"返回岗位数据: {result['jobs']}")
            add_log(f"返回关键词数据: {len(result['keywords'].keys()) if result['keywords'] else 0} 个岗位的关键词")
            
            return result
            
        # 如果API获取失败，尝试使用ConfigManager
        add_log(f"从服务器获取配置失败，尝试使用本地配置")
        
        if config_manager:
            # 保存手机号 - 这里只保存手机号，不修改其他配置
            current_config = config_manager.load_config()
            if current_config.get("username") != phone:
                add_log(f"保存手机号 {phone} 到配置中")
                save_config_item("username", phone, "手机号")
            else:
                add_log(f"配置中已存在手机号 {phone}，无需保存")
            
            # 初始化配置（优先使用服务器配置，如果服务器没有则创建默认配置）
            add_log("准备调用 initialize_with_phone 方法...")
            initialized = config_manager.initialize_with_phone(phone)
            add_log(f"initialize_with_phone 方法执行结果: {initialized}")
            
            if initialized:
                # 获取完整配置
                config = config_manager.config_data
                
                # 构造返回数据
                result = {
                    "success": True,
                    "jobs": config.get("jobsData", []),
                    "keywords": config.get("keywordsData", {})
                }
                
                # 记录返回的数据
                add_log(f"返回岗位数据: {result['jobs']}")
                add_log(f"返回关键词数据: {len(result['keywords'].keys()) if result['keywords'] else 0} 个岗位的关键词")
                
                return result
            else:
                add_log("初始化配置失败")
                return {"success": False, "message": "初始化配置失败"}
        else:
            add_log("ConfigManager初始化失败，也无法从服务器获取配置")
            return {"success": False, "message": "获取岗位列表失败"}
    except Exception as e:
        add_log(f"获取岗位列表出错: {str(e)}")
        traceback.print_exc()  # 打印详细的错误堆栈
        return {"success": False, "message": f"获取岗位列表失败: {str(e)}"}

# 选择浏览器
@eel.expose
def select_browser():
    try:
        # 创建一个临时的 Tkinter 根窗口
        root = tk.Tk()
        root.withdraw()  # 隐藏窗口
        
        # 获取当前浏览器路径的目录
        initial_dir = os.path.dirname(config_data.get("browser_path", ""))
        if not initial_dir or not os.path.exists(initial_dir):
            # 尝试查找系统Chrome浏览器路径
            default_chrome_paths = [
                r"C:\Program Files\Google\Chrome\Application",
                r"C:\Program Files (x86)\Google\Chrome\Application",
                os.path.expanduser("~") + r"\AppData\Local\Google\Chrome\Application"
            ]
            
            for path in default_chrome_paths:
                if os.path.exists(path):
                    initial_dir = path
                    break
        
        # 打开文件选择对话框
        browser_path = filedialog.askopenfilename(
            title="选择Chrome浏览器可执行文件",
            initialdir=initial_dir,
            filetypes=[("Chrome浏览器", "chrome.exe"), ("可执行文件", "*.exe")]
        )
        
        # 销毁临时窗口
        root.destroy()
        
        if browser_path:
            # 保存浏览器路径
            save_config_item("browser_path", browser_path, "浏览器路径")
            add_log(f"已更新浏览器路径: {browser_path}")
            return browser_path
        
        return None
    except Exception as e:
        add_log(f"选择浏览器出错: {str(e)}")
        return None

# 开始自动化流程
@eel.expose
def start_automation(automation_config=None):
    global automation, is_running, config_data, sound_enabled
    
    if not is_running:
        try:
            # 刷新平台配置
            try:
                from platform_configs import refresh_platform_configs
                refresh_platform_configs()
                add_log("已从服务器刷新平台配置")
            except Exception as e:
                add_log(f"刷新平台配置失败: {str(e)}")
            
            # 如果没有提供配置，使用全局配置
            if not automation_config:
                automation_config = {}
            
            # 更新配置数据
            if 'version' in automation_config:
                config_data['version'] = automation_config['version']
            if 'platform' in automation_config:
                config_data['platform'] = automation_config['platform']
            if 'phone' in automation_config:
                config_data['username'] = automation_config['phone']
            if 'jobName' in automation_config:
                config_data['selectedJob'] = automation_config['jobName']
            if 'delay' in automation_config:
                config_data['delay'] = automation_config['delay']
                add_log(f"设置候选人打开延迟时间: {automation_config['delay']['min']}-{automation_config['delay']['max']}秒")
                config_data['minDelay'] = automation_config['delay']['min']
                config_data['maxDelay'] = automation_config['delay']['max']
            
            # 加载声音设置
            sound_enabled = config_data.get("sound_enabled", True)
            
            # 确保声音文件目录存在
            sounds_dir = os.path.join("web", "sounds")
            if not os.path.exists(sounds_dir):
                os.makedirs(sounds_dir)
            
            # 检查版本
            current_version = config_data.get('version', 'free')
            version_data = load_version_data()
            
            # 检查免费版每日限额
            if current_version == 'free':
                today_count = version_data.get('todayCount', 0)
                free_quota = version_data.get('freeQuota', 100)
                
                # 检查今天的日期是否与最后重置日期相同
                today = datetime.now().date()
                last_reset_date_str = version_data.get('lastResetDate', '')
                
                try:
                    last_reset_date = datetime.strptime(last_reset_date_str, "%Y-%m-%d").date()
                    # 如果日期不同，重置计数
                    if last_reset_date != today:
                        version_data['todayCount'] = 0
                        version_data['greetCount'] = 0
                        version_data['remainingQuota'] = free_quota
                        version_data['lastResetDate'] = today.strftime("%Y-%m-%d")
                        today_count = 0
                        save_config()
                        add_log("检测到日期变更，已重置今日打招呼计数")
                except Exception as e:
                    add_log(f"解析日期失败: {str(e)}")
                
                if today_count >= free_quota:
                    add_log(f"今日免费版打招呼次数已达上限 ({free_quota} 次)，请明天再试或升级到捐赠版")
                    return False
            
            # 检查捐赠版到期日期
            if current_version == 'donation' and version_data.get('expiryDate'):
                try:
                    expiry_date = datetime.strptime(version_data['expiryDate'], "%Y-%m-%d").date()
                    today = datetime.now().date()
                    if expiry_date < today:
                        add_log("捐赠版已过期，请续费后再使用")
                        return False
                except:
                    pass
            
            # 检查企业版配额
            if current_version == 'enterprise':
                try:
                    # 获取企业版配额
                    remaining_quota = version_data.get('remainingQuota', 0)
                    if remaining_quota <= 0:
                        add_log("企业版配额已用完，请联系客服充值")
                        return False
                    add_log(f"企业版剩余配额: {remaining_quota} 次")
                except Exception as e:
                    add_log(f"检查企业版配额失败: {str(e)}")
            
            # 更新状态
            is_running = True
            eel.updateAutomationStatus(is_running)()
            
            add_log("正在启动自动化流程...")
            
            # 创建自动化对象，传入声音播放回调函数和更新打招呼计数回调函数
            automation = AutomationProcess(config_data, add_log_from_thread, play_sound, update_greet_count)
            
            # 设置达到打招呼数量限制的回调函数
            def on_max_greet_reached():
                add_log_from_thread("已达到设定的打招呼数量限制，正在停止自动化流程...")
                stop_automation()
            
            # 设置回调函数
            if hasattr(automation, 'handler') and automation.handler:
                automation.handler.on_max_greet_reached = on_max_greet_reached
            
            # 创建新的线程来运行自动化流程
            automation_thread = threading.Thread(target=run_automation)
            automation_thread.daemon = True  # 设置为守护线程
            automation_thread.start()
            
            return True
        except Exception as e:
            add_log(f"启动自动化流程失败: {str(e)}")
            
            # 恢复状态
            is_running = False
            eel.updateAutomationStatus(is_running)()
            
            return False
    else:
        add_log("自动化流程已在运行中")
        return False

# 停止自动化流程
@eel.expose
def stop_automation():
    global automation, is_running
    
    if is_running and automation:
        try:
            add_log("正在停止自动化流程...")
            
            # 设置停止标志
            automation.stop()
            
            # 更新状态
            is_running = False
            eel.updateAutomationStatus(is_running)()
            
            return True
        except Exception as e:
            add_log(f"停止自动化流程失败: {str(e)}")
            return False
    else:
        add_log("自动化流程未在运行中")
        return False

# 加载版本数据
def load_version_data():
    """从config_data中获取当前版本的数据"""
    global config_data
    
    # 获取当前版本
    current_version = config_data.get('version', 'free')
    
    # 如果版本不存在，默认使用免费版
    if current_version not in config_data.get("versions", {}):
        current_version = 'free'
        config_data['version'] = current_version
    
    # 获取版本数据
    version_data = config_data.get("versions", {}).get(current_version, {})
    
    # 如果版本数据为空，初始化默认值
    if not version_data:
        today = datetime.now().date()
        version_data = {
            "greetCount": 0,
            "remainingQuota": 100 if current_version == "free" else 0,
            "expiryDate": "永久有效" if current_version == "free" else 
                        (today + timedelta(days=30)).strftime("%Y-%m-%d") if current_version == "donation" else "余额不足",
            "lastResetDate": today.strftime("%Y-%m-%d"),
            "todayCount": 0,
            "freeQuota": 100
        }
        
        # 更新配置
        if "versions" not in config_data:
            config_data["versions"] = {}
        config_data["versions"][current_version] = version_data
        save_config()
    
    # 确保包含todayCount和freeQuota字段
    if "todayCount" not in version_data:
        version_data["todayCount"] = version_data.get("greetCount", 0)
    if "freeQuota" not in version_data:
        version_data["freeQuota"] = 100
    
    return version_data

# 在线程中运行自动化流程
def run_automation():
    global automation, is_running
    
    try:
        # 创建新的事件循环
        loop = asyncio.new_event_loop()
        asyncio.set_event_loop(loop)
        
        # 设置更详细的异常处理
        try:
            # 运行自动化流程并获取打招呼计数
            greet_count = loop.run_until_complete(automation.start())
            
            # 更新打招呼计数
            if greet_count > 0:
                add_log_from_thread(f"本次共打招呼 {greet_count} 次")
                update_greet_count(greet_count)
            else:
                add_log_from_thread("本次没有打招呼")
        except Exception as e:
            error_msg = str(e)
            add_log_from_thread(f"自动化流程出错: {error_msg}")
            
            # 针对特定错误提供更详细的信息
            if "signal only works in main thread" in error_msg:
                add_log_from_thread("这是由于在非主线程中使用信号处理导致的。")
                add_log_from_thread("尝试解决方案:")
                add_log_from_thread("1. 确保已禁用Pyppeteer的信号处理")
                add_log_from_thread("2. 考虑在主线程中运行浏览器操作")
            elif "browser has disconnected" in error_msg.lower():
                add_log_from_thread("浏览器已断开连接，可能是被用户手动关闭或崩溃")
            elif "target closed" in error_msg.lower():
                add_log_from_thread("目标页面已关闭，可能是被用户手动关闭")
    except Exception as e:
        add_log_from_thread(f"创建事件循环或设置事件循环时出错: {str(e)}")
    finally:
        # 清理资源
        try:
            if loop and loop.is_running():
                loop.stop()
            if loop:
                loop.close()
        except Exception as e:
            add_log_from_thread(f"清理事件循环资源时出错: {str(e)}")
        
        # 更新状态
        is_running = False
        eel.updateAutomationStatus(is_running)()
        
        add_log_from_thread("自动化流程已结束")
        
        # 只有在程序自动停止时才播放提示音
        # 检查是否是因为达到打招呼数量限制而停止
        if automation and hasattr(automation, 'handler') and automation.handler and automation.handler.greet_count > 0:
            max_greet_count = automation.handler.config_data.get("max_greet_count", 0)
            if max_greet_count > 0 and automation.handler.greet_count >= max_greet_count:
                # 播放完成音效
                eel.sleep(0.5)  # 等待一下，确保UI更新
                play_sound("63c0e6c0aa6ec422.mp3")  # 使用完成音效文件

# 获取最近的日志
@eel.expose
def get_recent_logs(max_lines=100):
    try:
        log_file = get_log_file_path()
        if os.path.exists(log_file):
            with open(log_file, "r", encoding="utf-8") as f:
                # 读取所有行并只保留最后max_lines行
                lines = f.readlines()
                recent_lines = lines[-max_lines:] if len(lines) > max_lines else lines
                
                # 返回日志行
                return recent_lines
        
        return []
    except Exception as e:
        print(f"加载日志失败: {str(e)}")
        return []

# 获取日志文件路径
def get_log_file_path():
    today = datetime.now().strftime("%Y-%m-%d")
    return os.path.join(log_dir, f"log_{today}.txt")

# 获取平台配置
@eel.expose
def get_platform_configs():
    """
    获取平台配置
    Returns:
        Dict: 平台配置信息
    """
    try:
        # 确保获取最新的平台配置
        platforms = refresh_platform_configs()
        
        # 构造返回给前端的平台数据
        platform_data = {
            "platforms": list(platforms.keys()) if platforms else ["BOSS直聘", "智联招聘", "前程无忧", "猎聘"]
        }


        # 检查是否存在api键，并将其移除
        platform_data.pop("api")
        
        add_log(f"获取平台配置成功")
        return platform_data
    except Exception as e:
        add_log(f"获取平台配置出错: {str(e)}")
        # 返回默认平台列表
        return {
            "platforms": ["BOSS直聘", "智联招聘", "前程无忧", "猎聘"]
        }

# 查找Chrome浏览器路径
def find_chrome_browser():
    """
    查找系统中的Chrome浏览器路径
    Returns:
        str: Chrome浏览器路径，如果找不到则返回None
    """
    # 常见的Chrome浏览器路径
    chrome_paths = [
        # Windows路径
        r"C:\Program Files\Google\Chrome\Application\chrome.exe",
        r"C:\Program Files (x86)\Google\Chrome\Application\chrome.exe",
        os.path.join(os.path.expanduser("~"), r"AppData\Local\Google\Chrome\Application\chrome.exe"),
        # 可能的其他安装路径
        os.path.join(os.environ.get('LOCALAPPDATA', ''), r"Google\Chrome\Application\chrome.exe"),
        os.path.join(os.environ.get('PROGRAMFILES', ''), r"Google\Chrome\Application\chrome.exe"),
        os.path.join(os.environ.get('PROGRAMFILES(X86)', ''), r"Google\Chrome\Application\chrome.exe"),
    ]
    
    # 检查路径是否存在
    for path in chrome_paths:
        if os.path.exists(path):
            add_log(f"找到Chrome浏览器: {path}")
            return path
    
    # 如果没有找到，尝试通过where命令查找（仅限Windows）
    if platform.system() == "Windows":
        try:
            result = subprocess.run(["where", "chrome"], capture_output=True, text=True, check=False)
            if result.returncode == 0:
                chrome_path = result.stdout.strip().split('\n')[0]
                if os.path.exists(chrome_path):
                    add_log(f"通过where命令找到Chrome: {chrome_path}")
                    return chrome_path
        except Exception as e:
            add_log(f"通过where命令查找Chrome失败: {str(e)}")
    
    add_log("未找到Chrome浏览器，程序可能无法正常启动")
    return None

# 主函数
def main():
    """应用程序主入口"""
    try:
        # 刷新平台配置
        try:
            from platform_configs import refresh_platform_configs
            refresh_platform_configs()
            add_log("已从服务器刷新平台配置")
        except Exception as e:
            add_log(f"刷新平台配置失败: {str(e)}")

        # 初始化eel
        add_log("正在初始化界面...")
        eel.init('web')
        
        # 获取配置（使用ConfigManager）
        if config_manager:
            config = config_manager.load_config()
            add_log(f"已加载配置: {config.get('version')} 版本")
            
            # 检查是否需要从服务器同步配置
            phone = config.get("username", "")
            if phone and len(phone) == 11:
                add_log(f"检测到已保存的手机号 {phone}，尝试同步配置...")
                server_config = config_manager.fetch_config_from_server(phone)
                if server_config and config_manager.should_use_server_config(server_config):
                    add_log("检测到服务器有更新的配置，正在同步...")
                    config_manager.merge_server_config(server_config)
                    add_log("配置同步完成")
                else:
                    add_log("未检测到服务器有更新的配置，使用本地配置")
        else:
            add_log("ConfigManager初始化失败，使用本地配置")
            load_config()  # 兼容旧版本
        
        # 启动web界面
        add_log("正在启动界面...")
        
    # 启动 Eel
        try:
        # 设置窗口标题和图标
            web_app_options = {
            'mode': 'chrome',
            'port': 0,
            'window_title': f'GoodHR 自动化工具 v{CURRENT_VERSION} - 联系方式: 17607080935',
            'icon_path': os.path.join('web', 'sounds', 'logo.png')
            }
            eel.start('index.html', size=(1000, 800), **web_app_options)
        except Exception as e:
            print(f"启动 Eel 失败: {str(e)}")
    except Exception as e:
        print(f"主函数异常: {str(e)}")
        traceback.print_exc()
        sys.exit(1)

if __name__ == "__main__":
    main() 