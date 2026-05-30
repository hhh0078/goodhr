"""本文件负责初始化本地浏览器 profile 的基础偏好设置。"""

from __future__ import annotations

import json
import logging
from pathlib import Path


logger = logging.getLogger("goodhr5.browser_profile")

BING_SEARCH_GUID = "485bf7d3-0215-45af-87dc-538868000003"
BING_SEARCH_PROVIDER = {
    "enabled": True,
    "encoding": "UTF-8",
    "favicon_url": "https://www.bing.com/favicon.ico",
    "guid": BING_SEARCH_GUID,
    "id": 1,
    "keyword": "bing.com",
    "name": "Bing",
    "reset_occurred": False,
    "search_url": "https://www.bing.com/search?q={searchTerms}",
    "suggest_url": "https://www.bing.com/osjson.aspx?query={searchTerms}",
}
BING_TEMPLATE_URL_DATA = {
    "alternate_urls": [],
    "contextual_search_url": "",
    "created_from_play_api": False,
    "date_created": "0",
    "doodle_url": "",
    "enforced_by_policy": False,
    "favicon_url": "https://www.bing.com/sa/simg/bing_p_rr_teal_min.ico",
    "featured_by_policy": False,
    "id": "3",
    "image_search_branding_label": "",
    "image_translate_source_language_param_key": "",
    "image_translate_target_language_param_key": "",
    "image_translate_url": "",
    "image_url": "https://www.bing.com/images/detail/search?iss=sbiupload&FORM=CHROMI#enterInsights",
    "image_url_post_params": "imageBin={google:imageThumbnailBase64}",
    "input_encodings": ["UTF-8"],
    "is_active": 0,
    "keyword": "bing.com",
    "last_modified": "0",
    "last_visited": "0",
    "logo_url": "https://cdn.sapphire.microsoftapp.net/icons/bing_144.png",
    "new_tab_url": "https://www.bing.com/chrome/newtab",
    "originating_url": "",
    "policy_origin": 0,
    "preconnect_to_search_url": False,
    "prefetch_likely_navigations": False,
    "prepopulate_id": 3,
    "safe_for_autoreplace": True,
    "search_intent_params": [],
    "search_url_post_params": "",
    "short_name": "Microsoft Bing",
    "starter_pack_id": 0,
    "suggestions_url": "https://www.bing.com/osjson.aspx?query={searchTerms}&language={language}",
    "suggestions_url_post_params": "",
    "synced_guid": BING_SEARCH_GUID,
    "url": "https://www.bing.com/search?q={searchTerms}",
    "usage_count": 0,
}


def configure_default_bing_search(user_data_dir: str) -> None:
    """
    将浏览器 profile 的默认搜索引擎初始化为必应。

    Args:
        user_data_dir: 浏览器用户数据目录路径。
    """
    if not user_data_dir:
        return
    prefs_path = Path(user_data_dir) / "Default" / "Preferences"
    prefs = _read_preferences(prefs_path)
    prefs["default_search_provider"] = BING_SEARCH_PROVIDER.copy()
    prefs["default_search_provider_data"] = {
        "mirrored_template_url_data": BING_TEMPLATE_URL_DATA.copy(),
    }
    _write_preferences(prefs_path, prefs)
    logger.info("已初始化默认搜索引擎为必应: %s", prefs_path)


def _read_preferences(prefs_path: Path) -> dict:
    """
    读取 Chromium Preferences 文件，不存在或损坏时返回空配置。

    Args:
        prefs_path: Preferences 文件路径。

    Returns:
        dict: 浏览器偏好设置。
    """
    if not prefs_path.exists():
        return {}
    try:
        with prefs_path.open("r", encoding="utf-8") as file:
            data = json.load(file)
        return data if isinstance(data, dict) else {}
    except (OSError, json.JSONDecodeError) as exc:
        logger.warning("读取浏览器 Preferences 失败，将重建默认搜索配置: %s", exc)
        return {}


def _write_preferences(prefs_path: Path, prefs: dict) -> None:
    """
    写入 Chromium Preferences 文件。

    Args:
        prefs_path: Preferences 文件路径。
        prefs: 浏览器偏好设置。
    """
    prefs_path.parent.mkdir(parents=True, exist_ok=True)
    with prefs_path.open("w", encoding="utf-8") as file:
        json.dump(prefs, file, ensure_ascii=False, separators=(",", ":"))
