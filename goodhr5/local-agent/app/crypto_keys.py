"""ECDH密钥对管理"""
import os, json
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.hazmat.primitives import serialization
from app.paths import data_dir

KEY_FILE = data_dir() / "crypto_key.json"

def load_or_generate():
    if KEY_FILE.exists(): return json.loads(KEY_FILE.read_text())
    sk = ec.generate_private_key(ec.SECP256R1())
    pk = sk.public_key()
    sk_pem = sk.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()).decode()
    pk_pem = pk.public_bytes(serialization.Encoding.PEM, serialization.PublicFormat.SubjectPublicKeyInfo).decode()
    data = {"private_key": sk_pem, "public_key": pk_pem}
    KEY_FILE.write_text(json.dumps(data, indent=2))
    return data
