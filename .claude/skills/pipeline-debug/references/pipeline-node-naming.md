# Pipeline 节点命名规范

本文档用于规范 MaaFramework Pipeline 节点命名方式，提升节点可读性、可维护性与跨模块一致性。

Pipeline 节点名是 JSON 根对象中的 key，会被 `next`、`on_error`、`target`、`anchor`、`And` / `Or` 识别条件、`pipeline_override` 等字段引用。因此，节点名应保持稳定、明确，并能表达节点在流程中的职责。

## 总体原则

节点名必须使用 PascalCase。推荐基础格式为：

```text
<Domain><SemanticPart><Role>
```

- `Domain`：所属业务域、模块或共享域，例如 `Common`、`Navigation`、`Shop`、`Battle`。
- `SemanticPart`：节点处理的动作、页面、对象、状态或业务目标，例如 `EnterExchangePage`、`RewardDialog`、`QuickBattle`。
- `Role`：节点在流程中的功能角色；动作节点可省略独立后缀，例如 `Main`、`Flow`、`Visible`、`Available`、`Claimed`、`Button`。

推荐示例：

```text
ShopEnterExchangePage
ShopOnExchangePage
BattleQuickBattleAvailable
DailyTaskClaimMissionReward
CommonConfirmReward
DailyRewardsRewardVisible
```

### 语序原则

不要在全项目无脑统一为“动宾”或“宾动”；先判断节点角色，再决定语序：

1. **动作节点使用动宾**：节点命中后会执行点击、选择、领取、购买、确认、关闭等动作时，使用 `<Domain><Verb><Object><OptionalRole>`，例如 `CommonConfirmReward`、`DailyTaskClaimMissionReward`、`ShopPurchaseGem`、`BattleClosePage`。不要使用 `RewardConfirm`、`RewardClaim`、`GemPurchase`、`PageClose` 这类宾动写法表达动作。
2. **状态 / 对象 / 纯检测节点使用宾 + 状态或角色后缀**：节点只判断 UI、页面或业务状态时，使用 `<Domain><Object><StateRole>`，例如 `ShopGemVisible`、`BattleStartButtonBlocked`、`DailyRewardsRewardClaimed`。不要为了动宾写成 `VisibleGem`、`BlockedStartButton`。
3. **按钮、弹窗、文本模板看语义**：`ConfirmButton` 可以表示“确认按钮”这个对象；但如果节点执行“确认购买/确认奖励”动作，应命名为 `ConfirmBuy` / `ConfirmReward`。同理，`PurchaseDialogVisible` 是状态节点，`ConfirmPurchase` 是动作节点。
4. **流程节点按目标语义命名**：流程只编排不动作时使用 `<Domain><Subtask>Flow`；若流程目标本身是动作，也可使用动宾目标，例如 `DailyRewardsClaimRewardFlow`、`ShopPurchaseItemFlow`。

## 禁止事项

不要使用：

```text
_StartTask1
25Check
clickReward
shop_enter
Flag_In_Shop
Node1
Check2
```

规则：

1. 不以下划线开头。
2. 不以数字开头。
3. 不使用 snake_case、camelCase 或混合分隔符。
4. 不使用无业务语义的临时编号。
5. 不单独使用过泛名称，例如 `Confirm`、`Check`、`Click`。
6. 新节点不使用 `FlagInX` 表达页面状态；使用 `On...Page` 或 `Visible`。
7. 重命名节点时，不修改识别或动作参数，除非目标就是功能调整。

## Domain 命名

`Domain` 应表达节点所属业务域或共享域。

常见 Domain：

| Domain       | 用途                                             |
| ------------ | ------------------------------------------------ |
| `Common`     | 全局通用 UI 操作，例如确认、关闭、返回、空白点击 |
| `Navigation` | 跨模块导航、回到首页、进入主功能区               |
| `Login`      | 登录、启动、下载确认、登录奖励等启动流程         |
| `Shop`       | 商店相关流程                                     |
| `Battle`     | 战斗相关流程                                     |
| `DailyTask`  | 日常任务相关流程                                 |
| `Event`      | 活动相关流程                                     |

共享域名称应保持唯一和一致。若使用 `Common` 表达全局通用 UI 操作，就不要同时用 `Base`、`Global` 表达同一类节点。

业务域应稳定，不随识别方式变化。例如按钮从 OCR 改为模板匹配后，节点名不应从 `Visible` 改为 `Detected`。

## 节点角色命名

### 入口节点

```text
<Domain>Main
```

入口节点通常只负责组织后继节点，不直接承担具体识别或点击动作。

### 流程节点

只负责编排、不直接识别或点击的节点：

```text
<Domain><Subtask>Flow
```

示例：

```json
{
    "ShopPurchaseItemFlow": {
        "next": [
            "ShopPurchaseDialogVisible",
            "[JumpBack]CommonConfirmAction",
            "CommonConfirmReward"
        ]
    }
}
```

### 进入页面节点

用于点击入口并进入某个页面：

```text
<Domain>Enter<Page>
```

示例：

```text
ShopEnterExchangePage
BattleEnterStagePage
DailyTaskEnterMissionPage
EventEnterLoginRewardPage
```

不要省略 Domain，例如 `EnterShop` 会使节点在全局 Pipeline 中难以区分来源和职责。

### 页面状态节点

用于判断当前是否处于某页面、界面或弹窗：

```text
<Domain>On<Page>Page
<Domain><Object>Visible
```

页面状态应描述“处于哪个页面”或“哪个 UI 对象可见”，不要描述内部标记。

不要过度拆分页面状态节点。`Visible` 节点只有在被多个流程复用、作为 `And` / `Or` 子条件复用、作为进入成功哨兵节点，或需要被 `pipeline_override` 单独控制时，才有必要独立存在。

典型过度拆分：`Visible` 只被一个点击节点引用，且该点击节点的 `And.all_of` 只有这一个子条件。此时应将识别条件直接合并到点击节点中。

### 进入成功哨兵节点

点击入口后确认是否进入目标页面，统一使用：

```text
<Domain><Page>Entered
```

它通常不执行动作，也没有后继节点；命中表示进入流程完成。若未命中，则由后续节点继续重试进入动作或处理异常。

识别进入成功哨兵节点时，优先看控制流语义：

1. 是否位于进入/打开/点击节点的 `next` 成功分支中。
2. 后续是否存在当前进入节点自身，表示未进入时重试。
3. 是否作为成功分支流程的首个页面状态检查。
4. 命中后是否用于结束当前进入子流程，或进入后续业务流程。

`Entered` 只用于表达“某个进入动作已经成功完成”。普通页面状态继续用 `On...Page`；弹窗、按钮、红点、图标等 UI 对象用 `Visible`。

### 纯检测节点

只负责识别状态、不执行动作的节点，优先使用业务语义后缀：

```text
<Domain><Object>Visible
<Domain><Object>Available
<Domain><Object>Claimed
<Domain><Object>Selected
<Domain><Object>Completed
<Domain><Object>Exhausted
<Domain><Object>Detected
```

| 后缀        | 含义                                                             |
| ----------- | ---------------------------------------------------------------- |
| `Visible`   | UI 元素、页面文字、按钮、红点、图标等在界面上可见                |
| `Available` | 功能、按钮、次数可用                                             |
| `Claimed`   | 奖励或任务已领取                                                 |
| `Selected`  | 选项已选中                                                       |
| `Completed` | 流程、任务、收集、阶段已完成                                     |
| `Exhausted` | 次数、资源、机会已耗尽                                           |
| `Detected`  | 非 UI 的异常、状态、算法信号被检测到；仅在其他后缀都不准确时使用 |

### 点击 / 选择 / 领取节点

执行动作的节点使用动词前置：

```text
<Domain>Click<Object>
<Domain>Select<Object>
<Domain>Claim<Object>
<Domain>Purchase<Object>
<Domain>Open<Object>
<Domain>Close<Object>
```

不要使用 `ClickMax`、`PassClick` 这类缺少业务域或动词位置不稳定的名称。

### 确认节点

确认弹窗、确认奖励、确认操作使用：

```text
<Domain>Confirm<Object>
```

例如 `CommonConfirmReward`、`ShopConfirmPurchase`。

### 滚动 / 滑动节点

```text
<Domain>Scroll<Direction>
<Domain>Swipe<Object>
```

例如 `ShopScrollDown`、`BattleSwipeStageList`。

### 终止节点

```text
<Domain>End
<Domain>EndTask
```

如果节点的作用是停止当前任务链，应在 action 中明确表达终止语义。

## 重命名检查清单

- [ ] 所有 `next` / `on_error` / `interrupt` / `sub` 引用已同步。
- [ ] `target` 引用节点名已同步。
- [ ] `And.all_of` / `Or.any_of` 中的节点名已同步。
- [ ] `pipeline_override` 中的节点名已同步。
- [ ] 不改变识别参数、动作参数、ROI、模板、expected，除非功能需求要求。
- [ ] 节点名仍符合当前项目业务域命名习惯。
