---
name: go-service-guide
description: MaaFramework Go Agent / go-service 编写指南。用于编写、修改或审查 agent/go-service/ 下的 Go 自定义识别器、动作、EventSink，或了解 MaaFramework Go Agent 的注册、日志、目录组织和参数解析模式。
---

# MaaFramework Go Service 编写指南

## 架构定位

Go Service / Go Agent 只处理 Pipeline 难以表达的复杂逻辑，例如图像算法、状态机、外部数据、系统环境检查、运行时上下文处理等。

不要在 Go 中编写大规模业务流程；业务流程控制应优先由 Pipeline JSON 负责。

除非项目另有说明，所有坐标与图像处理以 720p（1280×720）为基准。

## 常见目录结构

```text
agent/go-service/
├── main.go                 # 入口：初始化、registerAll、启动 AgentServer
├── register.go             # registerAll() 聚合各子包 Register()
├── logger.go               # 日志初始化
├── pkg/                    # 公共工具包
├── common/                 # 通用 Custom 组件
├── taskersink/             # TaskerEventSink / ContextEventSink 实现
└── <business>/             # 业务子包
    ├── register.go         # Register() 注册本包组件
    └── *.go                # 实现文件
```

实际项目可能只使用其中一部分；遵循当前仓库已有结构。

## 注册机制

### 子包 Register()

每个包含 MaaFramework 自定义组件的子包应暴露一个 `Register()` 函数，在其中注册本包所有组件：

```go
package mypkg

import maa "github.com/MaaXYZ/maa-framework-go/v4"

func Register() {
    maa.AgentServerRegisterCustomAction("MyAction", &MyAction{})
    maa.AgentServerRegisterCustomRecognition("MyRecognition", &MyRecognition{})
}
```

注册名称必须与 Pipeline JSON 中 `custom_action` / `custom_recognition` 的名称一致。

### main 聚合

子包 `Register()` 必须在总注册函数中调用：

```go
func registerAll() {
    mypkg.Register()
}
```

遗漏调用会导致 Pipeline 中的 Custom 节点运行时找不到组件。

## 编译期接口校验

所有注册类型应在定义该类型的文件中包含编译期校验：

```go
var _ maa.CustomActionRunner = &MyAction{}
var _ maa.CustomRecognitionRunner = &MyRecognition{}
var _ maa.TaskerEventSink = &MySink{}
var _ maa.ContextEventSink = &MySink{}
```

不要把这些校验集中放在 `register.go`，应靠近类型定义。

## 文件管理

- 一个 Custom 组件的主要实现尽量集中在单个文件。
- 同包内可按职责拆分多个 `.go` 文件，但避免无意义碎片化。
- 参数结构体放在实现文件中，紧跟类型定义。
- 公共工具放到 `pkg/` 或当前项目已有公共包中。

## 命名

- 包名简短、小写，使用单词，不重复上层目录语义。
- 导出类型和方法使用清晰驼峰命名。
- 未导出变量保持简短但可读。
- Pipeline 注册名使用稳定的 PascalCase 字符串。

## 日志

优先使用当前项目已配置的结构化日志库；常见 MaaFramework Go 项目使用 zerolog。

```go
log.Info().
    Str("component", "MyComponent").
    Str("step", "Prepare").
    Msg("prepared input")

log.Error().
    Err(err).
    Str("component", "MyComponent").
    Msg("failed to parse params")
```

规则：

- 上下文用链式字段，避免拼进 `Msg`。
- 错误用 `.Err(err)`。
- 坐标、识别结果、参数等结构化输出为字段。
- 不使用 `log.Printf` / `log.Println`，除非项目明确采用标准库日志。

## 注释

- 导出符号添加 Go 风格注释，以符号名开头。
- 未导出但复杂的逻辑可写简短注释解释为什么这样做。
- 不要为显而易见的代码写解释性注释。

## CustomAction 模板

```go
package mypkg

import (
    "encoding/json"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
    "github.com/rs/zerolog/log"
)

var _ maa.CustomActionRunner = &MyAction{}

type myActionParam struct {
    Target string `json:"target"`
}

// MyAction performs a custom Pipeline action.
type MyAction struct{}

func (a *MyAction) Run(ctx *maa.Context, arg *maa.CustomActionArg) bool {
    var params myActionParam
    if err := json.Unmarshal([]byte(arg.CustomActionParam), &params); err != nil {
        log.Error().
            Err(err).
            Str("component", "MyAction").
            Msg("failed to parse params")
        return false
    }

    return true
}
```

## CustomRecognition 模板

```go
package mypkg

import (
    "encoding/json"

    maa "github.com/MaaXYZ/maa-framework-go/v4"
    "github.com/rs/zerolog/log"
)

var _ maa.CustomRecognitionRunner = &MyRecognition{}

type myRecognitionParam struct {
    Threshold float64 `json:"threshold"`
}

// MyRecognition performs a custom Pipeline recognition.
type MyRecognition struct{}

func (r *MyRecognition) Run(ctx *maa.Context, arg *maa.CustomRecognitionArg) (*maa.CustomRecognitionResult, bool) {
    var params myRecognitionParam
    if err := json.Unmarshal([]byte(arg.CustomRecognitionParam), &params); err != nil {
        log.Error().
            Err(err).
            Str("component", "MyRecognition").
            Msg("failed to parse params")
        return nil, false
    }

    matched := true
    if !matched {
        return nil, false
    }

    return &maa.CustomRecognitionResult{
        Box:    arg.Roi,
        Detail: "{}",
    }, true
}
```

## EventSink 模板

```go
package mypkg

import maa "github.com/MaaXYZ/maa-framework-go/v4"

var _ maa.TaskerEventSink = &MySink{}

// MySink handles task lifecycle events.
type MySink struct{}

func (s *MySink) OnTaskerTask(tasker *maa.Tasker, event maa.EventStatus, detail maa.TaskerTaskDetail) {
    if event != maa.EventStatusStarting {
        return
    }
}
```

如需监听 Context 事件，实现 `maa.ContextEventSink` 并按项目现有方式注册。未使用的回调方法写空实现。

## 错误处理

- 参数解析失败要记录错误并返回 false。
- 外部 I/O、系统调用、图像处理错误要显式处理。
- 不要静默吞掉错误。
- 不要随意 `panic`；除非这是启动期不可恢复配置错误且项目已有相同模式。

## 审查清单

- [ ] 注册名与 Pipeline `custom_action` / `custom_recognition` 完全一致。
- [ ] 子包 `Register()` 已在总注册函数中调用。
- [ ] 编译期接口校验靠近类型定义。
- [ ] 参数解析有错误日志和失败返回。
- [ ] 日志为结构化字段，不拼接上下文。
- [ ] 导出符号有 Go 风格注释。
- [ ] 业务流程没有从 Pipeline 大量搬到 Go。
- [ ] 坐标/图像处理基于项目约定分辨率。
- [ ] 没有无解释的 `time.Sleep`。

## 参考

- MaaFramework Go binding：`github.com/MaaXYZ/maa-framework-go/v4`
- MaaFramework Custom 节点文档：<https://github.com/MaaXYZ/MaaFramework/blob/main/docs/en_us/3.1-PipelineProtocol.md>
