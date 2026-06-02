"""
本文件负责候选人详情弹框截图的 OCR 文字识别。

基于 RapidOCR + ONNXRuntime 实现，采用懒加载方式首次调用时初始化引擎。
"""

from __future__ import annotations

import asyncio
import importlib.util
import io
import logging
import os
import time
from pathlib import Path

import numpy as np
from PIL import Image, ImageEnhance, ImageFilter, ImageOps

logger = logging.getLogger("goodhr5.ocr")

_rapid_ocr_engine = None
_ocr_call_count = 0
_OCR_CONTRAST_FACTOR = 1.6
_OCR_SHARPEN_RADIUS = 1.0
_OCR_SHARPEN_PERCENT = 80
_OCR_SHARPEN_THRESHOLD = 3
_OCR_MAX_WIDTH = 680


def _get_ocr_max_width() -> int:
    """
    读取 OCR 图片最大宽度配置。

    Returns:
        int: OCR 预处理后的最大图片宽度。
    """
    raw_value = os.getenv("GOODHR_OCR_MAX_WIDTH", "").strip()
    if not raw_value:
        return _OCR_MAX_WIDTH
    try:
        value = int(raw_value)
    except ValueError:
        logger.warning("GOODHR_OCR_MAX_WIDTH 配置无效，使用默认值 %d: %s", _OCR_MAX_WIDTH, raw_value)
        return _OCR_MAX_WIDTH
    if value < 320:
        logger.warning("GOODHR_OCR_MAX_WIDTH 过小，使用默认值 %d: %s", _OCR_MAX_WIDTH, raw_value)
        return _OCR_MAX_WIDTH
    return value


def _get_rapid_engine():
    """获取 RapidOCR 引擎实例（懒加载）。"""
    global _rapid_ocr_engine
    if _rapid_ocr_engine is not None:
        return _rapid_ocr_engine
    from rapidocr import RapidOCR
    _rapid_ocr_engine = RapidOCR()
    logger.info("RapidOCR 引擎初始化完成")
    return _rapid_ocr_engine


def _preprocess_image_for_ocr(image: Image.Image) -> Image.Image:
    """
    对 OCR 图片做灰度、对比度增强、轻微锐化和等比例高清缩放。

    将网页截图先转成灰度图，减少颜色干扰，再提升文字和背景的对比度。
    再用轻微锐化增强文字边缘。图片过宽时使用 LANCZOS 等比例缩放，
    最后转回 RGB，保证 OCR 引擎接收稳定的三通道图片。

    Args:
        image: 原始截图图片。

    Returns:
        Image.Image: 预处理后的 RGB 图片。
    """
    rgb_image = image.convert("RGB")
    gray_image = ImageOps.grayscale(rgb_image)
    enhanced_image = ImageEnhance.Contrast(gray_image).enhance(_OCR_CONTRAST_FACTOR)
    sharpened_image = enhanced_image.filter(
        ImageFilter.UnsharpMask(
            radius=_OCR_SHARPEN_RADIUS,
            percent=_OCR_SHARPEN_PERCENT,
            threshold=_OCR_SHARPEN_THRESHOLD,
        )
    )
    resized_image = _resize_image_for_ocr(sharpened_image)
    return resized_image.convert("RGB")


def _resize_image_for_ocr(image: Image.Image) -> Image.Image:
    """
    对 OCR 图片做等比例高清缩放。

    Args:
        image: 已完成灰度、对比和锐化的图片。

    Returns:
        Image.Image: 宽度不超过 OCR 限制的图片。
    """
    max_width = _get_ocr_max_width()
    if image.width <= max_width:
        return image
    ratio = max_width / image.width
    target_height = max(1, int(round(image.height * ratio)))
    return image.resize((max_width, target_height), Image.Resampling.LANCZOS)


def _save_processed_image_for_debug(image: Image.Image, save_path: str | Path | None) -> str:
    """
    保存 OCR 实际识别的压缩后图片。

    Args:
        image: OCR 预处理后的图片。
        save_path: 调试图片保存路径，为空时不保存。

    Returns:
        str: 保存成功的路径，未保存时为空字符串。
    """
    if not save_path:
        return ""
    path = Path(save_path)
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        image.save(path, format="PNG")
        return str(path)
    except Exception as exc:
        logger.warning("OCR 压缩后调试图片保存失败 path=%s err=%s", path, exc)
        return ""


async def warmup_ocr_async() -> bool:
    """启动阶段预热 OCR 引擎，避免任务首次调用冷启动。"""
    start = time.perf_counter()
    try:
        await asyncio.to_thread(_get_rapid_engine)
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.info("RapidOCR 预热完成，耗时 %dms", elapsed_ms)
        return True
    except Exception as e:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.error("RapidOCR 预热失败，耗时 %dms, err=%s", elapsed_ms, e)
        return False


def _recognize_with_rapidocr(img_array: np.ndarray) -> tuple[str, dict[str, object]]:
    """
    使用 RapidOCR 识别图片文字。

    Args:
        img_array: 已预处理的 RGB 图片数组。

    Returns:
        tuple[str, dict[str, object]]: 识别文本和耗时等调试信息。
    """
    engine = _get_rapid_engine()
    result = engine(img_array, use_det=True, use_cls=False, use_rec=True)
    texts = getattr(result, "txts", None)
    if texts is None and isinstance(result, tuple) and result:
        texts = [item[1] for item in result[0] if isinstance(item, (list, tuple)) and len(item) >= 2]
    lines = [str(t).strip() for t in texts or [] if str(t).strip()]
    meta = {
        "engine_elapsed": getattr(result, "elapse", None),
        "engine_elapsed_list": getattr(result, "elapse_list", None),
        "line_count": len(lines),
    }
    return "\n".join(lines), meta


def _compact_text(text: str) -> str:
    """
    压缩文本空白字符，方便比较 OCR 重叠内容。

    Args:
        text: 原始 OCR 文本。

    Returns:
        str: 去掉空白后的文本。
    """
    return "".join(str(text or "").split())


def _trim_prefix_by_compact_length(text: str, compact_length: int) -> str:
    """
    按压缩后字符长度裁掉原文前缀。

    Args:
        text: 原始文本。
        compact_length: 需要裁掉的非空白字符数量。

    Returns:
        str: 裁掉重叠前缀后的文本。
    """
    if compact_length <= 0:
        return text
    seen = 0
    for index, char in enumerate(text):
        if not char.isspace():
            seen += 1
        if seen >= compact_length:
            return text[index + 1:]
    return ""


def _find_compact_overlap(left: str, right: str, min_overlap: int = 20, max_window: int = 2000) -> int:
    """
    查找两段 OCR 文本边界的最大压缩字符重叠。

    Args:
        left: 已合并文本。
        right: 下一段文本。
        min_overlap: 最小有效重叠字符数。
        max_window: 参与比较的最大窗口长度。

    Returns:
        int: 重叠的压缩字符数量。
    """
    left_compact = _compact_text(left)[-max_window:]
    right_compact = _compact_text(right)[:max_window]
    max_overlap = min(len(left_compact), len(right_compact))
    for size in range(max_overlap, min_overlap - 1, -1):
        if left_compact[-size:] == right_compact[:size]:
            return size
    return 0


def _merge_ocr_text_pair(left: str, right: str) -> str:
    """
    合并两段 OCR 文本并去掉边界重叠。

    Args:
        left: 已合并文本。
        right: 下一段 OCR 文本。

    Returns:
        str: 合并后的文本。
    """
    left = str(left or "").strip()
    right = str(right or "").strip()
    if not left:
        return right
    if not right:
        return left

    left_lines = [line.strip() for line in left.splitlines() if line.strip()]
    right_lines = [line.strip() for line in right.splitlines() if line.strip()]
    max_line_overlap = min(len(left_lines), len(right_lines), 20)
    for count in range(max_line_overlap, 0, -1):
        if [_compact_text(line) for line in left_lines[-count:]] == [_compact_text(line) for line in right_lines[:count]]:
            remain = "\n".join(right_lines[count:]).strip()
            return left if not remain else left + "\n" + remain

    compact_overlap = _find_compact_overlap(left, right)
    if compact_overlap > 0:
        remain = _trim_prefix_by_compact_length(right, compact_overlap).strip()
        return left if not remain else left + "\n" + remain

    return left + "\n" + right


def merge_ocr_texts(texts: list[str]) -> str:
    """
    合并多段 OCR 文本并去掉相邻截图的重复边界。

    Args:
        texts: 按截图顺序识别出的 OCR 文本列表。

    Returns:
        str: 去重合并后的 OCR 文本。
    """
    merged = ""
    for text in texts:
        merged = _merge_ocr_text_pair(merged, text)
    return merged.strip()


def ocr_image_bytes(image_bytes: bytes, processed_save_path: str | Path | None = None) -> str:
    """对图片字节数据进行 OCR 识别。

    使用 RapidOCR 识别，失败时返回空字符串，不抛异常。
    OCR 失败时返回空字符串，不抛异常。

    Args:
        image_bytes: PNG/JPEG 图片字节数据
        processed_save_path: 压缩后的 OCR 调试图片保存路径

    Returns:
        str: 识别出的文字
    """
    global _ocr_call_count
    _ocr_call_count += 1
    call_no = _ocr_call_count
    start = time.perf_counter()
    image_size = "unknown"
    image_mode = "unknown"
    processed_size = "unknown"
    processed_mode = "unknown"
    engine_meta: dict[str, object] = {}
    preprocess_ms = 0
    engine_ms = 0
    text = ""
    processed_debug_path = ""
    try:
        preprocess_start = time.perf_counter()
        image = Image.open(io.BytesIO(image_bytes))
        image_size = f"{image.width}x{image.height}"
        image_mode = image.mode
        processed_image = _preprocess_image_for_ocr(image)
        processed_size = f"{processed_image.width}x{processed_image.height}"
        processed_mode = processed_image.mode
        processed_debug_path = _save_processed_image_for_debug(processed_image, processed_save_path)
        img_array = np.array(processed_image)
        image.close()
        processed_image.close()
        preprocess_ms = int((time.perf_counter() - preprocess_start) * 1000)

        engine_start = time.perf_counter()
        text, engine_meta = _recognize_with_rapidocr(img_array)
        engine_ms = int((time.perf_counter() - engine_start) * 1000)
        del img_array
        return text
    except Exception as e:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.error(
            "OCR 识别失败 call=%d engine=rapidocr 总耗时=%dms 预处理=%dms 引擎=%dms 图片=%s 处理后=%s 缩放=%s 原mode=%s 处理mode=%s bytes=%d err=%s",
            call_no,
            elapsed_ms,
            preprocess_ms,
            engine_ms,
            image_size,
            processed_size,
            image_size != processed_size,
            image_mode,
            processed_mode,
            len(image_bytes),
            e,
        )
        return ""
    finally:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        line_count = len([line for line in text.splitlines() if line.strip()])
        logger.info(
            "OCR 识别完成 call=%d engine=rapidocr 总耗时=%dms 预处理=%dms 引擎=%dms 图片=%s 处理后=%s 缩放=%s 算法=LANCZOS 最大宽度=%d 原mode=%s 处理mode=%s 对比度=%.1f 锐化=radius%.1f/percent%d/threshold%d bytes=%d 文本行数=%d 文本长度=%d 保存=%s 引擎信息=%s",
            call_no,
            elapsed_ms,
            preprocess_ms,
            engine_ms,
            image_size,
            processed_size,
            image_size != processed_size,
            _get_ocr_max_width(),
            image_mode,
            processed_mode,
            _OCR_CONTRAST_FACTOR,
            _OCR_SHARPEN_RADIUS,
            _OCR_SHARPEN_PERCENT,
            _OCR_SHARPEN_THRESHOLD,
            len(image_bytes),
            line_count,
            len(text),
            processed_debug_path or "未保存",
            engine_meta,
        )


async def ocr_image_async(image_bytes: bytes, processed_save_path: str | Path | None = None) -> str:
    """异步版本 OCR，在线程池中执行避免阻塞事件循环。"""
    return await asyncio.to_thread(ocr_image_bytes, image_bytes, processed_save_path)


def close_ocr() -> None:
    """释放 OCR 引擎内存。"""
    global _rapid_ocr_engine
    if _rapid_ocr_engine is not None:
        _rapid_ocr_engine = None
        logger.info("RapidOCR 引擎已释放")


def is_available() -> bool:
    """检查 OCR 是否可用。"""
    return importlib.util.find_spec("rapidocr") is not None
