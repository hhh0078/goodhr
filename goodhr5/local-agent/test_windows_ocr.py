"""本文件用于在 Windows 上测试系统自带 OCR 的速度和中文识别效果。"""

from __future__ import annotations

import argparse
import asyncio
import sys
import time
from pathlib import Path


def parse_args() -> argparse.Namespace:
    """
    解析命令行参数。

    Returns:
        argparse.Namespace: 包含测试图片路径和预览长度的参数对象。
    """
    parser = argparse.ArgumentParser(description="测试 Windows 系统自带 OCR 单张图片耗时")
    parser.add_argument("image", help="需要测试的图片路径，例如 C:\\Users\\guagua\\Desktop\\1-1.png")
    parser.add_argument(
        "--preview",
        type=int,
        default=500,
        help="控制台预览文字长度，默认 500",
    )
    return parser.parse_args()


def import_winsdk_modules():
    """
    导入 Windows OCR 所需模块。

    Returns:
        tuple: Windows OCR 相关类。
    """
    try:
        from winsdk.windows.globalization import Language
        from winsdk.windows.graphics.imaging import BitmapDecoder
        from winsdk.windows.media.ocr import OcrEngine
        from winsdk.windows.storage import StorageFile
    except ImportError as exc:
        print("缺少 winsdk，请先执行：", file=sys.stderr)
        print(r".\.venv\Scripts\python.exe -m pip install winsdk", file=sys.stderr)
        raise exc
    return Language, BitmapDecoder, OcrEngine, StorageFile


async def recognize_windows_ocr(image_path: Path) -> tuple[str, dict[str, object]]:
    """
    使用 Windows 系统 OCR 识别单张图片。

    Args:
        image_path: 需要识别的图片路径。

    Returns:
        tuple[str, dict[str, object]]: 识别文本和调试信息。
    """
    Language, BitmapDecoder, OcrEngine, StorageFile = import_winsdk_modules()
    load_start = time.perf_counter()
    file = await StorageFile.get_file_from_path_async(str(image_path.resolve()))
    stream = await file.open_read_async()
    decoder = await BitmapDecoder.create_async(stream)
    bitmap = await decoder.get_software_bitmap_async()
    load_ms = int((time.perf_counter() - load_start) * 1000)

    engine_start = time.perf_counter()
    engine = OcrEngine.try_create_from_language(Language("zh-Hans"))
    if engine is None:
        engine = OcrEngine.try_create_from_user_profile_languages()
    engine_init_ms = int((time.perf_counter() - engine_start) * 1000)

    if engine is None:
        raise RuntimeError("Windows OCR 不可用，请检查是否安装了中文语言包或中文 OCR 组件")

    recognize_start = time.perf_counter()
    result = await engine.recognize_async(bitmap)
    recognize_ms = int((time.perf_counter() - recognize_start) * 1000)

    lines = [line.text.strip() for line in result.lines if line.text and line.text.strip()]
    text = "\n".join(lines)
    meta = {
        "load_ms": load_ms,
        "engine_init_ms": engine_init_ms,
        "recognize_ms": recognize_ms,
        "line_count": len(lines),
    }
    return text, meta


async def async_main() -> int:
    """
    执行 Windows 系统 OCR 测试并打印结果。

    Returns:
        int: 进程退出码，0 表示成功，1 表示失败。
    """
    args = parse_args()
    image_path = Path(args.image).expanduser()
    if not image_path.exists():
        print(f"图片不存在：{image_path}", file=sys.stderr)
        return 1

    total_start = time.perf_counter()
    try:
        text, meta = await recognize_windows_ocr(image_path)
    except Exception as exc:
        print(f"Windows OCR 测试失败：{exc}", file=sys.stderr)
        return 1

    elapsed = time.perf_counter() - total_start
    preview = text[: max(args.preview, 0)].replace("\n", " | ")
    print(f"image={image_path}")
    print(f"elapsed_sec={elapsed:.3f}")
    print(f"load_ms={meta.get('load_ms')}")
    print(f"engine_init_ms={meta.get('engine_init_ms')}")
    print(f"recognize_ms={meta.get('recognize_ms')}")
    print(f"text_len={len(text)}")
    print(f"line_count={meta.get('line_count')}")
    print(f"preview={preview}")
    return 0


def main() -> int:
    """
    启动异步 Windows OCR 测试。

    Returns:
        int: 进程退出码。
    """
    return asyncio.run(async_main())


if __name__ == "__main__":
    raise SystemExit(main())
