---
name: generate-pi-task
description: 根据 MaaFramework Pipeline 定义生成或更新 Project Interface 任务 JSON。用于创建、编辑或扩展 assets/tasks/ 下的任务文件，将 Pipeline 节点映射为用户可配置选项（switch/select/checkbox/input）并绑定 pipeline_override。
license: MIT
metadata:
    compatibility: Designed for Claude Code
---

# MaaFramework Project Interface 任务生成

## 功能说明

- 读取 Pipeline JSON 文件，识别可切换节点（通常是含 `enabled` 字段的节点）。
- 分析节点关系，确定选项类型：`switch`、`select`、`checkbox`、`input`。
- 生成或更新 `assets/tasks/*.json`，遵循 `tools/schema/interface_import.schema.json`。
- 更新 `assets/interface.json` 的 `import[]`。
- 补充 `assets/locales/interface/` 下项目实际支持语言的 i18n 键。

## 使用场景

- 为 Pipeline 创建新的 Project Interface 任务文件。
- 向已有任务文件添加选项。
- 把 Pipeline 的可配置节点暴露为用户可选配置。
- 用户提到“生成任务”、“PI 任务”、“pipeline 选项”、“interface 任务”、“pipeline_override”。

## 工作流程

### 1. 读取规范文件

从当前项目读取：

1. `tools/schema/interface_import.schema.json` — 任务文件 schema。
2. `tools/schema/interface.schema.json` — 主 PI schema。
3. `tools/schema/pipeline.schema.json` — Pipeline 节点 schema。

如果 schema 路径不同，先按当前项目实际结构查找，不要硬编码。

### 2. 读取 Interface 上下文

1. 读取 `assets/interface.json`。
2. 读取 `import[]` 中已有任务文件，收集已有任务名和 option key，避免冲突。
3. 记录 `group[]` 定义，确认新任务应归属哪个分组。
4. 检查 `assets/locales/interface/` 实际有哪些语言文件；只维护项目已经存在的语言。

### 3. 确定目标文件

1. 当前活动文件在 `assets/tasks/` 且已被 `interface.json` import → 编辑该文件。
2. 当前活动文件是 Pipeline JSON → 从 `assets/resource/pipeline/{PipelineName}/` 推导 pipeline 名称，检查 `assets/tasks/{PipelineName}.json` 是否存在。
3. 无上下文 → 询问用户要为哪个 pipeline 生成。
4. 新建任务文件时，同步在 `assets/interface.json` 的 `import[]` 中添加相对路径，例如 `tasks/MyTask.json`。

### 4. 读取 Pipeline 文件

1. 读取 `assets/resource/pipeline/{PipelineName}/` 下相关 `.json` 文件。
2. 提取每个节点的：节点名、`enabled`、`next[]`、`desc`、`recognition`、`action`。
3. 构建节点图：通过 `next[]` 建立父→子映射，识别兄弟节点和嵌套选项。
4. 如果 Pipeline 跨目录引用公共节点，只把当前任务应由用户配置的业务节点纳入选项。

### 5. 确定选项类型

详见 [选项类型决策参考](references/option-types.md)。快速规则：

- 单个含 `enabled` 的独立节点 → `switch`。
- 同一父节点下互斥的 `enabled` 子节点 → `select`。
- 多个可同时启用的独立项目 → `checkbox`。
- 选择某个 case 后才显示的子选项 → 嵌套 `case.option[]`。
- 可配置数值/文本参数 → `input`，配合 `pipeline_type` 和 `「FieldName」` 引用。

### 6. 生成任务 JSON

严格遵循 `interface_import.schema.json`。典型结构：

```json
{
    "task": [
        {
            "name": "PipelineName",
            "label": "$task.PipelineName.label",
            "entry": "PipelineName",
            "description": "$task.PipelineName.description",
            "option": ["OptionKey"],
            "group": ["daily"]
        }
    ],
    "option": {
        "OptionKey": {
            "type": "switch",
            "label": "$option.OptionKey.label",
            "cases": [
                {
                    "name": "Yes",
                    "pipeline_override": {
                        "TargetNode": {"enabled": true}
                    }
                },
                {
                    "name": "No",
                    "pipeline_override": {
                        "TargetNode": {"enabled": false}
                    }
                }
            ]
        }
    }
}
```

## Locale 规则

向 `assets/locales/interface/` 下项目实际存在的语言文件补充键：

- `task.{Name}.label`
- `task.{Name}.description`
- `option.{OptionKey}.label`
- `option.{OptionKey}.{CaseName}` 或项目 schema 要求的 case label key

原则：

- 用户可见字符串使用 `$` i18n key，不在任务文件中硬编码显示文案。
- 中文描述可优先参考 Pipeline 节点的 `desc`。
- 其它语言无法确定官方译名时，给出合理占位并在回复中请用户确认。
- 只维护当前项目已有 locale 文件，不要凭空新增语言文件，除非用户明确要求。

## 活动更新

更新活动主题、补 Hard/Story 模板、从截图裁剪或归档活动素材时，使用 [mda-event-update](../mda-event-update/SKILL.md)。本 skill 继续负责一般 PI 任务和选项生成，不维护活动适配步骤。

## 注意事项

- `interface_import.schema.json` 才是任务文件 schema，不是 `interface.schema.json`。
- `entry` 必须与 Pipeline 入口节点名完全一致，区分大小写。
- `interface.json` 中的 import 路径通常相对 `assets/interface.json`，例如 `tasks/MyTask.json`。
- `enabled: false` 表示节点默认关闭，选项通常让用户将其开启。
- `enabled: true` 表示节点默认开启，选项通常让用户将其关闭。
- `switch` 的 cases 通常为 `Yes` 和 `No` 两个。
- `select` 只应用选中的 case。完整 `next` 路由覆盖不需要额外启停其他主题；共享候选列表且依赖 `enabled` 互斥时才配置兄弟节点启停。
- `checkbox` 的每个 case 通常只启用自身节点，不禁用兄弟节点。
- 若项目已有同类任务采用不同顺序或 default_case，优先保持项目一致性。

## 验证清单

- [ ] JSON 语法合法。
- [ ] `task[].option[]` 的 key 都在 `option` 对象中存在。
- [ ] `case.option[]` 的嵌套引用都能解析。
- [ ] 所有 `pipeline_override` 目标节点在 Pipeline 文件中存在。
- [ ] 新任务文件已加入 `assets/interface.json` 的 `import[]`。
- [ ] 所有 `$...` locale key 在项目已有 interface locale 文件中存在。
- [ ] 选项默认值与 Pipeline `enabled` 默认状态一致。
