// 本文件负责微信消息签名及安全模式 AES 编解码，严格校验密钥、填充和 AppID。
package httpapi

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
)

// wechatMessageSignature 按微信协议排序参数并生成 SHA1 签名。
func wechatMessageSignature(parts ...string) string {
	values := append([]string(nil), parts...)
	sort.Strings(values)
	digest := sha1.Sum([]byte(strings.Join(values, "")))
	return hex.EncodeToString(digest[:])
}

// verifyWechatMessageSignature 使用常量时间比较验证签名，拒绝空签名。
func verifyWechatMessageSignature(signature string, parts ...string) bool {
	expected := wechatMessageSignature(parts...)
	return len(signature) == len(expected) && subtle.ConstantTimeCompare([]byte(signature), []byte(expected)) == 1
}

// wechatAESKey 将微信 43 位 EncodingAESKey 解析为 32 字节 AES 密钥。
func wechatAESKey(encodedKey string) ([]byte, error) {
	if len(encodedKey) != 43 {
		return nil, errors.New("微信消息密钥长度不匹配")
	}
	key, err := base64.StdEncoding.DecodeString(encodedKey + "=")
	if err != nil || len(key) != 32 {
		return nil, errors.New("微信消息密钥格式不匹配")
	}
	return key, nil
}

// decryptWechatMessage 解密安全模式数据并验证 PKCS7、消息长度和目标 AppID。
func decryptWechatMessage(encoded, encodedKey, appID string) ([]byte, error) {
	key, err := wechatAESKey(encodedKey)
	if err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	// 微信协议按32字节填充，不能接受只有普通AES 16字节对齐的其他协议密文。
	if err != nil || len(data) == 0 || len(data)%32 != 0 {
		return nil, errors.New("微信加密消息格式不匹配")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, data)
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > 32 || padding > len(plain) {
		return nil, errors.New("微信加密消息填充不匹配")
	}
	for _, value := range plain[len(plain)-padding:] {
		if int(value) != padding {
			return nil, errors.New("微信加密消息填充不匹配")
		}
	}
	plain = plain[:len(plain)-padding]
	if len(plain) < 20 {
		return nil, errors.New("微信加密消息长度不匹配")
	}
	length := uint64(binary.BigEndian.Uint32(plain[16:20]))
	if length > uint64(len(plain)-20) {
		return nil, errors.New("微信加密消息长度不匹配")
	}
	end := 20 + int(length)
	if !bytes.Equal(plain[end:], []byte(appID)) || appID == "" {
		return nil, errors.New("微信加密消息目标不匹配")
	}
	return append([]byte(nil), plain[20:end]...), nil
}

// encryptWechatMessage 生成微信要求的随机前缀及 32 字节填充后加密回复。
func encryptWechatMessage(message []byte, encodedKey, appID string) (string, error) {
	key, err := wechatAESKey(encodedKey)
	if err != nil {
		return "", err
	}
	if len(message) > 1<<20 || appID == "" {
		return "", errors.New("微信回复长度或目标不匹配")
	}
	plain := make([]byte, 20, 20+len(message)+len(appID)+32)
	if _, err := rand.Read(plain[:16]); err != nil {
		return "", err
	}
	binary.BigEndian.PutUint32(plain[16:20], uint32(len(message)))
	plain = append(plain, message...)
	plain = append(plain, appID...)
	padding := 32 - len(plain)%32
	plain = append(plain, bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	data := make([]byte, len(plain))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(data, plain)
	return base64.StdEncoding.EncodeToString(data), nil
}
