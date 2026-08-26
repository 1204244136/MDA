# EquipmentReroll 文档索引

本目录集中存放 MDA `EquipmentReroll` 任务相关的全部说明文档。

| 文档                                                 | 内容                                               |
| ---------------------------------------------------- | -------------------------------------------------- |
| [洗词条策略与Agent逻辑.md](洗词条策略与Agent逻辑.md) | 任务策略、锁定决策、全局前瞻、Pipeline/Go 实现说明 |
| [洗词条概率与期望计算.md](洗词条概率与期望计算.md)   | 槽位概率、效果权重、期望订制模块消耗模型           |
| [装备系统与洗词条研究.md](装备系统与洗词条研究.md)   | 游戏内数值、UI 文案、成本材料等原始依据            |

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
