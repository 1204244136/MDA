"""Android 测试资源的任务覆盖、按键提示及桌面行为保留回归。"""

import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

import jsonc

ROOT = Path(__file__).resolve().parents[2]
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
        BUILDER.BUILD = Path(cls.temp.name)
        cls.assets = BUILDER.prepare_assets()

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
        extra = read_json(self.assets / "tasks/AndroidTest.json")
        self.assertEqual(
            {task["name"] for task in extra["task"]},
            {"AndroidOpenGame", "AndroidAgentCheck"},
        )
        self.assertNotIn("github", prepared)
        self.assertNotIn("mirrorchyan_rid", prepared)

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

    def test_both_languages_warn_at_task_option_theme_and_preset(self):
        event = read_json(self.assets / "tasks/LargeEvent.json")
        for language in ("zh_cn", "en_us"):
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
