-- 插入各招聘平台选择器配置到 system_configs 表。
INSERT INTO system_configs (config_key, config_value, description)
VALUES
  ('platform.boss', '{
    "id":"boss","name":"Boss直聘","domain":"zhipin.com",
    "pages":[{"url":"https://www.zhipin.com/web/chat/recommend","title":"推荐牛人"}],
    "card":{"container":".card-list","card":[".candidate-card-wrap",".geek-info-card",".card-container",".card-inner.clear-fix",".card-inner.common-wrap"],"name":".name","basicInfo":[".job-card-left"],"education":[".base-info.join-text-wrap",".geek-info-detail"],"university":".content.join-text-wrap","description":".content"},
    "actions":{"greetBtn":[".btn.btn-greet",".btn.btn-getcontact"],"continueBtn":[".btn.btn-continue.btn-outline"],"phoneBtn":[],"wechatBtn":[],"resumeBtn":[],"confirmBtn":[]},
    "detail":{"openTarget":[".card-inner.common-wrap",".card-inner.clear-fix",".candidate-card-wrap"],"closeBtn":[".boss-popup__close",".resume-custom-close"],"messageTip":"","messageItem":""},
    "extras":[{"selector":".salary-text","label":"薪资"},{"selector":".job-info-primary","label":"基本信息"},{"selector":".tags-wrap","label":"标签"},{"selector":".content.join-text-wrap","label":"公司信息"}],
    "behavior":{"needsDetailPage":false,"supportsPaging":false,"nextPageBtn":"","nextPageDisabledClass":""}
  }'::jsonb, 'Boss直聘平台选择器配置'),

  ('platform.zhaopin', '{
    "id":"zhaopin","name":"智联招聘","domain":"zhaopin.com",
    "pages":[{"url":"https://rd6.zhaopin.com/app/recommend","title":"推荐"}],
    "card":{"container":"[role=group]","card":[".recommend-item__inner-content",".recommend-item__inner",".recommend-resume-item__inner"],"name":".talent-basic-info__name--inner","basicInfo":[".talent-basic-info__basic"],"education":[".resume-item__content.resume-card-exp"],"university":".school-name","description":".resume-item__content"},
    "actions":{"greetBtn":["[class*=is-mr-16]"],"continueBtn":[".btn-next"],"phoneBtn":[],"wechatBtn":[],"resumeBtn":[],"confirmBtn":[]},
    "detail":{"openTarget":[".resume-item__content",".resume-card-exp"],"closeBtn":[".km-icon.sati-times-circle-s",".close-btn"],"messageTip":"","messageItem":""},
    "extras":[{"selector":".talent-basic-info__extra--content","label":"薪资"}],
    "behavior":{"needsDetailPage":true,"supportsPaging":false,"nextPageBtn":"","nextPageDisabledClass":""}
  }'::jsonb, '智联招聘平台选择器配置')
ON CONFLICT (config_key) DO NOTHING;
