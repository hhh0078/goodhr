"""
本文件负责候选人详情弹框截图的 OCR 文字识别。

基于 RapidOCR + ONNXRuntime 实现，采用懒加载方式首次调用时初始化引擎。
"""

from __future__ import annotations

import asyncio
import importlib.util
import io
import logging
import time

import numpy as np
from PIL import Image, ImageEnhance, ImageFilter, ImageOps

logger = logging.getLogger("goodhr5.ocr")

_rapid_ocr_engine = None
_ocr_call_count = 0
_OCR_CONTRAST_FACTOR = 1.6
_OCR_SHARPEN_RADIUS = 1.0
_OCR_SHARPEN_PERCENT = 80
_OCR_SHARPEN_THRESHOLD = 3
_OCR_MAX_WIDTH = 1200


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
    if image.width <= _OCR_MAX_WIDTH:
        return image
    ratio = _OCR_MAX_WIDTH / image.width
    target_height = max(1, int(round(image.height * ratio)))
    return image.resize((_OCR_MAX_WIDTH, target_height), Image.Resampling.LANCZOS)


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


def ocr_image_bytes(image_bytes: bytes) -> str:
    """对图片字节数据进行 OCR 识别。

    使用 RapidOCR 识别，失败时返回空字符串，不抛异常。
    OCR 失败时返回空字符串，不抛异常。

    Args:
        image_bytes: PNG/JPEG 图片字节数据

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
    try:
        preprocess_start = time.perf_counter()
        image = Image.open(io.BytesIO(image_bytes))
        image_size = f"{image.width}x{image.height}"
        image_mode = image.mode
        processed_image = _preprocess_image_for_ocr(image)
        processed_size = f"{processed_image.width}x{processed_image.height}"
        processed_mode = processed_image.mode
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
            "OCR 识别完成 call=%d engine=rapidocr 总耗时=%dms 预处理=%dms 引擎=%dms 图片=%s 处理后=%s 缩放=%s 算法=LANCZOS 最大宽度=%d 原mode=%s 处理mode=%s 对比度=%.1f 锐化=radius%.1f/percent%d/threshold%d bytes=%d 文本行数=%d 文本长度=%d 引擎信息=%s",
            call_no,
            elapsed_ms,
            preprocess_ms,
            engine_ms,
            image_size,
            processed_size,
            image_size != processed_size,
            _OCR_MAX_WIDTH,
            image_mode,
            processed_mode,
            _OCR_CONTRAST_FACTOR,
            _OCR_SHARPEN_RADIUS,
            _OCR_SHARPEN_PERCENT,
            _OCR_SHARPEN_THRESHOLD,
            len(image_bytes),
            line_count,
            len(text),
            engine_meta,
        )


async def ocr_image_async(image_bytes: bytes) -> str:
    """异步版本 OCR，在线程池中执行避免阻塞事件循环。"""
    return await asyncio.to_thread(ocr_image_bytes, image_bytes)


def close_ocr() -> None:
    """释放 OCR 引擎内存。"""
    global _rapid_ocr_engine
    if _rapid_ocr_engine is not None:
        _rapid_ocr_engine = None
        logger.info("RapidOCR 引擎已释放")


def is_available() -> bool:
    """检查 OCR 是否可用。"""
    return importlib.util.find_spec("rapidocr") is not None
