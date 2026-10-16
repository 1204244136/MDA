---
name: mda-project-guide
description: MDA 项目专属开发指南。用于在 MDA 中编写或审查 MaaFramework Pipeline、Project Interface 任务、locale、go-service 组件时补充项目结构、业务域、命名习惯和现有任务选项示例。与通用 pipeline-guide、pipeline-debug、generate-pi-task、go-service-guide 配合使用。
---

# MDA 项目指南

## 适用范围

当你正在 `C:\Users\12042\Documents\GitHub\MDA` 仓库中工作，并且任务涉及以下内容时使用本 skill：

- Pipeline JSON 编写、调试或命名。
- Project Interface 任务文件生成或修改。
- `assets/interface.json`、`assets/tasks/`、`assets/locales/interface/`。
- MDA 的 `agent/go-service/` 自定义组件。
- 需要判断 MDA 现有业务域、任务选项、locale 语言范围或路径约定。

本 skill 只记录 MDA 项目特例；MaaFramework 通用规则请先看：

- `pipeline-guide`
- `pipeline-debug`
- `generate-pi-task`
- `go-service-guide`

## 项目结构

MDA 采用 MaaFramework / Project Interface / MXU 风格结构：

```text
MDA/
├── AGENTS.md
├── assets/
│   ├── interface.json
│   ├── tasks/
│   ├── locales/interface/
│   │   ├── zh_cn.json
│   │   └── en_us.json
│   ├── locales/go-service/
│   │   ├── zh_cn.json
│   │   └── en_us.json
│   └── resource/pipeline/
├── agent/go-service/
└── tools/schema/
```

关键文件：

| 路径                                  | 用途                                                                  |
| ------------------------------------- | --------------------------------------------------------------------- |
| `assets/interface.json`               | Project Interface 主配置，包含 import、group、controller、resource 等 |
| `assets/tasks/*.json`                 | PI 任务定义和 option 配置                                             |
| `assets/tasks/preset/*.json`          | 预设任务组合                                                          |
| `assets/resource/pipeline/**.json`    | MaaFramework Pipeline 节点                                            |
| `assets/locales/interface/zh_cn.json` | 界面中文文案                                                          |
| `assets/locales/interface/en_us.json` | 界面英文文案                                                          |
| `tools/schema/*.json`                 | Pipeline / PI / Custom schema                                         |
| `agent/go-service/`                   | Go Agent 自定义逻辑                                                   |

## MDA locale 范围

MDA 当前 interface locale 主要维护：

- `zh_cn`
- `en_us`

新增任务或 option 时，默认只同步这两个文件。不要按其它项目习惯擅自补 `zh_tw`、`ja_jp`、`ko_kr`，除非用户明确要求或项目新增了对应 locale 文件。

所有用户可见文本在任务文件中使用 `$` i18n key，例如：

```json
"label": "$task.Shop.label"
```

## 常见业务域

MDA Pipeline 当前常见业务域包括：

| Domain                  | 说明                   |
| ----------------------- | ---------------------- |
| `Advise`                | 咨询 / 剧情 / 剧集相关 |
| `Arena`                 | 竞技场相关             |
| `Battle`                | 战斗通用流程           |
| `Common`                | 通用 UI、弹窗、交互    |
| `CoordinatedOperations` | 协同作战               |
| `DailyRewards`          | 日常奖励               |
| `Event`                 | 活动通用域             |
| `Interception`          | 拦截战                 |
| `Login`                 | 登录流程               |
| `MapPushing`            | 推图                   |
| `Navigation`            | 导航和回到主界面       |
| `Outpost`               | 前哨基地               |
| `RedDotClear`           | 红点清理               |
| `Shop`                  | 商店                   |
| `SimulationRoom`        | 模拟室                 |
| `TribeTower`            | 企业塔 / 阵营塔        |

新增节点时优先沿用现有目录与 Domain；不要为了单个节点新建同义 Domain。

## Pipeline 命名习惯

MDA 使用通用 MaaFramework PascalCase 命名规则，并倾向于：

- 入口：`<Domain>Main`
- 编排：`<Domain><Subtask>Flow`
- 进入页面：`<Domain>Enter<Page>`
- 页面确认：`<Domain><Page>Entered`
- 页面状态：`<Domain>On<Page>Page`
- UI 可见：`<Domain><Object>Visible`
- 点击/选择/领取：`<Domain>Click<Object>`、`Select`、`Claim`、`Purchase`
- 确认：`<Domain>Confirm<Object>` 或 `CommonConfirm<Object>`

MDA 命名中特别注意：

- 新节点不要再用 `FlagInX` 表达页面状态。
- `Visible` 节点不要过度拆分；如果只被一个点击节点使用且没有组合识别价值，优先合并进点击节点。
- `Entered` 节点可以只被一个进入流程引用，只要它承担“进入成功哨兵”职责，就不是无意义中转节点。

## Project Interface 任务文件

MDA 任务文件位于 `assets/tasks/`，当前包括：

- `Advise.json`
- `Arena.json`
- `CashShop.json`
- `CoordinatedOperations.json`
- `DailyRewards.json`
- `Interception.json`
- `LargeEvent.json`
- `MapPushing.json`
- `Outpost.json`
- `RedDotClear.json`
- `Shop.json`
- `SimulationRoom.json`
- `SmallEvent.json`
- `TribeTower.json`

新增任务文件后，`assets/interface.json` 的 `import[]` 使用相对路径：

```json
"tasks/MyTask.json"
```

而不是：

```json
"assets/tasks/MyTask.json"
```

## MDA 选项建模示例

这些例子来自 MDA 现有任务，用于辅助判断 `generate-pi-task` 的 option 类型。

| Task File             | Option                      | Type            | 说明                                   |
| --------------------- | --------------------------- | --------------- | -------------------------------------- |
| `Arena.json`          | `SpecialReward`             | `switch`        | 单个是否领取奖励的开关                 |
| `Arena.json`          | `EnterRookieArena`          | `switch`        | 单个是否进入该竞技场的开关             |
| `Interception.json`   | `InterceptionType`          | `select`        | Normal / Anomaly 互斥                  |
| `Interception.json`   | `NormalyInterceptionLevel`  | nested `select` | 普通拦截的难度子选项                   |
| `Interception.json`   | `AnomalyInterceptionTarget` | nested `select` | 异常拦截的目标子选项                   |
| `Interception.json`   | `ManualInterceptionBattle`  | `switch`        | 单个功能开关                           |
| `Shop.json`           | `ArenaShopItemList`         | `checkbox`      | 多个独立商店物品可同时购买             |
| `Shop.json`           | `RecyclingShopList`         | `checkbox`      | 多个独立可回收物品，存在默认选择       |
| `Shop.json`           | `CommonShopFreeGoods`       | `switch`        | 单个是否领取免费商品开关               |
| `SimulationRoom.json` | `StartOverlock`             | `switch`        | Yes case 下嵌套 `AutoBIOSSetting`      |
| `SimulationRoom.json` | `AutoBIOSSetting`           | nested `switch` | 自动难度子开关                         |
| `Outpost.json`        | `DefenseRewards`            | `switch`        | 默认开启时可设置 `default_case: "Yes"` |
| `TribeTower.json`     | `EnterCommonTower`          | `switch`        | 默认关闭时可按既有风格让 No 在 Yes 前  |

判断原则：

- `select`：只应用选中 case；路由覆盖可用完整 `next`，共享候选且依赖启停互斥时再配置 `enabled`。
- `checkbox`：独立列表，case 通常只启用自身节点。
- `switch`：单个是否执行的功能。
- nested option：只有某个 case 激活后才显示的子配置。

## 活动更新

从完整截图/已裁图适配大小活动、补难度模板或更新当前主题时，使用 [mda-event-update](../mda-event-update/SKILL.md)。活动素材流程由该 skill 维护，现行架构以项目 `AGENTS.md` 和 `docs/event-theme-routing.md` 为准。

## MDA Go Service

MDA 存在 `agent/go-service/`，当前包含：

- `main.go`
- `register.go`
- `logger.go`
- `pkg/pienv`
- `pkg/resource`
- `pkg/i18n`
- `pkg/maafocus`
- `taskersink/aspectratio`
- `taskersink/hdrcheck`
- `taskersink/processcheck`
- `taskersink/membership`

开发 Go 组件时：

- 先看 `agent/go-service/register.go` 的聚合方式。
- 新包应有自己的 `Register()`。
- 注册名必须与 Pipeline Custom 节点一致。
- 日志和包结构遵循当前已有 Go 代码。
- 如果新增用户可见文案，同时维护 `assets/locales/go-service/zh_cn.json` 和 `en_us.json`。

## Windows / PowerShell 约束

MDA 的 `AGENTS.md` 明确项目运行环境为 Windows / PowerShell 7。

执行命令时：

- 优先使用 PowerShell 语法。
- 路径带空格时加双引号。
- 不要默认使用 Bash heredoc、`grep`、`find`、`sed`、`awk` 等 Unix 写法。
- 文件搜索和读取优先使用 Claude Code 专用工具。

## 与通用 skill 的配合方式

- 写 Pipeline：先用 `pipeline-guide`，再用本 skill 确认 MDA Domain、locale 范围和任务结构。
- 调试 Pipeline：先用 `pipeline-debug`，再用本 skill 判断 MDA 现有命名和任务语义。
- 生成 PI task：先用 `generate-pi-task`，再用本 skill 参考 MDA 选项建模示例。
- 写 Go Service：先用 `go-service-guide`，再用本 skill 确认 MDA 的包和 locale 目录。

## 自检清单

- [ ] 新增/修改的任务文件已被 `assets/interface.json` import。
- [ ] 只维护 MDA 当前存在的 interface locale：`zh_cn`、`en_us`。
- [ ] Pipeline 节点 Domain 与现有目录和业务域一致。
- [ ] option 类型符合 MDA 既有建模习惯。
- [ ] `pipeline_override` 节点名真实存在。
- [ ] Go 组件已注册到 `registerAll()`。
