# EquipmentReroll 文档索引

本目录集中存放 MDA `EquipmentReroll` 任务相关的全部说明文档。

“加载先前的锁定设置”已全面接入与增强：本任务最近实际使用的单次锁设置与当前完整锁计划一致时，在确认页优先一键加载；支持在装备已存在常驻固定锁时一键恢复单次锁（自动识别为第 2 锁并按 30 密钥核验计费）；逐槽验证后更新快照，再读取总费用。计划变化、余额不足或按钮不可用时沿用逐槽选择。加载本身不扣库存。

新版锁定流程已适配全量锁定状态：SELECT 选好材料（自订密钥变红，订制模组变亮蓝）后通过蓝色确认按钮返回确认页；锁定只更新状态不扣费。确认页支持无锁（1模组）、单单次锁（2模组+20密钥）、双模组锁（8模组=3基础+5累计锁费）、混合锁（3卡片，5模组+20密钥）、既有固定锁（免收锁费，仅收3基础洗练模组）等全部边界组合的动态总额读取与原子扣除。结果页自订密钥单次锁解除闪烁时自动防误判。旧文档中“上锁立即扣费”的描述以此为准。

高风险点击均采用“页面哨兵 + 有界重试 + 显式失败”闭环：放弃锁定后按实际返回的确认页/详情页继续；效果变更确认未进入结果页、历史锁加载后无法验证锁位时，清理暂存状态、显示原因并让任务失败，不再静默结束或在未知状态下继续消耗。

| 文档                                                 | 内容                                                        |
| ---------------------------------------------------- | ----------------------------------------------------------- |
| [洗词条策略与Agent逻辑.md](洗词条策略与Agent逻辑.md) | 任务策略、锁定决策、全局前瞻、Pipeline/Go 实现说明          |
| [洗词条概率与期望计算.md](洗词条概率与期望计算.md)   | 槽位概率、效果权重、期望订制模块消耗模型                    |
| [装备系统与洗词条研究.md](装备系统与洗词条研究.md)   | 游戏内数值、UI 文案、成本材料等原始依据                     |
| [装备改造系统调整报告.md](装备改造系统调整报告.md)   | 两张公告截图的六项调整、规则边界与文字存档；2026-09-20 整理 |

## 代码中的引用

核心 Go 文件头部已添加文档索引注释：

- `agent/go-service/equipmentreroll/plan.go`
- `agent/go-service/equipmentreroll/plan_dp.go`
- `agent/go-service/equipmentreroll/reroll.go`
- `agent/go-service/equipmentreroll/lock.go`
- `agent/go-service/equipmentreroll/choose_part.go`
- `agent/go-service/equipmentreroll/single.go`（单件模式纯决策逻辑）
- `agent/go-service/equipmentreroll/single_action.go`（单件模式入口动作）
- `agent/go-service/equipmentreroll/carrier.go`（任务选项承载点解析，两种模式共用）

Agent 在阅读这些代码时，应优先查看本目录下的对应文档。

> 说明：`EquipmentReroll` 任务（入口 `EquipmentRerollMain`）下有两个**同级互斥的模式**（选项 `EquipmentRerollMode`）——
>
> 1. **角色模式**（`Character`，默认）：四件装备联合分配、全局有限步前瞻，选项为 9 个效果配额 select（`EquipmentRerollQuota<Effect>`：禁止/不要求/需求 1-4 条直选），见《洗词条策略与Agent逻辑.md》§1.2、§4、§8；
> 2. **单件模式**（`Single`）：只扫描并只洗用户选定的一件装备、支持限定词条落槽、需求数上限 3，选项 `EquipmentRerollSinglePart` + 三组“需求词条/槽位”直选（`EquipmentRerollSingleWant1/2/3` + `...Slot1/2/3`），见同文档 §9。
>    两种模式共享同一入口与同一条编排，消费计费均按高消耗 5x 处理；全部选项统一承载于 `EquipmentRerollLockNeed` 节点的 `attach` 顶层键，Go 组件经 `loadCarrierConfig` 读取，**模式判定只认 `attach.mode`**。
