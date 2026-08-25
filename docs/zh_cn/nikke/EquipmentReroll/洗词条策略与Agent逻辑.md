# NIKKE 洗词条策略与 MDA 实现逻辑

> 本文件是供开发 Agent 理解需求并编写 MDA 任务的策略规格，描述“洗装备”和“洗角色”的目标、锁定策略、概率决策及伪代码。实际读取界面、计算结果和执行点击的是 MDA 运行时，不是 Agent。游戏内效果名称、数值与 UI 文案以[装备系统与洗词条研究](装备系统与洗词条研究.md)为准；概率模型与期望消耗计算见[洗词条概率与期望计算](洗词条概率与期望计算.md)。

- 整理日期：2026-08-19
- 状态：已实现自定义词条配额的识别、决策与效果变更流程
- 目标：让开发 Agent 据此实现 MDA，使 MDA 能先读取既有词条，再按四件装备的全局配额执行效果变更

## 0. 总原则

### 0.1 只处理 T10 装备效果

本文的“洗词条”只包含 T10 装备界面的“效果变更”，目标是改变效果名称及其栏位组合。“重新设置数值”属于洗数值，不在本任务范围内；“装备强化”和“T9 → T10 改造”也是独立流程，不得被本任务触发。游戏内详情页标题仍显示英文“OVERLOAD”，社区通常把这种装备称为“T10 装备”或“T10”。

装备改造为 T10 时会自动生成初始效果，这应视为已经进行过一次初始词条生成。因此洗词条**不可能从零开始**：进入任务后必须先读取四个部位当前已有的效果名称、数值、“未获得效果”栏位和锁定状态，再决定是否操作。效果数值和 Tier 不参与本任务的配置、评分或结果页决策（全量扫描仍会读取数值并在日志中输出，便于观察）。

### 0.2 结果页的接受方向

“OPTION CHANGE”结果页中：

- 点击“效果变更”会接受蓝色的“变更效果”，写入装备；
- 点击“效果维持”会放弃变更，继续使用橙色的“当前效果”。

MDA 的结果判定逻辑必须比较“当前效果”和“变更效果”，不能把“达标”机械地映射成某一个固定按钮。通常是：候选结果满足目标且优于当前结果时点击“效果变更”；候选结果不满足目标或破坏全局配额时点击“效果维持”。

## 1. 任务层级

### 1.1 洗装备

洗装备只负责一件已经是 T10 装备的局部目标。它接收：

- 目标装备及当前三个栏位；
- 每个栏位的目标规则，或“任意栏位满足”的目标规则；
- 一个或多个目标效果名称及其“或”/“与”关系；
- 是否满足后锁定；
- 订制模块、自订密钥库存和锁定材料策略。

洗装备不负责决定另外三件装备需要什么效果。

### 1.2 洗角色

洗角色是洗装备的上层封装：它同时管理角色的四个部位，并可以附加跨装备的数量目标。例如“四件装备各至少一条优越代码伤害增加”，或“四件装备合计需要 4 条优越代码伤害增加、4 条攻击力增加、1 条最大装弹数增加、3 条蓄力速度增加”。

洗角色不能简单退化为依次完成四次洗装备。它复用洗装备的单步操作逻辑，但调度权始终在角色层：每次只选择一件装备进行一次效果变更，随后重新比较四件装备。使用全局配额时，还必须统一分配已经获得的效果，不能让每件装备各自独立判断。

### 1.3 先全量扫描，每次效果变更后重新调度

> 实现层说明：首次全量扫描一次后，每次效果变更的结果会直接刷新 Go 词条快照，
> “重新调度”基于快照完成，**不会重复全量扫描四件装备**（详见 8.3）。

T10 改造已经自动生成初始效果，四件装备实际进入任务时几乎必然处于不同状态。角色任务不得按头、身、手、腿的固定顺序直接开始，也不得选中一件后持续洗到完成。正确的调度单位是**一次效果变更**：

1. 启动时一次性读取四件装备的三个栏位、效果名称、数值、“未获得效果”和现有锁定状态；
2. 用四件装备的当前词条建立全局配额分配，计算当前总价值；
3. 为每件装备枚举下一次“效果变更”动作，包括应锁栏位和锁定材料；
4. 估算每个动作的长期价值 `Q`；主比较目标完成概率和预算结束时的预期词条结构分，再比较订制模块保留能力与即时收益率；
5. 只对选中的装备执行一次“效果变更”；
6. 比较结果页的当前效果和变更效果，选择价值较高的一侧；
7. 无论是否接受候选结果，都重新读取该装备和锁定状态，更新库存与全局配额，然后回到步骤 3。

这相当于不断提高四件装备的期望总值，而不是“先把一件从 90 分反复洗到 100 分，再处理 60 分装备”。当 90 分装备的下一次提升概率很低、材料成本很高，而 60 分装备有大量容易命中的改进结果时，应暂停前者并选择后者。每次效果变更后重新评分，评分相同才使用用户顺序作为平局处理。

“最赚”不是按部位名称或当前总分直接判断，而是比较每一个**下一步动作**：

```text
状态长期价值 V(S) = 字典序向量：
  1. 在剩余预算内完成全局目标的最大概率
  2. 预算耗尽时的预期角色词条结构分
  3. 剩余订制模块可支持的预计效果变更次数
  4. 锁定材料效用（强烈惩罚把订制模块用于锁定）
  5. 预计剩余锁定能力

动作长期价值 Q(S, action) = Σ[结果概率 × lexicographicMax(
    V(维持当前效果后的状态),
    V(应用变更效果后的状态)
)]

动作即时期望增益 = Σ[结果概率 × max(
    候选结果当前结构分,
    当前结果当前结构分
)] - 当前结构分

动作成本 = 本次效果变更必需的订制模块
           + 本次锁定选择的订制模块或自订密钥
           + 自订密钥一次锁导致的后续重复锁定预期成本

动作即时收益率 = 动作即时期望增益 / 动作成本
```

`lexicographicMax(...)` 表示按上述字段依次比较，结果页允许放弃较差候选。因此在识别和点击正确的前提下，装备状态只会改善或保持，材料库存则持续下降。MDA 主排序使用动作长期价值 `Q`；即时收益率用于近似计算或同价值动作的辅助排序。3 → 2 → 1 的“先苦后甜”用于生成和评价单件装备的锁定方案，但不要求连续处理同一件装备。

## 2. 订制模块保留与自订密钥优先锁定

### 2.1 材料用途边界

- “效果变更”只能消耗订制模块；自订密钥不能代替本次洗词条费用；
- “效果锁定”才可以在订制模块和自订密钥之间二选一支付；
- 订制模块锁定持续到手动解除，自订密钥锁定只保护下一次效果变更；
- 使用自订密钥完成一次效果变更后，无论结果页选择“效果维持”还是“效果变更”，下一轮都必须重新评价并重新锁定。

**决策与执行的材料边界（关键）**

- **决策/规划侧**：材料库存视为**无限**，不参与决策。理由：用户材料不足就不会运行任务，所以规划时统一按成本最低口径（**自订密钥**，锁定获取成本 0）估算。
  - 实现：`planningMaterialForPart` 忽略库存、统一返回 `自订密钥`；`consumePlanningLockMaterial` 决策期不扣减、始终可支付；`chooseBestPartForQuota` 不做 `CanAffordReroll` 过滤；`planningInventoryCacheKey` 固定为 `#inventory:infinite`。
  - 因此 `expectedModulesForQuota` / `chooseBestPartForQuota` 的期望成本**不随库存/密钥有无变化**（对应测试 `TestExpectedModulesForQuotaIgnoresInventory`）。
- **执行侧**：材料**切换逻辑保留**（不影响决策）。真实执行锁定仍按“有自订密钥用密钥、不足/不够再切订制模块”（`selectLockMaterial` / `ChooseLockMaterial`），并做库存门禁与“cannot afford any lock material”提示；效果变更始终扣订制模块。
- **分工**：决策模型只回答“洗哪件/锁哪槽/要不要锁”，用无限材料算期望成本；执行层才根据真实库存选择实际材料并扣费。

### 2.2 启动时先读取两种库存

无论运行“洗装备”还是“洗角色”，MDA 都必须先读取并记录：

```text
initialCustomModules = 订制模块初始库存
initialCustomLockKeys = 自订密钥初始库存
```

“洗装备”随后读取目标装备，“洗角色”随后扫描四件装备并建立全局计划。订制模块需要同时承担“必需的效果变更成本”和“可选的锁定成本”，而自订密钥只能用于锁定。实际库存通常是订制模块先耗尽，因此规划时必须优先保留订制模块用于后续效果变更。每次操作后虽然只需要重新读取发生变化的装备，但两种材料库存都要同步更新。

### 2.3 锁定材料的高权重偏好

锁定材料不再追求均匀消耗。只要自订密钥足够，锁定动作应给自订密钥很高的优先权重，以避免锁定额外占用订制模块并缩短可继续洗词条的次数。

```text
moduleOperationCapacity = estimateSupportedEffectChanges(
    remainingCustomModules,
    currentPermanentLocks,
    expectedFutureLocks
)

lockMaterialPenalty(action) =
    modulesSpentForLocking(action) * HIGH_MODULE_LOCK_WEIGHT
    + normalizedKeysSpentForLocking(action) * KEY_LOCK_WEIGHT
    + expectedFutureRelockCost(action)

HIGH_MODULE_LOCK_WEIGHT >> KEY_LOCK_WEIGHT
```

`HIGH_MODULE_LOCK_WEIGHT` 是实现内的高权重，不是用户开关。它首先保护订制模块可支持的后续效果变更次数，再评价两种锁定方案的长期成本。默认情况下，只要自订密钥能够支付本轮锁定，就应优先生成并选择自订密钥分支。

这个偏好不是无条件硬编码。订制模块锁定是持续性的，自订密钥锁定是一次性的；如果同一栏位预计还要被保护很多轮，永久锁可能避免大量重复密钥成本。只有当动态规划确认永久锁带来的长期收益足以抵消订制模块的高稀缺权重，并且不会降低目标完成概率时，才允许选择订制模块锁定。自订密钥不足时，则自然回退到可支付的订制模块锁定方案。

### 2.4 单次动作的计费顺序

一次候选动作按以下顺序计算材料：

1. 读取该装备已有的永久锁；切换到其他装备再回来不会解除永久锁，也不能重复支付已有锁的成本；
2. 计算本轮需要新增的锁定栏位。若从 0 锁增加到 2 锁，锁定成本按 `0 → 1`、`1 → 2` 两步累计；
3. 根据全局库存规划，为每一个新增锁分别选择订制模块或自订密钥；若连续新增两条锁，允许两步使用不同材料，并枚举加锁顺序；
4. 加锁后，按本次实际受保护的栏位数计算“效果变更”成本：0 锁消耗 1 个订制模块、1 锁消耗 2 个、2 锁消耗 3 个；
5. 结果页处理完毕后，永久锁继续存在，自订密钥新增的锁全部失效；两种情况下效果变更所消耗的订制模块都不会返还。

因此，动作状态必须分别保存 `permanentLocks` 和 `temporaryLocks`。`desiredLocks` 已经全部包含在 `permanentLocks` 中时，`lockSteps=[]`，只支付本次效果变更成本。需要新增两条锁时，`lockSteps` 必须保留栏位、材料和先后顺序，不能只保存一个无序集合。

角色级配额变化后，已有永久锁也可能不再值得保留。候选动作应允许先解除不再贡献目标的永久锁，再为本轮重新规划锁定；解除不会返还先前消耗的订制模块。解除操作是否另有 UI 确认或成本，落地前必须用客户端核对；若无法可靠确认，MDA 不得自动解除。

## 3. 洗装备的完全自定义

### 3.1 栏位规则

自定义配置应允许分别指定 1、2、3 号栏位的目标。没有目标的栏位用 `null` 表示；同一栏位内使用“或”逻辑：

```text
栏位 1：优越代码伤害增加 或 攻击力增加
栏位 2：最大装弹数增加
栏位 3：蓄力速度增加 或 暴击率增加
```

建议的内部形式如下：

```json
{
    "slotRules": [
        {"anyOf": [
                "优越代码伤害增加",
                "攻击力增加"
            ]},
        "最大装弹数增加",
        {"anyOf": [
                "蓄力速度增加",
                "暴击率增加"
            ]}
    ],
    "lockSatisfied": true,
    "slotOrder": [
        3,
        2,
        1
    ]
}
```

`anyOf` 是“或”，需要全部满足时使用 `allOf`。配置只接受效果名称；未赋予效果的栏位永远不匹配任何效果规则。

### 3.2 默认锁定顺序与加锁原则：先苦后甜（饱和）/ 拿到就锁（非饱和）

栏位/效果概率见[洗词条概率与期望计算](洗词条概率与期望计算.md)。在没有更具体的用户配置时，默认按“难度序 3 → 2 → 1”（3 号最难出，故先攻）决定锁定。按“本件负责的配额效果数量”分两种口径（与实现 `DesiredLockSlotForQuota` 一致，用 `allocateQuotaRequired` 得到本件负责集合）：

- **饱和配额（本件负责 ≥3 种，即每槽都需填配额；如 优4/攻4/装弹4 → 每件 优1/攻1/装弹1）——先苦后甜**：
  - 先便宜刷最难的 **3 号**（当前未持本件负责效果的最高难度槽），落地才锁；**不先锁已有效的低优先槽**（如 2 号），避免把“赌 3 号与 1 号一次凑齐”的成本抬到每刷 2 模块；
  - 拿到 3 号后立即锁它，再处理 2 号并锁，最后让 1 号补上剩余一种（策略上**不锁 1 号**：锁 1 追 23 代价高——1 号 100% 易得、锁它反而抬重洗成本，真正难保的是 2/3 号）。
- **非饱和（本件负责 1~2 种）——拿到就锁**：
  - 只要 2/3 号栏位里有一个槽持有“本件仍需要提供”的配额词条，就把它锁定（先锁难出的、后锁易出的）；不要因为已锁一个就停止加锁，后续又拿到另一条仍需要的配额词条也要立刻加锁；
  - 目标是 2 条有效：若 2、3 号任一先出现有效词条，就锁定该栏位，再洗剩余未锁定栏位；达到两条后立即结束。若当前只有 1 号栏位有效，不要先锁 1 号再追 2、3 号栏位；
  - 目标是 1 条有效：可以接受任意栏位命中，达到目标后结束这件装备，不需要为了把词条移动到 1 号栏位而继续操作。

> **一条装备不能有两种相同效果**（游戏规则，见 §1.3）。已锁定的效果“占用”了这条装备的名额，因此在其余槽位的抽取池中会被排除——所以绝不可能出现“锁了两条攻击”或“锁了攻击又洗出攻击”。**多数量目标（如“两条装弹”）必然要落在两件不同装备上**。这条规则由游戏天然保证，策略层面无需额外“去重”，但必须在建模与调度中遵守。

> 模拟校验（单件、达到“三槽各含 优/攻/装弹”，详见 §8.5 模拟数据）：**从不锁**约 650 模块；**只锁已有效槽(如 2 号装弹)、然后赌 3 号与 1 号一次凑齐**约 230 模块；改**先苦后甜（先便宜刷 3 号，落地才锁）**降到约 **44 模块**（渐进去重“锁已有效槽并锁 3”约 49）。因此**饱和配额下应优先“先苦后甜”**（便宜刷最难槽，避免把赌一手抬成 2 模块/刷）；**非饱和下再用“拿到就锁”**（锁已落地、追剩余）。

“先苦后甜”不是强制覆盖用户的栏位规则。若用户明确写了“3 号栏位必须是某效果”，必须服从该规则；若用户只要求数量，则按栏位出现概率和剩余目标数量选择顺序。

### 3.3 只执行效果变更

- 目标效果类型不满足时，使用“效果变更”；
- 本任务不处理“重新设置数值”，也不因数值不足、Tier 较低或想刷新数值而继续效果变更；
- 结果页只比较当前效果和变更效果的效果名称、栏位组合及全局配额贡献；
- 发现目标已经满足时，立即停止该装备或从角色级候选集中移除，不为了刷新数值自动继续洗。

## 4. 自定义词条配额（唯一模型）

当前 MDA 只支持**自定义词条配额**，不再提供模板选择。角色模式（`EquipmentRerollMode=Character`）下，
9 种效果各有一个独立 select（`EquipmentRerollQuota<Effect>`），直选该效果的需求数量：

- `禁止`：禁止词条。任意装备出现该效果都视为整体目标未完成，策略会尽量把它洗成其它效果；
- `不要求`：不要求；
- `需求 1` ~ `需求 4`：需求数量（单件装备不能出现重复效果，单效果上限 4 = 四件装备各一条）。

全部正数配额合计必须在 `1-12` 条之间（Go 端 `quotaIsValid` 校验）。

> 接口统一说明：角色配额的每个效果 select 与单件模式的"需求词条行"select 一样，case 直接写死语义值，
> 配置统一写入承载点 **`EquipmentRerollLockNeed.attach` 的扁平顶层键**（模式 `mode`；单件部位 `part`；
> 角色 `quota_<效果名>`；单件 `want1/2/3`、`slot1/2/3`）。选择 attach 是因为 MaaFramework 对同一节点多次
> override 时 `custom_recognition_param` 是**整体替换**（后覆盖会丢失前值），而 attach 顶层键按 key 合并互不覆盖。
> 其余组件（AllSatisfied / ChoosePart / ResultPage / KeepLockCheck / SingleDecide 等）
> 统一经 `loadCarrierConfig` 从承载点读取，不再各自接收 override。
> 范围说明：需求数量用连字符 `1-4`/`1-12`，不要用波浪号 `1~4`/`1~12`——部分界面/字体把 `~` 连在数字间
> 渲染成类似**删除线**的横线。

**常见示例：**

| 习惯叫法                          | 等价全局配额                                                       |
| --------------------------------- | ------------------------------------------------------------------ |
| 四优                              | 优越代码伤害增加 4                                                 |
| 四优+装弹                         | 优越代码伤害增加 4、最大装弹数增加 1                               |
| 攻 4 + 优 4                       | 攻击力增加 4、优越代码伤害增加 4                                   |
| 攻 4 + 优 4 + 装弹 1 + 蓄力速度 3 | 攻击力增加 4、优越代码伤害增加 4、最大装弹数增加 1、蓄力速度增加 3 |

“装弹”“蓄力速度”等只是展示简写，内部识别必须使用官方效果名称“最大装弹数增加”“蓄力速度增加”，不能把简写直接当作 OCR 目标字符串。

### 4.1 配额如何决定单件是否完成

- 每件装备的 3 个栏位按当前词条累计消耗全局配额；
- 只要还有尚未满足的正数配额，就继续洗；所有正数配额都被四件装备满足后才结束；
- 若当前装备已把所有“仍需要的效果”组合齐全，则该装备不锁不洗，交由全局调度选择其它装备；
- 若某个被禁止的效果（`-1`）出现在任意装备上，整体视为未完成，需要洗掉该效果。

### 4.2 示例：四优（优越代码伤害增加 4）

四件装备分别至少出现 1 条“优越代码伤害增加”。开始前先扫描四件装备，每次从尚未满足目标的装备中选择长期价值 `Q` 最高的下一步动作；任意栏位出现该效果即接受结果，该装备不再生成后续动作。不需要锁定，因为该装备已经达到目标，继续洗只会增加风险和消耗。

### 4.3 示例：攻 4 + 优 4

全局配额为攻击力增加 4、优越代码伤害增加 4。每件装备按 §3.2 的“先苦后甜/拿到就锁”生成锁定方案：先在 2 号或 3 号栏位取得其中一种效果并锁定，再在剩余未锁定栏位追另一种效果；不应先锁 1 号栏位再追 2、3 号栏位。**若某件装备目标是 3 条有效（如 优4/攻4/装弹4 时每件各 优1/攻1/装弹1，属饱和配额，见 §4 与模拟），同样按“先苦后甜”逐步锁定——先便宜刷最难的 3 号、落地再锁；只锁住已有效的 2 号然后赌 3 号一次凑齐是次优的（见 §8.5 模拟数据）。** 角色级调度器每次效果变更后重算四件装备的动作长期价值，让容易提高的装备先追上，再回头处理最难的剩余缺口；任何一件装备都只是“本轮未被选中”，不应因为暂停而被永久标记为完成。

#### 3 号栏位的核心词条价值

在“攻 4 + 优 4”这类需要同件装备凑齐多个核心效果的配额中，3 号栏位的价值不是只看“是否有效”：

- 3 号栏位是攻或优：最难出现的栏位已经承担一个必选核心；1、2 号栏位只需再容纳另一个核心和任一尚缺的补充词条，后续组合空间更大；
- 3 号栏位是补充词条：虽然计入全局配额，但 1、2 号栏位必须恰好同时容纳攻和优，后续成功条件更窄；
- 所以在其他条件相同时，`3 栏攻/优` 的状态分数、锁定优先级和保留优先级都必须高于 `3 栏补充有效`。

以“补充词条仍可为最大装弹数或蓄力速度”为例的基线概率见[洗词条概率与期望计算](洗词条概率与期望计算.md)。具体概率会随剩余全局配额和已锁效果变化，但前者的结构价值更高这一判断不变。

### 4.4 宏观分配与不饱和配额

> 本节与 §8.5（锁定流程）一起说明“全局联合分配”。它是把 4 件装备当作**一个整体**、按全局配额分配“每件应该提供哪些效果”的模型。对**饱和配额**（每件都恰好补齐所有必需效果，如 4/4/4）= 每件 优1/攻1/装弹1，无需宏观分配；对**不饱和/不均匀配额**（如 4/4/1、4/4/1/1）则必须宏观分配。

**为什么不能按“每件独立补齐其余件尚未满足的效果”分解**

旧口径按“每件独立计算其余三件尚未满足的效果集合”来要求该件补齐，凡某个效果全局仍缺就会被每件都判定为“该件也要补”。对稀缺配额（配额 < 件数，如 装弹1、蓄力速度1）会造成**每件都认为要补该效果**——实测“四件均 优@1/攻@2/蓄力@3、全局 优4/攻4/装弹0”时，配额 4/4/1 与 4/4/4 都被算成一样的 **988.76**，显然错误（4/4/1 只需 1 条装弹，应远便宜）。

**分配感知模型（`allocateQuotaRequired`）—— 承载者“涌现式 + 成本交换”分配**

把“全局正数配额 Q[e] 的**持有名额**”分配到各件（每件至多 1 条，同件不重复），采用“角色阶梯 + 成本交换”的口径：

- **满配额效果**（Q[e] >= 件数，如 优4/攻4）：分给全部件。
- **稀缺配额效果**（Q[e] < 件数，如 装弹1）：用**显式交换判定 `pickCheapestCarriersForEffect`**，按“**补齐该件 required（assigned + e）的期望成本最低**”选出 `Q[e]` 件作承载者——**成本更低者晋升为承载者、原承载者退行（交换/冒泡）**。角色（slot3 命中与否）只是观察，通常 slot3 命中者成本最低（难出的 3 号槽已填），但若某件更接近完整（已有 优+攻 只差装弹）成本可能更低，便会与之交换。
- **每件承载稀缺数量 ≤ 容量 = `maxSlot − 满配额数`**（满配额占槽，剩余可承载多个稀缺）。
- **承载者之外的件退行**为 `{优,攻}`（仅满配额效果，不额外承担稀缺效果）；已持有但**超额**（超过 Q[e]）的件不被要求保留 e（可被洗掉）。

因此 4/4/1 的分配为：`assigned[承载者] = {优,攻,装弹}`，其余三件 `assigned = {优,攻}`——只有 1 件需要装弹，其余件不追稀缺装弹；承载者随各件状态每步重算而“流转”。实测 `expectedModulesForQuota`：4/4/1 = **247.19**，4/4/4 = **988.76**（稀缺配额显著更低，不再等同）。

**调度优先级（`chooseBestPartForQuota`，先找承载者 → 洗未完整非承载者探索交换 → 补完整承载者 → 兜底强选）**

- **无天然承载者**（无 slot3 命中）→ 只考虑“**未完整非承载者**”（**降格者 + 未定者**；洗它可翻盘 slot3 成承载者，故找承载者阶段也不排除降格者）；
- **已有天然承载者** → **不急着补完整承载者**，先洗“**未完整非承载者**”（**降格者 + 未定者**；洗它们可能翻盘成更优承载者，若期望收益超出当前承载者则与之**交换/冒泡**，或直接补好 优/攻）——避免“找到承载者就闷头补完整”造成边际效益递减；角色是阶梯（未定→降格→承载），**交换机制对所有非承载者角色同样生效**（内核相同）；
- 无未完整非承载者 → 再**补完整承载者**（其 1、2 槽还缺 优/装弹）；
- 仍无 → 回落到**任意未完整件**（兜底强选 / 继续推进）。
- 已**完整**（持有全部 assigned）的件不洗（洗了会破坏已有成果）。

**多稀缺效果（如 4/4/1/1/1/1，及任意配额）**

- 每个稀缺效果（quota[e] < 件数）各分给若干承载者；**每件承载稀缺效果的容量 = `maxSlot − 满配额数`**（满配额占槽，剩余槽可承载多个稀缺）。
- 因此：优4/攻4 为满配额（占 2 槽）时容量=1（每件至多 1 个稀缺）；仅 优4 满配额（占 1 槽）时容量=2（同件可承载 2 个稀缺）。每件配额效果总数恒 ≤ 3（3 槽上限）。
- 分配顺序仍按“slot3 命中者优先 → 未定 → 降格”，同层按补齐期望成本最低；已满容量的件不再重复承载。
- 因此 4/4/1/1/1/1 的分配为：每件 `{优,攻}` + **一件一个稀缺效果**（4 件各承担一种稀缺，每件最多 3 条配额）；而 4/1 类配额容量更高，稀缺可更宽松分布。

**禁止词条与“恰好达标/超额”口径**

- `-1` 禁止词条：任何件出现即不达标，策略**绝不锁它**、优先洗掉；多数量目标（如“两条装弹”）因同件不重复必须落在两件不同装备上。
- 验收是 `>=`（达标即可），因此**超额**（如 装弹1 却有 2/3 条）或**额外非配额词条**不拦达标，但会浪费槽位；若要求“恰好”达标，需改为精确计数并对超额给惩罚。
- **正数但超额的效果（如 蓄力速度 Q=1 却初始 4）不能当“浪费”激进清洗**——否则会洗到低于配额再反噬，导致震荡不收敛；应靠 `>=` 接受超额，或只削到目标。

**模拟数据（全局联合分配，模块永久锁；验证程序见 `agent/go-service/equipmentreroll/verification/main.go`）**

- **统一基线**：所有配额都从“每件装备只有 1 号槽且为防御词条（`[防御,空,空]`×4）”出发，横向比较才公平。
- **分配感知（可流转）**：每步基于当前快照用 `allocateQuotaRequired` 重算“每件负责的配额效果集合”，稀缺配额（如 装弹1）只分给承载者，且承载者按**成本交换**（`pickCheapestCarriersForEffect`，补齐成本最低者晋升、原承载者退行）并随状态**实时流转**；策略只追本件负责的效果，避免“每件都抢稀缺配额”导致破坏优/攻、振荡不收敛。验证程序 `verification/main.go` 的 `allocateAssigned` 也用**成本交换口径**（`carrierCost` 近似，固定一次），因此对称起点（四件完全相同）下结果不变，非对称状态会按成本选更优承载者。
- **`>=` 验收（至少 1 条，不是恰好 1 条）**：装弹/蓄力速度≥1 即达标；洗出两条也不强制刷掉。

| 配额 | 期望模块 | 期望刷新 | 触顶 | 最终装弹分布 |
|---|---:|---:|---:|---|
| 4（优4/四优） | **23.2** | 22.7 | 0% | 装弹(非配额) 0=51% / 1=37% / 2=10% |
| 4/1（优4/攻1） | **35.4** | 28.3 | 0% | 装弹(非配额) 0=53% / 1=36% / 2=9% |
| 4/2/1（优4/攻2/装弹1） | **59.8** | 39.4 | 0% | 装弹 1=82% / 2=17% |
| 4/4（优4/攻4） | **77.4** | 47.8 | 0% | 装弹(非配额) 0=72% / 1=25% / 2=3% |
| 4/4/1（优4/攻4/装弹1） | **106.9** | 57.1 | 0% | 装弹 1=83% / 2=16% |
| 4/4/1/1（优4/攻4/装弹1/蓄力速度1） | **136.6** | 66.6 | 0% | 装弹 1=90% / 2=10% |
| 4/4/4（优4/攻4/装弹4） | **202.9** | 87.4 | 0% | 装弹 4=100% |
| 4/4/1 + 命中率-1(禁) | **109.3** | 58.1 | 0% | 装弹 1=81% / 2=18% |
| **4/4/2（优4/攻4/装弹2）** | **138.4** | 67.0 | 0% | 装弹 2=90% / 3=10% |
| **仅稀缺（装弹1/蓄力速度1）** | **8.8** | 8.7 | 0% | 装弹 1=90% / 2=10% |
| **混合多稀缺（优4/装弹1/蓄力速度1）** | **44.6** | 32.0 | 0% | 装弹 1=79% / 2=19% |
| **混合多稀缺（优4/攻2/装弹2/蓄力速度1）** | **98.3** | 53.3 | 0% | 装弹 2=92% / 3=8% |

> 说明：统一基线 + 成本交换分配（`allocateAssigned`/`carrierCost`，对称起点下与启发式结果一致）后，期望成本**随需求总量大体单调递增**（4 < 4/1 < 4/2/1 < 4/4 < 4/4/1 < 4/4/1/1 < 4/4/4），全部 0% 触顶、0% 禁词残留。“四优 ≈ 23”与《洗词条概率与期望计算.md》理论值 22.04 基本吻合；`4/4/1 + 命中-1` 仅比 `4/4/1` 高约 2.4（避免禁词的开销，且起点不含命中，故清洗负担小）。
>
> 最后 4 行为**泛化配额（新增）**，对应 §4.5“任意配额适配”的几种结构：`4/4/2`（稀缺数=2）、`仅稀缺`（无满配额，容量=3）、`混合多稀缺`（满配额 + 2 种及以上稀缺）。可看出：需求总量越大期望成本越高；`4/4/2` 介于 `4/4/1` 与 `4/4/4` 之间；`仅稀缺` 因要求少（仅 2 条）成本最低；“优4+装弹1+蓄力1” 比 “优4+装弹1” 略高（多一种稀缺）。该表用于对“宏观分配 + 锁定”策略做数据校验，属于**开发期验证**，不参与运行时。

### 4.5 任意配额的统一分配模型（归纳）

> 本节把 §4.4 的“宏观分配”泛化并归纳为统一模型：**任意配额（同件不重复、每件 ≤3 条、合计 ≤12）**，由三块组成——容量分配、涌现承载者、调度优先级。

**统一分配模型（`allocateQuotaRequired`）**

对每个正数配额效果 `e`，需要 `quota[e]` 件持有（每件至多 1 条，同件不重复）：

1. **满配额效果**（`quota[e] >= 件数`，如 优4/攻4）：分给全部件，并占用该件 1 槽。
2. **稀缺配额效果**（`quota[e] < 件数`，如 装弹1）：分给 `quota[e]` 个承载者；**每件承载稀缺效果的容量 = `maxSlot − 满配额数`**（满配额占槽，剩余槽可承载多个稀缺）。
   - 例：优4/攻4 满配额（占 2 槽）→ 容量=1；仅 优4 满配额（占 1 槽）→ 容量=2（同件可承载 2 个稀缺）。每件配额效果总数恒 ≤ 3（3 槽上限）。

**涌现承载者（slot3 命中者优先）**

稀缺配额选择承载者时，按“**slot3 先命中 优/攻/装弹 者优先**”的涌现规则：

| 当前 slot3 | 当前 slot2 | 角色 | 该件 required |
|---|---|---|---|
| ∈ 正数配额效果（优/攻/装弹/…) | 任意 | **承载者** | `满配额 + 该稀缺`（1、2 槽补剩余） |
| ∉ 正数配额效果 | ∈ **满配额效果**（quota[e] >= 件数，如 优/攻，可含任意满配额） | **降格者** | `满配额`（仅 优/攻 等满配额效果） |
| 其余 | 其余 | **未定** | 待定（继续“边刷边找”承载者） |

- 承载者选择由**显式成本交换 `pickCheapestCarriersForEffect`** 决定（成本更低者晋升、原承载者退行）；角色分类只是观察/调度参考；
- 每件累计承载稀缺数不超过 `容量 = maxSlot − 满配额数`（否则超出 3 槽）。

**调度优先级（`chooseBestPartForQuota`）**

`先找承载者 → 洗未完整非承载者探索交换 → 补完整承载者 → 兜底强选`：

- **无天然承载者**（无 slot3 命中）→ 只考虑“**未完整非承载者**（降格者 + 未定者）”（边刷边找承载者）；
- **已有天然承载者** → 先洗“**未完整非承载者**（降格者 + 未定者）”（洗它们可能翻盘成更优承载者 → 与当前承载者**交换/冒泡**，或直接补好 优/攻）；无未完整非承载者再**补完整承载者**；
- 已完整（持有全部 assigned）的件不洗；该优先级层无可洗件 → 回落到任意未完整件（兜底强选 / 继续推进）。

**性价比主排序（洗一次的模块数进入优先级）**

`chooseBestPartForQuota` 的选择键为：

```
主排序：gainPerCost = (当前全局期望成本 − 洗后全局期望成本) / 每次洗的模块数
次排序：gain（降本更大优先）→ washPriority（位置+数量+权重兜底）
```

- **每次洗的模块数 = `RerollModuleCost(锁定数)`**：0 锁 = 1、1 锁 = 2、2 锁 = 3（`RerollModuleCost`）。
- 所以**带锁件（1/2 模块/洗）会被除以更大的 cost**，只有在它的“期望降本”足够高时才值得优先洗——这就是“性价比”驱动：**先洗“每花 1 模块能带来最大全局降本”的件**。
- **注意**：性价比主排序并不等于“带锁件一定被降权”。若某件因**锁住废槽/卡点**导致全局成本很高，洗它的**降本更大**，哪怕每次 2 模块，`gainPerCost` 仍可能领先——这是正确的：**被卡住的点更值得优先修**。只有当带锁件的收益（降本）不足时，才会被无锁便宜件（1 模块）反超。

**“每洗一次模块数”与“位置+数量+权重”的分工**
- `gainPerCost`（主排序）负责**成本/收益性价比**；
- `washPriority`（次排序兜底）负责**落后程度**（位置 + 数量 + 概率权重补正），在降本相近时决定谁更该洗。

**“压力”与风险归纳**

| 配额 | 合计 | 每件压力 | 容量 | 说明（成本权衡口径） |
|---|---|---:|---:|---|
| 4/1（优4/攻1） | 5 | 3 件=1 条，1 件=2 条 | 2 | 无“凑 3 条”件，无 1 号槽成本问题 |
| 4/4/1（优4/攻4/装弹1） | 9 | 3 件=2 条，1 件=3 条 | 1 | 唯一致使 1 号槽有“策略上不锁”的成本权衡 |
| 4/4/1/1/1/1（优4/攻4 + 4×稀缺1） | 12 | 每件=3 条 | 1 | 每件都有 1 号槽“策略上不锁”的成本权衡 |
| 4/4/4（优4/攻4/装弹4） | 12 | 每件=3 条 | 0 | 每件都有 1 号槽“策略上不锁”的成本权衡 |

> **“1 号槽”是成本权衡，而非物理限制**：1 号槽本身可以被锁定；策略不锁它是因为 1 号 100% 易得、锁 1 追 23 代价高、真正难保的是 2/3 号槽。因此“承载者必须凑 3 条”只是意味着**有一条落在 1 号槽且策略选择不为它上锁**（低成本回归），不是“锁不住”。

**与实现对应**

- `allocateQuotaRequired`：满配额分全部件；稀缺用**显式成本交换判定 `pickCheapestCarriersForEffect`** 按“补齐 required 期望成本最低”选承载者（成本更低者晋升、原承载者退行），并遵守容量上限。
- `chooseBestPartForQuota`：按“找承载者 → 洗未完整非承载者探索交换 → 补完整承载者 → 兜底未完整件”的优先级过滤候选件（已完整件不洗）。
- 对应测试：`TestAllocateMultipleScarceDistinct`、`TestAllocateMultipleScarceCapacity`、`TestChooseBestPartPriorityFindCarrier`、`TestChooseBestPartPriorityExploreNonCarrier`、`TestCarrierCostSwap`、`TestFrameworkArbitraryQuota`。

**任意配额适配说明**

这套“角色阶梯 + 成本交换 + 容量 + 调度优先级”对**任意由 9 种 T10 效果组成的正数配额**适用：
- 效果权重表 `effectWeights` 覆盖了全部 9 种效果（优/攻/装弹/蓄力/暴击/命中/防御等），DP 对任意这些效果都能算期望成本；
- 角色分类已泛化：承载者 = slot3 命中**任意正数配额**；降格者 = slot2 持**满配额效果**（`quota[e] >= 件数`，不限于 攻/优）；
- 容量 = `maxSlot − 满配额数`，承载稀缺数 ≤ 容量（支持无满配额时单件承载多个稀缺、以及饱和时容量为 0）；
- 覆盖结构：无满配额（仅稀缺）、有满配额+多稀缺、饱和（全满配额）、含禁止词条（`-1`）等。

## 5. 全局配额下的锁定决策

### 5.1 为什么不能一律锁定

在四优配额中，某条效果命中后有两种价值：

1. 它可能正好消耗一个仍然需要的全局配额，锁定后能减少再次丢失该配额的风险；
2. 它也可能占用锁定位，使下一次效果变更的订制模块成本上升，或使用自订密钥后还要在每次效果变更后重新加锁。

因此“命中目标就锁”不是总是最优。MDA 的策略算法应比较“锁定后完成剩余配额的期望材料成本”和“不锁定、重新抽取的期望材料成本”。

### 5.2 基础概率参考

单次效果变更的栏位/效果基线概率，以及四优等常见配额的期望消耗计算，统一见[洗词条概率与期望计算](洗词条概率与期望计算.md)。

基线概率表明：在追逐低概率栏位时，先处理 3 号、再处理 2 号、最后处理 1 号通常更节省；但全局配额、当前已有词条和锁定材料会改变结论。

### 5.3 把锁定纳入全局单步动作

锁定不能脱离装备调度单独决定。对同一件装备，一次决策至少需要比较：

- 不锁定 + 效果变更；
- 锁定不同栏位组合 + 效果变更；
- 每一种锁定组合下，根据当前两种库存生成订制模块锁定和 / 或自订密钥锁定分支；洗词条本身仍固定消耗订制模块；
- 同一锁定组合中，自订密钥分支带有显著更低的材料惩罚；只有长期重复锁定成本足够高或密钥不足时，订制模块锁定分支才可能胜出。

完整状态 `S` 必须包含四件装备当前词条、永久 / 一次性锁定、全局配额分配、订制模块、自订密钥和剩余操作预算。由于每次动作都会消耗预算，有限预算下可以把它建模为有终点的动态规划。为保证预算不足时仍会持续改善装备，`V(S)` 不是单一概率，而是按顺序比较的价值向量：

```text
V(S) = (
    完成目标的最大概率,
    预算结束时的预期词条结构分,
    剩余订制模块可支持的预计效果变更次数,
    锁定材料效用,
    预计剩余锁定能力
)

Q(S, action) = Σ[结果概率 × lexicographicMax(
    V(维持当前效果后的状态),
    V(应用变更效果后的状态)
)]

bestAction(S) = 使 Q 按字典序最大的单次动作
```

动作消耗应在进入两个结果分支前从库存扣除：效果变更成本始终扣订制模块，锁定成本再从所选材料中扣除；若使用自订密钥，两个分支中的临时锁定都必须失效。达到目标时完成概率为 1；预算不足且未达到目标时完成概率为 0，但仍返回当前词条结构分和材料结余，因此算法不会失去对 60 分、90 分状态的区分。在完成概率和预期结构分相同的动作之间，先保留更多可用于效果变更的订制模块，再以高权重优先选择自订密钥锁定，并减少操作次数。

若精确动态规划的状态空间过大，可使用记忆化搜索、状态对称归并或固定随机种子的 Monte Carlo rollout 近似 `V(S)`。此时不能只统计当前有效词条数量；近似评分至少要包含每件装备的攻/优硬约束、补充词条剩余配额、栏位位置、锁定状态和预计剩余成本。

锁定材料没有用户偏好开关。算法同时考虑两种材料的当前库存、锁定持续时间和预计后续操作次数，但默认高权重优先自订密钥，并优先保存订制模块的效果变更能力；若某种材料不足，则自然排除对应动作。是否锁定、锁哪些栏以及使用哪种锁定材料，都属于同一个全局动作规划问题。

## 6. MDA 实现伪代码

### 6.1 数据结构

```text
EffectRule:
    effectId                 # 官方效果名称

EffectExpression:
    anyOf: [EffectRule | EffectExpression]   # 同一栏位内的或逻辑
    allOf: [EffectRule | EffectExpression]   # 同一栏位内的与逻辑

EquipmentRule:
    slotRules[1..3]: EffectExpression | null
    anySlot: EffectExpression
    allOf: [EquipmentRule]                    # 例如同件装备同时需要优越代码和攻击力

EquipmentPlan:
    mode = "equipment"
    equipmentRule
    slotRules[1..3]          # null 表示该栏位没有局部目标
    requiredCount             # 只按数量目标时使用，例如 1、2、3
    slotOrder = [3, 2, 1]
    lockSatisfied = true       # 达标栏位是否加入本轮 desiredLocks
    maxModules = null          # 可选的单件装备上限，不覆盖角色级库存规划
    maxKeys = null             # 可选的单件装备上限，不覆盖角色级库存规划

ActionPlan:
    equipmentIndex
    locksToRelease            # 本轮先解除的永久锁；不返还材料
    desiredLocks
    newLocks                  # desiredLocks - (permanentLocks - locksToRelease)
    lockSteps                 # 有序 [{ slot, material }]；material 为模块或密钥
    actionValue               # Q(S, action)，全局调度主排序值
    expectedGain
    weightedMaterialCost
    expectedGainPerCost       # 近似算法和同价值动作的辅助排序值

CharacterPlan:
    mode = "character"
    equipmentPlans[4]
    globalQuota = { effectId: count }   # 自定义配额合计为 1-12
    perEquipmentRule = null             # 当前实现仅使用 globalQuota
    maxOperations = Infinity       # 本次任务允许的效果变更次数上限；仍受材料和风险约束
    maxFailureRisk = null          # 可选；达到风险上限时停止

GlobalState:
    remainingQuota
    quotaAssignments                    # 当前四件装备已占用的全局配额
    totalValue
    inventory
    initialInventory
    remainingOperationBudget

EquipmentState:
    index
    effects[1..3]
    permanentLocks
    temporaryLocks                       # 只在一次动作的状态分支内存在
    satisfied

PartialScore:                            # 按字段顺序比较
    matchedQuotaCount
    slot3CoreCount                       # 仅自定义配额使用
    perEquipmentCoreCoverage
    negativeEstimatedRemainingCost
    usefulSlotStructureScore
```

### 6.2 读取和评价既有词条

```text
function inspectEquipment(equipment):
    assert equipment.isT10 == true
    current = readThreeEffectSlots(equipment)
    # 每个槽位只需包含 effectId 和 isObtained。
    return current

function matchesEffect(rule, effect):
    if effect.isObtained == false:
        return false
    if rule is EffectRule:
        return effect.effectId == rule.effectId
    if rule.anyOf exists:
        return any(matchesEffect(child, effect) for child in rule.anyOf)
    if rule.allOf exists:
        return all(matchesEffect(child, effect) for child in rule.allOf)

function matchesEquipment(rule, effects):
    if rule.slotRules exists:
        return all(
            rule.slotRules[slot] == null
            or matchesEffect(rule.slotRules[slot], effects[slot])
            for slot in [1, 2, 3]
        )
    if rule.anySlot exists:
        return any(matchesEffect(rule.anySlot, effect) for effect in effects)
    if rule.allOf exists:
        return all(matchesEquipment(child, effects) for child in rule.allOf)

function initialState(plan, equipment):
    current = inspectEquipment(equipment)
    satisfied = matchesEquipment(plan.equipmentRule, current)
    return EquipmentState(
        index=equipment.index,
        effects=current,
        permanentLocks=readPermanentLocks(),
        temporaryLocks=[],
        satisfied=satisfied
    )
```

### 6.3 单件装备只生成下一步候选动作

```text
function enumerateActionsForEquipment(plan, equipmentState, characterState):
    if noFurtherContributionPossible(equipmentState, plan, characterState):
        return []

    actions = []
    # 3 -> 2 -> 1 用于生成合理锁定组合，不代表连续洗这件装备。
    for locksToRelease in enumerateUsefulReleases(equipmentState, characterState):
        remainingPermanent = equipmentState.permanentLocks - locksToRelease
        for desiredLocks in enumerateUsefulLockSets(
            equipmentState,
            remainingPermanent,
            plan,
            characterState
        ):
            newLocks = desiredLocks - remainingPermanent
            for lockSteps in enumerateLockStepPlans(
                newLocks,
                currentLockCount=count(remainingPermanent),
                inventory=characterState.inventory,
                prefer="自订密钥",
                moduleLockWeight=HIGH_MODULE_LOCK_WEIGHT
            ):
                # newLocks 为空时返回空列表；需要新增锁时，枚举栏位顺序和
                # 逐条支付组合。自订密钥优先，订制模块作为高成本长期锁分支。
                action = ActionPlan(
                    equipmentIndex=equipmentState.index,
                    locksToRelease=locksToRelease,
                    desiredLocks=desiredLocks,
                    newLocks=newLocks,
                    lockSteps=lockSteps
                )
                if not withinBudget(action, characterState):
                    continue
                action.actionValue = evaluateActionValue(
                    characterState,
                    action,
                    plan
                )
                action.expectedGain = estimateAcceptedStateGain(action, characterState)
                action.weightedMaterialCost = quoteWeightedCost(
                    action,
                    characterState.initialInventory,
                    characterState.inventory,
                    moduleLockWeight=HIGH_MODULE_LOCK_WEIGHT
                )
                action.expectedGainPerCost = safeDivide(
                    action.expectedGain,
                    action.weightedMaterialCost
                )
                actions.append(action)
    return actions
```

这里的函数不会执行点击，也不会循环到单件装备完成。它只回答：“如果下一次操作投在这件装备上，有哪些可行动作，各自的期望收益是多少？”

### 6.4 结果页决策

```text
function decideResultPage(spentState, action, candidate, plan):
    # spentState 来自实际效果变更后的界面读取：材料已经消耗，当前效果仍在橙色区域，
    # 自订密钥锁定仍在保护本次结果。这里只比较结果，不得再次扣除材料。
    keepState = expireTemporaryLocks(spentState)
    candidateState = expireTemporaryLocks(
        replaceEquipmentEffects(
            spentState,
            action.equipmentIndex,
            candidate
        )
    )
    keepValue = stateValue(keepState, plan)
    candidateValue = stateValue(candidateState, plan)

    if lexicographicallyGreater(candidateValue, keepValue):
        return "效果变更"
    if lexicographicallyGreater(keepValue, candidateValue):
        return "效果维持"

    # 后续完成概率相同时，用当前结构分作为平局条件。
    if scoreCharacterState(candidateState, plan).totalValue
       > scoreCharacterState(keepState, plan).totalValue:
        return "效果变更"
    return "效果维持"
```

`stateValue` 是结果页的主判定，`scoreCharacterState` 只处理价值相同的平局。后者仍必须把四件装备放在一起做“栏位 → 全局配额”的最大价值匹配；多条效果同时可以满足同一规则时，优先分配给剩余数量最少、替代来源最少的效果。自定义配额还必须加入 3 号栏位核心词条的结构奖励：同等有效数量下，3 栏攻/优的当前结构分高于 3 栏补充有效。

自定义配额的默认 `PartialScore` 按以下顺序比较：已匹配的 1-12 条全局配额数量 → 3 号栏位为攻/优的装备数量 → 每件装备的攻优覆盖程度 → 预计剩余材料成本 → 有效词条的栏位结构。前一个字段相同时才比较后一个字段。由此，两个状态同为 3 条有效时，3 栏攻/优的状态必然优于 3 栏补充词条；但结构奖励不会凭空把无效词条计作一条有效。

### 6.5 单件装备入口

“洗装备”不是另一套随机逻辑，而是把同一个单步调度器限制在一件装备上。它仍然要先读取两种库存、现有效果和永久锁；满足用户指定的栏位规则后，按 `lockSatisfied` 决定是否锁定对应栏位。`anyOf` / `allOf` 的解释与角色级全局配额相同。

```text
function rerollEquipment(plan, equipment):
    assert plan.mode == "equipment"
    initialInventory = inspectCustomModuleAndLockKeyInventory()
    equipmentState = initialState(plan, equipment)
    state = createSingleEquipmentState(
        equipmentState,
        initialInventory,
        plan.maxOperations
    )

    while not matchesEquipment(plan.equipmentRule, state.equipment.effects):
        actions = enumerateActionsForEquipment(
            plan,
            state.equipment,
            state
        )
        chosen = chooseBestActionOrStop(actions, state, plan)
        if chosen is STOP:
            return chosen
        state = executeOneActionAndDecideResult(state, chosen, plan)
        if state is STOP:
            return state

    return SUCCESS(state)
```

单件装备的 `executeOneActionAndDecideResult` 与角色级循环使用同一套扣费、临时锁失效、结果页比较和库存复核逻辑；区别只在于角色级每轮从四件装备汇总候选动作，单件装备每轮只有一件装备可被选择。

### 6.6 角色级全局配额

```text
function rerollCharacter(plan, character):
    assert count(plan.equipmentPlans) == 4
    # 材料库存必须先于装备扫描，作为本次全局规划的预算基线。
    initialInventory = inspectCustomModuleAndLockKeyInventory()
    characterState = inspectAllFourEquipments(character)
    characterState.initialInventory = initialInventory
    characterState.inventory = clone(initialInventory)
    characterState.remainingOperationBudget = plan.maxOperations
    characterState = rebuildGlobalAssignment(characterState, plan)

    while not targetSatisfied(characterState, plan):
        allActions = []
        for equipmentState in characterState.equipments:
            equipmentPlan = resolveEquipmentPlan(plan, equipmentState.index)
            allActions += enumerateActionsForEquipment(
                equipmentPlan,
                equipmentState,
                characterState
            )

        stopValue = terminalValueIfStopNow(characterState, plan)
        feasibleActions = filter(
            allActions,
            action -> withinInventoryAndBudget(action, characterState)
                      and lexicographicallyGreater(action.actionValue, stopValue)
        )
        if feasibleActions is empty:
            return STOP("没有长期价值高于立即停止的预算内动作")

        # 每轮只选一个动作。主排序是有限预算下完成全局目标的 Q 值；
        # 平局时再比较库存消耗均衡度、单位材料增益和用户顺序。
        chosen = max(
            feasibleActions,
            by=(actionValue, expectedGainPerCost, userOrder)
        )

        navigateToEquipment(chosen.equipmentIndex)
        releasePermanentLocks(chosen.locksToRelease)
        applyLockSteps(chosen.lockSteps)
        result = performOneEffectChange()
        # 规划阶段的模拟扣费只用于评估 Q 值。实际执行后，根据已经确认的
        # 加锁步骤和界面费用更新运行态；若结果页看不到仓库库存，不强求此时 OCR。
        confirmedCosts = result.confirmedCosts
        if confirmedCosts is unavailable:
            confirmedCosts = quoteActionCosts(characterState, chosen)
        runtimeSpentState = recordConfirmedExecution(
            characterState,
            chosen,
            confirmedCosts
        )
        if runtimeSpentState is STOP:
            return runtimeSpentState
        decision = decideResultPage(
            runtimeSpentState,
            chosen,
            result.changedEffects,
            plan
        )
        click(decision)

        # 一次操作就是一个调度周期。即使维持当前效果，也消耗了材料；
        # 自订密钥锁定也已失效，因此必须重新读取，而不是直接再洗当前装备。
        characterState = rescanChangedEquipmentAndInventory(
            characterState,
            chosen.equipmentIndex
        )
        assertInventoryMatchesConfirmedExecution(
            runtimeSpentState.inventory,
            characterState.inventory
        )
        assertLockStateMatchesConfirmedExecution(
            runtimeSpentState,
            characterState,
            chosen.equipmentIndex
        )
        characterState = rebuildGlobalAssignment(characterState, plan)

    return SUCCESS(characterState)
```

### 6.7 全局状态价值、动作价值与锁定选择

```text
function simulateActionExecution(characterState, action):
    state = clone(characterState)
    equipment = state.equipments[action.equipmentIndex]
    assert equipment.temporaryLocks is empty
    equipment.permanentLocks -= action.locksToRelease
    activeLockCount = count(equipment.permanentLocks)

    for step in action.lockSteps:
        lockCost = quoteLockCost(
            lockedBefore=activeLockCount,
            material=step.material
        )
        deduct(state.inventory, step.material, lockCost)
        if step.material == "订制模块":
            equipment.permanentLocks.add(step.slot)
        else:
            equipment.temporaryLocks.add(step.slot)
        activeLockCount += 1

    # 效果变更只能扣订制模块，成本取决于本次操作时的锁定数量。
    rerollCost = quoteRerollModuleCost(activeLockCount)
    deduct(state.inventory.customModules, rerollCost)
    state.remainingOperationBudget -= 1
    return state

function recordConfirmedExecution(characterState, action, confirmedCosts):
    state = simulateActionExecution(characterState, action)
    if confirmedCosts != quoteActionCosts(characterState, action):
        return STOP("界面确认的材料费用与规划不一致")
    return state

function stateValue(characterState, plan):
    state = rebuildGlobalAssignment(characterState, plan)
    terminal = ValueVector(
        completionProbability = targetSatisfied(state, plan) ? 1 : 0,
        expectedTerminalScore = scoreCharacterState(state, plan),
        moduleOperationCapacity = estimateSupportedEffectChanges(
            state.inventory.customModules,
            state.equipments
        ),
        lockMaterialUtility = -estimateLockMaterialPenalty(
            state,
            moduleLockWeight=HIGH_MODULE_LOCK_WEIGHT
        ),
        remainingLockCapacity = estimateRemainingLockCapacity(state.inventory)
    )
    if targetSatisfied(state, plan):
        return terminal
    if state.remainingOperationBudget <= 0
       or noBudgetForAnyAction(state.inventory, plan)
       or exceedsConfiguredRisk(state, plan):
        return terminal

    key = canonicalStateByEquipmentSymmetry(state, plan)
    if memo.has(key):
        return memo[key]

    # 允许停止，把当前部分成果作为下界；继续操作必须带来更高长期价值。
    value = terminal
    for action in enumerateAllFourEquipmentActions(state, plan):
        value = lexicographicMax(value, evaluateActionValue(state, action, plan))
    memo[key] = value
    return value

function evaluateActionValue(characterState, action, plan):
    # 每个动作固定扣除本次效果变更所需的订制模块；若有锁定，
    # 再按 action.lockSteps 的顺序逐条扣除锁定成本，并写入永久 / 临时锁状态。
    spentState = simulateActionExecution(characterState, action)
    expectedValue = zeroValueVector()

    for outcome, probability in enumerateEffectChangeOutcomes(spentState, action):
        keepState = expireTemporaryLocks(spentState)
        changeState = expireTemporaryLocks(
            replaceEquipmentEffects(
                spentState,
                action.equipmentIndex,
                outcome.effects
            )
        )

        # 结果页允许选择更有利的分支。比较的是整个角色状态的后续价值，
        # 不是本件装备当前有几条“有效”。
        expectedValue += probability * lexicographicMax(
            stateValue(keepState, plan),
            stateValue(changeState, plan)
        )
    return expectedValue

function chooseLockPlan(characterState, equipmentIndex, plan):
    candidates = enumerateActionsForEquipment(
        resolveEquipmentPlan(plan, equipmentIndex),
        characterState.equipments[equipmentIndex],
        characterState
    )
    # “不锁”“锁不同栏位”“订制模块”“自订密钥”都已经是不同 action；
    # 直接选择 Q 值最高者。订制模块锁定带有高惩罚，自订密钥默认优先。
    return max(candidates, by=(actionValue, expectedGainPerCost))
```

精确 `stateValue` 会自然理解 3 栏攻/优的价值：该状态通往最终目标的可行后续结果更多，预计完成成本更低。如果使用近似评分替代动态规划，则必须显式加入 `slot3CoreBonus` 或“预计剩余成本降低量”，否则算法会错误地把 3 栏补充有效与 3 栏攻/优视为同分。

## 7. MDA 运行时必须安全停止的情况

- 目标装备不是 T10 装备，或无法确认当前装备状态；
- 读取不到某个栏位，或无法区分“未获得效果”和已获得效果；
- 订制模块不足以支付下一次效果变更；即使自订密钥充足，也不能代替洗词条费用；
- 用户输入的效果简称无法映射到官方效果名称；
- 自定义词条配额总数不在 1-12 范围内，或单件装备目标与全局配额互相矛盾；
- “效果变更”结果页无法判断当前效果和变更效果，不能盲点按钮；
- 预算达到上限，或概率计算表明继续操作会超过用户设定的失败风险。

## 8. 当前 MDA 实现设计（Pipeline + Go 节点对应）

> 本节记录 EquipmentReroll 任务的当前实现结构与设计思路，节点名与
> `assets/resource/pipeline/EquipmentReroll/` 下的定义一一对应。
> 策略层（0-5 章）与实现层有冲突时，以本节为准；伪代码 6 章是策略级描述，不逐行对应本实现。

### 8.1 总体流程

```
EquipmentRerollMain
  → EquipmentRerollFlow
    → EquipmentRerollScanMain           # 首次全量扫描四件装备：词条/数值/锁定（仅一次）
      → (腿部扫描完成)EquipmentRerollMaterialCheckEnter   # 物资检测：点必出的第一槽(slot1, 100%出词条)进入效果锁定页读取材料库存初始化余额，再退出（未进入则再次点击兜底）
        → EquipmentRerollAfterMaterialCheck              # 独立检测→End；完整任务→Decide
      → EquipmentRerollDecide           # 决策分发（自定义词条配额 1-12）
        → EquipmentRerollChoosePart      # 全局有限步前瞻：选择期望收益最高的部位
          → Open{Part}Details（通用打开）
            → LockGate（按自定义配额检查 2/3号未锁目标 → 锁定流程，Flag 锚定仅详情页）
              → LockClickSlot2/3 → LockPageEntered → LockSelectMaterial → LockSelectKey/Module → LockConfirm → LockNotify → LockNotifyConfirm → LockDone(写快照)
            → ClickChangeEffect（一级，详情页进入消耗材料确认页）
              → PrepareRerollCost（记录待消耗订制模块数）
                → ConfirmChangeEffect（二级，确认页点击）
                  → RecordRerollCost（正式记录本次消耗）
                    → __EquipmentRerollResultButtonsVisible
                      → __EquipmentRerollLocateChangedSlot1Match
                        → EquipmentRerollResultPage
                          → ResultClickKeep（维持→ ConfirmChangeEffect 确认页重洗，不经过 Flag）
                          → ResultClickAccept（接受→ ReturnToDecide 关闭回人物页→ Decide 重调度）
        → EquipmentRerollEnd
```

> 自定义配额：`LockGate` 按当前配额锁定 2/3 号仍有价值的词条；无锁时直通 `ClickChangeEffect`（0锁计费1模组）。锁定流程为点击槽位→效果锁定页→有密钥用密钥否则用模块→确认→通知二次确认→返回详情页，乐观写快照。锁定材料策略为“有自订密钥用密钥，不够/不足再用订制模块”。

### 8.2 Pipeline / Go 作用域

| 职责                                                         | 归属                                      |
| ------------------------------------------------------------ | ----------------------------------------- |
| 识别（OCR / TemplateMatch / 锚点路由 / 页面流转 / 点击）     | Pipeline                                  |
| 跨节点词条快照、效果记录、自定义配额决策、锁定编排、结果路由 | Go（`agent/go-service/equipmentreroll/`） |

- 识别配置尽量放 Pipeline（含 `__` 内部节点），便于 MaaFramework 调试面板可视化；
- Go 通过 `ctx.RunRecognition("节点名", img)` 复用 Pipeline 识别节点做后处理/决策，不再在 Go 内硬编码识别参数；
- 例外：全量扫描按槽位百分比分区，ROI 需由 Go 依据识图结果框动态计算，因此使用 `ctx.RunRecognitionDirect` 执行 OCR，Pipeline 只负责提供槽位锚点与调度。

### 8.3 扫描快照与“不重复全量扫描”

- Go 维护 task 级快照：每部位 3 槽 ×（词条名、数值、锁定状态）。自定义配额通过 `GetEquipmentSlotScans(taskID)` / `GetPartScan(taskID,part)` 读取完整快照（含锁）进行全局匹配。
- `EquipmentRerollScanMain` 首次全量扫描写入快照；之后**不再全量扫描**。
- **档位显示与数值校准（扫描日志 + 面向用户）**：扫描时对每个槽位计算 `value`、`tier`、`value_tier`（如 `11.81%（T11）`）。档位由 `effectTiers`（1~15 档映射，见[装备系统与洗词条研究](装备系统与洗词条研究.md) §数值档位）判定；`resolveEffectTier` 取“最近档位”并在 **OCR 与档位有出入时校准**输出为档位表的精确数值（容差 0.05%），存入快照的 `Value`。未知效果 / 空值 / 无法确认档位 → 不校准（`tier=0`）。
  - **用户可见输出**：`EquipmentRerollScanRouteAction` 从快照取出词条和数值，在展示边界再次解析档位并通过 `maafocus.Print` 发送 focus，因此 MXU 显示 `11.81%（T11）`。`buildScanSlotDetail` 仍把 `value`、`raw_value`、`tier`、`value_tier` 和 `message` 写入自定义识别 Detail，但该结构只用于 `maafw.log` 诊断，不是 MXU 的展示契约。
- 职责划分：`EquipmentRerollScan*` 节点只负责「扫描四件装备」；扫描完全部装备（腿部完成）后，**无论独立检测还是完整任务**，都先“物资检测一次”进入效果锁定页读取材料库存初始化余额；随后由 `EquipmentRerollAfterMaterialCheck` 按入口分支——若任务入口是 `EquipmentRerollScanMain`（独立运行/调试）→ 结束；若入口是 `EquipmentRerollMain`（完整洗词条任务）→ 进入 `EquipmentRerollDecide` 决策。
- 收尾细节：独立收尾的“关闭详情页”必须使用普通节点引用（非 `[JumpBack]`），否则关闭后会回跳父节点再次查找关闭按钮，导致已关闭页面识别失败并超时。
- 锁定快照：`applyLockToSnapshot(taskID,part,slot,material)` 乐观写入锁（密钥=一次性蓝橙？实际蓝=永久/橙=一次），`expireOneTimeLocks(taskID,part)` 在每次效果变更后（Keep/Accept）使一次性锁失效，永久锁保留。待图校准后以实际 ColorMatch 为准。
- 每次决策页结果：Accept → 用「变更效果」刷新该部位词条快照，并同步更新从结果页 OCR 解析出的数值；Keep → 保留原快照；两者均过期一次性锁。
- 决策页沿用之前的锚点方案读取变更槽位（`ResultSlot` 定位 + 内部 OCR 节点），不使用 Flag offset；Go 从 OCR 原文解析词条名与数值。
- 洗练完成经 `EquipmentRerollReturnToDecide` 回 `EquipmentRerollDecide`，直接基于快照调度下一件未达标装备。

### 8.4 槽位定位：固有几何 + Flag 锚点 + offset

#### 固有几何（实测记录）

将三个槽位视为一个整体矩形：

- 整体宽：**300**
- 整体高：**70**
- 每个槽位高：**22**
- 槽与槽之间间距等分：`3×22 + 2×2 = 70`
- 槽位纵向步距（pitch）：**24**（`22 + 2`）

注意：由于装备描述行数不同，换装备后槽位组可能在竖直方向整体偏移，甚至 1 槽看起来落到 2 槽的位置。因此**不能使用固定窄条带 ROI 逐个定位**，应锚定固定标识后按上述几何做 offset。

#### Flag 锚点与 offset（实测记录）

- 锚点模板：`EquipmentReroll/InspectFlag.png`（Flag 小图标，14×15）。
- 实测映射：Flag 识别框 `[580, 439, 14, 15]` 时，第 1 槽位左上角为 `(490, 484)`。
- Flag 框左上角 → 第 1 槽位左上角偏移：`(-90, 45)`，距离恒定。
- 第 N 槽位左上角 = Flag 框左上角 + `(-90, 45 + (N-1)×24)`。
- 槽位矩形 = 左上角 + `(300, 22)`。

#### Pipeline 结构（已实现）

- `__EquipmentRerollLocateFlag`：TemplateMatch `InspectFlag.png`（0.9、order_by Score），锚定 `EquipmentRerollInspectFlag`。
- 三个槽位不再分别识别，统一由 Flag 锚点 + 上述 offset 推导；槽内词条/数值/锁定区域沿用三区 ROI（150/75/75），各子节点以 `[Anchor]EquipmentRerollInspectFlag` + roi_offset 表达。

#### 决策页变更槽定位（Pipeline，保持）

- `__EquipmentRerollLocateChangedSlot{N}Match`：TemplateMatch `ResultSlot.png`（0.9、index 0/1/2），锚定 `EquipmentRerollChangedSlot1/2/3`。
- 已将游戏截图从原始 `2521×1418` 等比例缩放为 `1280×720` 后复核：变更效果三行白色槽位左上角约为 `(490,407)`、`(490,432)`、`(490,457)`；现有定位 ROI `[481,400,318,130]`、`ResultSlot.png` 的纵向 `index 0/1/2` 与截图一致。
- `__EquipmentRerollResultChangedEffectSlot{N}`：OCR 引用各自 `[Anchor]EquipmentRerollChangedSlot{N}` + `[8, 0, 290, 0]`，覆盖整行词条和右侧百分比；旧宽度 `160` 会在约 `x=658` 处截断数值。

### 8.5 锁定流程（自定义配额）与材料策略

> 截图校准（1280×720）：效果锁定页标题“效果锁定”居中，卡片分别显示“订制模组 必要2/持有N”与“自订密钥 必要20/持有N”（持有数量由 OCR 读取，用于密钥耗尽时回退订制模组），底部“确认”启用前灰色、SELECT后变蓝；二次确认弹窗标题“通知”、文案“为固定所选效果，将进行锁定。确定要进行吗？消耗资金 2/20”，按钮“取消/确认”。

**玩家操作路径**：详情页点击槽位右侧锁图标 → 进入“效果锁定”页 → 点击左右 `SELECT`（有密钥优先点击右侧“自订密钥”，不足再点左侧“订制模组”）→ 点击底部“确认”（启用）→ 弹出“通知”二次确认 → 点击“确认” → 返回详情页（该槽显示蓝/橙锁，仅读）→ 再执行效果变更。

**Pipeline 实现（两条锁定入口，汇合到同一锁页链路）**：
- **详情页入口**：`EquipmentRerollLockGate → __LockLocateFlag(Flag锚点) → LockBranch[LockNeed/ClickChangeEffect] → LockRouteSlot → LockClickSlot2/3`（Flag 锚点坐标）。
- **效果变更确认/结果页入口**：`KeepLockGate(OCR“将…改造效果与数值”) → KeepLockCheck → KeepLockRoute → KeepClickSlot2/3`（固定坐标）。
- 二者汇合：`…ClickSlotX → LockPageEntered(OCR 效果锁定) → LockSelectMaterial(Go按库存优先密钥、必要时模块) → LockSelectRouteAction → LockSelectKey/Module(OCR SELECT) → LockConfirm(OCR 确认) → LockNotify(OCR 通知) → LockNotifyConfirm(OCR 确认) → LockDone(Go写快照+清pending)`。

**锁定完成后的导航（按当前页面自适应，避免用错坐标）**：
`LockDone → EquipmentRerollLockAfterRoute(页面确认) → LockAfterIsConfirm(OCR 确认页) → EquipmentRerollKeepLockRoute`；或 `→ LockAfterIsDetail(TemplateMatch 详情页)` → `EquipmentRerollLockRouteSlot`。其中：
- 上一把锁后若 `DesiredLockSlotForQuota` 仍建议锁第二把，`EquipmentRerollLockDoneAction` 只**写入 pending 待锁槽**（不再直接 `OverrideNext`）；
- `EquipmentRerollKeepLockRouteSlotAction` / `EquipmentRerollLockRouteSlotAction` 再**读 pending**：有待锁→路由到对应点击节点（确认页 `KeepClickSlotX` / 详情页 `LockClickSlotX`）；无待锁→详情页 `ClickChangeEffect`、确认页 `PrepareRerollCost`。

> **Pipeline / Go 分工**：页面识别（确认页/详情页、锁定页标题、材料持有）都在 **Pipeline**；**待锁槽状态与“走哪条锁定入口”的路由在 Go**（`EquipmentRerollLockDoneAction` 写 pending，`EquipmentRerollLockRouteSlotAction` / `EquipmentRerollKeepLockRouteSlotAction` 读 pending 分流）。Go 不硬编码任何识别 ROI。

> **已锁槽不得重复上锁 & 一次性锁过期重锁**：`EquipmentRerollLockCheckRecognition`、`EquipmentRerollLockRouteSlotAction`、`EquipmentRerollKeepLockRouteSlotAction` 均在“目标槽位在快照中已处于锁定状态”时**拒绝再次上锁**，转去效果变更/确认页刷新，避免“两槽都已锁仍反复尝试锁第3槽”。自订密钥为一次性锁，每次效果变更后失效，故下一轮重锁属**预期行为**；此护栏针对**不应重复上锁的已锁槽**（如永久蓝锁占用）。若仍有未锁槽可补（如仅锁 3 号、2 号未锁），按 `DesiredLockSlotForQuota` 锁定**其它未锁槽**，不回头点已锁槽。

**单件短视 DP + 分配感知（第三步）**：`DesiredLockSlotForQuota` 先用 `allocateQuotaRequired` 得到“本件负责的配额效果集合（required）”，再调用 `bestLockSlotAndCostForRequired` 比较“不锁 / 锁 2 / 锁 3”的期望成本，取最低者。

- DP 使用 `docs/zh_cn/nikke/EquipmentReroll/洗词条概率与期望计算.md` 的槽位概率与效果权重；
- 状态只区分“空槽 / 每个仍需配额效果 / 其他效果”，控制状态规模；
- 期望成本由线性方程组精确求解，不做价值迭代；
- 全局 memo 缓存：相同 `(装备快照, 配额, 必需集, 禁止集, 锁定位)` 的 DP 结果只计算一次；
- 常用配额命中策略表缓存；全局 1 步前瞻也带 memo；
- 自动处理“四优不锁”的场景：该件无需再提供任何配额效果时，锁不锁都不会降低期望成本，因此不锁；
- 自定义配额下每件最多 2 锁。当前只锁 Pipeline 支持的 2/3 号槽。

**注意（模拟结论）**：单件 DP `expectedModulesForPartAllocated(scan, quota, required, lockSlot)` 把 `lockSlot` 在整条吸收链上**冻结**（等价“锁住已有效的槽、然后赌一次凑齐”的**一次性**策略），**不是自适应最优**。真正的最优是 §3.2 的口径——**饱和配额下“先苦后甜”（先便宜刷最难槽，落地才锁），非饱和下“拿到就锁”（锁已落地、追剩余）**（每步重算、拿到仍需要的配额词条就锁）。因此锁定位必须按 §3.2 口径建模，不能停留于单次冻结 `lockSlot` 的结果，也不应因为已锁了 2 号就停止加锁。模拟数据与验证程序见 §4.4 与 `agent/go-service/equipmentreroll/verification/main.go`。

**游戏规则（不重复）**：一件装备不能有两种相同效果（见 §1.3）。已锁定的效果占用该装备名额，因此在其余槽位的抽取池中被排除——绝不会出现“锁两条同效果”或“锁了又洗出相同效果”。多数量目标（如“两条装弹”）必须分配到**两件不同装备**。DP 建模时，锁定槽位的效果应计入排除池。

**分配感知的全局期望成本**：`expectedModulesForQuota` 现改为调用 `allocateQuotaRequired` 把全局正数配额名额分配到各件后逐件计算（见 §4.4），并使用当前 Inventory 选择密钥/模块、扣除规划中的锁定材料，避免稀缺配额被每件都要求；单件短视 DP 仍是“拿到就锁 + 先苦后甜”的简化近似（详见本条“注意”）。宏观分配的**独立验证程序**在 `agent/go-service/equipmentreroll/verification/main.go`，可 `go run ./equipmentreroll/verification` 复跑并核对 §4.4 表格。

**材料计费**（已按客户端截图校准，见 `inventory.go:46-78`）：
| 锁定 | 订制模组 | 自订密钥 |
|------|----------|----------|
| 0→1 | 2 | 20 |
| 1→2 | 3 | 30 |
| 效果变更 | 0锁1 /1锁2 /2锁3 订制模组（自订密钥不可代替） |

**材料消耗统计**：**库存只在腿部扫描完成后的“物资检测”流程初始化一次**（进入效果锁定页，`EquipmentRerollMaterialCheckRecognition` 读取订制模组/自订密钥「持有」→ `setInventory`，不实际锁定）。之后所有消耗（效果变更扣模块、锁定扣密钥/模块）都靠**行为记录**扣减余额（`recordRerollModuleCost` / `recordLockMaterialCost` → `decrementInventory`），不再每次 OCR。
- **库存输出**：`EquipmentRerollMaterialCheckRecognition` 在识别 `Detail` 中记录库存，供 `maafw.log` 诊断；面向用户的库存由任务结束摘要统一通过 focus 输出。go-service.log 同时记录 `material inventory initialized (material check)`（Info，含 modules/keys）。

- 每次效果变更：按当前锁定数记录订制模块消耗；
- 每次锁定：按实际材料（自订密钥/订制模块）和当前锁位记录消耗；
- **仅在任务成功（`EventStatusSucceeded`）结束时**输出本次累计消耗（用户可见，`logMaterialConsumption`）：`custom_modules`（订制模块总消耗）、`custom_lock_keys`（自订密钥）、`reroll_modules`（其中效果变更模块）、`lock_modules`（其中订制模组锁定模块）。失败/手动停止**不打印**（失败时 pending 未确认成本也不计入）。
- **成功结束时的装备详情面向用户**：`EquipmentRerollFinalSummaryAction` 构造最终四件装备详情与库存摘要并通过 `maafocus.Print` 发送 focus。逐槽自定义识别 Detail 仍保留在 `maafw.log` 供诊断；`logMaterialConsumption` 在成功时向 go-service.log 输出本次材料消耗（`custom_modules` / `custom_lock_keys` / `reroll_modules` / `lock_modules`）。
- **任务结束摘要（独立 focus 事件）**：管线节点 `EquipmentRerollFinalSummary` 使用 `DirectHit + EquipmentRerollFinalSummaryAction`，置于 `EquipmentRerollEnd` 之前；调试用的独立扫描入口（`EquipmentRerollScanMain`）路由 `EquipmentRerollAfterMaterialCheckAction` → `EquipmentRerollFinalSummary`，完整任务 `EquipmentRerollAllSatisfied` → `EquipmentRerollFinalSummary`。动作把“文本加工”集中在一处，再通过 `_GO_SERVICE_FOCUS_` 发送 `【装备详情】…（含 11.81%（T11））…【库存】订制模块 N / 自订密钥 M`，与 MaaEnd 的用户可见输出方式一致。摘要按“已扫描到的部位”输出：角色模式四件，单件模式只有选定的那一件。
- `MaterialUsage` 结构含 `CustomModules` / `CustomLockKeys` / `RerollModules` / `LockModules`，对应 `recordRerollModuleCost` / `recordLockMaterialCost` 记账分解。

**锁定材料双模式与获取成本**（模拟 + 决策实现）：

- 两种材料成本结构不同：**自订密钥**=一次性橙锁，每轮重锁、只消耗密钥（折算模块获取成本为 0）；**订制模组**=永久蓝锁，一次性扣模块获取（0→1=2、1→2=3），之后锁持续存在、不再重复扣。
- 模拟（单件、达到“三槽各含 优/攻/装弹”，200k 样本）：**最优锁定策略在两种材料下一致（饱和下先苦后甜最优）**，仅模块成本略有差异（密钥版 44.5 vs 模块版 50.4 模块；从不锁约 650；只锁2赌3 约 230；渐进去重 49.4/54.0）。因此**当自订密钥耗尽、脚本回退用订制模组时，无需改变锁定策略（先苦后甜/拿到就锁）**，只需把“买锁的模块”计入真实成本。
- 决策实现：`bestLockSlotAndCostForRequired(scan, quota, required, material)` 接受锁定材料。材料来源：腿部扫描完成后的“物资检测”流程进入**效果锁定页**，由 `EquipmentRerollMaterialCheckRecognition` 用 OCR 读取一次材料「持有」数量（`__EquipmentRerollLockModuleHeld` / `__EquipmentRerollLockKeyHeld`）初始化 `Inventory` 余额；之后 `EquipmentRerollLockSelectRecognition` 直接读该余额并用 `Inventory.ChooseLockMaterial` 决策（有密钥用密钥、不足用订制模组），同时把材料传入 `DesiredLockSlotForQuota` / `desiredLockSlotForCurrentMode`。当材料为“订制模块”时，会把 `lockAcquireCost(当前锁数)`（0→1=2、1→2=3）计入锁定成本，使**模块锁定时决策更保守**；自订密钥获取成本为 0。

**去重与轮转**：自定义配额每件最多 2 锁。`expireOneTimeLocks` 在每次 reroll 后使橙锁失效，蓝锁保留。Keep 分支回到 `ConfirmChangeEffect` 直接重洗同件（确认页内，Flag 不参与），Accept 分支经 `ReturnToDecide→Decide` 重调度（基于快照，Flag 重新锚定）。

### 8.6 决策页就绪与按钮兜底

- 点击二级效果变更后，`__EquipmentRerollResultButtonsVisible` 检测左下角「效果维持」出现作为决策页就绪信号（替代固定延时/画面冻结），超时未出现则失败退出，避免未进入决策页死循环。
- `EquipmentRerollResultPage`（Go 自定义配额决策 + 路由）：
    - 自定义配额：用全局期望剩余订制模块数 `expectedModulesForQuota` 比较当前 / 候选全局状态（方向 A：决策改为期望成本）。候选状态的期望成本**严格更低**才接受，否则维持。期望成本基于真实概率模型（槽位获得概率、效果权重、同结果排除、锁定与重洗费用），而非旧积分制的"已匹配配额数 × 100 + 槽位结构分"——旧制无法识别"**1 号槽有效词条在策略上不锁**（1 号 100% 易得、锁它反而抬重洗成本，故一般不为它上锁；每轮约 88% 概率丢失）"，会把"装弹@1 + 优@2"误判为优于"装弹@3（可 100% 锁定）"。
    - **短期最优（新增）**：在宏观成本**未变差**（持平/相邻）的前提下，若洗词条后**物理有效词条数上升**，也接受该结果——哪怕该有效词条是**超额/临时**的、下次洗就会洗掉。这是“多拿多算”的短期加成，**不覆盖“宏观更差”的状态**（严格更高者仍保持），从而**不影响宏观决策**。实现：`decideQuotaByExpectedCostWithInventory` / `DecideResultPageQuota` 在“宏观成本严格更低”之外追加“`candCost <= curCost+1e-6 && 有效词条数上升`→接受”。有效词条数用 `effectiveAffixCount` / `globalEffectiveAffixCount`（正数配额效果数量）。
    - Accept → `EquipmentRerollResultClickAccept`（点右下「效果变更」）→ `EquipmentRerollReturnToDecide`；
    - Keep → `EquipmentRerollResultClickKeep`（点左下「效果维持」）→ `EquipmentRerollConfirmChangeEffect` 直接重洗同件（确认页内）。
- `ClickKeep` / `ClickAccept` next 含自身节点兜底，防止点击失效误跳流程。

### 8.7 节点对照表

| 节点                                            | 职责                             | 关键参数                                                                  |
| ----------------------------------------------- | -------------------------------- | ------------------------------------------------------------------------- |
| `EquipmentRerollScanMain` / `Flow`              | 全量扫描入口 / 编排              | 独立运行扫描后停止，完整任务内继续到 Decide                               |
| `EquipmentRerollMaterialCheck`                   | 物资检测：进入效果锁定页读取材料库存，初始化 Inventory 余额 | Go MaterialCheckRecognition（OCR 订制模组/自订密钥 持有）+ Pipeline 进入/退出   |
| `EquipmentRerollAfterMaterialCheck`               | 物资检测后路由：独立检测→End；完整任务→Decide | Go AfterMaterialCheckAction（按任务入口分支）                 |
| `EquipmentRerollScanDetailsPageEntered`         | 确认详情资讯页并设扫描锚点       | OCR 详情资讯                                                              |
| `__EquipmentRerollLocateSlot1/2/3Match`         | 左侧分槽模板定位第1/2/3槽标记    | `InspectSlot1/2/3.png`（左侧）、0.9、index 0/1/2、ROI `[480,467,321,105]` |
| `__EquipmentRerollSlot1/2/3AffixOCR/ValueOCR`   | 词条/数值区域 OCR                | `[Anchor]SlotN` + roi_offset                                              |
| `__EquipmentRerollSlot1/2/3LockBlue/Orange`     | 锁定状态颜色判定                 | ColorMatch 蓝/橙、count 20                                                |
| `EquipmentRerollScanSlot1/2/3`                  | 逐槽扫描业务：复用子识别并写快照 | Go ScanSlotRecognition                                                    |
| `EquipmentRerollScanCloseDetails`               | 关闭装备详情页                   | CommonCloseButton                                                         |
| `EquipmentRerollDecide`                         | 自定义配额决策分发、设洗练锚点   | global_quota 全局判定                                                     |
| `EquipmentRerollAllSatisfied`                   | 四件装备均满足自定义配额         | Go PartNeedRecognition（part=all + global_quota）                         |
| `EquipmentRerollChoosePart`                     | 全局有限步前瞻选择部位           | Go ChoosePartAction（期望收益/模块成本）                                  |
| `EquipmentRerollOpen{Part}Details`              | 通用打开部位详情                 | Head/Arms/Torso/Legs 模板                                                 |
| `EquipmentRerollLockGate` / `LockBranch`        | 锁定前置分发（配额模式）         | DirectHit 分支：LockNeed vs ClickChangeEffect                             |
| `__EquipmentRerollLockLocateFlag`               | 锁定前 Flag 锚定                 | `InspectFlag.png` 0.9, 贴详情页                                           |
| `EquipmentRerollLockNeed` / `LockRouteSlot`     | 锁定需求判定与槽位路由           | Go LockCheck + RouteSlot (3→2优先)                                        |
| `EquipmentRerollLockClickSlot2/3`               | 点击 2/3号锁图标                 | `[Anchor]InspectFlag` + (135,69/93) 待校准                                |
| `EquipmentRerollLockPageEntered`                | 效果锁定页确认                   | OCR 效果锁定                                                              |
| `EquipmentRerollLockSelectMaterial`             | 锁定材料优选（密钥优先）         | Go LockSelectRecognition + RouteAction                                    |
| `EquipmentRerollLockSelectModule/Key`           | 点击 SELECT                      | OCR SELECT（左右 ROI 区分）                                               |
| `EquipmentRerollLockConfirm`                    | 锁定页确认                       | OCR 确认（底部蓝条）                                                      |
| `EquipmentRerollLockNotify` / `NotifyConfirm`   | 通知二次确认                     | OCR 通知 / 确认                                                           |
| `EquipmentRerollLockDone`                       | 乐观写快照+清 pending            | Go LockDoneAction → ClickChangeEffect                                     |
| `EquipmentRerollClickChangeEffect`              | 一级效果变更（装备详情页）       | OCR 效果变更                                                              |
| `EquipmentRerollPrepareRerollCost`              | 确认前校验库存并记录待消耗订制模块数       | Go PrepareRerollCostAction（按当前锁定数 + Inventory 门禁）                                |
| `EquipmentRerollConfirmChangeEffect`            | 二级确认效果变更（确认页）       | OCR 效果变更                                                              |
| `EquipmentRerollRecordRerollCost`               | 确认后正式记录本次消耗           | Go RecordRerollCostAction（按当前锁定数）                                 |
| `__EquipmentRerollResultButtonsVisible`         | 决策页就绪信号                   | OCR 效果维持                                                              |
| `__EquipmentRerollLocateChangedSlot1/2/3Match`  | 决策页模板定位第1/2/3变更槽标记  | `ResultSlot.png`、0.9、index 0/1/2                                        |
| `__EquipmentRerollResultChangedEffectSlot1/2/3` | 读变更效果（Go 复用）            | `[Anchor]ChangedSlot1/2/3`、`[8,0,290,0]`，覆盖词条、数值和档位解析所需百分比 |
| `EquipmentRerollResultPage`                     | 读变更效果 + 决策路由            | Go ResultDecide + RouteAction（自定义配额）                               |
| `EquipmentRerollResultClickKeep/Accept`         | 点维持/接受（自身兜底）          | OCR 按钮（Keep→ConfirmChangeEffect，Accept→ReturnToDecide）               |
| `__EquipmentRerollLockTitle`                    | 锁定页标题（Go 复用）            | OCR 效果锁定                                                              |
| `EquipmentRerollReturnToDecide`                 | 关闭回人物页直接调度（不重扫）   | JumpBack 关闭                                                             |
| `EquipmentRerollEnd`                            | 任务结束                         | -                                                                         |

### 8.8 Go 组件对应

| Go 组件                                    | 对应节点 / 场景                                                                             |
| ------------------------------------------ | ------------------------------------------------------------------------------------------- |
| `EquipmentRerollScanBeginAction`           | `EquipmentRerollScanBegin{Part}`（初始化扫描状态）                                          |
| `EquipmentRerollScanSlotRecognition`       | `EquipmentRerollScanSlot1/2/3`（复用 Pipeline 子识别，解释并记录词条/数值/锁定）            |
| `EquipmentRerollScanRouteAction`           | `EquipmentRerollScanSlot3`（展示扫描摘要并路由下一部位；独立入口停止，完整任务继续 Decide） |
| `EquipmentRerollPartNeedRecognition`       | `EquipmentRerollAllSatisfied`（自定义配额全局判定）                                         |
| `EquipmentRerollLockCheckRecognition`      | `EquipmentRerollLockNeed`（3→2号未锁单目标检查，Box=slot）                                  |
| `EquipmentRerollLockRouteSlotAction`       | `EquipmentRerollLockRouteSlot`（按 Go 快照路由到 Slot2/3 点击）                             |
| `EquipmentRerollLockSelectRecognition`     | `EquipmentRerollLockSelectMaterial`（标题校验+密钥优先决策，Box=按钮）                      |
| `EquipmentRerollLockSelectRouteAction`     | `EquipmentRerollLockSelectMaterial`（按 Go 决策路由到 Module/Key SELECT）                   |
| `EquipmentRerollLockDoneAction`            | `EquipmentRerollLockDone`（乐观写快照 `applyLockToSnapshot` 并清 pending）                  |
| `EquipmentRerollPrepareRerollCostAction`   | `EquipmentRerollPrepareRerollCost`（确认前记录待扣订制模块数）                              |
| `EquipmentRerollRecordRerollCostAction`    | `EquipmentRerollRecordRerollCost`（确认成功后把 pending 写入材料统计）                      |
| `EquipmentRerollResultDecideRecognition`   | `EquipmentRerollResultPage`（自定义配额，读变更 + 期望成本决策）                          |
| `EquipmentRerollResultRouteAction`         | `EquipmentRerollResultPage`（按决策路由到维持/接受，过期一次性锁）                          |
| `clearMonitorState` / `expireOneTimeLocks` | `taskLifecycle OnTaskerTask` 及每次结果页后（Keep/Accept）                                  |

> 锁定卡片文案校准：选中“订制模组”后提示“在解除固定之前，半永久选取的效果不会发生更改。”；选中“自订密钥”后提示“在锁定之后更改效果或重新设置数值时，会解除选取效果的锁定状态。”；二次通知统一为“为固定所选效果，将进行锁定。确定要进行吗？消耗资金 2/20”。

## 9. 单件模式（洗单个装备词条）

> 本节描述 `EquipmentReroll` 任务下的**单件模式**（选项 `EquipmentRerollMode` = `Single`）的实现。
> 洗角色词条与洗单件词条是同一任务（入口 `EquipmentRerollMain`）下的**同级互斥模式**：
> 选项 `EquipmentRerollMode`（select，默认 `Character`）决定入口路由与物资检测后的决策分支，
> `EquipmentRerollSinglePart`（选择部位）与 `EquipmentRerollSingleWant1/2/3` + `EquipmentRerollSingleWant1/2/3Slot`（三组“需求词条+槽位”直选）
> 是 `Single` case 的嵌套子选项。
> 纯决策逻辑集中在 `agent/go-service/equipmentreroll/single.go`，入口动作在 `single_action.go`；
> 槽位概率 / 期望成本 DP 复用了 `plan_dp.go`（新增 `slotAllow` 槽位限定感知）。

### 9.1 定位与区别

角色模式（§1.2/§4/§8）把四件装备当作**一个整体**，用全局配额 + 分配感知 + 全局有限步前瞻做跨装备的
复杂组合调度。**单件模式**只洗用户选定的一件装备，明确“不做跨装备的复杂搭配策略计算”，只在单件内部
做目标判定与锁定决策。两者共享同一套原子化 Pipeline / Go 组件（扫描、锁定、效果变更、结果页），
通过承载点 `EquipmentRerollLockNeed.attach.mode` 切换模式：

| 维度 | 角色模式（EquipmentRerollMode=Character） | 单件模式（EquipmentRerollMode=Single） |
| --- | --- | --- |
| 任务 / 入口 | `EquipmentReroll`（入口 `EquipmentRerollMain`） | 同一任务、同一入口（由 `attach.mode` 分流） |
| 操作对象 | 四件装备联合调度 | 用户选定的一件（头部/臂部/身躯/腿部） |
| 扫描范围 | 四件全扫，腿部扫完做一次物资检测 | **只扫选定那一件**，扫完即做物资检测 |
| 目标模型 | 全局配额（-1 禁止 / 0 / 1-4，合计 1-12） | 单件目标（1-3 条需求词条，每条可限定落槽） |
| 槽位限定 | 不限定（只看是否持有） | 每个需求词条可限定落槽（不选则任意槽位均可） |
| 词条数量上限 | 1-12（四件 × 三槽） | 1-3（单件三槽、同效果不重复） |
| 锁定决策 | 分配感知 + 先苦后甜 / 拿到就锁（跨装备） | 需求 <2 不锁；==2 拿到就锁（期望收益差距 DP）；==3 先苦后甜（单件内） |
| 组合策略 | 有（分配感知、承载者流转、交换） | 无（纯单件局部目标） |

### 9.2 任务选项

**`EquipmentRerollMode`**（select，父级、互斥）：

| case | 含义 | 嵌套子选项 | pipeline_override |
| --- | --- | --- | --- |
| `Character`（默认） | 洗角色词条 | 9 个效果配额 select（`EquipmentRerollQuotaElementalDamage` 等） | `attach.mode = "character"` |
| `Single` | 洗单件词条 | `EquipmentRerollSinglePart`、`EquipmentRerollSingleWant1/2/3` | `attach.mode = "single"`；`EquipmentRerollScanDetailsPageEntered.next → EquipmentRerollSingleScanRoute`（只扫选定那一件） |

两种模式共用入口 `EquipmentRerollMain`（RuntimeQuotaCheck 计费）与扫描 + 物资检测流程；
`EquipmentRerollAfterMaterialCheck` 读取承载点 `attach.mode` 分流到 `EquipmentRerollDecide`（角色）或
`EquipmentRerollSingleDecide`（单件）。

> **模式判定只有 `attach.mode` 一个来源。** 早期实现用“单件目标是否为空”反推模式，
> 结果一个非法的单件配置（例如把同一个词条选了两次）会让目标解析为空，从而**静默退化**成
> 角色模式的默认配额去洗，用户完全无从察觉。现在配置非法就明确报错并结束任务（§9.9）。

**`EquipmentRerollSinglePart`**（select，嵌套于 Single）：选择要洗的部位（头部/臂部/身躯/腿部），默认头部。
写入 `attach.part`，同时决定扫描起点与决策对象。

**需求词条行**（`EquipmentRerollSingleWant1/2/3` + `EquipmentRerollSingleWant1/2/3Slot`，嵌套于 Single；
建模借鉴 MaaEnd 的逐项 select：每个语义独立控件、case 直接写死语义值，不做数字编码）：

- `EquipmentRerollSingleWantN`（select）：第 N 条需求词条，case = `None`（不需求，默认）+ 9 种官方效果名直选；
- `EquipmentRerollSingleWantNSlot`（select，嵌套在对应 WantN 的效果 case 下）：该词条的槽位限定，
  case = `Any`（任意槽位，默认）/ `Slot1` / `Slot2` / `Slot3`。
- 每个 case 的 `pipeline_override` 把语义值**写死**到统一配置承载节点
  `EquipmentRerollLockNeed.attach`（顶层键，多 option 覆盖互不覆盖——不同于整体替换的 `custom_recognition_param`）：
  - 效果 case → `attach.wantN = "<官方效果名>"`；
  - 槽位 case → `attach.slotN = 0（任意）/ 1 / 2 / 3`。
- Go 端 `singleTargetFromRows` 把三行组装为 `effect -> slot` 目标：空行忽略、同一效果重复选择判为非法；
  其余组件（锁定、结果页、接受路由、单件决策、扫描路由）统一经 `loadCarrierConfig(ctx)` 读取该节点配置，
  不再各自接收 target 参数。识别器按帧调用，因此一次 `Run` 内只读一次并把 `carrierConfig` 往下传。

全部需求词条（三行中非 None 的行数）合计必须为 **1-3 条**
（单件三槽、同效果不重复；不能把两条词条限定到同一槽——由 `singleTargetProblem` 校验并返回原因码）。
单件模式暂不提供“禁止词条”概念（角色模式 `-1` 仍可用）。

### 9.3 锁定决策（期望收益差距）

单件模式锁定决策 `singleDesiredLockSlot` 按需求词条数分档（复用 `plan_dp.go` 的 slot-aware DP）：

- **需求 == 1**：不锁。单条目标找到即达标，锁反而抬升重洗成本（每锁 1 槽多 1 模块）。
- **需求 == 2**：**拿到就锁**。对已落在“允许槽位”的可锁 2/3 号槽，用
  `bestLockSlotAndCostForTarget` 比较“锁该槽 vs 不锁”的期望剩余订制模块成本，选**期望收益差距
  （不锁成本 − 锁后成本，越低越优）**最大的槽锁定；若无收益则回退不锁。
- **需求 == 3**：**先苦后甜**。三槽全满（饱和），先便宜刷最难的 3 号（获得概率 30%）落地才锁，
  再顺序锁 2 号；1 号槽策略上不锁（100% 易得、锁 1 追 23 代价高）。

锁定材料策略与角色模式一致（有自订密钥用密钥、不足再订制模块；模块锁定时计入获取成本，决策更保守）。

### 9.4 达标判定与结果页决策

- **达标判定** `singlePartSatisfied`：每个需求词条都出现在其允许槽位（`effectInAllowedSlot`），
  且不存在任何禁止词条。
- **结果页决策** `DecideResultPageSingle`：用 slot-aware 期望成本比较当前/候选状态，
  候选期望成本严格更低才接受；持平且“允许槽位上的有效词条数”上升也接受（短期最优，不覆盖宏观更差）。

### 9.5 slot-aware 期望成本与原子化复用

为了支持槽位限定，`plan_dp.go` 的 DP 核心增加了一个**可选的 `slotAllow` 参数**
（`expectedModulesForPartCore(scan, quota, required, forbidden, lockSlot, slotAllow)`）：

- `nil` 表示任意槽位 → 与角色模式完全一致（行为不变，全部既有测试通过）；
- 非空时，`compressEffectSlot` 会把“需求效果落在不允许槽位”压缩为 `other`，完成判定
  `partHasRequiredAndNoForbiddenSlot` 只在允许槽位上计数，锁定在非允许槽位的需求效果视为不可达。
- 角色模式包装函数（`expectedModulesForPartAllocated`、`bestLockSlotAndCostForRequired`、
  `desiredLockSlotBitterSweet`、`lockStillWorthIt`）保持不变，内部传 `nil` 就等价于原有逻辑。
- 单件模式经 `singleExpectedCost` / `bestLockSlotAndCostForTarget` 等入口按 `slotAllow` 调用同一个
  `expectedModulesForPartCore`，
  复用同一套概率模型（槽位获得概率 / 效果权重 / 同件不重复排除）。

> 这是“若原有逻辑不够原子化则对齐进行拆分”的落点：把 DP 的“按需效果是否存在”判断拆成
> “按需效果是否在允许槽位存在”，角色与单件两条路径共用同一概率模型。

### 9.6 Pipeline / Go 节点对应（新增）

| 节点 | 职责 | 组件 |
| --- | --- | --- |
| `EquipmentRerollMain` | 共用入口（RuntimeQuotaCheck 计费）→ `EquipmentRerollFlow`（两种模式同一条编排） | membership `RuntimeQuotaCheckAction`、任务选项 `EquipmentRerollMode` |
| `EquipmentRerollSingleScanRoute` | 单件扫描起点：跳过其余三件，直接打开 `attach.part` 选定的那一件详情 | 新增 `EquipmentRerollSingleScanRouteAction` |
| `EquipmentRerollSingleDecide` | 判断选定部位是否达标：达标→摘要/结束；目标不可达→告知用户并结束；否则打开该部位详情（重设 AfterOpen 锚点→LockGate） | 新增 `EquipmentRerollSingleDecideAction` |
| `EquipmentRerollSingleReturnToDecide` | 单件一次效果变更后回单件决策（不再重新扫描） | - |
| `EquipmentRerollAfterMaterialCheck` | 物资检测后路由：独立扫描→摘要；角色（mode=character）→Decide；单件（mode=single）→SingleDecide | `EquipmentRerollAfterMaterialCheckAction`（读 `attach.mode` 分流） |
| `EquipmentRerollLockNeed` / `KeepLockCheck` | 配置承载点 + 锁定判定（单件模式：`singleDesiredLockSlot`） | `EquipmentRerollLockCheckRecognition`（`lockCheckSingle` 分支） |
| `EquipmentRerollResultPage` | 结果页决策（单件模式：`DecideResultPageSingle`） | `EquipmentRerollResultDecideRecognition`（`decideSingle` 分支） |
| `EquipmentRerollAfterAccept` | 接受后路由（单件模式→`EquipmentRerollSingleReturnToDecide`） | `EquipmentRerollAfterAcceptRouteAction`（读 `attach.mode`） |
| `EquipmentRerollLockSelectMaterial` / `LockDone` / `LockRouteSlot` / `KeepLockRoute` | 材料选择 / 二次锁 / 待锁槽路由（统一经 `desiredLockSlotForConfig` 按模式回退） | 复用，内部改用模式感知的 `desiredLockSlotForCurrentMode` |

> **复用原则**：除上述 Go 组件增加模式分支、以及新增一个扫描起点路由动作外，单件模式**不新增**打开详情、
> 锁定页、效果变更、结果按钮等原子化节点；所有识别参数仍只维护在 Pipeline，Go 不硬编码任何识别 ROI。

### 9.7 会员配额

两种模式共用入口 `EquipmentRerollMain`，属于**高消耗**任务（`taskersink/membership/multiplier.go`
的 `taskTierByEntry` 中 `taskTierHigh`）：非会员按 5 倍额度消耗、配额路由走专项优先
（`quotaRouteSpecialThenRegular`）。单件模式不额外注册入口，直接复用角色模式的计费口径。

### 9.8 数据示例（单件，期望订制模块基线）

与 §4.4 表同口径的“单件”直观基线（无初始锁定，目标达成即停；仅示意量级，未逐样本枚举）：

| 目标 | 词条数 | 期望模块量级 |
| --- | ---: | --- |
| 优（任意槽） | 1 | 低（找到即停，不锁） |
| 优@3 | 1 | 中（需刷 30% 槽，不锁保低成本） |
| 优@3 + 攻@2 | 2 | 中高（锁优@3 后按 2 模块/刷追攻） |
| 优@3 + 攻@2 + 装弹（任意） | 3 | 高（先苦后甜，逐步锁 3、2） |

> 具体期望值由 `singleExpectedCost` 按实际快照与库存计算，单个目标是否锁由
> `singleDesiredLockSlot` 的期望收益差距决定；此处仅说明量级与策略分档，不属于运行时契约。

### 9.9 不可达目标与非法配置的拦截

期望成本模型用哨兵值 `costUnreachable`（`plan_dp.go`）表示“这个目标做不到”，触发场景：

- 禁止词条被**永久锁**死在某槽（锁定无法解除）；
- **需求词条被锁在了槽位限定不允许的槽**——例如 3 号槽已锁“攻击力增加”，而用户把“攻击力增加”
  限定到 2 号槽。锁定既不能解除、词条也不会跨槽移动；
- 剩余可洗空槽数少于需求缺口。

`costUnreachable` 是**哨兵而不是“很贵的成本”**，必须在决策入口拦截并结束任务：

| 模式 | 拦截点 | 判定 | 用户提示 |
| --- | --- | --- | --- |
| 单件 | `EquipmentRerollSingleDecideAction` | `singleTargetUnreachable`（`singleExpectedCost >= costUnreachable`） | `tasker.equipment_reroll.single_target_unreachable` |
| 角色 | `EquipmentRerollChoosePartAction` | `expectedModulesForQuota >= costUnreachable` | `tasker.equipment_reroll.quota_unreachable` |

配置本身非法（重复选同一词条、两条限定到同一槽、需求数不在 1-3、部位未选）时同样立即结束，
`carrierConfig.TargetProblem` 带原因码进日志，用户侧发
`tasker.equipment_reroll.single_target_invalid` / `single_part_invalid`。

> **为什么必须拦截**：若不拦截，`singlePartSatisfied` 恒为 false（一直判定“还没达标，继续洗”），
> 而结果页当前/候选成本同为 `costUnreachable`、比较后恒判 `Keep`（一直不接受）。任务会一路洗下去，
> 直到 `EquipmentRerollPrepareRerollCostAction` 发现订制模块耗尽才结束——等于把用户全部付费材料
> 消耗在一个数学上不可能达成的目标上，且日志里没有任何一条说明原因。
