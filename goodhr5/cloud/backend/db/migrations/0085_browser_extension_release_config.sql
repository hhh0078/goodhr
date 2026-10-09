-- 本迁移新增浏览器插件公开下载配置：Chrome 与 Firefox 共用版本号，下载地址由超级管理员维护；不修改本地程序版本。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES ('system.browser_extension', '{"version":"1.0.1","note":"补齐可见头像保存与完整简历入口；Chrome 与 Firefox 共用版本号。当前为开发安装包。","url_chrome":"/downloads/goodhr-extension-chrome-1.0.1.zip","url_firefox":"/downloads/goodhr-extension-firefox-1.0.1.zip","sha256_chrome":"855118f185cfa5e8aa3f45a1de4e663fb50deef496da31134709ebe6684fefb8","sha256_firefox":"a1d1eb60f68264c029f5821884e0d2ec36b0d2221a824f55c5fc148bc9d497f5","firefox_signed":false}', '浏览器插件统一版本、Chrome/Firefox 下载地址、校验值和更新说明', true)
ON CONFLICT (config_key) DO NOTHING;
