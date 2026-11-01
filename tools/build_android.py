"""准备 MDA Android 资源及 APK；CI 使用显式版本和构建号。"""

import argparse
import json
import os
import platform
import re
import shutil
import subprocess
import sys
from pathlib import Path

if __package__:
    from .generate_zh_tw import generate_zh_tw
else:
    from generate_zh_tw import generate_zh_tw

ROOT = Path(__file__).resolve().parent.parent
ANDROID_VERSIONS = json.loads(
    (ROOT / "Android/ci-versions.json").read_text(encoding="utf-8")
)
RELEASE = ANDROID_VERSIONS["release"]
BUILD = ROOT / "Android" / "build"
MEMBER_BUILD = BUILD / "membership"
MAAFW_TAG = ANDROID_VERSIONS["maafw_tag"]
DEV_VERSION = "0.0.0-dev.2"
TEST_VERSION = DEV_VERSION
DEFAULT_TEST_VERSION = "2.5.0-android-test.2"
# 正式版本同时覆盖 MaaFwApp 的 versionName 与 PI 的 version，必须能被会员检查识别。
# 正式 tag 不接受 +build：git-cliff 的正式版 tag-pattern 不匹配它，发布正文会错位。
RELEASE_VERSION = re.compile(r"[1-9]\d*\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?")
# 测试版本带 +commit，供会员检查区分 Android CI 包。
MEMBER_VERSION = re.compile(
    r"[1-9]\d*\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?"
)
# 与实体按键审计测试对应；只给出提示，不替换原有动作。
KEYBOARD_PIPELINES = {
    "Battle/CustomBurst.json": "CustomBurst",
    "Event/Special/CoinRushShowdown.json": "CoinRushShowdown",
    "Event/Special/PersonaOnFrontline.json": "PersonaOnFrontline",
}

RELEASE_NOTICE = """# MDA 手机版

面向国际服 NIKKE、arm64、Shizuku/root，默认后台虚拟屏 1280×720。
本包随 MDA 正式版发布，安装后可在应用内检查更新。

1. 安装后授予 Shizuku 或 root，等待服务连接；后台虚拟屏需要 Android 10+。
2. 先运行「Agent 自检」，确认连接和会员/额度信息，再运行「启动国际服」。
3. 完成游戏登录并启用横屏，运行「进入大厅」，之后逐项运行其他任务。

## 会员

手机与电脑必须分别绑定、分别开通，并使用不同的会员 ID。
客户端沿用原 DoroPay 六字段协议，不需要协议或数据库迁移。
离线缓存、定向额度券与桌面版一致，换绑前请确认目标设备。

## 实体按键功能：手机版请勿使用

- 自定义爆裂：依赖 A/S/D。
- COIN RUSH SHOWDOWN 小游戏：依赖空格键。
- PERSONA ON FRONTLINE 小游戏：依赖 W/A/D。

上述功能暂未适配触控。对应活动的其他功能可以正常运行，
运行前请关闭「大活动小游戏」；「当前活动」若指向上述主题，同样适用。
"""

TEST_NOTICE = """# MDA Android 测试版

使用 Shizuku 或 root，固定后台 1280×720。

1. 先只添加并运行「Agent 自检」，确认 Go Agent 正常连接。
2. 单独运行「启动国际服」，完成登录并在游戏中启用横屏。
3. 再测试「进入大厅」，确认画面识别后逐项测试其他任务。

## 实体按键功能：手机版请勿使用

- 自定义爆裂：依赖 A/S/D。
- COIN RUSH SHOWDOWN 小游戏：依赖空格键。
- PERSONA ON FRONTLINE 小游戏：依赖 W/A/D。

上述功能暂未适配触控。对应活动的其他功能可继续测试，
运行前请关闭「大活动小游戏」；「当前活动」若指向上述主题，同样适用。

本包不是正式发行包，不承载应用内更新；测试反馈请附带导出的日志 ZIP。
"""

MOBILE_WARNINGS = {
    "zh_cn": {
        "task": "**手机版请勿使用：本功能依赖实体键盘 A/S/D，暂未适配触控。**",
        "task_suffix": "（实体按键，手机勿用）",
        "event": "**手机版注意：COIN RUSH SHOWDOWN 小游戏依赖空格键，PERSONA ON FRONTLINE 小游戏依赖 W/A/D。请勿使用这两个小游戏；运行对应主题的其他功能前，请关闭「大活动小游戏」。**",
        "option_suffix": "（部分主题依赖实体按键）",
        "case_suffix": "（小游戏手机勿用）",
        "CoinRushShowdown": "**手机版请勿运行此主题的小游戏：依赖实体空格键，暂未适配触控。请关闭「大活动小游戏」后再运行其他功能。**",
        "PersonaOnFrontline": "**手机版请勿运行此主题的小游戏：依赖实体键盘 W/A/D，暂未适配触控。请关闭「大活动小游戏」后再运行其他功能。**",
    },
    "en_us": {
        "task": "**Do not use on Android: this feature uses physical keyboard keys A/S/D and has no touch adaptation.**",
        "task_suffix": " (keyboard; do not use on Android)",
        "event": "**Android: the COIN RUSH SHOWDOWN minigame uses Space; the PERSONA ON FRONTLINE minigame uses W/A/D. Do not use these minigames. Turn off the large-event minigame option before running other features for these themes.**",
        "option_suffix": " (some themes require a keyboard)",
        "case_suffix": " (do not use minigame on Android)",
        "CoinRushShowdown": "**Do not use this minigame on Android: it requires Space and has no touch adaptation. Disable the large-event minigame option before running other features.**",
        "PersonaOnFrontline": "**Do not use this minigame on Android: it requires W/A/D and has no touch adaptation. Disable the large-event minigame option before running other features.**",
    },
}


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=4) + "\n", encoding="utf-8"
    )


def annotate_keyboard_features(target: Path) -> None:
    """仅在 Android 生成资源中添加中英文警告，保留任务与选项行为。"""
    event_path = target / "tasks/LargeEvent.json"
    event = json.loads(event_path.read_text(encoding="utf-8"))
    warning_key = "option.LargeEventMiniGame.android_warning"
    event["option"]["LargeEventMiniGame"]["description"] = "$" + warning_key
    for case in event["option"]["LargeEventTheme"]["cases"]:
        if case["name"] in {"CoinRushShowdown", "PersonaOnFrontline"}:
            case["description"] = (
                f"$option.LargeEventTheme.{case['name']}.android_warning"
            )
    write_json(event_path, event)

    for language, warning in MOBILE_WARNINGS.items():
        locale_path = target / f"locales/interface/{language}.json"
        locale = json.loads(locale_path.read_text(encoding="utf-8"))
        locale["task.CustomBurst.label"] += warning["task_suffix"]
        locale["task.CustomBurst.description"] = (
            warning["task"] + "\n\n" + locale["task.CustomBurst.description"]
        )
        locale["task.LargeEvent.description"] = (
            warning["event"] + "\n\n" + locale["task.LargeEvent.description"]
        )
        locale["option.LargeEventContent.LargeEventMiniGame"] += warning[
            "option_suffix"
        ]
        locale["option.LargeEventTheme.CurrentEvent.description"] += (
            "\n\n" + warning["event"]
        )
        # 新提示键追加到末尾，原有键序保持不变。
        locale[warning_key] = warning["event"]
        for theme in ("CoinRushShowdown", "PersonaOnFrontline"):
            locale[f"option.LargeEventTheme.{theme}"] += warning["case_suffix"]
            locale[f"option.LargeEventTheme.{theme}.android_warning"] = warning[theme]
        for name in ("DailyFull", "SelfUse"):
            key = f"preset.{name}.description"
            locale[key] += "\n\n" + warning["event"]
        write_json(locale_path, locale)


def prepare_assets(version: str, release: bool) -> Path:
    """复用全部任务与预设，并在 Android 生成资源中注明按键限制。

    正式包保留 GitHub / Mirror酱 更新源与正式名称；测试包改用独立包名并移除更新源，
    避免联调包把用户带到正式渠道。
    """
    source = ROOT / "assets"
    target = BUILD / "assets"
    if target.exists():
        shutil.rmtree(target)
    target.mkdir(parents=True)
    for directory in ("resource", "locales", "tasks"):
        shutil.copytree(source / directory, target / directory)
    if not (target / "resource/model/ocr/rec.onnx").is_file():
        raise RuntimeError("缺少 OCR 模型，请先运行 tools/configure.py")
    for name in ("README.md", "LICENSE"):
        shutil.copy2(ROOT / name, target / name)

    interface = json.loads((source / "interface.json").read_text(encoding="utf-8"))
    interface.update(
        version=version,
        description="国际服 / Shizuku / 后台 1280×720。全部任务已开放；实体按键功能请勿使用，详见任务提示。",
        controller=[c for c in interface["controller"] if c["type"] == "Adb"],
        **{"import": ["tasks/Android.json", *interface["import"]]},
    )
    if release:
        notice_name = "AndroidNotice.md"
        notice = RELEASE_NOTICE
        interface.update(name="MDA", welcome=notice_name)
        # mirrorchyan_multiplatform 只描述桌面多平台，Android 包不需要。
        interface.pop("mirrorchyan_multiplatform", None)
    else:
        notice_name = "AndroidTestNotice.md"
        notice = TEST_NOTICE
        interface.update(name="MDA Android 测试", welcome=notice_name)
        interface.pop("mirrorchyan_rid", None)
        interface.pop("mirrorchyan_multiplatform", None)
        interface.pop("github", None)
    write_json(target / "interface.json", interface)
    (target / notice_name).write_text(notice, encoding="utf-8")
    write_json(
        target / "tasks/Android.json",
        {
            "task": [
                {
                    "name": "AndroidOpenGame",
                    "label": "启动国际服",
                    "description": "将已安装的国际服启动到后台虚拟屏；登录后请确认游戏为横屏。",
                    "entry": "AndroidOpenGame",
                    "controller": ["ADB"],
                    "group": ["preprocess"],
                },
                {
                    "name": "AndroidAgentCheck",
                    "label": "Agent 自检",
                    "description": "调用现有 Go 自定义动作；显示会员与额度信息表示 Agent 回调链路已连通。",
                    "entry": "QuotaDisplayMain",
                    "controller": ["ADB"],
                    "group": ["utility"],
                },
            ],
            "preset": [
                {
                    "name": "AndroidSmokeTest",
                    "label": "Agent 自检（不操作游戏）",
                    "task": [{"name": "AndroidAgentCheck"}],
                }
            ],
        },
    )
    annotate_keyboard_features(target)
    generate_zh_tw(target)
    write_json(
        target / "resource/pipeline/Android.json",
        {
            "AndroidOpenGame": {
                "desc": "只拉起一次国际服，后续登录交给用户及现有大厅流程",
                "action": {
                    "type": "StartApp",
                    "param": {"package": "com.proximabeta.nikke"},
                },
            }
        },
    )
    print(f"Android 资源: {target}", flush=True)
    return target


def build_agent(ndk: Path, version: str = "dev") -> Path:
    host = {
        "Windows": "windows-x86_64",
        "Linux": "linux-x86_64",
        "Darwin": "darwin-x86_64",
    }[platform.system()]
    suffix = ".exe" if os.name == "nt" else ""
    clang = ndk / "toolchains/llvm/prebuilt" / host / "bin" / f"clang{suffix}"
    if not clang.is_file():
        raise RuntimeError(f"找不到 NDK clang: {clang}")
    output = BUILD / "agent-dist/arm64-v8a/jniLibs/libgo-service.so"
    output.parent.mkdir(parents=True, exist_ok=True)
    triple = "--target=aarch64-linux-android28"
    env = dict(
        os.environ,
        GOOS="android",
        GOARCH="arm64",
        CGO_ENABLED="1",
        CC=str(clang),
        CGO_CFLAGS=triple,
        CGO_LDFLAGS=triple,
    )
    subprocess.run(
        [
            "go",
            "build",
            "-mod=readonly",
            "-trimpath",
            "-buildvcs=false",
            f"-ldflags=-X main.Version={version}",
            "-o",
            str(output),
            ".",
        ],
        cwd=ROOT / "agent/go-service",
        env=env,
        check=True,
    )
    print(f"Android Agent: {output}", flush=True)
    return output


def prepare_build_number(shell: Path, number: int | None = None) -> tuple[int, Path]:
    """无需提交也能覆盖安装并触发外壳重新解包资源。"""
    if number is None:
        base = int(
            subprocess.check_output(
                ["git", "rev-list", "--count", "HEAD"], cwd=shell, text=True
            ).strip()
        )
        counter = BUILD / "build-number.txt"
        previous = (
            int(counter.read_text(encoding="utf-8").strip())
            if counter.exists()
            else base
        )
        number = max(base, previous) + 1
    if not 1 <= number <= 2_100_000_000:
        raise ValueError("versionCode 必须在 1..2100000000 之间")
    BUILD.mkdir(parents=True, exist_ok=True)
    init = BUILD / "version-code.init.gradle"
    init.write_text(
        "gradle.beforeProject { p ->\n"
        "    if (p.path == ':app') {\n"
        "        p.pluginManager.withPlugin('com.android.application') {\n"
        "            p.extensions.getByName('androidComponents').finalizeDsl { dsl ->\n"
        f"                dsl.defaultConfig.versionCode = {number}\n"
        "            }\n"
        "        }\n"
        "    }\n"
        "}\n",
        encoding="utf-8",
    )
    return number, init


def main() -> None:
    global BUILD, TEST_VERSION, MAAFW_TAG
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--shell", type=Path, help="独立 MaaFwApp checkout，构建缓存写入该目录"
    )
    parser.add_argument("--ndk", type=Path, help="Android NDK 目录")
    parser.add_argument("--ndk-version", default="29.0.14206865")
    parser.add_argument("--prepare-only", action="store_true", help="只准备 Android 资源")
    parser.add_argument("--version", help="统一 APK、PI 和 Go Agent 的版本")
    parser.add_argument(
        "--version-code", type=int, help="CI 显式构建号，不使用本地计数器"
    )
    parser.add_argument("--maafw-tag", default=MAAFW_TAG)
    parser.add_argument(
        "--release", action="store_true", help="构建正式发行 APK（release + 正式签名）"
    )
    parser.add_argument(
        "--membership", action="store_true", help="构建启用正式会员检查的测试包"
    )
    parser.add_argument("--pr-build", action="store_true", help="使用独立 PR 包名")
    parser.add_argument(
        "--offline", action="store_true", help="Gradle 仅使用已缓存的依赖"
    )
    parser.add_argument(
        "--skip-agent", action="store_true", help="未改动 Go 时复用已有 Android Agent"
    )
    parser.add_argument(
        "--skip-framework", action="store_true", help="已部署指定版本的框架时跳过下载"
    )
    args = parser.parse_args()
    if RELEASE["abi"] != "arm64-v8a":
        parser.error("当前 Android Agent 只支持 arm64-v8a")
    if args.release and args.membership:
        parser.error("--release 与 --membership 不能同时使用")
    if args.pr_build and not args.membership:
        parser.error("--pr-build 仅用于 --membership")
    if args.release:
        if not args.version:
            parser.error("正式构建必须指定 --version（正式 tag 去掉 v）")
        if not RELEASE_VERSION.fullmatch(args.version):
            parser.error("正式版本必须形如 1.12.3 或 1.12.3-beta.1")
    MAAFW_TAG = args.maafw_tag
    if args.membership or args.release:
        BUILD = MEMBER_BUILD
    if args.release:
        TEST_VERSION = args.version
    elif args.membership:
        TEST_VERSION = args.version or DEFAULT_TEST_VERSION
    elif args.version:
        TEST_VERSION = args.version
    if args.membership and not MEMBER_VERSION.fullmatch(TEST_VERSION):
        parser.error("会员测试需要 major >= 1 的版本，防止进入 dev 免会员模式")
    if args.release and not args.prepare_only:
        # 正式包必须签名；在复制整仓资源前失败，避免几十秒后才报缺 keystore。
        missing = [
            name
            for name in (
                "KEYSTORE_PATH",
                "KEYSTORE_PASSWORD",
                "KEY_ALIAS",
                "KEY_PASSWORD",
            )
            if not os.environ.get(name)
        ]
        if missing:
            parser.error("正式构建需要签名环境变量: " + ", ".join(missing))
    prepare_assets(TEST_VERSION, args.release)
    if args.membership:
        notice = BUILD / "assets/AndroidTestNotice.md"
        text = notice.read_text(encoding="utf-8").replace(
            "本包不是正式发行包，不承载应用内更新；测试反馈请附带导出的日志 ZIP。",
            "本包启用正式会员与运行额度检查。手机会员独立开通，请使用与电脑不同的用户 ID。"
            "沿用原 DoroPay 六字段接口，无需协议或数据库迁移。",
        )
        notice.write_text(text, encoding="utf-8")
    if args.prepare_only:
        return
    if args.shell is None or args.ndk is None:
        parser.error("构建 APK 必须指定 --shell 与 --ndk")
    shell = args.shell.resolve()
    if args.membership or args.release:
        from android_shell import apply_overlay

        apply_overlay(shell)
    if args.skip_framework:
        marker = shell / ".maafwversion"
        if (
            not marker.is_file()
            or marker.read_text(encoding="utf-8").strip() != MAAFW_TAG
        ):
            parser.error(f"--skip-framework 需要已部署 {MAAFW_TAG}")
    # 外壳只从 local.properties 读取 ABI；-P 和环境变量不会影响这里。
    properties = shell / "local.properties"
    lines = (
        properties.read_text(encoding="utf-8").splitlines()
        if properties.exists()
        else []
    )
    lines = [
        line
        for line in lines
        if not line.startswith(("build.debugAbi=", "build.releaseAbi="))
    ]
    sdk = os.environ.get("ANDROID_HOME") or os.environ.get("ANDROID_SDK_ROOT")
    if sdk and not any(line.startswith("sdk.dir=") for line in lines):
        lines.append(f"sdk.dir={sdk}")
    abi_lines = ["build.debugAbi=arm64-v8a"]
    if args.release:
        abi_lines.append(f"build.releaseAbi={RELEASE['abi']}")
    properties.write_text("\n".join([*lines, *abi_lines]) + "\n", encoding="utf-8")
    agent_dist = BUILD / "agent-dist/arm64-v8a/jniLibs/libgo-service.so"
    if args.skip_agent:
        if not agent_dist.is_file():
            parser.error("--skip-agent 需要已构建的 Android Agent")
    else:
        version = TEST_VERSION if (args.membership or args.release) else "dev"
        build_agent(args.ndk.resolve(), version)
    if not args.skip_framework:
        subprocess.run(
            [
                sys.executable,
                "scripts/setup_maa_framework.py",
                "--tag",
                MAAFW_TAG,
                "--abi",
                "arm64-v8a",
            ],
            cwd=shell,
            check=True,
        )
    if args.release:
        profile = "Android/release.profile.yaml"
    elif args.membership:
        profile = (
            "Android/pr.profile.yaml"
            if args.pr_build
            else "Android/membership.profile.yaml"
        )
    else:
        profile = "Android/profile.yaml"
    env = dict(
        os.environ,
        PI_PROFILE=str(ROOT / profile),
        BUILD_VERSION_NAME=TEST_VERSION,
        BUILD_NDK_VERSION=args.ndk_version,
    )
    gradle = shell / ("gradlew.bat" if os.name == "nt" else "gradlew")
    number, init = prepare_build_number(shell, args.version_code)
    task = ":app:assembleRelease" if args.release else ":app:assembleDebug"
    command = [
        str(gradle),
        "--init-script",
        str(init),
        task,
        "--console=plain",
    ]
    if args.offline:
        command.append("--offline")
    subprocess.run(
        command,
        cwd=shell,
        env=env,
        check=True,
    )
    output = "release" if args.release else "debug"
    apks = list((shell / f"app/build/outputs/apk/{output}").glob("*.apk"))
    if len(apks) != 1:
        raise RuntimeError(f"预期一个 {output} APK，实际发现 {len(apks)} 个")
    if args.release:
        destination = BUILD / f"{RELEASE['artifact_prefix']}-{TEST_VERSION}.apk"
    elif args.membership:
        destination = BUILD / "MDA-android-membership-test.apk"
    else:
        destination = BUILD / "MDA-android-arm64-test.apk"
    shutil.copy2(apks[0], destination)
    if args.membership:
        versioned = BUILD / f"MDA-android-membership-{TEST_VERSION}.apk"
        shutil.copy2(destination, versioned)
        print(f"带版本号的会员联调 APK: {versioned}", flush=True)
    (BUILD / "build-number.txt").write_text(str(number) + "\n", encoding="utf-8")
    print(f"Android APK: {destination}", flush=True)


if __name__ == "__main__":
    main()
