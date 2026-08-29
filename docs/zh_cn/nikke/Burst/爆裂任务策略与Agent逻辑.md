# 「自定义爆裂」任务策略与 Agent 逻辑

> 对应 MDA 任务：`CustomBurst`（⚡自定义爆裂）。仓库角色：战斗中检测右侧爆裂面板，
> 按用户为每个爆裂阶段指定的角色快速释放爆裂。
>
> - 编写日期：2026-08-27
> - 依据：`C:\Users\12042\Desktop\sample` 中的阶段、冷却、充能和暂停样图（均为 1280×720）逐一测量与验证；
>   Go 路由决策另配有单元测试（`agent/go-service/customburst/customburst_test.go`）。
> - 状态：阶段/槽位/冷却及充能信号已按样图复核；路由决策单测通过；仍需真机复核最终时延。

## 一、背景与目标

自动模式下爆裂按约 500ms 间隔依次释放；手动释放几乎无间隔，可显著加快爆裂循环。
本任务用脚本模拟“手动释放”。内置框架「快速爆裂」（不可取消）负责识别右侧爆裂面板并快速释放；
自定义层让用户**为每个爆裂阶段（Ⅰ/Ⅱ/Ⅲ）各指定一个释放角色**，指定角色在冷却时等待其冷却结束，
而不是切换到其他就绪角色。当前轮三个阶段都未指定时，则直接进入高频 `A→S→D` 无限循环，
不扫描槽位/冷却，也不等待 `ReadySlots`。

释放键：第一个角色 `A`（65）、第二个 `S`（83）、第三个 `D`（68）。

### 层次结构（重要约定）

```text
CustomBurst（任务层，唯一入口 CustomBurstMain）
   └─ 「快速爆裂」FastBurst（底层框架，不可取消）
        ├─ 检测引擎：阶段 / 人数 / 冷却（Pipeline ColorMatch 子节点 FastBurst*）
        ├─ 识别汇总：FastBurstPanelRecognition（复用子节点）
        └─ 快速释放逻辑：整轮未指定时固定 ASD 循环；混合配置时未指定阶段取首个就绪槽位
```

- **「快速爆裂」是「自定义爆裂」的底层框架，不是并行的任务。**
    - 任务列表（`assets/interface.json`）只导出 `CustomBurst` 这一个任务；
    - `assets/resource/pipeline/Battle/CustomBurst.json` 只有 `CustomBurstMain` 一个入口（`next` 自环）；
    - `FastBurst*` 只是被识别器复用的检测原语（`ctx.RunRecognition` 调用），不是任务入口；
    - Go 侧只注册 `FastBurstPanelRecognition`（框架识别）与 `CustomBurstRouteAction`（任务动作）。
- 上层「自定义」依赖底层「快速爆裂」：用户为各阶段指定角色、等待冷却等，全部建立在底层检测之上；
  当前轮三个阶段均未指定时，改为直接执行固定 `A→S→D` 循环；只有混合配置中的未指定阶段
  才保留底层原始的“第一个就绪”兜底。

### 1.1 职责边界（Pipeline / Go）

与本仓库 `AGENTS.md` 及 MaaEnd `AGENTS.md` 约定一致：**「Pipeline 管流程，Go 管难点；识别留给 Pipeline」**。

| 层                                 | 职责                                                                                                                                                                | 承载                                               |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| **Pipeline**（识别 + 流程 + 动作） | 原子识别（六边形阶段色 / 槽位灰条 / 下沿黑色冷却，携带 ROI/颜色区间/count）；流程控制（入口等待战斗画面 → 循环 → 末尾判暂停/结算）；配置承载（`CustomBurstConfig`） | `assets/resource/pipeline/Battle/CustomBurst.json` |
| **Go 检测汇总**（识别结果汇总）    | 复用上述 Pipeline 子识别节点（`ctx.RunRecognition`）聚合出 `FastBurstResult`，只描述"看到了什么"（阶段/人数/冷却/就绪）                                             | `agent/go-service/customburst/detect.go`           |
| **Go 业务/路由**（难点）           | 读选项（`CustomBurstConfig.attach`）→ 按"当前轮 + 检测阶段"跟轴决策 → 路由释放/等待 → 轮状态机（面板消失推进下一轮）→ focus 输出                                    | `agent/go-service/customburst/customburst.go`      |

**边界检查**（对照约定）：

- 识别参数（ROI / 颜色区间 / `count` / `roi_offset`）全部在 Pipeline JSON，Go 未硬编码（Go 仅按节点名 `ctx.RunRecognition` 复用）；
- 流程控制（等待战斗、循环、判暂停/结算）全部是 pipeline 节点，Go 未写流程；
- Go 只承载"汇总 + 跟轴决策 + 轮状态 + focus输出"这类低代码难以表达的**难点**；
- Go 与 Pipeline 的唯一硬耦合点是**节点名**（子识别名、ClickKey 名、承载节点名），与 equipmentreroll 的 `scanSlotByPipeline` 一致。

## 二、任务与选项（多轮爆裂轴）

- 任务名：`CustomBurst`（自定义爆裂），入口 `CustomBurstMain`，分组 `realtime`。
- `task` 引用选项：`BurstRoundCount`（使用轮数）+ `BurstRound{1..6}Stage{1..3}`（各阶段角色，作为其子选项）。

> 「轮爆裂轴」以选项化呈现：**最多 6 轮，每轮 3 个阶段（Ⅰ/Ⅱ/Ⅲ），每阶段下拉选择释放角色（A/S/D）**。
> **默认只显示 1 轮**（顶部「使用轮数」下拉选择 1-6，按所选轮数渐进显示对应轮的角色下拉，避免 6 轮全铺开）。

| 选项                                      | 类型     | 默认   | 说明                                                                                                                  |
| ----------------------------------------- | -------- | ------ | --------------------------------------------------------------------------------------------------------------------- |
| `BurstRoundCount`                         | `select` | `1`    | 使用几轮（1-6）；选 N 则显示第 1..N 轮的角色下拉（渐进式）                                                            |
| `BurstRound{N}Stage{M}`（N=1..6, M=1..3） | `select` | 不指定 | 第 N 轮第 M 阶段（Ⅰ/Ⅱ/Ⅲ）释放的角色：不指定 / A / S / D                                                               |
| `BurstRoundCount` 子选项（各阶段）        | —        | —      | 指定角色**冷却时固定等待其冷却结束**（严格按轴）；当前轮全为「不指定」时直接 ASD 循环，混合配置的空阶段才回落槽位兜底 |

> 顶层只显示 `BurstRoundCount`；`BurstRound{N}Stage{M}` 作为 `BurstRoundCount` 各 case 的
> 子选项，按所选轮数渐进显示。未启用的轮（> round_count）引擎直接忽略。
> 各阶段下拉四档：**不指定 / A / S / D**，默认「不指定」。

### 2.1 配置承载点（选项 → Go）

沿用 equipmentreroll 的做法：选项经 `pipeline_override` 写入承载节点 `CustomBurstConfig`
的 `attach` 顶层键（MaaFramework 对 `attach` 按 key 浅合并，多个选项写不同顶层键互不覆盖）。
Go 通过 `ctx.GetNodeJSON("CustomBurstConfig")` 读取 `attach`。键约定：

- `r{N}s{M}`：第 N 轮第 M 阶段的角色键（如 `r1s1=A`，空=不指定）；`round_count`：使用轮数。

```jsonc
"CustomBurstConfig": {
    "attach": { "round_count": 2,
                "r1s1":"A","r1s2":"S","r1s3":"D",
                "r2s1":"A","r2s2":"A","r2s3":"S",
                "r3s1":"A","r3s2":"S","r3s3":"D",
                "r4s1":"A","r4s2":"S","r4s3":"D",
                "r5s1":"A","r5s2":"S","r5s3":"D",
                "r6s1":"A","r6s2":"S","r6s3":"D" }
}
```

选项 case 的 `pipeline_override` 形如：

```jsonc
{"CustomBurstConfig": {"attach": {"r2s3": "S"}}}
```

## 三、检测层（Pipeline ColorMatch + roi_offset，内置「快速爆裂」）

检测信号与坐标全部由 Pipeline ColorMatch 完成（不依赖 OCR），位置右上角（1280×720）。

| 信号   | 方法                                       | 坐标（绝对位置）                                                                  |
| ------ | ------------------------------------------ | --------------------------------------------------------------------------------- |
| 充能条 | `BURST` 横条的中性灰充能段                 | `roi [1192, 293, 80, 5]`，RGB `[155,155,155]`–`[185,185,185]`，`count=100`        |
| 阶段   | 六边形颜色（Ⅰ绿/Ⅱ橙/Ⅲ红）                  | `roi [1150, 278, 60, 34]`，三色取最大                                             |
| 人数   | 头像框右侧不透明灰条（三通道 ≤62、中性暗） | 槽1 `[1262,313,10,56]` / 槽2 `[1262,381,10,56]` / 槽3 `[1262,447,10,56]`          |
| 冷却   | 头像框下沿固定区域为黑色（占比≥50%）       | 槽1 `[1188,367,70,5]` / 槽2 `[1188,435,70,5]` / 槽3 `[1188,501,70,5]`（容差 ≤30） |

注意：`爆裂充能.png` 中右上角 `BURST` 横条才是充能信号；阶段Ⅰ/Ⅱ/Ⅲ的绿色、橙色、红色长条
不能作为充能条识别。充能识别只取横条内的中性灰段，避免与阶段颜色混淆。
**灰色充能段命中的意义是“已开始充能 → 切换到高频检测”，不是“可以按键”**；满充能后就绪时，
该横条会转为对应阶段色（如绿色Ⅰ），因此就绪信号需一并纳入（`Or(充能条, 阶段色, 面板)`）。

> 2026-08-27 18:11 实机：任务启动时爆裂已就绪（绿色Ⅰ），灰色充能段早已消失，若门槛只用灰色充能条会永远等不到。

细节：只测亮度数灰条在暗背景图上会把空槽也算成有人物，故头像灰条用**中性暗色**判据
（`lower [0,0,0]`、`upper [62,62,62]`、`count ≥ 300`），能把暗背景空槽正确排除。

Pipeline 节点文件：`assets/resource/pipeline/Battle/CustomBurst.json`
（检测子节点以 `FastBurst*` 命名，构成内置框架；`CustomBurstConfig` 为配置承载；`CustomBurstMain` 为任务入口）。

### 3.1 流程（入口 → 循环 → 末尾判暂停/结算，全部 Pipeline）

```
CustomBurstMain（入口：等待战斗画面；参考项目原有节点 BattleCombatHudVisible；不出现则正常超时）
  └─ 命中战斗画面 → CustomBurstWaitCharge（300ms 低频检测「充能中或就绪」：灰色充能条命中=已开始充能；阶段色出现=已就绪）
          └─ 任一命中 → CustomBurstLoop（高频爆裂循环）
CustomBurstLoop（循环体：识别 FastBurstPanelRecognition + 动作 CustomBurstRouteAction，
                 rate_limit=0、pre/post_delay=0）
   └─ next = [CustomBurstReturnToLowFrequency, CustomBurstSafetyGate, CustomBurstLoop]
CustomBurstReturnToLowFrequency：一轮面板消失后由 Agent 状态门单次命中，返回 CustomBurstWaitCharge；
   其 next = [CustomBurstWaitCharge, CustomBurstWaitSafetyGate]，等待充能期间也每 500ms 做一次暂停/结算检查
CustomBurstWaitSafetyGate（等待充能期低频暂停/结算检查门，复用 CustomBurstSafetyGateRecognition 节流）
   └─ next = [CustomBurstCheckPause, CustomBurstCheckSettle, CustomBurstWaitCharge]
CustomBurstSafetyGate（每500ms放行一次；未到间隔立即回到循环）
   └─ next = [CustomBurstCheckPause, CustomBurstCheckSettle, CustomBurstLoop]
CustomBurstCheckPause：OCR「暂停」（暂停菜单标题，roi [610,38,180,42]）；命中 → 输出 focus 并结束任务
CustomBurstCheckSettle：Or(BattleSettlementEscVisible, BattleFailedVisible)；命中 → 输出 focus 并结束任务
```

- 低频阶段只做轻量识别（`Or(FastBurstChargeBar, FastBurstStage)`），不调用 Go 面板汇总，降低平时性能开销；灰色充能条命中=进入高频（开始充能），或阶段色出现=已就绪（也进入高频）。进入高频后，整轮未指定的按键循环不读取槽位/冷却。
- 高频循环只做面板识别和发键；一轮爆裂面板消失后回到低频充能检测。
- `CustomBurstSafetyGate` 每 500ms 放行一次暂停/结算检查。放行后先试
  `CustomBurstCheckPause`（用户暂停）→命中则输出“已暂停”并视为完成，否则试 `CustomBurstCheckSettle`
  （战斗结算）→命中则输出“已结算”并视为完成，否则回到 `CustomBurstLoop` 继续。
- 完成类 focus 文案通过 pipeline 节点 `focus` 字段（Node.Recognition.Succeeded 事件）输出。

### 3.2 爆裂角色排序与槽位填充（重要，决定按键顺序）

> 这是配置「多轮爆裂轴」与兜底「快速爆裂」按键顺序的核心依据，务必按此理解。

- **每个爆裂阶段只显示该阶段的角色**。爆裂时间推进到阶段 Ⅰ/Ⅱ/Ⅲ 时，右侧爆裂面板**只展示当前阶段的角色**，
  不会混入其他阶段的角色（例如：Ⅱ阶段界面看不到Ⅰ阶段角色）。
- **该阶段的角色按队伍顺序从顶部槽位依次填充**，三个槽位固定对应按键：
    - 位置 1 → `A`（65）
    - 位置 2 → `S`（83）
    - 位置 3 → `D`（68）
- **槽位填充数量 = 该阶段角色数**：
    - 该阶段只有 1 名角色 → 只占位置 1，按 `A`；
    - 该阶段有 2 名 → 占位置 1、2，按 `A`、`S`；
    - 该阶段有 3 名 → 占位置 1、2、3，按 `A`、`S`、`D`。
- 因此「按键顺序」由各阶段的角色数决定，而不是固定的 A→S→D。例如队伍阶段构成为
  **Ⅰ:1、Ⅱ:1、Ⅲ:3**（1 名Ⅰ、1 名Ⅱ、3 名Ⅲ）时，节奏为：
    - 阶段 Ⅰ：唯一角色在位置 1 → 按 `A`；
    - 阶段 Ⅱ：唯一角色仍在位置 1 → 按 `A`；
    - 阶段 Ⅲ：3 名角色占位置 1/2/3 → 按 `A` 或 `S` 或 `D`（取你想释放的那名Ⅲ角色）。
    - 即指定轴的合法顺序可以是 `AAA`、`AAS`、`AAD`……；若第 1 名Ⅲ角色进入冷却，则轮到下一名Ⅲ角色。
- **整轮未指定时不读取槽位状态**：当前轮三个阶段都为空时，灰色爆裂条命中即进入高频循环，
  动作层直接按 `A→S→D→A...`，不扫描槽位、不检测冷却，也不等待 `ReadySlots`。
- **混合配置保留槽位兜底**：如果同一轮至少有一个阶段指定了角色，为避免固定 ASD 盲按穿透到指定阶段，
  该轮仍使用阶段驱动；其中空阶段取当前阶段第一个就绪槽位，指定阶段严格按配置键并等待冷却。
- **若想让特定阶段Ⅲ角色优先（或指定轮换）**：用配置轴 `r{N}s{M}` 为该阶段指定固定角色键（A/S/D），
  脚本会改用该键；若该角色还在冷却则等待（不替换）。
- **注意混合配置的发键时机需与检测阶段同步**：指定阶段仍需检测到目标角色就绪；混合配置的空阶段取首个就绪槽位。
  整轮未指定则不等待阶段/槽位确认，而是在高频循环中按固定 ASD 顺序发送。

## 四、Go 逻辑（汇总 + 依据选项路由动作 + focus 输出）

文件：`agent/go-service/customburst/customburst.go`

- **`FastBurstPanelRecognition`（自定义识别）**：复用 Pipeline ColorMatch 子节点（`ctx.RunRecognition`），
  汇总成 `FastBurstResult`（stage/transition_stage/count/present_slots/cd_slots/ready_slots/ready_key/ready_keys），以 JSON 为 Detail。
  `stage=0 && transition_stage=2/3` 表示上一阶段色已消失，按已确认的Ⅰ→Ⅱ或Ⅱ→Ⅲ转换窗口预测的可抢先释放阶段；
  它不伪造已观察到的阶段色。
  `Or` 的 `CombinedResult` 子项在 MaaFramework 中不提供 `Hit` 字段，阶段命中依据子项 `Box` 是否为空判断。
  **整轮未指定时不扫描槽位/冷却**：阶段识别只用于生命周期判断，按键不依赖 `ReadySlots`；面板真正出现后，
  必要的 `FastBurstAnySlot` 仅用于确认面板消失。混合配置中的空阶段仍逐槽位判断在场与冷却，取首个就绪。
- **`CustomBurstRouteAction`（自定义动作，引擎按检测到的阶段跟轴走）**：
    1. 读 `arg.RecognitionDetail`（检测结果）；读 `CustomBurstConfig.attach`（多轮轴 `r{N}s{M}`、`round_count`）。
    2. **轮状态**（按任务记录 `currentRound`、`wasPresent`、`lastStage`、`transitionStage`）：
        - **面板不在场**（无阶段色且无头像）= 一轮爆裂结束 → `currentRound = (currentRound+1) % round_count`，清空防重复标记，等待下一次面板出现。
        - **过渡动画抢发**：混合配置中，已确认Ⅰ/Ⅱ后，阶段色消失即按下一阶段（Ⅱ/Ⅲ）发配置键，不等待下一阶段颜色出现；
          未指定阶段不在过渡帧盲按，等待真实阶段确认后再取首个就绪槽位。整轮未指定时不走阶段预发，直接由 ASD 循环处理。
        - **生效确认与重试**：若预发后该阶段仍真实出现（matched）或预测后回到上一阶段（cancelled），由 `observeStage` 清除本阶段防重复标记并重发。**同一过渡窗口只预发一次**（首次进入时发键，后续过渡帧直接等待阶段确认），避免过渡内重复预发拖慢节奏；普通稳定阶段（`res.Stage>0`）未推进时仍按 `shouldRetry`（约 150ms）重试，不设置零时长 KeyDown/KeyUp。
        - **阶段值变化**（1→2→3，含过渡后恢复）：清空各槽位防重复标记（新的释放机会）。
        - 面板出现且 stage>0：取 `rounds[currentRound][现检测阶段S]` 的角色键，`routeAction` 决策：
            - 该角色**就绪**（在现阶段且未冷却）→ 释放（经 `ctx.RunAction` 直接执行 `ClickKey` 动作节点），记录“已释放待冷却”防重复；
            - 该角色**冷却中** → 固定等待其冷却结束（严格按轴，不替换）；
            - 该角色**未出现**（现阶段无此槽位）→ 报告目标未出现；
            - 当前轮三个阶段均**「不指定」**（空字符键）→ 高频 ASD 无限循环，不读取槽位/冷却；
            - 混合配置中某阶段**「不指定」**（空字符键）→ 「快速爆裂」首个就绪槽位兜底。
    3. **重置适配**：某些角色释放后把爆裂拉回某阶段，引擎不显式配置重置，而是**每帧重新检测当前阶段**，
       在该阶段继续释放本轮配置角色 → 自动适配 `AAAA`（多次同阶段释放）。
    4. 仅在实际**释放**时用 `maafocus.Print` 输出一条简洁信息（如 `第1轮·爆裂2阶段 · 按S释放`），其余帧静默，避免过渡/无角色等冗余刷屏。

### 4.1 发键方式（Pipeline `ClickKey` 动作节点，经 `ctx.RunAction` 直接执行）

按键动作由 Pipeline 的单个 `ClickKey` 动作节点（`FastBurstClickKey{A,S,D}`）承载。
Go 在 `pressKey` 里用 `ctx.RunAction("FastBurstClickKeyX", ...)` **直接执行该动作节点**（节点为纯 action，
无 recognition）。节点显式设置 `pre_delay=0`、`post_delay=0`，由 MaaFramework/控制器内部执行按下与抬起间隔。
当前 Win32 控制器路径的内置间隔约为 50ms，既避免过短按键被游戏吞掉，也避免手动 `KeyDown→KeyUp` 链的默认延迟。

```go
ctx.RunAction("FastBurstClickKeyA", maa.Rect{}, "", nil)  // 直接执行 ClickKey（内置按下/抬起间隔）
```

> **采用 `ctx.RunAction` 而非 `ctx.RunTask` 的原因（参考 MaaEnd 成熟实现）**：
> `ctx.RunTask` 会为 `FastBurstClickKeyX` 创建一条完整 PipelineTask（先 DirectHit 识别再动作），
> 在“每帧/快速连续触发”时会与宿主 `CustomBurstLoop` 在同一任务器上争用，导致嵌套任务阻塞、
> 任务无法被 `PostStop` 中断（2026-08-27 17:40 日志已复现死锁且无法手动停止）。
> `ctx.RunAction` 只执行动作本身，更轻量、无嵌套 pipeline，规避该死锁。
>
> `ClickKey` 的按下/抬起由控制器内部完成；节点名保持 `FastBurstClickKey{A,S,D}`，Go 路由无需感知控制器细节。

| 槽位 | 节点                 | 键码 |
| ---- | -------------------- | ---- |
| 1    | `FastBurstClickKeyA` | 65   |
| 2    | `FastBurstClickKeyS` | 83   |
| 3    | `FastBurstClickKeyD` | 68   |

### 4.2 路由决策表（多轮轴，已被单测覆盖）

示例轴 `AAA|AAS`，引擎按确认或预测到的阶段取“当前轮该阶段配置的角色”：

| 当前轮    | 检测（阶段/槽位/冷却） | 决策                           |
| --------- | ---------------------- | ------------------------------ |
| 第1轮 AAA | Ⅰ 就绪[A]              | 释放 A                         |
| 第1轮 AAA | Ⅱ 就绪[A,S]            | 释放 A                         |
| 第2轮 AAS | Ⅲ 就绪[A,S,D]          | 释放 S                         |
| 第2轮 AAS | Ⅲ S 冷却               | 等待 S                         |
| 第2轮 AAS | Ⅲ S 就绪但刚释放过     | done（不重复释放，等 CD 显示） |
| 任意轮    | 当前轮三阶段均不指定   | 高频循环直接按 `A→S→D→A...`    |
| 任意轮    | 混合配置中的阶段不指定 | 快速爆裂兜底（第一个就绪）     |
| 任意轮    | 面板消失               | 本轮结束 → 推进到下一轮        |

## 五、验证

- **检测层**：阶段/槽位/冷却样图按 ColorMatch 参数复核通过；`爆裂充能.png` 的 `BURST` 横条命中，阶段样图不命中充能条 ROI。
- **路由层**：`go test ./customburst/` 通过，覆盖指定轴、混合配置兜底以及整轮 ASD 循环。

## 六、待办与风险

- **发键执行实机复核**：当前经 Pipeline `ClickKey` 节点发键（`FastBurstClickKeyA/S/D`）。
  需真机记录阶段 Ⅰ→Ⅲ 的端到端耗时；控制器内部点按间隔作为基础人类化时长，不再额外叠加固定等待。
- **运行日志基线**：2026-08-27 最新 `install/debug/maafw.log` 中，三次首次阶段识别为
  `12:59:19.832`（Ⅰ）、`12:59:20.585`（Ⅱ）、`12:59:21.422`（Ⅲ），Ⅰ→Ⅲ为约 1.59s；
  每次 `ClickKey` 的 `key_down`→`key_up` 为约 51–52ms，识别循环约 150–220ms 一次。
  因此主要耗时来自游戏自身的阶段动画/状态切换（Ⅰ→Ⅱ约 0.75s、Ⅱ→Ⅲ约 0.84s），不是 ClickKey 或人为 `rate_limit`。
  当前改为在Ⅰ/Ⅱ阶段色消失时预发下一阶段键，并通过下一帧阶段变化确认；`go-service.log` 会记录
  `burst key dispatched` 的 `observed_stage` 与 `decision_stage`，便于实机对照预发是否被游戏接受。整轮未指定爆裂轴时
  不走阶段预发，灰色充能条命中后直接按 ASD 循环；混合配置的空阶段不在过渡帧盲发，等阶段确认后按首个就绪槽位。
  暂停/结算 OCR 改为每 500ms 一次，不再占用每次爆裂转换的高频路径。
- **发键方式（检测驱动，不用定时盲发）**：此前尝试过“固定间隔盲发”（fastseq），但在“对已冷却/无就绪槽位仍强发
  `ctx.RunTask`”时会导致框架任务器阻塞，任务无法被 `PostStop` 中断（2026-08-27 17:40 日志已复现：用户无法手动停止），
  且 fastseq 用“固定间隔/序列序号”发键导致 `decision_stage` 与 `observed_stage` 错位（如 observed=1 却按了“阶段2”）。
  故已回退为**检测驱动**：
    - 仅在“检测到当前阶段 + 存在就绪角色（或配置键就绪）”时才发键，绝不对冷却槽位强发；
    - `decisionStage` 取真实检测阶段（`res.Stage`/预测过渡阶段），不再用枚举序号；
    - 整轮未配置轴：不取 `res.ReadySlots`，直接按 `A→S→D` 循环；混合配置空阶段取 `res.ReadySlots[0]`，配置轴用该阶段配置键，若该角色在 `CDSlots` 则等待（不替换）。
      这样既避免了死锁与错位，也保留了混合配置下的按 CD 动态轮换。
- **参考 MaaEnd 的两个安全点**：
    - 动作每次执行前检查 `ctx.GetTasker().Stopping()`，收到停止信号立即返回，保证手动停止能及时生效；
    - 高频/紧凑动作用 `ctx.RunAction` 直接执行动作节点（而非 `ctx.RunTask` 跑整条 pipeline）。
- **兜底防重复**：混合配置的未指定阶段仍同一阶段只释放一次。头像/冷却动画可能在下一帧暴露另一个就绪槽位，
  这类槽位会被标记为本阶段已消费，避免一次爆裂轴额外释放第 4 个键；整轮 ASD 循环不依赖该状态。
- **最终阶段（Ⅲ）matched 不再 reset**：预发Ⅲ即触发全爆裂，`observeStage` 返回 matched 时不再清防重复标记，
  避免在全爆裂播片期间冗余重发同一键（日志里曾出现“Ⅲ阶段两次释放”）；若预发未登记，由 `shouldRetry`（重试间隔后）兜底重发。
- **等待充能期暂停/结算检测**：`CustomBurstWaitSafetyGate` 复用 `CustomBurstSafetyGateRecognition` 节流，
  在 `CustomBurstReturnToLowFrequency` 的 next 里每 500ms 放行一次 `CustomBurstCheckPause`/`CustomBurstCheckSettle`；
  修复“一轮爆裂后面板消失、进入低频等待充能时用户暂停无法自停”的问题。
- **冷却检测（已验证）**：按"头像框下沿固定区域黑色占比≥50%（容差≤30）"判定冷却。经 5 张 sample 图验证（正常下沿约 0-20% 黑色，冷却约 69%）。区域像素 350，`count=175`（50%）。
- **防重复释放**：`pressed` 状态在观察到槽位出现冷却时重置；若游戏冷却显示过短导致状态滞留，
  可加入“连续就绪超阈值次则强制再释放”的兜底。
- **多轮引擎边界**：以"面板消失=本轮结束、推进下一轮"作为轮次推进信号（已确认）。重置（角色把爆裂拉回某阶段）
  依赖引擎"每帧重新检测阶段跟轴"实现；若某角色的重置目标阶段在轴上后续步骤里没有对应配置，该轮会等待该阶段出现。
  请在轴上按实际重置行为配置对应阶段的角色。
- **锚点盒**：`FastBurstSlotNCDDark` 通过 `roi` 引用槽位灰条节点计算相对位置；若 ColorMatch
  返回框偏小导致偏移不准，可改为绝对 `roi [1180, top, 76, 54]`。
- **坐标一致性**：以上均为 1280×720 基准。
