import json
import requests
import os
from datetime import datetime

class AIConfig:
    """AI配置管理类"""
    
    def __init__(self):
        self.config_url = "https://goodhr.58it.cn/aiConfig.json"
        self.local_config_path = "ai_config.json"
        self.config_data = None
        
    async def load_config(self, log_callback=None):
        """
        从服务器加载AI配置
        
        Args:
            log_callback: 日志回调函数
            
        Returns:
            dict: AI配置数据
        """
        try:
            # 尝试从服务器获取最新配置
            response = requests.get(self.config_url, timeout=10)
            
            if response.status_code == 200:
                self.config_data = response.json()
                
                # 保存到本地文件
                with open(self.local_config_path, 'w', encoding='utf-8') as f:
                    json.dump(self.config_data, f, ensure_ascii=False, indent=4)
                
                if log_callback:
                    log_callback("已从服务器获取最新AI配置")
                    
            else:
                if log_callback:
                    log_callback(f"从服务器获取AI配置失败: {response.status_code}")
                # 尝试加载本地配置
                self.load_local_config(log_callback)
                
        except Exception as e:
            if log_callback:
                log_callback(f"获取AI配置出错: {str(e)}")
            # 发生错误时加载本地配置
            self.load_local_config(log_callback)
            
        return self.config_data
    
    def load_local_config(self, log_callback=None):
        """加载本地配置文件"""
        try:
            if os.path.exists(self.local_config_path):
                with open(self.local_config_path, 'r', encoding='utf-8') as f:
                    self.config_data = json.load(f)
                if log_callback:
                    log_callback("已加载本地AI配置")
            else:
                self.config_data = self.get_default_config()
                if log_callback:
                    log_callback("使用默认AI配置")
        except Exception as e:
            if log_callback:
                log_callback(f"加载本地AI配置失败: {str(e)}")
            self.config_data = self.get_default_config()
    
    def get_default_config(self):
        """获取默认配置"""
        return {
            "api_key": "",
            "api_url": "https://api.deepseek.com/v1/chat/completions",
            "model": "deepseek-chat",
            "temperature": 0.1,
            "max_tokens": 10,
            "prompt_template": """请分析以下候选人简历是否符合岗位要求。

岗位要求：
{job_description}

候选人简历：
{candidate_text}

请仅回复"是"或"否"，表示是否建议与该候选人打招呼。""",
            "enterprise": {
                "total_quota": 0,  # 总分析次数配额
                "used_quota": 0,   # 已使用的分析次数
                "expiry_date": "", # 配额过期时间
                "last_reset_date": "", # 上次重置时间
                "total_tokens_used": 0 # 累计使用的token数量
            }
        }
    
    def get_config(self):
        """获取当前配置"""
        return self.config_data if self.config_data else self.get_default_config()
    
    def get_api_key(self):
        """获取API密钥"""
        return self.get_config().get("api_key", "")
    
    def get_api_url(self):
        """获取API地址"""
        return self.get_config().get("api_url", "")
    
    def get_model(self):
        """获取模型名称"""
        return self.get_config().get("model", "deepseek-chat")
    
    def get_temperature(self):
        """获取温度参数"""
        return self.get_config().get("temperature", 0.1)
    
    def get_max_tokens(self):
        """获取最大token数"""
        return self.get_config().get("max_tokens", 10)
    
    def get_prompt_template(self):
        """获取提示词模板"""
        return self.get_config().get("prompt_template", "")
        
    def get_enterprise_info(self):
        """获取企业版配额信息"""
        return self.get_config().get("enterprise", {})
        
    def get_remaining_quota(self):
        """获取剩余分析次数"""
        try:
            # 直接从config.json获取系统配额
            if os.path.exists("config.json"):
                with open("config.json", "r", encoding="utf-8") as f:
                    config_data = json.load(f)
                
                # 获取企业版配额
                enterprise_data = config_data.get("versions", {}).get("enterprise", {})
                remaining_quota = enterprise_data.get("remainingQuota", 0)
                return remaining_quota
            
            # 如果无法从系统配置获取，则使用AI配置中的配额
            enterprise = self.get_enterprise_info()
            total = enterprise.get("total_quota", 0)
            used = enterprise.get("used_quota", 0)
            return max(0, total - used)
        except Exception as e:
            print(f"获取剩余配额出错: {str(e)}")
            return 0
        
    def increment_used_quota(self, log_callback=None, tokens_used=0):
        """
        增加已使用的分析次数
        Args:
            log_callback: 日志回调函数
            tokens_used: 使用的token数量，默认为0
        """
        try:
            if not self.config_data:
                return False
                
            # 更新AI配置中的使用次数
            enterprise = self.config_data.get("enterprise", {})
            enterprise["used_quota"] = enterprise.get("used_quota", 0) + 1
            
            # 记录token使用情况
            if tokens_used > 0:
                enterprise["total_tokens_used"] = enterprise.get("total_tokens_used", 0) + tokens_used
                if log_callback:
                    log_callback(f"AI配置中已记录token使用量: {tokens_used}，累计: {enterprise['total_tokens_used']}")
            
            self.config_data["enterprise"] = enterprise
            
            # 保存到本地文件
            with open(self.local_config_path, 'w', encoding='utf-8') as f:
                json.dump(self.config_data, f, ensure_ascii=False, indent=4)
            
            # 同时更新系统配置文件中的企业版配额
            try:
                if os.path.exists("config.json"):
                    with open("config.json", "r", encoding="utf-8") as f:
                        config_data = json.load(f)
                    
                    # 减少系统配置中的企业版配额
                    if "versions" in config_data and "enterprise" in config_data["versions"]:
                        enterprise_version = config_data["versions"]["enterprise"]
                        remaining = enterprise_version.get("remainingQuota", 0)
                        
                        if remaining > 0:
                            # 减少配额
                            enterprise_version["remainingQuota"] = remaining - 1
                            
                            # 记录token使用情况
                            if tokens_used > 0:
                                enterprise_version["tokens_used"] = enterprise_version.get("tokens_used", 0) + tokens_used
                            
                            config_data["versions"]["enterprise"] = enterprise_version
                            
                            # 保存更新后的系统配置
                            with open("config.json", "w", encoding="utf-8") as f:
                                json.dump(config_data, f, ensure_ascii=False, indent=4)
                                
                            if log_callback:
                                log_callback(f"已从系统配额中扣除1次，剩余 {remaining - 1} 次")
                                
                                if tokens_used > 0:
                                    log_callback(f"系统配置已记录token使用量: {tokens_used}，累计: {enterprise_version.get('tokens_used', 0)}")
                    
                    # 注：token上传由AIAnalyzer处理，这里不再重复上传
            except Exception as e:
                if log_callback:
                    log_callback(f"更新系统配额失败: {str(e)}")
            
            # 获取更新后的剩余配额
            remaining = self.get_remaining_quota()
            if log_callback:
                log_callback(f"已使用1次分析配额，剩余 {remaining} 次")
                
            return True
            
        except Exception as e:
            if log_callback:
                log_callback(f"更新使用配额失败: {str(e)}")
            return False
            
    def check_quota_valid(self, log_callback=None):
        """检查配额是否有效（企业版只检查剩余配额）"""
        try:
            # 获取剩余配额（已修改为直接从config.json获取）
            remaining_quota = self.get_remaining_quota()
            
            # 检查是否有剩余次数
            if remaining_quota <= 0:
                if log_callback:
                    log_callback("企业版配额已用完，请联系客服充值")
                return False
            
            if log_callback:
                log_callback(f"企业版配额检查通过，剩余 {remaining_quota} 次")
            return True
            
        except Exception as e:
            if log_callback:
                log_callback(f"检查配额有效性失败: {str(e)}")
            return False 