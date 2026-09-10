"""
用 MaaFramework 公开 C API 验证 pipeline_override 的合并语义。

验证目标：
1. override 是否为「按 prop 深合并」（未指定字段继承 base）
2. 同一 option 的多 case 是否互斥（只应用选中的一个）
3. 多 option 的 override 是否按顺序叠加
4. 字符串型占位符替换后能否生成合法 JSON 数组

用法（从仓库根目录运行）：
    python tools/verify/verify_override.py
依赖 deps/bin/MaaFramework.dll，资源目录用 tools/verify/verify-resource。
"""

import ctypes
import json
import os
import sys

REPO_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
DLL_DIR = os.path.join(REPO_ROOT, "deps", "bin")
RES_DIR = os.path.join(REPO_ROOT, "tools", "verify", "verify-resource")

os.add_dll_directory(DLL_DIR)
maa = ctypes.CDLL(os.path.join(DLL_DIR, "MaaFramework.dll"))

maa.MaaResourceCreate.restype = ctypes.c_void_p
maa.MaaResourcePostBundle.restype = ctypes.c_longlong
maa.MaaResourcePostBundle.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
maa.MaaResourceWait.restype = ctypes.c_int
maa.MaaResourceWait.argtypes = [ctypes.c_void_p, ctypes.c_longlong]
maa.MaaResourceOverridePipeline.restype = ctypes.c_uint8
maa.MaaResourceOverridePipeline.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
maa.MaaResourceGetNodeData.restype = ctypes.c_uint8
maa.MaaResourceGetNodeData.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_void_p]
maa.MaaResourceDestroy.argtypes = [ctypes.c_void_p]

maa.MaaStringBufferCreate.restype = ctypes.c_void_p
maa.MaaStringBufferGet.restype = ctypes.c_char_p
maa.MaaStringBufferGet.argtypes = [ctypes.c_void_p]
maa.MaaStringBufferDestroy.argtypes = [ctypes.c_void_p]

PASS, FAIL = [], []


def check(name, cond, detail=""):
    (PASS if cond else FAIL).append(name)
    print(f"  [{'PASS' if cond else 'FAIL'}] {name}")
    if detail:
        print(f"        {detail}")


def get_node(res, node):
    buf = maa.MaaStringBufferCreate()
    ok = maa.MaaResourceGetNodeData(res, node.encode(), buf)
    raw = maa.MaaStringBufferGet(buf)
    data = json.loads(raw.decode("utf-8")) if (ok and raw) else None
    maa.MaaStringBufferDestroy(buf)
    return data


def templates_of(node_data):
    """从 GetNodeData 的返回里取 template 列表"""
    if not node_data:
        return None
    # 结构可能是 {"recognition": {"param": {"template": [...]}}}
    reco = node_data.get("recognition") or {}
    param = reco.get("param") or {}
    return param.get("template")


def main():
    res = maa.MaaResourceCreate()
    rid = maa.MaaResourcePostBundle(res, RES_DIR.encode())
    st = maa.MaaResourceWait(res, rid)
    print(f"\n资源加载状态: {st} (3000=Succeeded)")
    if st != 3000:
        print("资源加载失败，终止")
        sys.exit(1)

    base = get_node(res, "SmallEventClickStage")
    print(f"\n=== base 节点（未 override）===")
    print(f"  template = {templates_of(base)}")
    print(f"  完整数据 = {json.dumps(base, ensure_ascii=False)[:300]}")

    print("\n" + "=" * 60)
    print("验证 1：override 是否为 prop 级深合并（未指定字段继承 base）")
    print("=" * 60)
    ov = {
        "SmallEventClickStage": {
            "recognition": {"param": {"template": ["SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png"]}}
        }
    }
    ok = maa.MaaResourceOverridePipeline(res, json.dumps(ov).encode())
    merged = get_node(res, "SmallEventClickStage")
    tpl = templates_of(merged)
    check("override 生效，template 被替换为单个元素", tpl == ["SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png"],
          f"实际 template = {tpl}")
    # base 里 order_by=Vertical, index=-1 —— 检查是否继承
    param = (merged.get("recognition") or {}).get("param") or {}
    check("base 的 order_by 被继承（未在 override 中指定）", param.get("order_by") == "Vertical",
          f"order_by = {param.get('order_by')}")
    check("base 的 index 被继承（未在 override 中指定）", param.get("index") == -1,
          f"index = {param.get('index')}")
    roi = param.get("roi")
    check("base 的 roi 被继承", roi == [314, 103, 661, 604], f"roi = {roi}")

    print("\n" + "=" * 60)
    print("验证 2：template 是整体替换还是追加？")
    print("=" * 60)
    ov2 = {
        "SmallEventClickStage": {
            "recognition": {"param": {"template": ["A.png", "B.png"]}}
        }
    }
    maa.MaaResourceOverridePipeline(res, json.dumps(ov2).encode())
    tpl2 = templates_of(get_node(res, "SmallEventClickStage"))
    check("template 是整体替换（结果是 [A.png, B.png]，不是 3 个元素）", tpl2 == ["A.png", "B.png"],
          f"实际 = {tpl2}")

    print("\n" + "=" * 60)
    print("验证 3：多 option 的 override 是否按顺序叠加（后者覆盖前者）")
    print("=" * 60)
    ov_a = {"SmallEventClickStage": {"recognition": {"param": {"template": ["FROM_A.png"]}}}}
    ov_b = {"SmallEventClickStage": {"recognition": {"param": {"template": ["FROM_B.png"]}}}}
    maa.MaaResourceOverridePipeline(res, json.dumps(ov_a).encode())
    maa.MaaResourceOverridePipeline(res, json.dumps(ov_b).encode())
    tpl3 = templates_of(get_node(res, "SmallEventClickStage"))
    check("后应用的 override 覆盖前者（模拟 option 优先级）", tpl3 == ["FROM_B.png"], f"实际 = {tpl3}")

    print("\n" + "=" * 60)
    print("验证 4：字符串占位符替换后能否生成合法 JSON 数组（input 方案核心）")
    print("=" * 60)
    # 模拟 Configurator.cpp:481-511 的 input 处理
    template_json = {
        "SmallEventClickStage": {
            "recognition": {"param": {"template": ["{stage1}", "{stage2}"]}}
        }
    }
    override_str = json.dumps(template_json)
    print(f"  替换前: {override_str}")
    inputs = {
        "stage1": "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png",
        "stage2": "SmallEvent/DemoTheme/GreatVillainUnionStory2StageHard.png",
    }
    for name, value in inputs.items():
        ph = "{" + name + "}"
        override_str = override_str.replace('"' + ph + '"', '"' + value + '"')
        override_str = override_str.replace(ph, value)
    print(f"  替换后: {override_str}")
    try:
        parsed = json.loads(override_str)
        tpl4 = parsed["SmallEventClickStage"]["recognition"]["param"]["template"]
        check("占位符替换产出合法 JSON", True, f"解析结果 template = {tpl4}")
        check("数组元素数量正确（2 个）", len(tpl4) == 2, f"实际 {len(tpl4)} 个")
        # 真正应用到 resource 验证
        maa.MaaResourceOverridePipeline(res, override_str.encode())
        tpl4_real = templates_of(get_node(res, "SmallEventClickStage"))
        check("替换后的 override 被框架接受并生效", tpl4_real == tpl4, f"框架内实际 = {tpl4_real}")
    except Exception as e:
        check("占位符替换产出合法 JSON", False, f"解析失败: {e}")

    print("\n" + "=" * 60)
    print("验证 5：单个占位符承载逗号分隔的多路径会怎样（边界）")
    print("=" * 60)
    template_json2 = {
        "SmallEventClickStage": {
            "recognition": {"param": {"template": ["{multi}"]}}
        }
    }
    s = json.dumps(template_json2)
    multi_value = "SmallEvent/DemoTheme/A.png,SmallEvent/DemoTheme/B.png"
    s = s.replace('"{multi}"', '"' + multi_value + '"').replace("{multi}", multi_value)
    print(f"  替换结果片段: {s[s.index('template'):s.index('template')+120]}")
    try:
        parsed2 = json.loads(s)
        tpl5 = parsed2["SmallEventClickStage"]["recognition"]["param"]["template"]
        check("逗号串被当作单个字符串（未自动拆分为数组）", tpl5 == [multi_value],
              f"实际 = {tpl5}")
        maa.MaaResourceOverridePipeline(res, s.encode())
        tpl5_real = templates_of(get_node(res, "SmallEventClickStage"))
        check("框架接受该字符串作为单一模板路径", tpl5_real == [multi_value], f"实际 = {tpl5_real}")
    except Exception as e:
        check("逗号串替换解析", False, f"失败: {e}")

    print("\n" + "=" * 60)
    print("验证 6：值中含双引号会否破坏 JSON（转义风险）")
    print("=" * 60)
    s = json.dumps({"N": {"recognition": {"param": {"template": ["{bad}"]}}}})
    bad_value = 'A", "INJECTED'
    s2 = s.replace('"{bad}"', '"' + bad_value + '"').replace("{bad}", bad_value)
    print(f"  替换后: {s2}")
    try:
        p = json.loads(s2)
        tpl6 = p["N"]["recognition"]["param"]["template"]
        check("含引号的值导致结构被破坏（证明存在注入风险）", len(tpl6) > 1,
              f"template = {tpl6}  ← 元素数被改变，说明结构被注入")
    except Exception as e:
        check("含引号的值导致 JSON 解析失败", True, f"异常: {type(e).__name__}")

    maa.MaaResourceDestroy(res)

    print("\n" + "=" * 60)
    print(f"结果汇总: {len(PASS)} 通过 / {len(FAIL)} 失败")
    if FAIL:
        print("失败项:")
        for f in FAIL:
            print(f"  - {f}")
    print("=" * 60)


if __name__ == "__main__":
    main()
