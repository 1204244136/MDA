"""用项目实际资源和 MaaFramework.dll 验证小活动主题路由及动作隔离，无设备操作。"""

import ctypes
import json
import os
from pathlib import Path
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from tools.validate_schema import load_jsonc

ROUTE = "SmallEventEventStageQuickBattleFlow"
SHARED = "SmallEventClickStageRepeatable"
NORMAL = "SmallEventLittleWitchTrickClickStageNormalRepeatable"
HARD = "SmallEventLittleWitchTrickClickStageHardRepeatable"


def main():
    dll_dir = os.add_dll_directory(str(ROOT / "deps/bin"))
    maa = ctypes.CDLL(str(ROOT / "deps/bin/MaaFramework.dll"))
    signatures = {
        "MaaResourceCreate": (ctypes.c_void_p, []),
        "MaaResourceDestroy": (None, [ctypes.c_void_p]),
        "MaaResourcePostBundle": (ctypes.c_longlong, [ctypes.c_void_p, ctypes.c_char_p]),
        "MaaResourceWait": (ctypes.c_int, [ctypes.c_void_p, ctypes.c_longlong]),
        "MaaResourceOverridePipeline": (ctypes.c_uint8, [ctypes.c_void_p, ctypes.c_char_p]),
        "MaaResourceGetNodeData": (ctypes.c_uint8, [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_void_p]),
        "MaaStringBufferCreate": (ctypes.c_void_p, []),
        "MaaStringBufferGet": (ctypes.c_char_p, [ctypes.c_void_p]),
        "MaaStringBufferDestroy": (None, [ctypes.c_void_p]),
    }
    for name, (restype, argtypes) in signatures.items():
        api = getattr(maa, name)
        api.restype, api.argtypes = restype, argtypes

    def node(res, name):
        buf = maa.MaaStringBufferCreate()
        try:
            assert maa.MaaResourceGetNodeData(res, name.encode(), buf), name
            return json.loads(maa.MaaStringBufferGet(buf).decode())
        finally:
            maa.MaaStringBufferDestroy(buf)

    def override(res, data):
        assert maa.MaaResourceOverridePipeline(res, json.dumps(data).encode()), data

    def next_names(data):
        return [item if isinstance(item, str) else item["name"] for item in data["next"]]

    cases = load_jsonc(ROOT / "assets/tasks/SmallEvent.json")["option"]["SmallEventTheme"]["cases"]
    story = load_jsonc(ROOT / "assets/resource/pipeline/Event/SmallEvent/SmallEventStory.json")
    leaves = {}
    for file in (ROOT / "assets/resource/pipeline/Event/SmallEvent/Themes").glob("*.json"):
        leaves.update(load_jsonc(file))
    assert set(leaves.keys()) == {NORMAL, HARD}, set(leaves.keys())

    passed = 0
    for case in cases:
        res = maa.MaaResourceCreate()
        try:
            job = maa.MaaResourcePostBundle(res, str(ROOT / "assets/resource").encode())
            assert maa.MaaResourceWait(res, job) == 3000, "实际资源加载失败"
            before = {name: node(res, name) for name in leaves}
            po = case.get("pipeline_override", {})
            override(res, po)
            expected = po.get(ROUTE, story[ROUTE])["next"]
            got = node(res, ROUTE)
            assert next_names(got) == expected, (case["name"], got)
            assert got["action"]["type"] == "DoNothing", got
            assert {name: node(res, name) for name in leaves} == before, case["name"]
            assert node(res, NORMAL)["action"]["param"]["target_offset"] == [-80, -30, 0, 0]
            assert node(res, HARD)["action"]["param"]["target_offset"] == [-40, 45, 0, 0]
            assert node(res, SHARED)["action"]["param"]["target_offset"] == [0, 0, 0, 0]
            if expected[0] == SHARED:
                expected_tpl = po.get(SHARED, story[SHARED])["recognition"]["param"]["template"]
                assert node(res, SHARED)["recognition"]["param"]["template"] == expected_tpl, case["name"]
            print(f"[PASS] {case['name']}: 路由整体替换，共用节点与特例节点动作保持隔离")
            passed += 1

            if case["name"] == "CurrentEvent":
                witch = next(c for c in cases if c["name"] == "LittleWitchTrick")
                old = next(c for c in cases if c["name"] == "BSideIdol")

                # 模拟 base 升级为特例主题后，再显式回选旧常规主题。
                override(res, witch["pipeline_override"])
                assert next_names(node(res, ROUTE)) == [NORMAL, HARD, "SmallEventGoBack"]
                assert node(res, NORMAL)["action"]["param"]["target_offset"] == [-80, -30, 0, 0]
                assert node(res, HARD)["action"]["param"]["target_offset"] == [-40, 45, 0, 0]

                override(res, old["pipeline_override"])
                assert next_names(node(res, ROUTE)) == [SHARED, "SmallEventGoBack"]
                assert (
                    node(res, SHARED)["recognition"]["param"]["template"]
                    == old["pipeline_override"][SHARED]["recognition"]["param"]["template"]
                )
                assert node(res, SHARED)["action"]["param"]["target_offset"] == [0, 0, 0, 0]
                assert {name: node(res, name) for name in leaves} == before

                # 再从常规主题显式回选特例主题，确认仍拿到各自偏移与路由。
                override(res, witch["pipeline_override"])
                assert next_names(node(res, ROUTE)) == [NORMAL, HARD, "SmallEventGoBack"]
                assert {name: node(res, name) for name in leaves} == before
                print("[PASS] 特例/常规双向切换回选：各自保留路由与偏移，无跨主题污染")
                passed += 1
        finally:
            maa.MaaResourceDestroy(res)
    dll_dir.close()
    print(f"汇总：{passed} 组框架运行时验证通过；未连接设备。")


if __name__ == "__main__":
    main()
