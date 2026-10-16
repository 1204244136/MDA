# 活动主题路由与点击参数隔离

## 当前约定

`LargeEventTheme` / `SmallEventTheme` 的默认值均为 `CurrentEvent`，该 case 不携带 `pipeline_override`。资源侧 base 的模板和路由代表最新主题，用户选择一次后即可随资源更新跟随；具体主题 case 名保持稳定，旧配置仍能显式回选。

小活动的 `Other` 维持无 override 的历史行为，也跟随 base；它不是一个独立主题。

### 小活动：共用业务流程，主题拥有识别与点击

- `SmallEventEnterMainPage`、`SmallEventClickStage` 的 base 模板承载当前主题，具体主题 case 完整覆盖这两个模板节点。
- `SmallEventEventStageQuickBattleFlow` 是无识别、无点击动作的扫荡路由。base 的 `next` 指向当前主题点击节点，最后一个候选为 `SmallEventGoBack`。具体主题 case 显式覆盖完整 `next`。
- `assets/resource/pipeline/Event/SmallEvent/Themes/{CaseName}.json` 保存各主题扫荡模板与动作；节点名为 `SmallEvent{CaseName}ClickStage...Repeatable`。case 只切换路由，不覆盖主题点击节点的参数。
- 点击策略相同的模板可放在同一节点。不同难度或 Story 的落点补偿不同，就拆识别/点击节点；共用战斗、体力、返回流程继续复用，不复制业务状态机。
- 点击节点沿用 `Vertical`、`index: -1`、`post_wait_freezes: 500`，后续为 `SmallEventEventStageBattlePage` / `SmallEventGoBack`。不恢复历史已移除的点击 `max_hit: 1` 和 `[JumpBack]` 流程。
- `SmallEventEnterMainPage` / `LargeEventEnterMainPage` 的 `[30, 0, 0, 0]` 属通用入口调参，保留在 base。主题专属扫荡偏移只在该主题点击节点中出现。

### LITTLE WITCH TRICK 的 Normal / Hard

| 节点                                                   | 模板特征                                    | target_offset      |
| ------------------------------------------------------ | ------------------------------------------- | ------------------ |
| `SmallEventLittleWitchTrickClickStageNormalRepeatable` | Normal 扫荡标识位于关卡右下侧，保留既有修复 | `[-80, -30, 0, 0]` |
| `SmallEventLittleWitchTrickClickStageHardRepeatable`   | Hard 糖果位于南瓜关卡右上侧，移向左下主体   | `[-40, 45, 0, 0]`  |

Hard 截图的命中框 `[596, 333, 24, 23]` 应用旧 Normal 偏移后为 `[516, 303, 24, 23]`，中心 `(528, 314.5)` 在空白区域；新偏移得到 `[556, 378, 24, 23]`，中心 `(568, 389.5)` 位于 1-11 南瓜主体内。框架会在目标矩形内取点击点，因此应验证整个偏移后的区域，而非只计算中心。

该偏移来自用户提供的 Hard 截图；离线识别与控制器回放只能验证路由、匹配、计算落点，真实点击后是否进入详情页仍须实机验收。Normal 保留原值，未从 Hard 截图反推 Normal 参数。

### 大活动保持原有约定

大活动的三个 base 模板节点和 `LargeEventMiniGame` 承载当前主题；所有具体主题完整覆盖模板，除 `CurrentEvent` 外的全部 case（含 `Other`）完整覆盖小游戏 `next`，无小游戏时接 `CommonEndTask`。

`LargeEventEnterMission` 是通用红点识别；`LargeEventMissionClaimed` 的颜色特例仍只由相应主题覆盖。小活动本次路由调整不改这些行为。

## 为什么这样分层

2026-06-18 的 `2d986aa` 合并 Normal / Hard 点击节点，减少了重复流程；在动作相同的前提下成立。2026-09-10 的 `f18087a` 让 base 承载当前主题，消除了 `CurrentEvent` 与最新主题 case 之间重复书写 override 的问题。

2026-10-13 的 `fb511cf` 给 LITTLE WITCH TRICK Normal 加偏移，同时让当时已有的其他主题显式归零，解决了跨已有主题继承。但偏移仍在共用节点上。2026-10-15 的 `9af6ce0` 为同一节点追加 Hard 模板，Hard 因而共用了 Normal 的动作参数。

旧护栏只检查字段是否声明，无法证明偏移适用于哪个模板；也不能自动防止下次换 base 模板时遗留旧偏移。现在通过主题独立节点和完整路由替换保存模板与动作的归属，避免要求每个新主题记住清除旧主题的动作参数。

## 框架依据

核对本地 MaaFramework 源码修订 `35d38dc3c9153c29099d1b7a0fcfb7b4b001aef5`，并用项目 `deps/bin/MaaFramework.dll` 做运行时验证：

- `Resource/ResourceMgr.cpp` 的 `override_pipeline` 进入 `PipelineResource` 的解析与覆盖流程。
- `Resource/PipelineParser.cpp` 的 `parse_node` 分别用既有值解析 recognition、action、next；省略的参数会继承。换模板不会自动重置动作偏移。
- 显式提供 `template` 或 `next` 数组会替换该字段，而不是追加；因此主题可以用完整 `next` 切换到自己的节点集合。
- `Task/Component/ActionHelper.cpp` 将 `target_offset` 加到识别目标矩形的 x、y、width、height，再执行点击。
- `Or` 引用的是子节点识别参数，不会分派执行各子节点的 action；要使用不同偏移，应让不同识别/点击节点成为 `next` 候选。

版本化原始协议：[MaaFramework v5.14.3 Pipeline](https://github.com/MaaXYZ/MaaFramework/blob/8963191215ca49d6b037802acd07d20a4213160a/docs/zh_cn/3.1-%E4%BB%BB%E5%8A%A1%E6%B5%81%E6%B0%B4%E7%BA%BF%E5%8D%8F%E8%AE%AE.md)。

`verify_override.py`、`verify_base_as_latest.py` 仍保留用于验证上述底层语义；后者中的“只换模板会继承偏移”是风险示例，不再是小活动生产配置的推荐结构。

## 适配新小活动

1. 确认活动是否存在 Story 切换，按真实难度/Story 命名模板；没有 Story 的活动不用 Story1/Story2 前缀。
2. 在 `Themes/{CaseName}.json` 新增主题点击节点，放入扫荡模板和该节点自己的动作参数；偏移不同则拆节点。节点必须预先声明在资源里。
3. 新增具体主题 case：覆盖入口模板、普通关卡模板、完整扫荡路由。保留原有主题 case 和节点。
4. 更新 base 的两个模板和完整扫荡路由指向新主题，保持 `CurrentEvent` 与 `Other` 无 override。
5. 运行护栏、schema/资源检查和相关离线回放，再验证实际点击后的页面。

主题 case 的名字是用户配置契约，不为显示名调整而重命名。只有删除或重命名了旧 case，才提示受影响用户重新选择；单纯新增主题和切换 base 不需要用户重选。

历史已确认的两点继续保留：修改 `default_case` 只影响未配置用户，不会改写旧配置中的具体 case 名；用 `input` 保存“最新主题”会得到用户数据快照，无法表达每次更新重新解析当前主题，因此不采用隐藏 input 或手动重写用户配置的方案。模板、路由与 task override 应同版本发布；若以后拆包更新，需重新验证兼容性。

## 自动校验

- A：`CurrentEvent` 存在、是 default_case、无非空 override。
- B：各具体主题完整覆盖要求的共用模板节点。小活动为两个，大活动为三个。
- C：大活动小游戏路由对所有非 CurrentEvent case 全量覆盖。
- D：小活动具体主题完整覆盖扫荡路由；base 路由与当前入口模板属于同一主题；主题节点及模板归属正确；case 不覆盖主题点击节点；路由无点击动作；节点接回公共后续；非零偏移的 Normal/Hard 混合模板报错。

护栏按 `Themes/{CaseName}.json` 自动发现小活动主题，无需为每次新主题维护偏移登记名单。它检查结构与归属，不能代替游戏截图和实机落点验证。

```powershell
npm run test:theme
npm run check:theme
.venv\Scripts\python.exe tools\verify\verify_small_event_routing.py
.venv\Scripts\python.exe tools\verify\verify_base_as_latest.py
```

`test:theme` 使用临时目录执行真实护栏，覆盖跨主题路由、换入口忘换路由、Normal/Hard 混用偏移等失败场景，以及新增未来主题而保留旧主题参数的成功场景。CI 同时运行测试和护栏。

`verify_small_event_routing.py` 加载项目实际资源，逐个应用全部主题 case，用真实 DLL 检查完整路由替换及所有主题节点参数不变，并模拟切换当前主题后回选旧主题；不连接或操作设备。

2026-10-16 本次验证：护栏回归 12/12、实际资源路由验证 8 组、原有框架语义验证 12/12，以及完整 schema / maa-tools 资源检查均通过。另用本地框架 Python binding、项目 DLL 与自定义模拟控制器回放 Hard 截图，命中 Hard 节点并记录点击 `(568, 391)`；未操作真实设备，未确认实机详情页跳转。
