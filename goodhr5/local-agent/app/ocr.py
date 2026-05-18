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

import numpy as np
from PIL import Image

logger = logging.getLogger("goodhr5.ocr")

_ocr_engine = None
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


def ocr_image_bytes(image_bytes: bytes) -> str:
    """对图片字节数据进行 OCR 识别。

    支持 PaddleOCR v3（predict）和旧版（ocr）两种 API。
    OCR 失败时返回空字符串，不抛异常。

    Args:
        image_bytes: PNG/JPEG 图片字节数据

    Returns:
        str: 识别出的文字
    """
    try:
        image = Image.open(io.BytesIO(image_bytes))
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
                return "\n".join(lines)
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
        return "\n".join(lines)
    except Exception as e:
        logger.error("OCR 识别失败: %s", e)
        return ""


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
