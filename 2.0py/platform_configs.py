# 平台配置文件
# 包含所有招聘平台的配置信息和API配置
import requests
import json
import os
import time

# 默认配置，作为备用
DEFAULT_CONFIGS = {}

# 从服务器获取配置
def fetch_platform_configs():
    try:
        # 服务器配置URL
        config_url = "https://goodhr.58it.cn/config2.json"
        
        # 尝试从服务器获取配置
        response = requests.get(config_url, timeout=10)
        
        if response.status_code == 200:
            try:
                # 解析JSON响应
                server_configs = response.json()
                print("成功从服务器获取平台配置")
                
                # 保存配置到本地文件，以便离线使用
                try:
                    with open("platform_configs_cache.json", "w", encoding="utf-8") as f:
                        json.dump(server_configs, f, ensure_ascii=False, indent=4)
                    print("已将平台配置缓存到本地")
                except Exception as e:
                    print(f"缓存平台配置到本地失败: {str(e)}")
                
                return server_configs
            except json.JSONDecodeError as e:
                print(f"服务器返回的数据不是有效的JSON格式: {str(e)}")
                print(f"响应内容: {response.text[:200]}...")  # 只打印前200个字符，避免日志过长
        else:
            print(f"从服务器获取平台配置失败，状态码: {response.status_code}")
    except Exception as e:
        print(f"获取平台配置出错: {str(e)}")
    
    # 如果从服务器获取失败，尝试从本地缓存加载
    try:
        if os.path.exists("platform_configs_cache.json"):
            with open("platform_configs_cache.json", "r", encoding="utf-8") as f:
                cached_configs = json.load(f)
            print("从本地缓存加载平台配置")
            return cached_configs
    except Exception as e:
        print(f"从本地缓存加载平台配置失败: {str(e)}")
    
    # 如果所有尝试都失败，使用默认配置
    print("使用默认平台配置")
    return DEFAULT_CONFIGS

# 初始化平台配置
PLATFORM_CONFIGS = fetch_platform_configs()

# 提供刷新配置的函数，以便在需要时重新获取
def refresh_platform_configs():
    global PLATFORM_CONFIGS
    PLATFORM_CONFIGS = fetch_platform_configs()
    return PLATFORM_CONFIGS