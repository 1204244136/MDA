"""用项目实际资源和 MaaFramework.dll 验证小活动主题路由及动作隔离，无设备操作。"""

import ctypes
import json
from pathlib import Path
import os
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))
from tools.validate_schema import load_jsonc

ROUTE = "SmallEventEventStageQuickBattleFlow"
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

    passed = 0
    for case in cases:
        res = maa.MaaResourceCreate()
        try:
            job = maa.MaaResourcePostBundle(res, str(ROOT / "assets/resource").encode())
            assert maa.MaaResourceWait(res, job) == 3000, "实际资源加载失败"
            before = {name: node(res, name) for name in leaves}
            override(res, case.get("pipeline_override", {}))
            expected = case.get("pipeline_override", {}).get(ROUTE, story[ROUTE])["next"]
            got = node(res, ROUTE)
            assert next_names(got) == expected, (case["name"], got)
            assert got["action"]["type"] == "DoNothing", got
            assert {name: node(res, name) for name in leaves} == before, case["name"]
            assert node(res, NORMAL)["action"]["param"]["target_offset"] == [-80, -30, 0, 0]
            assert node(res, HARD)["action"]["param"]["target_offset"] == [-40, 45, 0, 0]
            print(f"[PASS] {case['name']}: 路由整体替换，全部主题模板/动作保持独立")
            passed += 1

            if case["name"] == "CurrentEvent":
                old = next(c for c in cases if c["name"] == "BSideIdol")
                # 模拟下次升级：base 当前模板/路由换为另一主题，不改任何旧主题动作。
                override(res, old["pipeline_override"])
                assert next_names(node(res, ROUTE)) == old["pipeline_override"][ROUTE]["next"]
                assert {name: node(res, name) for name in leaves} == before
                # 更新后显式回选 LittleWitchTrick，仍拿到自己的 Normal/Hard 节点与偏移。
                witch = next(c for c in cases if c["name"] == "LittleWitchTrick")
                override(res, witch["pipeline_override"])
                assert next_names(node(res, ROUTE)) == [NORMAL, HARD, "SmallEventGoBack"]
                assert {name: node(res, name) for name in leaves} == before
                print("[PASS] 升级当前路由后回选旧主题：保留旧主题动作，不继承新主题参数")
                passed += 1
        finally:
            maa.MaaResourceDestroy(res)
    dll_dir.close()
    print(f"汇总：{passed} 组框架运行时验证通过；未连接设备。")


if __name__ == "__main__":
    main()
