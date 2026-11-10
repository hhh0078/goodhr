// Package common 本文件只读当前候选人的在线基础资料，复用平台核身和真实关闭能力，不把在线资料当作附件简历。
package common

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"goodhr5/local-agent-go-new/internal/browser/contract"
	"goodhr5/local-agent-go-new/internal/platform/model"
)

// ReadConfiguredAutoReplyOnlineProfile 只在完整入口配置和当前身份已核实时打开在线简历，读取后恢复原聊天。
func ReadConfiguredAutoReplyOnlineProfile(ctx context.Context, browser model.Browser, cfg model.Config, snapshot model.AutoReplyConversationSnapshot, verify, restore func(context.Context) error, clean func(string) string) (profile model.AutoReplyOnlineProfile, returnErr error) {
	if err := ctx.Err(); err != nil {
		return profile, err
	}
	mode := model.DetailRecognitionMode(cfg.ID)
	keys := []string{"message.online_profile_entry", "message.online_profile_panel", "message.online_profile_name", "message.online_profile_close"}
	if mode == "ai" {
		keys = append(keys, "message.online_profile_screenshot", "message.online_profile_frame_url")
	} else {
		keys = append(keys, "message.online_profile_body")
	}
	for _, key := range keys {
		if _, err := RequiredSelector(cfg, key); err != nil {
			return profile, fmt.Errorf("%w：当前平台在线资料入口尚未核实", model.ErrAutoReplyOnlineProfileUnavailable)
		}
	}
	if verify == nil || restore == nil || snapshot.CandidateName == "" {
		return profile, fmt.Errorf("%w：候选人资料身份尚未核实", model.ErrAutoReplyOnlineProfileUnavailable)
	}
	page, err := UniqueCurrentPageURL(ctx, browser)
	if err != nil {
		return profile, err
	}
	if !PageURLMatches(page, cfg.MessagesURL) {
		return profile, fmt.Errorf("当前页面与招聘会话入口不符，暂不读取在线资料")
	}
	fragment := cfg.Behavior.AutoReplyOnlineProfileFragment
	if fragment != "" && !onlineProfilePageAllowed(page, page, fragment) {
		return profile, fmt.Errorf("当前页面不是已配置的在线简历片段，暂不操作")
	}
	entry, _ := RequiredSelector(cfg, "message.online_profile_entry")
	panel, _ := RequiredSelector(cfg, "message.online_profile_panel")
	openedByThisCall := false
	// 当前会话和弹层姓名都须一致，不能关闭外部操作后来切换的另一人弹层。
	verifyProfile := func(check context.Context) error {
		hiddenConversation := false
		if err := verify(check); err != nil {
			// 只接受本轮从已核实原生会话实际点开的DOM资料；旧弹层、未知点击及canvas不能凭同名绕过核身。
			if !errors.Is(err, model.ErrAutoReplyConversationNotOpen) || !cfg.Behavior.AutoReplyOnlineProfileHidesConversation || mode != "dom" || !openedByThisCall {
				return err
			}
			if _, err := verifyOnlineProfilePage(check, browser, page, fragment); err != nil {
				return err
			}
			hiddenConversation = true
		}
		name, found, err := ReadOptional(check, browser, cfg, "message.online_profile_name")
		if err != nil {
			return err
		}
		nameMatches := CandidateNamesMatch(snapshot.CandidateName, name)
		if hiddenConversation {
			nameMatches = strings.TrimSpace(name) == strings.TrimSpace(snapshot.CandidateName)
		}
		if !found || !nameMatches {
			return fmt.Errorf("在线资料姓名与当前会话不一致，暂不继续")
		}
		return nil
	}
	closeAndRestore := func(check context.Context, guard func(context.Context) error) error {
		opened, err := verifyOnlineProfilePage(check, browser, page, fragment)
		if err != nil {
			return err
		}
		if err = ClosePanelEscapeFirst(check, browser, cfg, "message.online_profile_panel", "message.online_profile_close", opened, guard); err != nil && !onlineProfileClosedAfterFragmentChange(check, browser, cfg, page, opened) {
			return fmt.Errorf("在线资料还没关好：%w", err)
		}
		if _, err = verifyOnlineProfilePage(check, browser, page, fragment); err != nil {
			return err
		}
		// 面板消失也不代表允许切回原人；已经换成别人的聊天时停止恢复。
		if err = verify(check); err != nil && !errors.Is(err, model.ErrAutoReplyConversationNotOpen) {
			return err
		}
		if err = restore(check); err != nil {
			return fmt.Errorf("在线资料已关闭，但原聊天还没恢复：%w", err)
		}
		if _, err = verifyOnlineProfilePage(check, browser, page, fragment); err != nil {
			return err
		}
		if err = verify(check); err != nil {
			return fmt.Errorf("关闭在线资料后原聊天身份未核实：%w", err)
		}
		return nil
	}
	preexisting, err := ProbeSelectorExists(ctx, browser, cfg, "message.online_profile_panel")
	if err != nil {
		return profile, err
	}
	reuse := false
	if preexisting {
		if err = verify(ctx); err != nil {
			return profile, err
		}
		if mode == "ai" {
			// Canvas没有可信DOM正文可证明旧弹层归属，先按原页和当前会话核身安全关闭，再从原聊天重新打开。
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
			err = closeAndRestore(cleanup, verify)
			cancel()
			if err != nil {
				return profile, err
			}
		} else {
			if err = verifyProfile(ctx); err != nil {
				return profile, err
			}
			reuse = true
		}
	} else if err = verify(ctx); err != nil {
		if !errors.Is(err, model.ErrAutoReplyConversationNotOpen) {
			return profile, err
		}
		if err = restore(ctx); err != nil {
			return profile, err
		}
		if err = verify(ctx); err != nil {
			return profile, err
		}
	}
	// 点击可能已经打开弹层、但后续验证报错；必须在触发点击之前登记受归属保护的清理。
	owned := reuse
	defer func() {
		if !owned {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
		defer cancel()
		if err := closeAndRestore(cleanup, verifyProfile); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if !reuse {
		if err = ctx.Err(); err != nil {
			return profile, err
		}
		if _, err = verifyOnlineProfilePage(ctx, browser, page, fragment); err != nil {
			return profile, err
		}
		if err = verify(ctx); err != nil {
			return profile, err
		}
		owned = true
		result, clickErr := browser.Click(ctx, contract.ElementClickRequest{Selector: entry, Verify: &contract.ClickVerification{TargetVisible: &panel, TimeoutMS: 2500}})
		openedByThisCall = result.Clicked || onlineProfileEntryClickCompleted(clickErr)
		if clickErr != nil {
			err = clickErr
			return profile, err
		}
	}
	openedPage, err := verifyOnlineProfilePage(ctx, browser, page, fragment)
	if err != nil {
		return profile, err
	}
	if err := verifyProfile(ctx); err != nil {
		return profile, err
	}
	if mode == "ai" {
		images, sourceKey, err := captureAutoReplyOnlineProfile(ctx, browser, cfg, snapshot)
		if err != nil {
			return profile, err
		}
		profile.CandidateName, profile.Mode, profile.Screenshots, profile.SourceKey = snapshot.CandidateName, mode, images, sourceKey
		return profile, nil
	}
	body, found, err := ReadOptional(ctx, browser, cfg, "message.online_profile_body")
	if err != nil {
		return profile, err
	}
	if !found || strings.TrimSpace(body) == "" {
		return profile, fmt.Errorf("在线资料正文还没读到，稍后再核对")
	}
	if clean != nil {
		body = clean(body)
	}
	if strings.TrimSpace(body) == "" {
		return profile, fmt.Errorf("在线资料清理后没有正文，暂不猜测候选人信息")
	}
	currentPage, err := UniqueCurrentPageURL(ctx, browser)
	if err != nil || currentPage != openedPage {
		return profile, fmt.Errorf("读取在线资料期间招聘页面已变化，暂不使用这份资料")
	}
	profile = ParseAutoReplyOnlineProfileText(body, snapshot.CandidateName)
	profile.Mode = mode
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"gender", &profile.Gender}, {"education", &profile.EducationLevel},
		{"earliest_company_name", &profile.EarliestCompanyName}, {"earliest_school_name", &profile.EarliestSchoolName},
	} {
		value, found, err := ReadOptional(ctx, browser, cfg, "message.online_profile_"+field.key)
		if err != nil {
			return profile, err
		}
		if found && strings.TrimSpace(value) != "" {
			if field.key == "education" {
				*field.target = uniqueOnlineMatch(onlineProfileHeaderEducation, value)
			} else {
				*field.target = strings.TrimSpace(value)
			}
		} else if field.key == "education" {
			if _, configured := cfg.Selectors["message.online_profile_education"]; configured {
				*field.target = "" // 明确配置的头部学历是权威来源，缺失时不沿用其它段落中的猜测。
			}
		}
	}
	for _, field := range []struct {
		key    string
		target **int
		min    int
		max    int
	}{
		{"age", &profile.Age, 16, 100}, {"earliest_company_year", &profile.EarliestCompanyYear, 1900, time.Now().Year()}, {"earliest_school_year", &profile.EarliestSchoolYear, 1900, time.Now().Year()},
	} {
		if field.key == "age" {
			if _, configured := cfg.Selectors["message.online_profile_age"]; configured {
				*field.target = nil
			}
		}
		value, _, err := ReadOptional(ctx, browser, cfg, "message.online_profile_"+field.key)
		if err != nil {
			return profile, err
		}
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(value), "岁"), "年"))
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= field.min && parsed <= field.max {
			*field.target = &parsed
		}
	}
	if err := readConfiguredOnlineExperiences(ctx, browser, cfg, &profile); err != nil {
		return model.AutoReplyOnlineProfile{}, fmt.Errorf("在线经历尚未核实：%w", err)
	}
	profile.SourceKey = "online:" + HashText(profile.CandidateName+"|"+profile.RawText)
	return profile, nil
}

// onlineProfileEntryClickCompleted 只接受Worker明确回执的已点击证据，不能把开始按鼠标或未知超时当成成功打开。
func onlineProfileEntryClickCompleted(err error) bool {
	var workerErr *contract.WorkerError
	if !errors.As(err, &workerErr) || workerErr == nil || contract.IsSessionUnavailable(err) {
		return false
	}
	var details struct {
		Clicked bool `json:"clicked"`
	}
	return json.Unmarshal(workerErr.Body.Details, &details) == nil && details.Clicked
}
