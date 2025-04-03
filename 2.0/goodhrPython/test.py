import undetected_chromedriver as uc
from selenium.webdriver.common.by import By
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC

def get_locator(class_name):
    """
    根据 class 名称生成定位器
    :param class_name: 元素的 class 名称
    :return: (By, selector)
    """
    if ' ' in class_name:
        # 如果是复合 class，生成 CSS 选择器
        css_selector = '.' + '.'.join(class_name.split())
        return By.CSS_SELECTOR, css_selector
    else:
        # 如果是单个 class，直接使用 By.CLASS_NAME
        return By.CLASS_NAME, class_name

# 启动浏览器（使用 undetected_chromedriver）
driver = uc.Chrome()

# 打开目标网页
driver.get('https://www.baidu.com')

try:
    # 指定 class 名称
    class_name = 's-top-login-btn c-btn c-btn-primary c-btn-mini lb btn-fixed'

    # 获取定位器
    by, selector = get_locator(class_name)

    # 等待元素加载
    wait = WebDriverWait(driver, 2)
    elements = wait.until(EC.presence_of_all_elements_located((by, selector)))

    # 获取并打印每个元素的内容
    for index, element in enumerate(elements):
        print(f'元素 {index + 1} 的内容: {element.text}')

    # 如果需要获取特定元素的内容，可以通过索引访问
    if len(elements) > 0:
        specific_element = elements[0]  # 获取第一个元素
        print(f'第一个元素的内容: {specific_element.text}')

except Exception as e:
    print(f'发生错误: {e}')

finally:
    # 不关闭浏览器
    print('完成: 浏览器保持打开状态')
    input('按 Enter 键退出...')  # 暂停程序，等待用户输入