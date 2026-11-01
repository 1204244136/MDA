"""防止 Android 测试包/正式包意外进入 dev、版本倒退、签名漂移或混入旧 Agent。"""

import base64
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

    def release_meta(self, tag="v1.12.3", sha="b" * 40, tagged=None, count="1200"):
        if tagged is None:
            tagged = sha
        with patch.object(
            ci.subprocess, "check_output", side_effect=[sha, tagged, count]
        ):
            return ci.release_metadata(tag)

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

    def test_release_metadata_uses_tag_and_commit_count(self):
        meta = self.release_meta()
        self.assertEqual(meta["version"], "1.12.3")
        self.assertEqual(meta["version_code"], 1200)
        self.assertEqual(meta["mode"], "release")
        self.assertEqual(meta["commit"], "b" * 40)
        prerelease = self.release_meta(tag="v1.12.3-beta.1")
        self.assertEqual(prerelease["version"], "1.12.3-beta.1")

    def test_release_metadata_rejects_bad_tag_and_version_code(self):
        for tag in [
            "1.12.3",
            "v0.1.0",
            "v1.12",
            "v1.12.3-",
            "v1.12.3+build",
            "dev",
            "",
        ]:
            with self.assertRaises(ValueError):
                self.release_meta(tag=tag)
        for count in ["0", "2100000001"]:
            with self.assertRaises(ValueError):
                self.release_meta(count=count)

    def test_release_metadata_rejects_tag_not_on_head(self):
        with self.assertRaisesRegex(ValueError, "不指向当前提交"):
            self.release_meta(tagged="c" * 40)

    def test_release_identity_is_separate_from_test_packages(self):
        self.assertEqual(ci.expected_package("test"), "com.aliothmoon.maafw.mda.membershiptest")
        self.assertEqual(ci.expected_package("pr"), "com.aliothmoon.maafw.mda.membershippr")
        self.assertEqual(ci.expected_package("release"), "com.aliothmoon.maafw.mda")
        self.assertEqual(ci.artifact_prefix("test"), "MDA-android-arm64-test")
        self.assertEqual(ci.artifact_prefix("pr"), "MDA-android-arm64-pr")
        self.assertEqual(ci.artifact_prefix("release"), "MDA-android-arm64-v8a")

    def test_github_output_only_contains_scalars(self):
        scalars = ci.scalar_metadata(self.release_meta())
        self.assertNotIn("release", scalars)
        self.assertNotIn("{}", str(scalars))
        self.assertEqual(scalars["assemble"], "release")
        self.assertEqual(scalars["package"], "com.aliothmoon.maafw.mda")
        self.assertEqual(scalars["artifact_prefix"], "MDA-android-arm64-v8a")

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

    def test_release_signing_requires_all_material(self):
        with (
            patch.dict(ci.os.environ, {}, clear=True),
            self.assertRaises(ValueError),
        ):
            ci.prepare_release_signing()

    def test_release_signing_reports_missing_cert_variable(self):
        env = {name: "fixture" for name in ci.RELEASE_SIGNING_SECRETS}
        with (
            patch.dict(ci.os.environ, env, clear=True),
            self.assertRaisesRegex(ValueError, ci.RELEASE_CERT_VARIABLE),
        ):
            ci.prepare_release_signing()

    def test_release_signing_writes_keystore_and_github_output(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "github-output.txt"
            env = {
                "ANDROID_KEYSTORE_BASE64": base64.b64encode(b"fixture").decode(),
                "ANDROID_STORE_PASSWORD": "fixture",
                "ANDROID_KEY_ALIAS": "fixture",
                "ANDROID_KEY_PASSWORD": "fixture",
                "ANDROID_CERT_SHA256": "a" * 64,
                "RUNNER_TEMP": directory,
                "GITHUB_OUTPUT": str(output),
            }
            with patch.dict(ci.os.environ, env, clear=True):
                key = ci.prepare_release_signing()
                self.assertEqual(key.read_bytes(), b"fixture")
                self.assertIn(f"path={key}", output.read_text(encoding="utf-8"))

    def make_apk(
        self,
        path,
        *,
        version="2.5.0-ci.1",
        enabled="1",
        probe=False,
        ocr=True,
        update=False,
    ):
        pi = io.BytesIO()
        interface = {"version": version}
        if update:
            interface.update(
                mirrorchyan_rid="MDA",
                github="https://github.com/1204244136/MDA",
            )
        with zipfile.ZipFile(pi, "w") as archive:
            archive.writestr("interface.json", json.dumps(interface))
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
                ci.inspect_contents(apk, "2.5.0-ci.1", agent, False)
            with (
                patch.object(
                    ci.subprocess,
                    "check_output",
                    side_effect=["current", "stale", info],
                ),
                self.assertRaisesRegex(ValueError, "Agent"),
            ):
                ci.inspect_contents(apk, "2.5.0-ci.1", agent, False)
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
                    ci.inspect_contents(apk, "2.5.0-ci.1", agent, False)

    def test_release_apk_requires_update_sources(self):
        info = "GOOS=android GOARCH=arm64"
        with tempfile.TemporaryDirectory() as directory:
            apk = Path(directory) / "release.apk"
            agent = Path(directory) / "agent.so"
            agent.write_bytes(b"unused")
            self.make_apk(apk, update=True)
            with patch.object(
                ci.subprocess, "check_output", side_effect=["same", "same", info]
            ):
                ci.inspect_contents(apk, "2.5.0-ci.1", agent, True)
            self.make_apk(apk, update=False)
            with (
                patch.object(
                    ci.subprocess,
                    "check_output",
                    side_effect=["same", "same", info],
                ),
                self.assertRaisesRegex(ValueError, "更新源"),
            ):
                ci.inspect_contents(apk, "2.5.0-ci.1", agent, True)

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
                "test",
            )

    def test_release_build_rejects_wrong_certificate(self):
        with (
            patch.object(ci.subprocess, "run"),
            patch.object(
                ci.subprocess,
                "check_output",
                side_effect=[
                    "package: name='com.aliothmoon.maafw.mda' versionCode='1200' versionName='1.12.3'",
                    "Verified using v2 scheme (APK Signature Scheme v2): true\nSigner #1 certificate SHA-256 digest: "
                    + "a" * 64,
                ],
            ),
            patch.dict(ci.os.environ, {"ANDROID_CERT_SHA256": "b" * 64}),
            self.assertRaisesRegex(ValueError, "证书"),
        ):
            ci.verify_apk(
                Path("fixture.apk"),
                Path("tools"),
                {"version": "1.12.3", "version_code": 1200},
                "release",
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
                    apk,
                    Path("tools"),
                    {"version": "2.5.0", "version_code": 1},
                    "test",
                )


if __name__ == "__main__":
    unittest.main()
