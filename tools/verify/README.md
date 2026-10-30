# tools/verify

验证 MaaFramework **运行时语义**的脚本。用于在改动 pipeline / interface 前后确认框架真实行为，
而不是依赖读源码推断。

## 脚本

| 脚本                          | 用途                                                                                                                                               |
| ----------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `verify_override.py`          | 用 ctypes 驱动 `deps/bin/MaaFramework.dll`，实测 `pipeline_override` 的合并语义、`template` 是否整体替换、多 option 叠加顺序、占位符替换与注入风险 |
| `verify_input_persistence.py` | 复刻 `Configurator` 的 input 分支，验证 `input` 值在各类配置状态下的取值来源与持久化行为                                                           |
| `verify_base_as_latest.py`    | 验证「base 节点承载最新主题」方案：往期主题能否正确覆盖 base、`CurrentEvent` 能否省略 override、漏覆盖时的继承行为                                 |

`verify_small_event_routing.py` 加载实际项目资源，验证全部小活动 case 的路由替换、主题模板/动作隔离，以及更新当前主题后回选旧主题；无需设备。

`verify_scene_navigation.py` 加载实际项目资源，通过当前框架 DLL 和虚拟控制器验证目标页复用、纯导航与业务分离、跳回后额度检查次数、选项关闭、无效点击上限，以及「误入竞技场子页时点返回退回首页」和「竞技场累积奖励只认百分比」。识别探针与虚拟动作只用于验证控制流，不证明实机识别或落点；日志写入系统临时目录，不连接设备。

`verify_map_pushing_minimap.py` 用实际项目资源和虚拟控制器验证小地图收起态恢复、展开后的确认、已展开时不重复点击，以及点击无效时不放行。fixture 仅保留真实 UI 的识别区域，周边为合成画面；可用 `--image <截图路径>` 替换收起态输入。日志写入系统临时目录，不连接设备。

## 运行

从仓库根目录：

```bash
.venv/Scripts/python.exe tools/verify/verify_override.py
.venv/Scripts/python.exe tools/verify/verify_input_persistence.py
.venv/Scripts/python.exe tools/verify/verify_base_as_latest.py
.venv/Scripts/python.exe tools/verify/verify_small_event_routing.py
.venv/Scripts/python.exe tools/verify/verify_scene_navigation.py
.venv/Scripts/python.exe tools/verify/verify_map_pushing_minimap.py
```

`verify_override.py` 依赖 `deps/bin/MaaFramework.dll` 及同目录配套 DLL，
资源取自 `tools/verify/verify-resource/`（含最简 pipeline 与模板图）。

## 已确认的框架语义

以下为实测结论（对应 `verify_override.py` 的断言），改动 pipeline 时需遵守：

1. `pipeline_override` 是**按 prop 深合并** —— 未指定的字段继承 base 节点。
2. `template` 是**整体替换**，不是追加。
3. 多个 option 的 override **按顺序叠加**，后应用者覆盖前者。
   优先级：`global_option` < `resource.option` < `controller.option` < `task.option`。
4. 同一 `select` option **只应用选中的一个 case**。
5. `MaaResourceGetNodeData` 返回的是框架内部**完整解析后**的节点（含 `enabled`、`max_hit`、`post_delay` 等默认值），可用于 dump 调试。
6. 单个占位符承载逗号串时**不会自动拆成数组**，而是当作一个字符串。
7. `input` 的占位符替换**存在 JSON 注入面** —— 值中含 `"` 会改变数组元素数量。
8. `MaaResourceGetNodeData` 可读回节点当前生效的识别参数，是排查「override 是否生效」的首选手段。

## 相关文档

- `docs/event-theme-routing.md` —— 活动主题「当前活动」路由的重复问题分析、方案对比与完整验证报告。

`verify_base_as_latest.py` 中关于偏移继承与显式归零的断言保留为框架风险验证；生产小活动的专属扫荡偏移已迁入主题节点，不再推荐在共用节点上添加偏移并让其他主题逐个归零。
