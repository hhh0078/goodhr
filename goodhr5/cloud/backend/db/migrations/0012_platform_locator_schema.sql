-- 将平台配置更新为统一元素定位协议的新结构。

UPDATE system_configs
SET config_value = '{
  "id":"boss",
  "name":"Boss直聘",
  "domain":"zhipin.com",
  "pages":[{"url":"https://www.zhipin.com/web/chat/recommend","title":"推荐牛人"}],
  "card":{
    "scroll":{"target_classes":[[".card-list"]]},
    "item":{
      "parent_classes":[[".card-list"]],
      "target_classes":[[".candidate-card-wrap",".geek-info-card",".card-container",".card-inner.clear-fix",".card-inner.common-wrap"]]
    },
    "fields":[
      {"name":{"target_classes":[[".name"]]}},
      {"basic_info":{"target_classes":[[".job-card-left"]]}},
      {"education":{"target_classes":[[".base-info.join-text-wrap",".geek-info-detail"]]}},
      {"university":{"target_classes":[[".content.join-text-wrap"]]}},
      {"description":{"target_classes":[[".content"]]}}
    ]
  },
  "actions":{
    "greetBtn":{"target_classes":[[".btn.btn-greet",".btn.btn-getcontact"]]},
    "continueBtn":{"target_classes":[[".btn.btn-continue.btn-outline"]]},
    "phoneBtn":{"target_classes":[]},
    "wechatBtn":{"target_classes":[]},
    "resumeBtn":{"target_classes":[]},
    "confirmBtn":{"target_classes":[]}
  },
  "detail":{
    "openTarget":{"target_classes":[[".card-inner.common-wrap",".card-inner.clear-fix",".candidate-card-wrap"]]},
    "closeBtn":{"target_classes":[[".boss-popup__close",".resume-custom-close"]]},
    "messageTip":{"target_classes":[]},
    "messageItem":{"target_classes":[]}
  },
  "extras":[
    {"label":"薪资","element":{"target_classes":[[".salary-text"]]}},
    {"label":"基本信息","element":{"target_classes":[[".job-info-primary"]]}},
    {"label":"标签","element":{"target_classes":[[".tags-wrap"]]}},
    {"label":"公司信息","element":{"target_classes":[[".content.join-text-wrap"]]}}
  ],
  "behavior":{"needsDetailPage":false,"supportsPaging":false,"nextPageBtn":"","nextPageDisabledClass":""}
}'::jsonb
WHERE config_key = 'platform.boss'
  AND NOT (config_value ? 'auth');

UPDATE system_configs
SET config_value = '{
  "id":"zhaopin",
  "name":"智联招聘",
  "domain":"zhaopin.com",
  "pages":[{"url":"https://rd6.zhaopin.com/app/recommend","title":"推荐"}],
  "card":{
    "scroll":{"target_classes":[["[role=group]"]]},
    "item":{
      "parent_classes":[["[role=group]"]],
      "target_classes":[[".recommend-item__inner-content",".recommend-item__inner",".recommend-resume-item__inner"]]
    },
    "fields":[
      {"name":{"target_classes":[[".talent-basic-info__name--inner"]]}},
      {"basic_info":{"target_classes":[[".talent-basic-info__basic"]]}},
      {"education":{"target_classes":[[".resume-item__content.resume-card-exp"]]}},
      {"university":{"target_classes":[[".school-name"]]}},
      {"description":{"target_classes":[[".resume-item__content"]]}}
    ]
  },
  "actions":{
    "greetBtn":{"target_classes":[["[class*=is-mr-16]"]]},
    "continueBtn":{"target_classes":[[".btn-next"]]},
    "phoneBtn":{"target_classes":[]},
    "wechatBtn":{"target_classes":[]},
    "resumeBtn":{"target_classes":[]},
    "confirmBtn":{"target_classes":[]}
  },
  "detail":{
    "openTarget":{"target_classes":[[".resume-item__content",".resume-card-exp"]]},
    "closeBtn":{"target_classes":[[".km-icon.sati-times-circle-s",".close-btn"]]},
    "messageTip":{"target_classes":[]},
    "messageItem":{"target_classes":[]}
  },
  "extras":[
    {"label":"薪资","element":{"target_classes":[[".talent-basic-info__extra--content"]]}}
  ],
  "behavior":{"needsDetailPage":true,"supportsPaging":false,"nextPageBtn":"","nextPageDisabledClass":""}
}'::jsonb
WHERE config_key = 'platform.zhaopin'
  AND NOT (config_value ? 'auth');
