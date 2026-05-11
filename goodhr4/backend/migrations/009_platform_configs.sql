-- 插入各招聘平台选择器配置到 system_configs
-- config_key 格式：platform.{平台ID}
-- config_value 包含完整平台配置 JSON（选择器、行为等）
-- urlPattern 改为域名字符串，前端用 includes() 匹配

insert into system_configs (config_key, config_value, description)
values
  (
    'platform.boss',
    '{
      "id": "boss",
      "name": "Boss直聘",
      "domain": "zhipin.com",
      "card": {
        "container": ".card-list",
        "card": [".candidate-card-wrap", ".geek-info-card", ".card-container"],
        "name": ".name",
        "basicInfo": [".job-card-left"],
        "education": [".base-info.join-text-wrap", ".geek-info-detail"],
        "university": ".content.join-text-wrap",
        "description": ".content"
      },
      "actions": {
        "greetBtn": [".btn.btn-greet", ".btn.btn-getcontact"],
        "continueBtn": [".btn.btn-continue.btn-outline"],
        "phoneBtn": [".operate-item"],
        "wechatBtn": [],
        "resumeBtn": [],
        "confirmBtn": []
      },
      "detail": {
        "openTarget": [".card-inner.common-wrap", ".card-inner.clear-fix", ".candidate-card-wrap"],
        "closeBtn": [".boss-popup__close", ".resume-custom-close"],
        "messageTip": ".chat-global-entry",
        "messageItem": ".friend-list-item"
      },
      "extras": [
        { "selector": ".salary-text", "label": "薪资" },
        { "selector": ".job-info-primary", "label": "基本信息" },
        { "selector": ".tags-wrap", "label": "标签" },
        { "selector": ".content.join-text-wrap", "label": "公司信息" }
      ],
      "behavior": {
        "needsDetailPage": false,
        "supportsPaging": false,
        "nextPageBtn": "",
        "nextPageDisabledClass": ""
      }
    }'::jsonb,
    'Boss直聘平台选择器配置'
  ),
  (
    'platform.lagou',
    '{
      "id": "lagou",
      "name": "拉勾网",
      "domain": "lagou.com",
      "card": {
        "container": ".position-list",
        "card": [".position-item"],
        "name": ".position-name",
        "basicInfo": [".age-info"],
        "education": [".edu-background"],
        "university": ".school-name",
        "description": ".position-detail"
      },
      "actions": {
        "greetBtn": [".position-title"],
        "continueBtn": [],
        "phoneBtn": [],
        "wechatBtn": [],
        "resumeBtn": [],
        "confirmBtn": []
      },
      "detail": {
        "openTarget": [".position-item"],
        "closeBtn": [],
        "messageTip": "",
        "messageItem": ""
      },
      "extras": [
        { "selector": ".salary", "label": "薪资" },
        { "selector": ".company-name", "label": "公司" },
        { "selector": ".industry-field", "label": "行业" },
        { "selector": ".position-address", "label": "地点" }
      ],
      "behavior": {
        "needsDetailPage": true,
        "supportsPaging": false,
        "nextPageBtn": "",
        "nextPageDisabledClass": ""
      }
    }'::jsonb,
    '拉勾网平台选择器配置'
  ),
  (
    'platform.liepin',
    '{
      "id": "liepin",
      "name": "猎聘网",
      "domain": "lpt.liepin.com",
      "card": {
        "container": ".recommandResumes--",
        "card": [".newResumeItemWrap--"],
        "name": ".nest-resume-personal-name",
        "basicInfo": [".personal-detail-age"],
        "education": [".personal-detail-edulevel"],
        "university": ".resume-university",
        "description": ".resume-description"
      },
      "actions": {
        "greetBtn": [".ant-lpt-btn.ant-lpt-btn-primary.ant-lpt-teno-btn.ant-lpt-teno-btn-secondary"],
        "continueBtn": [".ant-lpt-btn.ant-lpt-btn-primary.ant-lpt-teno-btn.ant-lpt-teno-btn-secondary"],
        "phoneBtn": [".im-ui-action-button.action-item.action-phone"],
        "wechatBtn": [".im-ui-action-button.action-item.action-wechat"],
        "resumeBtn": [".im-ui-action-button.action-item.action-resume"],
        "confirmBtn": [".ant-im-btn.ant-im-btn-primary"]
      },
      "detail": {
        "openTarget": [".newResumeItem"],
        "closeBtn": [".closeBtn--"],
        "messageTip": "",
        "messageItem": ""
      },
      "extras": [
        { "selector": ".nest-resume-personal-skills", "label": "技能" },
        { "selector": ".personal-expect-content", "label": "薪资" },
        { "selector": ".personal-detail-location", "label": "地点" }
      ],
      "behavior": {
        "needsDetailPage": true,
        "supportsPaging": false,
        "nextPageBtn": "",
        "nextPageDisabledClass": ""
      }
    }'::jsonb,
    '猎聘网(lpt.liepin.com)平台选择器配置'
  ),
  (
    'platform.hliepin',
    '{
      "id": "hliepin",
      "name": "猎聘网(h)",
      "domain": "h.liepin.com",
      "card": {
        "container": ".recommandResumes--",
        "card": [".no-hover-tr"],
        "name": ".new-resume-personal-name",
        "basicInfo": [".personal-detail-age"],
        "education": [".J1lRR"],
        "university": ".J1lRR",
        "description": ".new-resume-personal-expect"
      },
      "actions": {
        "greetBtn": [".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"],
        "continueBtn": [".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"],
        "phoneBtn": [".ant-btn.ant-btn-primary.__im_basic__basic-input-action", ".im-ui-action-button.action-item"],
        "wechatBtn": [],
        "resumeBtn": [],
        "confirmBtn": [".ant-btn.ant-btn-link.ant-btn-lg.btn-cancel.directly-open-chat-btn"]
      },
      "detail": {
        "openTarget": [".tlog-common-resume-card"],
        "closeBtn": [".closeBtn--"],
        "messageTip": "",
        "messageItem": ""
      },
      "extras": [
        { "selector": ".J1lRR", "label": "详情" },
        { "selector": ".new-resume-offline", "label": "在线状态" }
      ],
      "behavior": {
        "needsDetailPage": true,
        "supportsPaging": true,
        "nextPageBtn": ".ant-pagination-next",
        "nextPageDisabledClass": "ant-pagination-disabled"
      }
    }'::jsonb,
    '猎聘网(h.liepin.com)平台选择器配置'
  ),
  (
    'platform.zhilian',
    '{
      "id": "zhilian",
      "name": "智联招聘",
      "domain": "zhaopin.com",
      "card": {
        "container": "[role=\"group\"]",
        "card": [".recommend-item__inner.recommend-resume-item__inner"],
        "name": ".talent-basic-info__name--inner",
        "basicInfo": [".talent-basic-info__basic"],
        "education": [".resume-item__content.resume-card-exp"],
        "university": "",
        "description": ".resume-item__content"
      },
      "actions": {
        "greetBtn": [".small-screen-btn.is-mr-16"],
        "continueBtn": [".small-screen-btn.is-mr-16.km-button.km-control.km-ripple-off.km-button--light.km-button--plain.resume-btn-small"],
        "phoneBtn": [],
        "wechatBtn": [],
        "resumeBtn": [],
        "confirmBtn": []
      },
      "detail": {
        "openTarget": [".new-resume-detail--inner"],
        "closeBtn": [],
        "messageTip": "",
        "messageItem": ""
      },
      "extras": [
        { "selector": ".talent-basic-info__extra--content", "label": "薪资" }
      ],
      "behavior": {
        "needsDetailPage": true,
        "supportsPaging": false,
        "nextPageBtn": "",
        "nextPageDisabledClass": ""
      }
    }'::jsonb,
    '智联招聘平台选择器配置'
  ),
  (
    'platform.employer58',
    '{
      "id": "employer58",
      "name": "58同城",
      "domain": "employer.58.com",
      "card": {
        "container": ".recommandResumes--",
        "card": [".recommend-list.recommendList"],
        "name": ".trueName.mycustomf",
        "basicInfo": [".mycustomfontgobp1qai0ai2y2.resumeFont2"],
        "education": [".hover-wrapper"],
        "university": ".hover-wrapper",
        "description": ".mycustomfont0zfunx8fq907"
      },
      "actions": {
        "greetBtn": [".el-button.chat-btn.el-button--primary"],
        "continueBtn": [],
        "phoneBtn": [],
        "wechatBtn": [],
        "resumeBtn": [],
        "confirmBtn": [".ant-btn.ant-btn-link.ant-btn-lg.btn-cancel.directly-open-chat-btn"]
      },
      "detail": {
        "openTarget": [".recommend-list.recommendList"],
        "closeBtn": [".closeBtn--"],
        "messageTip": "",
        "messageItem": ""
      },
      "extras": [
        { "selector": ".J1lRR", "label": "详情" },
        { "selector": ".recommend-status", "label": "在线状态" }
      ],
      "behavior": {
        "needsDetailPage": false,
        "supportsPaging": true,
        "nextPageBtn": ".ant-pagination-next",
        "nextPageDisabledClass": "ant-pagination-disabled"
      }
    }'::jsonb,
    '58同城平台选择器配置'
  )
on conflict (config_key) do update set
  config_value = excluded.config_value,
  description = excluded.description,
  updated_at = now();
