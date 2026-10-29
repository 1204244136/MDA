"""使用项目当前 MaaFramework DLL 离线验证纯场景导航，不连接设备。

保留实际 Pipeline 的 next、JumpBack、And/Or、inverse、enabled 与 max_hit。
只把底层画面识别替换为可控探针，把动作替换为可观察的虚拟页面变化；
业务主流程在明确的测试边界终止。测试不评价 OCR、点击落点或实机稳定性。
"""

from __future__ import annotations

import argparse
import base64
import ctypes as C
import json
import os
from pathlib import Path
import re
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
P, B, I, ID, SIZE = C.c_void_p, C.c_uint8, C.c_int32, C.c_int64, C.c_uint64
SUCCEEDED, FAILED = 3000, 4000
RECO_NAME, ACTION_NAME = "SceneNavigationProbe", "SceneNavigationAction"
PIXEL = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
)


class ControllerCallbacks(C.Structure):
    # ABI order: deps/include/MaaFramework/Instance/MaaCustomController.h.
    _fields_ = [(name, P) for name in (
        "connect", "connected", "request_uuid", "get_features", "start_app", "stop_app",
        "screencap", "click", "swipe", "touch_down", "touch_move", "touch_up",
        "click_key", "input_text", "key_down", "key_up", "scroll", "relative_move",
        "shell", "inactive", "get_info",
    )]


def read_jsonc(path: Path):
    """Read JSONC without touching source files or requiring extra packages."""
    token = re.compile(r'("(?:\\.|[^"\\])*")|//[^\r\n]*|/\*[\s\S]*?\*/')
    source = token.sub(lambda m: m.group(1) or "", path.read_text(encoding="utf-8-sig"))
    trailing = re.compile(r'("(?:\\.|[^"\\])*")|,\s*(?=[}\]])')
    return json.loads(trailing.sub(lambda m: m.group(1) or "", source))


def read_nodes(resource: Path) -> dict:
    nodes = {}
    for path in sorted((resource / "pipeline").rglob("*.json")):
        parsed = read_jsonc(path)
        for name, node in parsed.items():
            if name != "$schema":
                if name in nodes:
                    raise ValueError(f"重复节点：{name}")
                nodes[name] = node
    return nodes


def option_overrides(task_file, **choices):
    """Use declared PI cases so the option wiring is part of the regression."""
    options = read_jsonc(ROOT / "assets" / "tasks" / task_file)["option"]
    overrides = {}
    for name, choice in choices.items():
        case = next(case for case in options[name]["cases"] if case["name"] == choice)
        for node, value in case.get("pipeline_override", {}).items():
            overrides.setdefault(node, {}).update(value)
    return overrides


def load_api(dll_path: Path, log_dir: Path):
    cookie = os.add_dll_directory(str(dll_path.parent))
    maa = C.CDLL(str(dll_path))
    signatures = {
        "MaaVersion": (C.c_char_p, []),
        "MaaGlobalSetOption": (B, [I, P, SIZE]),
        "MaaResourceCreate": (P, []),
        "MaaResourceDestroy": (None, [P]),
        "MaaResourcePostBundle": (ID, [P, C.c_char_p]),
        "MaaResourceWait": (I, [P, ID]),
        "MaaResourceRegisterCustomRecognition": (B, [P, C.c_char_p, P, P]),
        "MaaResourceRegisterCustomAction": (B, [P, C.c_char_p, P, P]),
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
        "MaaTaskerStatus": (I, [P, ID]),
        "MaaTaskerPostStop": (ID, [P]),
        "MaaTaskerAddContextSink": (ID, [P, P, P]),
    }
    for name, (restype, argtypes) in signatures.items():
        fn = getattr(maa, name)
        fn.restype, fn.argtypes = restype, argtypes
    encoded = str(log_dir).encode()
    assert maa.MaaGlobalSetOption(1, encoded, len(encoded))
    level = I(0)
    assert maa.MaaGlobalSetOption(4, C.byref(level), C.sizeof(level))
    return maa, cookie


def reco_type(node):
    recognition = node.get("recognition", "DirectHit")
    return recognition.get("type", "DirectHit") if isinstance(recognition, dict) else recognition


def probe_overrides(nodes: dict) -> dict:
    result = {}
    for name, node in nodes.items():
        override = {"timeout": 150, "rate_limit": 0, "pre_delay": 0, "post_delay": 0,
                    "pre_wait_freezes": 0, "post_wait_freezes": 0}
        # Keep compound recognitions intact: this is what verifies the framework's
        # And reference semantics rather than a Python model of the node graph.
        if reco_type(node) not in {"DirectHit", "And", "Or"}:
            override["recognition"] = {"type": "Custom", "param": {
                "custom_recognition": RECO_NAME,
                "custom_recognition_param": {"node": name},
            }}
        action = node.get("action", "DoNothing")
        action_type = action.get("type", "DoNothing") if isinstance(action, dict) else action
        if action_type != "DoNothing":
            override["action"] = {"type": "Custom", "param": {
                "custom_action": ACTION_NAME, "custom_action_param": {"node": name},
            }}
        result[name] = override
    return result


class NavigationRun:
    def __init__(self, maa, resource, nodes):
        self.maa, self.resource, self.nodes = maa, resource, nodes
        self.refs, self.completed, self.actions, self.errors = [], [], [], []
        self.page, self.reward, self.missing = "unknown", False, set()
        self.frozen, self.extra_visible = False, set()
        self.recognition_count = 0
        self.controller = self.tasker = None
        callbacks = ControllerCallbacks()

        def bind(name, result_type, argtypes, function):
            cb = C.CFUNCTYPE(result_type, *argtypes)(function)
            self.refs.append(cb)
            setattr(callbacks, name, C.cast(cb, P).value)

        bind("connect", B, [P], lambda _: 1)
        bind("connected", B, [P], lambda _: 1)
        bind("request_uuid", B, [P, P], lambda _, buf: maa.MaaStringBufferSet(buf, b"scene-fixture"))
        bind("get_features", SIZE, [P], lambda _: 0)
        bind("screencap", B, [P, P], lambda _, buf: maa.MaaImageBufferSetEncoded(buf, PIXEL, len(PIXEL)))
        bind("inactive", B, [P], lambda _: 1)
        # All executable production actions are replaced; any raw controller
        # action indicates an incomplete harness instead of touching a device.
        bind("click", B, [I, I, P], lambda x, y, _: self.unexpected(f"raw click {x},{y}"))
        self.refs.append(callbacks)

        reco = C.CFUNCTYPE(B, P, ID, C.c_char_p, C.c_char_p, C.c_char_p, P, P, P, P, P)(self.recognize)
        action = C.CFUNCTYPE(B, P, ID, C.c_char_p, C.c_char_p, C.c_char_p, ID, P, P)(self.act)
        sink = C.CFUNCTYPE(None, P, C.c_char_p, C.c_char_p, P)(self.event)
        self.refs.extend([reco, action, sink])
        assert maa.MaaResourceRegisterCustomRecognition(resource, RECO_NAME.encode(), C.cast(reco, P), None)
        assert maa.MaaResourceRegisterCustomAction(resource, ACTION_NAME.encode(), C.cast(action, P), None)
        self.controller = maa.MaaCustomControllerCreate(C.byref(callbacks), None)
        assert maa.MaaControllerWait(self.controller, maa.MaaControllerPostConnection(self.controller)) == SUCCEEDED
        self.tasker = maa.MaaTaskerCreate()
        assert maa.MaaTaskerBindResource(self.tasker, resource)
        assert maa.MaaTaskerBindController(self.tasker, self.controller)
        maa.MaaTaskerAddContextSink(self.tasker, C.cast(sink, P), None)

    def unexpected(self, message):
        self.errors.append(message)
        return 0

    def visible(self, name):
        if name in self.missing:
            return False
        if name in self.extra_visible:
            return True
        pages = {
            "CommonArkEntered": {"ark"},
            "CommonArkCornerVisible": {"arena", "interception", "simulation", "tower"},
            "CommonArkVisible": {"hall"},
            "CommonBellIconVisible": {"hall"},
            "CommonMailIconVisible": {"hall"},
            "CommonFreeStateVisible": {"hall"},
            "CommonEnterOutpost": {"hall"},
            "OutpostMainPageEntered": {"outpost"},
            "ArkEnterArena": {"ark"},
            "ArkEnterInterception": {"ark"},
            "SimulationRoomEnterMain": {"ark"},
            "ArkEnterTribeTower": {"ark"},
            "RookieArenaVisible": {"arena"},
            "SpecialArenaVisible": {"arena"},
            "ChampionArenaVisible": {"arena"},
            "InterceptionMainPageEntered": {"interception"},
            "SimulationRoomMainPageEntered": {"simulation"},
            "TribeTowerMainPageEntered": {"tower"},
            "CommonClickHomeButton": {"unknown", "outpost", "arena", "interception", "simulation", "tower"},
            "CommonClickHall": {"unknown", "outpost", "arena", "interception", "simulation", "tower"},
            "CommonClickFreeState": {"hall"},
            "CommonNoticeVisible": {"hall_notice"},
            "CommonClosePage": {"hall_notice"},
            "ShopMainPageEntered": {"shop"},
            "HallEnterShop": {"hall"},
            "FriendManagementOnFriendPage": {"friend"},
            "FriendManagementClickFriendEntry": {"hall"},
            "AdviseMainPageEntered": {"advise"},
            "AdviseNikkeMainPageEntered": {"nikke"},
            "AdviseClickAdvisePage": {"nikke"},
            # Both titles satisfy the historical .*商店.* OCR. Only the paid
            # shop has the ordinary package tab required by ResumeFlow.
            "CashShopMainPageEntered": {"shop", "cashshop"},
            "ShopChooseOrdinaryPackage": {"cashshop"},
            "SynchroDeviceEntered": {"synchro"},
            "RecycleRoomEntered": {"recycle"},
        }
        if name == "ArenaClaimSpecialReward":
            return self.page == "ark" and self.reward
        return self.page in pages.get(name, set())

    def recognize(self, context, task_id, name, custom, params, image, roi, trans, out_box, out_detail):
        try:
            self.recognition_count += 1
            node = json.loads(params.decode())["node"]
            if not self.visible(node):
                return 0
            C.cast(out_box, C.POINTER(I * 4)).contents[:] = (10, 10, 10, 10)
            return 1
        except Exception as exc:
            return self.unexpected(f"recognition callback: {exc}")

    def act(self, context, task_id, name, custom, params, reco_id, box, trans):
        try:
            node = json.loads(params.decode())["node"]
            self.actions.append(node)
            targets = {
                "CommonClickHomeButton": "hall", "CommonClickHall": "hall",
                "CommonClickFreeState": "hall_notice", "CommonClosePage": "hall",
                "CommonClickArk": "ark",
                "__ScenePrivateClickArk": "ark",
                "__ScenePrivateClickOutpost": "outpost", "__SceneArkClickArena": "arena",
                "__SceneArkClickInterception": "interception",
                "__SceneArkClickSimulationRoom": "simulation", "__SceneArkClickTribeTower": "tower",
                "__ScenePrivateClickShop": "shop", "__ScenePrivateClickFriend": "friend",
                "AdviseClickAdvisePage": "advise",
            }
            if node in targets:
                if not self.frozen:
                    self.page = targets[node]
            elif node == "ArenaClaimSpecialReward":
                self.reward = False
            elif node == "CommonClickBlank":
                pass  # Existing interception entry popup dismissal.
            elif node in {"ArenaMain", "OutpostMain", "InterceptionMain", "SimulationRoomMain", "TribeTowerMain",
                          "AdviseMain", "CashShopMain", "AccountNurturingMain"}:
                pass  # RuntimeQuotaCheck boundary.
            else:
                return self.unexpected(f"unexpected business action: {node}")
            return 1
        except Exception as exc:
            return self.unexpected(f"action callback: {exc}")

    def event(self, handle, message, details, trans):
        if message == b"Node.PipelineNode.Succeeded":
            self.completed.append(json.loads(details)["node_details"]["name"])

    def run(self, entry, page, *, missing=(), reward=False, extra=None, frozen=False, visible=()):
        self.page, self.reward, self.missing = page, reward, set(missing)
        self.frozen, self.extra_visible = frozen, set(visible)
        self.completed.clear()
        self.actions.clear()
        self.errors.clear()
        self.recognition_count = 0
        overrides = probe_overrides(self.nodes)
        for name, override in (extra or {}).items():
            overrides[name].update(override)
        job = self.maa.MaaTaskerPostTask(self.tasker, entry.encode(), json.dumps(overrides).encode())
        started = time.monotonic()
        while self.maa.MaaTaskerStatus(self.tasker, job) in (1000, 2000):
            if time.monotonic() - started > 5:
                self.maa.MaaTaskerWait(self.tasker, self.maa.MaaTaskerPostStop(self.tasker))
                raise AssertionError(f"导航未在5秒内结束：{entry}; {self.completed[-20:]}")
            time.sleep(0.005)
        status = self.maa.MaaTaskerWait(self.tasker, job)
        assert not self.errors, self.errors
        return status

    def close(self):
        if self.tasker:
            self.maa.MaaTaskerDestroy(self.tasker)
        if self.controller:
            self.maa.MaaControllerDestroy(self.controller)


def check_behaviors(run):
    count = 0
    for entry, page in (("SceneEnterHall", "hall"), ("SceneEnterArk", "ark"),
                        ("SceneEnterOutpost", "outpost"), ("SceneEnterArena", "arena"),
                        ("SceneEnterInterception", "interception"),
                        ("SceneEnterSimulationRoom", "simulation"), ("SceneEnterTribeTower", "tower"),
                        ("SceneEnterShop", "shop"), ("SceneEnterFriend", "friend")):
        assert run.run(entry, page) == SUCCEEDED, (entry, run.completed)
        assert not run.actions, (entry, run.actions)
        assert run.page == page
        count += 1
    print("[PASS] 9个纯导航入口已在目标页时不点击、不执行业务")

    for entry, parent, target in (("SceneEnterOutpost", "hall", "outpost"),
                                   ("SceneEnterArena", "ark", "arena"),
                                   ("SceneEnterInterception", "ark", "interception"),
                                   ("SceneEnterSimulationRoom", "ark", "simulation"),
                                   ("SceneEnterTribeTower", "ark", "tower"),
                                   ("SceneEnterShop", "hall", "shop"),
                                   ("SceneEnterFriend", "hall", "friend")):
        assert run.run(entry, parent) == SUCCEEDED, (entry, run.completed, run.actions)
        assert run.page == target and len(run.actions) == 1, (entry, run.actions)
        count += 1
    print("[PASS] 父页进入只导航，And引用没有执行原Enter业务后续")

    assert run.run("SceneEnterArena", "unknown") == SUCCEEDED, (run.completed, run.actions)
    assert run.page == "arena"
    count += 1
    print("[PASS] 未知页面经大厅和方舟恢复目标")

    assert run.run("SceneEnterInterception", "ark", missing={"ArkEnterInterception"}) == FAILED
    assert not run.actions and run.recognition_count < 50000
    count += 1
    print("[PASS] 父页目标入口缺失有界失败，没有反复返回父页空转")

    for entry, parent, click, limit in (
        ("SceneEnterArk", "hall", "__ScenePrivateClickArk", 3),
        ("SceneEnterOutpost", "hall", "__ScenePrivateClickOutpost", 6),
        ("SceneEnterArena", "ark", "__SceneArkClickArena", 3),
        ("SceneEnterInterception", "ark", "__SceneArkClickInterception", 3),
        ("SceneEnterSimulationRoom", "ark", "__SceneArkClickSimulationRoom", 3),
        ("SceneEnterTribeTower", "ark", "__SceneArkClickTribeTower", 3),
    ):
        assert run.run(entry, parent, frozen=True) == FAILED, (entry, run.completed)
        assert run.page == parent and run.actions.count(click) == limit, (entry, run.actions)
        if entry == "SceneEnterInterception":
            assert run.actions == [click] * 3 + ["CommonClickBlank"] * 3, run.actions
        else:
            assert run.actions == [click] * limit, (entry, run.actions)
        count += 1
    print("[PASS] 画面不推进时入口点击有界：方舟/方舟模块3次，前哨基地6次，最终失败")
    print("[PASS] 拦截战入场点击无效时仅3次入口点击+3次空白点击，不耗尽公共预算")

    # Stop at business boundaries. The real Main and scene navigation are still
    # executed, while no game business beyond the boundary is modeled here.
    assert run.run("InterceptionMain", "hall", extra={"InterceptionBattleFlow": {"next": []}}) == SUCCEEDED
    assert run.actions.count("InterceptionMain") == 1, run.actions
    assert run.page == "interception" and "InterceptionBattleFlow" in run.completed
    count += 1
    print("[PASS] Main跨场景恢复后额度动作只执行一次")

    disabled = option_overrides("Arena.json", ArenaEnterRookieArena="No", ArenaEnterSpecialArena="No",
                                ArenaEnterChampionArena="No")
    assert run.run("ArenaMain", "hall", reward=True, extra=disabled) == SUCCEEDED, (run.completed, run.actions)
    assert run.actions.count("ArenaMain") == 1
    reward = run.actions.index("ArenaClaimSpecialReward")
    enter = run.actions.index("__SceneArkClickArena")
    assert reward < enter, run.actions
    assert not any(n in run.completed for n in disabled), run.completed
    assert run.page == "arena"
    count += 1
    print("[PASS] Arena从大厅先到方舟领奖再进入竞技场，可选关闭保持关闭")

    advise_disabled = option_overrides("Advise.json", NikkeAdvise="No")
    for page in ("nikke", "advise"):
        assert run.run("AdviseMain", page, extra=advise_disabled) == SUCCEEDED, run.completed
        assert run.page == page and run.actions == ["AdviseMain"], run.actions
        assert "AdviseEnterAdvisePage" not in run.completed
        assert "AdviseMainPageFlow" not in run.completed
        count += 1
    advise_enabled = option_overrides("Advise.json", NikkeAdvise="Yes", AdviseEpisodeViewingReward="No")
    for page in ("nikke", "advise"):
        assert run.run("AdviseMain", page, extra=advise_enabled) == SUCCEEDED, run.completed
        assert run.page == "advise" and run.actions.count("AdviseMain") == 1
        assert "AdviseMainPageFlow" in run.completed and "AdviseChooseNikke" not in run.completed
        assert set(run.actions) <= {"AdviseMain", "AdviseClickAdvisePage"}, run.actions
        count += 1
    print("[PASS] 咨询关闭时在妮姬/咨询页就地结束；开启但没有对象时正常结束")

    cash_boundary = {"CashShopCornerFlow": {"next": []}}
    assert run.run("CashShopResumeFlow", "shop", extra=cash_boundary) == FAILED
    assert "CashShopCornerFlow" not in run.completed and not run.actions
    count += 1
    assert run.run("CashShopResumeFlow", "cashshop", extra=cash_boundary) == SUCCEEDED
    assert "CashShopCornerFlow" in run.completed and not run.actions
    count += 1
    print("[PASS] 付费商店复用同时要求标题与普通礼包，百货标题单独命中不放行")

    outpost_disabled = option_overrides("Outpost.json", DefenseRewards="No", BulletinBoard="No", BriefEncounter="No")
    # These leaf probes would match if the declared options did not disable the
    # corresponding task entries; callback failure would expose a leaked action.
    available = {"OutpostEnterDefenseRewards", "OutpostEnterBulletinBoard",
                 "OutpostDailyLivesAvailable", "OutpostStaminaAvailable"}
    for page in ("outpost", "hall"):
        assert run.run("OutpostMain", page, extra=outpost_disabled, visible=available) == SUCCEEDED
        assert run.page == "outpost" and run.actions.count("OutpostMain") == 1
        assert not any(node in run.completed for node in outpost_disabled), run.completed
        assert set(run.actions) <= {"OutpostMain", "__ScenePrivateClickOutpost"}, run.actions
        count += 1
    print("[PASS] 前哨基地子任务全部关闭时不泄漏奖励、派遣或突发剧情动作")

    for entry, page, boundary in (
        ("AccountNurturingSynchroDeviceEnhance", "outpost", None),
        ("AccountNurturingRecycleRoomUpgrade", "outpost", None),
        ("AccountNurturingSynchroDeviceEnhance", "synchro", "SynchroDeviceEnhanceFlow"),
        ("AccountNurturingRecycleRoomUpgrade", "recycle", "RecycleRoomUpgradeFlow"),
    ):
        extra = {boundary: {"next": []}} if boundary else None
        assert run.run(entry, page, extra=extra) == SUCCEEDED, (entry, run.completed)
        assert run.page == page and not run.actions, (entry, run.actions)
        assert "SceneEnterHall" not in run.completed and "SceneEnterOutpost" not in run.completed
        count += 1
    print("[PASS] 同步器与循环室养成从前哨基地/目标子页直接复用，不绕大厅")
    return count


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--resource", type=Path, default=ROOT / "assets" / "resource")
    parser.add_argument("--dll", type=Path, default=ROOT / "deps" / "bin" / "MaaFramework.dll")
    args = parser.parse_args()
    nodes = read_nodes(args.resource)
    required = {"SceneEnterHall", "SceneEnterArk", "SceneEnterOutpost", "SceneEnterArena",
                "SceneEnterInterception", "SceneEnterSimulationRoom", "SceneEnterTribeTower",
                "SceneEnterShop", "SceneEnterFriend"}
    assert not required - nodes.keys(), f"缺少场景入口：{sorted(required - nodes.keys())}"
    log_dir = Path(tempfile.mkdtemp(prefix="mda-scene-navigation-"))
    maa, cookie = load_api(args.dll, log_dir)
    print(f"MaaFramework {maa.MaaVersion().decode()}；日志：{log_dir}")
    resource, runner = None, None
    try:
        resource = maa.MaaResourceCreate()
        assert maa.MaaResourceWait(resource, maa.MaaResourcePostBundle(resource, str(args.resource).encode())) == SUCCEEDED
        runner = NavigationRun(maa, resource, nodes)
        count = check_behaviors(runner)
        print(f"汇总：{count}组真实框架行为验证通过；所有输入均由虚拟控制器截获。")
    finally:
        if runner:
            runner.close()
        if resource:
            maa.MaaResourceDestroy(resource)
        cookie.close()


if __name__ == "__main__":
    main()
