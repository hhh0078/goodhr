// Package common 本文件仅按平台显式配置核验在线简历的URL片段变化，保留原页面和候选人身份边界。
package common

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"goodhr5/local-agent-go-new/internal/platform/model"
)

// onlineProfilePageAllowed 只允许同源、同路径、同查询在空片段和配置片段间切换，未配置时要求完整URL相同。
func onlineProfilePageAllowed(original, current, fragment string) bool {
	if fragment == "" {
		return original == current
	}
	if strings.ContainsAny(fragment, "#?/%\\ \t\r\n") {
		return false
	}
	before, err := url.Parse(original)
	if err != nil || before.Opaque != "" || before.User != nil || before.Host == "" || (before.Scheme != "https" && before.Scheme != "http") {
		return false
	}
	after, err := url.Parse(current)
	if err != nil || after.Opaque != "" || after.User != nil {
		return false
	}
	if before.Scheme != after.Scheme || before.Host != after.Host || before.Path != after.Path || before.RawPath != after.RawPath || before.RawQuery != after.RawQuery || before.ForceQuery != after.ForceQuery {
		return false
	}
	return before.RawFragment == "" && after.RawFragment == "" &&
		(before.Fragment == "" || before.Fragment == fragment) && (after.Fragment == "" || after.Fragment == fragment)
}

// verifyOnlineProfilePage 读取真实当前页并核对允许的片段状态，不改写Worker页指针或导航。
func verifyOnlineProfilePage(ctx context.Context, browser model.Browser, original, fragment string) (string, error) {
	current, err := UniqueCurrentPageURL(ctx, browser)
	if err != nil {
		return "", err
	}
	if !onlineProfilePageAllowed(original, current, fragment) {
		return "", fmt.Errorf("在线资料所在页面已变化，暂不继续")
	}
	return current, nil
}

// onlineProfileClosedAfterFragmentChange 只在快捷键已使页面切到另一合法片段且弹层确实消失时确认收尾，绝不追加动作。
func onlineProfileClosedAfterFragmentChange(ctx context.Context, browser model.Browser, cfg model.Config, original, opened string) bool {
	fragment := cfg.Behavior.AutoReplyOnlineProfileFragment
	if fragment == "" {
		return false
	}
	current, err := verifyOnlineProfilePage(ctx, browser, original, fragment)
	if err != nil || current == opened {
		return false
	}
	found, err := ProbeSelectorExists(ctx, browser, cfg, "message.online_profile_panel")
	if err != nil || found {
		return false
	}
	_, err = verifyOnlineProfilePage(ctx, browser, original, fragment)
	return err == nil
}
