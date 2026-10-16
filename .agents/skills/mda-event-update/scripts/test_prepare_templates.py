import json
from pathlib import Path
import tempfile
import unittest

import cv2
import numpy as np

from prepare_templates import prepare


class TemplatePreparationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="mda-event-templates-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.image = np.random.default_rng(12).integers(0, 255, (1080, 1920, 3), dtype=np.uint8)
        self.save("screen.png", self.image)

    def save(self, name, image):
        path = self.root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(cv2.imencode(".png", image)[1].tobytes())
        return path

    def run_plan(self, assets, folder="prepared", repo=None, **options):
        spec = {"event": "SmallEvent", "theme": "TestTheme", "assets": assets, **options}
        manifest = self.root / "plan.json"
        manifest.write_text(json.dumps(spec), encoding="utf-8")
        return prepare(manifest, self.root / folder, repo)

    def asset(self, **changes):
        return {"source": "screen.png", "kind": "screenshot", "role": "stage", "slot": "StageHard",
                "box": [600, 300, 60, 30], **changes}

    def result_image(self, report, folder="prepared", index=0):
        path = self.root / folder / report["assets"][index]["path"]
        return cv2.imdecode(np.fromfile(path, dtype=np.uint8), cv2.IMREAD_COLOR)

    def test_full_screen_pair_scales_and_names(self):
        report = self.run_plan([self.asset(), self.asset(role="repeatable", box=[900, 450, 30, 30])])
        a, b = report["assets"]
        self.assertEqual(a["pipeline_size"], [1280, 720])
        self.assertEqual(a["box"], [400, 200, 40, 20])
        self.assertEqual(a["size"], [40, 20])
        self.assertTrue(a["path"].endswith("TestThemeStageHard.png"))
        self.assertTrue(b["path"].endswith("TestThemeStageHardRepeatable.png"))
        expected = cv2.resize(self.image, (1280, 720), interpolation=cv2.INTER_AREA)[200:220, 400:440]
        np.testing.assert_array_equal(self.result_image(report), expected)

    def test_screen_and_already_cropped_inputs_converge(self):
        screenshot = self.run_plan([self.asset()])
        pixels = self.result_image(screenshot)
        self.save("cropped.png", pixels)
        cropped = self.run_plan([
            {"source": "cropped.png", "kind": "crop", "role": "stage", "slot": "StageHard"}
        ], folder="crop-result")
        self.assertEqual(screenshot["assets"][0]["path"], cropped["assets"][0]["path"])
        self.assertEqual(screenshot["assets"][0]["sha256"], cropped["assets"][0]["sha256"])

    def test_pipeline_coordinates(self):
        report = self.run_plan([self.asset(space="pipeline", box=[400, 200, 40, 20])])
        self.assertEqual(report["assets"][0]["size"], [40, 20])

    def test_mixed_screenshot_and_crop_pair(self):
        self.save("crop.png", self.image[:23, :24])
        report = self.run_plan([self.asset(), {"source": "crop.png", "kind": "crop",
                                "role": "repeatable", "slot": "StageHard"}])
        self.assertEqual([a["kind"] for a in report["assets"]], ["screenshot", "crop"])
        self.assertEqual(report["assets"][1]["size"], [24, 23])

    def test_reserved_case_names_are_not_themes(self):
        for theme in ["CurrentEvent", "Other"]:
            with self.subTest(theme=theme), self.assertRaisesRegex(ValueError, "theme"):
                self.run_plan([self.asset()], theme=theme)

    def test_large_event_story_and_logo_naming(self):
        report = self.run_plan([self.asset(slot="Story2StageHard"), self.asset(role="logo")], event="LargeEvent")
        self.assertTrue(report["assets"][0]["path"].endswith("TestThemeStory2StageHard.png"))
        self.assertTrue(report["assets"][1]["path"].endswith("TestThemeLogo.png"))

    def test_wide_screenshot_preserves_aspect_ratio(self):
        self.save("wide.png", self.image[:1000])
        report = self.run_plan([self.asset(source="wide.png")])
        self.assertEqual(report["assets"][0]["pipeline_size"], [1382, 720])

    def test_portrait_uses_short_side(self):
        self.save("portrait.png", self.image.transpose(1, 0, 2))
        report = self.run_plan([self.asset(source="portrait.png")])
        self.assertEqual(report["assets"][0]["pipeline_size"], [720, 1280])

    def test_frame_removes_app_chrome_before_scaling(self):
        frame = np.full((1200, 2100, 3), 5, dtype=np.uint8)
        frame[60:1140, 90:2010] = self.image
        self.save("window.png", frame)
        report = self.run_plan([self.asset(source="window.png", frame=[90, 60, 1920, 1080], box=[690, 360, 60, 30])])
        self.assertEqual(report["assets"][0]["box"], [400, 200, 40, 20])

    def test_crop_scale_uses_original_frame_not_crop_size(self):
        pixels = self.image[300:330, 600:660]
        self.save("crop.png", pixels)
        report = self.run_plan([{"source": "crop.png", "kind": "crop", "source_frame_size": [1920, 1080],
                                 "role": "repeatable", "slot": "StageHard"}])
        self.assertEqual(report["assets"][0]["size"], [40, 20])
        self.assertTrue(report["assets"][0]["scale_verified"])

    def test_ambiguous_crop_retains_pixels_and_records_unknown_scale(self):
        self.save("crop.png", self.image[:19, :37])
        report = self.run_plan([{"source": "crop.png", "kind": "crop", "role": "stage", "slot": "StageHard"}])
        self.assertEqual(report["assets"][0]["size"], [37, 19])
        self.assertFalse(report["assets"][0]["scale_verified"])

    def test_invalid_second_crop_writes_nothing(self):
        with self.assertRaisesRegex(ValueError, "越界"):
            self.run_plan([self.asset(), self.asset(role="repeatable", box=[1900, 1000, 60, 30])])
        self.assertFalse((self.root / "prepared").exists())

    def test_duplicate_output_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "重复生成"):
            self.run_plan([self.asset(), self.asset()])

    def test_install_conflict_does_not_partially_write(self):
        repo = self.root / "repo"
        (repo / "assets").mkdir(parents=True)
        (repo / "assets/interface.json").write_text(json.dumps({"resource": [{"path": ["./resource"]}]}))
        directory = repo / "assets/resource/image/SmallEvent/TestTheme"
        directory.mkdir(parents=True)
        conflict = directory / "TestThemeStageHardRepeatable.png"
        conflict.write_bytes(b"original")
        with self.assertRaisesRegex(ValueError, "模板已存在"):
            self.run_plan([self.asset(), self.asset(role="repeatable")], repo=repo)
        self.assertEqual(conflict.read_bytes(), b"original")
        self.assertFalse((directory / "TestThemeStageHard.png").exists())

    def test_install_only_copies_named_templates_and_is_idempotent(self):
        repo = self.root / "repo"
        (repo / "assets").mkdir(parents=True)
        (repo / "assets/interface.json").write_text(json.dumps({"resource": [{"path": ["./resource"]}]}))
        report = self.run_plan([self.asset()], repo=repo)
        self.run_plan([self.asset()], repo=repo)
        self.assertEqual((repo / report["assets"][0]["path"]).read_bytes(),
                         (self.root / "prepared" / report["assets"][0]["path"]).read_bytes())
        self.assertEqual(list((repo / "assets/resource").rglob("report.json")), [])


if __name__ == "__main__":
    unittest.main()
