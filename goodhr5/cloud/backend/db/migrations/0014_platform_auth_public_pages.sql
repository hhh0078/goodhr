-- 将平台配置统一为 auth/public 页面分组结构，前端和任务流程均强制使用该格式。

UPDATE system_configs
SET config_value = (config_value - 'pages')
  || '{
    "auth": {
      "pages": [
        {
          "title": "登录后通用路由",
          "url": "https://www.zhipin.com/web/chat",
          "match": "contains"
        },
        {
          "title": "推荐牛人",
          "url": "https://www.zhipin.com/web/chat/recommend",
          "match": "prefix",
          "entry": true
        }
      ]
    },
    "public": {
      "pages": [
        {
          "title": "Boss登录页",
          "url": "https://login.zhipin.com",
          "match": "prefix"
        },
        {
          "title": "Boss用户登录页",
          "url": "https://www.zhipin.com/web/user/",
          "match": "prefix"
        }
      ]
    }
  }'::jsonb
WHERE config_key = 'platform.boss'
  AND enabled = true;

UPDATE system_configs
SET config_value = (config_value - 'pages')
  || '{
    "auth": {
      "pages": [
        {
          "title": "智联推荐页",
          "url": "https://rd6.zhaopin.com/app/recommend",
          "match": "prefix",
          "entry": true
        }
      ]
    },
    "public": {
      "pages": [
        {
          "title": "智联登录页",
          "url": "https://passport.zhaopin.com",
          "match": "prefix"
        },
        {
          "title": "智联备用登录页",
          "url": "https://login.zhaopin.com",
          "match": "prefix"
        }
      ]
    }
  }'::jsonb
WHERE config_key = 'platform.zhaopin'
  AND enabled = true;
