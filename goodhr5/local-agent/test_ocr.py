"""本文件用于手动测试 Local Agent 当前 OCR 流程的速度和输出效果。"""

from __future__ import annotations

import argparse
import sys
import time
from pathlib import Path

from PIL import Image

from app.ocr import ocr_image_bytes


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

    start = time.perf_counter()
    text = ocr_image_bytes(image_path.read_bytes(), output_path)
    elapsed = time.perf_counter() - start

    with Image.open(output_path) as processed_image:
        processed_size = processed_image.size
        processed_mode = processed_image.mode

    preview = text[: max(args.preview, 0)].replace("\n", " | ")
    print(f"image={image_path}")
    print(f"output={output_path}")
    print(f"elapsed_sec={elapsed:.3f}")
    print(f"original_size={original_size[0]}x{original_size[1]} mode={original_mode}")
    print(f"processed_size={processed_size[0]}x{processed_size[1]} mode={processed_mode}")
    print(f"text_len={len(text)}")
    print(f"line_count={len([line for line in text.splitlines() if line.strip()])}")
    print(f"preview={preview}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
