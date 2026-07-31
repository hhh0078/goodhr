// 本文件负责测试云端后端环境变量配置读取。
package httpapi

import "testing"

// TestLoadConfigFromEnvReadsUniversalLoginCodeOffset 验证万能验证码偏移分钟数来自环境变量。
func TestLoadConfigFromEnvReadsUniversalLoginCodeOffset(t *testing.T) {
	t.Setenv("GOODHR_UNIVERSAL_LOGIN_CODE_OFFSET_MINUTES", "5")
	config := LoadConfigFromEnv()
	if config.UniversalLoginCodeOffsetMin != 5 {
		t.Fatalf("UniversalLoginCodeOffsetMin = %d, want 5", config.UniversalLoginCodeOffsetMin)
	}
}
