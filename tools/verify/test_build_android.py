"""Android 测试/正式资源的任务覆盖、按键提示及桌面行为保留回归。"""

import importlib.util
import json
import sys
import tempfile
import unittest
from pathlib import Path

import jsonc

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools"))
SPEC = importlib.util.spec_from_file_location(
    "build_android", ROOT / "tools/build_android.py"
)
BUILDER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(BUILDER)


def read_json(path: Path):
    return json.loads(path.read_text(encoding="utf-8"))


class AndroidAssetsTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.previous_build = BUILDER.BUILD
        BUILDER.BUILD = Path(cls.temp.name) / "test"
        cls.assets = BUILDER.prepare_assets("2.5.0-android-test.2", False)
        BUILDER.BUILD = Path(cls.temp.name) / "release"
        cls.release_assets = BUILDER.prepare_assets("1.12.3", True)

    @classmethod
    def tearDownClass(cls):
        BUILDER.BUILD = cls.previous_build
        cls.temp.cleanup()

    def test_all_source_tasks_and_presets_are_imported_in_order(self):
        original = read_json(ROOT / "assets/interface.json")
        prepared = read_json(self.assets / "interface.json")
        self.assertEqual(prepared["import"][1:], original["import"])
        for name in original["import"]:
            source = read_json(ROOT / "assets" / name)
            staged = read_json(self.assets / name)
            self.assertEqual(staged.get("task"), source.get("task"), name)
            self.assertEqual(staged.get("preset"), source.get("preset"), name)
        extra = read_json(self.assets / "tasks/Android.json")
        self.assertEqual(
            {task["name"] for task in extra["task"]},
            {"AndroidOpenGame", "AndroidAgentCheck"},
        )

    def test_test_build_has_no_update_source(self):
        prepared = read_json(self.assets / "interface.json")
        self.assertEqual(prepared["name"], "MDA Android 测试")
        self.assertEqual(prepared["welcome"], "AndroidTestNotice.md")
        self.assertEqual(prepared["version"], "2.5.0-android-test.2")
        self.assertNotIn("github", prepared)
        self.assertNotIn("mirrorchyan_rid", prepared)
        self.assertNotIn("mirrorchyan_multiplatform", prepared)
        self.assertTrue((self.assets / "AndroidTestNotice.md").is_file())

    def test_release_build_keeps_release_identity_and_update_sources(self):
        prepared = read_json(self.release_assets / "interface.json")
        self.assertEqual(prepared["name"], "MDA")
        self.assertEqual(prepared["welcome"], "AndroidNotice.md")
        self.assertEqual(prepared["version"], "1.12.3")
        self.assertEqual(prepared["github"], "https://github.com/1204244136/MDA")
        self.assertEqual(prepared["mirrorchyan_rid"], "MDA")
        self.assertNotIn("mirrorchyan_multiplatform", prepared)
        self.assertTrue((self.release_assets / "AndroidNotice.md").is_file())

    def test_release_profile_matches_ci_versions(self):
        profile = (ROOT / "Android/release.profile.yaml").read_text(encoding="utf-8")
        self.assertIn(f"id: {BUILDER.RELEASE['app_id']}", profile)
        self.assertIn(f"abi: [{BUILDER.RELEASE['abi']}]", profile)
        self.assertIn("mirrorchyanRid: MDA", profile)
        self.assertIn("MDA_ANDROID_MEMBERSHIP_ENABLED", profile)

    def test_release_version_rejects_build_metadata(self):
        self.assertIsNone(BUILDER.RELEASE_VERSION.fullmatch("1.12.3+build"))
        self.assertIsNotNone(BUILDER.RELEASE_VERSION.fullmatch("1.12.3-beta.1"))
        self.assertIsNotNone(
            BUILDER.MEMBER_VERSION.fullmatch("2.5.0-android.ci.1.1+aaaaaaaa")
        )

    def test_keyboard_actions_are_covered_and_not_adapted(self):
        found = set()
        for path in (ROOT / "assets/resource/pipeline").rglob("*.json"):
            source = path.read_bytes()
            staged = (
                self.assets
                / "resource/pipeline"
                / path.relative_to(ROOT / "assets/resource/pipeline")
            )
            self.assertEqual(staged.read_bytes(), source, str(path))
            nodes = jsonc.loads(source.decode("utf-8"))
            if any(
                isinstance(node, dict)
                and isinstance(node.get("action"), dict)
                and node["action"].get("type")
                in {"ClickKey", "LongPressKey", "KeyDown", "KeyUp"}
                for node in nodes.values()
            ):
                found.add(
                    path.relative_to(ROOT / "assets/resource/pipeline").as_posix()
                )
        self.assertEqual(found, set(BUILDER.KEYBOARD_PIPELINES))

        original = read_json(ROOT / "assets/tasks/LargeEvent.json")
        staged = read_json(self.assets / "tasks/LargeEvent.json")
        for name, option in original["option"].items():
            current = staged["option"][name]
            self.assertEqual(current.get("default_case"), option.get("default_case"))
            for case, source_case in zip(
                current.get("cases", []), option.get("cases", []), strict=True
            ):
                self.assertEqual(
                    case.get("pipeline_override"), source_case.get("pipeline_override")
                )

    def test_all_languages_warn_at_task_option_theme_and_preset(self):
        event = read_json(self.assets / "tasks/LargeEvent.json")
        for language in ("zh_cn", "en_us", "zh_tw"):
            locale = read_json(self.assets / f"locales/interface/{language}.json")
            self.assertIn("A/S/D", locale["task.CustomBurst.description"])
            self.assertIn("COIN RUSH SHOWDOWN", locale["task.LargeEvent.description"])
            self.assertIn("W/A/D", locale["task.LargeEvent.description"])
            description = event["option"]["LargeEventMiniGame"]["description"]
            self.assertIn("COIN RUSH SHOWDOWN", locale[description[1:]])
            self.assertIn(
                "PERSONA ON FRONTLINE", locale["preset.DailyFull.description"]
            )
            self.assertIn("PERSONA ON FRONTLINE", locale["preset.SelfUse.description"])
            for case in event["option"]["LargeEventTheme"]["cases"]:
                if case["name"] in {"CoinRushShowdown", "PersonaOnFrontline"}:
                    self.assertTrue(locale[case["description"][1:]])


if __name__ == "__main__":
    unittest.main()
