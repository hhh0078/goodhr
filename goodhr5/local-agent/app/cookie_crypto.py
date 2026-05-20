"""本文件负责 Local Agent 侧 cookie 数据密钥解封和 AES-GCM 解密。"""

from __future__ import annotations

import base64
import json

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF


COOKIE_KEY_INFO = b"goodhr5-cookie-v1"


def decrypt_wrapped_key(private_key_pem: str, wrapped_key_json: str) -> bytes:
    """
    使用本机私钥解封云端为当前 Agent 加密的数据密钥。

    Args:
        private_key_pem: Local Agent 本地私钥 PEM。
        wrapped_key_json: 云端保存的 WrappedCookieKey JSON 字符串。

    Returns:
        返回 32 字节 cookie 数据密钥。
    """
    wrapped = json.loads(wrapped_key_json)
    ephemeral_public_key = base64.b64decode(wrapped["ephemeral_public_key"])
    encrypted_key = base64.b64decode(wrapped["encrypted_key"])
    private_key = serialization.load_pem_private_key(private_key_pem.encode(), password=None)
    peer_key = ec.EllipticCurvePublicKey.from_encoded_point(ec.SECP256R1(), ephemeral_public_key)
    shared = private_key.exchange(ec.ECDH(), peer_key)
    wrap_key = HKDF(algorithm=hashes.SHA256(), length=32, salt=None, info=COOKIE_KEY_INFO).derive(shared)
    return decrypt_aes_gcm(encrypted_key, wrap_key)


def decrypt_aes_gcm(encrypted_data: bytes, key: bytes) -> bytes:
    """
    解密 nonce+ciphertext 格式的 AES-GCM 密文。

    Args:
        encrypted_data: nonce 和 ciphertext 拼接后的密文。
        key: 32 字节 AES-GCM 密钥。

    Returns:
        返回明文字节。
    """
    nonce = encrypted_data[:12]
    ciphertext = encrypted_data[12:]
    return AESGCM(key).decrypt(nonce, ciphertext, None)
