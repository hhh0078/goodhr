from __future__ import annotations

import os
from pathlib import Path


APP_ROOT = Path(__file__).resolve().parents[1]


def data_dir() -> Path:
    configured = os.getenv("GOODHR_AGENT_DATA_DIR")
    if configured:
        return Path(configured).expanduser().resolve()
    return APP_ROOT / "agent_data"
