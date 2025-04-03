import undetected_chromedriver as uc
from selenium.webdriver.common.by import By
from selenium.webdriver.support.ui import WebDriverWait
from selenium.webdriver.support import expected_conditions as EC
from selenium import webdriver

def get_locator(class_name):
    if ' ' in class_name:
        css_selector = '.' + '.'.join(class_name.split())
        return By.CSS_SELECTOR, css_selector
    else:
        return By.CLASS_NAME, class_name

def main():
    # 使用undetected_chromedriver代替默认的webdriver
    options = uc.ChromeOptions()
    # 添加一些反检测的选项
    options.add_argument("--disable-blink-features=AutomationControlled")
    options.add_argument("--disable-dev-shm-usage")
    options.add_argument("--no-sandbox")
    options.add_argument("--disable-gpu")
    options.add_argument("--disable-extensions")
    
    try:
        driver = uc.Chrome(options=options)
    except SessionNotCreatedException as e:
        if 'This version of ChromeDriver only supports Chrome version' in str(e):
            print('检测到浏览器版本不兼容，请更新浏览器后重启程序')
            print('建议更新到最新版本Chrome浏览器')
            sys.exit(1)
        else:
            raise e
    
    # 修改webdriver属性
    driver.execute_cdp_cmd("Page.addScriptToEvaluateOnNewDocument", {
        "source": """
        Object.defineProperty(navigator, 'webdriver', {
            get: () => undefined
        })
        """
    })
    
    
    
    while True:
        user_input = input('请输入网址或class名称（输入q退出）：')
        if user_input.lower() == 'q':
            break
            
        if user_input.startswith('http'):
            driver.get(user_input)
            print(f'已打开网址：{user_input}')
        else:
            try:
                by, selector = get_locator(user_input)
                wait = WebDriverWait(driver, 2)
                elements = wait.until(EC.presence_of_all_elements_located((by, selector)))
                
                if len(elements) == 1:
                    elements[0].click()
                    print(f'已点击元素：{user_input}')
                elif len(elements) > 1:
                    print(f'找到{len(elements)}个匹配元素：')
                    for i, element in enumerate(elements):
                        print(f'{i+1}. {element.text}')
                    choice = input('请输入要点击的元素的编号：')
                    if choice.isdigit() and 1 <= int(choice) <= len(elements):
                        elements[int(choice)-1].click()
                        print(f'已点击第{choice}个元素')
                    else:
                        print('输入无效，跳过点击')
                else:
                    print('未找到匹配元素')
            except Exception as e:
                print(f'操作出错：{str(e)}')
    
    driver.quit()

if __name__ == '__main__':
    main()