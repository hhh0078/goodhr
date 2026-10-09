// 本文件用固定测试密钥和独立协议夹具验证微信签名、AES 加解密及恶意消息边界，不使用真实公众号凭据。
package httpapi

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const wechatTestAppID = "wx0123456789abcdef"

// wechatTestAESKey 生成公开、固定的 32 字节测试密钥，避免引用任何真实用户密钥。
func wechatTestAESKey() string {
	key := make([]byte, 32)
	for index := range key {
		key[index] = byte(index)
	}
	return base64.RawStdEncoding.EncodeToString(key)
}

// wechatTestSignature 独立按官方协议计算签名，避免回调测试依赖被测签名方法。
func wechatTestSignature(parts ...string) string {
	values := append([]string(nil), parts...)
	sort.Strings(values)
	digest := sha1.Sum([]byte(strings.Join(values, "")))
	return hex.EncodeToString(digest[:])
}

// wechatTestFrame 构造确定性的微信协议明文，不调用被测加密方法。
func wechatTestFrame(message []byte, appID string) []byte {
	frame := make([]byte, 20)
	copy(frame, "0123456789abcdef")
	binary.BigEndian.PutUint32(frame[16:20], uint32(len(message)))
	frame = append(frame, message...)
	return append(frame, appID...)
}

// wechatTestEncryptFrame 为协议夹具添加 32 字节填充并独立加密，支持构造篡改后的明文。
func wechatTestEncryptFrame(t *testing.T, frame []byte) string {
	t.Helper()
	padding := 32 - len(frame)%32
	frame = append(append([]byte(nil), frame...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	return wechatTestEncryptPadded(t, frame)
}

// wechatTestEncryptPadded 加密已准备的填充数据，便于测试无效填充是否被正确拒绝。
func wechatTestEncryptPadded(t *testing.T, padded []byte) string {
	t.Helper()
	key, err := base64.RawStdEncoding.DecodeString(wechatTestAESKey())
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(data, padded)
	return base64.StdEncoding.EncodeToString(data)
}

// wechatTestDecodeReply 独立解析被测加密回复，核验协议帧、填充及目标 AppID。
func wechatTestDecodeReply(t *testing.T, encoded, appID string) []byte {
	t.Helper()
	key, err := base64.RawStdEncoding.DecodeString(wechatTestAESKey())
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data)%32 != 0 {
		t.Fatalf("回复密文不是 32 字节协议填充：长度=%d，错误=%v", len(data), err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, key[:aes.BlockSize]).CryptBlocks(plain, data)
	padding := int(plain[len(plain)-1])
	if padding < 1 || padding > 32 || !bytes.Equal(plain[len(plain)-padding:], bytes.Repeat([]byte{byte(padding)}, padding)) {
		t.Fatal("回复的 PKCS7 填充不符合微信协议")
	}
	plain = plain[:len(plain)-padding]
	if len(plain) < 20 {
		t.Fatal("回复缺少随机前缀和消息长度")
	}
	length := int(binary.BigEndian.Uint32(plain[16:20]))
	if length > len(plain)-20 || string(plain[20+length:]) != appID {
		t.Fatal("回复的消息长度或 AppID 不匹配")
	}
	return plain[20 : 20+length]
}

// TestWechatCryptoOfficialSignatureVector 验证官方签名示例及参数排序，拒绝空、截断和篡改签名。
func TestWechatCryptoOfficialSignatureVector(t *testing.T) {
	const expected = "899cf89e464efb63f54ddac96b0a0a235f53aa78"
	parts := []string{"AAAAA", "1714037059", "486452656"}
	if got := wechatMessageSignature(parts...); got != expected {
		t.Fatalf("官方签名示例不符：got=%q", got)
	}
	if !verifyWechatMessageSignature(expected, "486452656", "AAAAA", "1714037059") {
		t.Fatal("签名校验没有正确排序参数")
	}
	for _, signature := range []string{"", expected[:39], strings.Repeat("0", 40), strings.ToUpper(expected), expected + "0"} {
		if verifyWechatMessageSignature(signature, parts...) {
			t.Fatalf("错误签名被接受：%q", signature)
		}
	}
	if verifyWechatMessageSignature(expected, "AAAAA", "1714037059", "different-nonce") {
		t.Fatal("被篡改的 nonce 被接受")
	}
}

// TestWechatCryptoAESKeyValidation 验证密钥严格为 43 位 Base64 编码的 32 字节值。
func TestWechatCryptoAESKeyValidation(t *testing.T) {
	key, err := wechatAESKey(wechatTestAESKey())
	if err != nil || len(key) != 32 || key[31] != 31 {
		t.Fatalf("测试密钥解析失败：key=%v，错误=%v", key, err)
	}
	for _, encoded := range []string{"", "short", wechatTestAESKey() + "=", strings.Repeat("!", 43), wechatTestAESKey()[:42]} {
		if _, err := wechatAESKey(encoded); err == nil {
			t.Fatalf("错误密钥被接受：%q", encoded)
		}
	}
}

// TestWechatCryptoIndependentRoundTrip 验证独立夹具解密及回复独立解码，覆盖中文、空消息和完整 32 字节填充。
func TestWechatCryptoIndependentRoundTrip(t *testing.T) {
	for _, message := range []string{"", "<xml><Content>中文 &amp; 字符</Content></xml>", strings.Repeat("x", 26)} {
		t.Run("length_"+strconv.Itoa(len(message)), func(t *testing.T) {
			encoded := wechatTestEncryptFrame(t, wechatTestFrame([]byte(message), wechatTestAppID))
			plain, err := decryptWechatMessage(encoded, wechatTestAESKey(), wechatTestAppID)
			if err != nil || string(plain) != message {
				t.Fatalf("独立协议消息解密不符：got=%q，错误=%v", plain, err)
			}
			reply, err := encryptWechatMessage([]byte(message), wechatTestAESKey(), wechatTestAppID)
			if err != nil || string(wechatTestDecodeReply(t, reply, wechatTestAppID)) != message {
				t.Fatalf("加密回复不符：错误=%v", err)
			}
		})
	}
	first, err := encryptWechatMessage([]byte("same-message"), wechatTestAESKey(), wechatTestAppID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := encryptWechatMessage([]byte("same-message"), wechatTestAESKey(), wechatTestAppID)
	if err != nil || first == second {
		t.Fatalf("加密回复没有生成独立随机前缀：错误=%v", err)
	}
}

// TestWechatCryptoRejectMalformedFrames 验证密文格式、AppID、消息长度及每个填充字节均严格校验且不发生越界。
func TestWechatCryptoRejectMalformedFrames(t *testing.T) {
	wrongLength := wechatTestFrame([]byte("message"), wechatTestAppID)
	binary.BigEndian.PutUint32(wrongLength[16:20], ^uint32(0))
	zeroPadding := bytes.Repeat([]byte{0}, 32)
	largePadding := bytes.Repeat([]byte{33}, 32)
	inconsistentPadding := bytes.Repeat([]byte{2}, 32)
	inconsistentPadding[30] = 3
	nonWechatPadding := wechatTestFrame([]byte("short"), wechatTestAppID)
	nonWechatPadding = append(nonWechatPadding, bytes.Repeat([]byte{5}, 5)...)
	for _, test := range []struct {
		name    string
		encoded string
		appID   string
	}{
		{"empty_cipher", "", wechatTestAppID},
		{"invalid_base64", "%%%", wechatTestAppID},
		{"short_cipher", base64.StdEncoding.EncodeToString([]byte{1}), wechatTestAppID},
		{"zero_padding", wechatTestEncryptPadded(t, zeroPadding), wechatTestAppID},
		{"large_padding", wechatTestEncryptPadded(t, largePadding), wechatTestAppID},
		{"inconsistent_padding", wechatTestEncryptPadded(t, inconsistentPadding), wechatTestAppID},
		{"non_wechat_block_padding", wechatTestEncryptPadded(t, nonWechatPadding), wechatTestAppID},
		{"short_frame", wechatTestEncryptFrame(t, []byte("short")), wechatTestAppID},
		{"overflow_length", wechatTestEncryptFrame(t, wrongLength), wechatTestAppID},
		{"wrong_appid", wechatTestEncryptFrame(t, wechatTestFrame([]byte("message"), "wx1111111111111111")), wechatTestAppID},
		{"missing_appid", wechatTestEncryptFrame(t, wechatTestFrame([]byte("message"), "")), wechatTestAppID},
		{"empty_expected_appid", wechatTestEncryptFrame(t, wechatTestFrame([]byte("message"), "")), ""},
		{"extra_appid_bytes", wechatTestEncryptFrame(t, wechatTestFrame([]byte("message"), wechatTestAppID+"extra")), wechatTestAppID},
	} {
		t.Run(test.name, func(t *testing.T) {
			if plain, err := decryptWechatMessage(test.encoded, wechatTestAESKey(), test.appID); err == nil || plain != nil {
				t.Fatalf("无效协议帧被接受：plain=%q，错误=%v", plain, err)
			}
		})
	}
}

// TestWechatCryptoEncryptRejectsInvalidInput 验证超长回复、缺失 AppID 和无效密钥不能生成密文。
func TestWechatCryptoEncryptRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct {
		message []byte
		key     string
		appID   string
	}{
		{bytes.Repeat([]byte("x"), (1<<20)+1), wechatTestAESKey(), wechatTestAppID},
		{[]byte("message"), wechatTestAESKey(), ""},
		{[]byte("message"), "invalid-key", wechatTestAppID},
	} {
		if encoded, err := encryptWechatMessage(test.message, test.key, test.appID); err == nil || encoded != "" {
			t.Fatal("错误回复参数被接受")
		}
	}
}
