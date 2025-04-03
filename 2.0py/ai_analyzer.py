import json
import requests
from datetime import datetime
from ai_config import AIConfig
import os
from api_client import ApiClient

# 创建API客户端实例
api_client = ApiClient("https://goodhr.58it.cn/api/service/api/")

class AIAnalyzer:
    """AI分析器类"""
    
    def __init__(self, log_callback=None):
        self.config = AIConfig()
        self.log_callback = log_callback
        
    async def initialize(self):
        """初始化AI分析器，加载配置"""
        await self.config.load_config(self.log_callback)
        
    def analyze_candidate(self, candidate_text, job_description):
        """
        使用 DeepSeek API 分析候选人是否符合岗位要求
        
        Args:
            candidate_text (str): 候选人简历文本
            job_description (str): 岗位描述文本
            
        Returns:
            tuple: (是否应该打招呼, token使用量字典)
        """
        try:
            # 检查配额是否有效
            if not self.config.check_quota_valid(self.log_callback):
                return False, {"total_tokens": 0}
                
            # 获取配置
            api_key = self.config.get_api_key()
            api_url = self.config.get_api_url()
            model = self.config.get_model()
            temperature = self.config.get_temperature()
            max_tokens = self.config.get_max_tokens()
            prompt_template = self.config.get_prompt_template()
            
            if not api_key:
                if self.log_callback:
                    self.log_callback("未配置API密钥，请先配置")
                return False, {"total_tokens": 0}
                
            # 构建提示词
            prompt = prompt_template.format(
                job_description=job_description,
                candidate_text=candidate_text
            )

            # 准备请求数据
            payload = {
                "model": model,
                "messages": [
                    {
                        "role": "system",
                        "content": "你是一个专业的HR，擅长分析候选人是否符合岗位要求。你只能回答是或者否，不要输出其他内容"
                    },
                    {
                        "role": "user",
                        "content": prompt
                    }
                ],
                "temperature": temperature,
                "max_tokens": max_tokens
            }

            # 准备请求头
            headers = {
                "Authorization": f"Bearer {api_key}",
                "Content-Type": "application/json"
            }

            # 发送请求
            response = requests.post(
                api_url,
                headers=headers,
                json=payload,
                timeout=30
            )
            self.log_callback(f"AI请求{api_url}，请求头{headers}，请求体{payload}")

            # 提取token使用情况数据
            token_usage = {"total_tokens": 0, "prompt_tokens": 0, "completion_tokens": 0}

            # 检查响应
            if response.status_code == 200:
                result = response.json()
                answer = result.get("choices", [{}])[0].get("message", {}).get("content", "").strip().lower()
                
                # 提取token使用情况
                usage_data = result.get("usage", {})
                token_usage = {
                    "total_tokens": usage_data.get("total_tokens", 0),
                    "prompt_tokens": usage_data.get("prompt_tokens", 0),
                    "completion_tokens": usage_data.get("completion_tokens", 0)
                }
                
                if self.log_callback:
                    self.log_callback(f"Token使用情况: 总计={token_usage['total_tokens']}, 提示={token_usage['prompt_tokens']}, 完成={token_usage['completion_tokens']}")
                
                # 无论分析结果如何，都增加使用次数和记录token用量
                self.config.increment_used_quota(self.log_callback, token_usage['total_tokens'])
                
                # 上传token使用情况到服务器 - 只在这里上传，避免重复
                if token_usage['total_tokens'] > 0:
                    self.upload_tokens_used(token_usage['total_tokens'])
                
                # 记录分析结果
                if self.log_callback:
                    self.log_callback(f"AI分析结果：{'建议打招呼' if answer == '是' else '不建议打招呼'}")
                
                # 判断回答并返回token使用情况
                return answer == "是", token_usage
            else:
                if self.log_callback:
                    self.log_callback(f"API请求失败: {response.status_code} - {response.text}")
                return False, token_usage  # API调用失败时返回False和空token使用情况

        except Exception as e:
            if self.log_callback:
                self.log_callback(f"分析候选人时出错: {str(e)}")
            return False, {"total_tokens": 0}  # 发生错误时返回False和空token使用情况
            
    def upload_tokens_used(self, tokens_used):
        """
        上传token使用情况到企业版专用接口
        
        Args:
            tokens_used (int): 使用的token数
        
        Returns:
            bool: 是否上传成功
        """
        try:
            if tokens_used <= 0:
                return False
            
            # 从config.json获取手机号
            phone = ""
            try:
                if os.path.exists("config.json"):
                    with open("config.json", "r", encoding="utf-8") as f:
                        config_data = json.load(f)
                    phone = config_data.get("username", "")
            except Exception as e:
                if self.log_callback:
                    self.log_callback(f"获取手机号失败: {str(e)}")
                return False
            
            if not phone:
                if self.log_callback:
                    self.log_callback("未找到手机号，无法上传token使用情况")
                return False
            
            if self.log_callback:
                self.log_callback(f"正在上传token使用情况: {tokens_used} tokens")
            
            # 使用api_client上传token使用情况
            result = api_client.upload_tokens_used(phone, tokens_used)
            
            if result:
                if self.log_callback:
                    self.log_callback("Token使用情况上传成功")
                return True
            else:
                if self.log_callback:
                    self.log_callback("Token使用情况上传失败")
                return False
                
        except Exception as e:
            if self.log_callback:
                self.log_callback(f"上传token使用情况出错: {str(e)}")
            return False

# 用于测试的示例代码
if __name__ == "__main__":
    # 测试数据
    test_job = """
    招聘Python开发工程师
    要求：
    1. 3年以上Python开发经验
    2. 熟悉Web开发框架如Django或Flask
    3. 有良好的代码风格和文档习惯
    """
    
    test_candidate = """
    工作经验：5年Python开发
    技术栈：Django, Flask, FastAPI
    项目经验：开发过多个企业级Web应用
    """
    
    async def test():
        analyzer = AIAnalyzer()
        await analyzer.initialize()
        result, token_usage = analyzer.analyze_candidate(test_candidate, test_job)
        print(f"是否建议打招呼: {result}")
        print(f"Token使用情况: {token_usage}")
        
    import asyncio
    asyncio.run(test()) 