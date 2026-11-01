import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


class RegisterTest(unittest.TestCase):
    def test_registers_controller_methods_and_locales(self):
        script = Path(__file__).resolve().parents[1] / 'register.py'
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / 'locales').mkdir()
            interface = {
                'controller': [{'name': 'AAA', 'type': 'Win32',
                                'win32': {'mouse': 'Interception', 'keyboard': 'Seize'}}],
                'languages': {'zh_cn': 'locales/zh.json', 'en_us': 'locales/en.json'},
            }
            (root / 'interface.json').write_text(json.dumps(interface), encoding='utf-8')
            for name in ('zh', 'en'):
                (root / f'locales/{name}.json').write_text('{}', encoding='utf-8')
            subprocess.run([sys.executable, str(script), str(root)], check=True)
            result = json.loads((root / 'interface.json').read_text(encoding='utf-8'))
            self.assertEqual(result['controller'][0], interface['controller'][0])
            for entry, name in zip(result['controller'][1:], ('BBB', 'CCC'), strict=True):
                self.assertEqual(entry['name'], name)
                self.assertEqual(entry['win32']['mouse'], name)
                self.assertEqual(entry['win32']['keyboard'], name)
                for lang in ('zh', 'en'):
                    locale = json.loads((root / f'locales/{lang}.json').read_text(encoding='utf-8'))
                    self.assertEqual(locale[f'controller.{name}.label'], name)


if __name__ == '__main__':
    unittest.main()
