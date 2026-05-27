from __future__ import annotations

import hashlib
import json
import os
import platform
import socket
import subprocess
from datetime import datetime, timezone
from pathlib import Path

from app.paths import data_dir


MACHINE_FILE = "machine.json"
MACHINE_ALGORITHM = "stable-hardware-v1"


def load_machine() -> dict[str, str]:
    path = machine_file_path()
    if path.exists():
        with path.open("r", encoding="utf-8") as file:
            machine = json.load(file)
        migrated = migrate_machine(machine)
        if migrated != machine:
            save_machine(migrated)
        return migrated

    path.parent.mkdir(parents=True, exist_ok=True)
    machine = create_machine()
    save_machine(machine)
    return machine


def machine_file_path() -> Path:
    """返回机器码文件路径。"""
    return data_dir() / MACHINE_FILE


def save_machine(machine: dict[str, str]) -> None:
    """
    保存机器码信息。

    Args:
        machine: 机器码信息字典。
    """
    path = machine_file_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as file:
        json.dump(machine, file, ensure_ascii=False, indent=2)
        file.write("\n")


def migrate_machine(machine: dict[str, str]) -> dict[str, str]:
    """
    将旧随机安装机器码迁移为稳定机器码。

    Args:
        machine: 已保存的机器码信息。

    Returns:
        dict[str, str]: 迁移后的机器码信息。
    """
    current_id = str(machine.get("machine_id", "")).strip()
    if machine.get("algorithm") == MACHINE_ALGORITHM and current_id:
        return machine

    stable = create_machine()
    if current_id and current_id != stable["machine_id"]:
        stable["legacy_machine_id"] = current_id
    if machine.get("created_at"):
        stable["created_at"] = str(machine["created_at"])
    stable["migrated_at"] = datetime.now(timezone.utc).isoformat()
    return stable


def create_machine() -> dict[str, str]:
    stable_source, source_value = stable_machine_source()
    created_at = datetime.now(timezone.utc).isoformat()
    machine_id = build_machine_id(stable_source, source_value)
    return {
        "machine_id": machine_id,
        "algorithm": MACHINE_ALGORITHM,
        "source": stable_source,
        "created_at": created_at,
    }


def stable_machine_source() -> tuple[str, str]:
    """
    获取跨重装稳定的机器识别来源。

    Returns:
        tuple[str, str]: 来源名称和来源值。
    """
    system = platform.system().lower()
    if system == "darwin":
        hardware_uuid = read_macos_platform_uuid()
        if hardware_uuid:
            return "macos_ioplatform_uuid", hardware_uuid
    if system == "windows":
        hardware_uuid = read_windows_hardware_uuid()
        if hardware_uuid:
            return "windows_hardware_uuid", hardware_uuid
    return "fallback_host_user", fallback_machine_source()


def read_macos_platform_uuid() -> str:
    """
    读取 macOS 的 IOPlatformUUID。

    Returns:
        str: 硬件 UUID；读取失败时返回空字符串。
    """
    try:
        result = subprocess.run(
            ["ioreg", "-rd1", "-c", "IOPlatformExpertDevice"],
            capture_output=True,
            text=True,
            timeout=5,
        )
    except (OSError, subprocess.TimeoutExpired):
        return ""
    for line in result.stdout.splitlines():
        if "IOPlatformUUID" not in line:
            continue
        parts = line.split("=", 1)
        if len(parts) != 2:
            continue
        value = parts[1].strip().strip('"')
        if value:
            return value
    return ""


def read_windows_hardware_uuid() -> str:
    """
    读取 Windows 的硬件 UUID。

    Returns:
        str: 硬件 UUID；读取失败时返回空字符串。
    """
    for command in windows_uuid_commands():
        value = run_uuid_command(command)
        if value:
            return value
    return ""


def windows_uuid_commands() -> list[list[str]]:
    """
    返回 Windows UUID 读取命令列表。

    Returns:
        list[list[str]]: 可依次尝试的命令。
    """
    return [
        ["powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystemProduct).UUID"],
        ["wmic", "csproduct", "get", "UUID"],
    ]


def run_uuid_command(command: list[str]) -> str:
    """
    执行 UUID 读取命令并解析输出。

    Args:
        command: 待读取的系统命令。

    Returns:
        str: 解析后的 UUID；不可用时返回空字符串。
    """
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=5)
    except (OSError, subprocess.TimeoutExpired):
        return ""
    for line in result.stdout.splitlines():
        value = line.strip()
        if not value or value.lower() == "uuid":
            continue
        if value == "FFFFFFFF-FFFF-FFFF-FFFF-FFFFFFFFFFFF":
            continue
        return value
    return ""


def fallback_machine_source() -> str:
    """
    生成硬件 UUID 不可用时的稳定兜底来源。

    Returns:
        str: 兜底机器来源字符串。
    """
    return "|".join(
        [
            platform.system(),
            platform.machine(),
            platform.node(),
            socket.gethostname(),
            str(Path.home()),
            os.getenv("USER", ""),
            os.getenv("USERNAME", ""),
        ]
    )


def build_machine_id(source: str, source_value: str) -> str:
    """
    根据稳定来源生成机器码。

    Args:
        source: 机器来源名称。
        source_value: 机器来源值。

    Returns:
        str: sha256 格式机器码。
    """
    raw_parts = [
        MACHINE_ALGORITHM,
        source,
        source_value,
    ]
    raw = "|".join(raw_parts)
    digest = hashlib.sha256(raw.encode("utf-8")).hexdigest()
    return f"sha256-{digest}"


def cookie_machine_ids(machine: dict[str, str]) -> list[str]:
    """
    返回可用于解密 cookie 的机器码列表。

    Args:
        machine: 当前机器码信息。

    Returns:
        list[str]: 当前机器码和兼容旧机器码。
    """
    ids: list[str] = []
    for key in ["machine_id", "legacy_machine_id"]:
        value = str(machine.get(key, "")).strip()
        if value and value not in ids:
            ids.append(value)
    return ids
