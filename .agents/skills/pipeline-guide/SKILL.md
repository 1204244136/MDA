---
name: pipeline-guide
description: MaaFramework Pipeline JSON 编写指南。用于编写、修改或审查 Pipeline JSON，设计节点流程，使用 TemplateMatch/OCR/ColorMatch/Custom 识别或 Click/Swipe/Custom 动作，并检查 MaaFramework 项目的通用 Pipeline 可靠性。
---

# MaaFramework Pipeline 编写指南

## 核心原则

1. **状态驱动**：遵循“识别 → 操作 → 识别”循环。每次操作必须基于识别结果，禁止假设操作后画面状态。
2. **覆盖可能画面**：扩充 `next` 列表，覆盖操作后可能出现的页面、弹窗、加载态和异常态。
3. **少用硬延迟**：尽量不用 `pre_delay` / `post_delay` / `timeout` 解决稳定性问题，优先通过中间识别节点和 `pre_wait_freezes` / `post_wait_freezes` 等待画面稳定。
4. **720p 基准**：坐标、ROI、模板裁剪和截图判断统一以 1280×720 为基准，除非当前项目另有明确约定。
5. **格式一致**：遵循项目现有 JSON 格式化规则；常见 MaaFramework 项目使用 4 空格缩进和数组元素换行。

## 节点命名

- 节点名使用 PascalCase，推荐基础格式为 `<Domain><SemanticPart><Role>`。
- 不要在全项目无脑统一为“动宾”或“宾动”；先判断节点角色，再决定语序。
- 动作节点使用动宾：命中后会执行点击、选择、领取、购买、确认、关闭等动作时，使用 `<Domain><Verb><Object><OptionalRole>`，例如 `CommonConfirmReward`、`DailyTaskClaimMissionReward`、`ShopPurchaseGem`、`BattleClosePage`。
- 状态 / 对象 / 纯检测节点使用宾 + 状态或角色后缀：例如 `ShopGemVisible`、`BattleStartButtonBlocked`、`DailyRewardsRewardClaimed`；不要为了动宾写成 `VisibleGem`、`BlockedStartButton`。
- 按钮、弹窗、文本模板看语义：`ConfirmButton` 可以表示“确认按钮”这个对象；但执行“确认购买/确认奖励”动作的节点应命名为 `ConfirmBuy` / `ConfirmReward`。
- 入口节点常用 `<Domain>Main`；只编排流程、不直接识别或动作的节点常用 `<Domain><Subtask>Flow`，若流程目标本身是动作，也可用动宾目标，例如 `DailyRewardsClaimRewardFlow`。
- 页面或 UI 状态节点使用 `On<Page>Page`、`Visible`、`Available`、`Selected`、`Completed`、`Exhausted` 等能表达业务语义的后缀。
- 不要使用 `_Start`、`Node1`、`clickReward`、`shop_enter`、`FlagInX`、`RewardConfirm`、`RewardClaim`、`GemPurchase`、`PageClose` 这类不稳定、语序不一致或不表达职责的名称。

## Pipeline v2 推荐格式

```jsonc
{
    "MyNode": {
        "recognition": {
            "type": "TemplateMatch",
            "param": {
                "template": "MyTask/button.png",
                "roi": [100, 200, 300, 100],
                "threshold": 0.7,
            },
        },
        "action": {
            "type": "Click",
        },
        "next": ["NextNode"],
    },
}
```

## 常用识别算法

### TemplateMatch

```jsonc
"recognition": {
    "type": "TemplateMatch",
    "param": {
        "template": "path/to/image.png",
        "roi": [x, y, w, h],
        "threshold": 0.7
    }
}
```

- 模板路径通常相对项目 image/resource 图片目录，按当前项目约定为准。
- 尽量缩小 ROI，避免全屏匹配造成性能和误识别问题。
- `green_mask: true` 可遮蔽不参与匹配的区域。

### OCR

```jsonc
"recognition": {
    "type": "OCR",
    "param": {
        "roi": [x, y, w, h],
        "expected": ["完整文本"]
    }
}
```

- `expected` 优先写完整文本。
- 多语言项目中，确认当前项目是否有 i18n 工具自动处理 OCR 文案。
- 需要片段或正则时，按项目约定标注跳过 i18n。

### ColorMatch

```jsonc
"recognition": {
    "type": "ColorMatch",
    "param": {
        "roi": [x, y, w, h],
        "method": 40,
        "lower": [h_low, s_low, v_low],
        "upper": [h_high, s_high, v_high],
        "count": 100
    }
}
```

- 优先使用 HSV 或灰度空间，避免直接 RGB 匹配带来的设备差异。

### And / Or

```jsonc
"recognition": {
    "type": "And",
    "param": {
        "all_of": ["NodeA", "NodeB"],
        "box_index": 0
    }
}
```

```jsonc
"recognition": {
    "type": "Or",
    "param": {
        "any_of": ["NodeA", "NodeB"]
    }
}
```

- 只有组合条件会被多个节点复用，或确实需要组合识别时，才拆出独立子节点。
- 不要为了“看起来清楚”把单次使用的 `Visible` 节点过度拆分。

### Custom

```jsonc
"recognition": {
    "type": "Custom",
    "param": {
        "custom_recognition": "MyRecognition",
        "custom_recognition_param": {}
    }
}
```

- `custom_recognition` / `custom_action` 名称必须与 Agent 注册名一致。
- 参数结构应和 Go/C++/Python 扩展代码中的解析结构一致。

## 常用动作类型

| 动作                   | 用途           | 关键字段                               |
| ---------------------- | -------------- | -------------------------------------- |
| `Click`                | 点击           | `target`, `target_offset`              |
| `LongPress`            | 长按           | `target`, `duration`                   |
| `Swipe`                | 滑动           | `begin`, `end`, `duration`             |
| `Scroll`               | 滚轮           | `target`, `dx`, `dy`                   |
| `ClickKey`             | 按键           | `key`                                  |
| `InputText`            | 输入文本       | `input_text`                           |
| `StartApp` / `StopApp` | 启停应用       | `package`                              |
| `StopTask`             | 停止当前任务链 | 无                                     |
| `Custom`               | 自定义动作     | `custom_action`, `custom_action_param` |
| `DoNothing`            | 不执行动作     | 无                                     |

`target` 常见形式：`true`、节点名字符串、`[x, y]`、`[x, y, w, h]`。

## 流程控制

- `next`：按序识别，首个命中节点执行其 action 后成为当前节点；全部超时或为空则流程结束。
- `on_error`：识别超时或动作失败后的兜底节点。
- `[JumpBack]`：执行完子链后返回父节点继续识别，适合弹窗、加载、奖励确认等中断处理。
- `[Anchor]`：动态引用锚点，按 MaaFramework 当前协议使用。
- `max_hit`：限制节点最大命中次数，适合避免重复领取或循环点击。

## 典型模式

### 带弹窗处理的任务入口

```jsonc
{
    "MyTaskMain": {
        "next": [
            "MyTaskStep",
            "[JumpBack]CommonConfirmDialog",
            "[JumpBack]CommonWaitLoadingExit",
            "[JumpBack]NavigationReturnHome",
        ],
    },
}
```

### 点击后验证画面变化

```jsonc
{
    "ClickConfirm": {
        "recognition": {"type": "TemplateMatch", "param": {"template": "confirm.png", "roi": [100, 100, 200, 80]}},
        "action": {"type": "Click"},
        "post_wait_freezes": {"time": 200, "target": [0, 0, 0, 0]},
        "next": ["VerifyNextScreen", "[JumpBack]ClickConfirm"],
    },
}
```

## 审查清单

- [ ] 字段名、类型、枚举值符合 Pipeline 协议或项目 schema。
- [ ] `next` / `on_error` / `sub` / `interrupt` 引用的节点都存在。
- [ ] 每次点击后有识别验证，不假设操作后状态。
- [ ] ROI / target 坐标基于 1280×720 或项目约定分辨率。
- [ ] 没有不必要的硬延迟；必须等待时优先使用 freeze wait。
- [ ] OCR 文本和 locale/i18n 规则符合当前项目约定。
- [ ] Custom 节点注册名和参数结构与 Agent 代码一致。
- [ ] 未过度拆分只使用一次且无复用价值的中转检测节点。

## 参考

- 字段速查：[field-reference.md](field-reference.md)
- MaaFramework Pipeline Protocol：<https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md>
