from pathlib import Path

import os
import platform
import shutil
import subprocess
import sys

try:
    import jsonc
except ModuleNotFoundError as e:
    raise ImportError(
        "Missing dependency 'json-with-comments' (imported as 'jsonc').\n"
        f"Install it with:\n  {sys.executable} -m pip install json-with-comments\n"
        "Or add it to your project's requirements."
    ) from e

from configure import configure_ocr_model

if __package__:
    from .generate_zh_tw import generate_zh_tw
else:
    from generate_zh_tw import generate_zh_tw

try:
    from cli_support import configure_utf8_stdio
except ImportError:
    configure_utf8_stdio = None

if configure_utf8_stdio:
    configure_utf8_stdio()


working_dir = Path(__file__).parent.parent.resolve()
install_path = working_dir / Path("install")

build_go_only = "--build-go-only" in sys.argv
args = [a for a in sys.argv[1:] if a != "--build-go-only"]
version = args[0] if len(args) > 0 else "v0.0.1"
os_name = args[1] if len(args) > 1 else ""
arch = args[2] if len(args) > 2 else ""

if not build_go_only and (not os_name or not arch):
    print("Usage: python install.py [--build-go-only] <version> <os> <arch>")
    print("Example: python install.py v1.0.0 win x86_64")
    print("         python install.py --build-go-only v1.0.0 win x86_64")
    sys.exit(1)


def install_deps():
    if not (working_dir / "deps" / "bin").exists():
        print('Please download the MaaFramework to "deps" first.')
        print('请先下载 MaaFramework 到 "deps"。')
        sys.exit(1)

    maafw_path = install_path / "maafw"

    # 复制 MaaFramework bin 到 install/maafw/
    shutil.copytree(
        working_dir / "deps" / "bin",
        maafw_path,
        ignore=shutil.ignore_patterns(
            "*MaaNode*",
        ),
        dirs_exist_ok=True,
    )

    # 复制 MaaAgentBinary 到 install/maafw/
    agent_binary_src = working_dir / "deps" / "share" / "MaaAgentBinary"
    if agent_binary_src.exists():
        shutil.copytree(
            agent_binary_src,
            maafw_path / "MaaAgentBinary",
            dirs_exist_ok=True,
        )


def install_resource():

    configure_ocr_model()

    shutil.copytree(
        working_dir / "assets" / "resource",
        install_path / "resource",
        dirs_exist_ok=True,
    )
    shutil.copytree(
        working_dir / "assets" / "tasks",
        install_path / "tasks",
        dirs_exist_ok=True,
    )
    shutil.copytree(
        working_dir / "assets" / "locales",
        install_path / "locales",
        dirs_exist_ok=True,
    )
    shutil.copy2(
        working_dir / "assets" / "interface.json",
        install_path,
    )

    with open(install_path / "interface.json", "r", encoding="utf-8") as f:
        interface = jsonc.load(f)

    interface["version"] = version

    with open(install_path / "interface.json", "w", encoding="utf-8") as f:
        jsonc.dump(interface, f, ensure_ascii=False, indent=4)

    generate_zh_tw(install_path)


def install_chores():
    shutil.copy2(
        working_dir / "README.md",
        install_path,
    )
    shutil.copy2(
        working_dir / "LICENSE",
        install_path,
    )


def build_go_agent():
    """编译 Go Agent 为目标平台的二进制文件"""
    go_service_dir = working_dir / "agent" / "go-service"
    if not go_service_dir.exists():
        print(f"Go service source not found: {go_service_dir}")
        return False

    # 确定目标平台
    if os_name:
        goos = {"win": "windows", "macos": "darwin", "linux": "linux"}.get(
            os_name, os_name
        )
    else:
        system = platform.system().lower()
        goos = {"windows": "windows", "darwin": "darwin"}.get(system, "linux")

    if arch:
        goarch = {"x86_64": "amd64", "aarch64": "arm64"}.get(arch, arch)
    else:
        machine = platform.machine().lower()
        goarch = (
            "amd64"
            if machine in ("x86_64", "amd64")
            else "arm64"
            if machine in ("aarch64", "arm64")
            else machine
        )

    ext = ".exe" if goos == "windows" else ""

    agent_dir = install_path / "agent"
    agent_dir.mkdir(parents=True, exist_ok=True)
    output_path = agent_dir / f"go-service{ext}"

    print(f"  Target platform: {goos}/{goarch}")
    print(f"  Output path: {output_path}")

    env = {**os.environ, "GOOS": goos, "GOARCH": goarch, "CGO_ENABLED": "0"}

    # go mod tidy
    tidy_result = subprocess.run(
        ["go", "mod", "tidy"],
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=env,
    )
    if tidy_result.returncode != 0:
        print(f"  go mod tidy failed: {tidy_result.stderr}")
        return False

    # 构建 ldflags
    ldflags = f"-X main.Version={version}"

    # go build
    build_cmd = [
        "go",
        "build",
        "-trimpath",
        f"-ldflags={ldflags}",
        "-o",
        str(output_path),
        ".",
    ]

    print(f"  Build command: {' '.join(build_cmd)}")

    result = subprocess.run(
        build_cmd,
        cwd=go_service_dir,
        capture_output=True,
        text=True,
        encoding="utf-8",
        env=env,
    )
    if result.stdout:
        print(result.stdout)
    if result.returncode != 0:
        print(f"  Go build failed: {result.stderr}")
        return False

    print(f"  Go agent built successfully: {output_path}")
    return True


def install_agent():
    """安装编译好的 Go Agent 二进制到 install 目录"""
    agent_dir = install_path / "agent"
    agent_dir.mkdir(parents=True, exist_ok=True)

    # 复制已编译的 Go Agent 二进制
    ext = ".exe" if os_name == "win" else ""
    go_binary = agent_dir / f"go-service{ext}"
    if go_binary.exists():
        print(f"  Go agent binary found: {go_binary}")
    else:
        print("  Warning: Go agent binary not found, attempting to build...")
        if not build_go_agent():
            print("  Error: Go agent binary not found and build failed.")
            sys.exit(1)


if __name__ == "__main__":
    if build_go_only:
        build_go_agent()
    else:
        install_deps()
        install_resource()
        install_chores()
        install_agent()

        print(f"Install to {install_path} successfully.")
