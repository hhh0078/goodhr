import json
import requests
import logging
import os
from datetime import datetime
from typing import Dict, Any, Optional, List, Union

class ApiClient:
    """API客户端，用于与服务器进行通信"""
    
    def __init__(self, base_url: str, timeout: int = 10, logger=None):
        """
        初始化API客户端
        
        Args:
            base_url: API服务器的基础URL
            timeout: 请求超时时间（秒）
            logger: 日志记录器
        """
        self.base_url = base_url.rstrip('/')
        self.timeout = timeout
        self.logger = logger or logging.getLogger(__name__)
    
    def _log(self, message: str):
        """
        记录日志，格式与config_manager.py和eel_app.py中保持一致
        Args:
            message: 日志消息
        """
        try:
            # 获取当前时间
            now = datetime.now().strftime("%H:%M:%S")
            
            # 格式化日志消息
            log_message = f"[{now}] [ApiClient] {message}"
            
            # 输出到控制台
            print(log_message)
            
            # 记录到logger
            if self.logger:
                self.logger.info(message)
            
            # 尝试通过eel直接发送到前端
            try:
                import eel
                eel.addLogFromPython(log_message)
            except Exception:
                # 静默失败，不打印错误
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
    
    def _handle_response(self, response: requests.Response) -> Dict[str, Any]:
        """
        处理API响应
        
        Args:
            response: 请求响应对象
            
        Returns:
            Dict: 响应数据
            
        Raises:
            Exception: 如果响应状态码不是200或者响应数据不是JSON格式
        """
        if response.status_code != 200:
            error_message = f"API请求失败，状态码：{response.status_code},URL:{response.url}"
            self._log(error_message)
            raise Exception(error_message)
        
        try:
            data = response.json()
            return data
        except json.JSONDecodeError:
            error_message = "API响应格式不是有效的JSON"
            self._log(error_message)
            raise Exception(error_message)
    
    def get_user_config(self, phone: str) -> Optional[Dict[str, Any]]:
        """
        获取用户配置
        
        Args:
            phone: 用户手机号
            
        Returns:
            Dict: 用户配置数据，如果获取失败则返回None
        """
        try:
            url = f"{self.base_url}/user_config.php?phone={phone}"
            params = {"phone": phone}
            
            self._log(f"正在从服务器获取手机号 {phone} 的配置...")
            response = requests.get(url, params=params, timeout=self.timeout)
            
            data = self._handle_response(response)
            
            if data.get("success"):
                self._log("从服务器获取配置成功")
                return data.get("data")
            else:
                self._log(f"从服务器获取配置出错: {data.get('error', '未知错误')}")
                return None
                
        except Exception as e:
            self._log(f"从服务器获取配置出错: {str(e)}")
            return None
    
    def update_user_config(self, config: Dict[str, Any]) -> bool:
        """
        更新用户配置
        
        Args:
            config: 用户配置数据，必须包含phone字段
            
        Returns:
            bool: 是否更新成功
        """
        try:
            if not config.get("phone"):
                self._log("更新配置失败：配置数据中缺少phone字段")
                return False
            
            url = f"{self.base_url}/user_config.php"
            phone = config.get("phone")
            
            self._log(f"正在将配置上传到服务器(手机号: {phone})...")
            self._log(f"准备上传的数据: {config}")
            
            # 转换为JSON字符串，记录日志，方便调试
            json_data = json.dumps(config, ensure_ascii=False)
            self._log(f"发送的JSON数据: {json_data[:100]}...")
            
            response = requests.post(url, json=config, timeout=self.timeout)
            
            data = self._handle_response(response)
            
            if data.get("success"):
                self._log("配置上传成功")
                return True
            else:
                self._log(f"配置上传失败，服务器返回错误: {data.get('error', '未知错误')}")
                return False
                
        except Exception as e:
            self._log(f"将配置上传到服务器时出错: {str(e)},URL:{url}")
            return False
    
    def update_greet_count(self, phone: str, tokens_used: int = 0) -> Optional[Dict[str, Any]]:
        """
        更新打招呼计数
        
        Args:
            phone: 用户手机号
            tokens_used: 使用的token数量，默认为0
            
        Returns:
            Dict: 更新后的版本信息，如果更新失败则返回None
        """
        try:
            url = f"{self.base_url}/update_count.php"
            payload = {"phone": phone}
            
            # 如果有token使用量，添加到payload
            if tokens_used > 0:
                payload["tokens_used"] = tokens_used
                self._log(f"上传token使用量: {tokens_used}")
            self._log(f"上传token使用量: {tokens_used}")
            self._log(f"正在更新手机号 {phone} 的打招呼计数...")
            response = requests.post(url, json=payload, timeout=self.timeout)
            
            data = self._handle_response(response)
            
            if data.get("success"):
                self._log("打招呼计数更新成功")
                return data.get("data")
            else:
                self._log(f"打招呼计数更新失败，服务器返回错误: {data.get('error', '未知错误')}")
                return None
                
        except Exception as e:
            self._log(f"更新打招呼计数出错: {str(e)}")
            return None
    
    def get_platform_config(self, platform: str = "") -> Union[Dict[str, Any], List[Dict[str, Any]], None]:
        """
        获取平台配置
        
        Args:
            platform: 平台名称，如果为空则获取所有平台配置
            
        Returns:
            Dict或List: 平台配置数据，如果获取失败则返回None
        """
        try:
            url = f"{self.base_url}/platform_config"
            params = {}
            
            if platform:
                params["platform"] = platform
                self._log(f"正在获取平台 {platform} 的配置...")
            else:
                self._log("正在获取所有平台的配置...")
                
            response = requests.get(url, params=params, timeout=self.timeout)
            
            data = self._handle_response(response)
            
            if data.get("success"):
                self._log("成功从服务器获取平台配置")
                return data.get("data")
            else:
                self._log(f"从服务器获取平台配置失败: {data.get('error', '未知错误')}")
                return None
                
        except Exception as e:
            self._log(f"获取平台配置出错: {str(e)}")
            return None
    
    def update_platform_config(self, platform: str, config: Dict[str, Any]) -> bool:
        """
        更新平台配置
        
        Args:
            platform: 平台名称
            config: 平台配置数据
            
        Returns:
            bool: 是否更新成功
        """
        try:
            url = f"{self.base_url}/platform_config"
            payload = {
                "platform": platform,
                "config": config
            }
            
            self._log(f"正在更新平台 {platform} 的配置...")
            response = requests.post(url, json=payload, timeout=self.timeout)
            
            data = self._handle_response(response)
            
            if data.get("success"):
                self._log("平台配置更新成功")
                return True
            else:
                self._log(f"平台配置更新失败，服务器返回错误: {data.get('error', '未知错误')}")
                return False
                
        except Exception as e:
            self._log(f"更新平台配置出错: {str(e)}")
            return False
    
    def upload_tokens_used(self, phone: str, tokens_used: int) -> bool:
        """
        上传token使用情况到企业版专用接口
        
        Args:
            phone: 用户手机号
            tokens_used: 使用的token数量
            
        Returns:
            bool: 是否上传成功
        """
        try:
            # 如果token使用量为0，则不上传
            if tokens_used <= 0:
                return True
                
            # 验证手机号有效性
            if not phone or not isinstance(phone, str) or len(phone) != 11:
                self._log("上传token使用情况失败: 手机号无效")
                return False
            
            # 企业版专用接口URL
            url = "https://goodhr.58it.cn/api/service/api/qiyebantokens.php"
            
            # 构造请求数据
            payload = {
                "phone": phone,
                "tokens_used": tokens_used
            }
            
            self._log(f"正在上传token使用情况: {tokens_used} tokens (手机号: {phone})...")
            response = requests.post(url, json=payload, timeout=self.timeout)
            
            # 检查响应状态码
            if response.status_code == 200:
                try:
                    data = response.json()
                    self._log(f"Token使用情况上传成功: {data}")
                    return True
                except:
                    self._log(f"Token使用情况上传成功，但响应不是有效的JSON: {response.text}")
                    return True
            else:
                self._log(f"上传token使用情况失败，状态码: {response.status_code}")
                return False
                
        except Exception as e:
            self._log(f"上传token使用情况出错: {str(e)}")
            return False 