# Burst 文档索引

本目录集中存放 MDA "爆裂相关任务"的前期研究与后续说明文档。

| 文档                                                     | 内容                                                        |
| -------------------------------------------------------- | ----------------------------------------------------------- |
| [爆裂系统研究.md](爆裂系统研究.md)                       | 爆裂机制、冷却、配队规则、元素克制等原始依据                |
| [爆裂任务策略与Agent逻辑.md](爆裂任务策略与Agent逻辑.md) | 「自定义爆裂」任务：检测坐标/阈值、选项、路由逻辑、验证结论 |

## 状态

- 当前为**资料研究 + 任务实现阶段**：已完成对 NIKKE 爆裂系统的网络资料整理（机制、冷却档位、配队规则、元素克制）。
- **战斗 UI 部分核对**：已用 5 张 1280×720 实机截图（`C:\Users\12042\Desktop\sample`）测量确认右侧爆裂面板的阶段六边形颜色、槽位灰条与冷却变暗特征。
- **已实现**：`CustomBurst`（自定义爆裂）任务。**「快速爆裂」（FastBurst）是它的底层框架（检测引擎 + 快速释放逻辑，不可取消），不是并行的任务**——任务列表只有 `CustomBurst` 一个入口；用户在底层框架之上配置**多轮爆裂轴（顶部「使用轮数」下拉选 1-6，默认 1 轮；每轮 3 阶段各选一个角色：不指定/A/S/D）**，指定角色冷却时固定等待其冷却结束（严格按轴）。Go 依据选项按检测到的阶段跟轴路由释放动作并 focus 输出。检测层 5/5 截图验证通过，路由决策单测通过。
- **流程**：入口 `CustomBurstMain` 等待战斗画面（`BattleCombatHudVisible`，不出现则正常超时）→ 命中后进入 `CustomBurstLoop` 循环；循环末尾用 `CustomBurstCheckPause`（OCR「暂停」）与 `CustomBurstCheckSettle`（结算）判断，命中其一即停止任务、经 focus 输出对应提示（视为完成）。引擎按检测阶段跟轴（重置自动适配），**面板消失 = 本轮结束，推进下一轮**。

## 代码命名约定（FastBurst 框架 vs CustomBurst 任务）

- **CustomBurst（自定义爆裂）= 任务**，唯一入口 `CustomBurstMain`，任务列表只导出它。
- **FastBurst（快速爆裂）= 底层框架**，不是任务；它由 CustomBurst 内部使用，负责面板检测与快速释放。
- 代码中如何区分：
    - `FastBurst*`：底层框架的检测原语/汇总（`FastBurstResult`、`FastBurstPanelRecognition`、`FastBurstStage`、`FastBurstSlot*`、`FastBurstHex*` 等）与 ClickKey 原语（`FastBurstClickKey{A,S,D}`）。
    - `CustomBurst*`：任务层的流程节点与动作（`CustomBurstMain/Loop/WaitCharge/SafetyGate/CheckPause/CheckSettle/ReturnToLowFrequency`、`CustomBurstRouteAction`、`CustomBurstSafetyGateRecognition`、`CustomBurstReturnToLowFrequencyRecognition`）。
- Go 包：`agent/go-service/customburst`（承载整个 CustomBurst 特性，内部按上面前缀区分框架/任务）；日志 component 也分 `FastBurst`（检测）与 `CustomBurst`（任务）两档。

## 未来规划（待用户确认）

- 已按 `install/debug` 运行日志优化：阶段识别改为 Pipeline `Or` 汇总，按轴只检查目标槽位，发键改回单个 `ClickKey` 节点，并将循环与动作节点的外层 `rate_limit/pre_delay/post_delay` 设为 `0`，保留控制器内置点按间隔，避免过短按键和默认延迟拖慢爆裂链；
  Ⅰ/Ⅱ阶段色消失时会预发Ⅱ/Ⅲ阶段键（未指定轴时按已见冷却键推断候选），并以画面是否切换决定重发当前阶段还是进入下一阶段；仍需真机记录阶段Ⅰ→Ⅲ端到端耗时与预发命中率；
- 补充更多实机截图（尤其白发角色冷却、不同冷却时长）复核冷却阈值；
- 如需独立的「快速爆裂」裸任务（不指定角色、纯第一个就绪），可在 `CustomBurst` 上放开 `BurstStage*` 未配置时的兜底。
