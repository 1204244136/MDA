"""繁体派生资源的发布生成、内容保留及 Android 提示回归。"""

import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from tools import build_android
from tools.generate_zh_tw import generate_zh_tw

ROOT = Path(__file__).resolve().parents[2]


def write_json(path: Path, value: object) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(value, ensure_ascii=False, indent=4) + "\n", encoding="utf-8"
    )


def read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def snapshot(directory: Path) -> dict[str, bytes]:
    return {
        path.relative_to(directory).as_posix(): path.read_bytes()
        for path in directory.rglob("*")
        if path.is_file()
    }


class GenerateZhTwTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.source = Path(self.temp.name) / "source"
        self.assets = Path(self.temp.name) / "staging"
        self.documents = {
            "description.file": "locales/interface/DESCRIPTION/DESCRIPTION.zh_cn.md",
            "contact.file": "locales/CONTACT.zh_cn.md",
            "welcome.file": "locales/WELCOME.zh_cn.md",
        }
        self.locale = {
            **self.documents,
            "task.设置.label": "请关闭游戏",
            "task.CustomBurst.label": "自定义爆裂",
            "task.CustomBurst.description": "请先设置键盘。",
            "task.LargeEvent.description": "请先关闭游戏。",
            "option.LargeEventContent.LargeEventMiniGame": "大活动小游戏",
            "option.LargeEventTheme.CurrentEvent.description": "当前活动",
            "option.LargeEventTheme.CoinRushShowdown": "COIN RUSH SHOWDOWN",
            "option.LargeEventTheme.PersonaOnFrontline": "PERSONA ON FRONTLINE",
            "preset.DailyFull.description": "每日任务",
            "preset.SelfUse.description": "自用任务",
            "option.ArenaShopItemList.ArenaShopFireCodeManual": "H.S.T.A.代码手册",
            "format.code": "检查程序代码",
            "format.example": "请重试 123 次：{count} {name} %s %d A/S/D W/A/D",
            "link.example": (
                "请访问 https://example.com/设置?q=游戏#说明，"
                "或[查看说明](docs/使用说明.md) "
                "![设置截图](images/设置.png)。"
            ),
        }
        write_json(
            self.source / "interface.json",
            {
                "interface_version": 2,
                "name": "MDA",
                "description": "$description.file",
                "contact": "$contact.file",
                "welcome": "$welcome.file",
                "languages": {
                    "zh_cn": "locales/interface/zh_cn.json",
                    "en_us": "locales/interface/en_us.json",
                },
                "import": ["tasks/LargeEvent.json"],
            },
        )
        write_json(self.source / "locales/interface/zh_cn.json", self.locale)
        write_json(
            self.source / "locales/interface/en_us.json",
            {key: f"English: {key}" for key in self.locale},
        )
        write_json(
            self.source / "tasks/LargeEvent.json",
            {
                "option": {
                    "LargeEventMiniGame": {"description": "原始说明"},
                    "LargeEventTheme": {
                        "cases": [
                            {"name": "CurrentEvent"},
                            {"name": "CoinRushShowdown"},
                            {"name": "PersonaOnFrontline"},
                        ]
                    },
                }
            },
        )
        for relative in self.documents.values():
            path = self.source / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(
                "# 请关闭游戏\n\n"
                "请按 A/S/D，等待 123 秒后重试 {count}。\n\n"
                "[使用说明](docs/使用说明.md)\n"
                "![设置截图](images/设置.png)\n"
                "https://example.com/设置?q=游戏#说明\n\n"
                "请保留变量 {任务名} 和路径 `assets/设置.json`。\n\n"
                "```json\n{\"设置\": \"游戏\"}\n```\n\n"
                "[手册]: docs/使用说明.md\n\n"
                '<a href="docs/使用说明.md" title="设置">请阅读</a>\n',
                encoding="utf-8",
            )
        self.original_source = snapshot(self.source)
        shutil.copytree(self.source, self.assets)

    def test_generates_registered_documents_without_changing_source_languages(self):
        staged_before = snapshot(self.assets)

        generate_zh_tw(self.assets)

        self.assertEqual(snapshot(self.source), self.original_source)
        for relative, content in staged_before.items():
            if relative != "interface.json":
                self.assertEqual((self.assets / relative).read_bytes(), content, relative)
        interface = read_json(self.assets / "interface.json")
        self.assertEqual(
            interface["languages"]["zh_tw"], "locales/interface/zh_tw.json"
        )
        original_interface = read_json(self.source / "interface.json")
        del interface["languages"]["zh_tw"]
        self.assertEqual(interface, original_interface)
        traditional = read_json(self.assets / "locales/interface/zh_tw.json")
        for key, original_path in self.documents.items():
            with self.subTest(key=key):
                expected_path = original_path.replace(".zh_cn.md", ".zh_tw.md")
                self.assertEqual(traditional[key], expected_path)
                self.assertIn(
                    "# 請關閉遊戲", (self.assets / expected_path).read_text("utf-8")
                )

    def test_preserves_keys_order_placeholders_and_link_targets(self):
        generate_zh_tw(self.assets)

        traditional = read_json(self.assets / "locales/interface/zh_tw.json")
        self.assertEqual(list(traditional), list(self.locale))
        self.assertEqual(traditional["task.设置.label"], "請關閉遊戲")
        self.assertEqual(
            traditional["option.ArenaShopItemList.ArenaShopFireCodeManual"],
            "H.S.T.A.代碼手冊",
        )
        self.assertIn("程式碼", traditional["format.code"])
        for token in ("123", "{count}", "{name}", "%s", "%d", "A/S/D", "W/A/D"):
            self.assertIn(token, traditional["format.example"])
        link_targets = (
            "https://example.com/设置?q=游戏#说明",
            "(docs/使用说明.md)",
            "(images/设置.png)",
        )
        generated_contents = [traditional["link.example"]]
        generated_contents.extend(
            (self.assets / traditional[key]).read_text("utf-8")
            for key in self.documents
        )
        for content in generated_contents:
            for target in link_targets:
                with self.subTest(target=target, content=content):
                    self.assertIn(target, content)
        for content in generated_contents[1:]:
            for literal in (
                "{任务名}",
                "`assets/设置.json`",
                '```json\n{"设置": "游戏"}\n```',
                "]: docs/使用说明.md",
                '<a href="docs/使用说明.md" title="设置">',
            ):
                with self.subTest(literal=literal, content=content):
                    self.assertIn(literal, content)

    def test_android_annotations_are_included_in_generated_traditional_locale(self):
        build_android.annotate_keyboard_features(self.assets)
        simplified = read_json(self.assets / "locales/interface/zh_cn.json")
        annotated_before = snapshot(self.assets)

        generate_zh_tw(self.assets)

        traditional = read_json(self.assets / "locales/interface/zh_tw.json")
        self.assertEqual(list(traditional), list(simplified))
        self.assertIn("實體按鍵，手機勿用", traditional["task.CustomBurst.label"])
        self.assertIn("實體鍵盤 A/S/D", traditional["task.CustomBurst.description"])
        self.assertIn("W/A/D", traditional["task.LargeEvent.description"])
        warning_keys = (
            "option.LargeEventMiniGame.android_warning",
            "option.LargeEventTheme.CoinRushShowdown.android_warning",
            "option.LargeEventTheme.PersonaOnFrontline.android_warning",
        )
        for key in warning_keys:
            with self.subTest(key=key):
                self.assertIn("請", traditional[key])
                self.assertNotIn("请", traditional[key])
        self.assertIn("COIN RUSH SHOWDOWN", traditional[warning_keys[0]])
        self.assertIn("PERSONA ON FRONTLINE", traditional["preset.DailyFull.description"])
        for relative, content in annotated_before.items():
            if relative != "interface.json":
                self.assertEqual((self.assets / relative).read_bytes(), content, relative)

    def test_repeated_generation_is_byte_for_byte_idempotent(self):
        generate_zh_tw(self.assets)
        first = snapshot(self.assets)

        generate_zh_tw(self.assets)

        self.assertEqual(snapshot(self.assets), first)

    def test_android_ci_help_does_not_require_site_packages(self):
        result = subprocess.run(
            [sys.executable, "-S", str(ROOT / "tools/android_ci.py"), "--help"],
            capture_output=True,
            check=False,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(b"meta", result.stdout)
        self.assertIn(b"agent", result.stdout)

    def test_rejects_repository_source_assets_and_its_descendants(self):
        source_assets = ROOT / "assets"
        source_files = (
            source_assets / "interface.json",
            source_assets / "locales/interface/zh_cn.json",
            source_assets / "locales/interface/en_us.json",
        )
        before = {path: path.read_bytes() for path in source_files}

        for target in (source_assets, source_assets / "locales"):
            with self.subTest(target=target):
                with self.assertRaises(ValueError):
                    generate_zh_tw(target)

        self.assertEqual({path: path.read_bytes() for path in source_files}, before)

    def test_rejects_input_symlink_outside_staging_without_writing_source(self):
        input_path = self.assets / "locales/interface/zh_cn.json"
        input_path.unlink()
        self.create_symlink(
            input_path, self.source / "locales/interface/zh_cn.json"
        )

        with self.assertRaises(ValueError):
            generate_zh_tw(self.assets)

        self.assertEqual(snapshot(self.source), self.original_source)

    def test_rejects_output_symlink_outside_staging_without_writing_source(self):
        self.create_symlink(
            self.assets / "locales/interface/zh_tw.json",
            self.source / "locales/interface/zh_cn.json",
        )

        with self.assertRaises(ValueError):
            generate_zh_tw(self.assets)

        self.assertEqual(snapshot(self.source), self.original_source)

    def create_symlink(self, link: Path, target: Path) -> None:
        try:
            link.symlink_to(target)
        except (NotImplementedError, OSError) as exc:
            self.skipTest(f"当前环境不能创建测试用符号链接：{exc}")


if __name__ == "__main__":
    unittest.main()
