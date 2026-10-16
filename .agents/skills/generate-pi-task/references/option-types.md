# Project Interface 选项类型决策参考

本文件用于从 MaaFramework Pipeline 节点关系推导 Project Interface 选项类型。

## 1. 识别可切换节点

扫描 Pipeline 节点中的 `enabled` 字段：

| `enabled` 值 | 含义               | 选项行为                                    |
| ------------ | ------------------ | ------------------------------------------- |
| `false`      | 节点默认关闭       | 选项让用户打开它                            |
| `true`       | 节点默认开启       | 选项让用户关闭它                            |
| 缺失         | 不是直接可切换节点 | 跳过，除非它是 select/checkbox 的路由父节点 |

含 `enabled` 的节点通常是 PI option 的候选，但不要机械地为每个节点生成独立选项；先分析父子和兄弟关系。

## 2. 构建兄弟节点组

从每个父节点的 `next[]` 中找同一父节点下的可切换子节点：

```text
Parent.next: ["OptionA", "OptionB", "EndTask"]
```

如果 `OptionA` 和 `OptionB` 都有 `enabled`，它们构成一个候选兄弟组。接下来判断它们是互斥还是可并行。

## 3. 判断选项类型

### Mutual Exclusion → `select`

适用信号：

- 多个节点代表替代路线，只应运行一个。
- 同一父节点在它们之间分支。
- 语义上是难度、目标、模式、关卡等单选项。

`pipeline_override` 模式：

```json
{
    "type": "select",
    "cases": [
        {
            "name": "OptionA",
            "pipeline_override": {
                "NodeA": {"enabled": true},
                "NodeB": {"enabled": false}
            }
        },
        {
            "name": "OptionB",
            "pipeline_override": {
                "NodeA": {"enabled": false},
                "NodeB": {"enabled": true}
            }
        }
    ],
    "default_case": "OptionA"
}
```

### Independent Items → `checkbox`

适用信号：

- 多个节点代表独立项目，可以同时启用。
- 常见于购买清单、领取清单、多个塔/副本入口、多个可选功能。
- 节点间没有“只能选择一个”的业务约束。

`pipeline_override` 模式：

```json
{
    "type": "checkbox",
    "default_case": [],
    "cases": [
        {
            "name": "ItemA",
            "pipeline_override": {
                "NodeA": {"enabled": true}
            }
        },
        {
            "name": "ItemB",
            "pipeline_override": {
                "NodeB": {"enabled": true}
            }
        }
    ]
}
```

checkbox case 通常只启用自身节点，不禁用兄弟节点。

### Single Toggle → `switch`

适用信号：

- 单个独立可切换节点。
- 语义是是否执行某一步、是否领取某奖励、是否启用某功能。

`pipeline_override` 模式：

```json
{
    "type": "switch",
    "cases": [
        {
            "name": "Yes",
            "pipeline_override": {
                "Node": {"enabled": true}
            }
        },
        {
            "name": "No",
            "pipeline_override": {
                "Node": {"enabled": false}
            }
        }
    ]
}
```

如果节点默认开启，通常给 `default_case: "Yes"`；如果节点默认关闭，按项目既有风格决定是否 `No` 在前。

### Nested Options → `case.option[]`

当某个 case 启用的节点后续还有子选项时，用嵌套选项：

```json
{
    "name": "Yes",
    "pipeline_override": {
        "NodeA": {"enabled": true}
    },
    "option": ["SubOptionName"]
}
```

随后在 `option` 对象中定义 `SubOptionName`。子选项类型仍按 `switch` / `select` / `checkbox` / `input` 判断。

### Input Options → `input`

当 Pipeline 参数需要用户填写数字、文本或枚举值时使用：

```json
{
    "type": "input",
    "inputs": [
        {
            "name": "Count",
            "pipeline_type": "int",
            "default": "5"
        }
    ],
    "pipeline_override": {
        "NodeName": {
            "custom_action_param": {
                "count": "「Count」"
            }
        }
    }
}
```

`「名称」` 表示运行时引用 input 字段的值。具体可写位置取决于项目 schema 和 MaaFramework 版本。

## 4. 判断默认值

- 如果 Pipeline 节点 `enabled: true` 且这是推荐默认行为，选项默认应保持开启。
- 如果 `enabled: false` 代表可选额外功能，默认一般保持关闭。
- `select` 的 `default_case` 应对应 Pipeline 默认会走的那一路。
- `checkbox` 的 `default_case` 数组只包含默认启用的项目。

## 5. 命名建议

- option key 使用稳定、可读、无空格的 PascalCase 或项目既有风格。
- case name 使用稳定英文标识，不直接写用户显示文案。
- 显示文案通过 locale key 维护，例如 `$option.MyOption.label`。
- 如果一个 option 只服务于某个 task，仍需避免与全局 option key 冲突。

## 6. 自检清单

- [ ] 选项类型反映真实业务关系，而不是只看节点数量。
- [ ] `select` case 互相禁用同组节点。
- [ ] `checkbox` case 不误禁用可并行兄弟节点。
- [ ] `switch` 只有两个清晰 case。
- [ ] 嵌套 option 只在对应 case 下可见。
- [ ] 所有 `pipeline_override` 节点名真实存在。
- [ ] 默认值与 Pipeline 的 `enabled` 初始状态一致。
