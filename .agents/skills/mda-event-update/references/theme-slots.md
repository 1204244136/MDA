# 主题槽位与视觉变体

本页记录从现有素材归纳出的命名维度，以及怎样在仓库里读出一个主题的真实槽位。新主题入库后按末尾命令重新核对。

## 命名语法

`{Theme}` + `[Story1|Story2]` + `Stage` + `[Normal|Hard]` + `[SP]` + `[Left]` + `[Repeatable]`

- 大活动和有 Story 的小活动带 `StoryN`；无 Story 的小活动直接用 `StageNormal` / `StageHard`。
- `Story1` 的既有素材始终只有一张，没有难度变体；难度变体只出现在 `Story2`（大活动、ProjectMatis）或无 Story 小活动。
- `SP` 是主题特有的额外标记样式：StarAnis / WaveToYou 的 `Story2StageHardSP`，BitterSpice 的 `StageNormalSP` 与 `StageNormalRepeatableSP`。它不是难度，也不是普适规则。
- `Left` 只用于同模式同角色存在两套样式、且差异来自列位置的主题：目前只有 MARIAn（Story1 与 Story2Hard 各一套）。

## 现网槽位

| 主题               | 类型 | Story           | 难度槽位                | 额外变体                                   |
| ------------------ | ---- | --------------- | ----------------------- | ------------------------------------------ |
| ArkRanger          | 大   | Story1 / Story2 | Story2 Normal + Hard    | 无                                         |
| CoinRushShowdown   | 大   | Story1 / Story2 | Story2 Normal + Hard    | 无                                         |
| PersonaOnFrontline | 大   | Story1 / Story2 | Story2 Normal + Hard    | 无                                         |
| StarAnis           | 大   | Story1 / Story2 | Story2 Normal + Hard    | `Story2StageHardSP`                        |
| WaveToYou          | 大   | Story1 / Story2 | Story2 Normal + Hard    | `Story2StageHardSP`                        |
| MARIAn             | 大   | Story1 / Story2 | Story2 Normal + Hard    | `Left`（Story1 两列、Story2Hard 两列）     |
| B-SideIdol         | 小   | 无              | StageNormal + StageHard | 无                                         |
| BitterSpice        | 小   | 无              | StageNormal + StageHard | `StageNormalSP`、`StageNormalRepeatableSP` |
| GreatVillainUnion  | 小   | 无              | StageNormal + StageHard | 无                                         |
| LittleWitchTrick   | 小   | 无              | StageNormal + StageHard | 无                                         |
| ProjectMatis       | 小   | Story1 / Story2 | Story2 Normal + Hard    | 无                                         |

每个主题的 `Logo` 是入口模板，不参与上述槽位。

## 变体的判据

- **同模式同角色出现两套以上样式，就都要模板**：MARIAn 的左右列 Event 在同一帧里分数相差 0.93 与 0.61，单张模板盖不住另一列。
- **变体不是难度**：`SP` 不是 Hard，`Left` 不是第二个难度；命名与 case 模板数组都要按真实语义写。
- **主题没有的变体不造**：新主题是否带 `SP` / `Left` 由该主题素材决定；`check:theme` 只校验结构与归属，不会替素材是否真实存在把关。

## 重新生成清单

```powershell
Get-ChildItem assets\resource\image\LargeEvent, assets\resource\image\SmallEvent -Directory | ForEach-Object {
    $t = $_.Name
    "{0}/{1}: {2}" -f $_.Directory.Name, $t, ((Get-ChildItem $_.FullName -File).Name -replace "^$t", '' -replace '\.png$', '' -join ', ')
}
```

先读 `assets/tasks/LargeEvent.json` / `SmallEvent.json` 里对应 case 的模板数组，再与本清单交叉核对：case 的列表必须与 base 一致（`npm run check:theme` 断言 B），且不应引用不存在的文件（`npx maa-tools check`）。
