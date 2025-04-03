import json
import os
import time
import requests
from datetime import datetime, timedelta
from typing import Any, Dict, Optional, List, Tuple, Union
import traceback

# 导入ApiClient类
try:
    from api_client import ApiClient
except ImportError:
    ApiClient = None

# 添加这个全局变量，以便稍后初始化
add_log_func = None

class ConfigManager:
    def __init__(self, config_file: str = "config.json"):
        """
        初始化配置管理器
        Args:
            config_file: 配置文件路径，默认为'config.json'
        """
        # 导入 eel，尝试直接向前端发送初始化日志
        try:
            import eel
            eel.addLogFromPython(f"[ConfigManager] 初始化 ConfigManager，配置文件路径：{config_file}")
        except Exception as e:
            print(f"直接通过eel发送初始化日志失败: {str(e)}")
            
        print(f"初始化ConfigManager，配置文件路径：{config_file}")
        self.config_file = config_file
        self.config_data: Dict[str, Any] = {}
        
        # 创建API客户端实例，用于与服务器通信
        self.api_url = "http://goodhr.58it.cn/api/service/api/"
        if ApiClient:
            self.api_client = ApiClient(self.api_url, logger=self)
            self._log("API客户端初始化成功")
        else:
            self.api_client = None
            self._log("未找到ApiClient类，将使用内置服务器通信方法")
        
        # 检查配置文件是否存在，如果存在则加载
        # 但不会在此时创建默认配置
        if os.path.exists(self.config_file):
            try:
                self.load_config()
                self._log("现有配置文件加载成功")
            except Exception as e:
                self._log(f"加载现有配置文件失败: {str(e)}")
                self.config_data = {}  # 初始化为空字典，但不创建默认配置
        else:
            self._log("配置文件不存在，等待用户输入手机号后再初始化配置")
            self.config_data = {}  # 初始化为空字典
        
    def _log(self, message: str) -> None:
        """
        记录日志，格式与eel_app.py中的add_log保持一致
        Args:
            message: 日志消息
        """
        try:
            # 获取当前时间
            now = datetime.now().strftime("%H:%M:%S")
            
            # 格式化日志消息
            log_message = f"[{now}] [ConfigManager] {message}"
            
            # 输出到控制台
            print(log_message)
            
            # 尝试使用UI日志函数
            global add_log_func
            if add_log_func is not None:
                try:
                    add_log_func(message)
                except Exception as e:
                    print(f"向UI发送日志失败(通过add_log_func): {str(e)}")
            
            # 尝试通过eel直接发送到前端
            try:
                import eel
                eel.addLogFromPython(log_message)
            except Exception as e:
                # 静默失败，不再打印错误
                pass
                
            # 保存到日志文件
            try:
                today = datetime.now().strftime("%Y-%m-%d")
                log_dir = "运行日志logs"
                
                # 确保日志目录存在
                if not os.path.exists(log_dir):
                    os.makedirs(log_dir)
                    
                log_file = os.path.join(log_dir, f"log_{today}.txt")
                
                with open(log_file, "a", encoding="utf-8") as f:
                    f.write(log_message + "\n")
            except Exception as e:
                print(f"保存日志到文件失败: {str(e)}")
        except Exception as e:
            print(f"记录日志时出错: {str(e)}")
        
    def load_config(self) -> Dict[str, Any]:
        """
        从文件加载配置数据
        Returns:
            Dict: 配置数据
        """
        try:
            if os.path.exists(self.config_file):
                with open(self.config_file, 'r', encoding='utf-8') as f:
                    self.config_data = json.load(f)
                self._log("配置文件加载成功")
                
                # 确保必要的字段存在
                self._ensure_config_fields()
                
                return self.config_data
            else:
                self._log(f"配置文件不存在: {self.config_file}")
                # 不再自动创建默认配置
                return {}
        except json.JSONDecodeError as e:
            self._log(f"配置文件JSON格式错误: {str(e)}")
            # 不再备份错误的配置文件，直接返回空配置
            self._log("JSON格式错误，返回空配置")
            return {}
        except Exception as e:
            self._log(f"加载配置文件出错: {str(e)}")
            return {}

    def save_config(self) -> bool:
        """
        保存配置到文件
        Returns:
            bool: 是否保存成功
        """
        try:
            # 确保目录存在
            config_dir = os.path.dirname(self.config_file)
            if config_dir and not os.path.exists(config_dir):
                os.makedirs(config_dir)
                self._log(f"创建配置目录: {config_dir}")
            
            # 直接保存配置到文件，不再创建备份
            with open(self.config_file, 'w', encoding='utf-8') as f:
                json.dump(self.config_data, f, ensure_ascii=False, indent=4)
            
            self._log(f"配置已保存到: {self.config_file}")
            return True
        except Exception as e:
            self._log(f"保存配置文件出错: {str(e)}")
            return False

    def get_config_item(self, key: str) -> Any:
        """
        获取指定配置项的值
        Args:
            key: 配置项键名
        Returns:
            Any: 配置项的值
        """
        return self.config_data.get(key)

    def save_config_item(self, key: Union[str, list], value: Any, display_name: Optional[str] = None, sync_to_server: bool = True) -> bool:
        """
        保存单个配置项，支持通过数组形式的key访问嵌套对象，并同步到服务器
        Args:
            key: 配置项键名，可以是字符串或列表(用于访问嵌套对象)
            value: 配置项的值
            display_name: 显示名称（用于日志）
            sync_to_server: 是否同步到服务器
        Returns:
            bool: 是否保存成功
        """
        try:
            # 如果key是字符串，直接设置值
            if isinstance(key, str):
                self.config_data[key] = value
            # 如果key是列表，递归访问嵌套对象
            elif isinstance(key, list):
                current = self.config_data
                for i, k in enumerate(key):
                    if i == len(key) - 1:
                        current[k] = value
                    else:
                        if k not in current:
                            current[k] = {}
                        current = current[k]
            
            # 保存到本地文件
            success = self.save_config()
            
            # 同步到服务器
            if success and sync_to_server:
                try:
                    phone = self.config_data.get("username", "")
                    if phone:
                        upload_success = self.upload_config_to_server(phone)
                        if upload_success:
                            self._log("配置已同步到服务器")
                        else:
                            self._log("同步到服务器失败")
                except Exception as e:
                    self._log(f"同步到服务器时出错: {str(e)}")
            
            if success:
                key_str = key if isinstance(key, str) else '.'.join(str(k) for k in key)
                self._log(f"已保存{display_name or key_str}: {value}")
            return success
        except Exception as e:
            key_str = key if isinstance(key, str) else '.'.join(str(k) for k in key)
            self._log(f"保存配置项{display_name or key_str}出错: {str(e)}")
            return False

    def update_config(self, new_config: Dict[str, Any]) -> bool:
        """
        更新配置数据
        Args:
            new_config: 新的配置数据
        Returns:
            bool: 是否更新成功
        """
        try:
            self.config_data.update(new_config)
            return self.save_config()
        except Exception as e:
            self._log(f"更新配置出错: {str(e)}")
            return False

    def _create_default_config(self) -> Dict[str, Any]:
        """
        创建默认配置
        Returns:
            Dict: 默认配置数据
        """
        today = datetime.now().date().strftime("%Y-%m-%d")
        return {
            "version": "free",
            "platform": "",
            "username": "",
            "selected_job_id": "",
            "userID": "",
            "browser_path": "",
            "jobsData": ["成都销售"],
            "keywordsData": {
                "成都销售": {
                    "include": ["电话销售", "在线销售", "电销"],
                    "exclude": ["保险销售"],
                    "relation": "OR",
                    "description": ""
                }
            },
            "selectedJob": "成都销售",
            "versions": {
                "free": {
                    "greetCount": 0,
                    "remainingQuota": 100,
                    "expiryDate": "永久有效",
                    "lastResetDate": today
                },
                "donation": {
                    "greetCount": 0,
                    "remainingQuota": 0,
                    "expiryDate": "",
                    "lastResetDate": today
                },
                "enterprise": {
                    "greetCount": 0,
                    "remainingQuota": 0,
                    "expiryDate": "",
                    "lastResetDate": today
                }
            }
        }

    def _ensure_config_fields(self) -> None:
        """
        确保配置中包含所有必要的字段
        """
        today = datetime.now().date().strftime("%Y-%m-%d")
        default_config = self._create_default_config()
        
        # 确保基本字段存在
        for key, value in default_config.items():
            if key not in self.config_data:
                self.config_data[key] = value
        
        # 确保版本信息完整
        if "versions" in self.config_data:
            for version, data in default_config["versions"].items():
                if version not in self.config_data["versions"]:
                    self.config_data["versions"][version] = data
                else:
                    for field, value in data.items():
                        if field not in self.config_data["versions"][version]:
                            self.config_data["versions"][version][field] = value

    def merge_server_config(self, server_config: Dict[str, Any]) -> bool:
        """
        合并服务器配置，保留本地的岗位描述
        Args:
            server_config: 服务器配置数据
        Returns:
            bool: 是否合并成功
        """
        try:
            # 检查服务器配置是否有效
            if not server_config:
                self._log("服务器配置为空，不执行合并")
                return False
            
            # 输出服务器配置以进行调试
            self._log(f"准备合并服务器配置: {server_config}")
            
            # 保存当前的岗位描述
            current_descriptions = {}
            if "keywordsData" in self.config_data:
                for job, data in self.config_data["keywordsData"].items():
                    if "description" in data:
                        current_descriptions[job] = data["description"]

            # 更新配置（选择性地更新特定字段，而不是整个配置）
            if "jobsData" in server_config:
                self.config_data["jobsData"] = server_config["jobsData"]
            
            if "keywordsData" in server_config:
                # 如果本地没有keywordsData字段，创建一个空字典
                if "keywordsData" not in self.config_data:
                    self.config_data["keywordsData"] = {}
                
                # 合并关键词数据
                for job, data in server_config["keywordsData"].items():
                    self.config_data["keywordsData"][job] = data
                
            if "selectedJob" in server_config:
                self.config_data["selectedJob"] = server_config["selectedJob"]
            
            if "platform" in server_config:
                self.config_data["platform"] = server_config["platform"]
            
            if "version" in server_config:
                self.config_data["version"] = server_config["version"]

            # 恢复岗位描述
            if "keywordsData" in self.config_data:
                for job, data in self.config_data["keywordsData"].items():
                    if job in current_descriptions:
                        data["description"] = current_descriptions[job]

            # 保存更新后的配置
            self._log("合并服务器配置完成，正在保存...")
            return self.save_config()
        except Exception as e:
            self._log(f"合并服务器配置出错: {str(e)}")
            return False

    def reset_daily_quota(self) -> bool:
        """
        重置每日配额
        Returns:
            bool: 是否重置成功
        """
        try:
            today = datetime.now().date().strftime("%Y-%m-%d")
            versions = self.config_data.get("versions", {})
            
            for version_data in versions.values():
                if version_data.get("lastResetDate") != today:
                    version_data["greetCount"] = 0
                    version_data["lastResetDate"] = today
                    if version_data.get("expiryDate") == "永久有效":  # 免费版
                        version_data["remainingQuota"] = 100

            return self.save_config()
        except Exception as e:
            self._log(f"重置每日配额出错: {str(e)}")
            return False

    def get_version_info(self) -> Dict[str, Any]:
        """
        获取当前版本信息
        Returns:
            Dict: 版本信息
        """
        try:
            current_version = self.config_data.get("version", "free")
            version_data = self.config_data.get("versions", {}).get(current_version, {})
            
            # 重置每日配额
            self.reset_daily_quota()
            
            return {
                "version": current_version,
                "greetCount": version_data.get("greetCount", 0),
                "remainingQuota": version_data.get("remainingQuota", 0),
                "expiryDate": version_data.get("expiryDate", "")
            }
        except Exception as e:
            self._log(f"获取版本信息出错: {str(e)}")
            return {}
            
    def update_greet_count(self, count: int = 1, tokens_used: int = 0) -> Dict[str, Any]:
        """
        更新打招呼计数，先尝试通过API更新，失败则使用本地更新
        Args:
            count: 增加的次数，默认为1。如果为0，则只更新token使用情况
            tokens_used: 使用的token数量，默认为0
        Returns:
            Dict: 更新后的版本信息
        """
        try:
            # 获取手机号
            phone = self.config_data.get("username", "")
            
            # 记录token使用情况
            if tokens_used > 0:
                self._log(f"本次分析消耗了 {tokens_used} 个tokens")
                # 注：token上传已经由AIAnalyzer处理，这里不再重复上传
            
            # 如果count为0，只更新token使用情况，不增加打招呼次数
            if count == 0:
                return {}
            
            # 如果有手机号，先尝试通过API更新
            if phone:
                api_result = self.update_greet_count_via_api(phone, count, tokens_used)
                if api_result:
                    self._log("成功通过API更新打招呼计数")
                    return api_result
                else:
                    self._log("API更新失败，使用本地更新")
                    
            # 尝试通过API更新失败或没有手机号，使用本地更新
            # 获取当前版本
            current_version = self.config_data.get("version", "free")
            
            # 检查版本合法性，默认为free
            if current_version not in ["free", "donation", "enterprise"]:
                current_version = "free"
                self.config_data["version"] = current_version
            
            # 确保versions字段存在
            if "versions" not in self.config_data:
                self.config_data["versions"] = {
                    "free": {
                        "greetCount": 0,
                        "remainingQuota": 100,
                        "expiryDate": "永久有效",
                        "lastResetDate": datetime.now().strftime("%Y-%m-%d")
                    },
                    "donation": {
                        "greetCount": 0,
                        "remainingQuota": 0,
                        "expiryDate": (datetime.now() + timedelta(days=30)).strftime("%Y-%m-%d"),
                        "lastResetDate": datetime.now().strftime("%Y-%m-%d")
                    },
                    "enterprise": {
                        "greetCount": 0,
                        "remainingQuota": 0,
                        "expiryDate": "",
                        "lastResetDate": datetime.now().strftime("%Y-%m-%d")
                    }
                }
            
            # 确保当前版本字段存在
            if current_version not in self.config_data["versions"]:
                self.config_data["versions"][current_version] = {
                    "greetCount": 0,
                    "remainingQuota": 100 if current_version == "free" else 0,
                    "expiryDate": "永久有效" if current_version == "free" else "",
                    "lastResetDate": datetime.now().strftime("%Y-%m-%d")
                }
            
            # 获取当前版本数据
            version_data = self.config_data["versions"][current_version]
            
            # 更新打招呼次数
            greet_count = version_data.get("greetCount", 0) + count
            version_data["greetCount"] = greet_count
            
            # 更新今日打招呼次数
            today_count = version_data.get("todayCount", 0) + count
            version_data["todayCount"] = today_count
            
            # 更新token使用情况
            if tokens_used > 0:
                total_tokens_used = version_data.get("tokens_used", 0) + tokens_used
                version_data["tokens_used"] = total_tokens_used
            
            # 更新剩余配额
            remaining_quota = version_data.get("remainingQuota", 0)
            if current_version == "free":
                # 免费版默认每日100次，不扣减剩余配额
                free_quota = version_data.get("freeQuota", 100)
                version_data["remainingQuota"] = free_quota - today_count
            elif current_version == "enterprise":
                # 企业版减少剩余配额
                if remaining_quota > 0:
                    version_data["remainingQuota"] = remaining_quota - count
            
            # 更新配置文件
            self.config_data["versions"][current_version] = version_data
            self.save_config()
            
            self._log(f"本地更新打招呼计数成功，当前为 {greet_count} 次，剩余配额 {version_data.get('remainingQuota', 0)} 次")
            
            # 构造返回结果
            result = {
                "version": current_version,
                "greetCount": greet_count,
                "remainingQuota": version_data.get("remainingQuota", 0),
                "expiryDate": version_data.get("expiryDate", "")
            }
            
            return result
            
        except Exception as e:
            self._log(f"更新打招呼计数失败: {str(e)}")
            # 打印堆栈跟踪，帮助调试
            import traceback
            traceback.print_exc()
            return {}
            
    def fetch_config_from_server(self, phone: str) -> Dict[str, Any]:
        """
        从服务器获取配置数据
        Args:
            phone: 手机号
        Returns:
            Dict: 服务器配置数据
        """
        try:
            self._log(f"正在从服务器获取手机号 {phone} 的配置...")
            
            # 优先使用ApiClient获取配置
            if self.api_client:
                self._log("使用API客户端获取配置")
                result = self.api_client.get_user_config(phone)
                if result:
                    self._log("通过API客户端成功获取配置")
                    return result
                self._log("API客户端获取配置失败，尝试使用备用方法")
            
            # 备用方法，直接使用请求
            url = f"{self.api_url}/user_config.php?phone={phone}"
            headers = {
                "Content-Type": "application/json; charset=utf-8", 
                "Accept": "application/json; charset=utf-8"
            }
            
            response = requests.get(url, headers=headers, timeout=10)
            
            # 检查响应状态
            if response.status_code == 200:
                self._log(f"服务器原始响应: {response.text[:200]}...")
                
                # 尝试直接解析响应的JSON
                try:
                    result = json.loads(response.text)
                    self._log(f"接收到服务器响应，成功解析JSON: {result}")
                    
                    # 新的API返回格式: {"success": true, "data": {...}}
                    if isinstance(result, dict) and "success" in result and result["success"]:
                        if "data" in result and isinstance(result["data"], dict):
                            server_config = result["data"]
                            self._log("从服务器获取配置成功")
                            return server_config
                        else:
                            self._log("服务器响应缺少data字段或data不是对象")
                            return {}
                    # 如果API返回错误
                    elif isinstance(result, dict) and "error" in result:
                        self._log(f"服务器返回错误: {result['error']}")
                        return {}
                    else:
                        self._log(f"服务器响应格式不符合预期")
                        return {}
                except json.JSONDecodeError as e:
                    self._log(f"解析服务器响应JSON失败: {str(e)}")
                    self._log(f"服务器原始响应: {response.text[:200]}")
                    return {}
            else:
                self._log(f"获取配置请求失败，状态码: {response.status_code}, 响应: {response.text}，URL:{url}")
                return {}
        except Exception as e:
            self._log(f"从服务器获取配置出错: {str(e)}")
            traceback.print_exc()  # 打印详细的错误堆栈
            return {}
            
    def upload_config_to_server(self, phone: str) -> bool:
        """
        将配置上传到服务器
        Args:
            phone: 手机号
        Returns:
            bool: 是否上传成功
        """
        try:
            self._log(f"正在将配置上传到服务器(手机号: {phone})...")
            
            # 准备上传的数据
            upload_data = {
                "phone": phone,
                "jobsData": self.config_data.get("jobsData", []),
                "keywordsData": self.config_data.get("keywordsData", {}),
                "selectedJob": self.config_data.get("selectedJob", ""),
                "platform": self.config_data.get("platform", ""),
                "version": self.config_data.get("version", "free")
            }
            
            # 打印准备上传的数据以进行调试
            self._log(f"准备上传的数据: {upload_data}")
            
            # 优先使用ApiClient上传配置
            if self.api_client:
                self._log("使用API客户端上传配置")
                result = self.api_client.update_user_config(upload_data)
                if result:
                    self._log("通过API客户端成功上传配置")
                    return True
                self._log("API客户端上传配置失败，尝试使用备用方法")
            
            # 备用方法，直接使用请求
            url = f"{self.api_url}/user_config.php"
            headers = {
                "Content-Type": "application/json; charset=utf-8",
                "Accept": "application/json; charset=utf-8"
            }
            
            # 使用ensure_ascii=False确保中文字符不被转义为Unicode码点
            json_data = json.dumps(upload_data, ensure_ascii=False)
            self._log(f"发送的JSON数据: {json_data[:200]}...")
            
            # 使用json参数直接发送JSON数据
            response = requests.post(
                url, 
                json=upload_data,  # 直接使用json参数
                headers=headers, 
                timeout=10
            )
            
            # 检查响应状态
            if response.status_code == 200:
                self._log(f"服务器原始响应: {response.text}")
                try:
                    result = response.json()
                    
                    # 检查是否成功 - 新API使用 success: true 格式
                    if result.get("success") is True:
                        self._log("配置上传成功")
                        return True
                    else:
                        error_msg = result.get('error', '未知错误')
                        self._log(f"配置上传失败，服务器返回错误: {error_msg}")
                        return False
                except Exception as e:
                    self._log(f"解析服务器响应失败: {str(e)}")
                    # 即使无法解析JSON，也尝试返回原始响应
                    self._log(f"服务器原始响应: {response.text[:200]}")
                    # 如果响应包含"success"，认为上传成功
                    if "success" in response.text:
                        self._log("根据响应内容判断，配置上传成功")
                        return True
                    return False
            else:
                self._log(f"配置上传请求失败，状态码: {response.status_code}, 响应: {response.text}")
                return False
                
        except Exception as e:
            self._log(f"将配置上传到服务器时出错: {str(e)}")
            traceback.print_exc()  # 打印详细的错误堆栈
            return False
            
    def should_use_server_config(self, server_config: Dict[str, Any]) -> bool:
        """
        判断是否应该使用服务器配置
        Args:
            server_config: 服务器配置数据
        Returns:
            bool: 是否应该使用服务器配置
        """
        try:
            # 如果服务器配置为空，不使用
            if not server_config:
                return False
                
            # 检查服务器配置是否有岗位数据
            if not server_config.get("jobsData") or len(server_config.get("jobsData", [])) == 0:
                return False
                
            # 检查本地配置是否为空
            if not self.config_data.get("jobsData") or len(self.config_data.get("jobsData", [])) == 0:
                return True
                
            # 如果服务器配置有更多的岗位数据，使用服务器配置
            if len(server_config.get("jobsData", [])) > len(self.config_data.get("jobsData", [])):
                return True
                
            # 如果服务器配置有更多的关键词数据，使用服务器配置
            server_keywords = server_config.get("keywordsData", {})
            local_keywords = self.config_data.get("keywordsData", {})
            
            if len(server_keywords) > len(local_keywords):
                return True
                
            # 检查是否有新的岗位
            for job in server_config.get("jobsData", []):
                if job not in self.config_data.get("jobsData", []):
                    return True
                    
            # 检查是否有新的关键词
            for job, data in server_keywords.items():
                if job not in local_keywords:
                    return True
                
                # 检查包含关键词
                server_include = data.get("include", [])
                local_include = local_keywords.get(job, {}).get("include", [])
                
                if len(server_include) > len(local_include):
                    return True
                    
                # 检查排除关键词
                server_exclude = data.get("exclude", [])
                local_exclude = local_keywords.get(job, {}).get("exclude", [])
                
                if len(server_exclude) > len(local_exclude):
                    return True
                    
            # 默认不使用服务器配置
            return False
            
        except Exception as e:
            self._log(f"判断是否使用服务器配置时出错: {str(e)}")
            return False

    def initialize_with_phone(self, phone: str) -> bool:
        """
        使用手机号初始化配置
        首先尝试从服务器获取配置，如果服务器没有配置，则创建默认配置
        Args:
            phone: 用户手机号
        Returns:
            bool: 是否成功初始化
        """
        try:
            # 验证手机号格式
            if not phone or not isinstance(phone, str) or len(phone) != 11 or not phone.isdigit() or not phone.startswith('1'):
                self._log(f"手机号 {phone} 格式不正确，必须是11位数字且以1开头")
                return False
                
            self._log(f"使用手机号 {phone} 初始化配置...")
            
            # 如果已有本地配置文件，且包含手机号，检查是否与当前手机号相同
            if self.config_data and self.config_data.get("username") and self.config_data.get("username") == phone:
                self._log(f"本地配置文件中已存在手机号 {phone}，不需要重新初始化")
                # 检查本地配置是否包含必要的岗位数据和关键词数据
                # 如果没有，尝试从服务器获取，而不是使用默认值
                if (not self.config_data.get("jobsData") or len(self.config_data.get("jobsData", [])) == 0 or
                    not self.config_data.get("keywordsData") or len(self.config_data.get("keywordsData", {})) == 0):
                    self._log("本地配置缺少岗位或关键词数据，尝试从服务器获取")
                    # 尝试从服务器获取配置
                    server_config = self.fetch_config_from_server(phone)
                    
                    if server_config and "jobsData" in server_config and len(server_config["jobsData"]) > 0:
                        self._log("从服务器获取到有效配置，合并到本地配置")
                        # 合并配置，保留本地可能有的其他配置
                        if "jobsData" in server_config:
                            self.config_data["jobsData"] = server_config["jobsData"]
                        if "keywordsData" in server_config:
                            self.config_data["keywordsData"] = server_config["keywordsData"]
                        if "selectedJob" in server_config:
                            self.config_data["selectedJob"] = server_config["selectedJob"]
                        
                        # 保存配置
                        self.save_config()
                
                return True
                
            # 尝试从服务器获取配置
            server_config = self.fetch_config_from_server(phone)
            
            self._log(f"服务器配置: {server_config}")
            if server_config and "jobsData" in server_config:
                self._log(f"成功从服务器获取手机号 {phone} 的配置，应用服务器配置")
                
                # 确保手机号字段存在
                server_config["username"] = phone
                
                # 更新本地配置
                self.config_data = server_config
                
                # 保存配置
                success = self.save_config()
                if success:
                    self._log(f"成功保存服务器配置到本地")
                    return True
                else:
                    self._log(f"保存服务器配置到本地失败")
                    return False
            else:
                self._log(f"服务器上没有找到手机号 {phone} 的配置，创建默认配置")
                
                # 创建默认配置
                self.config_data = self._create_default_config()
                
                # 设置手机号
                self.config_data["username"] = phone
                
                # 保存配置
                success = self.save_config()
                if success:
                    self._log(f"成功创建默认配置并保存到本地")
                    
                    # 上传到服务器
                    upload_success = self.upload_config_to_server(phone)
                    if upload_success:
                        self._log(f"成功将默认配置上传到服务器")
                    else:
                        self._log(f"将默认配置上传到服务器失败")
                    
                    return True
                else:
                    self._log(f"保存默认配置到本地失败")
                    return False
        except Exception as e:
            self._log(f"使用手机号初始化配置失败: {str(e)}")
            traceback.print_exc()
            return False

    def update_greet_count_via_api(self, phone: str, count: int = 1, tokens_used: int = 0) -> Dict[str, Any]:
        """
        通过API更新打招呼计数
        Args:
            phone: 用户手机号
            count: 增加的次数，默认为1。如果为0，则只更新token使用情况
            tokens_used: 使用的token数量，默认为0（仅记录，不主动上传，token上传由AIAnalyzer处理）
        Returns:
            Dict: 更新后的版本信息
        """
        try:
            self._log(f"正在通过API更新手机号 {phone} 的打招呼计数...")
            
            # 记录token使用情况
            if tokens_used > 0:
                self._log(f"记录token使用情况: {tokens_used} tokens")
                # 注：token上传已经由AIAnalyzer处理，这里不再重复上传
            
            # 如果count为0，只更新token使用情况，不增加打招呼次数
            if count == 0:
                return {}
            
            # 优先使用ApiClient更新打招呼计数
            if self.api_client:
                self._log("使用API客户端更新打招呼计数")
                result = self.api_client.update_greet_count(phone, tokens_used)
                if result:
                    self._log("通过API客户端成功更新打招呼计数")
                    return result
                self._log("API客户端更新打招呼计数失败，尝试使用备用方法")
            
            # 备用方法，直接使用请求
            url = f"{self.api_url}/update_count.php"
            headers = {
                "Content-Type": "application/json; charset=utf-8",
                "Accept": "application/json; charset=utf-8"
            }
            
            # 准备上传的数据，包含token使用量
            payload = {
                "phone": phone
            }
            
            # 如果有token使用量，添加到payload
            if tokens_used > 0:
                payload["tokens_used"] = tokens_used
                self._log(f"上传token使用量: {tokens_used}")
            
            # 发送POST请求
            response = requests.post(
                url, 
                json=payload,
                headers=headers, 
                timeout=10
            )
            
            # 检查响应状态
            if response.status_code == 200:
                self._log(f"服务器原始响应: {response.text}")
                try:
                    result = response.json()
                    
                    # 检查是否成功
                    if result.get("success") is True:
                        self._log("打招呼计数更新成功")
                        if "data" in result and isinstance(result["data"], dict):
                            return result["data"]
                        else:
                            self._log("API返回成功但没有数据")
                            return {}
                    else:
                        error_msg = result.get('error', '未知错误')
                        self._log(f"打招呼计数更新失败，服务器返回错误: {error_msg}")
                        return {}
                except Exception as e:
                    self._log(f"解析服务器响应失败: {str(e)}")
                    return {}
            else:
                self._log(f"打招呼计数更新请求失败，状态码: {response.status_code}, 响应: {response.text}")
                return {}
                
        except Exception as e:
            self._log(f"通过API更新打招呼计数出错: {str(e)}")
            traceback.print_exc()  # 打印详细的错误堆栈
            return {} 

    def info(self, message: str) -> None:
        """
        供ApiClient调用的日志方法，将消息转发给_log方法
        Args:
            message: 日志消息
        """
        self._log(message) 