# Pipeline `desc` 与 `//` 注释规范

本文规定 MaaFramework Pipeline JSON 中节点 `desc` 字段与 `//` 注释的分工边界。目标是让 `desc` 保持"一句话名片"的形态，让注释尽量贴在它说明的那一行行尾，避免 AI 生成时把所有说明都堆进 `desc`。

## 一句话原则

- `desc` 写 **"这是什么"**，给没打开这个节点的人看。
- `//` 写 **"为什么这样"**，贴在那一行行尾，给正在改这个节点的人看。

## 框架事实

先厘清 `desc` 在框架里的真实地位，避免高估或低估它。

1. **只定义在 schema**：`tools/pipeline.schema.json` 把 `desc` 与 `*_desc` 归入 `jsonDocument`（`Document Property`，`type: string`），与 `doc` / `*_doc` 同组。`docs/zh_cn/3.1-任务流水线协议.md` 与 `3.3-ProjectInterfaceV2协议.md` 均未定义该字段。
2. **运行时不存在**：`PipelineParser` 按白名单取键，`PipelineData` 结构体没有 `desc` 成员。`desc` 在解析阶段即被丢弃，不参与识别、动作、路由、超时与日志，也不进入 dump。
3. **唯一消费者是编辑器工具链**：`@nekosu/maa-pipeline-manager` 把 `doc` / `desc` 注册为 `task.doc`；`maa-support-extension` 在 `next` 补全项的详情面板与 hover 中渲染 `${doc}\n\nReco: X\n\nAct: Y`。因此 `desc` 的读者永远是"在别处引用这个节点的人"，且显示区域很窄。
4. **PI 层的 `desc` 是 `doc` 的别名**：`interface.schema.json` 中两者 title 同为"任务文档"（提交 `2ab9b5a1 chore: add desc field`）。本仓库 PI 层统一使用 `description`（面向用户）与 `doc`（长文档），**不使用 `desc`**。
5. **`//` 是官方支持的一等公民**：pipeline 以 JSONC 解析（`PipelineResMgr.cpp` 的 `json::open(path, true, true)`，meojson 开启 `accept_jsonc`），`//` 与 `/* */` 均合法且被忽略。本仓库统一使用 `//`。
6. **注释与排序不冲突**：`prettier-plugin-maafw-sort` 重排节点键时，注释会跟随它所标注的键一起移动；`desc` 固定排序在最前。
7. **`{` / `[` 行尾的注释会被抬走**：把注释放到 `"next": [ // …` 或 `"attach": { // …` 这类容器开头，Prettier 会把它改成独立行。所以容器级说明要挂到容器内第一条可挂注释的行尾，或挂到 `desc` 行尾。

## `desc` 规则

- **D1 一句话、短**：不超过 24 个字符；不含 `。` `；` `;`；逗号最多 1 个，且只用于"条件, 动作"的紧凑写法。
- **D2 只写业务语义**：不写识别/动作类型（hover 已显示 `Reco` / `Act`）、不写 ROI / 阈值 / 坐标 / 字段名、不写 `Go` / `Pipeline` / `Custom` 等实现来源。
- **D3 不复述节点名**：节点名已经表达 Domain、动作与角色；`desc` 只在能补充语境时写，否则省略。
- **D4 允许省略**：`desc` 是可选字段。`Common*` 这类自解释节点、纯路由节点可以完全不写。
- **D5 句式固定**：`<动词><对象>`、`<对象><状态>` 或 `<流程名>`，例如"购买打折商品"、"小地图已展开"、"洗词条流程编排"。
- **D6 语言**：中文；界面专有名词（`SALE`、`Pass`、`STORY`）保留原文。

## `//` 注释规则

- **C1 优先写在行尾**：注释通常跟在被说明的那一行行尾，便于随字段移动；需要独立成行时也允许保留。
    - 标量字段、数组元素：直接写在该行行尾。
    - 对象 / 数组整体说明：写到该容器内第一条可挂注释的行尾；数组整体说明写到节点 `desc` 行尾（见「框架事实」第 7 条）。
    - 节点级说明：写到 `desc` 行尾。
- **C2 只写五类内容**：
    1. 数值依据：阈值怎么定的、ROI 为什么这么框、等待时长从哪来。
    2. 踩坑与反例：为什么不能写得更简单，会误命中 / 误删 / 连点。
    3. 耦合与不变量：必须挂在哪个哨兵之后、`attach` 键约定、主题模板归属、Go 与 Pipeline 的接口。
    4. 流程与状态机语义：为什么自环、为什么不能进 `[JumpBack]`、超时后由谁接管。
    5. 临时手段与 TODO：什么条件下失效、何时清理。
- **C3 不复述被注释的内容**：行里已经有键名和值，注释直接写原因。写成 `"max_hit": 1, // 只点一次…`，不要写 `// max_hit 1：只点一次…`。
- **C4 长度**：一条注释尽量控制在 140 字符以内（含前面的代码部分）；更长的说明搬到 `docs/` 并在行尾留一句指针。
- **C5 禁止**：用注释写"这个节点做什么"（那是 `desc`）、写行号或临时文件路径这类立刻会过期的引用、逐行翻译代码。
- **C6 语言**：中文；字段名、API 名、正则原样保留。

## 三层分工

| 层         | 载体                                   | 读者                     | 写什么             | 反例                                         |
| ---------- | -------------------------------------- | ------------------------ | ------------------ | -------------------------------------------- |
| 用户可见   | `assets/tasks/*.json` 的 `description` | 玩家                     | 功能、注意事项     | 内部字段名                                   |
| 开发者速览 | pipeline `desc`                        | 在 `next` / hover 中看到 | 一句话角色         | "第1槽锁定按钮橙色锁 ColorMatch（一次性锁）" |
| 施工笔记   | `//`                                   | 正在改这个节点的人       | 为什么、数字、耦合 | "max_hit 1：只点一次"                        |
| 设计文档   | `docs/zh_cn/...`                       | 维护者                   | 策略、算法、期望值 | 单个阈值的实测值                             |

## 判定流程

1. 这句话是给"没打开这个节点的人"看的一句话吗？→ 写 `desc`。
2. 它在解释"为什么 / 这个数字 / 和谁耦合 / 踩过什么坑"吗？→ 写到对应行的行尾。
3. 两者都不是 → 都不写。

## 正反例

### 1. 实现细节从 `desc` 移回字段行尾

不推荐：

```jsonc
"__EquipmentRerollSlot1LockOrange": {
    "desc": "第1槽锁定按钮橙色锁 ColorMatch（一次性锁）；roi_offset 为增量：锚点框(14x15)+该值=(768,501,26,23)",
    "recognition": {"type": "ColorMatch", "param": {"roi_offset": [0, 0, 0, 0]}}
}
```

推荐：

```jsonc
"__EquipmentRerollSlot1LockOrange": {
    "desc": "1 号槽橙色锁",
    "recognition": {
        "type": "ColorMatch",
        "param": {
            "roi_offset": [0, 0, 0, 0] // 相对锚点框的增量：锚点框 + 该值 = (768,501,26,23)
        }
    }
}
```

### 2. 理由写在路由数组所在节点的 `desc` 行尾

不推荐：

```jsonc
"ArenaPrepareEntry": {
    "desc": "任务入场先恢复方舟再回到Main领奖，避免纯场景导航直接进入竞技场后漏领奖励",
    "next": ["SceneEnterArk"]
}
```

推荐：

```jsonc
"ArenaPrepareEntry": {
    "desc": "入场前先回方舟", // 直接场景导航进竞技场会漏掉累积奖励，所以先回方舟再回 Main 领奖
    "next": ["SceneEnterArk"]
}
```

### 3. 容器约定挂在容器内第一条，`desc` 只留角色

不推荐：

```jsonc
"CustomBurstConfig": {
    // 选项通过 pipeline_override 写 attach 顶层键，按 key 浅合并
    "desc": "自定义爆裂配置承载节点：选项通过 pipeline_override 写 attach 顶层键（按 key 浅合并）。r{轮}s{阶段}=角色（空=不指定）；round_count=使用轮数（默认1）"
}
```

推荐：

```jsonc
"CustomBurstConfig": {
    "desc": "爆裂选项承载",
    "attach": {
        "round_count": 1, // 选项统一写本节点 attach：pipeline_override 按 key 浅合并，分散声明会互相覆盖；r{轮}s{阶段}=该阶段释放的角色（空=不指定）
        "r1s1": ""
    }
}
```

### 4. 不要复述键值

不推荐：

```jsonc
"max_hit": 1, // max_hit 1：只点一次「重新再试」
```

推荐：

```jsonc
"max_hit": 1, // 只点一次「重新再试」，再等不到商店页就交给后续超时收尾
```

### 5. 不要复述节点名

不推荐：

```jsonc
"SynchroDeviceClickAutoSelectButton": {"desc": "点击自动选择"}
"AdviseAttemptsExhausted": {"desc": "次数耗尽"}
```

推荐：省略 `desc`，或补充节点名没有的语境：

```jsonc
"SynchroDeviceClickAutoSelectButton": {"desc": "同步器一键选满"}
```

## 反模式清单

1. `desc` 写成"角色：说明"的冒号句，冒号后半段是整段解释。
2. `desc` 里写实测数据（阈值 0.999、命中框坐标、帧数）。
3. `desc` 里写 `attach` 键约定或 `pipeline_override` 合并规则。
4. 注释另起一行，浮在被说明字段上方。
5. 注释开头重复键名或键值，例如 `// max_hit 1：…`、`// roi_offset 是…`。
6. 把注释放到 `"next": [ // …`、`"attach": { // …` 这类容器开头（会被格式化器抬成独立行）。
7. 用 `"//": "..."` 伪键冒充注释：schema 视其为未知键，排序会把它甩到节点末尾。
8. 同一条信息同时出现在 `desc`、注释与设计文档里。

## 护栏

`scripts/check-desc.mjs`（`npm run check:desc`）检查本规范的可机械判定部分：

- `desc` 超过 24 字、含 `。` `；`、逗号 ≥ 2 → 报错。
- `desc` 含 ROI / 阈值 / 坐标 / `attach` / `pipeline_override` / `Go` / `OCR` / `TemplateMatch` 等实现词 → 警告。
- 注释位置不参与机械校验；建议优先使用行尾注释，独立成行的注释也可以保留。
- 节点没有 `desc` → 不报错（允许省略）。

存量违规通过 `docs/pipeline-desc-allowlist.json` 临时豁免，清理一条删一条；新增文件不允许进入白名单。
