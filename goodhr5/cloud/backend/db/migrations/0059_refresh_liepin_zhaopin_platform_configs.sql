-- 本迁移按旧插件独立平台配置刷新猎聘猎头端、猎聘企业端和智联招聘选择器。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES
(
  'platform.hliepin',
  $$
  {
    "id": "hliepin",
    "name": "猎聘猎头端",
    "open": true,
    "domain": "h.liepin.com",
    "auth": {
      "pages": [
        { "url": "https://h.liepin.com/search/getConditionItem", "entry": true, "match": "contains", "title": "找人" },
        { "url": "https://h.liepin.com", "match": "contains", "title": "猎聘猎头端" }
      ],
      "entry_url": "https://h.liepin.com/search/getConditionItem",
      "login_url_prefixes": ["https://passport.liepin.com", "https://h.liepin.com/login"],
      "logged_in_url_contains": ["h.liepin.com"]
    },
    "card": {
      "item": {
        "parent_classes": [[".recommandResumes--", "tbody"]],
        "target_classes": [[".no-hover-tr", ".tlog-common-resume-card"]]
      },
      "fields": [
        { "name": { "target_classes": [[".new-resume-personal-name"]] } },
        { "basic_info": { "target_classes": [[".personal-detail-age"]] } },
        { "education": { "target_classes": [[".J1lRR"]] } },
        { "university": { "target_classes": [[".J1lRR"]] } },
        { "description": { "target_classes": [[".new-resume-personal-expect", ".new-resume-offline", ".J1lRR"]] } }
      ],
      "scroll": { "target_classes": [[".recommandResumes--", ".ant-table-body", "body"]] }
    },
    "actions": {
      "greetBtn": { "target_classes": [[".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"]] },
      "continueBtn": { "target_classes": [[".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"]] },
      "phoneBtn": { "target_classes": [[".ant-btn.ant-btn-primary.__im_basic__basic-input-action", ".im-ui-action-button.action-item"]] },
      "confirmBtn": { "target_classes": [[".ant-btn.ant-btn-link.ant-btn-lg.btn-cancel.directly-open-chat-btn"]] }
    },
    "detail": {
      "openTarget": { "target_classes": [[".tlog-common-resume-card"]] },
      "content": { "target_classes": [["body", ".resume-detail", ".new-resume-personal", ".tlog-common-resume-card", ".J1lRR"]] },
      "closeBtn": { "target_classes": [[".closeBtn--"]] }
    },
    "position": {
      "current": { "target_classes": [[".ant-select-selection-item", ".position-name", ".job-name", ".current-position"]] },
      "switchBtn": { "target_classes": [[".ant-select-selector", ".position-name", ".job-name", ".current-position"]] },
      "list": { "target_classes": [[".ant-select-dropdown", ".ant-dropdown", "body"]] },
      "item": { "target_classes": [[".ant-select-item-option", ".ant-dropdown-menu-item"]] },
      "itemText": { "target_classes": [[".ant-select-item-option-content", ".ant-dropdown-menu-title-content"]] }
    },
    "behavior": {
      "needsDetailPage": true,
      "supportsPaging": true,
      "nextPageBtn": ".ant-pagination-next",
      "nextPageDisabledClass": "ant-pagination-disabled"
    }
  }
  $$::jsonb,
  '猎聘猎头端平台选择器配置',
  true
),
(
  'platform.liepin',
  $$
  {
    "id": "liepin",
    "name": "猎聘企业端",
    "open": true,
    "domain": "lpt.liepin.com",
    "auth": {
      "pages": [
        { "url": "https://lpt.liepin.com/recommend", "entry": true, "match": "contains", "title": "人才推荐" },
        { "url": "https://lpt.liepin.com", "match": "contains", "title": "猎聘企业端" }
      ],
      "entry_url": "https://lpt.liepin.com/recommend",
      "login_url_prefixes": ["https://passport.liepin.com", "https://lpt.liepin.com/login"],
      "logged_in_url_contains": ["lpt.liepin.com"]
    },
    "card": {
      "item": {
        "parent_classes": [[".recommandResumes--", "body"]],
        "target_classes": [[".newResumeItemWrap--"]]
      },
      "fields": [
        { "name": { "target_classes": [[".nest-resume-personal-name"]] } },
        { "basic_info": { "target_classes": [[".personal-detail-age"]] } },
        { "education": { "target_classes": [[".personal-detail-edulevel"]] } },
        { "university": { "target_classes": [[".resume-university"]] } },
        { "description": { "target_classes": [[".resume-description", ".nest-resume-personal-skills", ".personal-expect-content", ".personal-detail-location"]] } }
      ],
      "scroll": { "target_classes": [[".recommandResumes--", "body"]] }
    },
    "actions": {
      "greetBtn": { "target_classes": [[".ant-lpt-btn.ant-lpt-btn-primary.ant-lpt-teno-btn.ant-lpt-teno-btn-secondary"]] },
      "continueBtn": { "target_classes": [[".ant-lpt-btn.ant-lpt-btn-primary.ant-lpt-teno-btn.ant-lpt-teno-btn-secondary"]] },
      "phoneBtn": { "target_classes": [[".im-ui-action-button.action-item.action-phone"]] },
      "wechatBtn": { "target_classes": [[".im-ui-action-button.action-item.action-wechat"]] },
      "resumeBtn": { "target_classes": [[".im-ui-action-button.action-item.action-resume"]] },
      "confirmBtn": { "target_classes": [[".ant-im-btn.ant-im-btn-primary"]] }
    },
    "detail": {
      "openTarget": { "target_classes": [[".newResumeItem"]] },
      "content": { "target_classes": [["body", ".newResumeItem", ".resume-detail", ".resume-description", ".nest-resume-personal-skills", ".personal-expect-content"]] },
      "closeBtn": { "target_classes": [[".closeBtn--"]] }
    },
    "position": {
      "current": { "target_classes": [[".ant-select-selection-item", ".position-name", ".job-name"]] },
      "switchBtn": { "target_classes": [[".ant-select-selector", ".position-name", ".job-name"]] },
      "list": { "target_classes": [[".ant-select-dropdown", ".ant-dropdown", "body"]] },
      "item": { "target_classes": [[".ant-select-item-option", ".ant-dropdown-menu-item"]] },
      "itemText": { "target_classes": [[".ant-select-item-option-content", ".ant-dropdown-menu-title-content"]] }
    },
    "behavior": {
      "needsDetailPage": true,
      "supportsPaging": false,
      "nextPageBtn": "",
      "nextPageDisabledClass": ""
    }
  }
  $$::jsonb,
  '猎聘企业端平台选择器配置',
  true
),
(
  'platform.zhaopin',
  $$
  {
    "id": "zhaopin",
    "name": "智联招聘",
    "open": true,
    "domain": "zhaopin.com",
    "auth": {
      "pages": [
        { "url": "https://rd6.zhaopin.com/app/recommend", "entry": true, "match": "contains", "title": "推荐" },
        { "url": "https://rd6.zhaopin.com", "match": "contains", "title": "智联招聘" },
        { "url": "https://www.zhaopin.com", "match": "contains", "title": "智联招聘" }
      ],
      "entry_url": "https://rd6.zhaopin.com/app/recommend",
      "login_url_prefixes": ["https://passport.zhaopin.com", "https://rd6.zhaopin.com/login"],
      "logged_in_url_contains": ["rd6.zhaopin.com"]
    },
    "card": {
      "item": {
        "parent_classes": [["[role=\"group\"]", "body"]],
        "target_classes": [[".recommend-item__inner.recommend-resume-item__inner"]]
      },
      "fields": [
        { "name": { "target_classes": [[".talent-basic-info__name--inner"]] } },
        { "basic_info": { "target_classes": [[".talent-basic-info__basic"]] } },
        { "education": { "target_classes": [[".resume-item__content.resume-card-exp"]] } },
        { "description": { "target_classes": [[".resume-item__content", ".talent-basic-info__extra--content"]] } }
      ],
      "scroll": { "target_classes": [["[role=\"group\"]", "body"]] }
    },
    "actions": {
      "greetBtn": { "target_classes": [[".small-screen-btn.is-mr-16"]] },
      "continueBtn": { "target_classes": [[".small-screen-btn.is-mr-16.km-button.km-control.km-ripple-off.km-button--light.km-button--plain.resume-btn-small"]] }
    },
    "detail": {
      "openTarget": { "target_classes": [[".new-resume-detail--inner", ".recommend-item__inner.recommend-resume-item__inner"]] },
      "content": { "target_classes": [["body", ".new-resume-detail--inner", ".resume-detail", ".resume-item__content", ".talent-basic-info__extra--content"]] }
    },
    "position": {
      "current": { "target_classes": [[".ant-select-selection-item", ".position-name", ".job-name", ".current-position"]] },
      "switchBtn": { "target_classes": [[".ant-select-selector", ".position-name", ".job-name", ".current-position"]] },
      "list": { "target_classes": [[".ant-select-dropdown", ".km-select-dropdown", "body"]] },
      "item": { "target_classes": [[".ant-select-item-option", ".km-select-option", ".ant-dropdown-menu-item"]] },
      "itemText": { "target_classes": [[".ant-select-item-option-content", ".km-select-option-content", ".ant-dropdown-menu-title-content"]] }
    },
    "behavior": {
      "needsDetailPage": true,
      "supportsPaging": false,
      "nextPageBtn": "",
      "nextPageDisabledClass": ""
    }
  }
  $$::jsonb,
  '智联招聘平台选择器配置',
  true
)
ON CONFLICT (config_key) DO UPDATE
SET config_value = EXCLUDED.config_value,
    description = EXCLUDED.description,
    enabled = EXCLUDED.enabled,
    updated_at = now();
