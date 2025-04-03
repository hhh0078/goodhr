from utils import Utils
import logging
import json
import random
import os
from datetime import datetime

class CandidateHandler:
    """处理候选人的类"""
    
    def __init__(self, page, log_callback, config_data, play_sound_callback=None, update_greet_count_callback=None):
        self.page = page
        self.log_callback = log_callback
        self.config_data = config_data  # 保留这个引用，但在关键方法中会重新加载
        self.play_sound_callback = play_sound_callback
        self.update_greet_count_callback = update_greet_count_callback  # 添加更新打招呼计数的回调函数
        self.greet_count = 0  # 打招呼计数器
        self.max_greet_count = 0  # 最大打招呼数量限制
        self.on_max_greet_reached = None  # 达到最大打招呼数量时的回调函数

    def _load_latest_config(self):
        """从配置文件中加载最新的配置信息"""
        try:
            if os.path.exists("config.json"):
                with open("config.json", "r", encoding="utf-8") as f:
                    latest_config = json.load(f)
                return latest_config
            else:
                self.log_callback("配置文件不存在，使用当前配置")
                return self.config_data
        except Exception as e:
            self.log_callback(f"加载配置文件失败: {str(e)}，使用当前配置")
            return self.config_data

    async def handle_candidate(self, candidate, candidate_config, dialog_config, index):
        """
        处理单个候选人的主流程
        Args:
            candidate: 候选人元素
            candidate_config: 候选人配置
            dialog_config: 对话框配置
            index: 候选人索引
        """
        try:
            # 1. 检查候选人列表元素是否在可见区域
            if not await self._check_candidate_visibility(candidate, index):
                self.log_callback(f"第 {index} 个候选人不在可见区域，跳过处理")
                return

            # 2. 点击候选人列表元素并检查弹框是否打开
            max_retries = 3
            for retry in range(max_retries):
                if not await self._click_candidate(candidate, index):
                    self.log_callback(f"点击第 {index} 个候选人失败，第 {retry + 1} 次重试")
                    continue
                self.log_callback(f"标记5，候选人点击成功")
                # 检查弹框是否成功打开
                dialog_selector = dialog_config.get("selector", "")
                selector_type = dialog_config.get("selector_type", "class")

                result = await Utils.wait_for_element_state(self.page, dialog_selector, selector_type, True)
                if result:
                    self.log_callback(f"标记6，弹框打开成功")
                    break
                else:
                    self.log_callback(f"标记7，弹框打开失败")
                    return

                
            # 3. 获取候选人弹框中的文本内容
            candidate_info = await self._get_candidate_info(dialog_config)
            if not candidate_info:
                self.log_callback(f"获取第 {index} 个候选人信息失败，跳过处理")
                await self._close_dialog_with_retry(dialog_config, index)
                return

            # 4. 通过API分析候选人
            should_greet = await self._analyze_candidate(candidate_info)
            if not should_greet:
                self.log_callback(f"第 {index} 个候选人不符合条件，不打招呼")
                await self._close_dialog_with_retry(dialog_config, index)
                return

            # 5. 点击打招呼按钮
            if await self._greet_candidate(dialog_config):
                self.log_callback(f"已成功向第 {index} 个候选人打招呼")
                if dialog_config['dialog_elements']['greet_ok_is_close']:
                    await self._close_dialog_with_retry(dialog_config, index)
                return

        except Exception as e:
            self.log_callback(f"处理第 {index} 个候选人时发生错误: {str(e)}")
            # 确保弹框被关闭
            try:
                await self._close_dialog_with_retry(dialog_config, index)
            except:
                pass

    async def _check_candidate_visibility(self, candidate, index):
        """
        检查候选人元素是否在可见区域
        Args:
            candidate: 候选人元素对象

        Returns:
            bool: 是否在可见区域内
        """
        try:
            is_visible, position_info = await Utils.is_element_in_viewport(self.page, candidate,20,500,index)
            
            if not is_visible:
                # 保留错误信息，但不输出详细的位置信息
                self.log_callback(f"第 {index} 个候选人不在可视区域内")
                return False
                
            return True
            
        except Exception as e:
            self.log_callback(f"检查第 {index} 个候选人可见性时出错: {str(e)}")
            return False

    async def _click_candidate(self, candidate, index):
        """
        点击候选人元素
        Args:
            candidate: 候选人元素
            index: 候选人索引
        
        Returns:
            bool: 是否成功点击
        """
        try:
            self.log_callback(f"标记1")
            # 获取最新配置
            latest_config = self._load_latest_config()
            self.log_callback(f"标记2")
            # 使用通用点击方法点击候选人
            result = await Utils.click_element(self.page, candidate, f"第 {index} 个候选人")
            self.log_callback(f"标记3")
            if result:
                # 获取延迟时间设置
                min_delay = latest_config.get("minDelay", 7)
                max_delay = latest_config.get("maxDelay", 12)
                
                # 如果配置中有delay字段，优先使用
                if "delay" in latest_config:
                    min_delay = latest_config["delay"].get("min", min_delay)
                    max_delay = latest_config["delay"].get("max", max_delay)
                
                # 确保延迟时间在有效范围内
                min_delay = max(5, min(20, min_delay))
                max_delay = max(5, min(20, max_delay))
                if min_delay > max_delay:
                    min_delay, max_delay = max_delay, min_delay
                
                # 生成随机延迟时间
                delay_time = random.uniform(min_delay, max_delay)
                
                self.log_callback(f"正在查看第 {index} 个候选人简历，等待 {1:.1f} 秒...")
                
                # 等待随机延迟时间（转换为毫秒）
                await self.page.waitFor(delay_time * 1000)
                self.log_callback(f"标记4")
                return True
            else:
                self.log_callback(f"点击第 {index} 个候选人失败")
                return False
                
        except Exception as e:
            self.log_callback(f"点击第 {index} 个候选人时出错: {str(e)}")
            return False

    async def _get_candidate_info(self, dialog_config):
        """
        获取候选人弹框中的信息
        Args:
            dialog_config: 弹框配置信息
            
        Returns:
            dict: 候选人信息，如果获取失败则返回None
        """
        try:
            # 等待弹框加载完成 - 增加等待时间确保弹框完全加载
            await self.page.waitFor(1000)  # 等待1秒，确保弹框完全加载
            
            # 获取弹框元素
            dialog_selector = dialog_config.get("selector", "")
            selector_type = dialog_config.get("selector_type", "class")
            
            # 使用 Utils.get_element_by_css_selector 获取弹框元素
            elements = await Utils.get_element_by_css_selector(
                self.page, 
                dialog_selector, 
                selector_type,
                timeout=5000
            )
            
            if not elements or len(elements) == 0:
                self.log_callback("未找到候选人弹框元素")
                return None
            
            # 获取最后一个元素（假设最后打开的弹框是最后一个元素）
            dialog_element = elements[len(elements) - 1]
            
            # 获取弹框中的文本内容
            dialog_text = await Utils.get_element_text(self.page, dialog_element)
            
            if not dialog_text:
                self.log_callback("弹框中没有文本内容")
                return None
                
            # 只记录简历文本的长度，不输出全部内容
            self.log_callback(f"获取到候选人简历: {dialog_text}")
            
            # 返回原始文本内容
            return {
                'raw_text': dialog_text
            }
            
        except Exception as e:
            self.log_callback(f"获取候选人信息时出错: {str(e)}")
            return None

    async def _analyze_candidate(self, candidate_info):
        """分析候选人信息"""
        try:
            # 每次分析时从配置文件中获取最新的配置
            latest_config = self._load_latest_config()
            
            # 使用最新的配置
            version = latest_config.get('version', 'free')
            
            # 如果是免费版，检查日期并重置计数
            if version == 'free':
                today = datetime.now().date().strftime("%Y-%m-%d")
                version_data = latest_config.get('versions', {}).get('free', {})
                last_reset_date = version_data.get('lastResetDate', '')
                
                # 如果日期不同，重置计数
                if last_reset_date != today:
                    self.log_callback("检测到日期变更，重置免费版打招呼计数")
                    version_data.update({
                        'greetCount': 0,
                        'remainingQuota': 100,
                        'lastResetDate': today,
                        'todayCount': 0
                    })
                    latest_config['versions']['free'] = version_data
                    
                    # 保存配置
                    try:
                        with open("config.json", "w", encoding="utf-8") as f:
                            json.dump(latest_config, f, ensure_ascii=False, indent=4)
                        self.log_callback("已重置免费版配置")
                    except Exception as e:
                        self.log_callback(f"保存配置文件失败: {str(e)}")
            
            # 获取当前岗位
            current_job = latest_config.get('selected_job', '')
            if not current_job:
                current_job = latest_config.get('selectedJob', '')
            
            # 企业版使用AI分析
            if version == 'enterprise':
                try:
                    from ai_analyzer import AIAnalyzer
                    
                    # 获取岗位描述
                    job_description = latest_config.get('keywordsData', {}).get(current_job, {}).get('description', '')
                    if not job_description:
                        self.log_callback("未找到岗位描述，请先填写岗位描述")
                        # 停止运行
                        if hasattr(self, 'on_max_greet_reached') and callable(self.on_max_greet_reached):
                            self.on_max_greet_reached()
                        return False
                    
                    # 创建AI分析器实例并初始化
                    analyzer = AIAnalyzer(self.log_callback)
                    await analyzer.initialize()
                    
                    # 检查企业版配额
                    if not analyzer.config.check_quota_valid(self.log_callback):
                        self.log_callback("企业版配额不足，无法使用AI分析功能")
                        # 通知外部需要停止
                        if hasattr(self, 'on_max_greet_reached') and callable(self.on_max_greet_reached):
                            self.on_max_greet_reached()
                        return False
                    
                    # 分析候选人
                    self.log_callback("正在使用AI分析候选人...")
                    result, token_usage = analyzer.analyze_candidate(candidate_info['raw_text'], job_description)
                    
                    # 保存token使用情况
                    total_tokens = token_usage.get('total_tokens', 0)
                    self.log_callback(f"本次AI分析消耗了 {total_tokens} 个tokens")
                    
                    # 无论是否打招呼，都更新计数并传递token消耗数据
                    if hasattr(self, 'update_greet_count_callback') and callable(self.update_greet_count_callback) and total_tokens > 0:
                        try:
                            # 注意：这里只传递token数据，不增加打招呼次数
                            self.update_greet_count_callback(0, total_tokens)
                            self.log_callback("已更新token使用情况")
                        except Exception as e:
                            self.log_callback(f"更新token使用情况失败: {str(e)}")
                    
                    # 分析后再次检查配额，如果已用完则通知停止
                    if analyzer.config.get_remaining_quota() <= 0:
                        self.log_callback("企业版配额已用完，自动停止")
                        if hasattr(self, 'on_max_greet_reached') and callable(self.on_max_greet_reached):
                            self.on_max_greet_reached()
                    
                    return result
                    
                except Exception as e:
                    self.log_callback(f"AI分析出错: {str(e)}")
                    return False
            
            # 免费版和捐赠版使用关键词匹配
            else:
                # 获取关键词数据
                keywords_data = latest_config.get('keywordsData', {})
                keywords = keywords_data.get(current_job, {})
                
                # 记录分析过程
                self.log_callback(f"正在分析候选人是否符合【{current_job}】岗位要求...")
                self.log_callback(f"当前使用的关键词配置: {keywords}")
                
                return self._analyze_with_keywords(candidate_info, keywords)
                
        except Exception as e:
            self.log_callback(f"分析候选人信息时出错: {str(e)}")
            return False

    def _analyze_with_keywords(self, candidate_info, keywords):
        """使用关键词匹配分析候选人"""
        try:
            # 获取候选人信息文本
            if not candidate_info or 'raw_text' not in candidate_info:
                self.log_callback("候选人信息不完整，无法分析")
                return False
                
            candidate_text = candidate_info['raw_text']
            
            # 如果没有关键词数据，默认不匹配
            if not keywords or not isinstance(keywords, dict):
                self.log_callback("没有有效的关键词数据，默认不匹配")
                return False
            
            # 获取包含关键词、排除关键词和关系
            include_keywords = keywords.get('include', [])
            exclude_keywords = keywords.get('exclude', [])
            relation = keywords.get('relation', 'OR')
            
            # 显示当前正在使用的关键词配置
            self.log_callback(f"当前使用的包含关键词: {', '.join(include_keywords)}")
            self.log_callback(f"当前使用的排除关键词: {', '.join(exclude_keywords)}")
            self.log_callback(f"关键词匹配关系: {relation}")
            
            # 检查排除关键词
            for keyword in exclude_keywords:
                if keyword and keyword.lower() in candidate_text.lower():
                    self.log_callback(f"发现排除关键词: {keyword}，不符合要求")
                    return False
            
            # 如果没有包含关键词，默认匹配
            if not include_keywords:
                self.log_callback("没有设置包含关键词，默认符合要求")
                return True
            
            # 根据关系检查包含关键词
            if relation == 'AND':
                # 全部匹配
                match_all = True
                matched_keywords = []
                unmatched_keywords = []
                
                for keyword in include_keywords:
                    if not keyword or keyword.lower() not in candidate_text.lower():
                        match_all = False
                        unmatched_keywords.append(keyword)
                    else:
                        matched_keywords.append(keyword)
                
                if match_all:
                    self.log_callback(f"关键词分析: 匹配所有关键词 {', '.join(matched_keywords)}，符合要求")
                    return True
                else:
                    self.log_callback(f"关键词分析: 未匹配关键词 {', '.join(unmatched_keywords)}，不符合要求")
                    return False
            else:
                # 任一匹配
                for keyword in include_keywords:
                    if keyword and keyword.lower() in candidate_text.lower():
                        self.log_callback(f"关键词分析: 匹配关键词 {keyword}，符合要求")
                        return True
                
                self.log_callback(f"关键词分析: 未匹配任何关键词 {', '.join(include_keywords)}，不符合要求")
                return False
        except Exception as e:
            self.log_callback(f"关键词分析出错: {str(e)}")
            return False

    async def _greet_candidate(self, dialog_config):
        """
        点击打招呼按钮
        Args:
            dialog_config: 弹框配置信息
            
        Returns:
            bool: 是否成功打招呼
        """
        try:
            # 获取最新配置
            latest_config = self._load_latest_config()
            
            # 获取打招呼按钮
            greet_button_selector =  dialog_config['dialog_elements']['greet_button']['selector']
            selector_type = dialog_config['dialog_elements']['greet_button']['selector_type']
            
            if not greet_button_selector:
                self.log_callback("未配置打招呼按钮选择器")
                return False
                
            greet_buttons = await Utils.get_element_by_css_selector(
                self.page, 
                greet_button_selector, 
                selector_type,
                timeout=3000
            )
            
            if not greet_buttons or len(greet_buttons) == 0:
                self.log_callback("未找到打招呼按钮")
                return False
                
            # 使用通用点击方法点击打招呼按钮
            result = await Utils.click_element(self.page, greet_buttons[0], "打招呼按钮")
            
            if result:
                # 等待操作完成
                await self.page.waitFor(1000)
                self.greet_count += 1  # 增加打招呼计数
                self.log_callback(f"打招呼成功，今日已打招呼 {self.greet_count} 次")
                
                # 更新配置文件中的打招呼计数
                if self.update_greet_count_callback:
                    try:
                        self.update_greet_count_callback(1)
                        self.log_callback("已更新UI界面的打招呼计数")
                    except Exception as e:
                        self.log_callback(f"更新UI界面打招呼计数失败: {str(e)}")
                
                # 播放打招呼成功提示音
                if self.play_sound_callback:
                    self.play_sound_callback("notification.mp3")
                
                # 检查是否达到打招呼数量限制
                max_greet_count = latest_config.get("max_greet_count", 0)
                if max_greet_count > 0 and self.greet_count >= max_greet_count:
                    self.log_callback(f"已达到设定的打招呼数量限制 ({max_greet_count} 次)，将自动停止")
                    # 通知外部已达到限制
                    if hasattr(self, 'on_max_greet_reached') and callable(self.on_max_greet_reached):
                        self.on_max_greet_reached()
                
                return True
            else:
                self.log_callback("点击打招呼按钮失败")
                return False
            
        except Exception as e:
            self.log_callback(f"点击打招呼按钮时出错: {str(e)}")
            return False

    async def _close_dialog(self, dialog_config):
        """
        关闭弹框
        Args:
            dialog_config: 弹框配置信息
            
        Returns:
            bool: 是否成功关闭弹框
        """
        try:
            # 获取关闭按钮
            close_button_selector = dialog_config['dialog_elements']['close_button']['selector']
            selector_type =dialog_config['dialog_elements']['close_button']['selector_type']
            
            if not close_button_selector:
                self.log_callback("未配置关闭按钮选择器")
                return False
                
            close_buttons = await Utils.get_element_by_css_selector(
                self.page, 
                close_button_selector, 
                selector_type,
                timeout=3000
            )
            
            if not close_buttons or len(close_buttons) == 0:
                self.log_callback("未找到关闭按钮")
                return False
                
            # 使用新的通用点击方法点击关闭按钮
            result = await Utils.click_element(self.page, close_buttons[0], "关闭按钮")
            
            if not result:
                self.log_callback("点击关闭按钮失败")
                return False
                
            return True
            
        except Exception as e:
            self.log_callback(f"关闭弹框时出错: {str(e)}")
            return False

    async def _close_dialog_with_retry(self, dialog_config, index):
        """
        尝试关闭弹框并验证是否成功关闭
        Args:
            dialog_config: 弹框配置
            index: 候选人索引
        """
        max_retries = 3
        for retry in range(max_retries):
            # 尝试关闭弹框
            if not await self._close_dialog(dialog_config):
                self.log_callback(f"关闭第 {index} 个候选人弹框失败，第 {retry + 1} 次重试")
                continue

            # 等待一小段时间确保弹框关闭动画完成
            await self.page.waitFor(500)

            # 检查弹框是否还存在
            dialog_selector = dialog_config.get("selector", "")
            selector_type = dialog_config.get("selector_type", "class")
            dialog_elements = await Utils.get_element_by_css_selector(
                self.page,
                dialog_selector,
                selector_type,
                timeout=1000
            )

            if not dialog_elements or len(dialog_elements) == 0:
                return True  # 弹框已成功关闭
            
            if retry == max_retries - 1:
                self.log_callback(f"关闭第 {index} 个候选人弹框失败，已重试 {max_retries} 次")
                # 播放被动结束提示音
                # if self.play_sound_callback:
                #     self.play_sound_callback("63c0e6c0aa6ec422.mp3")
                    
                # 通知外部需要停止
                if hasattr(self, 'on_max_greet_reached') and callable(self.on_max_greet_reached):
                    self.on_max_greet_reached()
                return False

            self.log_callback(f"弹框未完全关闭，第 {retry + 1} 次重试")
            await self.page.waitFor(500)  # 等待1秒后重试

        return False 