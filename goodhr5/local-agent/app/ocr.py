"""
本文件负责候选人详情弹框截图的 OCR 文字识别。

基于 PaddleOCR 实现，采用懒加载方式首次调用时初始化引擎。
沿用 goodhrpy 的已验证可用代码，迁入 GoodHR 5 Local Agent 执行层。
"""

from __future__ import annotations

import asyncio
import gc
import io
import logging
import os
import time

import numpy as np
from PIL import Image

logger = logging.getLogger("goodhr5.ocr")

_ocr_engine = None
_ocr_call_count = 0
_PADDLEX_DIR = os.path.expanduser("~/.paddlex")


def _ensure_paddlex_dir() -> None:
    """确保 PaddleX 缓存目录存在。"""
    os.makedirs(os.path.join(_PADDLEX_DIR, "temp"), exist_ok=True)


def _get_engine():
    """获取 PaddleOCR 引擎实例（懒加载）。"""
    global _ocr_engine
    if _ocr_engine is not None:
        return _ocr_engine
    _ensure_paddlex_dir()
    from paddleocr import PaddleOCR
    _ocr_engine = PaddleOCR(
        lang="ch",
        use_doc_orientation_classify=False,
        use_doc_unwarping=False,
        use_textline_orientation=False,
    )
    logger.info("PaddleOCR 引擎初始化完成")
    return _ocr_engine


async def warmup_ocr_async() -> bool:
    """启动阶段预热 OCR 引擎，避免任务首次调用冷启动。"""
    start = time.perf_counter()
    try:
        await asyncio.to_thread(_get_engine)
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.info("OCR 预热完成，耗时 %dms", elapsed_ms)
        return True
    except Exception as e:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.error("OCR 预热失败，耗时 %dms, err=%s", elapsed_ms, e)
        return False


def ocr_image_bytes(image_bytes: bytes) -> str:
    """对图片字节数据进行 OCR 识别。

    支持 PaddleOCR v3（predict）和旧版（ocr）两种 API。
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
    text = ""
    try:
        image = Image.open(io.BytesIO(image_bytes))
        image_size = f"{image.width}x{image.height}"
        image_mode = image.mode
        img_array = np.array(image)
        image.close()
        engine = _get_engine()

        if hasattr(engine, "predict"):
            result = engine.predict(img_array)
            del img_array
            gc.collect()
            if not result:
                return ""
            r0 = result[0]
            if hasattr(r0, "json"):
                res_obj = r0.json.get("res", r0.json)
                rec_texts = res_obj.get("rec_texts", [])
                lines = [t.strip() for t in rec_texts if t and t.strip()]
                text = "\n".join(lines)
                return text
            return ""

        result = engine.ocr(img_array)
        del img_array
        gc.collect()
        if not result or not result[0]:
            return ""
        lines = []
        for line in result[0]:
            if line and len(line) >= 2:
                text = str(line[1][0]).strip()
                if text:
                    lines.append(text)
        text = "\n".join(lines)
        return text
    except Exception as e:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        logger.error(
            "OCR 识别失败 call=%d 耗时=%dms 图片=%s mode=%s bytes=%d err=%s",
            call_no,
            elapsed_ms,
            image_size,
            image_mode,
            len(image_bytes),
            e,
        )
        return ""
    finally:
        elapsed_ms = int((time.perf_counter() - start) * 1000)
        line_count = len([line for line in text.splitlines() if line.strip()])
        logger.info(
            "OCR 识别完成 call=%d 耗时=%dms 图片=%s mode=%s bytes=%d 文本行数=%d 文本长度=%d",
            call_no,
            elapsed_ms,
            image_size,
            image_mode,
            len(image_bytes),
            line_count,
            len(text),
        )


async def ocr_image_async(image_bytes: bytes) -> str:
    """异步版本 OCR，在线程池中执行避免阻塞事件循环。"""
    return await asyncio.to_thread(ocr_image_bytes, image_bytes)


def close_ocr() -> None:
    """释放 PaddleOCR 引擎内存。"""
    global _ocr_engine
    if _ocr_engine is not None:
        _ocr_engine = None
        gc.collect()
        logger.info("PaddleOCR 引擎已释放")


def is_available() -> bool:
    """检查 PaddleOCR 是否可用。"""
    try:
        _ensure_paddlex_dir()
        from paddleocr import PaddleOCR  # noqa: F401
        return True
    except ImportError:
        return False
    except Exception:
        return False
