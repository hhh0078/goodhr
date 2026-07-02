-- 本迁移新增猎聘猎头端平台选择器配置，供云端下发给本地程序执行。
INSERT INTO system_configs (config_key, config_value, description, enabled)
VALUES (
	'platform.hliepin',
	'{
		"id": "hliepin",
		"name": "猎聘猎头端",
		"domain": "h.liepin.com",
		"auth": {
			"pages": [
				{
					"url": "https://h.liepin.com/search/getConditionItem",
					"entry": true,
					"match": "contains",
					"title": "找人"
				},
				{
					"url": "https://h.liepin.com",
					"match": "contains",
					"title": "猎聘猎头端"
				}
			],
			"entry_url": "https://h.liepin.com/search/getConditionItem",
			"login_url_prefixes": [
				"https://passport.liepin.com",
				"https://h.liepin.com/login"
			],
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
				{ "description": { "target_classes": [[".new-resume-personal-expect"]] } }
			],
			"scroll": {
				"target_classes": [[".recommandResumes--", ".ant-table-body", "body"]]
			}
		},
		"actions": {
			"greetBtn": {
				"target_classes": [[".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"]]
			},
			"continueBtn": {
				"target_classes": [[".ant-btn.ant-btn-default.ant-btn-lg.lp-ant-btn-light"]]
			},
			"phoneBtn": {
				"target_classes": [[".ant-btn.ant-btn-primary.__im_basic__basic-input-action", ".im-ui-action-button.action-item"]]
			},
			"confirmBtn": {
				"target_classes": [[".ant-btn.ant-btn-link.ant-btn-lg.btn-cancel.directly-open-chat-btn"]]
			}
		},
		"detail": {
			"openTarget": {
				"target_classes": [[".tlog-common-resume-card", ".new-resume-personal-name"]]
			},
			"content": {
				"target_classes": [["body", ".resume-detail", ".resume-content", ".resume"]]
			},
			"closeBtn": {
				"target_classes": [[".closeBtn--"]]
			}
		},
		"position": {
			"current": {
				"target_classes": [[".ant-select-selection-item", ".position-name", ".job-name"]]
			},
			"switchBtn": {
				"target_classes": [[".ant-select-selector", ".position-name", ".job-name"]]
			},
			"list": {
				"target_classes": [[".ant-select-dropdown", ".ant-dropdown", "body"]]
			},
			"item": {
				"target_classes": [[".ant-select-item-option", ".ant-dropdown-menu-item"]]
			},
			"itemText": {
				"target_classes": [[".ant-select-item-option-content", ".ant-dropdown-menu-title-content"]]
			}
		},
		"behavior": {
			"needsDetailPage": true,
			"supportsPaging": true,
			"nextPageBtn": ".ant-pagination-next",
			"nextPageDisabledClass": "ant-pagination-disabled"
		}
	}'::jsonb,
	'猎聘猎头端平台选择器配置',
	true
)
ON CONFLICT (config_key) DO UPDATE SET
	config_value = EXCLUDED.config_value,
	description = EXCLUDED.description,
	enabled = EXCLUDED.enabled,
	updated_at = now();
