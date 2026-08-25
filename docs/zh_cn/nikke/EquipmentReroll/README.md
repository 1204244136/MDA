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

Agent 在阅读这些代码时，应优先查看本目录下的对应文档。
