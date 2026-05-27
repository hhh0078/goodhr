// 本文件负责 cookie 共享所需的 ECDH 密钥封装和 AES-GCM 数据加密。
package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const cookieKeyInfo = "goodhr5-cookie-v1"

// WrappedCookieKey 表示为单台 Local Agent 加密后的数据密钥。
type WrappedCookieKey struct {
	EphemeralPublicKey string `json:"ephemeral_public_key"`
	EncryptedKey       string `json:"encrypted_key"`
}

// GenerateSK 生成 256 位随机对称数据密钥。
func GenerateSK() ([]byte, error) {
	sk := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, sk)
	return sk, err
}

// EncryptData 用 AES-256-GCM 加密原文。
// 返回值为 nonce+ciphertext，便于数据库只保存一个 BYTEA 字段。
func EncryptData(plaintext, sk []byte) ([]byte, error) {
	block, err := aes.NewCipher(sk)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aesgcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aesgcm.Seal(nonce, nonce, plaintext, nil), nil
}

// EncryptSKForAgent 用 Local Agent 公钥封装数据密钥。
// 返回 JSON 字符串，包含临时公钥和 AES-GCM 加密后的数据密钥。
func EncryptSKForAgent(pubKeyPEM string, sk []byte) (string, error) {
	pub, err := parseAgentPublicKey(pubKeyPEM)
	if err != nil {
		return "", err
	}
	ephemeral, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	shared, err := ephemeral.ECDH(pub)
	if err != nil {
		return "", err
	}
	wrapKey, err := deriveCookieWrapKey(shared)
	if err != nil {
		return "", err
	}
	encryptedSK, err := EncryptData(sk, wrapKey)
	if err != nil {
		return "", err
	}
	wrapped := WrappedCookieKey{
		EphemeralPublicKey: base64.StdEncoding.EncodeToString(ephemeral.PublicKey().Bytes()),
		EncryptedKey:       base64.StdEncoding.EncodeToString(encryptedSK),
	}
	data, err := json.Marshal(wrapped)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseAgentPublicKey 解析 Local Agent 上报的 PEM 公钥。
// 当前只接受 P-256 ECDH/ECDSA 公钥，避免错误密钥类型进入共享链路。
func parseAgentPublicKey(pubKeyPEM string) (*ecdh.PublicKey, error) {
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid public key pem")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := pubAny.(interface {
		ECDH() (*ecdh.PublicKey, error)
	})
	if !ok {
		return nil, fmt.Errorf("public key does not support ecdh")
	}
	return pub.ECDH()
}

// deriveCookieWrapKey 从 ECDH shared secret 派生 AES-GCM 包装密钥。
func deriveCookieWrapKey(shared []byte) ([]byte, error) {
	reader := hkdf.New(sha256.New, shared, nil, []byte(cookieKeyInfo))
	key := make([]byte, 32)
	_, err := io.ReadFull(reader, key)
	return key, err
}
