# 活动主题「当前活动」路由：重复问题的完整验证报告

> 验证环境：`C:\Users\12042\Documents\GitHub\MaaFramework`（源码）+ `deps/bin` 实际二进制
> 验证日期：2026-09-10
> 复现脚本：`tools/verify/verify_*.py`

## 结论摘要

**推荐方案：让 resource 侧的 base 节点直接承载「最新主题」的模板，`CurrentEvent` 不再需要 `pipeline_override`。**

适配新主题时只需改一处（base 节点）+ 新增该主题 case 的 override（本来就要写），
`CurrentEvent` 的 35~40 行重复书写降为 0。

**代价**：老用户 `maa_pi_config.json` 里仍是旧 case 名，需重选一次主题（已确认可接受）。

**必配护栏**：校验所有主题 case 的 `pipeline_override` 节点集合一致，防止漏覆盖导致静默继承 base。

---

## 一、下发逻辑的事实（已核实 CI）

以下来自仓库 CI 配置，非推断：

| 环节     | 事实                                                            | 证据                                              |
| -------- | --------------------------------------------------------------- | ------------------------------------------------- |
| 触发条件 | 改动 `assets/**` 即触发 install 构建                            | `.github/workflows/install.yml:18`                |
| 打包方式 | `assets/resource` 与 `assets/tasks` 一起 copy 进同一 `install/` | `tools/install.py:73-90`（`install_resource()`）  |
| 发布产物 | 整个 `install/` 目录打成一个 zip                                | `install.yml:256-268`                             |
| 分发渠道 | MirrorChyan 以整包上传（`filetype: latest-release`）            | `.github/workflows/mirrorchyan_release.yml:23-27` |

**关键结论**：`resource` 与 `tasks` **永远同版本、同一次更新**，
不存在「新 tasks 配旧 resource」或反之的错配可能。

因此 **修改 base 节点是安全的** —— 老用户的 resource 会随包一起更新。

---

## 二、验证方法与结果

### 2.1 对 MaaFramework.dll 的直接驱动验证（12/12 通过）

用 ctypes 调用公开 C API（`MaaResourcePostBundle` → `MaaResourceOverridePipeline` → `MaaResourceGetNodeData`），
实测 override 合并语义。

| 验证项                         | 结果 | 实测数据                                                                                      |
| ------------------------------ | ---- | --------------------------------------------------------------------------------------------- |
| override 是按 prop 深合并      | ✅   | 只给 `template` 时，base 的 `order_by=Vertical`、`index=-1`、`roi=[314,103,661,604]` 全部继承 |
| `template` 是整体替换          | ✅   | override `[A,B]` → 结果 `[A,B]`，不是 `[RedDot,A,B]`                                          |
| 多 option override 按顺序叠加  | ✅   | 先 `[FROM_A]` 后 `[FROM_B]` → 结果 `[FROM_B]`                                                 |
| 占位符替换能产出合法 JSON 数组 | ✅   | `["{s1}","{s2}"]` → 替换后解析出 2 个元素，框架接受                                           |
| 单占位符装逗号串               | ✅   | `"A.png,B.png"` 被当作**单个**字符串，不会自动拆成数组                                        |
| 值含双引号会注入               | ⚠️   | `A.png", "INJECTED` → template 变成 3 个元素，结构被注入                                      |

**补充发现**：`MaaResourceGetNodeData` 返回的是**框架内部完整解析后的节点**
（含 `enabled`、`max_hit=4294967295`、`post_delay=200` 等框架默认值），证明 override 走了完整的
`PipelineParser::parse_node` 合并链。

### 2.2 base 承载最新主题方案的可行性验证（8/8 通过）

脚本 `tools/verify/verify_base_as_latest.py`。

| 假设                                    | 结果 | 含义                                               |
| --------------------------------------- | ---- | -------------------------------------------------- |
| A. 往期主题 override 完整替换 base 模板 | ✅   | 改 base **不污染**往期主题                         |
| B. 不应用任何 override 时 base 模板保留 | ✅   | **`CurrentEvent` 可以完全没有 override**           |
| C. 空 override `{}` 不清空 base         | ✅   | 即使 case 写了空对象也安全                         |
| D. 未覆盖的节点落到 base                | ⚠️   | **风险点**：旧主题若覆盖不全，会继承最新主题的模板 |

### 2.3 `input` 方案的否决依据（7/7 通过）

脚本 `tools/verify/verify_input_persistence.py`。

源码依据 `source/MaaPiCli/Impl/Configurator.cpp:481-511` 与 `source/MaaPiCli/CLI/interactor.cpp:1564-1603`。

| 场景                                 | 结果              | 说明                                         |
| ------------------------------------ | ----------------- | -------------------------------------------- |
| 新用户（配置无该 option）            | ✅ 用 default     | `Configurator.cpp:486-491`                   |
| 老用户（配置有旧值）+ 资源包换新主题 | ❌ 仍用旧值       | 与 `default_case` 同样的坑                   |
| 老用户只配置了部分 input             | ❌ 新旧混合       | 产生诡异中间态                               |
| 用户敲回车                           | ❌ default 被固化 | `interactor.cpp:1584` 取值，`:1600` 写入配置 |

**否决理由**：`input` 的定位是「用户数据快照」，与 `CurrentEvent` 需要的「每次启动重新解析最新值」
方向相反。且引入 JSON 注入面。

---

## 三、推荐方案

### 3.1 改动内容

**resource 侧（base 节点）**：把占位模板换成最新主题的真实模板。

```diff
  "SmallEventEnterMainPage": {
      "recognition": {
          "type": "TemplateMatch",
          "param": {
              "roi": [799, 571, 170, 68],
-             "template": ["Common/RedDot.png"]   // 占位
+             "template": ["SmallEvent/GreatVillainUnion/GreatVillainUnionLogo.png"]
          }
      }
  }
```

同样处理 `SmallEventClickStage`、`SmallEventClickStageRepeatable`（LargeEvent 对应三个节点同理）。

**tasks 侧（`CurrentEvent`）**：删除 `pipeline_override`，保留 `label` 与 `option` 子选项。

```diff
  {
      "name": "CurrentEvent",
      "label": "$option.SmallEventTheme.CurrentEvent",
      "description": "$option.SmallEventTheme.CurrentEvent.description",
-     "pipeline_override": { ... 35~40 行 ... }
+     // 无需 override：base 即最新主题
  }
```

注意 `LargeEvent` 的 `CurrentEvent` 带 `option: ["LargeEventPersonaOnFrontlineMiniGame"]`，
该子选项必须保留，只删 `pipeline_override`。

### 3.2 适配新主题的操作流

1. 改 base 节点的 `template`（resource 侧，3 个节点）
2. 新增该主题 case 的 `pipeline_override`（tasks 侧，本来就要写）
3. 跑 `npm run check:theme` 自检

`CurrentEvent` 完全不需要动。

### 3.3 必配护栏：节点集合一致性校验

验证 D 暴露的真实风险。现状已存在该结构（实测扫描结果）：

| 活动       | case                                      | 覆盖节点数                                 |
| ---------- | ----------------------------------------- | ------------------------------------------ |
| SmallEvent | 全部主题                                  | 3（一致）                                  |
| LargeEvent | **ArkRanger**                             | **4**（额外含 `LargeEventMissionClaimed`） |
| LargeEvent | StarAnis / WaveToYou / PersonaOnFrontline | 3                                          |

现在无害，因为 `LargeEventMissionClaimed` 的 base 是中性默认值（`ColorMatch` 默认阈值）。
**一旦 base 改成最新主题的值**，`StarAnis` 等主题就会静默继承 `PersonaOnFrontline` 的阈值 —— 真 bug。

护栏逻辑（`scripts/check-theme-sync.mjs`）：

1. 对每个活动，收集所有主题 case 的 `pipeline_override` 节点名集合。
2. 断言各主题的节点集合**完全一致**（允许多余，不允许缺失）。
3. 若不一致，列出「哪个主题缺哪个节点」，提示补全。
4. 额外校验：`CurrentEvent` **不应**含 `pipeline_override`。

挂到 `package.json`：`"check:theme": "node scripts/check-theme-sync.mjs"`，进 CI。

---

## 四、方案对比（最终版）

| 方案                  | 消除重复     | 老用户自动跟随 | 前提        | 结论                   |
| --------------------- | ------------ | -------------- | ----------- | ---------------------- |
| 现状（两处手写）      | 否           | ✅             | —           | 可用但脆弱             |
| 复制粘贴 + 逐字校验   | 否（仅护栏） | ✅             | —           | 次选，不解决书写量     |
| `input` + 占位符      | ✅           | ❌ 失效        | —           | **否决**（持久化反向） |
| **base 承载最新主题** | ✅           | ❌ 需重选一次  | 同包下发 ✅ | **推荐**               |
| 构建期生成 override   | ✅           | ✅             | 需额外流程  | 不必，已被推荐方案覆盖 |

---

## 五、对 AGENTS.md 的建议

替换现有「活动主题『当前活动』路由」小节：

> - `LargeEventTheme` 和 `SmallEventTheme` 的首个 case 固定为 `CurrentEvent`（显示名「当前活动」），
>   并且是该选项的 `default_case`。
> - **`CurrentEvent` 不需要 `pipeline_override`**：resource 侧 `EnterMainPage` / `ClickStage` /
>   `ClickStageRepeatable` 等 base 节点的 `template` 直接承载最新主题的模板，base 即默认值。
> - **适配新主题时的动作**：改 base 节点的 `template` → 新增该主题 case 的 `pipeline_override`。
>   `CurrentEvent` 无需改动；`label` / `description` / `option` 保持不变。
> - **禁止**在 base 节点写 `Common/RedDot.png` 这类占位模板 —— 占位会让「无 override」的 case 失效。
> - **所有主题 case 的 `pipeline_override` 节点集合必须一致**（允许超集，不允许缺项），
>   否则缺失节点会静默继承 base（最新主题）的值。该约束由 `npm run check:theme` 强制校验。
> - 往期主题 case 保留各自的 `pipeline_override` 不动，供用户回选仍在开放的老活动。
> - 本设计依赖「resource 与 tasks 同包下发」（见 `tools/install.py`），
>   若未来改为分包发布，需重新评估。
> - `CurrentEvent` 不是主题名，locale 显示名用「当前活动」/ `Current Event`，不适用全大写约定。

---

## 附：复现方式

```bash
# 从仓库根目录运行，脚本自动定位 deps/bin/MaaFramework.dll 与 tools/verify/verify-resource
.venv/Scripts/python.exe tools/verify/verify_override.py
.venv/Scripts/python.exe tools/verify/verify_input_persistence.py
.venv/Scripts/python.exe tools/verify/verify_base_as_latest.py
```

脚本说明见 `tools/verify/README.md`。
