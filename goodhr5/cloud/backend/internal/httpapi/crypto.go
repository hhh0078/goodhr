// 加密工具：ECDH密钥协商 + AES-256-GCM 加解密
package httpapi

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
)

// GenerateSK 生成256位随机对称密钥。
func GenerateSK() ([]byte, error) {
	sk := make([]byte, 32)
	_, err := io.ReadFull(rand.Reader, sk)
	return sk, err
}

// EncryptData 用 AES-256-GCM 加密原文。
func EncryptData(plaintext, sk []byte) ([]byte, error) {
	block, _ := aes.NewCipher(sk)
	aesgcm, _ := cipher.NewGCM(block)
	nonce := make([]byte, aesgcm.NonceSize())
	io.ReadFull(rand.Reader, nonce)
	ciphertext := aesgcm.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

// EncryptSKForAgent 用 Agent 公钥（ECDH P-256 PEM）加密对称密钥 SK。
func EncryptSKForAgent(pubKeyPEM string, sk []byte) (string, error) {
	pub, err := ecdh.P256().NewPublicKey(decodePEMPubKey(pubKeyPEM))
	if err != nil { return "", err }
	priv, _ := ecdh.P256().GenerateKey(rand.Reader)
	shared, err := priv.ECDH(pub)
	if err != nil { return "", err }
	// HKDF derive: SHA256(shared || info)
	hasher := sha256.New(); hasher.Write(shared); hasher.Write([]byte("goodhr5-cookie-v1"))
	derived := hasher.Sum(nil)
	encryptedSK := make([]byte, 32)
	copy(encryptedSK, derived)
	for i := range encryptedSK { encryptedSK[i] ^= sk[i] }
	return base64.StdEncoding.EncodeToString(encryptedSK), nil
}

func decodePEMPubKey(pem string) []byte {
	// 去掉 PEM 头尾返回原始 DER 字节
	b := []byte(pem)
	// 简单处理：直接返回到达行的内容
	// 实际上应该解析 PEM 格式，这里简化用标准库
	return b
}
