---
name: maa-logging
description: MaaFramework C++ 日志宏用法指南。用于写 C++ MaaFramework 扩展、Custom 组件或工具代码时使用 LogInfo/LogError/LogWarn/LogDebug/LogTrace、VAR、容器输出和自定义类型日志。
---

# MaaFramework C++ 日志宏用法

头文件：`<MaaUtils/Logger.h>`

## 可用宏

| 宏             | 级别 / 用途                    |
| -------------- | ------------------------------ |
| `LogFatal`     | 致命错误                       |
| `LogError`     | 不可恢复或当前操作失败的错误   |
| `LogWarn`      | 可恢复异常、环境风险、降级行为 |
| `LogInfo`      | 关键状态、任务阶段、初始化结果 |
| `LogDebug`     | 调试信息、中间结果             |
| `LogTrace`     | 高频或详细数据                 |
| `LogFunc`      | 函数作用域 enter/leave 和耗时  |
| `VAR(x)`       | 格式化为 `[x=value]`           |
| `VAR_VOIDP(x)` | 将指针转为 `void*` 后输出      |

## 核心原则：不要手动拼日志字符串

使用 `<<` 流式输出，各片段自动分隔：

```cpp
LogInfo << "matched" << VAR(zone_id) << VAR(x) << VAR(y) << VAR(score);
LogError << "locator init failed";
```

避免：

```cpp
LogInfo << "position: " + std::to_string(x) + ", " + std::to_string(y);
LogInfo << std::format("position: ({}, {})", x, y);
```

`std::format` 可用于一般字符串构建，但日志中优先直接输出字段。

## 典型用法

```cpp
#include <MaaUtils/Logger.h>

MaaBool MyRecognitionRun(MaaContext* context, MaaTaskId task_id, const char* node_name)
{
    LogInfo << VAR(context) << VAR(task_id) << VAR(node_name);

    if (context == nullptr) {
        LogError << "context is null";
        return MAA_FALSE;
    }

    LogDebug << "recognition input prepared" << VAR(task_id);
    return MAA_TRUE;
}
```

## 多变量输出

```cpp
LogInfo << "phase transition" << VAR(from_phase) << VAR(to_phase) << VAR(reason);
LogDebug << "match result" << VAR(score) << VAR(x) << VAR(y) << VAR(width) << VAR(height);
```

## 容器直接输出

`vector`、`set`、`map<string, T>` 等 STL 容器可以直接 `<<`，会自动序列化为 JSON：

```cpp
std::vector<std::string> names = { "a", "b", "c" };
LogInfo << "names" << names;

std::map<std::string, int> config;
LogInfo << "config" << config;
```

不需要手写循环打印容器内容。

## 自定义类型输出

给结构体加 `MEO_TOJSON(...)` 或 `MEO_JSONIZATION(...)`，即可直接输出：

```cpp
struct LocateOutput {
    int status = 0;
    std::string message;
    int x = 0;
    int y = 0;

    MEO_JSONIZATION(status, message, MEO_OPT x, MEO_OPT y)
};

LogInfo << VAR(output);
```

容器嵌套也可以工作：

```cpp
std::vector<LocateOutput> results;
LogInfo << VAR(results);
```

## 日志级别建议

| 场景                          | 级别       |
| ----------------------------- | ---------- |
| 初始化成功/失败、关键状态变更 | `LogInfo`  |
| 任务阶段切换、最终识别结果    | `LogInfo`  |
| 中间分数、候选数量、调试变量  | `LogDebug` |
| 完整矩阵、向量、逐帧详细数据  | `LogTrace` |
| 可恢复异常、环境风险          | `LogWarn`  |
| 当前操作无法继续              | `LogError` |
| 进程级不可恢复错误            | `LogFatal` |

## 高频日志约束

- 每帧推理、循环匹配、滚动扫描中的大量数据不要用 `LogInfo`。
- 详细数组、完整 softmax、候选列表优先用 `LogTrace`。
- 用户需要定位问题的关键结果用 `LogInfo` 或 `LogDebug`。

## 原理简述

`LogStream::stream` 通常按优先级尝试：

1. 可构造 `json::value` → `dumps()` 输出。
2. 可构造 `json::array` → `dumps()` 输出。
3. 可构造 `json::object` → `dumps()` 输出。
4. 有 `operator<<` → 直接流输出。
5. 以上都不满足 → 编译期报错。

如果遇到 “Unsupported type”，给类型加 `MEO_TOJSON`、`MEO_JSONIZATION`，或特化 `json::ext::jsonization<T>`。

## 审查清单

- [ ] 日志没有手动拼接多个字段。
- [ ] 关键上下文使用 `VAR(x)` 输出。
- [ ] 高频详细数据不使用 `LogInfo`。
- [ ] 错误路径有 `LogError` 或合理级别日志。
- [ ] 自定义类型日志支持 JSON 化或流输出。
