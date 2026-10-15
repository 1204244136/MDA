"""Android CI 的版本解析、Agent 校验、测试签名及 APK 验收；不发布。"""

import argparse
import base64
import hashlib
import io
import json
import os
import re
import subprocess
import tempfile
import zipfile
from pathlib import Path

import build_android as builder
from android_shell import SHELL_REVISION

ROOT = builder.ROOT
DIST = ROOT / "Android/build/ci"
MEMBERSHIP = ROOT / "Android/build/membership"


def metadata(run: int, attempt: int) -> dict:
    """同一 workflow 的下一次运行与重跑均递增，不依赖临时 runner 文件。"""
    if run < 1 or not 1 <= attempt <= 99:
        raise ValueError("run 必须为正数，attempt 必须在 1..99 之间")
    code = 100_000 + run * 100 + attempt
    if code > 2_100_000_000:
        raise ValueError("CI versionCode 超出 Android 范围")
    config = json.loads((ROOT / "Android/ci-versions.json").read_text(encoding="utf-8"))
    if config["shell_revision"] != SHELL_REVISION:
        raise ValueError("CI 外壳版本与身份桥接补丁锁定版本不一致")
    tag = subprocess.check_output(
        ["git", "describe", "--tags", "--match", "v[0-9]*", "--abbrev=0"],
        cwd=ROOT,
        text=True,
    ).strip()
    # 版本必须能被现有 Go 会员检查识别，不能退化为 short SHA / dev。
    match = re.fullmatch(r"v([1-9]\d*\.\d+\.\d+)(?:-[0-9A-Za-z.-]+)?", tag)
    if not match:
        raise ValueError(f"找不到 major >= 1 的可用版本 tag: {tag}")
    sha = subprocess.check_output(
        ["git", "rev-parse", "HEAD"],
        cwd=ROOT,
        text=True,
    ).strip()
    return dict(
        config,
        version=f"{match[1]}-android.ci.{run}.{attempt}+{sha[:8]}",
        version_code=code,
        commit=sha,
    )


def validate_agent(path: Path, ndk: Path, version: str) -> None:
    readelf = ndk / "toolchains/llvm/prebuilt/linux-x86_64/bin/llvm-readelf"
    for flag, required in [
        ("-h", "AArch64"),
        ("-l", "/system/bin/linker64"),
        ("-d", "NEEDED"),
    ]:
        output = subprocess.check_output([str(readelf), flag, str(path)], text=True)
        if required not in output:
            raise ValueError(f"Agent ELF 校验失败: {flag} 缺少 {required}")
    output = subprocess.check_output(["go", "version", "-m", str(path)], text=True)
    # -trimpath 的 Go 元数据不保留 -ldflags，注入版本直接校验字符串段。
    if (
        version.encode() not in path.read_bytes()
        or "GOOS=android" not in output
        or "GOARCH=arm64" not in output
    ):
        raise ValueError("Agent 的版本或目标平台与构建元数据不一致")


def sign_apk(apk: Path, tools: Path) -> None:
    """固定测试证书只在手动构建使用；密钥不落入 artifact 或 checkout。"""
    names = [
        "ANDROID_TEST_KEYSTORE_BASE64",
        "ANDROID_TEST_STORE_PASSWORD",
        "ANDROID_TEST_KEY_ALIAS",
        "ANDROID_TEST_KEY_PASSWORD",
        "ANDROID_TEST_CERT_SHA256",
    ]
    if any(not os.environ.get(name) for name in names):
        raise ValueError("缺少固定测试签名配置，拒绝输出无法覆盖安装的联调包")
    with tempfile.TemporaryDirectory(dir=os.environ.get("RUNNER_TEMP")) as directory:
        key = Path(directory) / "test.jks"
        key.write_bytes(base64.b64decode(os.environ[names[0]], validate=True))
        key.chmod(0o600)
        signed = Path(directory) / "signed.apk"
        subprocess.run(
            [
                str(tools / "apksigner"),
                "sign",
                "--ks",
                str(key),
                "--ks-pass",
                "env:ANDROID_TEST_STORE_PASSWORD",
                "--ks-key-alias",
                os.environ["ANDROID_TEST_KEY_ALIAS"],
                "--key-pass",
                "env:ANDROID_TEST_KEY_PASSWORD",
                "--v2-signing-enabled",
                "true",
                "--v3-signing-enabled",
                "true",
                "--out",
                str(signed),
                str(apk),
            ],
            check=True,
        )
        # APK 仍保留在同一目录；验证后才复制进最终 artifact。
        apk.write_bytes(signed.read_bytes())


def inspect_contents(apk: Path, version: str, agent: Path) -> dict:
    with zipfile.ZipFile(apk) as archive:
        names = set(archive.namelist())
        libraries = ["libgo-service.so", "libMaaFramework.so", "libMaaAgentServer.so"]
        for lib in libraries:
            if f"lib/arm64-v8a/{lib}" not in names:
                raise ValueError(f"APK 缺少 arm64 库 {lib}")
        if any(
            name.startswith("lib/") and not name.startswith("lib/arm64-v8a/")
            for name in names
        ):
            raise ValueError("测试 APK 包含 arm64 之外的 ABI")
        embedded_agent = archive.read("lib/arm64-v8a/libgo-service.so")
        # AGP 会剥离调试节，比较保留的 Go build ID 与版本，而非 strip 前后字节。
        with tempfile.TemporaryDirectory() as directory:
            packed = Path(directory) / "agent.so"
            packed.write_bytes(embedded_agent)
            ids = [
                subprocess.check_output(
                    ["go", "tool", "buildid", str(path)], text=True
                ).strip()
                for path in (agent, packed)
            ]
            info = subprocess.check_output(
                ["go", "version", "-m", str(packed)], text=True
            )
            if (
                not ids[0]
                or ids[0] != ids[1]
                or version.encode() not in embedded_agent
                or "GOOS=android" not in info
                or "GOARCH=arm64" not in info
            ):
                raise ValueError("APK 内 Agent 与本次构建产物不一致")
        with zipfile.ZipFile(io.BytesIO(archive.read("assets/pi.zip"))) as pi:
            interface = json.loads(pi.read("interface.json"))
            if interface["version"] != version:
                raise ValueError("PI 版本与 APK/Agent 不一致")
            if "mirrorchyan_rid" in interface or "github" in interface:
                raise ValueError("测试资源不应携带自动更新源")
            if "resource/model/ocr/rec.onnx" not in pi.namelist():
                raise ValueError("APK 缺少 OCR 模型")
        runtime = json.loads(archive.read("assets/agent/agent-runtime.json"))
        # descriptor 由固定外壳生成；不依赖条目嵌套形式，只检查开关实际入包。
        text = json.dumps(runtime)
        if (
            '"MDA_ANDROID_MEMBERSHIP_ENABLED": "1"' not in text
            or "MDA_IDENTITY_PROBE_ENABLED" in text
        ):
            raise ValueError("APK 未启用会员或误启用了原始身份探测")
    return {"agent_sha256": hashlib.sha256(embedded_agent).hexdigest()}


def verify_apk(apk: Path, tools: Path, meta: dict, pr: bool) -> Path:
    expected_package = "com.aliothmoon.maafw.mda." + (
        "membershippr" if pr else "membershiptest"
    )
    badging = subprocess.check_output(
        [str(tools / "aapt"), "dump", "badging", str(apk)], text=True
    )
    for value in [
        f"name='{expected_package}'",
        f"versionCode='{meta['version_code']}'",
        f"versionName='{meta['version']}'",
    ]:
        if value not in badging.splitlines()[0]:
            raise ValueError(f"APK 元数据错误: {value}")
    subprocess.run(
        [str(tools / "zipalign"), "-c", "-P", "16", "4", str(apk)], check=True
    )
    signature = subprocess.check_output(
        [
            str(tools / "apksigner"),
            "verify",
            "--verbose",
            "--print-certs",
            # minSdk >= 28 时默认只验证 v3，打印 v2=false 不代表签名块缺失。
            # 扩展验证范围到 API 24，要求 v2/v3 两条验签路径均可用。
            "--min-sdk-version",
            "24",
            str(apk),
        ],
        text=True,
    )
    print(signature, flush=True)
    cert = re.search(r"Signer #1 certificate SHA-256 digest: ([a-fA-F0-9]+)", signature)
    if (
        not cert
        or "Verified using v2 scheme (APK Signature Scheme v2): true" not in signature
    ):
        raise ValueError("APK 签名未通过 v2 校验")
    digest = cert[1].lower()
    if not pr:
        expected = (
            os.environ.get("ANDROID_TEST_CERT_SHA256", "").replace(":", "").lower()
        )
        if not expected or digest != expected:
            raise ValueError("APK 签名证书与固定测试证书指纹不一致")
    agent = MEMBERSHIP / "agent-dist/arm64-v8a/jniLibs/libgo-service.so"
    details = inspect_contents(apk, meta["version"], agent)
    DIST.mkdir(parents=True, exist_ok=True)
    stem = f"MDA-android-arm64-{'pr' if pr else 'test'}-{meta['version']}"
    target = DIST / f"{stem}.apk"
    target.write_bytes(apk.read_bytes())
    report = dict(
        meta,
        **details,
        package=expected_package,
        signing_cert_sha256=digest,
        sha256=hashlib.sha256(target.read_bytes()).hexdigest(),
        bytes=target.stat().st_size,
    )
    (DIST / f"{stem}.json").write_text(
        json.dumps(report, indent=2) + "\n", encoding="utf-8"
    )
    (DIST / f"{stem}.sha256").write_text(
        f"{report['sha256']}  {target.name}\n", encoding="utf-8"
    )
    return target


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("phase", choices=["meta", "agent", "sign", "verify"])
    parser.add_argument("--ndk", type=Path)
    parser.add_argument("--tools", type=Path)
    parser.add_argument("--pr", action="store_true")
    args = parser.parse_args()
    if args.phase == "meta":
        meta = metadata(
            int(os.environ["GITHUB_RUN_NUMBER"]), int(os.environ["GITHUB_RUN_ATTEMPT"])
        )
        DIST.mkdir(parents=True, exist_ok=True)
        (DIST / "metadata.json").write_text(
            json.dumps(meta, indent=2) + "\n", encoding="utf-8"
        )
        with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as output:
            output.writelines(f"{key}={value}\n" for key, value in meta.items())
        return
    meta = json.loads((DIST / "metadata.json").read_text(encoding="utf-8"))
    if args.phase == "agent":
        if args.ndk is None:
            parser.error("agent 需要 --ndk")
        builder.BUILD = MEMBERSHIP
        agent = builder.build_agent(args.ndk, meta["version"])
        validate_agent(agent, args.ndk, meta["version"])
        return
    if args.tools is None:
        parser.error("sign/verify 需要 --tools")
    apk = MEMBERSHIP / "MDA-android-membership-test.apk"
    if args.phase == "sign":
        sign_apk(apk, args.tools)
    else:
        print(verify_apk(apk, args.tools, meta, args.pr))


if __name__ == "__main__":
    main()
