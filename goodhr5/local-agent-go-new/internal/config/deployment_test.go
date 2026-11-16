// 本文件验证私有客户端只从指定服务同源下载，普通版HTTPS策略不变，三个桌面目标共用同一规则。
package config

import "testing"

// TestPrivateDownloadOriginIsolation 验证端口、协议、凭证及相似域名不能绕过内网下载来源检查。
func TestPrivateDownloadOriginIsolation(t *testing.T) {
	previous := DefaultDeploymentMode
	t.Cleanup(func() { DefaultDeploymentMode = previous })
	DefaultDeploymentMode = "private"
	t.Setenv("GOODHR_CLOUD_API_BASE", "http://192.168.1.20:8080")
	if err := ValidateDownloadURL("http://192.168.1.20:8080/downloads/runtime.zip"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOODHR_CLOUD_API_BASE", "https://HR.EXAMPLE.LOCAL:443")
	if ServiceOrigin() != "https://hr.example.local" || ValidateDownloadURL("https://hr.example.local/runtime.zip") != nil {
		t.Fatal("默认HTTPS端口与浏览器Origin不一致")
	}
	t.Setenv("GOODHR_CLOUD_API_BASE", "http://192.168.1.20:8080")
	for _, value := range []string{"http://192.168.1.20:8081/runtime.zip", "https://192.168.1.20:8080/runtime.zip", "https://oss2.58it.cn/runtime.zip", "http://user@192.168.1.20:8080/runtime.zip", "http://192.168.1.20:8080/runtime.zip#x"} {
		if ValidateDownloadURL(value) == nil {
			t.Fatalf("来源未隔离：%s", value)
		}
	}
	DefaultDeploymentMode = "public"
	if ValidateDownloadURL("http://192.168.1.20:8080/runtime.zip") == nil {
		t.Fatal("普通版放开了HTTP")
	}
	if err := ValidateDownloadURL("https://oss2.58it.cn/runtime.zip"); err != nil {
		t.Fatal(err)
	}
}
