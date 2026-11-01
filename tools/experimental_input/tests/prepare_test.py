import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))

import prepare


class PrepareTest(unittest.TestCase):
    def test_sets_mxu_version_without_touching_dependencies(self):
        cargo = 'name = "mxu"\nversion = "0.1.0"\n\n[[package]]\nname = "other"\nversion = "0.1.0"\n'
        patched = prepare.patch_cargo_version(cargo)
        self.assertIn(f'name = "mxu"\nversion = "{prepare.MXU_VERSION}"', patched)
        self.assertIn('name = "other"\nversion = "0.1.0"', patched)
        self.assertIn(f'"version": "{prepare.MXU_VERSION}"', prepare.patch_json_version('{"version": "0.1.0"}'))

    def test_rejects_unexpected_version_source(self):
        with self.assertRaises(RuntimeError):
            prepare.patch_json_version('{"version": "2.8.1"}')

    def test_rejects_framework_input_method_collision(self):
        prepare.verify_input_methods('#define MaaWin32InputMethod_AnchoredTouch (1ULL << 10)')
        with self.assertRaises(RuntimeError):
            prepare.verify_input_methods('#define MaaWin32InputMethod_New (1ULL << 11)')


if __name__ == '__main__':
    unittest.main()
