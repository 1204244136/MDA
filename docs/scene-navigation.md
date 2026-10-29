# 场景导航维护

`assets/resource/pipeline/Navigation/` 声明可复用的页面导航。目标是保留当前已到达的页面，再选择已确认的入口；无法直接进入时才恢复最近父页面。它与业务流程分开，不负责领奖、购买、选择关卡、养成或战斗。

## 接口与调用

公开接口使用 `SceneEnter*`；点击和恢复守卫使用内部节点。接口候选顺序为：目标页纯识别哨兵、导航点击、带守卫的父场景恢复。纯哨兵没有业务 `next`；需要复用带动作/业务后续节点的识别时，用 `And` 引用其识别结果，显式选择 `box_index`，只执行导航点击，不接入原节点的业务后续。

任务入口保留原有额度检查，先尝试当前页的业务枢纽，再用 `[JumpBack]SceneEnter*` 导航。导航子链完成后返回入口重试业务，避免重复执行入口动作。例如：

```jsonc
"OutpostMain": {
    "action": {
        "type": "Custom",
        "param": { "custom_action": "RuntimeQuotaCheck" }
    },
    "next": [
        "OutpostTaskFlow",
        "[JumpBack]SceneEnterOutpost"
    ]
}
```

页面 `Entered` 哨兵只表示到达。不要把它以 `[JumpBack]` 放在业务循环的无条件兜底位置：静态画面会反复命中，既无动作也不结束。无待办时应使用普通终端哨兵或 `CommonEndTask`。

左上角的层级文字只表示当前层级，没有互动效果。它只能作为纯识别依据，不得给它附加点击动作或用作“点击返回父页”的目标。

## 当前覆盖

当前提供 9 个公开场景接口：

| 场景接口                   | 接受的目标页     | 父页面与边界                                             |
| -------------------------- | ---------------- | -------------------------------------------------------- |
| `SceneEnterHall`           | 大厅方舟入口可见 | 未在大厅时复用 `CommonReturnHall` 的登录、签到和弹窗恢复 |
| `SceneEnterArk`            | 方舟首页         | 点击大厅方舟入口；其他页面经大厅恢复                     |
| `SceneEnterArena`          | 竞技场首页       | 方舟；不领取累积奖励                                     |
| `SceneEnterInterception`   | 拦截战首页       | 方舟；保留原入场空白弹窗处理                             |
| `SceneEnterSimulationRoom` | 模拟室首页       | 方舟；仅在入场链处理 BIOS 更新确认                       |
| `SceneEnterTribeTower`     | 无限之塔选塔页   | 方舟；不选择塔或关卡                                     |
| `SceneEnterOutpost`        | 前哨基地首页     | 大厅；不执行前哨子任务                                   |
| `SceneEnterShop`           | 百货商店         | 大厅；不购买商品                                         |
| `SceneEnterFriend`         | 好友页           | 大厅；不删除好友、接受申请或领取友情点                   |

已经接入的任务入口包括竞技场、拦截战、模拟室、无限之塔、前哨基地、商店和好友管理。付费商店优先复用有普通礼包页签佐证的当前页，否则沿用大厅入场；没有单独的付费商店 Scene 接口。

咨询、开宝箱优先接受现有咨询/妮姬页和物品栏；角色突破、同步器、循环室、指挥中心、魔方强化优先尝试已有目标业务流程，再恢复可确认的父页面。账号养成汇总层只编排，具体子任务负责恢复入口。原选项 `enabled`、业务 `max_hit`、红点判断和无红点跳过继续生效。

`NikkeAdvise=No` 时，已在妮姬/咨询页以普通哨兵结束，不执行咨询及其花絮鉴赏会奖励链。这保持原控制路径；`AdviseEpisodeViewingReward` 虽是独立显示的选项，原来同样需要经过启用的咨询入口。

## 已知边界

- 妮姬和物品栏的大厅入口目前依赖红点/NEW 模板，缺少无红点普通入口素材，因此未建立 `SceneEnterNikke`、`SceneEnterAdvise` 或 `SceneEnterInventory`。不能用无条件场景重试代替原来的无红点跳过。
- 同步器、循环室、指挥中心等深页到前哨的逐层返回尚未确认；已有目标页可直接复用，其他画面仍可能恢复大厅。未声明深层 Scene 接口。
- 方舟各模块之间尚无专门的相邻返回路径。从竞技场等子页去另一个方舟模块，仍可能先经大厅再进入方舟；当前覆盖不等于跨模块直接跳转。
- 大小活动的“活动地区”“剧情”等通用标题不能验证当前页面是否属于用户选中的主题，因此保留原主题入口与原业务流程，暂不复用已有活动主页，也没有活动 Scene 接口。待具备可靠的主题识别依据后再评估。

## 重试边界

父恢复节点必须带反向识别守卫：已经在父页而目标入口缺失时，不再调用一个立即成功的父场景，避免空转；由当前导航节点等待超时并失败。点击节点使用有限 `max_hit`，点击后仍须识别目标页。

`max_hit` 在同一次 task 内累计，跨 `[JumpBack]` 调用不会重新计数。新增复用路径时需核对整个 task 的进入次数，不能把点击重试预算误当每次调用的独立预算；耗尽后应失败，不能循环恢复来绕过限制。

## 验证

从仓库根目录运行：

```powershell
python tools\verify\verify_scene_navigation.py
python tools\validate_schema.py --resource-dirs assets\resource --interface-files assets\interface.json --task-dirs assets\tasks
npm run test:theme
npm run check:theme
```

`verify_scene_navigation.py` 使用当前 `deps/bin/MaaFramework.dll` 和虚拟控制器，保留实际 `next`、JumpBack、组合识别、`inverse`、`enabled` 与 `max_hit`，以可控探针验证导航和业务边界。临时日志写入系统临时目录；可用 `--dll`、`--resource` 指定依赖和资源路径。它不验证 OCR/模板质量、点击落点、加载时序或实机稳定性。

实机验收仍需覆盖：已在目标页、从父页进入、从其他页面恢复、父页入口缺失、选项关闭、无红点跳过、活动主题缺失及多次调用预算。涉及模板与点击参数的新路径，应截图确认点击后到达预期页面。
