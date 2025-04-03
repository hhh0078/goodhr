from flask import Flask, request, jsonify
from flask_cors import CORS
import time
import json
import logging
import pyautogui
import sys
import requests
from PyQt5.QtWidgets import (QApplication, QWidget, QLabel, QVBoxLayout, 
                            QPushButton, QProgressBar, QMessageBox, QTextEdit, QHBoxLayout)
from PyQt5.QtCore import Qt, QTimer, QPoint, QObject, pyqtSignal
from PyQt5.QtGui import QIcon, QPainter, QColor, QPen
import socket
import psutil
import os
import threading

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)

# 定义当前版本号和服务器地址
CURRENT_VERSION = "2.0.0"
VERSION_CHECK_URL = "https://goodhr.58it.cn/pyv.json"
OFFICIAL_WEBSITE = "https://www.goodhr.com"

def check_version():
    """检查版本更新"""
    try:
        print("正在检查版本更新...")
        response = requests.get(VERSION_CHECK_URL)
        data = response.json()
        
        # 检查公告
        if data.get("gonggao"):
            QMessageBox.information(None, "系统公告", data["gonggao"])
        
        # 检查版本
        server_version = data.get("version")
        if server_version and server_version != CURRENT_VERSION:
            release_notes = data.get("releaseNotes", "")
            force_update = "必须更新" in release_notes
            
            msg = QMessageBox()
            msg.setIcon(QMessageBox.Information)
            msg.setWindowTitle("发现新版本")
            msg.setText(f"当前版本: {CURRENT_VERSION}\n发现新版本: {server_version}\n\n更新内容:\n{release_notes}")
            
            if force_update:
                msg.setStandardButtons(QMessageBox.Ok)
                msg.buttonClicked.connect(lambda: open_website_and_exit())
                msg.setText(msg.text() + "\n\n该版本为强制更新，请点击确定前往官网下载新版本。")
            else:
                msg.setStandardButtons(QMessageBox.Ok | QMessageBox.Cancel)
                msg.buttonClicked.connect(lambda btn: open_website() if btn.text() == "OK" else None)
                msg.setText(msg.text() + "\n\n点击确定前往官网下载新版本，点击取消继续使用当前版本。")
            
            msg.exec_()
            
    except Exception as e:
        print(f"检查版本时出错: {str(e)}")

def open_website():
    """打开官网"""
    import webbrowser
    webbrowser.open(OFFICIAL_WEBSITE)

def open_website_and_exit():
    """打开官网并退出程序"""
    open_website()
    QApplication.quit()

def kill_process_on_port(port):
    """结束占用指定端口的进程"""
    try:
        for proc in psutil.process_iter(['pid', 'name', 'connections']):
            try:
                connections = proc.connections()
                for conn in connections:
                    if conn.laddr.port == port:
                        os.kill(proc.pid, 9)
                        print(f"已结束占用端口 {port} 的进程: {proc.name()} (PID: {proc.pid})")
                        return True
            except (psutil.NoSuchProcess, psutil.AccessDenied):
                continue
    except Exception as e:
        print(f"结束进程时出错: {str(e)}")
    return False

def find_available_port(start_port=5000, max_attempts=10):
    """查找可用的端口"""
    for port in range(start_port, start_port + max_attempts):
        try:
            # 尝试结束占用端口的进程
            kill_process_on_port(port)
            
            # 测试端口是否可用
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.bind(('', port))
            sock.close()
            return port
        except OSError:
            continue
    raise RuntimeError(f"无法找到可用端口 ({start_port}-{start_port + max_attempts})")

class MouseIndicator(QWidget):
    """鼠标指示器"""
    def __init__(self):
        super().__init__()
        self.setWindowFlags(Qt.FramelessWindowHint | Qt.WindowStaysOnTopHint | Qt.Tool)
        self.setAttribute(Qt.WA_TranslucentBackground)
        self.resize(30, 30)
        self.hide()
        
        # 添加定时器用于跟踪鼠标位置
        self.track_timer = QTimer()
        self.track_timer.timeout.connect(self.track_mouse)
        self.track_timer.setInterval(10)  # 10ms更新一次位置
        
    def track_mouse(self):
        """跟踪鼠标位置"""
        if self.isVisible():
            pos = pyautogui.position()
            self.move(pos.x - 15, pos.y - 15)
            
    def start_tracking(self):
        """开始跟踪"""
        self.show()
        self.track_timer.start()
        
    def stop_tracking(self):
        """停止跟踪"""
        self.track_timer.stop()
        self.hide()

    def paintEvent(self, event):
        painter = QPainter(self)
        painter.setRenderHint(QPainter.Antialiasing)
        
        # 设置画笔
        pen = QPen(QColor(255, 0, 0))  # 红色
        pen.setWidth(2)
        painter.setPen(pen)
        
        # 绘制十字准线
        center = QPoint(15, 15)
        size = 12
        
        # 水平线
        painter.drawLine(center.x() - size, center.y(), center.x() + size, center.y())
        # 垂直线
        painter.drawLine(center.x(), center.y() - size, center.x(), center.y() + size)
        
        # 绘制圆圈
        painter.drawEllipse(center, size, size)

class MouseController(QObject):
    """鼠标控制器"""
    move_signal = pyqtSignal(float, float, float, bool)  # x, y, duration, click
    scroll_signal = pyqtSignal(float, float, int, float)  # x, y, scroll_amount, duration
    operation_done = pyqtSignal()  # 添加完成信号

    def __init__(self, parent=None):
        super().__init__(parent)
        self.move_signal.connect(self._do_move)
        self.scroll_signal.connect(self._do_scroll)
        self.indicator = None
        self.operation_completed = threading.Event()  # 添加事件标志

    def _do_move(self, x, y, duration, click):
        """执行鼠标移动"""
        try:
            if self.indicator:
                self.indicator.start_tracking()
            
            pyautogui.moveTo(x, y, duration=duration)
            if click:
                pyautogui.click()
                
            if self.indicator:
                self.indicator.stop_tracking()
        except Exception as e:
            print(f"鼠标移动出错: {str(e)}")
            if self.indicator:
                self.indicator.stop_tracking()
        finally:
            self.operation_completed.set()  # 设置完成标志

    def _do_scroll(self, x, y, scroll_amount, duration):
        """执行滚动"""
        try:
            if self.indicator:
                self.indicator.start_tracking()
            
            pyautogui.moveTo(x, y, duration=duration/2)
            
            if scroll_amount != 0:
                steps = min(10, abs(scroll_amount))
                delay = (duration/2) / steps
                scroll_per_step = scroll_amount // steps
                
                for _ in range(steps):
                    current_pos = pyautogui.position()
                    if current_pos.x != x or current_pos.y != y:
                        pyautogui.moveTo(x, y)
                    pyautogui.scroll(scroll_per_step)
                    time.sleep(delay)
                
                remainder = scroll_amount % steps
                if remainder:
                    current_pos = pyautogui.position()
                    if current_pos.x != x or current_pos.y != y:
                        pyautogui.moveTo(x, y)
                    pyautogui.scroll(remainder)
            
            if self.indicator:
                self.indicator.stop_tracking()
        except Exception as e:
            print(f"滚动出错: {str(e)}")
            if self.indicator:
                self.indicator.stop_tracking()
        finally:
            self.operation_completed.set()  # 设置完成标志

class PermissionGuide(QWidget):
    """权限设置引导界面"""
    def __init__(self):
        super().__init__()
        self.setWindowTitle(f'GoodHR鼠标控制程序 v{CURRENT_VERSION}')
        self.setFixedSize(400, 450)
        
        # 创建主布局
        self.main_layout = QVBoxLayout(self)
        self.main_layout.setSpacing(8)
        self.main_layout.setContentsMargins(10, 10, 10, 10)
        
        # 初始化变量
        self.current_check_index = 0
        self.check_count = 0
        self.max_checks = 100
        self.check_timer = QTimer()
        self.check_timer.timeout.connect(self.check_current_permission)
        self.check_timer.setInterval(300)
        
        # 初始化列表
        self.permission_groups = []
        self.step_labels = []
        self.step_widgets = []
        
        # 修改权限列表
        self.permissions = [
            ("1. 辅助功能权限", "用于控制鼠标移动和点击", True, self.check_mouse_control),
            ("2. 输入监控权限", "用于检测空格键以便随时停止操作", True, self.check_keyboard_control),
            ("3. 屏幕录制权限", "用于获取屏幕坐标信息", False, self.check_screen_capture)
        ]
        
        # 初始化UI
        self.initUI()
        
        # 延迟2秒检查版本
        QTimer.singleShot(2000, check_version)
        
        # 添加关闭事件处理
        self.closeEvent = self.handle_close
        
        self.mouse_indicator = MouseIndicator()
        self.mouse_controller = MouseController()
        self.mouse_controller.indicator = self.mouse_indicator
        
    def initUI(self):
        """初始化用户界面"""
        layout = self.main_layout
        
        # 1. 添加标题
        title = QLabel("需要以下权限才能正常运行 goodhr.58it.cn 作者:瓜瓜:")
        title.setStyleSheet("font-size: 14px; font-weight: bold;")
        layout.addWidget(title)
        
        # 2. 添加状态提示标签
        self.status_label = QLabel("正在等待检查权限...")
        self.status_label.setStyleSheet("color: #e74c3c; font-weight: bold;")
        layout.addWidget(self.status_label)
        
        # 3. 创建步骤条
        steps_layout = QVBoxLayout()
        steps_layout.setSpacing(3)
        steps_row = QHBoxLayout()
        steps_row.setSpacing(1)
        
        # 初始化权限标签列表
        self.permission_labels = []
        for permission, _, required, _ in self.permissions:
            status_label = QLabel("⚪ 等待检查")
            status_label.setStyleSheet("color: #7f8c8d;")
            self.permission_labels.append(status_label)
        
        # 创建每个步骤的图标和标签
        for i, (permission, reason, required, _) in enumerate(self.permissions, 1):
            step_widget = QWidget()
            step_layout = QVBoxLayout(step_widget)
            step_layout.setSpacing(2)
            
            circle_label = QLabel(str(i))
            circle_label.setFixedSize(24, 24)
            circle_label.setStyleSheet("""
                QLabel {
                    background-color: #f5f5f5;
                    color: #666666;
                    border: 1px solid #d9d9d9;
                    border-radius: 12px;
                    font-weight: bold;
                }
            """)
            circle_label.setAlignment(Qt.AlignCenter)
            step_layout.addWidget(circle_label, alignment=Qt.AlignCenter)
            self.step_labels.append(circle_label)
            
            name = permission.split('. ')[1]
            name_label = QLabel(name)
            name_label.setStyleSheet("color: #666666;")
            name_label.setFixedWidth(80)
            step_layout.addWidget(name_label, alignment=Qt.AlignCenter)
            
            steps_row.addWidget(step_widget)
            self.step_widgets.append(step_widget)
            
            if i < len(self.permissions):
                line = QLabel("–")
                line.setStyleSheet("color: #d9d9d9;")
                steps_row.addWidget(line)
        
        steps_layout.addLayout(steps_row)
        layout.addLayout(steps_layout)
        
        # 4. 添加日志框
        self.log_box = QTextEdit()
        self.log_box.setReadOnly(True)
        self.log_box.setStyleSheet("""
            QTextEdit {
                background-color: #1e1e1e;
                color: #00ff00;
                font-family: Consolas;
            }
        """)
        layout.addWidget(self.log_box)
        
        # 5. 添加空格键提示
        self.space_tip = QLabel("按空格键可随时停止鼠标操作")
        self.space_tip.setStyleSheet("""
            QLabel {
                color: red;
                background-color: #fff1f0;
                border: 1px solid #ffccc7;
                padding: 8px;
                border-radius: 4px;
            }
        """)
        layout.addWidget(self.space_tip)
        
        # 6. 添加重试按钮
        self.retry_button = QPushButton("重新检查权限")
        self.retry_button.clicked.connect(self.start_permission_check)
        self.retry_button.hide()
        layout.addWidget(self.retry_button)

    def check_mouse_control(self):
        """检查鼠标控制权限"""
        current_pos = pyautogui.position()
        pyautogui.moveTo(current_pos.x + 1, current_pos.y + 1)
        pyautogui.moveTo(current_pos.x, current_pos.y)
    
    def check_keyboard_control(self):
        """检查键盘监控权限"""
        import keyboard
        keyboard.is_pressed('space')
    
    def check_screen_capture(self):
        """检查屏幕截图权限"""
        pyautogui.screenshot()
    
    def log_message(self, message):
        """添加日志消息"""
        current_time = time.strftime("%H:%M:%S", time.localtime())
        self.log_box.append(f"[{current_time}] {message}")
        self.log_box.verticalScrollBar().setValue(
            self.log_box.verticalScrollBar().maximum()
        )

    def start_permission_check(self):
        """开始权限检查流程"""
        try:
            self.current_check_index = 0
            self.check_count = 0
            self.status_label.setText("开始检查权限...")
            self.log_message("正在检查权限...")
            self.retry_button.hide()
            
            if not self.check_timer.isActive():
                self.check_timer.start()
                
        except Exception as e:
            self.log_message(f"❌ 启动权限检查失败: {str(e)}")
            QMessageBox.warning(self, "错误", f"启动权限检查失败:\n{str(e)}")
            self.retry_button.show()
    
    def check_current_permission(self):
        """检查当前索引对应的权限"""
        self.check_count += 1
        
        if self.check_count >= self.max_checks:
            self.check_timer.stop()
            self.status_label.setText("权限检查超时，请手动设置权限后重试")
            self.log_message("❌ 权限检查超时，请手动设置权限后重试")
            QMessageBox.warning(self, "检查超时", 
                "已检查100次仍未获得所有必需权限。\n"
                "请确保已正确设置所有必需权限，然后点击'重新检查权限'按钮重试。")
            self.retry_button.show()
            return

        if self.current_check_index >= len(self.permissions):
            self.check_timer.stop()
            self.finish_check()
            return
        
        permission, reason, required, check_func = self.permissions[self.current_check_index]
        try:
            check_func()
            self.permission_labels[self.current_check_index].setText("✅ 已获得权限")
            self.permission_labels[self.current_check_index].setStyleSheet("color: #27ae60;")
            self.status_label.setText(f"已获得{permission}，正在检查下一个权限...")
            self.log_message(f"✅ 已获得{permission}")
            self.current_check_index += 1
            self.check_count = 0
            
            # 更新步骤条样式
            for i, label in enumerate(self.step_labels):
                if i < self.current_check_index:
                    label.setStyleSheet("""
                        QLabel {
                            background-color: #2d8cf0;
                            color: white;
                            border: 1px solid #2d8cf0;
                            border-radius: 12px;
                            font-weight: bold;
                        }
                    """)
                elif i == self.current_check_index:
                    label.setStyleSheet("""
                        QLabel {
                            background-color: #ff4d4f;
                            color: white;
                            border: 1px solid #ff4d4f;
                            border-radius: 12px;
                            font-weight: bold;
                        }
                    """)
                else:
                    label.setStyleSheet("""
                        QLabel {
                            background-color: #f5f5f5;
                            color: #666666;
                            border: 1px solid #d9d9d9;
                            border-radius: 12px;
                            font-weight: bold;
                        }
                    """)
            
        except Exception as e:
            if required:
                self.permission_labels[self.current_check_index].setText("❌ 权限检查失败")
                self.permission_labels[self.current_check_index].setStyleSheet("color: #e74c3c;")
                self.status_label.setText(f"请设置{permission}，然后等待检查...")
                self.log_message(f"❌ {permission}检查失败，请设置权限")
            else:
                self.permission_labels[self.current_check_index].setText("⚠ 可选权限未设置")
                self.permission_labels[self.current_check_index].setStyleSheet("color: #f39c12;")
                self.log_message(f"⚠ {permission}未设置（可选）")
                self.current_check_index += 1
                self.check_count = 0
    
    def finish_check(self):
        """完成权限检查"""
        required_permissions_ok = all(
            not required or "✅" in label.text()
            for (_, _, required, _), label in zip(self.permissions, self.permission_labels)
        )
        
        if required_permissions_ok:
            self.status_label.setText("✅ 所有必需权限已设置完成！")
            self.log_message("✅ 所有必需权限已设置完成！")
        else:
            self.status_label.setText("⚠️ 某些必需权限未设置，请重试...")
            self.log_message("⚠️ 某些必需权限未设置，请重试...")
        
            self.retry_button.show()

    def show_step_details(self, index):
        """显示步骤的详细信息"""
        permission, reason, required, _ = self.permissions[index]
        status_text = self.permission_labels[index].text()
        
        # 创建详细信息对话框
        msg = QMessageBox(self)
        msg.setWindowTitle("权限详细信息")
        
        # 设置图标
        if "✅" in status_text:
            msg.setIcon(QMessageBox.Information)
        elif "❌" in status_text:
            msg.setIcon(QMessageBox.Warning)
        else:
            msg.setIcon(QMessageBox.Question)
        
        # 构建详细信息文本
        detail_text = f"""
权限名称: {permission}
用途: {reason}
是否必需: {'是' if required else '否'}
当前状态: {status_text}

设置说明:
{self.get_permission_guide(permission)}
"""
        msg.setText(detail_text)
        msg.exec_()

    def get_permission_guide(self, permission):
        """获取权限设置指南"""
        if "辅助功能权限" in permission:
            return """Windows系统:
1. 通常默认允许应用程序控制鼠标
2. 如遇问题，请以管理员身份运行程序
3. 检查杀毒软件是否拦截了鼠标控制"""
        elif "输入监控权限" in permission:
            return """Windows系统:
1. 通常默认允许应用程序监控键盘
2. 如遇问题，请以管理员身份运行程序
3. 检查杀毒软件是否拦截了键盘监控"""
        else:
            return """Windows系统:
1. 通常默认允许应用程序截取屏幕
2. 如遇问题，请以管理员身份运行程序
3. 检查是否有其他程序限制了屏幕访问"""

    def handle_close(self, event):
        """处理窗口关闭事件"""
        try:
            # 停止Flask服务器
            if 'server' in globals():
                server.stop()
                server.close()
            
            # 停止所有定时器
            self.check_timer.stop()
            
            # 结束所有线程
            for thread in threading.enumerate():
                if thread != threading.current_thread():
                    thread.join(timeout=1.0)
            
            # 强制结束进程
            os._exit(0)  # 使用 os._exit 强制结束进程
            
        except Exception as e:
            print(f"关闭时出错: {str(e)}")
            os._exit(1)  # 出错时也强制结束

# 创建Flask应用
app = Flask(__name__)
CORS(app)

# 设置pyautogui的安全限制
pyautogui.FAILSAFE = True

# DeepSeek API配置
DEEPSEEK_API_URL = "https://api.deepseek.com/v1/chat/completions"
DEEPSEEK_API_KEY = "your_api_key_here"  # 需要替换为实际的API密钥

# 构建职位分析的prompt
def build_job_analysis_prompt(text):
    return f"""作为一个专业的HR助手，请帮我解析以下职位描述，并提取关键信息填充到结构化数据中。

职位描述文本：
{text}

请按照以下要求进行解析：
1. 提取所有关键信息，包括职位名称、部门、职级、薪资范围等
2. 对于文本中未明确提到的字段，根据上下文和行业经验进行合理推断
3. 将工作职责和要求分条列出
4. 确保所有必填字段都有值
5. 保持数据格式的一致性

请将解析结果以JSON格式返回。
"""

# 路由定义
@app.route('/api/health')
def health_check():
    return jsonify({
        "status": "ok",
        "success": True,
        "timestamp": time.time()
    })

@app.route('/api/mouse/move', methods=['GET'])
def move_mouse():
    try:
        x = float(request.args.get('x', 0))
        y = float(request.args.get('y', 0))
        click = request.args.get('click', 'false').lower() == 'true'
        duration = float(request.args.get('duration', 0.5))
        
        # 重置完成标志
        window.mouse_controller.operation_completed.clear()
        
        # 发送信号到Qt线程
        window.mouse_controller.move_signal.emit(x, y, duration, click)
        
        # 等待操作完成
        window.mouse_controller.operation_completed.wait()
            
        return jsonify({
            "success": True,
            "message": f"鼠标已移动到 ({x}, {y})" + (" 并点击" if click else "")
        })
    except Exception as e:
        return jsonify({"error": str(e)}), 400

@app.route('/api/mouse/scroll', methods=['GET'])
def scroll_mouse():
    try:
        x = float(request.args.get('x', 0))
        y = float(request.args.get('y', 0))
        scroll_amount = int(request.args.get('scroll', 0))
        duration = float(request.args.get('duration', 0.5))
        
        # 重置完成标志
        window.mouse_controller.operation_completed.clear()
        
        # 发送信号到Qt线程
        window.mouse_controller.scroll_signal.emit(x, y, scroll_amount, duration)
        
        # 等待操作完成
        window.mouse_controller.operation_completed.wait()
            
        return jsonify({
            "success": True,
            "message": f"鼠标已移动到 ({x}, {y}) 并滚动 {scroll_amount} 个刻度"
        })
    except Exception as e:
        return jsonify({"error": str(e)}), 400

@app.route('/api/ai/analyze', methods=['POST'])
def analyze_job():
    text = request.json.get('text', '')
    if not text:
        return jsonify({"error": "文本不能为空"}), 400
    
    try:
        # 构建请求
        prompt = build_job_analysis_prompt(text)
        response = requests.post(
            DEEPSEEK_API_URL,
            headers={
                "Authorization": f"Bearer {DEEPSEEK_API_KEY}",
                "Content-Type": "application/json"
            },
            json={
                "model": "deepseek-chat",
                "messages": [
                    {
                        "role": "user",
                        "content": prompt
                    }
                ],
                "temperature": 0.3,
                "max_tokens": 2000
            }
        )
        
        # 检查响应
        if response.status_code == 200:
            result = response.json()
            # 提取AI回复的内容
            ai_response = result['choices'][0]['message']['content']
            try:
                # 尝试解析JSON响应
                parsed_data = json.loads(ai_response)
                return jsonify({
                    "success": True,
                    "data": parsed_data
                })
            except json.JSONDecodeError:
                return jsonify({
                    "success": False,
                    "error": "AI返回的数据格式不正确"
                }), 400
        else:
            return jsonify({
                "success": False,
                "error": f"API调用失败: {response.status_code}"
            }), response.status_code
            
    except Exception as e:
        logging.error(f"AI分析失败: {str(e)}")
        return jsonify({"error": str(e)}), 500

@app.route('/api/heartbeat', methods=['GET'])
def heartbeat():
    """处理心跳检测"""
    try:
        return jsonify({
            'type': 'pong',
            'timestamp': time.time(),
            'status': 'ok'
        })
    except Exception as e:
        logging.error(f"心跳检测错误: {str(e)}")
        return jsonify({"error": str(e)}), 500

def run_flask():
    """在单独的线程中运行Flask服务器"""
    try:
        port = 5000  # 固定使用5000端口
        kill_process_on_port(port)
        print(f"使用端口: {port}")
        
        # 使用 Flask 的开发服务器
        app.run(host='127.0.0.1', port=port, threaded=True)
            
    except Exception as e:
        print(f"Flask服务器启动失败: {str(e)}")

if __name__ == '__main__':
    try:
        # 创建Qt应用
        qt_app = QApplication(sys.argv)
        
        # 创建并显示权限检查界面
        window = PermissionGuide()
        window.show()
        
        # 启动Flask服务器（在单独的线程中）
        import threading
        flask_thread = threading.Thread(target=run_flask, daemon=True)
        flask_thread.start()
        
        # 记录启动日志
        window.log_message("服务已启动，正在监听 http://127.0.0.1:5000")
        
        # 运行Qt事件循环
        sys.exit(qt_app.exec_())
        
    except Exception as e:
        print(f"启动失败: {str(e)}")
        input("按回车键退出...") 