"""
GoodHR 自动化工具 - OCR 文字识别模块

基于 PaddleOCR 实现图片文字识别，用于候选人详情弹框截图的文字提取。
采用懒加载方式，首次调用时才初始化 PaddleOCR 引擎，避免启动时阻塞。
"""

import asyncio
import io
import os
from typing import Optional

from utils.logger import get_logger

logger = get_logger("ocr")

_ocr_engine = None

_PADDLEX_DIR = os.path.expanduser("~/.paddlex")


def _ensure_paddlex_dir() -> None:
    """
    确保 PaddleX 缓存目录存在

    PaddleOCR 依赖 PaddleX，首次导入时需要创建缓存目录，
    如果目录不存在会导致 PermissionError。
    """
    temp_dir = os.path.join(_PADDLEX_DIR, "temp")
    os.makedirs(temp_dir, exist_ok=True)


def _get_engine():
    """
    获取 PaddleOCR 引擎实例（懒加载）

    首次调用时初始化引擎，后续调用直接返回缓存实例。
    使用中文+英文模型，关闭方向分类以提高速度。

    Returns:
        PaddleOCR: PaddleOCR 引擎实例
    """
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
    try:
        import gc
        import numpy as np
        from PIL import Image

        image = Image.open(io.BytesIO(image_bytes))
        img_array = np.array(image)
        image.close()

        engine = _get_engine()

        if hasattr(engine, 'predict'):
            result = engine.predict(img_array)
            del img_array
            gc.collect()
            if not result:
                return ""
            r0 = result[0]
            if hasattr(r0, 'json'):
                res = r0.json.get('res', r0.json)
                rec_texts = res.get('rec_texts', [])
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
        logger.error(f"OCR 识别失败: {e}")
        return ""


async def ocr_image_async(image_bytes: bytes) -> str:
    """
    异步版本的 OCR 识别

    PaddleOCR 是同步 CPU 密集型操作，通过 asyncio.to_thread
    放到线程池中执行，避免阻塞事件循环。

    Args:
        image_bytes: 图片的二进制字节数据

    Returns:
        str: 识别出的文字内容
    """
    return await asyncio.to_thread(ocr_image_bytes, image_bytes)


def is_available() -> bool:
    """
    检查 PaddleOCR 是否可用

    尝试导入 paddleocr 模块，判断是否已正确安装。

    Returns:
        bool: PaddleOCR 是否可用
    """
    try:
        _ensure_paddlex_dir()
        from paddleocr import PaddleOCR  # noqa: F401

        return True
    except ImportError:
        return False
    except Exception:
        return False
