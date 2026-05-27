"""本文件负责管理页面元素临时引用。"""

from __future__ import annotations

import secrets
import threading
from dataclasses import dataclass
from typing import Any


@dataclass
class ElementRefEntry:
    """记录单个元素引用对应的 Locator。"""

    locator: Any
    index: int


class ElementRefStore:
    """管理当前浏览器上下文中的元素引用。"""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._items: dict[str, ElementRefEntry] = {}

    def clear(self) -> None:
        with self._lock:
            self._items.clear()

    def register(self, locator: Any, index: int) -> dict[str, int | str]:
        ref = f"el_{secrets.token_hex(12)}"
        with self._lock:
            self._items[ref] = ElementRefEntry(locator=locator, index=index)
        return {"ref": ref, "index": index}

    def get(self, ref: str) -> ElementRefEntry | None:
        with self._lock:
            return self._items.get(ref)


ELEMENT_REFS = ElementRefStore()
