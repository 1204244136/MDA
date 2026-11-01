"""在 Windows 安装包中注册 Logitech 和 Razer 控制器。"""

import copy
import json
import sys
from pathlib import Path


def main():
    root = Path(sys.argv[1])
    interface_path = root / 'interface.json'
    interface = json.loads(interface_path.read_text(encoding='utf-8-sig'))
    controllers = interface['controller']
    if any(item['name'] in ('Logitech', 'Razer') for item in controllers):
        raise SystemExit('安装包已包含 Logitech 或 Razer 控制器')
    default = next(item for item in controllers if item['name'] == 'AAA')
    for name in ('Logitech', 'Razer'):
        item = copy.deepcopy(default)
        item['name'] = name
        item['label'] = f'$controller.{name}.label'
        item['description'] = f'$controller.{name}.description'
        item['win32']['mouse'] = name
        item['win32']['keyboard'] = name
        controllers.append(item)
    interface_path.write_text(json.dumps(interface, ensure_ascii=False, indent=4) + '\n', encoding='utf-8')
    for language, relative in interface['languages'].items():
        path = root / relative
        data = json.loads(path.read_text(encoding='utf-8-sig'))
        for name in ('Logitech', 'Razer'):
            data[f'controller.{name}.label'] = name
            data[f'controller.{name}.description'] = (
                '需要对应的 DLL，并保持目标窗口在前台'
                if language.lower().startswith('zh') else
                'Requires the corresponding DLL and a foreground target window.'
            )
        path.write_text(json.dumps(data, ensure_ascii=False, indent=4) + '\n', encoding='utf-8')


if __name__ == '__main__':
    main()
