"""在指定版本的独立 MaaFramework 与 MXU 检出中准备实验性输入。"""

import argparse
import json
import shutil
import subprocess
from pathlib import Path

FRAMEWORK_REVISION = "8963191215ca49d6b037802acd07d20a4213160a"
MXU_REVISION = "968f4315a6d43ddb03c5cee091b8314737081d03"
HERE = Path(__file__).resolve().parent


def replace(text, old, new):
    if text.count(old) != 1:
        raise RuntimeError(f"源码与预期不符：{old!r}")
    return text.replace(old, new)


def patch_manager(text):
    text = replace(text, '#include "Input/InterceptionInput.h"',
                   '#include "Input/InterceptionInput.h"\n#include "Input/ExperimentalInput.h"')
    text = replace(text, '    connected_ = true;\n    return true;',
                   '    const auto experimental = [](MaaWin32InputMethod method) {\n'
                   '        return method == MdaLogitechInputMethod || method == MdaRazerInputMethod;\n'
                   '    };\n'
                   '    if ((experimental(mouse_method_) && !mouse_) ||\n'
                   '        (experimental(keyboard_method_) && !keyboard_)) return false;\n\n'
                   '    connected_ = true;\n    return true;')
    return replace(text, '    case MaaWin32InputMethod_Interception:',
                   '    case MdaLogitechInputMethod:\n    case MdaRazerInputMethod: {\n'
                   '        auto input = std::make_shared<ExperimentalInput>(hwnd_,\n'
                   '            method == MdaLogitechInputMethod ? L"logitech_connector.dll" : L"razer_connector.dll");\n'
                   '        return input->initialize() ? input : nullptr;\n    }\n'
                   '    case MaaWin32InputMethod_Interception:')


def patch_framepool(text):
    return replace(text, '        cap_session_.IncludeSecondaryWindows(true);',
                   '        []<class T>(T& session) {\n'
                   '            if constexpr (requires { session.IncludeSecondaryWindows(true); }) {\n'
                   '                session.IncludeSecondaryWindows(true);\n'
                   '            }\n        }(cap_session_);')


def patch_types(text):
    text = replace(text, '  Interception: 1n << 9n,',
                   '  Interception: 1n << 9n,\n  Logitech: 1n << 11n,\n  Razer: 1n << 12n,')
    return replace(text, '  Interception: Win32InputMethod.Interception,',
                   '  Interception: Win32InputMethod.Interception,\n'
                   '  Logitech: Win32InputMethod.Logitech,\n  Razer: Win32InputMethod.Razer,')


def patch_rust(text):
    for name in ('mouse', 'keyboard'):
        old = f'maa_framework::common::Win32InputMethod::from_bits_truncate(*{name}_method)\n                        .bits(),'
        new = (f'maa_framework::common::Win32InputMethod::from_bits_truncate(*{name}_method)\n'
               f'                        .bits() | (*{name}_method & ((1u64 << 11) | (1u64 << 12))),')
        text = replace(text, old, new)
    guard = ('    let _input_probe_guard = super::experimental_input::PROBE_TASK_GATE.try_read()\n'
             '        .map_err(|_| "输入检测进行中，请稍后操作。".to_string())?;\n')
    for line in ('    info!("maa_create_instance called, instance_id: {}", instance_id);',
                 '    info!("destroy_instance_impl called, instance_id: {}", instance_id);'):
        text = replace(text, line, guard + line)
    text = replace(text,
                   '    selected_task_id: Option<&str>,\n) -> Result<i64, String> {\n    let mut instances = state.instances.lock()',
                   '    selected_task_id: Option<&str>,\n) -> Result<i64, String> {\n'
                   '    let _input_probe_guard = super::experimental_input::PROBE_TASK_GATE.try_read()\n'
                   '        .map_err(|_| "输入检测进行中，请稍后启动任务。".to_string())?;\n'
                   '    let mut instances = state.instances.lock()')
    return replace(text, '    tokio::task::spawn_blocking(move || {\n        // ControllerPool:',
                   '    let _input_probe_guard = super::experimental_input::PROBE_TASK_GATE.read().await;\n'
                   '    tokio::task::spawn_blocking(move || {\n        // ControllerPool:')


def patch_agent(text):
    return replace(text, '    info!("start_tasks_impl called");',
                   '    let _input_probe_guard = super::experimental_input::PROBE_TASK_GATE.read().await;\n'
                   '    info!("start_tasks_impl called");')


def patch_settings(text):
    text = ("import { ExperimentalInputSection } from './settings/ExperimentalInputSection';\n"
            "import { isTauri } from '@/utils/paths';\n" + text)
    text = replace(text, '  ArrowLeft,', '  FlaskConical,\n  ArrowLeft,')
    text = replace(text, "    items.push(\n      { id: 'debug'",
                   "    if (isTauri()) items.push({ id: 'experimental-input', icon: FlaskConical, labelKey: 'experimentalInput.title' });\n"
                   "    items.push(\n      { id: 'debug'")
    return replace(text, '            <DebugSection />',
                   '            {isTauri() && <ExperimentalInputSection />}\n            <DebugSection />')


def patch_connection_panel(text):
    return replace(text,
                   '                          await maaService.destroyInstance(instanceId).catch(() => {});\n'
                   '                          await maaService.createInstance(instanceId).catch(() => {});',
                   '                          try {\n'
                   '                            await maaService.destroyInstance(instanceId);\n'
                   '                            await maaService.createInstance(instanceId);\n'
                   '                          } catch (error) {\n'
                   '                            setDeviceError(error instanceof Error ? error.message : String(error));\n'
                   '                            return;\n'
                   '                          }')


def patch_loader(text):
    text = replace(text, '    filterControllersByPlatform(pi, backendOS);',
                   '    filterControllersByPlatform(pi, backendOS);\n'
                   "    const enabled = await invoke<boolean>('mda_experimental_status').catch(() => false);\n"
                   "    if (!enabled) pi.controller = pi.controller.filter((c) => c.name !== 'Logitech' && c.name !== 'Razer');")
    text = replace(text, "      filterControllersByPlatform(apiResult.interface, apiResult.backendOS ?? '');",
                   "      filterControllersByPlatform(apiResult.interface, apiResult.backendOS ?? '');\n"
                   "      apiResult.interface.controller = apiResult.interface.controller.filter((c) => c.name !== 'Logitech' && c.name !== 'Razer');")
    return replace(text, "    filterControllersByPlatform(pi, '');",
                   "    filterControllersByPlatform(pi, '');\n"
                   "    pi.controller = pi.controller.filter((c) => c.name !== 'Logitech' && c.name !== 'Razer');")


def verify(repo, revision):
    actual = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo, text=True).strip()
    if actual != revision:
        raise RuntimeError(f"{repo} 的 HEAD 是 {actual}，需要 {revision}")


def update(plan, repo, revision, relative, transform):
    original = subprocess.check_output(['git', 'show', f'{revision}:{relative}'], cwd=repo).decode('utf-8')
    updated = transform(original)
    path = repo / relative
    current = path.read_text(encoding='utf-8')
    if current not in (original, updated):
        raise RuntimeError(f"文件已有其他改动，请使用独立检出：{path}")
    plan.append((path, updated.encode('utf-8')))


def copy_new(plan, source, target):
    data = source.read_bytes()
    if target.exists() and target.read_bytes() != data:
        raise RuntimeError(f"目标文件已存在且不同：{target}")
    plan.append((target, data))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('framework', type=Path)
    parser.add_argument('mxu', type=Path)
    args = parser.parse_args()
    framework, mxu = args.framework.resolve(), args.mxu.resolve()
    verify(framework, FRAMEWORK_REVISION)
    verify(mxu, MXU_REVISION)
    plan = []
    update(plan, framework, FRAMEWORK_REVISION, 'source/MaaWin32ControlUnit/Manager/Win32ControlUnitMgr.cpp', patch_manager)
    update(plan, framework, FRAMEWORK_REVISION, 'source/MaaWin32ControlUnit/Screencap/FramePoolScreencap.cpp', patch_framepool)
    for name in ('ExperimentalInputApi.h', 'ExperimentalInputCore.h', 'ExperimentalInputLibrary.h', 'ExperimentalInput.h'):
        copy_new(plan, HERE / name, framework / 'source/MaaWin32ControlUnit/Input' / name)
    update(plan, mxu, MXU_REVISION, 'src/types/maa.ts', patch_types)
    update(plan, mxu, MXU_REVISION, 'src-tauri/src/commands/maa_core.rs', patch_rust)
    update(plan, mxu, MXU_REVISION, 'src-tauri/src/commands/maa_agent.rs', patch_agent)
    update(plan, mxu, MXU_REVISION, 'src/components/SettingsPage.tsx', patch_settings)
    update(plan, mxu, MXU_REVISION, 'src/components/ConnectionPanel.tsx', patch_connection_panel)
    update(plan, mxu, MXU_REVISION, 'src/services/interfaceLoader.ts', patch_loader)
    update(plan, mxu, MXU_REVISION, 'src-tauri/src/commands/mod.rs', lambda text: text + '\npub mod experimental_input;\n')
    update(plan, mxu, MXU_REVISION, 'src-tauri/src/lib.rs', lambda text: replace(
        text, '.invoke_handler(tauri::generate_handler![',
        '.invoke_handler(tauri::generate_handler![\n'
        '            commands::experimental_input::mda_experimental_status,\n'
        '            commands::experimental_input::mda_set_experimental,\n'
        '            commands::experimental_input::mda_probe_experimental,'))
    copy_new(plan, HERE / 'client/ExperimentalInputSection.tsx', mxu / 'src/components/settings/ExperimentalInputSection.tsx')
    copy_new(plan, HERE / 'client/experimental_input.rs', mxu / 'src-tauri/src/commands/experimental_input.rs')
    locales = json.loads((HERE / 'client/locales.json').read_text(encoding='utf-8'))
    for lang in ('zh-CN', 'zh-TW', 'en-US', 'ja-JP', 'ko-KR'):
        value = json.dumps(locales.get(lang, locales['en-US']), ensure_ascii=False, indent=2)
        update(plan, mxu, MXU_REVISION, f'src/i18n/locales/{lang}.ts',
               lambda text, value=value: replace(text, 'export default {', f'export default {{\n  experimentalInput: {value},'))
    # Validate every target before writing any file, including the generated overlays.
    for path, data in plan:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data)
    print('已准备实验性输入；请在独立检出中构建。')


if __name__ == '__main__':
    main()
