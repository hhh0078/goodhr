"""本文件负责本地提示音下载缓存与播放。"""

from __future__ import annotations

import hashlib
import shutil
import subprocess
from pathlib import Path
from urllib.parse import urlparse

import httpx

from app.paths import APP_ROOT


AUDIO_DIR = APP_ROOT / "assets" / "audio"
DEFAULT_SUCCESS = AUDIO_DIR / "success.mp3"
DEFAULT_FAILED = AUDIO_DIR / "failed.mp3"


def ensure_audio_dir() -> Path:
    AUDIO_DIR.mkdir(parents=True, exist_ok=True)
    return AUDIO_DIR


def resolve_builtin_audio(kind: str) -> Path:
    key = (kind or "").strip().lower()
    if key == "success":
        return DEFAULT_SUCCESS
    if key == "failed":
        return DEFAULT_FAILED
    raise ValueError("kind must be success or failed")


async def ensure_audio_from_url(url: str) -> Path:
    parsed = urlparse(url)
    name = Path(parsed.path).name.strip()
    if not name:
        digest = hashlib.sha1(url.encode("utf-8")).hexdigest()[:24]
        name = f"{digest}.mp3"
    target = ensure_audio_dir() / name
    if target.exists():
        return target

    async with httpx.AsyncClient(timeout=30) as client:
        resp = await client.get(url)
        resp.raise_for_status()
        target.write_bytes(resp.content)
    return target


def play_once(path: Path) -> None:
    if not path.exists():
        raise FileNotFoundError(f"audio file not found: {path}")
    player = _pick_player()
    if not player:
        raise RuntimeError("no supported audio player found (afplay/mpg123/ffplay)")
    if player == "ffplay":
        subprocess.run([player, "-nodisp", "-autoexit", "-loglevel", "quiet", str(path)], check=True)
    else:
        subprocess.run([player, str(path)], check=True)


def _pick_player() -> str:
    for cmd in ("afplay", "mpg123", "ffplay"):
        if shutil.which(cmd):
            return cmd
    return ""
