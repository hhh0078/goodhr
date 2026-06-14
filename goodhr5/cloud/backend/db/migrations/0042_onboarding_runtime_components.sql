-- 本迁移为新手教学配置增加本地运行组件下载配置，替代独立 goodhr-local-runtime-manifest.json 清单文件。
UPDATE system_configs
SET
    config_value = jsonb_set(
        config_value,
        '{runtime_components}',
        COALESCE(
            config_value -> 'runtime_components',
            '{
              "node_runtime": {
                "win": {
                  "version": "22.19.0",
                  "url": "https://oss.58it.cn/goodhr-node-runtime-win-x64.zip",
                  "sha256": "ea3fad0e67a991d8477d8c01344b56e69c676ccb733f065b22436994b1253f86",
                  "note": "GoodHR Node 运行环境 Windows x64"
                },
                "mac": {
                  "version": "22.19.0",
                  "url": "https://oss.58it.cn/goodhr-node-runtime-darwin-arm64.tar.gz",
                  "sha256": "c59006db713c770d6ec63ae16cb3edc11f49ee093b5c415d667bb4f436c6526d",
                  "note": "GoodHR Node 运行环境 macOS Apple Silicon"
                }
              },
              "node_worker": {
                "win": {
                  "version": "0.1.0",
                  "url": "https://oss.58it.cn/goodhr-browser-worker-win-x64-0.1.0.zip",
                  "sha256": "1d84febad716db2c41f60e273aa89a116f9b62f92a39f7e3a08f42551b188c5e",
                  "note": "GoodHR 浏览器控制 Worker Windows x64"
                },
                "mac": {
                  "version": "0.1.0",
                  "url": "https://oss.58it.cn/goodhr-browser-worker-darwin-arm64-0.1.0.zip",
                  "sha256": "1d84febad716db2c41f60e273aa89a116f9b62f92a39f7e3a08f42551b188c5e",
                  "note": "GoodHR 浏览器控制 Worker macOS Apple Silicon"
                }
              },
              "cloakbrowser": {
                "win": {
                  "version": "146.0.7680.177.5",
                  "url": "https://oss.58it.cn/cloakbrowser-windows-x64.zip",
                  "sha256": "",
                  "note": "CloakBrowser Windows x64"
                },
                "mac": {
                  "version": "145.0.7632.109.2",
                  "url": "https://oss.58it.cn/cloakbrowser-darwin-arm64.tar.gz",
                  "sha256": "505582aa1bd3971c577f70e0cbbe016431702bdb693529abfd943b5bd9120c1c",
                  "note": "CloakBrowser macOS Apple Silicon"
                }
              },
              "ocr": {
                "win": {
                  "version": "rapidocr-json-2.0.0",
                  "url": "https://oss.58it.cn/goodhr-ocr-win-x64.zip",
                  "sha256": "4209f60feb4248376c56b8b9924d7c21aaf91de5058c6daddccc6bd1e0a025f3",
                  "note": "RapidOCR JSON Windows x64"
                },
                "mac": {
                  "version": "",
                  "url": "",
                  "sha256": "",
                  "note": "macOS OCR 组件待上传"
                }
              }
            }'::jsonb
        ),
        true
    ),
    description = '新手教学配置，包含本地程序下载链接、运行组件下载链接、版本号、版本说明和注册赠送会员天数'
WHERE config_key = 'system.onboarding_config';
