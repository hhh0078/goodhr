// Package platform 本文件验证猎聘企业端在线简历不依赖附件，并严格限定当前可见资料弹层及独立姓名。
package platform

import (
	"os"
	"strings"
	"testing"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// TestLiepinOnlineProfileConfigDoesNotRequireAttachment 验证当前聊天姓名可直接打开在线资料，不退回最后附件卡入口。
func TestLiepinOnlineProfileConfigDoesNotRequireAttachment(t *testing.T) {
	content, err := os.ReadFile("liepin/config.json")
	if err != nil {
		t.Fatal(err)
	}
	assertConfigComments(t, content)
	cfg, err := LoadConfig("liepin")
	if err != nil {
		t.Fatal(err)
	}
	if model.DetailRecognitionMode(cfg.ID) != "dom" {
		t.Fatal("猎聘企业端在线资料必须直接读取可见DOM，不调用图片AI或本机OCR")
	}
	if cfg.Behavior.AutoReplyOnlineProfileFragment != "preview" {
		t.Fatal("在线面板只允许已实证的preview片段，关闭后可保留此状态")
	}
	if !cfg.Behavior.AutoReplyOnlineProfileHidesConversation {
		t.Fatal("已实证的在线弹层隐藏聊天行为必须显式配置，不能用URL片段推断")
	}
	entry := cfg.Selectors["message.online_profile_entry"]
	if len(entry.Target.Selectors) != 1 || entry.Target.Selectors[0].Value != ".im-ui-basic-chat-modal:visible .im-ui-basic-chat-header-name" || len(entry.Parents) != 0 || len(entry.Frames) != 0 || entry.Target.Index != nil {
		t.Fatal("在线入口须是当前可见聊天头部姓名，不得依赖附件卡或按序号猜候选人")
	}
	for _, group := range entry.Parents {
		for _, candidate := range group.Selectors {
			if strings.Contains(candidate.Value, "attachment") {
				t.Fatal("在线基础资料不应等待候选人发送附件")
			}
		}
	}
	legacy := cfg.Selectors["message.resume_online_entry"]
	if len(legacy.Parents) != 2 || !strings.Contains(legacy.Parents[1].Selectors[0].Value, "im-ui-send-attachment-card") {
		t.Fatal("旧附件卡在线入口应保留独立兼容，不能被新头部路径覆盖")
	}
}

// TestLiepinOnlineProfileConfigScopesPanelNameBodyAndClose 验证正文和姓名来自同一可见在线资料，不读取整页或聊天姓名冒充。
func TestLiepinOnlineProfileConfigScopesPanelNameBodyAndClose(t *testing.T) {
	cfg, err := LoadConfig("liepin")
	if err != nil {
		t.Fatal(err)
	}
	for key, target := range map[string]string{
		"message.online_profile_panel": ".ant-lpt-modal:visible .resume-detail-content-body",
		"message.online_profile_name":  ".ant-lpt-modal:visible .resume-detail-content-body [class^=\"individualInfo--\"] [class^=\"name--\"]",
		"message.online_profile_body":  ".ant-lpt-modal:visible .resume-detail-content-body .xpath-resume-body",
		"message.online_profile_close": ".ant-lpt-modal:visible [data-tlg-elem-id=\"b_pc_pop_res_detail_close_btn\"]",
	} {
		spec, found := cfg.Selectors[key]
		if !found || len(spec.Target.Selectors) != 1 || spec.Target.Selectors[0].Value != target || spec.Target.Index != nil || len(spec.Frames) != 0 || len(spec.FrameFallbacks) != 0 || spec.TimeoutMS <= 0 {
			t.Fatalf("%s须只使用已验证的当前可见弹层，不能按第一项或旧整页降级", key)
		}
		if key != "message.online_profile_close" && spec.State != "visible" {
			t.Fatalf("%s不能读取后台隐藏的候选人资料", key)
		}
	}
	for _, key := range []string{"message.online_profile_screenshot", "message.online_profile_frame_url"} {
		if _, found := cfg.Selectors[key]; found {
			t.Fatalf("DOM路径不应伪造%s", key)
		}
	}
}

// TestLiepinOnlineProfileExperienceConfigPairsVisibleRows 验证工作、教育按独立行读机构和日期，头部年龄学历均有明确图标来源。
func TestLiepinOnlineProfileExperienceConfigPairsVisibleRows(t *testing.T) {
	cfg, err := LoadConfig("liepin")
	if err != nil {
		t.Fatal(err)
	}
	for key, suffix := range map[string]string{
		"message.online_profile_work_item":   ".xpath-work-experience [class^=\"workCont--\"]",
		"message.online_profile_school_item": ".xpath-education-experience-wrap li",
		"message.online_profile_age":         "[class^=\"individualDetail--\"] [class^=\"infoItem--\"]:has([aria-label=\"birthday\"])",
		"message.online_profile_education":   "[class^=\"individualDetail--\"] [class^=\"infoItem--\"]:has([aria-label=\"education\"])",
	} {
		spec := cfg.Selectors[key]
		if len(spec.Target.Selectors) != 1 || !strings.HasPrefix(spec.Target.Selectors[0].Value, ".ant-lpt-modal:visible .resume-detail-content-body ") || !strings.HasSuffix(spec.Target.Selectors[0].Value, suffix) || spec.Target.Index != nil || spec.State != "visible" {
			t.Fatalf("%s必须限定本次可见在线资料，不能选DOM第一条代替最早记录", key)
		}
	}
	for key, target := range map[string]string{
		"online_profile_work_name":     "header .xpath-work-cont-company",
		"online_profile_work_period":   "header [class^=\"time--\"]",
		"online_profile_school_name":   "[class^=\"schoolName--\"]",
		"online_profile_school_period": "time",
	} {
		field, found := cfg.CandidateFields[key]
		if !found || len(field.Target.Selectors) != 1 || field.Target.Selectors[0].Value != target || len(field.Parents) != 0 || len(field.Frames) != 0 || field.Target.Index != nil {
			t.Fatalf("%s须相对当前经历行读取，不能跨行配对公司与时间", key)
		}
	}
}
