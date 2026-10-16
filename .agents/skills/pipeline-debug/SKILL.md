---
name: pipeline-debug
description: 调试和优化 MaaFramework Pipeline JSON。用于对照 schema 验证配置，检测缺失引用、循环依赖、孤立节点、命名与行为不匹配、ROI/阈值问题，并给出可靠性、性能和可维护性改进建议。触发词包括 debug pipeline、validate pipeline、optimize pipeline、pipeline error。
license: MIT
metadata:
    compatibility: Designed for Claude Code
allowed-tools: Read Grep Glob
---

# MaaFramework Pipeline 调试

## 功能说明

- 对照 `tools/schema/pipeline.schema.json` 及相关 schema 验证 Pipeline JSON。
- 检测结构性问题：缺失引用、循环依赖、孤立节点、意外终止。
- 通过命名规范分析节点角色，从节点名推断其预期行为。
- 检测命名与代码不匹配，例如 `Click` 节点没有动作，`Visible` 节点却执行点击。
- 提供性能、可靠性和可维护性优化建议。
- 必要时生成修正后的 Pipeline 片段，可以增删节点以实现正确行为。

## 使用场景

- Pipeline JSON 运行不符合预期。
- 部署前验证 Pipeline 配置。
- 排查节点跳转、识别失败、重复点击、误点、卡死。
- 优化 ROI、阈值、流程结构或异常处理。
- 根据节点名理解流程语义，检查命名是否与实际识别/动作一致。

## 工作流程

### 1. 读取规范文件

优先读取当前项目中的 schema：

1. `tools/schema/pipeline.schema.json`
2. `tools/schema/interface_import.schema.json`
3. `tools/schema/interface.schema.json`
4. `tools/schema/custom.recognition.schema.json`
5. `tools/schema/custom.action.schema.json`

如果项目缺少某个 schema，说明缺失项并继续基于 MaaFramework 通用协议分析。

### 2. 分析 Pipeline 片段

1. 解析 JSON，列出所有节点。
2. 提取关键字段：`recognition`、`action`、`enabled`、`next`、`interrupt`、`sub`、`on_error`、`timeout`、`max_hit`。
3. 构建节点图：父节点到子节点、反向引用、入口节点、终止节点。
4. 区分 v1 简写字段和 v2 `{type, param}` 字段。

### 3. 对照 schema 验证

逐节点检查：

- 必需字段是否齐全。
- 字段类型是否匹配。
- 枚举值、数值范围、字符串模式是否合法。
- 识别类型及参数是否正确。
- 动作类型及参数是否正确。
- Custom 节点名和参数是否可能与 agent 注册不一致。

### 4. 通过命名分析节点语义

使用 [Pipeline 节点命名规范](references/pipeline-node-naming.md) 理解节点角色：

- `<Domain>Main`：入口节点。
- `<Domain><Subtask>Flow`：流程编排节点。
- `<Domain>Enter<Page>`：进入页面或功能。
- `<Domain>On<Page>Page` / `<Domain><Object>Visible`：页面或 UI 状态检测。
- `<Domain>Click<Object>` / `Select` / `Claim` / `Purchase`：动作节点。
- `<Domain>Confirm<Object>`：确认弹窗或确认操作。
- `<Domain><Page>Entered`：进入成功哨兵节点。
- `<Domain>End` / `EndTask`：终止节点。

重点检查命名与行为不匹配：

- `Click<Object>` 但没有 `action: Click` 或等价动作。
- `Visible` / `On<Page>Page` 却执行点击。
- `Flow` 节点包含具体识别或动作。
- `Enter<Page>` 点击后没有成功哨兵或重试/异常处理。
- `Detected` 暴露底层识别实现，而 `Visible` / `Available` / `Selected` 更符合业务语义。

### 5. 分析节点关系

- 所有 `next[]`、`interrupt[]`、`sub[]`、`on_error[]` 目标都必须存在。
- 识别循环是否有退出条件、`max_hit`、`timeout` 或明确终止节点。
- 找出无法从入口到达的孤立节点。
- 找出没有 `next` 且不像终止节点的死胡同。
- 检查 `[JumpBack]`、`[Anchor]` 等节点属性是否用于合适场景。

### 6. 识别常见问题

详见 [调试规则参考](references/debug-rules.md)。快速检查：

- 缺少识别或识别类型错误。
- TemplateMatch 缺模板、OCR 缺 expected、ColorMatch 缺 lower/upper。
- ROI 过大、阈值过低或过高。
- 点击后没有验证下一画面。
- 重复点击同一按钮，可能误点到下一页元素。
- `enabled` 默认值与 Project Interface 选项语义不一致。

### 7. 生成优化建议

建议按优先级输出：

1. 正确性问题：会导致执行失败、误点、卡死。
2. 可靠性问题：弹窗、加载、动画、网络波动下容易失败。
3. 性能问题：全屏识别、大模板、高频 OCR。
4. 可维护性问题：命名不清、过度拆分、重复节点、缺少 desc。

### 8. 提供修正方案

如发现问题，给出修正后的 JSON 片段，并说明每处改动原因。响应格式见 [输出格式](references/output-format.md)。

## 注意事项

- v1 简写和 v2 object 格式可能共存，应按项目现状判断，不要强行重写无关节点。
- 无 `next` 的节点可以是合法终止节点，但必须符合流程语义。
- `enabled` 默认为 true，只有显式 `false` 才默认关闭。
- `interrupt` / `sub` / `on_error` 的语义依项目使用习惯和 MaaFramework 版本而定，先查 schema 和现有模式。
- 模板路径通常相对 image/resource 图片目录，具体以当前项目约定为准。
- OCR `expected` 可能支持正则；是否自动 i18n 取决于当前项目工具链。
- 输出问题时优先给高置信度结论；不确定项标为“需要运行日志或截图验证”。

## 参考资料

- [Pipeline 节点命名规范](references/pipeline-node-naming.md)
- [调试规则](references/debug-rules.md)
- [常见模式](references/common-patterns.md)
- [输出格式](references/output-format.md)
- MaaFramework Pipeline Protocol：<https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md>

## 多模板调试结果

调试图可能分别展示各模板候选。单张模板低分或红框不代表整个节点失败，应查看最终 RecognitionDetail 的命中、box 和动作目标。同一节点的模板数组共享动作参数；偏移不同应分开识别/点击节点。被标注框覆盖的展示图不等同于框架原始截图，不用它的匹配分数反推原始识别失败。
