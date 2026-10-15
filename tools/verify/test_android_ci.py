"""防止 CI 会员包意外进入 dev、版本倒退、签名漂移或混入旧 Agent。"""

import io
import json
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import android_ci as ci
import build_android as builder


class AndroidCITest(unittest.TestCase):
    def meta(self, run=1, attempt=1, tag="v2.5.0"):
        with patch.object(ci.subprocess, "check_output", side_effect=[tag, "a" * 40]):
            return ci.metadata(run, attempt)

    def test_version_codes_increase_on_rerun_and_next_run(self):
        first = self.meta()
        retry = self.meta(attempt=99)
        next_run = self.meta(run=2)
        self.assertGreater(first["version_code"], 434)
        self.assertLess(first["version_code"], retry["version_code"])
        self.assertLess(retry["version_code"], next_run["version_code"])
        self.assertEqual(first["version"], "2.5.0-android.ci.1.1+aaaaaaaa")
        for run, attempt in [(0, 1), (1, 100), (21_000_000, 1)]:
            with self.assertRaises(ValueError):
                self.meta(run, attempt)

    def test_membership_version_never_falls_back_to_dev_or_hash(self):
        for tag in ["v0.1.0", "dev", "a" * 8, "v2.5.0\nunsafe"]:
            with self.assertRaises(ValueError):
                self.meta(tag=tag)

    def test_explicit_code_does_not_read_local_counter_or_shell_history(self):
        with (
            tempfile.TemporaryDirectory() as directory,
            patch.object(builder, "BUILD", Path(directory)),
            patch.object(builder.subprocess, "check_output") as git,
        ):
            number, init = builder.prepare_build_number(Path(directory), 100101)
            self.assertEqual(number, 100101)
            self.assertIn("versionCode = 100101", init.read_text())
            git.assert_not_called()
            with self.assertRaises(ValueError):
                builder.prepare_build_number(Path(directory), 0)

    def test_signing_is_required_before_running_tools(self):
        with (
            patch.dict(ci.os.environ, {}, clear=True),
            patch.object(ci.subprocess, "run") as run,
        ):
            with self.assertRaises(ValueError):
                ci.sign_apk(Path("missing.apk"), Path("tools"))
            run.assert_not_called()

    def make_apk(
        self, path, *, version="2.5.0-ci.1", enabled="1", probe=False, ocr=True
    ):
        pi = io.BytesIO()
        with zipfile.ZipFile(pi, "w") as archive:
            archive.writestr("interface.json", json.dumps({"version": version}))
            if ocr:
                archive.writestr("resource/model/ocr/rec.onnx", b"fixture")
        with zipfile.ZipFile(path, "w") as archive:
            for lib in [
                "libgo-service.so",
                "libMaaFramework.so",
                "libMaaAgentServer.so",
            ]:
                archive.writestr("lib/arm64-v8a/" + lib, b"fixture 2.5.0-ci.1")
            env = {"MDA_ANDROID_MEMBERSHIP_ENABLED": enabled}
            if probe:
                env["MDA_IDENTITY_PROBE_ENABLED"] = "1"
            archive.writestr(
                "assets/agent/agent-runtime.json",
                json.dumps({"runtimes": [{"env": env}]}),
            )
            archive.writestr("assets/pi.zip", pi.getvalue())

    def test_apk_rejects_old_agent_wrong_pi_missing_ocr_and_diagnostic_mode(self):
        info = "GOOS=android GOARCH=arm64"
        with tempfile.TemporaryDirectory() as directory:
            apk = Path(directory) / "test.apk"
            agent = Path(directory) / "agent.so"
            agent.write_bytes(b"unused")
            self.make_apk(apk)
            with patch.object(
                ci.subprocess, "check_output", side_effect=["same", "same", info]
            ):
                ci.inspect_contents(apk, "2.5.0-ci.1", agent)
            with (
                patch.object(
                    ci.subprocess,
                    "check_output",
                    side_effect=["current", "stale", info],
                ),
                self.assertRaisesRegex(ValueError, "Agent"),
            ):
                ci.inspect_contents(apk, "2.5.0-ci.1", agent)
            for changes in [
                {"version": "old"},
                {"ocr": False},
                {"probe": True},
                {"enabled": "0"},
            ]:
                self.make_apk(apk, **changes)
                with (
                    patch.object(
                        ci.subprocess,
                        "check_output",
                        side_effect=["same", "same", info],
                    ),
                    self.assertRaises(ValueError),
                ):
                    ci.inspect_contents(apk, "2.5.0-ci.1", agent)

    def test_manual_build_rejects_wrong_certificate(self):
        with (
            patch.object(ci.subprocess, "run"),
            patch.object(
                ci.subprocess,
                "check_output",
                side_effect=[
                    "package: name='com.aliothmoon.maafw.mda.membershiptest' versionCode='1' versionName='2.5.0'",
                    "Verified using v2 scheme (APK Signature Scheme v2): true\nSigner #1 certificate SHA-256 digest: "
                    + "a" * 64,
                ],
            ),
            patch.dict(ci.os.environ, {"ANDROID_TEST_CERT_SHA256": "b" * 64}),
            self.assertRaisesRegex(ValueError, "证书"),
        ):
            ci.verify_apk(
                Path("fixture.apk"),
                Path("tools"),
                {"version": "2.5.0", "version_code": 1},
                False,
            )

    def test_signing_and_verification_explicitly_cover_v2(self):
        with tempfile.TemporaryDirectory() as directory:
            apk = Path(directory) / "input.apk"
            apk.write_bytes(b"unsigned fixture")
            env = {
                "ANDROID_TEST_KEYSTORE_BASE64": "Zml4dHVyZQ==",
                "ANDROID_TEST_STORE_PASSWORD": "fixture",
                "ANDROID_TEST_KEY_ALIAS": "fixture",
                "ANDROID_TEST_KEY_PASSWORD": "fixture",
                "ANDROID_TEST_CERT_SHA256": "a" * 64,
            }

            def sign(command, **kwargs):
                self.assertEqual(
                    command[command.index("--v2-signing-enabled") + 1], "true"
                )
                self.assertEqual(
                    command[command.index("--v3-signing-enabled") + 1], "true"
                )
                Path(command[command.index("--out") + 1]).write_bytes(b"signed fixture")

            with (
                patch.dict(ci.os.environ, env),
                patch.object(ci.subprocess, "run", side_effect=sign),
            ):
                ci.sign_apk(apk, Path("tools"))
            self.assertEqual(apk.read_bytes(), b"signed fixture")

            def output(command, **kwargs):
                if "badging" in command:
                    return "package: name='com.aliothmoon.maafw.mda.membershiptest' versionCode='1' versionName='2.5.0'"
                self.assertEqual(command[command.index("--min-sdk-version") + 1], "24")
                return (
                    "Verified using v2 scheme (APK Signature Scheme v2): true\nSigner #1 certificate SHA-256 digest: "
                    + "b" * 64
                )

            with (
                patch.dict(ci.os.environ, env),
                patch.object(ci.subprocess, "run"),
                patch.object(ci.subprocess, "check_output", side_effect=output),
                self.assertRaisesRegex(ValueError, "证书"),
            ):
                ci.verify_apk(
                    apk, Path("tools"), {"version": "2.5.0", "version_code": 1}, False
                )


if __name__ == "__main__":
    unittest.main()
