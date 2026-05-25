"""
本文件负责候选人详情弹框截图的 OCR 文字识别。

优先使用 RapidOCR + ONNXRuntime 识别，失败时回退 PaddleOCR。
两个 OCR 引擎都采用懒加载方式，首次调用时才初始化。
沿用 goodhrpy 的已验证可用代码，迁入 GoodHR 5 Local Agent 执行层。
"""

from __future__ import annotations

import asyncio
import gc
import importlib.util
import io
import logging
import os
import time

import numpy as np
from PIL import Image, ImageEnhance, ImageOps

logger = logging.getLogger("goodhr5.ocr")

_rapid_ocr_engine = None
_paddle_ocr_engine = None
_ocr_call_count = 0
_PADDLEX_DIR = os.path.expanduser("~/.paddlex")
_OCR_CONTRAST_FACTOR = 1.6


def _ensure_paddlex_dir() -> None:
    """确保 PaddleX 缓存目录存在。"""
    os.makedirs(os.path.join(_PADDLEX_DIR, "temp"), exist_ok=True)


def _get_paddle_engine():
    """获取 PaddleOCR 引擎实例（懒加载）。"""
    global _paddle_ocr_engine
    if _paddle_ocr_engine is not None:
        return _paddle_ocr_engine
    _ensure_paddlex_dir()
    from paddleocr import PaddleOCR
    _paddle_ocr_engine = PaddleOCR(
        lang="ch",
        use_doc_orientation_classify=False,
        use_doc_unwarping=False,
        use_textline_orientation=False,
    )
    logger.info("PaddleOCR 引擎初始化完成")
    return _paddle_ocr_engine


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
    对 OCR 图片做灰度和对比度增强。

    将网页截图先转成灰度图，减少颜色干扰，再提升文字和背景的对比度。
    最后转回 RGB，保证 PaddleOCR 接收稳定的三通道图片。

    Args:
        image: 原始截图图片。

    Returns:
        Image.Image: 预处理后的 RGB 图片。
    """
    rgb_image = image.convert("RGB")
    gray_image = ImageOps.grayscale(rgb_image)
    enhanced_image = ImageEnhance.Contrast(gray_image).enhance(_OCR_CONTRAST_FACTOR)
    return enhanced_image.convert("RGB")


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
        logger.warning("RapidOCR 预热失败，耗时 %dms, err=%s，尝试预热 PaddleOCR", elapsed_ms, e)
    try:
        await asyncio.to_thread(_get_paddle_engine)
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.info("PaddleOCR 预热完成，耗时 %dms", elapsed_ms)
        return True
    except Exception as e:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.error("OCR 预热失败，耗时 %dms, err=%s", elapsed_ms, e)
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


def _recognize_with_paddleocr(img_array: np.ndarray) -> tuple[str, dict[str, object]]:
    """
    使用 PaddleOCR 识别图片文字。

    Args:
        img_array: 已预处理的 RGB 图片数组。

    Returns:
        tuple[str, dict[str, object]]: 识别文本和调试信息。
    """
    engine = _get_paddle_engine()

    if hasattr(engine, "predict"):
        result = engine.predict(img_array)
        if not result:
            return "", {"line_count": 0}
        r0 = result[0]
        if hasattr(r0, "json"):
            res_obj = r0.json.get("res", r0.json)
            rec_texts = res_obj.get("rec_texts", [])
            lines = [t.strip() for t in rec_texts if t and t.strip()]
            return "\n".join(lines), {"line_count": len(lines)}
        return "", {"line_count": 0}

    result = engine.ocr(img_array)
    if not result or not result[0]:
        return "", {"line_count": 0}
    lines = []
    for line in result[0]:
        if line and len(line) >= 2:
            text = str(line[1][0]).strip()
            if text:
                lines.append(text)
    return "\n".join(lines), {"line_count": len(lines)}


def ocr_image_bytes(image_bytes: bytes) -> str:
    """对图片字节数据进行 OCR 识别。

    优先使用 RapidOCR 识别；RapidOCR 异常或识别为空时回退 PaddleOCR。
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
    processed_mode = "unknown"
    engine_name = "rapidocr"
    engine_meta: dict[str, object] = {}
    text = ""
    try:
        image = Image.open(io.BytesIO(image_bytes))
        image_size = f"{image.width}x{image.height}"
        image_mode = image.mode
        processed_image = _preprocess_image_for_ocr(image)
        processed_mode = processed_image.mode
        img_array = np.array(processed_image)
        image.close()
        processed_image.close()

        try:
            text, engine_meta = _recognize_with_rapidocr(img_array)
        except Exception as rapid_error:
            logger.warning("RapidOCR 识别失败，准备回退 PaddleOCR call=%d err=%s", call_no, rapid_error)
            text = ""
            engine_meta = {"rapid_error": str(rapid_error)}
        if text.strip():
            return text

        engine_name = "paddleocr"
        text, engine_meta = _recognize_with_paddleocr(img_array)
        del img_array
        gc.collect()
        return text
    except Exception as e:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.error(
            "OCR 识别失败 call=%d engine=%s 耗时=%dms 图片=%s 原mode=%s 处理mode=%s bytes=%d err=%s",
            call_no,
            engine_name,
            elapsed_ms,
            image_size,
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
            "OCR 识别完成 call=%d engine=%s 耗时=%dms 图片=%s 原mode=%s 处理mode=%s 对比度=%.1f bytes=%d 文本行数=%d 文本长度=%d 引擎信息=%s",
            call_no,
            engine_name,
            elapsed_ms,
            image_size,
            image_mode,
            processed_mode,
            _OCR_CONTRAST_FACTOR,
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
    global _rapid_ocr_engine, _paddle_ocr_engine
    if _rapid_ocr_engine is not None:
        _rapid_ocr_engine = None
        logger.info("RapidOCR 引擎已释放")
    if _paddle_ocr_engine is not None:
        _paddle_ocr_engine = None
        gc.collect()
        logger.info("PaddleOCR 引擎已释放")


def is_available() -> bool:
    """检查 OCR 是否可用。"""
    if importlib.util.find_spec("rapidocr") is not None:
        return True
    try:
        _ensure_paddlex_dir()
        return importlib.util.find_spec("paddleocr") is not None
    except Exception:
        return False
