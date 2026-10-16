---
name: mda-issue-log-analysis
description: 分析 MDA 公开 Issue、日志包或本地日志。用于 `https://github.com/1204244136/MDA/issues/...`、`#1234`、MDA 日志压缩包、识别失败、任务卡死、控制器差异、Pipeline/Agent/MXU 问题。会从 maafw.log、maafw.bak.*.log、go-service.log、mxu-tauri.log、mxu-web-*.log、mxu-agent*.log、config/*、on_error/ 中筛选证据，并结合 MDA、MaaFramework、MXU 代码和文档判断根因。
---

# MDA Issue / Log Analysis

## Scope

- 用于 MDA 仓库：`https://github.com/1204244136/MDA`。
- 输入可以是完整 issue URL、`#1234` 形式的 issue 编号、本地日志目录或日志压缩包路径。
- 只分析用户授权访问的公开 issue、用户提供的日志包或本地文件。
- 如果 issue 没有日志包，且问题明确需要运行证据，先说明证据不足；不要凭空推断根因。
- 如果发现 `.dmp` 文件，必须联动 `dmp-analysis` skill，不要只凭文本日志猜测崩溃原因。

## Workflow

### 1. 规范化输入

- `#1234` 视为 `https://github.com/1204244136/MDA/issues/1234`。
- 如果 URL 不是 `1204244136/MDA`，说明本 skill 不适用，除非用户明确要求按 MDA 同类日志格式分析。
- 如果输入是本地路径，先识别它是目录、zip 包还是单个日志文件。

### 2. 获取 issue / 日志上下文

从 issue 或用户描述中提取：

- MDA 版本。
- MaaFramework / MXU 版本。
- 控制器类型。
- 任务名 / 入口名。
- 用户期望行为。
- 实际行为。
- 复现步骤。
- 维护者评论。

维护者评论只能作为线索；仍需用日志和代码自行验证。

### 3. 提取日志附件

关注 MDA 导出的日志压缩包。附件命名可能随打包工具变化，不要只匹配单一文件名；优先寻找包含以下文件或目录的 zip：

- `maafw.log`
- `maafw.bak.*.log`
- `go-service.log`
- `mxu-tauri.log`
- `mxu-web-YYYY-MM-DD.log`
- `mxu-agent*.log`
- `config/`
- `on_error/`
- `.dmp`

如果同一个 issue 有多个日志包：

1. 先看最新一次复现。
2. 若 issue 在对比版本、控制器或配置，再补看前面的包。

### 4. 下载 / 解压日志包

二进制 zip 不要用网页抓取工具直接读取。使用终端下载到临时目录，例如：

```text
.cache/issue-logs/issue-<number>/
.cache/log-analysis/local-<timestamp>/
```

解压后先列目录，不要假定结构固定。常见差异：

- 多份 `mxu-agent-<index>-<pid>.log`。
- 多天 `mxu-web-YYYY-MM-DD.log`。
- `maafw.log` 过短，需要看 `maafw.bak.*.log`。
- 没有 `on_error/`。
- 现场图被日志导出体积限制截断。
- 包内存在 `.dmp`。

### 5. DMP 崩溃转储分析

如果发现 `.dmp`：

1. 立即读取并执行 `.agents/skills/dmp-analysis/SKILL.md`。
2. 解析异常类型、崩溃模块、crashing thread。
3. 需要时下载 MaaFramework / MXU PDB 并符号化。
4. 用 DMP 文件名中的 PID 与 `maafw.log` 的 `[Px<pid>]` 或 MXU 日志交叉验证。
5. 最终报告必须包含 DMP 分析区域，并列出 crashing thread 的全部有效帧。

不要把 `.dmp` 当作普通附件跳过。

### 6. 建立时间线

先定位用户认为出问题的时间，再串联日志：

1. `mxu-web-*`：前端提交了什么任务和配置。
2. `mxu-tauri.log`：实例、控制器、资源、任务开始/停止、agent 生命周期。
3. `maafw.log` / `maafw.bak.*.log`：Pipeline 节点识别、动作、超时、回调。
4. `go-service.log`：Go 自定义逻辑、环境检查、业务日志。
5. `mxu-agent*.log`：agent stdout/stderr 和用户运行日志面板。
6. `on_error/`：错误现场截图。

优先锁定本次复现的 `task_id`，后续分析都限定到该任务实例。一个日志包里经常混有历史运行，不要串任务。

### 7. 回溯到 MDA 代码和配置

MDA 代码证据优先顺序：

- 任务入口、控制器限制、用户选项：
    - `assets/tasks/*.json`
    - `assets/interface.json`
    - `assets/tasks/preset/*.json`
- Pipeline 节点：
    - `assets/resource/pipeline/**/*.json`
- Go 扩展逻辑：
    - `agent/go-service/**`
- Schema / 协议：
    - `tools/schema/*.json`
- 本地化文案：
    - `assets/locales/interface/zh_cn.json`
    - `assets/locales/interface/en_us.json`

总结任务、选项、界面提示时，优先读取 `zh_cn.json` 的中文文案，不要直接把 task id 当成用户可见名称。

### 8. 必要时查上游

先用 issue、日志、MDA 仓库和 MaaFramework 文档做初步归因。只有证据指向上游实现或现有证据不足时，再看：

- MaaFramework：`https://github.com/MaaXYZ/MaaFramework`
- MXU：`https://github.com/MistEO/MXU`
- maa-framework-go：`https://github.com/MaaXYZ/maa-framework-go`
- maa-framework-rs：`https://github.com/MaaXYZ/maa-framework-rs`

不要为了普通 Pipeline 配置问题过早下钻上游源码。

## Log Map

### `maafw.log`

MaaFramework 核心运行时日志。

最适合看：

- Pipeline 是否按预期推进。
- `next` / `on_error` 是否命中。
- `Tasker.Task.Starting` / `Succeeded` / `Failed`。
- `Node.Recognition.Failed` 连续重复。
- `Node.Action.Failed`。
- OCR / TemplateMatch / ColorMatch 细节。
- 控制器、截图、资源加载问题。

### `maafw.bak.*.log`

滚动日志。用于：

- 当前 `maafw.log` 不够长。
- 复现发生在较早时间。
- 对比同配置的历史成功/失败样本。

不要把旧日志误判成最新复现。

### `go-service.log`

MDA Go Agent 日志。

最适合看：

- 自定义识别器/动作是否触发。
- taskersink 环境检查，例如 HDR、分辨率、进程检查、会员/额度逻辑。
- Go 参数解析、外部请求、业务扩展错误。

环境告警不一定是根因，必须和 `maafw.log`、现场截图一起判断。

### `mxu-tauri.log`

MXU Tauri/Rust 后端日志。

最适合看：

- 实例创建与资源加载。
- 控制器连接。
- 任务开始/停止。
- agent 生命周期。
- `maa_ffi` 回调。
- 用户是否手动停止任务。

### `mxu-web-YYYY-MM-DD.log`

MXU 前端日志。

最适合看：

- UI 是否加载了正确的 `interface.json`。
- 用户实际提交了哪些任务和配置。
- `pipeline_override` 是否符合用户选择。
- 配置持久化或界面状态问题。

日志包可能含多天文件，要优先看复现当天。

### `mxu-agent*.log`

MXU 捕获的 agent 子进程 stdout/stderr。

最适合看：

- 用户在运行日志面板看到的内容。
- 子进程 stdout/stderr 报错。
- MaaFramework 或 go-service 的辅助输出。

它有用但不是最权威的根因日志；细节仍优先看 `maafw.log` 和 `go-service.log`。

### `config/*`

配置快照。常见文件：

- `config/mxu-MDA.json`
- `config/maa_option.json`
- `config/maa_pi_config.json`

最适合看：

- 实际启用了哪些任务和选项。
- 控制器配置。
- 是否开启错误截图保存。
- 用户口述配置与实际配置是否一致。

### `on_error/`

MaaFramework 错误现场截图。

最适合看：

- 实际停留界面。
- 模板/OCR 是否识别错画面。
- 是否被弹窗、加载态、遮罩、分辨率、HDR 等干扰。

日志和用户描述冲突时，优先相信现场截图。

## How To Filter Evidence

1. 先从用户描述或 issue 拿锚点：版本、控制器、任务名、入口名、失败画面、复现步骤。
2. 在日志中找高价值信号：
    - `Tasker.Task.Starting` / `Succeeded` / `Failed`
    - `Node.Recognition.Failed`
    - `Node.Action.Failed`
    - `timeout`
    - `Warn` / `Error` / `Fatal`
    - agent 启动失败、断连、被停止
3. 锁定本次复现 task_id 后再看细节。
4. Pipeline 问题重点看 `maafw.log` 和 `mxu-tauri.log` 回调。
5. Go 扩展问题重点看 `go-service.log` 和 `mxu-agent*.log`。
6. UI / 配置 / 编排问题重点看 `mxu-web-*`、`mxu-tauri.log`、`config/*`。
7. 只摘足够支撑结论的关键片段，不要倾倒整份日志。

## Common Patterns

- `next` 列表中的识别连续失败直到超时：

    - 当前画面不在预期分支、模板/OCR 失配、漏了中间节点、弹窗打断、控制器资源不匹配。

- 某个兜底返回/退出节点连续成功但流程没有前进：

    - Pipeline 对当前状态判断错了，或回退动作本身就是错误行为。

- issue 文字说失败，但目标 `task_id` 最终 `Tasker.Task.Succeeded`：

    - 明确写“本日志未复现用户描述的失败”。
    - 如代码仍有脆弱点，单独标为潜在风险，不当作已证实根因。

- 用户日志流程与当前主线代码明显不一致：

    - 先确认用户版本。
    - 必要时按对应 tag/commit 复核旧逻辑。
    - 不要用当前分支直接否定旧版本日志。

- 奖励/确认弹窗节点识别和点击成功，但父流程没有验证弹窗消失：

    - 这是常见脆弱点；若本次没失败，写成潜在风险。

- `go-service.log` 只有 HDR / 分辨率 / 进程告警：

    - 通常是环境风险提示，不自动等同根因。

- `maafw.bak.*.log` 有同配置历史成功样本，本次失败：
    - 可作为行为回归或环境差异的强旁证。

## Localized Copy

输出任务、选项、界面提示时，优先查：

1. `assets/locales/interface/zh_cn.json`
2. `assets/locales/interface/en_us.json`

常见 key：

- `task.<TaskId>.label`
- `task.<TaskId>.description`
- `option.<OptionId>.label`
- `option.<OptionId>.description`
- `task.<TaskId>.option.<OptionId>.label`

如果中文 key 缺失，退回原始 id，并说明未找到对应文案。

## Linking Code Evidence

如果要指向具体代码行：

- 本地工作中可以用相对路径引用文件。
- 面向 issue 报告时，优先给远端 GitHub blob 行号链接。
- MDA 链接格式：
    - `https://github.com/1204244136/MDA/blob/<commit>/<path>#L1-L2`
- `<commit>` 使用本次分析依据的版本：当前 `HEAD`、用户版本 tag，或日志对应 commit。

## Output Format

最终回答推荐结构：

````markdown
## Issue / 日志概要

- issue / 日志包：`...`
- 版本 / 控制器：`...`
- 任务 / 相关选项：优先使用中文 label，必要时补 task id
- 用户现象：...

## 关键证据

<details><summary>点击展开</summary>

- `maafw.log`：...
- `go-service.log`：...
- `mxu-tauri.log`：...
- `mxu-web-*.log`：...
- `mxu-agent*.log` / `on_error`：...
- 代码依据：...

### DMP 崩溃分析

（仅当存在 `.dmp` 时输出；否则删除本节。）

- DMP 文件：`...`
- 异常类型：`...`
- 崩溃模块：`...`
- 符号化状态：...

```text
Frame 0: ...
Frame 1: ...
```

</details>

## 根因判断

- 直接结论：...
- 证据链：...
- 置信度：高 / 中 / 低

## 给用户的建议

- 用户现在可以尝试：...
- 是否建议升级 / 重下完整包 / 同步资源 / 重置配置：...
- 是否有临时绕过方案：...

## 修复方案

1. Pipeline / 配置 / Go 代码修复方向。
2. 需要补充的测试或日志。
3. 如属于不支持场景，说明应如何限制入口或改进提示。

## 给修复 AI 的建议（可复制）

```text
现象：
...

关键证据：
...

可能相关线索（待验证）：
...
```
````

## Reminders

- 发现 `.dmp` 必须执行 `dmp-analysis`。
- 不要只看一个日志文件下结论。
- 不要把维护者评论当唯一证据。
- 不要把环境告警自动等同根因。
- 日志与 issue 文字不一致时，明确说明“证据未复现”还是“证据已复现但用户表述不精确”。
- 如果证据表明问题已在新版本修复，明确建议升级。
- 如果怀疑安装包、资源或配置损坏，明确建议重新下载、同步资源或重建配置。
