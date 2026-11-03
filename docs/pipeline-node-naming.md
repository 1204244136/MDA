# Pipeline 节点命名规范

本文档用于规范 MaaFramework Pipeline 节点命名方式，提升节点可读性、可维护性与跨模块一致性。

Pipeline 节点名是 JSON 根对象中的 key，会被 `next`、`on_error`、`target`、`anchor`、`And` / `Or` 识别条件、`pipeline_override` 等字段引用。因此，节点名应保持稳定、明确，并能表达节点在流程中的职责。

节点语义说明（`desc`）与 `//` 注释的分工边界见 [Pipeline `desc` 与 `//` 注释规范](pipeline-desc-and-comments.md)：`desc` 只写一句话名片，解释性内容写进注释。

## 总体原则

节点名必须使用 **PascalCase**，推荐格式为：

```text
<Domain><ActionOrObject><Role>
```

各部分含义如下：

| 部分             | 含义                                       | 示例                                               |
| ---------------- | ------------------------------------------ | -------------------------------------------------- |
| `Domain`         | 所属业务域、模块或共享域                   | `Common`、`Navigation`、`Shop`、`Battle`           |
| `ActionOrObject` | 节点处理的动作、页面、对象、状态或业务目标 | `EnterExchangePage`、`RewardDialog`、`QuickBattle` |
| `Role`           | 节点在流程中的功能角色                     | `Main`、`Flow`、`Visible`、`Available`、`Confirm`  |

推荐示例：

```text
ShopEnterExchangePage
ShopOnExchangePage
BattleQuickBattleAvailable
DailyTaskClaimMissionReward
CommonConfirmReward
```

## 禁止事项

节点名不得使用以下形式：

```text
_StartTask1
25Check
clickReward
shop_enter
Flag_In_Shop
```

具体规则：

1. 不得以下划线 `_` 开头。
2. 不得以数字开头。
3. 不得使用 `snake_case`、`camelCase` 或混合分隔符。
4. 不得使用无业务语义的临时编号，例如 `Node1`、`Check2`。
5. 不得仅使用过于泛化的名称，例如 `Confirm`、`Check`、`Click`。
6. 不使用 `FlagInX` 作为新节点名；页面状态应使用 `On...Page` 或 `Visible` 表达。
7. 重命名节点时，不应修改与识别或动作逻辑相关的业务参数，例如 `expected`、`roi`、`template`、`target` 等，除非修改目标本身就是功能调整。

## Domain 命名

`Domain` 应表达节点所属的业务域或共享域。

常见 Domain 示例：

| Domain       | 用途                                             |
| ------------ | ------------------------------------------------ |
| `Common`     | 全局通用 UI 操作，例如确认、关闭、返回、空白点击 |
| `Navigation` | 跨模块导航、回到首页、进入主功能区等             |
| `Login`      | 登录、启动、下载确认、登录奖励等启动阶段流程     |
| `Shop`       | 商店相关流程                                     |
| `Battle`     | 战斗相关流程                                     |
| `DailyTask`  | 日常任务相关流程                                 |
| `Event`      | 活动相关流程                                     |

共享域名称应保持唯一和一致。若选择 `Common` 表达全局通用 UI 操作，就不应同时使用 `Base`、`Global` 表达同一类节点。

业务域应尽量稳定，不要随具体识别方式变化。例如按钮从 OCR 改为模板匹配后，节点名不应因此从 `Visible` 改为 `Detected`。

## 节点角色命名

### 入口节点

任务或模块入口节点使用：

```text
<Domain>Main
```

示例：

```text
ShopMain
BattleMain
DailyTaskMain
EventMain
```

入口节点通常只负责组织后继节点，不直接承担具体识别或点击动作。

### 流程节点

只负责组织后继节点、不直接点击的节点，使用：

```text
<Domain><Subtask>Flow
```

示例：

```text
ShopPurchaseItemFlow
BattleRepeatStageFlow
DailyTaskClaimRewardFlow
EventLoginRewardFlow
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

用于点击入口并进入某个页面的节点，使用：

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

不要省略 Domain：

```text
EnterShop
EnterBattle
EnterMission
```

省略 Domain 会使节点在全局 Pipeline 中难以区分来源和职责。

### 页面状态节点

用于判断当前是否处于某页面、界面或弹窗的节点，使用：

```text
<Domain>On<Page>Page
```

或：

```text
<Domain><Object>Visible
```

示例：

```text
ShopOnExchangePage
BattleOnStagePage
DailyTaskMissionPageVisible
CommonRewardDialogVisible
```

不要使用：

```text
FlagInShop
FlagInBattle
FlagInMission
```

页面状态应描述“处于哪个页面”或“哪个 UI 对象可见”，而不是描述内部标记。

不要过度解耦页面状态节点。`Visible` 节点只有在被多个流程复用、作为 `And` / `Or` 子条件复用、作为进入成功哨兵节点，或需要被 `pipeline_override` 单独控制时，才有必要独立存在。若页面状态只服务于单个进入页面节点，且不承担确认进入成功、失败重试或流程出口职责，应优先并入该进入页面节点，避免产生只被使用一次的中转检测节点。

典型的过度解耦：`Visible` 只被一个点击节点引用，且该点击节点的 `And.all_of` 只有这一个子条件。此时 `Visible` 节点没有复用价值，也没有组合识别价值，应将识别条件直接合并到点击节点中。

不推荐：

```json
{
    "EventRedDotVisible": {
        "desc": "活动红点",
        "recognition": {
            "type": "TemplateMatch"
        }
    },
    "EventClickRedDot": {
        "desc": "点击活动红点",
        "recognition": {
            "type": "And",
            "param": {
                "all_of": ["EventRedDotVisible"]
            }
        },
        "action": {
            "type": "Click"
        }
    }
}
```

推荐：

```json
{
    "EventClickRedDot": {
        "desc": "点击活动红点",
        "recognition": {
            "type": "TemplateMatch"
        },
        "action": {
            "type": "Click"
        }
    }
}
```

#### 进入成功哨兵节点

进入成功哨兵节点用于在点击入口后确认是否已经进入目标页面，统一使用 `Entered` 后缀：

```text
<Domain><Page>Entered
```

它通常不执行动作，也没有后继节点；当它在 `next` 中命中时，表示进入流程已经完成，当前子流程自然结束。若未命中，则由后续节点继续重试进入动作或处理异常。

典型写法：

```json
{
    "ShopEnterArenaShop": {
        "desc": "进入竞技场商店",
        "recognition": {
            "type": "TemplateMatch"
        },
        "action": {
            "type": "Click"
        },
        "next": [
            "ShopArenaShopEntered",
            "ShopEnterArenaShop"
        ]
    },
    "ShopArenaShopEntered": {
        "desc": "已进入竞技场商店",
        "recognition": {
            "type": "OCR"
        }
    }
}
```

上例中，`ShopArenaShopEntered` 是进入成功哨兵节点：如果识别到竞技场商店页面，进入流程结束；如果识别不到，则继续执行 `ShopEnterArenaShop` 重新尝试进入。

也可以通过流程节点间接确认进入成功：

```json
{
    "ShopEnterMainPage": {
        "desc": "进入商店主页",
        "recognition": {
            "type": "OCR"
        },
        "action": {
            "type": "Click"
        },
        "next": [
            "ShopMainPageFlow",
            "ShopEnterMainPage"
        ]
    },
    "ShopMainPageFlow": {
        "next": [
            "ShopMainPageEntered",
            "ShopPurchaseItemFlow"
        ]
    },
    "ShopMainPageEntered": {
        "desc": "已进入商店主页",
        "recognition": {
            "type": "OCR"
        }
    }
}
```

这种情况下，`ShopMainPageEntered` 虽然只被 `ShopMainPageFlow` 使用，但它承担的是进入成功确认职责，不应仅因引用次数少就判定为过度解耦。

识别进入成功哨兵节点时，应优先检查控制流语义，而不是只看引用次数：

1. 是否位于进入/打开/点击节点的 `next` 成功分支中。
2. 后续是否存在当前进入节点自身，表示未进入时重试。
3. 是否作为成功分支流程的首个页面状态检查。
4. 命中后是否用于结束当前进入子流程，或进入后续业务流程。

`Entered` 只用于表达“某个进入动作已经成功完成”。若节点用于可复用的普通页面状态判断，而不是进入动作的成功出口，则继续使用 `On...Page`，例如 `ShopOnArenaShopPage`、`NavigationOnHomePage`。若检测对象不是完整页面，而是弹窗、按钮、红点、图标等 UI 对象，则使用 `Visible`，例如 `CommonRewardDialogVisible`、`DailyTaskRedDotVisible`。

### 纯检测节点

只负责识别某个元素、状态、文本、红点，不执行动作的节点，优先使用能表达业务状态的后缀。

普通 UI 元素、页面文字、按钮、红点、图标等“界面上可见”的对象，默认使用 `Visible`，不要使用 `Detected` 暴露底层识别实现。

```text
<Domain><Object>Visible
<Domain><Object>Available
<Domain><Object>Claimed
<Domain><Object>Selected
<Domain><Object>Completed
<Domain><Object>Detected
<Domain><Object>Blocked
```

示例：

```text
DailyTaskRedDotVisible
ShopPurchaseButtonVisible
BattleQuickBattleAvailable
ShopItemSelected
DailyTaskMissionClaimed
BattleScreenFreezeDetected
```

根据语义选择后缀：

| 后缀        | 含义                                                                                                     |
| ----------- | -------------------------------------------------------------------------------------------------------- |
| `Visible`   | UI 元素、页面文字、按钮、红点、图标等在界面上可见；这是普通 UI 检测的默认后缀                            |
| `Available` | 功能、按钮、次数可用                                                                                     |
| `Claimed`   | 奖励或任务已领取                                                                                         |
| `Selected`  | 选项已选中                                                                                               |
| `Completed` | 流程、任务、收集、阶段已完成                                                                             |
| `Detected`  | 非 UI 的异常、状态、算法信号被检测到；仅在 `Visible` / `Available` / `Selected` 等业务后缀都不准确时使用 |
| `Blocked`   | 检测到特定内容后阻断当前流程，用于中断异常状态、跳过不可用功能或拦截不需要继续执行的分支                 |

### 点击/选择节点

执行点击、选择、领取等动作的节点，使用动词前置：

```text
<Domain>Click<Object>
<Domain>Select<Object>
<Domain>Claim<Object>
<Domain>Purchase<Object>
<Domain>Open<Object>
<Domain>Close<Object>
```

示例：

```text
CommonClickBlank
ShopPurchaseFreeItem
BattleSelectStage
DailyTaskClaimMissionReward
CommonClosePage
```

不要使用：

```text
ClickMax
PassClick
FreeRecruitClick
```

### 确认节点

确认弹窗、确认奖励、确认操作使用：

```text
<Domain>Confirm<Object>
```

示例：

```text
CommonConfirmAction
CommonConfirmReward
ShopConfirmPurchase
BattleConfirmRetreat
```

不要使用：

```text
Confirm
ActionConfirm
RewardConfirm
ConfirmEnd
```

### 滚动/滑动节点

滚动、滑动节点使用：

```text
<Domain>Scroll<Direction>
<Domain>Swipe<Object>
```

示例：

```text
CommonScrollUp
ShopScrollItemListDown
EventSwipeBanner
BattleSwipeStageListLeft
```

不要使用：

```text
ScrollUp
SlideBanner
ListSwipe
```

### 结束节点

结束节点应明确表达结束范围：

```text
<Domain>End
<Domain>EndTask
```

示例：

```text
ShopEnd
BattleEnd
CommonEndTask
```

如果只需要一个全局终止节点，推荐使用 `CommonEndTask`。

## 共享节点命名

共享节点是跨模块复用的节点，应使用稳定的共享 Domain 前缀，避免和业务模块节点混淆。

推荐使用 `Common` 表示通用 UI 操作：

```text
CommonConfirmReward
CommonConfirmAction
CommonClosePage
CommonClickBlank
CommonGoBack
CommonScrollUp
CommonEndTask
```

推荐使用 `Navigation` 表示跨模块导航：

```text
NavigationEnterHome
NavigationEnterMainArea
NavigationClickHomeButton
NavigationOnHomePage
NavigationMainAreaVisible
```

推荐使用 `Login` 表示登录或启动阶段流程：

```text
LoginConfirmServer
LoginConfirmDownload
LoginConfirmLogin
LoginReward
LoginRewardClaimed
LoginRewardClose
```

共享域名称应保持唯一和一致。不要同时混用 `Base`、`Common`、`Global` 表达同一类通用节点。

## 重命名检查清单

每次重命名节点后，必须检查以下引用位置：

1. 节点定义 key。
2. `next`。
3. `on_error`。
4. `[JumpBack]NodeName`。
5. `[Anchor]AnchorName` 相关节点引用。
6. `target` 中的节点名引用。
7. `anchor` 对象中的节点名引用。
8. `recognition.type = "And"` 中的 `all_of`。
9. `recognition.type = "Or"` 中的 `any_of`。
10. `pipeline_override`。
11. Project Interface 或任务配置中的任务入口与 Pipeline 覆盖配置。
12. 构建产物、安装资源、镜像资源或重复资源包中的 Pipeline 文件。

## 命名示例

### 推荐

```json
{
    "ShopMain": {
        "next": [
            "ShopEnterExchangePage",
            "[JumpBack]NavigationEnterHome"
        ]
    },
    "ShopEnterExchangePage": {
        "desc": "进入兑换商店页面",
        "recognition": {
            "type": "OCR"
        },
        "action": {
            "type": "Click"
        },
        "next": [
            "ShopOnExchangePage",
            "ShopEnterExchangePage"
        ]
    },
    "ShopOnExchangePage": {
        "desc": "处于兑换商店页面",
        "recognition": {
            "type": "OCR"
        },
        "next": [
            "[JumpBack]ShopPurchaseItemFlow",
            "CommonEndTask"
        ]
    }
}
```

### 不推荐

```json
{
    "Shop": {},
    "EnterShop": {},
    "FlagInShop": {},
    "_Shop1": {},
    "25Check": {},
    "Confirm": {}
}
```

## 总结

Pipeline 节点命名统一采用：

```text
PascalCase + 模块域 + 功能语义 + 角色后缀
```

节点名应优先表达“节点在流程中的功能”，而不是表达底层识别或动作实现。

推荐风格：

```text
ShopEnterExchangePage
ShopOnExchangePage
BattleQuickBattleAvailable
DailyTaskMissionClaimed
CommonConfirmReward
```

避免风格：

```text
FlagInShop
ClickMax
25Check
_Shop1
Confirm
```
