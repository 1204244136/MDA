# 活动接线

项目当前 `AGENTS.md` 和 `docs/event-theme-routing.md` 是配置架构的依据；本页负责把素材角色接到它们上面。

## 小活动

- 先判断主页是否真有 STORY I/II 切换。有则按 Story 语义命名，没有则用 `StageNormal` / `StageHard`。两批关卡编号不代表两个 Story。
- 普通关卡标记加入主题 case 的 `SmallEventClickStage.recognition.param.template`；扫荡标记加入 `Themes/{CaseName}.json` 中对应点击节点。
- 每个具体主题完整覆盖 `SmallEventEnterMainPage`、`SmallEventClickStage` 及 `SmallEventEventStageQuickBattleFlow.next`；路由末尾保留 `SmallEventGoBack`。模板与非零偏移留在主题点击节点，case 只切换扫荡路由。
- 相同点击策略可合并模板；Normal/Hard 或 Story 的偏移不同就拆点击节点。点击后复用 `SmallEventEventStageBattlePage` / `SmallEventGoBack`，保留现有顺序、等待和战斗/体力逻辑。
- 升级当前主题时更新 base 的入口、普通关卡模板及完整扫荡路由。`CurrentEvent` 与 `Other` 均无 override，保持自动跟随。只补当前主题 Hard 时同步该主题自己的节点/路由与 base，不动其他主题。
- Story 2 开放后页面自动切到 Story 2，无需照搬大活动的 Story 入口切换。未开放的 Story 2 Hard 不创建模板。

## 大活动

- 普通关卡/扫荡标记接入 `LargeEventClickStoryStage` / `LargeEventClickStoryStageRepeatable`；主题 case 还需覆盖 `LargeEventEnterMainPage`。base 模板代表当前主题。
- `LargeEventMiniGame` 是常驻顶层选项，放在 Story 与 Mission 之间，默认开启；`LargeEventMainPageFlow` 中保留 `[JumpBack]LargeEventMiniGame`，不要改整条主流程。
- base `LargeEventMiniGame.next` 指向当前主题小游戏并接 `CommonEndTask`，无小游戏时只接 `CommonEndTask`。全部具体主题与 `Other` 必须各自覆盖小游戏路由；`CurrentEvent` 无 override。
- 只有素材不能证明小游戏已经实现。既有小游戏可接线，新玩法交给相应开发流程，不填写猜测入口。
- 用户只提供 Story 1 时只适配已知模式，不为省略的 Story 2 改默认 `LargeEventStoryPriority`。主题专属动作参数如发生变化，核对未声明字段的继承，不能只换模板后默认安全。
- `LargeEventEnterMission` 为中性红点检测，不随主题改；`LargeEventMissionClaimed` 的颜色特例只由对应主题覆盖。

## 主题与语言

- 保留 `default_case: CurrentEvent` 和所有既有 case 标识。`CurrentEvent` 显示名是“当前活动”/`Current Event`；真实主题显示名用全大写，检查两个活动选项中的历史显示名风格。
- 只同步项目已有语言，当前主要为 `zh_cn`、`en_us`。新增 locale 键按项目约定追加；不重排其他任务。
- 一般适配不需要用户重选。只有本次删除/重命名旧主题 case 时，才提示对应用户重新选当前开放主题或“当前活动”。
- 模板来自用户原图的裁剪/缩放，保留原件在仓库外。素材、Pipeline、task 和 locale 作为同一资源版本发布。
