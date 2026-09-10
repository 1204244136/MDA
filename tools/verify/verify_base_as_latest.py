"""
验证「base 节点承载最新主题」方案的可行性。

核心假设：
A. base 节点的 template 是「默认值」，选中主题 case 的 override 会整体替换它
   → 往期主题不受 base 变化影响
B. 未选中任何主题 case（或选中一个无 override 的 case）时，保留 base 的 template
   → CurrentEvent 可以完全没有 override
C. 用一个「无 override 的 case」承接 CurrentEvent 语义是可行的
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


def tpl_of(res, node):
    buf = maa.MaaStringBufferCreate()
    ok = maa.MaaResourceGetNodeData(res, node.encode(), buf)
    raw = maa.MaaStringBufferGet(buf)
    data = json.loads(raw.decode("utf-8")) if (ok and raw) else None
    maa.MaaStringBufferDestroy(buf)
    if not data:
        return None
    return ((data.get("recognition") or {}).get("param") or {}).get("template")


LATEST = "SmallEvent/DemoTheme/GreatVillainUnionStory1Stage.png"
LATEST2 = "SmallEvent/DemoTheme/GreatVillainUnionStory2StageHard.png"
OLD = "SmallEvent/OldTheme/OldStory1Stage.png"


def main():
    res = maa.MaaResourceCreate()
    rid = maa.MaaResourcePostBundle(res, RES_DIR.encode())
    st = maa.MaaResourceWait(res, rid)
    print(f"资源加载: {st} (3000=Succeeded)")
    if st != 3000:
        sys.exit(1)

    print("\n" + "=" * 64)
    print("假设 A：base 已是「最新主题」时，往期主题 override 仍能正确覆盖")
    print("=" * 64)
    # 先把 base 改成「最新主题」的模板（模拟新方案的 base 节点）
    base_new = {
        "SmallEventClickStage": {
            "recognition": {"param": {"template": [LATEST, LATEST2]}}
        }
    }
    ok = maa.MaaResourceOverridePipeline(res, json.dumps(base_new).encode())
    check("base 节点成功改为最新主题模板", ok == 1)
    got = tpl_of(res, "SmallEventClickStage")
    check("base 当前 template 为最新主题", got == [LATEST, LATEST2], f"{got}")

    # 现在模拟用户选了往期主题：应用旧主题的 override
    old_override = {
        "SmallEventClickStage": {
            "recognition": {"param": {"template": [OLD]}}
        }
    }
    maa.MaaResourceOverridePipeline(res, json.dumps(old_override).encode())
    got = tpl_of(res, "SmallEventClickStage")
    check("往期主题 override 完整替换 base 模板（不残留最新主题的图）",
          got == [OLD], f"实际 = {got}  ← 应为 [OLD]，不能混入 LATEST")

    print("\n" + "=" * 64)
    print("假设 B：不应用任何 override 时，base 的新模板保留")
    print("=" * 64)
    res2 = maa.MaaResourceCreate()
    rid2 = maa.MaaResourcePostBundle(res2, RES_DIR.encode())
    maa.MaaResourceWait(res2, rid2)
    # 直接应用 base 模板，然后「不应用任何 case override」
    maa.MaaResourceOverridePipeline(res2, json.dumps(base_new).encode())
    got2 = tpl_of(res2, "SmallEventClickStage")
    check("CurrentEvent 无 override 时，base 模板即为最新主题",
          got2 == [LATEST, LATEST2], f"实际 = {got2}")

    print("\n" + "=" * 64)
    print("假设 C：空 override（{}）是否会清空 base")
    print("=" * 64)
    maa.MaaResourceOverridePipeline(res2, json.dumps({}).encode())
    got3 = tpl_of(res2, "SmallEventClickStage")
    check("空 override 不影响 base（仍是最新主题）",
          got3 == [LATEST, LATEST2], f"实际 = {got3}")

    print("\n" + "=" * 64)
    print("假设 D：只覆盖部分节点时，其余节点保持 base")
    print("=" * 64)
    res3 = maa.MaaResourceCreate()
    rid3 = maa.MaaResourcePostBundle(res3, RES_DIR.encode())
    maa.MaaResourceWait(res3, rid3)
    # 模拟：新方案下三个节点 base 全部写成最新主题
    full_base = {
        "SmallEvent": {"recognition": {"param": {"template": ["LATEST_LOGO"]}}},
        "SmallEventClickStage": {"recognition": {"param": {"template": [LATEST, LATEST2]}}},
        "SmallEventClickStageRepeatable": {"recognition": {"param": {"template": ["LATEST_REP1", "LATEST_REP2"]}}},
    }
    maa.MaaResourceOverridePipeline(res3, json.dumps(full_base).encode())
    # 往期主题只覆盖其中两个节点（模拟 RealWorld：某些主题覆盖不全）
    partial = {
        "SmallEvent": {"recognition": {"param": {"template": ["OLD_LOGO"]}}},
        "SmallEventClickStage": {"recognition": {"param": {"template": [OLD]}}},
    }
    maa.MaaResourceOverridePipeline(res3, json.dumps(partial).encode())
    check("被覆盖的节点用旧主题", tpl_of(res3, "SmallEvent") == ["OLD_LOGO"])
    check("被覆盖的节点用旧主题（关卡）", tpl_of(res3, "SmallEventClickStage") == [OLD])
    check("未覆盖的节点落到 base（最新主题）",
          tpl_of(res3, "SmallEventClickStageRepeatable") == ["LATEST_REP1", "LATEST_REP2"],
          f"实际 = {tpl_of(res3, 'SmallEventClickStageRepeatable')}  ← 这是风险点：旧主题会混入最新主题的模板")

    maa.MaaResourceDestroy(res)
    maa.MaaResourceDestroy(res2)
    maa.MaaResourceDestroy(res3)

    print("\n" + "=" * 64)
    print(f"汇总: {len(PASS)} 通过 / {len(FAIL)} 失败")
    for f in FAIL:
        print(f"  - {f}")
    print("=" * 64)


if __name__ == "__main__":
    main()
