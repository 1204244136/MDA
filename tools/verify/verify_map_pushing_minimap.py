"""用真实 MaaFramework 及虚拟控制器验证小地图展开流程；不连接设备。"""

import argparse
import ctypes as C
import json
import os
from pathlib import Path
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OPEN = "MapPushingOpenMinimap"
VISIBLE = "MapPushingMinimapVisible"
PROBE = "__MapPushingMinimapProbe"
P, B, I, ID, SIZE = C.c_void_p, C.c_uint8, C.c_int32, C.c_int64, C.c_uint64


class ControllerCallbacks(C.Structure):
    # 顺序对应 deps/include/MaaFramework/Instance/MaaCustomController.h。
    _fields_ = [(name, P) for name in (
        "connect", "connected", "request_uuid", "get_features", "start_app", "stop_app",
        "screencap", "click", "swipe", "touch_down", "touch_move", "touch_up",
        "click_key", "input_text", "key_down", "key_up", "scroll", "relative_move",
        "shell", "inactive", "get_info",
    )]


def load_runtime(log_dir):
    dll_dir = os.add_dll_directory(str(ROOT / "deps/bin"))
    maa = C.CDLL(str(ROOT / "deps/bin/MaaFramework.dll"))
    signatures = {
        "MaaGlobalSetOption": (B, [I, P, SIZE]),
        "MaaResourceCreate": (P, []),
        "MaaResourceDestroy": (None, [P]),
        "MaaResourcePostBundle": (ID, [P, C.c_char_p]),
        "MaaResourceWait": (I, [P, ID]),
        "MaaCustomControllerCreate": (P, [P, P]),
        "MaaControllerPostConnection": (ID, [P]),
        "MaaControllerWait": (I, [P, ID]),
        "MaaControllerDestroy": (None, [P]),
        "MaaImageBufferSetEncoded": (B, [P, P, SIZE]),
        "MaaStringBufferSet": (B, [P, C.c_char_p]),
        "MaaTaskerCreate": (P, []),
        "MaaTaskerDestroy": (None, [P]),
        "MaaTaskerBindResource": (B, [P, P]),
        "MaaTaskerBindController": (B, [P, P]),
        "MaaTaskerPostTask": (ID, [P, C.c_char_p, C.c_char_p]),
        "MaaTaskerWait": (I, [P, ID]),
        "MaaTaskerAddContextSink": (ID, [P, P, P]),
    }
    for name, (restype, argtypes) in signatures.items():
        api = getattr(maa, name)
        api.restype, api.argtypes = restype, argtypes
    encoded_dir = str(log_dir).encode()
    assert maa.MaaGlobalSetOption(1, encoded_dir, len(encoded_dir))
    level = I(0)
    assert maa.MaaGlobalSetOption(4, C.byref(level), C.sizeof(level))
    return maa, dll_dir


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", type=Path, help="可选：替代收起态 fixture 的现场截图")
    args = parser.parse_args()
    fixtures = ROOT / "tools/verify/fixtures"
    collapsed = (args.image or fixtures / "map_pushing_minimap_collapsed.png").read_bytes()
    expanded = (fixtures / "map_pushing_minimap_expanded.png").read_bytes()
    log_dir = Path(tempfile.mkdtemp(prefix="mda-map-minimap-"))
    print(f"运行日志：{log_dir}")
    maa, dll_dir = load_runtime(log_dir)
    callbacks = ControllerCallbacks()
    callback_refs = []
    state = {"frame": collapsed, "change_on_click": False}
    clicks, completed = [], []

    def bind(name, result_type, argtypes, function):
        callback = C.CFUNCTYPE(result_type, *argtypes)(function)
        callback_refs.append(callback)
        setattr(callbacks, name, C.cast(callback, P).value)

    def screencap(_, buffer):
        frame = state["frame"]
        return maa.MaaImageBufferSetEncoded(buffer, frame, len(frame))

    def click(x, y, _):
        clicks.append((x, y))
        if state["change_on_click"]:
            state["frame"] = expanded
        return 1

    def on_event(_, message, details, __):
        if message == b"Node.PipelineNode.Succeeded":
            completed.append(json.loads(details)["node_details"]["name"])

    bind("connect", B, [P], lambda _: 1)
    bind("connected", B, [P], lambda _: 1)
    bind("request_uuid", B, [P, P], lambda _, buf: maa.MaaStringBufferSet(buf, b"minimap-fixture"))
    bind("get_features", SIZE, [P], lambda _: 0)
    bind("screencap", B, [P, P], screencap)
    bind("click", B, [I, I, P], click)
    bind("inactive", B, [P], lambda _: 1)
    sink = C.CFUNCTYPE(None, P, C.c_char_p, C.c_char_p, P)(on_event)
    resource = controller = tasker = None
    try:
        resource = maa.MaaResourceCreate()
        job = maa.MaaResourcePostBundle(resource, str(ROOT / "assets/resource").encode())
        assert maa.MaaResourceWait(resource, job) == 3000, "项目资源加载失败"
        controller = maa.MaaCustomControllerCreate(C.byref(callbacks), None)
        assert maa.MaaControllerWait(controller, maa.MaaControllerPostConnection(controller)) == 3000
        tasker = maa.MaaTaskerCreate()
        assert maa.MaaTaskerBindResource(tasker, resource)
        assert maa.MaaTaskerBindController(tasker, controller)
        maa.MaaTaskerAddContextSink(tasker, C.cast(sink, P), None)

        def run(frame, next_nodes, change=False):
            state.update(frame=frame, change_on_click=change)
            clicks.clear()
            completed.clear()
            override = {
                PROBE: {"next": next_nodes, "timeout": 300, "rate_limit": 0,
                        "pre_delay": 0, "post_delay": 0},
                OPEN: {"timeout": 300, "rate_limit": 0, "pre_delay": 0, "post_delay": 0},
                VISIBLE: {"pre_delay": 0, "post_delay": 0},
            }
            job = maa.MaaTaskerPostTask(tasker, PROBE.encode(), json.dumps(override).encode())
            return maa.MaaTaskerWait(tasker, job)

        assert run(collapsed, [VISIBLE]) == 4000, "收起态误命中展开标志"
        assert not clicks
        print("[PASS] 收起态不满足原有小地图展开条件")

        assert run(collapsed, [f"[JumpBack]{OPEN}", VISIBLE], change=True) == 3000, "小地图展开恢复失败"
        assert completed == [PROBE, OPEN, VISIBLE, VISIBLE], completed
        assert len(clicks) == 1, clicks
        x, y = clicks[0]
        assert 19 <= x < 41 and 59 <= y < 83, clicks
        print(f"[PASS] 收起态点击一次 {clicks[0]}，确认展开并 JumpBack 返回正常分支")

        assert run(expanded, [OPEN]) == 4000, "展开态误命中展开按钮"
        assert not clicks and OPEN not in completed
        print("[PASS] 已展开时不触发展开点击")

        assert run(expanded, [OPEN, VISIBLE]) == 3000
        assert completed == [PROBE, VISIBLE] and not clicks, (completed, clicks)
        print("[PASS] 已展开时直接进入正常地图分支")

        assert run(collapsed, [OPEN]) == 4000, "点击未改变页面却被判成功"
        assert clicks and len(clicks) == 1 and VISIBLE not in completed, (completed, clicks)
        print("[PASS] 点击无效时验证失败，且不会重复点击或放行")
        print("汇总：5 组框架运行时验证通过；全部点击由虚拟控制器记录，无设备操作。")
    finally:
        if tasker:
            maa.MaaTaskerDestroy(tasker)
        if controller:
            maa.MaaControllerDestroy(controller)
        if resource:
            maa.MaaResourceDestroy(resource)
        dll_dir.close()


if __name__ == "__main__":
    main()
