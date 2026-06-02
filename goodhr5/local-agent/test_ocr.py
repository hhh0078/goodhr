"""本文件用于手动测试 Local Agent 当前 OCR 流程的速度和输出效果。"""

from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path

import numpy as np
from PIL import Image

from app.ocr import _get_rapid_engine, _preprocess_image_for_ocr, _recognize_with_rapidocr


def parse_args() -> argparse.Namespace:
    """
    解析命令行参数。

    Returns:
        argparse.Namespace: 包含图片路径和输出路径的参数对象。
    """
    parser = argparse.ArgumentParser(description="测试 GoodHR Local Agent OCR 单张图片耗时")
    parser.add_argument("image", help="需要测试的图片路径，例如 C:\\Users\\admin\\Desktop\\1-1.png")
    parser.add_argument(
        "--out",
        default="",
        help="处理后图片保存路径；不填时保存到原图同目录，文件名追加 -ocr-input.png",
    )
    parser.add_argument(
        "--preview",
        type=int,
        default=500,
        help="控制台预览文字长度，默认 500",
    )
    parser.add_argument(
        "--rec-batch",
        type=int,
        default=0,
        help="临时调整文字识别批量大小，例如 16 或 32；不填则使用 RapidOCR 默认值",
    )
    return parser.parse_args()


def build_default_output_path(image_path: Path) -> Path:
    """
    生成默认的 OCR 输入图保存路径。

    Args:
        image_path: 原始测试图片路径。

    Returns:
        Path: 默认输出图片路径。
    """
    return image_path.with_name(f"{image_path.stem}-ocr-input.png")


def get_package_version(package_name: str) -> str:
    """
    获取已安装 Python 包版本。

    Args:
        package_name: Python 包名。

    Returns:
        str: 包版本号，获取失败时返回 unknown。
    """
    try:
        from importlib.metadata import version

        return version(package_name)
    except Exception:
        return "unknown"


def get_onnx_provider_text() -> str:
    """
    获取 RapidOCR 当前 ONNXRuntime 推理后端。

    Returns:
        str: 三个 OCR 子模型的 Provider 信息。
    """
    try:
        engine = _get_rapid_engine()
        parts = []
        for label, attr_name in (
            ("det", "text_det"),
            ("cls", "text_cls"),
            ("rec", "text_rec"),
        ):
            part = getattr(engine, attr_name, None)
            session_wrapper = getattr(part, "session", None)
            session = getattr(session_wrapper, "session", None)
            providers = session.get_providers() if session is not None else []
            parts.append(f"{label}={providers or 'unknown'}")
        return "; ".join(parts)
    except Exception as exc:
        return f"unknown({exc})"


def format_engine_elapsed_list(value: object) -> str:
    """
    格式化 RapidOCR 内部分段耗时。

    Args:
        value: RapidOCR 返回的 engine_elapsed_list。

    Returns:
        str: 适合控制台阅读的分段耗时。
    """
    labels = ("det_detect", "cls_rotate", "rec_text")
    if not isinstance(value, (list, tuple)):
        return str(value)
    parts = []
    for index, item in enumerate(value):
        label = labels[index] if index < len(labels) else f"part_{index + 1}"
        if isinstance(item, (int, float)):
            parts.append(f"{label}={item:.3f}s")
        else:
            parts.append(f"{label}={item}")
    return ", ".join(parts)


def apply_rec_batch(rec_batch: int) -> int:
    """
    临时调整 RapidOCR 的文字识别批量大小。

    Args:
        rec_batch: 希望设置的批量大小，小于等于 0 时不调整。

    Returns:
        int: 当前实际生效的批量大小，无法获取时返回 0。
    """
    engine = _get_rapid_engine()
    text_rec = getattr(engine, "text_rec", None)
    if text_rec is None:
        return 0
    if rec_batch > 0:
        text_rec.rec_batch_num = rec_batch
    try:
        return int(getattr(text_rec, "rec_batch_num", 0) or 0)
    except Exception:
        return 0


def main() -> int:
    """
    执行 OCR 测试并打印耗时、尺寸和文本预览。

    Returns:
        int: 进程退出码，0 表示成功，1 表示失败。
    """
    args = parse_args()
    image_path = Path(args.image).expanduser()
    if not image_path.exists():
        print(f"图片不存在：{image_path}", file=sys.stderr)
        return 1

    output_path = Path(args.out).expanduser() if args.out else build_default_output_path(image_path)
    with Image.open(image_path) as original_image:
        original_size = original_image.size
        original_mode = original_image.mode

    total_start = time.perf_counter()
    load_start = time.perf_counter()
    image_bytes = image_path.read_bytes()
    with Image.open(image_path) as image:
        preprocess_start = time.perf_counter()
        processed_image = _preprocess_image_for_ocr(image)
        processed_image.save(output_path, format="PNG")
        processed_size = processed_image.size
        processed_mode = processed_image.mode
        img_array = np.array(processed_image)
        processed_image.close()
    load_ms = int((preprocess_start - load_start) * 1000)
    preprocess_ms = int((time.perf_counter() - preprocess_start) * 1000)

    engine_start = time.perf_counter()
    active_rec_batch = apply_rec_batch(args.rec_batch)
    text, engine_meta = _recognize_with_rapidocr(img_array)
    engine_ms = int((time.perf_counter() - engine_start) * 1000)
    elapsed = time.perf_counter() - total_start
    del img_array

    engine_elapsed = engine_meta.get("engine_elapsed")
    engine_elapsed_list = engine_meta.get("engine_elapsed_list")
    preview = text[: max(args.preview, 0)].replace("\n", " | ")
    print(f"image={image_path}")
    print(f"output={output_path}")
    print(f"elapsed_sec={elapsed:.3f}")
    print(f"load_ms={load_ms}")
    print(f"preprocess_ms={preprocess_ms}")
    print(f"engine_ms={engine_ms}")
    print(f"engine_elapsed={engine_elapsed}")
    print(f"engine_elapsed_list={format_engine_elapsed_list(engine_elapsed_list)}")
    print(f"rec_batch_num={active_rec_batch}")
    print(f"rapidocr_version={get_package_version('rapidocr')}")
    print(f"onnxruntime_version={get_package_version('onnxruntime')}")
    print(f"onnx_providers={get_onnx_provider_text()}")
    print(f"bytes={len(image_bytes)}")
    print(f"original_size={original_size[0]}x{original_size[1]} mode={original_mode}")
    print(f"processed_size={processed_size[0]}x{processed_size[1]} mode={processed_mode}")
    print(f"text_len={len(text)}")
    print(f"line_count={len([line for line in text.splitlines() if line.strip()])}")
    print(f"preview={preview}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
