"""
精确复刻 MaaFramework Configurator::merge_option_overrides 的语义，
验证 input 值在各类配置状态下的取值来源。

源码依据：source/MaaPiCli/Impl/Configurator.cpp:481-511
"""

import json

# 实际 interface.json 中的 input 定义（含 default）
INPUT_DEFS = [
    {"name": "latestLogo", "default": "SmallEvent/DemoTheme/GreatVillainUnionLogo.png"},
    {"name": "latestStage1", "default": "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png"},
    {"name": "latestStage2", "default": "SmallEvent/DemoTheme/GreatVillainUnionStory2StageHard.png"},
]

# 新版 interface.json：主题换成了 NEWTHEME，default 全部更新
INPUT_DEFS_NEW_VERSION = [
    {"name": "latestLogo", "default": "SmallEvent/NewTheme/NewThemeLogo.png"},
    {"name": "latestStage1", "default": "SmallEvent/NewTheme/NewThemeStory1Stage.png"},
    {"name": "latestStage2", "default": "SmallEvent/NewTheme/NewThemeStory2StageHard.png"},
]

OVERRIDE_TEMPLATE = {
    "SmallEventClickStage": {
        "recognition": {"param": {"template": ["{latestStage1}", "{latestStage2}"]}}
    }
}


def build_input_override(input_defs, config_inputs):
    """
    复刻 Configurator.cpp:481-511 的 Input 分支。
    config_inputs 为 None 表示配置中完全没有该 option（模拟新用户 / 未配置）。
    """
    override_str = json.dumps(OVERRIDE_TEMPLATE)
    for input_def in input_defs:
        name = input_def["name"]
        placeholder = "{" + name + "}"
        if config_inputs is not None and name in config_inputs:
            value = config_inputs[name]
            source = "配置中的值"
        else:
            value = input_def["default"]
            source = "default"
        override_str = override_str.replace('"' + placeholder + '"', '"' + value + '"')
        override_str = override_str.replace(placeholder, value)
    return json.loads(override_str), source


PASS, FAIL = [], []


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(f"  [{'PASS' if cond else 'FAIL'}] {name}")
    if detail:
        print(f"        {detail}")


def tpl(result):
    return result["SmallEventClickStage"]["recognition"]["param"]["template"]


print("=" * 70)
print("场景 1：新用户（配置中无该 option）→ 应使用 interface.json 的 default")
print("=" * 70)
r, src = build_input_override(INPUT_DEFS, None)
print(f"  取值来源: {src}")
print(f"  template = {tpl(r)}")
check("新用户拿到最新 default", tpl(r)[0] == "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png",
      f"实际 = {tpl(r)}")

print()
print("=" * 70)
print("场景 2：老用户（配置里有旧值），资源包更新为新主题")
print("=" * 70)
old_config = {"latestStage1": "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png",
              "latestStage2": "SmallEvent/DemoTheme/GreatVillainUnionStory2StageHard.png"}
r, src = build_input_override(INPUT_DEFS_NEW_VERSION, old_config)
print(f"  取值来源: {src}")
print(f"  template = {tpl(r)}")
check("老用户仍使用旧值，不会自动跟随新 default",
      tpl(r)[0] == "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png",
      f"实际 = {tpl(r)}  ← 确认与 default_case 同样的坑")

print()
print("=" * 70)
print("场景 3：老用户只配置了部分 input")
print("=" * 70)
partial_config = {"latestStage1": "SmallEvent/Old/OldStage1.png"}
r, src = build_input_override(INPUT_DEFS_NEW_VERSION, partial_config)
print(f"  template = {tpl(r)}")
check("已配置的字段用旧值", tpl(r)[0] == "SmallEvent/Old/OldStage1.png", f"{tpl(r)[0]}")
check("未配置的字段回落到新 default", tpl(r)[1] == "SmallEvent/NewTheme/NewThemeStory2StageHard.png",
      f"{tpl(r)[1]}  ← 部分更新会产生「新旧混合」的诡异状态")

print()
print("=" * 70)
print("场景 4：用户在 CLI 中敲回车（空输入）")
print("=" * 70)
# interactor.cpp:1584: value = buffer.empty() ? default_val : buffer
# 然后 :1600 config_opt.inputs[name] = value  →  写入配置
empty_input_buffer = ""
written_value = INPUT_DEFS[1]["default"] if empty_input_buffer == "" else empty_input_buffer
print(f"  敲回车后写入配置的值: {written_value}")
check("敲回车时把当时的 default 固化进配置文件",
      written_value == "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png",
      "→ 即使将来 default 改变，配置里已是快照值")
r, src = build_input_override(INPUT_DEFS_NEW_VERSION, {"latestStage1": written_value, "latestStage2": written_value})
check("固化后的值在主题更新后不再跟随", tpl(r)[0] == written_value,
      f"实际 = {tpl(r)}")

print()
print("=" * 70)
print("场景 5：注入风险 —— 配置值含引号")
print("=" * 70)
malicious = {"latestStage1": 'A.png", "INJECTED.png'}
r, src = build_input_override(INPUT_DEFS_NEW_VERSION, malicious)
print(f"  template = {tpl(r)}")
check("含引号的值向 template 数组注入额外元素", len(tpl(r)) > 2, f"元素数 = {len(tpl(r))}")

print()
print("=" * 70)
print(f"汇总: {len(PASS)} 通过 / {len(FAIL)} 失败")
for f in FAIL:
    print(f"  - FAIL: {f}")
print("=" * 70)
