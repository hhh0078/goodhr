"""本文件负责 Local Agent 侧 cookie 数据密钥解封、AES-GCM 解密和 cookie 载荷解析。"""

from __future__ import annotations

import base64
import json
from typing import Any

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


def decrypt_cookie_payload(
    private_key_pem: str,
    machine_id: str,
    encrypted_data_b64: str,
    encrypted_keys: dict[str, Any],
) -> list[dict[str, Any]]:
    """
    按当前机器 machine_id 选择对应密钥并解密 cookie 列表。

    Args:
        private_key_pem: Local Agent 本地私钥 PEM。
        machine_id: 当前机器标识，用于挑选 encrypted_keys 中对应条目。
        encrypted_data_b64: Base64 编码的 cookie 密文。
        encrypted_keys: 云端返回的 machine_id -> wrapped key 映射。

    Returns:
        返回解密后的 cookies 列表。
    """
    if not encrypted_data_b64:
        raise ValueError("encrypted_data is required")
    if not machine_id:
        raise ValueError("machine_id is required")
    if not isinstance(encrypted_keys, dict) or not encrypted_keys:
        raise ValueError("encrypted_keys is required")

    wrapped_key = encrypted_keys.get(machine_id)
    if not wrapped_key:
        raise ValueError("当前机器未找到可用 cookie 密钥")

    sk = decrypt_wrapped_key(private_key_pem, str(wrapped_key))
    plaintext = decrypt_aes_gcm(base64.b64decode(encrypted_data_b64), sk)
    data = json.loads(plaintext.decode("utf-8"))
    if not isinstance(data, list):
        raise ValueError("cookie 数据格式不正确")
    return data
