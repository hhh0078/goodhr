"""本文件负责 Boss 平台本地运行时的页面提取能力。"""

from __future__ import annotations

import hashlib
from typing import Any


async def extract_visible_candidates(page: Any, platform_config: dict, max_items: int = 30) -> list[dict]:
    """
    提取当前页面可见 Boss 候选人卡片。

    Args:
        page: Playwright 页面对象。
        platform_config: 云端平台配置。
        max_items: 最多提取数量。

    Returns:
        list[dict]: 候选人列表。
    """
    cards = await _visible_cards(page, max_items, platform_config)
    candidates: list[dict] = []
    for index, card in enumerate(cards):
        fields = await _extract_card_fields(card, platform_config)
        raw_text = _candidate_raw_text(fields)
        candidate_id = _candidate_id(fields, raw_text, index)
        candidates.append(
            {
                "id": candidate_id,
                "name": fields.get("name") or f"候选人{index + 1}",
                "candidate_name": fields.get("name") or f"候选人{index + 1}",
                "status": "scanned",
                "raw_text": raw_text,
                "filter_text": raw_text,
                "platform_id": "boss",
                "card_index": index,
                "fields": fields,
            }
        )
    return candidates


async def greet_candidate_by_index(page: Any, card_index: int, platform_config: dict) -> None:
    """
    点击指定候选人的 Boss 打招呼按钮。

    Args:
        page: Playwright 页面对象。
        card_index: 候选人卡片序号。
        platform_config: 云端平台配置。
    """
    card = await _card_by_index(page, card_index, platform_config)
    if hasattr(card, "scroll_into_view_if_needed"):
        await card.scroll_into_view_if_needed(timeout=1500)
    clicked = await _click_first_visible(card, _boss_selectors(platform_config, "greet_buttons"), timeout=1500)
    if not clicked:
        raise RuntimeError("未找到可点击的打招呼按钮")
    await _click_first_visible(page, _boss_selectors(platform_config, "continue_buttons", optional=True), timeout=800)
    await _click_first_visible(page, _boss_selectors(platform_config, "confirm_buttons", optional=True), timeout=800)


async def fetch_candidate_detail_text(page: Any, card_index: int, platform_config: dict) -> str:
    """
    打开指定候选人详情并提取详情文本。

    Args:
        page: Playwright 页面对象。
        card_index: 候选人卡片序号。
        platform_config: 云端平台配置。

    Returns:
        str: 详情文本。
    """
    card = await _card_by_index(page, card_index, platform_config)
    if hasattr(card, "scroll_into_view_if_needed"):
        await card.scroll_into_view_if_needed(timeout=1500)
    clicked = await _click_first_visible(
        card,
        _boss_selectors(platform_config, "detail_buttons"),
        timeout=1500,
    )
    if not clicked:
        await card.click(timeout=1500)
    await _safe_wait(page, 1200)
    text = await _first_detail_text(page, platform_config)
    await _click_first_visible(page, _boss_selectors(platform_config, "detail_close_buttons", optional=True), timeout=800)
    if not text:
        raise RuntimeError("未提取到候选人详情文本")
    return text


async def scroll_candidate_list(page: Any, platform_config: dict, distance: int = 720) -> None:
    """
    滚动 Boss 候选人列表以加载更多卡片。

    Args:
        page: Playwright 页面对象。
        platform_config: 云端平台配置。
        distance: 滚动距离。
    """
    safe_distance = max(120, int(distance or 720))
    for selector in _boss_selectors(platform_config, "scroll_containers"):
        try:
            locator = page.locator(selector).first
            if await locator.count() <= 0:
                continue
            if hasattr(locator, "is_visible") and not await locator.is_visible():
                continue
            await locator.evaluate("(el, y) => el.scrollBy(0, y)", safe_distance)
            await page.wait_for_timeout(1200)
            return
        except Exception:
            continue
    try:
        if hasattr(page, "mouse"):
            await page.mouse.wheel(0, safe_distance)
        else:
            await page.evaluate("(y) => window.scrollBy(0, y)", safe_distance)
        await page.wait_for_timeout(1200)
    except Exception:
        return


async def _card_by_index(page: Any, card_index: int, platform_config: dict) -> Any:
    """
    按序号返回候选人卡片。

    Args:
        page: Playwright 页面对象。
        card_index: 候选人卡片序号。
        platform_config: 云端平台配置。

    Returns:
        Any: 候选人卡片 locator。
    """
    safe_index = max(0, int(card_index or 0))
    locator = page.locator(", ".join(_boss_selectors(platform_config, "candidate_card")))
    count = await locator.count()
    if safe_index >= count:
        raise RuntimeError("候选人卡片已不在当前页面")
    card = locator.nth(safe_index)
    if not await card.is_visible():
        raise RuntimeError("候选人卡片当前不可见")
    return card


async def _click_first_visible(scope: Any, selectors: list[str], timeout: int = 1000) -> bool:
    """
    点击选择器列表中第一个可见元素。

    Args:
        scope: 页面或卡片 locator。
        selectors: CSS 选择器列表。
        timeout: 单次点击超时时间。

    Returns:
        bool: 点击成功返回 true。
    """
    for selector in selectors:
        try:
            locator = scope.locator(selector).first
            if await locator.count() <= 0:
                continue
            if hasattr(locator, "is_visible") and not await locator.is_visible():
                continue
            await locator.click(timeout=timeout)
            return True
        except Exception:
            continue
    return False


async def _first_detail_text(page: Any, platform_config: dict) -> str:
    """
    提取第一个可见详情容器文本。

    Args:
        page: Playwright 页面对象。
        platform_config: 云端平台配置。

    Returns:
        str: 详情文本。
    """
    for selector in _boss_selectors(platform_config, "detail_containers"):
        try:
            locator = page.locator(selector).first
            if await locator.count() <= 0:
                continue
            if hasattr(locator, "is_visible") and not await locator.is_visible():
                continue
            text = (await locator.inner_text(timeout=1500)).strip()
            if text:
                return text
        except Exception:
            continue
    return ""


async def _safe_wait(page: Any, timeout_ms: int) -> None:
    """
    安全等待页面变化。

    Args:
        page: Playwright 页面对象。
        timeout_ms: 等待毫秒数。
    """
    try:
        await page.wait_for_timeout(timeout_ms)
    except Exception:
        return


async def _visible_cards(page: Any, max_items: int, platform_config: dict) -> list[Any]:
    """
    返回当前页面可见候选人卡片元素。

    Args:
        page: Playwright 页面对象。
        max_items: 最多返回数量。
        platform_config: 云端平台配置。

    Returns:
        list[Any]: 可见卡片 locator 列表。
    """
    locator = page.locator(", ".join(_boss_selectors(platform_config, "candidate_card")))
    count = await locator.count()
    cards: list[Any] = []
    for index in range(min(count, max(1, max_items))):
        item = locator.nth(index)
        try:
            if await item.is_visible():
                cards.append(item)
        except Exception:
            continue
    return cards


async def _extract_card_fields(card: Any, platform_config: dict) -> dict[str, str]:
    """
    提取单张候选人卡片字段。

    Args:
        card: 候选人卡片 locator。
        platform_config: 云端平台配置。

    Returns:
        dict[str, str]: 字段字典。
    """
    fields: dict[str, str] = {}
    for field, selectors in _boss_field_selectors(platform_config).items():
        fields[field] = await _first_text(card, selectors)
    if not fields.get("basic_info"):
        fields["basic_info"] = await _safe_inner_text(card)
    return fields


async def _first_text(card: Any, selectors: list[str]) -> str:
    """
    返回选择器列表中第一个非空文本。

    Args:
        card: 候选人卡片 locator。
        selectors: CSS 选择器列表。

    Returns:
        str: 文本内容。
    """
    for selector in selectors:
        try:
            item = card.locator(selector).first
            if await item.count() <= 0:
                continue
            text = (await item.inner_text(timeout=800)).strip()
            if text:
                return text
        except Exception:
            continue
    return ""


async def _safe_inner_text(card: Any) -> str:
    """
    安全读取卡片完整文本。

    Args:
        card: 候选人卡片 locator。

    Returns:
        str: 卡片文本。
    """
    try:
        return (await card.inner_text(timeout=800)).strip()
    except Exception:
        return ""


def _candidate_raw_text(fields: dict[str, str]) -> str:
    """
    拼接候选人筛选文本。

    Args:
        fields: 候选人字段。

    Returns:
        str: 拼接后的文本。
    """
    keys = ["name", "basic_info", "education", "university", "description"]
    return " ".join(fields.get(key, "").strip() for key in keys if fields.get(key, "").strip()).strip()


def _candidate_id(fields: dict[str, str], raw_text: str, index: int) -> str:
    """
    生成候选人本地 ID。

    Args:
        fields: 候选人字段。
        raw_text: 候选人文本。
        index: 页面序号。

    Returns:
        str: 候选人 ID。
    """
    base = "|".join([fields.get("name", ""), raw_text, str(index)])
    digest = hashlib.sha1(base.encode("utf-8")).hexdigest()[:16]
    return f"boss_{digest}"


def _boss_field_selectors(platform_config: dict) -> dict[str, list[str]]:
    """
    返回 Boss 候选人字段选择器。

    Args:
        platform_config: 云端平台配置。

    Returns:
        dict[str, list[str]]: 字段选择器字典。
    """
    rules = _boss_rules(platform_config)
    fields = rules.get("fields") if isinstance(rules.get("fields"), dict) else {}
    result: dict[str, list[str]] = {}
    for field, value in fields.items():
        selectors = _selector_list(value)
        if selectors:
            result[str(field)] = selectors
    if not result:
        raise RuntimeError("云端平台配置缺少候选人字段选择器")
    return result


def _boss_selectors(platform_config: dict, key: str, optional: bool = False) -> list[str]:
    """
    返回 Boss 运行时选择器。

    Args:
        platform_config: 云端平台配置。
        key: 规则字段名。
        optional: 是否允许该选择器为空。

    Returns:
        list[str]: 选择器列表。
    """
    selectors = _selector_list(_boss_rules(platform_config).get(key))
    if selectors or optional:
        return selectors
    raise RuntimeError(f"云端平台配置缺少选择器：{key}")


def _boss_rules(platform_config: dict) -> dict[str, Any]:
    """
    将云端平台配置转换为运行时选择器。

    Args:
        platform_config: 云端平台配置。

    Returns:
        dict[str, Any]: 规则字典。
    """
    if not isinstance(platform_config, dict):
        return {}
    selectors = platform_config.get("selectors")
    if isinstance(selectors, dict):
        return selectors

    card = platform_config.get("card") if isinstance(platform_config.get("card"), dict) else {}
    actions = platform_config.get("actions") if isinstance(platform_config.get("actions"), dict) else {}
    detail = platform_config.get("detail") if isinstance(platform_config.get("detail"), dict) else {}
    fields = _field_rules_from_card(card)
    return {
        "candidate_card": card.get("item") or card.get("card"),
        "scroll_containers": card.get("scroll") or card.get("container"),
        "fields": fields,
        "greet_buttons": actions.get("greetBtn") or actions.get("greet_buttons"),
        "continue_buttons": actions.get("continueBtn") or actions.get("continue_buttons"),
        "confirm_buttons": actions.get("confirmBtn") or actions.get("confirm_buttons"),
        "detail_buttons": detail.get("openTarget") or detail.get("open_target"),
        "detail_containers": detail.get("content") or detail.get("container"),
        "detail_close_buttons": detail.get("closeBtn") or detail.get("close_buttons"),
    }


def _field_rules_from_card(card: dict) -> dict[str, Any]:
    """
    从 card 配置中读取字段选择器。

    Args:
        card: 平台配置中的 card 节点。

    Returns:
        dict[str, Any]: 字段选择器。
    """
    result: dict[str, Any] = {}
    raw_fields = card.get("fields")
    if isinstance(raw_fields, list):
        for item in raw_fields:
            if not isinstance(item, dict):
                continue
            for name, spec in item.items():
                result[str(name)] = spec
    elif isinstance(raw_fields, dict):
        result.update(raw_fields)

    for cloud_key, runtime_key in {
        "name": "name",
        "basicInfo": "basic_info",
        "basic_info": "basic_info",
        "education": "education",
        "university": "university",
        "description": "description",
    }.items():
        if cloud_key in card and runtime_key not in result:
            result[runtime_key] = card.get(cloud_key)
    return result


def _selector_list(value: Any) -> list[str]:
    """
    将规则值转换为选择器列表。

    Args:
        value: 规则值。

    Returns:
        list[str]: 选择器列表。
    """
    if isinstance(value, str):
        return [value.strip()] if value.strip() else []
    if isinstance(value, list):
        selectors: list[str] = []
        for item in value:
            selectors.extend(_selector_list(item))
        return _dedupe_selectors(selectors)
    if isinstance(value, dict):
        selectors: list[str] = []
        for key in ("target_classes", "selectors", "selector", "css"):
            if key in value:
                selectors.extend(_selector_list(value.get(key)))
        return _dedupe_selectors(selectors)
    return []


def _dedupe_selectors(selectors: list[str]) -> list[str]:
    """
    对选择器列表去重并清理空值。

    Args:
        selectors: 原始选择器列表。

    Returns:
        list[str]: 清理后的选择器列表。
    """
    result: list[str] = []
    seen: set[str] = set()
    for selector in selectors:
        safe_selector = str(selector or "").strip()
        if not safe_selector or safe_selector in seen:
            continue
        seen.add(safe_selector)
        result.append(safe_selector)
    return result
