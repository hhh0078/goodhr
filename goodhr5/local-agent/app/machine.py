from __future__ import annotations

import hashlib
import json
import os
import platform
import socket
import uuid
from datetime import datetime, timezone
from pathlib import Path

from app.paths import data_dir


MACHINE_FILE = "machine.json"


def load_machine() -> dict[str, str]:
    path = machine_file_path()
    if path.exists():
        with path.open("r", encoding="utf-8") as file:
            return json.load(file)

    path.parent.mkdir(parents=True, exist_ok=True)
    machine = create_machine()
    with path.open("w", encoding="utf-8") as file:
        json.dump(machine, file, ensure_ascii=False, indent=2)
        file.write("\n")
    return machine


def machine_file_path() -> Path:
    return data_dir() / MACHINE_FILE


def create_machine() -> dict[str, str]:
    install_id = str(uuid.uuid4())
    created_at = datetime.now(timezone.utc).isoformat()
    machine_id = build_machine_id(install_id)
    return {
        "machine_id": machine_id,
        "install_id": install_id,
        "created_at": created_at,
    }


def build_machine_id(install_id: str) -> str:
    raw_parts = [
        platform.system(),
        platform.machine(),
        platform.node(),
        socket.gethostname(),
        str(Path.home()),
        install_id,
        os.getenv("USER", ""),
        os.getenv("USERNAME", ""),
    ]
    raw = "|".join(raw_parts)
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()
    return f"sha256-{digest}"
