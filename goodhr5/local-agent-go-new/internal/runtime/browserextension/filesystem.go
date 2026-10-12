// 文件作用：管理浏览器插件发布收据，并原子切换已校验的插件目录。
package browserextension

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// fileSHA256 流式计算已保存签名插件的摘要，供幂等判断核对原始 XPI。
func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("签名插件文件不是普通文件：%s", path)
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type releaseReceipt struct {
	Version    string `json:"version"`
	SHA256     string `json:"sha256"`
	TreeSHA256 string `json:"tree_sha256"`
}

// readReceipt 读取上一次已校验插件的版本和压缩包摘要收据。
func readReceipt(receiptPath string) (releaseReceipt, error) {
	content, err := os.ReadFile(receiptPath)
	if err != nil {
		return releaseReceipt{}, err
	}
	var receipt releaseReceipt
	if err = json.Unmarshal(content, &receipt); err != nil {
		return releaseReceipt{}, err
	}
	receipt.Version = strings.TrimSpace(receipt.Version)
	receipt.SHA256 = strings.ToLower(strings.TrimSpace(receipt.SHA256))
	receipt.TreeSHA256 = strings.ToLower(strings.TrimSpace(receipt.TreeSHA256))
	if receipt.Version == "" || validateSHA256(receipt.SHA256) != nil || validateSHA256(receipt.TreeSHA256) != nil {
		return releaseReceipt{}, fmt.Errorf("插件发布收据不完整")
	}
	return receipt, nil
}

// writeReceipt 在临时目录写入已验证版本和摘要，供同版本幂等判断。
func writeReceipt(receiptPath string, receipt releaseReceipt) error {
	content, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("生成插件发布收据失败：%w", err)
	}
	if err = os.WriteFile(receiptPath, append(content, '\n'), 0o644); err != nil {
		return fmt.Errorf("写入插件发布收据失败：%w", err)
	}
	return nil
}

// replaceDirectory 用同盘重命名原子切换插件目录，失败时恢复旧目录。
func replaceDirectory(target string, staging string, rename func(string, string) error) error {
	backup := target + ".previous"
	_, targetErr := os.Stat(target)
	targetExists := targetErr == nil
	if targetErr != nil && !os.IsNotExist(targetErr) {
		return fmt.Errorf("检查旧插件目录失败：%w", targetErr)
	}
	_, backupErr := os.Stat(backup)
	backupExists := backupErr == nil
	if backupErr != nil && !os.IsNotExist(backupErr) {
		return fmt.Errorf("检查插件备份目录失败：%w", backupErr)
	}
	if !targetExists && backupExists {
		if err := rename(backup, target); err != nil {
			return fmt.Errorf("恢复上次中断的插件目录失败：%w", err)
		}
		targetExists = true
		backupExists = false
	}
	if backupExists {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("清理插件备份目录失败：%w", err)
		}
	}
	if targetExists {
		if err := rename(target, backup); err != nil {
			return fmt.Errorf("备份旧插件目录失败：%w", err)
		}
	}
	if err := rename(staging, target); err != nil {
		if targetExists {
			if rollbackErr := rename(backup, target); rollbackErr != nil {
				return fmt.Errorf("切换插件目录失败：%v；恢复旧目录也失败：%w", err, rollbackErr)
			}
		}
		return fmt.Errorf("切换插件目录失败：%w", err)
	}
	if targetExists {
		if err := os.RemoveAll(backup); err != nil {
			return fmt.Errorf("插件已更新，但清理旧目录失败：%w", err)
		}
	}
	return nil
}
