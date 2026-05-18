from __future__ import annotations

import json
from datetime import datetime, timezone
from pathlib import Path

from app.paths import data_dir


CLOUD_ACCOUNT_FILE = "cloud_account.json"


def load_cloud_account() -> dict[str, str] | None:
    path = cloud_account_path()
    if not path.exists():
        return None

    with path.open("r", encoding="utf-8") as file:
        return json.load(file)


def save_cloud_account(cloud_user_id: str, cloud_email: str, agent_token: str) -> dict[str, str]:
    account = {
        "cloud_user_id": cloud_user_id,
        "cloud_email": cloud_email,
        "agent_token": agent_token,
        "bound_at": datetime.now(timezone.utc).isoformat(),
    }

    path = cloud_account_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as file:
        json.dump(account, file, ensure_ascii=False, indent=2)
        file.write("\n")

    return account


def cloud_account_path() -> Path:
    return data_dir() / CLOUD_ACCOUNT_FILE
