// 本文件定义仅由服务端环境开启的私有部署策略，并初始化无需公网验证码的管理员账号。
package httpapi

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// privateDeployment 判断是否由部署管理员明确开启私有部署，默认保留线上限制。
func privateDeployment() bool {
	return os.Getenv("GOODHR_DEPLOYMENT_MODE") == "private"
}

// bootstrapPrivateAdmin 仅为尚无密码的私有管理员设置初始密码，重启时不覆盖已有密码。
func bootstrapPrivateAdmin(passwords PasswordStore, admins []string) error {
	if !privateDeployment() || strings.TrimSpace(os.Getenv("GOODHR_PRIVATE_ADMIN_PASSWORD")) == "" {
		return nil
	}
	if len(admins) != 1 {
		return fmt.Errorf("私有部署初始化时需要配置一个有效管理员邮箱")
	}
	email, ok := normalizeEmail(admins[0])
	if !ok {
		return fmt.Errorf("私有部署管理员邮箱格式不正确")
	}
	_, set, err := passwords.AccountStatus(email)
	if err != nil || set {
		return err
	}
	password := os.Getenv("GOODHR_PRIVATE_ADMIN_PASSWORD")
	if err := validateNewPassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return passwords.SavePasswordHash(email, string(hash))
}
