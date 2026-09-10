# 项目代理说明

## 平台与环境

- **操作系统**：Windows（此 AGENTS.md 面向 Windows）
- **Shell**：PowerShell 7
- **路径分隔符**：在终端命令中引用 Windows 本地路径时优先使用反斜杠 `\`（例如 `C:\Users\...`）；Markdown 链接、URL、前端 import、配置约定等语境按各自规范使用 `/` 或 `\`

## 终端环境

- 本项目在 Windows 的 **PowerShell 7** 中运行终端命令。
- 执行命令时，使用兼容 PowerShell 7 的语法，并为包含空格的路径加引号。
- 除非任务明确要求且工具可用，否则避免使用 Bash 专用语法，例如 heredoc（`cat <<'EOF'`）、依赖 GNU 工具的 Unix 管道，以及 `grep`、`find`、`sed`、`awk` 等命令。
- 避免使用 Linux/macOS 专用命令（例如 `ls`、`cat`、`chmod`、`mkdir -p`、`rm -rf`）。
- 优先使用 PowerShell 原生命令、PowerShell 7 现代特性，或仓库提供的 npm/scripts 命令，减少因 shell 语法差异导致的反复试错。
- 除非任务明确要求，否则不要切换到 `cmd`、Git Bash、WSL Bash 或其他 shell。

## 常用 PowerShell 对照

| Linux/macOS | PowerShell                                     |
| ----------- | ---------------------------------------------- |
| `ls`        | `Get-ChildItem` 或 `dir`                       |
| `cat`       | `Get-Content` 或 `type`                        |
| `mkdir -p`  | `New-Item -ItemType Directory -Force`          |
| `rm -rf`    | `Remove-Item -Recurse -Force`                  |
| `cp -r`     | `Copy-Item -Recurse`                           |
| `mv`        | `Move-Item`                                    |
| `chmod`     | `Set-ItemProperty` 或 `icacls`                 |
| `grep`      | `Select-String`                                |
| `find`      | `Get-ChildItem -Recurse`                       |
| `sed`       | `-replace` 运算符或 `ForEach-Object`           |
| `awk`       | `ConvertFrom-Csv`、`Select-Object` 或 `-split` |

## 项目约定

- 与用户对话、生成文件内容、生成 commit 信息时优先使用中文。
- commit 后不要自动 push；由用户决定何时推送。
- commit 信息应使用中文并遵守 Conventional Commits 风格。
- commit scope 涉及具体任务时，使用任务本身的正式名称并保持原有大小写和连续拼写（例如 `SoloRaid`，不要写成 `solo-raid`）。
- commit scope 涉及活动主题时，使用对应任务的正式名称 `LargeEvent` 或 `SmallEvent`，不要使用具体主题名（例如 `ProjectMatis`）。
- 编辑 i18 本地化文件时，保持与参考文件一致的排序。
- 大活动和小活动的主题 i18n 显示名统一使用全大写；新增或调整主题时，同时检查 `LargeEventTheme` 和 `SmallEventTheme` 的往期主题是否保持一致。
- Go 测试从仓库根目录运行；Go module 位于 `agent\go-service`。
- 若本次修改涉及 Go 相关代码，则在会话收尾时自动重新构建 Go（`agent\go-service`），确保改动后的 Go Agent 已编译可用；未改动 Go 代码时不构建。
- 审查 pipeline 名称时，要检查每个节点的实际职责，不只根据后缀判断。
- 对 OCR 添加 `threshold` 没有意义：OCR 节点不要写 `threshold`，阈值只对模板匹配 / 颜色匹配等识别有效。
- 项目框架默认按 `1280*720` 这个分辨率基准来算。用户传入更大图片并要提取内容、生成涉及 `roi` 的信息时，不用死板地先把图缩成 `1280*720`；只要最终识别结果和 `roi` 坐标都换算到 `1280*720` 坐标系即可，直接按比例缩放坐标往往更省事。

## Pipeline 与 Go 的分工

- MDA 同时使用 Pipeline 与 Go 两种语言实现任务，二者是互补关系，不是竞争关系。
- 简单逻辑用 Pipeline 实现，复杂逻辑用 Go 实现。
- 避免用 Pipeline 硬写复杂逻辑导致配置冗长、难以维护；也避免用 Go 实现简单逻辑，无端抬高维护成本。
- 一个任务同时包含 Pipeline 与 Go 是正常且合理的现象；涉及任务实现时，按此分工为每个环节选择最合适的语言。

### 基本识别交给 Pipeline，复杂业务交给 Go

参考 MaaEnd（`C:\Users\12042\Documents\GitHub\MaaEnd`）的项目实践：涉及 Go 的任务中，**基本识别一律由 Pipeline 声明式完成，Go 只负责更深层的业务逻辑**，这样更容易维护、调试面板更直观。

- **基本识别放 Pipeline**：模板定位、OCR、颜色确认（ColorMatch）、二次验证、区域偏移（`roi_offset`）、页面/弹窗确认等，都用 Pipeline JSON 声明。
    - 识别参数（ROI、模板、阈值、颜色区间、`count`、`roi_offset`、`expected`）应留在 Pipeline 中，不要在 Go 代码里硬编码。
    - 需要组合/二次验证时，优先用 Pipeline 的 `TemplateMatch` + `ColorMatch` + `And`/`Or` + `roi_offset` 表达；例如“模板匹配到槽位后，再确认匹配框内颜色数量达标”。
- **Go 只承载业务**：跨节点状态/快照、决策算法、路由、结果解释、数据聚合、需要动态计算或 Pipeline 无法表达的逻辑。
    - Go 复用 Pipeline 识别节点时使用 `ctx.RunRecognition("节点名", img)`，不要用 `ctx.RunRecognitionDirect` 在 Go 内硬编码识别参数。
    - 若某个识别必须动态计算 ROI，优先把可变的识别参数下沉到 Pipeline（如通过 `roi_offset`/锚点组合），实在无法表达时才允许在 Go 中计算。
- **审查原则**：
    - 审查 Go 代码时，若发现识别参数写在 Go 里，优先考虑挪回 Pipeline。
    - 审查 Pipeline 时，若发现复杂业务逻辑（状态机、决策、计算、跨节点聚合）硬写在 JSON，优先考虑挪到 Go。
    - 判断一个环节归属时，问“这是‘看到了什么/在哪里’，还是‘看到之后要做什么/怎么算’”：前者给 Pipeline，后者给 Go。

## 活动主题「当前活动」路由

### 机制

- `LargeEventTheme` 和 `SmallEventTheme` 的首个 case 固定为 `CurrentEvent`（显示名「当前活动」），并且是该选项的 `default_case`。
- 客户端把用户选中的 case 名写进 `config\maa_pi_config.json`，且存的是**当时选的具体 case 名**（真实样例：`"SmallEventTheme": "GreatVillainUnion"`）。框架按 `case.name == 配置值` 精确查找，命中则应用该 case 的 override，未命中则 `LogWarn` 后跳过。由此推出两条重要结论：
    - 只改 `default_case` 只影响**新用户**与**未配置过的用户**；已配置的老用户配置里存的是具体 case 名，不会因 `default_case` 改动而变化。
    - 选了 `CurrentEvent` 的用户配置里存的是字符串 `"CurrentEvent"`，该 case 永久存在，因此会随 base 自动跟随最新活动——这是「当前活动」机制成立的根本原因。
- `CurrentEvent` **不携带 `pipeline_override`**。它靠 resource 侧 base 节点承载最新主题模板来工作，用户选它即可在下次更新后自动跟随，无需重写。
- `CurrentEvent` 不是主题名，locale 显示名用「当前活动」/ `Current Event`，不适用全大写约定；同时在 `zh_cn`、`en_us` 中补上 `option.{LargeEventTheme,SmallEventTheme}.CurrentEvent` 与对应 `.description`。
- `CurrentEvent` 可以带 `option`（子选项列表，如 `LargeEventPersonaOnFrontlineMiniGame`），这不属于 override，必须保留。

### base 节点承载最新主题

- `LargeEvent` / `SmallEvent` 的以下 base 节点，其 `template` 直接写**最新主题**的真实模板，并加注释 `//当前活动，随最新主题更新`：
    - `LargeEventEnterMainPage`、`LargeEventClickStoryStage`、`LargeEventClickStoryStageRepeatable`
    - `SmallEventEnterMainPage`、`SmallEventClickStage`、`SmallEventClickStageRepeatable`
- 这些节点原先写 `Common/RedDot.png //占位，应该去task页面修改`，已不再需要占位——base 本身就是当前活动的真实配置。
- `LargeEventEnterMission` 是**中性红点检测**（`Common/RedDot.png` + `Common/RedDotSP.png`），不是主题模板节点，**不要改动**。
- `LargeEventMissionClaimed` 是**颜色阈值特例节点**（base 灰白 `[190,190,190]`~~`[219,219,219]`，`ArkRanger` 覆盖为蓝紫 `[15,25,55]`~~`[35,35,65]`）。它**不纳入 base 承载范围**，保持 base 原值；只有需要特例的主题才在自己的 case 里覆盖。

### 适配新主题的操作顺序

1. 为新主题写好完整的 case（含 `label`、`option`、三个模板类节点的 `pipeline_override`）。
2. 把 base 节点的 `template` 换成新主题的模板列表（即把「当前活动」升级为新主题）。
3. `CurrentEvent` **不动**：它没有 override，base 一改就自动跟随。
4. 往期主题 case 的 `pipeline_override` 一律保留不动，供用户显式回选仍在开放的老活动。
5. 跑 `npm run check:theme` 校验。
6. **本次适配是否下架或重命名了某个往期主题 case？** 若是，见下节「用户影响与提示话术」，需要在更新说明里提示这批用户重选主题；若否，不要发提示。

### 用户影响与提示话术

改动前先判断自己属于哪种情况——框架在 `Configurator.cpp:447-468` 按 `case.name == 配置里的值` 查找，找不到就 `LogWarn` 后跳过该 option 的全部 override：

| 用户配置里存的值                                   | 改 base 后的行为                                            | 是否要提示                 |
| -------------------------------------------------- | ----------------------------------------------------------- | -------------------------- |
| 某个**仍在的**具体主题名（如 `GreatVillainUnion`） | case 命中，override 照常应用，行为与改动前完全一致          | 不需要，无感               |
| `CurrentEvent`                                     | case 命中，`pipeline_override` 为空 → 落到 base = 最新主题  | 不需要，**这就是自动跟随** |
| **已下架主题**的名字                               | `case not found` → 跳过全部 override → 落到 base = 最新主题 | **需要，用户需重选主题**   |

- 大多数用户存的是**具体主题名**（真实配置样例：`"SmallEventTheme": "GreatVillainUnion"`），因此改 base **不会打扰他们**，不要过度提示。
- 只有「往期主题 case 被删除 / 重命名」才会让用户失配。此时该用户想打的老活动已识别不到，必须重选。
- 提示时给可操作的信息，不要只丢结论。建议话术：

    > 本次更新下架了往期主题活动 XXX，若你在「活动主题」中选的是它，请在任务设置里重新选择当前开放的主题（或直接选「当前活动」以后自动跟随）。

- 反过来说：**适配新主题本身不需要任何提示**。只有「操作顺序」第 6 步确认删了 case 才提示，避免每次更新都发无用公告。
- `case not found` 的降级是"吃 base"而非报错崩溃，所以用户侧表现为"识别不到关卡"而非程序异常，缺少日志的用户很难自己定位——这正是必须主动提示的原因。

### 护栏：`npm run check:theme`

`scripts/check-theme-sync.mjs` 自动守住两条不变量，违规时退出码 1：

- **断言 A**：`CurrentEvent` 不得携带非空 `pipeline_override`（否则自动跟随失效）。
- **断言 B**：所有往期主题 case 必须逐个覆盖同一批模板类节点。漏覆盖的主题在 base 换新后会**静默继承新主题模板**，导致回选老活动时永远匹配不上且不报错——这正是护栏存在的意义。

新增主题或调整 base 时，若模板类节点的集合发生变化，需同步更新脚本顶部的 `TARGETS[].expectedTemplateNodes`。

## 大型小活动适配

### 先判断：这个 SmallEvent 主题有没有 Story

小活动分两类，**命名体系完全不同**，适配前先确认属于哪一类：

| 类型                          | 判断依据                                  | 命名体系                                                                                 | 实例                                            |
| ----------------------------- | ----------------------------------------- | ---------------------------------------------------------------------------------------- | ----------------------------------------------- |
| **无 Story** 的普通小活动     | 活动主页没有 STORY I / STORY II 切换按钮  | `{Theme}StageNormal.png` / `{Theme}StageHard.png`                                        | `BitterSpice`、`BSideIdol`、`GreatVillainUnion` |
| **有 Story** 的特殊大型小活动 | 活动主页有独立按钮切换 STORY I / STORY II | `{Theme}Story1Stage.png` / `{Theme}Story2StageNormal.png` / `{Theme}Story2StageHard.png` | `ProjectMatis`                                  |

- **不要把 `Story1` / `Story2` 用在没有 Story 的小活动上。** `GreatVillainUnion` 曾误用这套前缀（`Story1Stage` / `Story2StageHard`），已被纠正为 `StageNormal` / `StageHard`。
- 判据是**活动主页有没有 Story 切换按钮**，不是"有没有两批关卡"。普通小活动也可能有多个关卡，但那是同一 Story 下的关卡序号（`1-01`、`1-02`…），不是 Story。
- `Normal` / `Hard` 指**难度模式**。同一模式下 EVENT 可点击标记的外观不同（颜色、字号、装饰），因此两种模式各自需要独立模板。Repeatable（扫荡按钮）同理，两种模式各一张。

### Story 切换与覆盖

- 有 Story 的活动，不适配 Story 切换按钮。Story 2 开放后，活动页面会自动切换到 Story 2，无需 Pipeline 额外点击；不要照搬 `LargeEvent` 的 Story 优先级或 Story 入口切换流程。
- 有 Story 时，关卡模板必须按实际 Story 语义命名。Story 1 使用 `{Theme}Story1Stage.png` 和 `{Theme}Story1StageRepeatable.png`；Story 2 普通难度使用 `{Theme}Story2StageNormal.png` 和 `{Theme}Story2StageNormalRepeatable.png`。不要用 `SP` 代指 Story 2。
- 将 Story 1、Story 2 模板共同加入 `SmallEventClickStage` 和 `SmallEventClickStageRepeatable` 的主题覆盖，由现有统一关卡流程识别并推进。
- Story 2 Hard 未开放时，不添加 `{Theme}Story2StageHard*` 模板或配置；开放后再根据实际素材适配。

### 关卡已全清（CLEAR）时截不到 EVENT

- **Normal / Hard 关卡全部 CLEAR 后，关卡行显示 `CLEAR` 而不是 `EVENT`，此时无法截到该模式的 EVENT 模板。**
- 判断图层归属时不要因为"截图里没有 EVENT"就断定模板作废。更可靠的方式是**用该模板对截图做模板匹配**：若在 ROI 内找不到高分命中（如 < 0.5），才可疑；若截图上该模式已全 CLEAR，属于正常现象。
- 需要补某模式的模板时，必须在该模式**仍有未 CLEAR 关卡**的状态下截图。
