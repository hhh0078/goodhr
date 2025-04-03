import customtkinter as ctk
import json
import os
from PIL import Image, ImageDraw, ImageFont
from datetime import datetime
import requests
from CTkMessagebox import CTkMessagebox  # 添加导入
from automation import AutomationProcess  # 导入自动化流程类
import asyncio
from tkinter import filedialog
import tkinter as tk
import tkinter.ttk as ttk
import tkinter.messagebox as messagebox
import threading


import keyboard



class HRAutomationUI(ctk.CTk):
    def __init__(self):
        super().__init__()
        
        # 设置主题
        ctk.set_appearance_mode("light")
        ctk.set_default_color_theme("blue")
        
        self.title("GoodHR 自动化工具")
        self.geometry("400x800")  # 增加窗口宽度
        
        # 初始化automation属性
        self.automation = None
        
        # 创建日志目录
        self.log_dir = "运行日志logs"
        if not os.path.exists(self.log_dir):
            os.makedirs(self.log_dir)
        
        # 创建主容器
        self.grid_columnconfigure(0, weight=1)
        self.grid_rowconfigure(0, weight=1)
        
        # 创建主框架
        self.main_frame = ctk.CTkFrame(self)
        self.main_frame.grid(row=0, column=0, padx=0, pady=0, sticky="nsew")
        self.main_frame.grid_columnconfigure(0, weight=1)
        
        # 标题
        self.title_label = ctk.CTkLabel(
            self.main_frame, 
            text="GoodHR 自动化工具", 
            font=ctk.CTkFont(size=24, weight="bold")
        )
        self.title_label.grid(row=0, column=0, padx=20, pady=(20, 10))
        
        # 初始化配置变量
        self.platform_var = ctk.StringVar(value="前程无忧")

        # 创建日志区域（移到前面）
        self.create_log_frame()
        
        # 记录程序启动日志
        self.add_log("程序启动")
        
        # 先加载配置
        self.load_config()

        # 创建岗位列表区域
        self.create_job_list_frame()
        
        # 创建设置区域
        self.create_settings_frame()
        
        # 创建控制按钮
        self.create_control_buttons()

        # 绑定窗口关闭事件
        self.protocol("WM_DELETE_WINDOW", self.on_closing)

    def create_job_list_frame(self):
        # 岗位列表框架
        self.job_frame = ctk.CTkFrame(self.main_frame)
        self.job_frame.grid(row=2, column=0, padx=20, pady=10, sticky="nsew")
        self.main_frame.grid_rowconfigure(2, weight=1)
        self.job_frame.grid_columnconfigure(0, weight=1)
        self.job_frame.grid_rowconfigure(1, weight=1)
        
        # 岗位列表标题
        ctk.CTkLabel(self.job_frame, text="岗位列表", font=ctk.CTkFont(weight="bold")).grid(
            row=0, column=0, padx=10, pady=5, sticky="w"
        )
        
        # 岗位列表滚动区域
        self.job_scrollable_frame = ctk.CTkScrollableFrame(self.job_frame)
        self.job_scrollable_frame.grid(row=1, column=0, padx=10, pady=5, sticky="nsew")
        self.job_scrollable_frame.grid_columnconfigure(0, weight=1)

    def create_settings_frame(self):
        # 设置框架
        self.settings_frame = ctk.CTkFrame(self.main_frame)
        self.settings_frame.grid(row=1, column=0, padx=20, pady=10, sticky="ew")
        self.settings_frame.grid_columnconfigure(1, weight=1)
        
        # 平台选择
        platform_label = ctk.CTkLabel(self.settings_frame, text="招聘平台:", font=ctk.CTkFont(weight="bold"))
        platform_label.grid(row=0, column=0, columnspan=2, padx=10, pady=(10, 5), sticky="w")
        
        # 创建单选框框架
        self.radio_frame = ctk.CTkFrame(self.settings_frame)
        self.radio_frame.grid(row=1, column=0, columnspan=2, padx=10, pady=(0, 10), sticky="ew")
        self.radio_frame.grid_columnconfigure((0, 1), weight=1)  # 两列等宽
        
        # 创建单选按钮
        platforms = ["BOSS直聘", "智联招聘", "前程无忧", "猎聘"]
        for i, platform in enumerate(platforms):
            row = i // 2  # 计算行号
            col = i % 2   # 计算列号
            radio = ctk.CTkRadioButton(
                self.radio_frame,
                text=platform,
                variable=self.platform_var,
                value=platform,
                font=ctk.CTkFont(size=12),
                command=self.on_config_change
            )
            radio.grid(row=row, column=col, padx=10, pady=10, sticky="w")
        
        # 用户名（手机号）
        ctk.CTkLabel(self.settings_frame, text="手机号:").grid(row=2, column=0, padx=10, pady=10)
        self.username_entry = ctk.CTkEntry(
            self.settings_frame, 
            placeholder_text="请输入手机号",
            height=35,
            font=ctk.CTkFont(size=12)
        )
        self.username_entry.grid(row=2, column=1, padx=10, pady=(10, 2), sticky="ew")
        self.username_entry.bind('<KeyRelease>', self.on_phone_change)
        
        # 手机号提示文字
        phone_hint = ctk.CTkLabel(
            self.settings_frame,
            text="* 请使用GoodHR注册的手机号",
            font=ctk.CTkFont(size=12),
            text_color="gray"
        )
        phone_hint.grid(row=3, column=1, padx=5, pady=(0, 5), sticky="w")

        # 浏览器路径选择框架
        browser_frame = ctk.CTkFrame(self.settings_frame)
        browser_frame.grid(row=4, column=0, columnspan=2, padx=10, pady=(10, 5), sticky="ew")
        browser_frame.grid_columnconfigure(1, weight=1)

        # 浏览器路径标签
        ctk.CTkLabel(browser_frame, text="浏览器路径:", font=ctk.CTkFont(weight="bold")).grid(
            row=0, column=0, padx=5, pady=5, sticky="w"
        )

        # 浏览器路径显示
        self.browser_path_label = ctk.CTkLabel(
            browser_frame,
            text=self.config_data.get("browser_path", "未选择"),
            font=ctk.CTkFont(size=12)
        )
        self.browser_path_label.grid(row=0, column=1, padx=5, pady=5, sticky="ew")

        # 选择浏览器目录按钮
        self.select_directory_button = ctk.CTkButton(
            browser_frame,
            text="选择",
            command=self.select_browser_directory,
            font=ctk.CTkFont(size=12),
            width=60
        )
        self.select_directory_button.grid(row=0, column=2, padx=5, pady=5)

        # 填充已保存的配置
        if hasattr(self, 'config_data'):
            self.username_entry.insert(0, self.config_data["username"])
            # 如果加载的手机号是11位，立即获取岗位列表
            phone = self.config_data["username"].strip()
            if len(phone) == 11:
                self.fetch_job_list(phone)

    def create_log_frame(self):
        # 日志框架
        self.log_frame = ctk.CTkFrame(self.main_frame)
        self.log_frame.grid(row=3, column=0, padx=20, pady=10, sticky="nsew")
        self.main_frame.grid_rowconfigure(3, weight=1)
        self.log_frame.grid_columnconfigure(0, weight=1)
        self.log_frame.grid_rowconfigure(1, weight=1)
        
        # 日志标题
        ctk.CTkLabel(self.log_frame, text="运行日志", font=ctk.CTkFont(weight="bold")).grid(
            row=0, column=0, padx=10, pady=5
        )
        
        # 日志文本区域
        self.log_text = ctk.CTkTextbox(
            self.log_frame,
            text_color="#00FF00",  # 亮绿色
            font=ctk.CTkFont(size=12),
            bg_color="#fff",  # 纯黑色
            fg_color="#000000"  # 白色前景色
        )
        self.log_text.grid(row=1, column=0, padx=10, pady=10, sticky="nsew")
        
        # 加载最近的日志
        self.load_recent_logs()

    def create_control_buttons(self):
        # 控制按钮框架
        self.control_frame = ctk.CTkFrame(self.main_frame)
        self.control_frame.grid(row=4, column=0, padx=20, pady=10, sticky="ew")
        self.control_frame.grid_columnconfigure((0, 1), weight=1)
        
        # 开始按钮
        self.start_button = ctk.CTkButton(
            self.control_frame,
            text="开始运行",
            command=self.start_automation,
            font=ctk.CTkFont(size=14),
            state="normal"  # 初始状态为可用
        )
        self.start_button.grid(row=0, column=0, padx=10, pady=10, sticky="ew")
        
        # 停止按钮
        self.stop_button = ctk.CTkButton(
            self.control_frame,
            text="停止",
            command=self.stop_automation,
            state="disabled",  # 初始状态为禁用
            font=ctk.CTkFont(size=14),
            fg_color="#FF5555",  # 红色背景
            hover_color="#CC3333"  # 深红色悬停效果
        )
        self.stop_button.grid(row=0, column=1, padx=10, pady=10, sticky="ew")

    def on_phone_change(self, event):
        """当手机号输入变化时的处理"""
        phone = self.username_entry.get().strip()
        if len(phone) == 11:  # 当输入11位手机号时
            self.fetch_job_list(phone)
        # 无论长度是否为11位，都保存当前配置
        self.save_config_item("username", phone, "手机号")

    def fetch_job_list(self, phone):
        """获取岗位列表"""
        try:
            response = requests.get(f"http://127.0.0.1:7000/opsli-boot/api/v1/company/positions/getPositionsListByPhone?phone={phone}")
            data = response.json()
            
            if data.get("code") == 0:  # 成功状态码为0
                self.update_job_list(data.get("data", []))
                self.add_log(f"岗位列表获取成功: {data.get('msg', '')}")
            else:
                error_msg = data.get("msg", "未知错误")
                self.show_error_dialog("获取岗位列表失败", error_msg)
                self.add_log(f"获取岗位列表失败: {error_msg}")
        except Exception as e:
            self.show_error_dialog("获取岗位列表出错", str(e))
            self.add_log(f"获取岗位列表出错: {str(e)}")

    def update_job_list(self, jobs):
        """更新岗位列表显示"""
        # 清除现有的岗位列表
        for widget in self.job_scrollable_frame.winfo_children():
            widget.destroy()
        
        # 添加新的岗位列表
        self.selected_job_var = ctk.StringVar(value=self.config_data.get("selected_job_id", ""))  # 使用已保存的岗位ID
        
        # 如果有岗位，保存第一个岗位的createBy作为userID
        if jobs and len(jobs) > 0:
            self.save_config_item("userID", jobs[0].get('createBy', ''), "用户ID")
            
        for job in jobs:
            # 创建单选按钮作为岗位选项
            radio = ctk.CTkRadioButton(
                self.job_scrollable_frame,
                text=job['name'],
                variable=self.selected_job_var,
                value=job['id'],
                font=ctk.CTkFont(size=14),
                command=lambda: self.on_job_selected()
            )
            radio.grid(sticky="w", padx=10, pady=5)

    def on_job_selected(self):
        """当选择岗位时的处理"""
        selected_job_id = self.selected_job_var.get()
        if selected_job_id:
            self.save_config_item("selected_job_id", selected_job_id, "选中岗位ID")

    def load_config(self):
        """加载配置文件"""
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
        
        self.config_data = {
            "platform": "",
            "username": "",
            "selected_job_id": "",
            "userID": "",
            "browser_path": default_browser_path
        }
        
        if os.path.exists("config.json"):
            try:
                with open("config.json", "r", encoding="utf-8") as f:
                    loaded_config = json.load(f)
                    self.config_data.update(loaded_config)
                    self.platform_var.set(self.config_data["platform"])
            except Exception as e:
                pass

    def on_config_change(self, *args):
        """当配置发生变化时自动保存"""
        self.save_config_item("platform", self.platform_var.get(), "招聘平台")

    def add_log(self, message):

        """添加日志到文件和显示区域"""
        try:
            time = datetime.now().strftime("%H:%M:%S")
            log_message = f"[{time}] {message}\n"
            
            # 添加到显示区域
            self.log_text.insert("end", log_message)
            self.log_text.see("end")
            
            # 添加到文件
            log_file = self.get_log_file_path()
            with open(log_file, "a", encoding="utf-8") as f:
                f.write(log_message)
        except Exception as e:
            print(f"写入日志失败: {str(e)}")



    def on_key_press(self, event):
        """
        键盘按键回调函数
        :param event: 键盘事件对象
        """
        if event.name == 'esc':
            # 确保在主线程中处理
            self.after(0, lambda: self.handle_esc_key())
            
    def handle_esc_key(self):
        """处理ESC键按下事件"""
        self.add_log("检测到ESC键被按下，正在停止自动化流程...")
        self.stop_automation()

    def start_automation(self):
        """开始自动化流程"""
        if not self.automation:
            # 禁用开始按钮，启用停止按钮
            self.start_button.configure(state="disabled")
            self.stop_button.configure(state="normal")
            
            self.add_log("正在启动自动化流程...")
            
            # 在主线程中设置键盘监听
            self.add_log("开始监听键盘事件，按ESC键可停止程序")
            keyboard.on_press(self.on_key_press)
            
            # 创建自动化对象
            self.automation = AutomationProcess(self.config_data, self.add_log)
            
            # 创建新的线程来运行自动化流程
            automation_thread = threading.Thread(target=self.run_automation)
            automation_thread.daemon = True  # 设置为守护线程
            automation_thread.start()

    def run_automation(self):
        """运行自动化流程"""
        try:
            # 在Windows上使用ProactorEventLoop
            if os.name == 'nt':
                loop = asyncio.new_event_loop()
            else:
                loop = asyncio.new_event_loop()
            
            asyncio.set_event_loop(loop)
            
            # 使用run_until_complete来运行异步函数
            loop.run_until_complete(self.automation.start())
            
        except Exception as e:
            # 使用after方法在主线程中更新UI
            self.after(0, lambda: self.add_log(f"自动化流程出错: {str(e)}"))
        finally:
            # 使用after方法在主线程中更新UI
            self.after(0, lambda: self.update_ui_after_automation())
            if loop.is_running():
                loop.stop()
            loop.close()
    
    def update_ui_after_automation(self):
        """在主线程中更新UI状态"""
        # 恢复按钮状态
        self.start_button.configure(state="normal")
        self.stop_button.configure(state="disabled")
        self.add_log("自动化流程已结束")

    def stop_automation(self):
        """停止自动化流程"""
        if self.automation:
            self.add_log("正在停止自动化流程...")
            
            # 创建新线程来停止自动化流程
            stop_thread = threading.Thread(target=self.run_stop_automation)
            stop_thread.daemon = True
            stop_thread.start()
        else:
            # 如果没有运行中的自动化流程，直接恢复按钮状态
            self.start_button.configure(state="normal")
            self.stop_button.configure(state="disabled")
    
    def run_stop_automation(self):
        """在子线程中运行停止自动化流程的操作"""
        try:
            # 创建新的事件循环
            loop = asyncio.new_event_loop()
            asyncio.set_event_loop(loop)
            
            # 停止自动化流程
            loop.run_until_complete(self.automation.stop())
            
            # 在主线程中更新UI
            self.after(0, lambda: self.add_log("自动化流程已停止"))
        except Exception as e:
            # 在主线程中更新UI
            self.after(0, lambda: self.add_log(f"停止自动化流程出错: {str(e)}"))
        finally:
            # 清理资源
            if loop.is_running():
                loop.stop()
            loop.close()
            self.automation = None
            
            # 在主线程中恢复按钮状态
            self.after(0, lambda: self.update_ui_after_stop())

    def update_ui_after_stop(self):
        """在主线程中更新UI状态"""
        # 恢复按钮状态
        self.start_button.configure(state="normal")
        self.stop_button.configure(state="disabled")
        self.add_log("自动化流程已停止")

    def show_error_dialog(self, title, message):
        """显示错误弹框"""
        CTkMessagebox(
            title=title,
            message=message,
            icon="cancel",  # 显示错误图标
            option_1="确定",
            button_color="red",
            button_hover_color="#8B0000"  # 深红色
        )

    def save_config_item(self, key, value, display_name=None):
        """保存单个配置项
        Args:
            key: 配置项的键名
            value: 配置项的值
            display_name: 配置项的中文显示名称，如果为None则使用key
        """
        try:
            # 先读取现有配置
            if os.path.exists("config.json"):
                with open("config.json", "r", encoding="utf-8") as f:
                    config = json.load(f)
            else:
                config = {}

            # 使用display_name或key作为日志显示名称
            show_name = display_name if display_name else key
            self.add_log(f"设置{show_name}: {value}")
            
            # 更新指定的配置项
            config[key] = value
            
            # 保存回文件
            with open("config.json", "w", encoding="utf-8") as f:
                json.dump(config, f, ensure_ascii=False, indent=4)
            
            # 同时更新内存中的配置
            self.config_data[key] = value
            
        except Exception as e:
            self.add_log(f"保存配置项 {key} 失败: {str(e)}")

    def get_config_item(self, key, default=""):
        """获取单个配置项"""
        return self.config_data.get(key, default)

    def get_log_file_path(self):
        """获取当天的日志文件路径"""
        today = datetime.now().strftime("%Y-%m-%d")
        return os.path.join(self.log_dir, f"log_{today}.txt")

    def load_recent_logs(self, max_lines=100):
        """加载最近的日志"""
        try:
            log_file = self.get_log_file_path()
            if os.path.exists(log_file):
                with open(log_file, "r", encoding="utf-8") as f:
                    # 读取所有行并只保留最后max_lines行
                    lines = f.readlines()
                    recent_lines = lines[-max_lines:] if len(lines) > max_lines else lines
                    
                    # 将日志添加到显示区域
                    for line in recent_lines:
                        self.log_text.insert("end", line)
                    self.log_text.see("end")
        except Exception as e:
            print(f"加载日志失败: {str(e)}")

    def on_closing(self):
        """窗口关闭时的处理"""
        if self.automation and self.automation.is_running:
            # 如果自动化正在运行，询问用户是否确认停止
            confirm = CTkMessagebox(
                title="确认退出",
                message="自动化程序正在运行中，是否确认停止并退出？",
                icon="warning",
                option_1="确定",
                option_2="取消",
                button_color=["#FF0000", "#808080"]  # 红色确定按钮，灰色取消按钮
            )
            
            if confirm.get() == "确定":
                self.add_log("正在停止自动化流程并退出程序...")
                
                # 创建新线程来停止自动化流程
                def stop_and_quit():
                    try:
                        # 创建新的事件循环
                        loop = asyncio.new_event_loop()
                        asyncio.set_event_loop(loop)
                        
                        # 停止自动化流程
                        loop.run_until_complete(self.automation.stop())
                        
                        # 在主线程中退出程序
                        self.after(0, lambda: self.final_quit("程序已停止运行并退出"))
                    except Exception as e:
                        # 在主线程中更新UI并退出
                        self.after(0, lambda: self.final_quit(f"停止自动化流程出错: {str(e)}"))
                    finally:
                        if loop and loop.is_running():
                            loop.stop()
                        if loop:
                            loop.close()
                
                # 启动线程
                stop_thread = threading.Thread(target=stop_and_quit)
                stop_thread.daemon = True
                stop_thread.start()
            # 如果用户取消，不做任何操作
        else:
            # 如果自动化没有运行，直接关闭
            self.final_quit("程序关闭")
    
    def final_quit(self, message):
        """最终退出程序"""
        self.add_log(message)
        self.quit()

    def on_space_pressed(self):
        """空格键按下时的处理"""
        if self.automation and self.automation.is_running:
            self.automation.stop()
            self.add_log("程序已停止运行")
        else:
            self.add_log("程序未运行，无需停止")

    def select_browser_directory(self):
        """选择浏览器安装目录"""
        directory_path = filedialog.askdirectory(
            title="选择Chrome浏览器安装目录",
            initialdir=os.path.dirname(self.get_config_item("browser_path"))
        )
        if directory_path:
            # 先尝试查找chrome.exe
            chrome_path = os.path.join(directory_path, "chrome.exe")
            if not os.path.exists(chrome_path):
                # 如果没找到chrome.exe，再查找chromee.exe
                chrome_path = os.path.join(directory_path, "chromee.exe")
            
            if os.path.exists(chrome_path):
                self.save_config_item("browser_path", chrome_path, "浏览器路径")
                # 更新路径显示
                self.browser_path_label.configure(text=chrome_path)
                self.add_log(f"已更新浏览器路径: {chrome_path}")
            else:
                self.show_error_dialog("错误", "所选目录下未找到chrome.exe或chromee.exe，请选择正确的Chrome安装目录。")
                self.add_log("选择的目录中未找到浏览器可执行文件")

if __name__ == "__main__":
    app = HRAutomationUI()
    app.mainloop()