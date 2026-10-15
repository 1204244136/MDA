"""将正式会员身份桥接应用到固定版本的独立 MaaFwApp checkout。"""

import shutil
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SHELL_REVISION = "fb0aa7b2f294938c61289859c503691f91144e1c"
JAVA_ROOT = Path("app/src/main/java/com/aliothmoon/maafw")


def patch_once(path: Path, old: str, new: str) -> None:
    text = path.read_text(encoding="utf-8")
    if new in text:
        return
    if text.count(old) != 1:
        raise RuntimeError(f"外壳补丁定位失败，未修改: {path}")
    path.write_text(text.replace(old, new, 1), encoding="utf-8")


def apply_overlay(shell: Path) -> None:
    revision = subprocess.check_output(
        ["git", "rev-parse", "HEAD"], cwd=shell, text=True
    ).strip()
    if revision != SHELL_REVISION:
        raise RuntimeError(
            f"会员桥接只验证过 MaaFwApp {SHELL_REVISION}，当前为 {revision}"
        )
    destination = shell / JAVA_ROOT / "membership"
    destination.mkdir(parents=True, exist_ok=True)
    for name in ("AndroidIdentity.kt", "AndroidIdentityCompat.java"):
        shutil.copy2(ROOT / "Android/shell-overlay" / name, destination / name)
    patch_once(
        shell / JAVA_ROOT / "remote/MaaRunner.kt",
        "        val reusable = loadedAgents == payload.agents &&",
        "        val reusable = !com.aliothmoon.maafw.membership.AndroidIdentityCompat.refreshRequired(payload.piEnv) &&\n"
        "            loadedAgents == payload.agents &&",
    )
    path = shell / JAVA_ROOT / "runner/MaaFrameworkRunnerPort.kt"
    anchor = "    private val textInputSink: () -> ITextInputSink,\n"
    patch_once(
        path,
        anchor,
        anchor
        + "    private val membershipEnv: () -> Map<String, String> = { emptyMap() },\n",
    )
    patch_once(
        path,
        "            piEnv = plan.piEnv,",
        "            piEnv = plan.piEnv + membershipEnv(),",
    )
    anchor = "            textInputSink = { get<AccessibilityTextInputSink>() },\n"
    patch_once(
        shell / JAVA_ROOT / "di/RunnerModule.kt",
        anchor,
        anchor
        + "            membershipEnv = { com.aliothmoon.maafw.membership.AndroidIdentity.appEnv(context) },\n",
    )
    anchor = (
        '        Ln.i("ExecAgentHost: launching $command (cwd=${request.workingDir})")'
    )
    injection = """        if (entry.env["MDA_ANDROID_MEMBERSHIP_ENABLED"] == "1") {
            builder.environment()["MDA_ANDROID_IDENTITY_JSON"] =
                com.aliothmoon.maafw.membership.AndroidIdentity.privilegedJson(request.piEnv["MDA_ANDROID_IDENTITY_APP_JSON"])
        }
"""
    patch_once(
        shell / JAVA_ROOT / "remote/ExecAgentHost.kt", anchor, injection + anchor
    )
