// 本文件验证测试和正式环境的站内链接隔离，以及自定义外部地址的保留。
package httpapi

import "testing"

// TestEnvironmentPublicURL 验证历史域名迁移时不会误改第三方相似域名。
func TestEnvironmentPublicURL(t *testing.T) {
	for _, target := range []string{"https://goodhr.58it.cn", "https://goodhr-test.ai.58it.cn"} {
		t.Run(target, func(t *testing.T) {
			t.Setenv("GOODHR_PUBLIC_BASE_URL", target+"/")
			for _, source := range []string{"https://goodhr5.58it.cn", "https://goodhr.ai.58it.cn", "https://goodhr.58it.cn", "https://goodhr-test.ai.58it.cn"} {
				if got := environmentPublicURL(source + "/admin?invite=abc"); got != target+"/admin?invite=abc" {
					t.Fatalf("环境链接错误: %s", got)
				}
			}
			for _, external := range []string{"", "https://custom.example/api", "https://goodhr.58it.cn.example/api"} {
				if got := environmentPublicURL(external); got != external {
					t.Fatalf("外部链接被改写: %s", got)
				}
			}
		})
	}
}
