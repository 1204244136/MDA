"""固定外壳的会员桥接补丁回归。"""

import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import android_shell as shell_patch


class AndroidShellTest(unittest.TestCase):
    def test_overlay_is_repeatable_and_only_injects_membership(self):
        with tempfile.TemporaryDirectory() as directory:
            shell = Path(directory)
            java = shell / shell_patch.JAVA_ROOT
            sources = {
                "runner/MaaFrameworkRunnerPort.kt": "    private val textInputSink: () -> ITextInputSink,\n            piEnv = plan.piEnv,\n",
                "di/RunnerModule.kt": "            textInputSink = { get<AccessibilityTextInputSink>() },\n",
                "remote/ExecAgentHost.kt": '        Ln.i("ExecAgentHost: launching $command (cwd=${request.workingDir})")\n',
                "remote/MaaRunner.kt": "        val reusable = loadedAgents == payload.agents &&\n",
            }
            for name, text in sources.items():
                path = java / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(text, encoding="utf-8")
            with patch.object(
                shell_patch.subprocess,
                "check_output",
                return_value=shell_patch.SHELL_REVISION,
            ):
                shell_patch.apply_overlay(shell)
                shell_patch.apply_overlay(shell)
            host = (java / "remote/ExecAgentHost.kt").read_text(encoding="utf-8")
            runner = (java / "remote/MaaRunner.kt").read_text(encoding="utf-8")
            self.assertEqual(
                host.count('entry.env["MDA_ANDROID_MEMBERSHIP_ENABLED"]'), 1
            )
            self.assertEqual(runner.count("AndroidIdentityCompat.refreshRequired"), 1)
            self.assertNotIn("MDA_IDENTITY_PROBE", host)
            self.assertEqual(
                (java / "membership/AndroidIdentity.kt").read_bytes(),
                (
                    shell_patch.ROOT / "Android/shell-overlay/AndroidIdentity.kt"
                ).read_bytes(),
            )

    def test_unknown_revision_or_anchor_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            shell = Path(directory)
            with (
                patch.object(
                    shell_patch.subprocess, "check_output", return_value="unknown"
                ),
                self.assertRaises(RuntimeError),
            ):
                shell_patch.apply_overlay(shell)
            path = shell / "source.kt"
            path.write_text("unknown", encoding="utf-8")
            with self.assertRaises(RuntimeError):
                shell_patch.patch_once(path, "missing", "new")
            self.assertEqual(path.read_text(), "unknown")

    def test_collector_does_not_keep_research_identifiers_or_write_raw_files(self):
        source = (
            shell_patch.ROOT / "Android/shell-overlay/AndroidIdentity.kt"
        ).read_text(encoding="utf-8")
        for forbidden in [
            "Telephony",
            "Wifi",
            "imei",
            "meid",
            "mac.",
            "sysfs",
            "writeText",
            "installationId",
        ]:
            self.assertNotIn(forbidden, source)
        for required in [
            "android_id.app",
            "android_id.privileged",
            "ro.serialno",
            "ro.boot.serialno",
            "Build.getSerial",
        ]:
            self.assertIn(required, source)


if __name__ == "__main__":
    unittest.main()
