# NIKKE 洗词条策略与 MDA 实现逻辑

> 本文件是供开发 Agent 理解需求并编写 MDA 任务的策略规格，描述“洗装备”和“洗角色”的目标、锁定策略、概率决策及伪代码。实际读取界面、计算结果和执行点击的是 MDA 运行时，不是 Agent。效果名称、效果概率和 UI 文案以同目录的[装备系统与洗词条研究](装备系统与洗词条研究.md)为准。

- 整理日期：2026-08-19
- 状态：已实现“四优”模板的识别、决策与效果变更流程；其它模板仍处于设计阶段
- 目标：让开发 Agent 据此实现 MDA，使 MDA 能先读取既有词条，再按单件装备目标或四件装备的全局目标执行效果变更

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

洗角色不能简单退化为依次完成四次洗装备。它复用洗装备的单步操作逻辑，但调度权始终在角色层：每次只选择一件装备进行一次效果变更，随后重新比较四件装备。使用模板或全局配额时，还必须统一分配已经获得的效果，不能让每件装备各自独立判断。

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

### 3.2 默认锁定顺序：先苦后甜

栏位赋予效果的概率为 1 号栏位 100%、2 号栏位 50%、3 号栏位 30%。在没有更具体的用户配置时，默认优先处理 3 → 2 → 1：

- 目标是 3 条有效：先把 3 号栏位洗出目标并锁定，再处理 2 号，最后处理 1 号；
- 目标是 2 条有效：若 2、3 号栏位任一先出现有效词条，就锁定该栏位，再洗剩余未锁定栏位；达到两条后立即结束。若当前只有 1 号栏位有效，不要先锁 1 号再追 2、3 号栏位；
- 目标是 1 条有效：可以接受任意栏位命中，达到目标后结束这件装备，不需要为了把词条移动到 1 号栏位而继续操作。

“先苦后甜”不是强制覆盖用户的栏位规则。若用户明确写了“3 号栏位必须是某效果”，必须服从该规则；若用户只要求数量，则按栏位出现概率和剩余目标数量选择顺序。

### 3.3 只执行效果变更

- 目标效果类型不满足时，使用“效果变更”；
- 本任务不处理“重新设置数值”，也不因数值不足、Tier 较低或想刷新数值而继续效果变更；
- 结果页只比较当前效果和变更效果的效果名称、栏位组合及全局配额贡献；
- 发现目标已经满足时，立即停止该装备或从角色级候选集中移除，不为了刷新数值自动继续洗。

## 4. 洗角色模板

模板是洗角色的快捷封装。模板名使用社区简写，冒号后的内容是**完整逻辑描述**，不是 UI 展示文案。实现时必须从模板展开出哪些效果算有效、何时结束、是否需要锁定以及是否存在跨装备配额。

模板是面向多数人的便捷封装，不承诺覆盖所有栏位规划细节。例如“2、3 栏有效”通常比“1、2 栏有效”更适合追加第三条词条，因为后续处理 1 号栏位比处理 3 号栏位容易；模板可以为了易用性忽略这种拓展策略，但不能因此限制“洗装备”的完全自定义能力。需要精细控制栏位、追加顺序或全局配额时，应改用自定义配置。

### 4.1 四优（4 有效）

**逻辑：**四件装备分别至少出现 1 条“优越代码伤害增加”。开始前先扫描四件装备，每次从尚未满足目标的装备中选择长期价值 `Q` 最高的下一步动作；任意栏位出现该效果即接受结果，该装备不再生成后续动作。不需要锁定，因为该装备已经达到目标，继续洗只会增加风险和消耗。

这个模板对四件装备的目标定义是对称的，但不代表四件装备当前状态等价：初始词条、已获得栏位和已有锁定通常不同，所以仍必须先扫描并按当前状态选择顺序。

**伪配置：**

```text
perEquipment: anySlot(优越代码伤害增加)
onSatisfied: acceptAndFinish
lock: never
```

若装备进入任务时已经有该效果，应直接完成该装备，不进行任何效果变更。

### 4.2 四攻四优（8 有效）

**逻辑：**四件装备分别需要同时拥有 1 条“优越代码伤害增加”和 1 条“攻击力增加”，合计 4 攻击力、4 优越代码，共 8 条有效词条。两种效果的栏位不限，但同一件装备必须各有一条。每件装备按“两条有效”的先苦后甜策略生成锁定方案：先在 2 号或 3 号栏位取得其中一种效果并锁定，再在剩余未锁定栏位追另一种效果；不应先锁 1 号栏位再追 2、3 号栏位。但锁定后不要求立刻继续洗同一件装备，角色级调度器仍会把它与另外三件的下一步动作比较。

**伪配置：**

```text
perEquipment: allOf(
  anySlot(优越代码伤害增加),
  anySlot(攻击力增加)
)
onFirstTargetInSlot2Or3: acceptAndLock
onBothSatisfied: acceptAndFinish
preferredSlotOrder: [3, 2, 1]
```

“有效”只判断效果名称。模板和自定义配置没有其他门槛。

### 4.3 四攻四优四有效（12 有效）

**逻辑：**用户输入额外 4 条有效词条的全局数量配额；它们可以是 1~4 种效果，不要求四条互不相同。加上固定的 4 条“优越代码伤害增加”和 4 条“攻击力增加”，四件装备合计需要 12 条有效效果，每件装备的 1、2、3 号栏位都必须被填满。例如：

```text
四攻四优 + 1 装弹 + 3 蓄力速度
```

应被展开为类似下面的全局配额，而不是让每件装备各自把四种效果都视为无限有效：

```text
优越代码伤害增加：4
攻击力增加：4
最大装弹数增加：1
蓄力速度增加：3
```

这里的“装弹”是“最大装弹数增加”的简写。当某件装备的第三条效果已经获得“最大装弹数增加”，就消耗掉全局的 1 条装弹配额；其余装备再出现该效果时，不再算作有效目标，是否保留只能由整体结果和材料成本决定。示例中的“蓄力速度”指官方效果“蓄力速度增加”。由于同一件装备不能出现重复效果，示例最终会自然分配为：每件装备各一条优越代码和攻击力，第三条分别由 1 条最大装弹数和 3 条蓄力速度组成。

模板展示可以使用“装弹”“蓄力速度”等简写，但内部识别规则必须使用官方效果名称“最大装弹数增加”和“蓄力速度增加”，不能把简写直接当作 OCR 目标字符串。

该模板必须使用装备交替调度。12 条有效词条越接近完成，继续提升高分装备的边际成本越高；MDA 应在每次效果变更后重算四件装备的动作长期价值，让容易提高的装备先追上，再回头处理最难的剩余缺口。任何一件装备都只是“本轮未被选中”，不应因为暂停而被永久标记为完成。

#### 3 号栏位的核心词条价值

在这个模板中，“优越代码伤害增加”和“攻击力增加”是**每件装备都必须各有一条**的核心词条，额外 4 条则是可跨装备调配的补充词条。因此 3 号栏位的价值不是只看“是否有效”：

- 3 号栏位是攻或优：最难出现的栏位已经承担一个必选核心；1、2 号栏位只需再容纳另一个核心和任一尚缺的补充词条，后续组合空间更大；
- 3 号栏位是补充词条：虽然计入 12 有效，但 1、2 号栏位必须恰好同时容纳攻和优，后续成功条件更窄；
- 所以在其他条件相同时，`3 栏攻/优` 的状态分数、锁定优先级和保留优先级都必须高于 `3 栏补充有效`。

以“补充词条仍可为最大装弹数或蓄力速度”为例，锁定 3 栏优越代码后，下一次在 1、2 栏同时得到“攻击力 + 任一补充词条”的基线概率约为 3.38%；锁定 3 栏最大装弹数后，下一次在 1、2 栏同时得到“优越代码 + 攻击力”的概率约为 1.46%。具体概率会随剩余全局配额和已锁效果变化，但前者的结构价值更高这一判断不变。

## 5. 全局配额下的锁定决策

### 5.1 为什么不能一律锁定

在四有效模板中，某条效果命中后有两种价值：

1. 它可能正好消耗一个仍然需要的全局配额，锁定后能减少再次丢失该配额的风险；
2. 它也可能占用锁定位，使下一次效果变更的订制模块成本上升，或使用自订密钥后还要在每次效果变更后重新加锁。

因此“命中目标就锁”不是总是最优。MDA 的策略算法应比较“锁定后完成剩余配额的期望材料成本”和“不锁定、重新抽取的期望材料成本”。

### 5.2 基础概率参考

下表是未锁定、没有现有效果排除时的基线估计，使用当前研究中的栏位概率和效果权重；锁定或同件已有相同效果后，必须重新计算：

| 目标效果集合                    | 1 号栏位命中 | 2 号栏位命中 | 3 号栏位命中 | 一次结果至少命中 1 条 |
| ------------------------------- | -----------: | -----------: | -----------: | --------------------: |
| 仅优越代码伤害增加              |       10.00% |        5.08% |        3.07% |                18.15% |
| 优越代码伤害增加或攻击力增加    |       20.00% |       10.15% |        6.14% |                34.17% |
| 四种目标效果合计权重 44% 的示例 |       44.00% |       22.03% |       13.23% |                50.67% |

这说明在追逐低概率栏位时，先处理 3 号、再处理 2 号、最后处理 1 号通常更节省；但全局配额、当前已有词条和锁定材料会改变结论。

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
    globalQuota = { effectId: count }   # 可选；四攻四优四有效模板合计为 12
    perEquipmentRule = null             # 四优、四攻四优使用
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
    slot3CoreCount                       # 仅 12 有效模板使用
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

`stateValue` 是结果页的主判定，`scoreCharacterState` 只处理价值相同的平局。后者仍必须把四件装备放在一起做“栏位 → 全局配额”的最大价值匹配；多条效果同时可以满足同一规则时，优先分配给剩余数量最少、替代来源最少的效果。12 有效模板还必须加入 3 号栏位核心词条的结构奖励：同等有效数量下，3 栏攻/优的当前结构分高于 3 栏补充有效。

12 有效模板的默认 `PartialScore` 按以下顺序比较：已匹配的 12 条全局配额数量 → 3 号栏位为攻/优的装备数量 → 每件装备的攻优覆盖程度 → 预计剩余材料成本 → 有效词条的栏位结构。前一个字段相同时才比较后一个字段。由此，两个状态同为 3 条有效时，3 栏攻/优的状态必然优于 3 栏补充词条；但结构奖励不会凭空把无效词条计作一条有效。

### 6.5 单件装备入口

“洗装备”不是另一套随机逻辑，而是把同一个单步调度器限制在一件装备上。它仍然要先读取两种库存、现有效果和永久锁；满足用户指定的栏位规则后，按 `lockSatisfied` 决定是否锁定对应栏位。`anyOf` / `allOf` 的解释与角色模板相同。

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

### 6.6 角色级模板和全局配额

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
- “四攻四优四有效”模板的全局配额总数不是 12，或单件装备目标与全局配额互相矛盾；
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
      → EquipmentRerollDecide           # 四优决策分发
        → Need{Part}（未达标部位）
          → Open{Part}Details（通用打开）
            → ClickChangeEffect（一级效果变更）
              → ConfirmChangeEffect（二级确认）
                → __EquipmentRerollResultButtonsVisible（决策页就绪信号）
                  → __EquipmentRerollLocateChangedSlot1Match（定位变更槽锚点）
                    → EquipmentRerollResultPage（读变更效果 + 决策路由）
                      → ResultClickKeep（维持→继续洗同一件）
                      → ResultClickAccept（接受→ReturnToDecide 调度）
        → EquipmentRerollEnd（四件达标）
```

### 8.2 Pipeline / Go 作用域

| 职责                                                     | 归属                                      |
| -------------------------------------------------------- | ----------------------------------------- |
| 识别（OCR / TemplateMatch / 锚点路由 / 页面流转 / 点击） | Pipeline                                  |
| 跨节点词条快照、效果记录、四优决策、结果路由             | Go（`agent/go-service/equipmentreroll/`） |

- 识别配置尽量放 Pipeline（含 `__` 内部节点），便于 MaaFramework 调试面板可视化；
- Go 通过 `ctx.RunRecognition("节点名", img)` 复用 Pipeline 识别节点做后处理/决策，不再在 Go 内硬编码识别参数；
- 例外：全量扫描按槽位百分比分区，ROI 需由 Go 依据识图结果框动态计算，因此使用 `ctx.RunRecognitionDirect` 执行 OCR，Pipeline 只负责提供槽位锚点与调度。

### 8.3 扫描快照与“不重复全量扫描”

- Go 维护 task 级快照：每部位 3 槽 ×（词条名、数值、锁定状态）。既有决策逻辑继续通过 `GetEquipmentEffects(taskID)` 读取词条名；后续任务可通过 `GetEquipmentSlotScans(taskID)` 读取完整快照（词条 + 数值 + 锁定）。
- `EquipmentRerollScanMain` 首次全量扫描写入快照；之后**不再全量扫描**。
- 职责划分：`EquipmentRerollScan*` 节点只负责「扫描四件装备」；扫描结束后若任务入口是 `EquipmentRerollScanMain`（独立运行/调试），则扫描完腿部即停止；若入口是 `EquipmentRerollMain`（完整洗词条任务），才继续路由到 `EquipmentRerollDecide` 进入洗练决策。
- 收尾细节：独立收尾的“关闭详情页”必须使用普通节点引用（非 `[JumpBack]`），否则关闭后会回跳父节点再次查找关闭按钮，导致已关闭页面识别失败并超时。
- 每次决策页结果：Accept → 用「变更效果」刷新该部位词条快照，并同步更新从结果页 OCR 解析出的数值；锁定关系保留（洗4优不增删锁）；Keep → 保留原快照。
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
- `__EquipmentRerollResultChangedEffectSlot{N}`：OCR 引用各自 `[Anchor]EquipmentRerollChangedSlot{N}` + `[0, 0, 155, 0]`，精确位置，不再用 `0/24/48` 偏移。

### 8.5 决策页就绪与按钮兜底

- 点击二级效果变更后，`__EquipmentRerollResultButtonsVisible` 检测左下角「效果维持」出现作为决策页就绪信号（替代固定延时/画面冻结），超时未出现则失败退出，避免未进入决策页死循环。
- `EquipmentRerollResultPage`（Go 决策 + 路由）：
    - Accept → `EquipmentRerollResultClickAccept`（点右下「效果变更」）→ `EquipmentRerollReturnToDecide`；
    - Keep → `EquipmentRerollResultClickKeep`（点左下「效果维持」）→ `EquipmentRerollConfirmChangeEffect` 继续洗同一件。
- `ClickKeep` / `ClickAccept` next 含自身节点兜底，防止点击失效误跳流程。

### 8.6 节点对照表

| 节点                                            | 职责                             | 关键参数                                                                  |
| ----------------------------------------------- | -------------------------------- | ------------------------------------------------------------------------- |
| `EquipmentRerollScanMain` / `Flow`              | 全量扫描入口 / 编排              | 独立运行扫描后停止，完整任务内继续到 Decide                               |
| `EquipmentRerollScanDetailsPageEntered`         | 确认详情资讯页并设扫描锚点       | OCR 详情资讯                                                              |
| `__EquipmentRerollLocateSlot1/2/3Match`         | 左侧分槽模板定位第1/2/3槽标记    | `InspectSlot1/2/3.png`（左侧）、0.9、index 0/1/2、ROI `[480,467,321,105]` |
| `__EquipmentRerollSlot1/2/3AffixOCR/ValueOCR`   | 词条/数值区域 OCR                | `[Anchor]SlotN` + roi_offset                                              |
| `__EquipmentRerollSlot1/2/3LockBlue/Orange`     | 锁定状态颜色判定                 | ColorMatch 蓝/橙、count 20                                                |
| `EquipmentRerollScanSlot1/2/3`                  | 逐槽扫描业务：复用子识别并写快照 | Go ScanSlotRecognition                                                    |
| `EquipmentRerollScanCloseDetails`               | 关闭装备详情页                   | CommonCloseButton                                                         |
| `EquipmentRerollDecide`                         | 四优决策分发、设洗练锚点         | part=all/头/臂/身/腿                                                      |
| `EquipmentRerollNeed{Part}`                     | 判断某部位需洗                   | Go PartNeedRecognition                                                    |
| `EquipmentRerollOpen{Part}Details`              | 通用打开部位详情                 | Head/Arms/Torso/Legs 模板                                                 |
| `EquipmentRerollClickChangeEffect`              | 一级效果变更（装备详情页）       | OCR 效果变更                                                              |
| `EquipmentRerollConfirmChangeEffect`            | 二级确认效果变更（确认页）       | OCR 效果变更                                                              |
| `__EquipmentRerollResultButtonsVisible`         | 决策页就绪信号                   | OCR 效果维持                                                              |
| `__EquipmentRerollLocateChangedSlot1/2/3Match`  | 决策页模板定位第1/2/3变更槽标记  | `ResultSlot.png`、0.9、index 0/1/2                                        |
| `__EquipmentRerollResultChangedEffectSlot1/2/3` | 读变更效果（Go 复用）            | `[Anchor]ChangedSlot1/2/3`、`[0,0,155,0]`                                 |
| `EquipmentRerollResultPage`                     | 读变更效果 + 决策路由            | Go ResultDecide + RouteAction                                             |
| `EquipmentRerollResultClickKeep/Accept`         | 点维持/接受（自身兜底）          | OCR 按钮                                                                  |
| `EquipmentRerollReturnToDecide`                 | 关闭回人物页直接调度（不重扫）   | JumpBack 关闭                                                             |
| `EquipmentRerollEnd`                            | 任务结束                         | -                                                                         |

### 8.7 Go 组件对应

| Go 组件                                  | 对应节点 / 场景                                                                             |
| ---------------------------------------- | ------------------------------------------------------------------------------------------- |
| `EquipmentRerollScanBeginAction`         | `EquipmentRerollScanBegin{Part}`（初始化扫描状态）                                          |
| `EquipmentRerollScanSlotRecognition`     | `EquipmentRerollScanSlot1/2/3`（复用 Pipeline 子识别，解释并记录词条/数值/锁定）            |
| `EquipmentRerollScanRouteAction`         | `EquipmentRerollScanSlot3`（展示扫描摘要并路由下一部位；独立入口停止，完整任务继续 Decide） |
| `EquipmentRerollPartNeedRecognition`     | `EquipmentRerollDecide` 的分支（AllSatisfied / Need{Part}）                                 |
| `EquipmentRerollResultDecideRecognition` | `EquipmentRerollResultPage`（读变更效果 + 决策）                                            |
| `EquipmentRerollResultRouteAction`       | `EquipmentRerollResultPage`（按决策路由到维持/接受）                                        |
