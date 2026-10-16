# Pipeline 字段速查表

基于 MaaFramework Pipeline Protocol，供快速查阅字段名、类型和默认值。完整说明以当前项目的 `tools/schema/pipeline.schema.json` 与 MaaFramework 文档为准。

## 通用字段

| 字段                  | 类型                       | 默认值        | 说明                                                          |
| --------------------- | -------------------------- | ------------- | ------------------------------------------------------------- |
| `recognition`         | string \| object           | `"DirectHit"` | 识别算法；v2 常用 object `{type, param}`                      |
| `action`              | string \| object           | `"DoNothing"` | 动作类型；v2 常用 object `{type, param}`                      |
| `next`                | string \| NodeAttr \| list | `[]`          | 后续节点列表                                                  |
| `on_error`            | string \| NodeAttr \| list | `[]`          | 超时/失败后执行的节点                                         |
| `timeout`             | int                        | `20000`       | next 循环识别超时，单位 ms；`-1` 表示永不超时                 |
| `rate_limit`          | uint                       | `1000`        | 识别速率限制，单位 ms                                         |
| `pre_delay`           | uint                       | `200`         | 识别成功后、动作前的延迟，单位 ms                             |
| `post_delay`          | uint                       | `200`         | 动作后、识别 next 前的延迟，单位 ms                           |
| `pre_wait_freezes`    | uint \| object             | `0`           | 动作前等待画面稳定                                            |
| `post_wait_freezes`   | uint \| object             | `0`           | 动作后等待画面稳定                                            |
| `repeat`              | uint                       | `1`           | 动作重复次数                                                  |
| `repeat_delay`        | uint                       | `0`           | 重复动作之间的延迟                                            |
| `repeat_wait_freezes` | uint \| object             | `0`           | 重复动作之间等待画面稳定                                      |
| `inverse`             | bool                       | `false`       | 反转识别结果                                                  |
| `enabled`             | bool                       | `true`        | 是否启用节点；常用于 Project Interface 的 `pipeline_override` |
| `max_hit`             | uint                       | UINT_MAX      | 最大命中次数                                                  |
| `anchor`              | string \| list \| object   | `""`          | 锚点名或锚点配置                                              |
| `focus`               | object                     | `null`        | 节点通知/提示                                                 |
| `attach`              | object                     | `{}`          | 附加配置，按协议做字典合并                                    |

## 节点生命周期

```text
pre_wait_freezes → pre_delay → action →
[repeat_wait_freezes → repeat_delay → action] × (repeat - 1) →
post_wait_freezes → post_delay → 截图 → 识别 next
```

## 识别算法字段

### DirectHit

| 字段         | 类型                   | 默认值                 |
| ------------ | ---------------------- | ---------------------- |
| `roi`        | array<int,4> \| string | `[0, 0, 0, 0]`（全屏） |
| `roi_offset` | array<int,4>           | `[0, 0, 0, 0]`         |

### TemplateMatch

| 字段                 | 类型                   | 默认值                  |
| -------------------- | ---------------------- | ----------------------- |
| `template`           | string \| list<string> | 必填                    |
| `roi` / `roi_offset` | 同 DirectHit           |                         |
| `threshold`          | double \| list<double> | `0.7`                   |
| `order_by`           | string                 | `"Horizontal"`          |
| `index`              | int                    | `0`                     |
| `method`             | int                    | `5`（TM_CCOEFF_NORMED） |
| `green_mask`         | bool                   | `false`                 |

### FeatureMatch

| 字段                 | 类型                   | 默认值         |
| -------------------- | ---------------------- | -------------- |
| `template`           | string \| list<string> | 必填           |
| `roi` / `roi_offset` | 同 DirectHit           |                |
| `count`              | uint                   | `4`            |
| `detector`           | string                 | `"SIFT"`       |
| `ratio`              | double                 | `0.6`          |
| `order_by`           | string                 | `"Horizontal"` |
| `index`              | int                    | `0`            |
| `green_mask`         | bool                   | `false`        |

### ColorMatch

| 字段                 | 类型                         | 默认值                                |
| -------------------- | ---------------------------- | ------------------------------------- |
| `roi` / `roi_offset` | 同 DirectHit                 |                                       |
| `method`             | int                          | `4`（RGB）；项目中可优先考虑 HSV `40` |
| `lower`              | list<int> \| list<list<int>> | 必填                                  |
| `upper`              | list<int> \| list<list<int>> | 必填                                  |
| `count`              | uint                         | `1`                                   |
| `connected`          | bool                         | `false`                               |
| `order_by`           | string                       | `"Horizontal"`                        |
| `index`              | int                          | `0`                                   |

### OCR

| 字段                 | 类型                    | 默认值           |
| -------------------- | ----------------------- | ---------------- |
| `roi` / `roi_offset` | 同 DirectHit            |                  |
| `expected`           | string \| list<string>  | 匹配全部         |
| `threshold`          | double                  | `0.3`            |
| `replace`            | array<string,2> \| list | 无               |
| `only_rec`           | bool                    | `false`          |
| `model`              | string                  | `""`（默认模型） |
| `color_filter`       | string                  | `""`             |
| `order_by`           | string                  | `"Horizontal"`   |
| `index`              | int                     | `0`              |

### NeuralNetworkClassify

| 字段                 | 类型             | 默认值         |
| -------------------- | ---------------- | -------------- |
| `roi` / `roi_offset` | 同 DirectHit     |                |
| `model`              | string           | 必填           |
| `labels`             | list<string>     | `["Unknown"]`  |
| `expected`           | int \| list<int> | 匹配全部       |
| `order_by`           | string           | `"Horizontal"` |
| `index`              | int              | `0`            |

### NeuralNetworkDetect

| 字段                 | 类型                   | 默认值         |
| -------------------- | ---------------------- | -------------- |
| `roi` / `roi_offset` | 同 DirectHit           |                |
| `model`              | string                 | 必填           |
| `labels`             | list<string>           | 自动读取       |
| `expected`           | int \| list<int>       | 匹配全部       |
| `threshold`          | double \| list<double> | `0.3`          |
| `order_by`           | string                 | `"Horizontal"` |
| `index`              | int                    | `0`            |

### And

| 字段        | 类型                   | 默认值 |
| ----------- | ---------------------- | ------ |
| `all_of`    | list<string \| object> | 必填   |
| `box_index` | int                    | `0`    |

### Or

| 字段     | 类型                   | 默认值 |
| -------- | ---------------------- | ------ |
| `any_of` | list<string \| object> | 必填   |

### Custom

| 字段                       | 类型             | 默认值 |
| -------------------------- | ---------------- | ------ |
| `custom_recognition`       | string           | 必填   |
| `custom_recognition_param` | object \| string | `{}`   |

## 动作字段速查

### Click / LongPress

| 字段            | 类型                             | 说明                          |
| --------------- | -------------------------------- | ----------------------------- |
| `target`        | bool \| string \| array<int,2/4> | 点击位置；`true` 表示识别结果 |
| `target_offset` | array<int,2>                     | 基于 target 的偏移            |
| `duration`      | uint                             | LongPress 持续时间            |

### Swipe

| 字段       | 类型           | 说明         |
| ---------- | -------------- | ------------ |
| `begin`    | array<int,2/4> | 起点         |
| `end`      | array<int,2/4> | 终点         |
| `duration` | uint           | 滑动持续时间 |

### Custom

| 字段                  | 类型             | 说明               |
| --------------------- | ---------------- | ------------------ |
| `custom_action`       | string           | Agent 注册的动作名 |
| `custom_action_param` | object \| string | 自定义参数         |

## 参考

- MaaFramework Pipeline Protocol：<https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md>
