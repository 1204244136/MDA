# 项目代理说明

## 平台与环境

- **操作系统**：Windows（本项目仅面向 Windows）
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

## 大型小活动适配

- 部分归入 `SmallEvent` 的特殊大型小活动包含 `STORY I` / `STORY II` 两篇剧情，活动主页通过独立按钮切换 Story。
- 不适配 Story 切换按钮。Story 2 开放后，活动页面会自动切换到 Story 2，无需 Pipeline 额外点击；不要照搬 `LargeEvent` 的 Story 优先级或 Story 入口切换流程。
- 关卡模板必须按实际 Story 语义命名。Story 1 使用 `{Theme}Story1Stage.png` 和 `{Theme}Story1StageRepeatable.png`；Story 2 普通难度使用 `{Theme}Story2StageNormal.png` 和 `{Theme}Story2StageNormalRepeatable.png`。不要用 `SP` 代指 Story 2。
- 将 Story 1、Story 2 模板共同加入 `SmallEventClickStage` 和 `SmallEventClickStageRepeatable` 的主题覆盖，由现有统一关卡流程识别并推进。
- Story 2 Hard 未开放时，不添加 `{Theme}Story2StageHard*` 模板或配置；开放后再根据实际素材适配。
