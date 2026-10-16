---
name: dmp-analysis
description: 分析 Windows 崩溃转储文件（.dmp），诊断 MaaFramework / MXU 项目及其依赖项的崩溃。用于 issue 日志包或附件中发现 .dmp 文件，或用户要求分析 DMP / 崩溃转储时。
---

# Windows DMP Analysis for MaaFramework / MXU Projects

## Scope

- Windows minidump (`.dmp`) files from MaaFramework / MXU based projects.
- 崩溃通常可能来自 MaaFramework C++ 运行时、MXU Rust/Tauri 层、控制器模块、第三方视觉/推理依赖，或项目自身扩展。
- 默认覆盖 x86_64；aarch64 需要替换对应下载包架构。

## Prerequisites

需要 `minidump-stackwalk` 和 `dump_syms`。

检查：

```powershell
Get-Command minidump-stackwalk
Get-Command dump_syms
```

若本地缺失，按当前项目 CI / workflow / 文档安装；不要在未经用户同意时擅自安装全局工具。

## Workflow

### 1. 获取 DMP

将 `.dmp` 放到临时分析目录，例如：

```text
.cache/dmp-analysis/issue-<number>/
.cache/dmp-analysis/local-<timestamp>/
```

来源可能是：

- issue 附件中的 `.dmp`。
- 日志包内部的 `.dmp`。
- 用户本地提供的崩溃转储文件。

### 2. 无符号快速分析

```powershell
minidump-stackwalk "<path-to-dmp>"
```

先获取：

- OS 信息。
- 异常类型和异常码。
- 崩溃线程。
- 模块列表。
- 崩溃地址和模块偏移。

根据异常地址或 crashing thread 顶部帧识别疑似崩溃模块。

### 3. 与日志关联

DMP 文件名常包含崩溃进程 PID，例如：

```text
Project.exe.18188.dmp → PID 18188
```

用该 PID 与日志交叉验证：

- `maafw.log` 中的 `[Px18188]`。
- `mxu-tauri.log` 中的实例 / agent / 进程生命周期。
- `go-service.log` 或 agent stdout/stderr 中的启动信息。

确认 DMP 对应的运行会话，避免把不同时间的日志混在一起。

### 4. 确定关键版本

DMP 模块版本字段可能为空，优先从日志和配置获取：

| 优先级 | 来源             | 线索                                          |
| ------ | ---------------- | --------------------------------------------- |
| 1      | `mxu-tauri.log`  | MXU / MaaFramework 初始化版本                 |
| 2      | `go-service.log` | PI environment / client version               |
| 3      | config 文件      | `interface.json`、`maa_option.json`、项目配置 |
| 4      | issue 正文       | 用户报告版本                                  |
| 5      | DMP 模块列表     | 模块 version 字段，可能为 `?`                 |

记录：

- MaaFramework version。
- MXU version。
- 项目自身版本。
- 控制器类型（Win32 / ADB / 其他）。

### 5. 下载 PDB 符号

#### MaaFramework

```powershell
$MaaVersion = "<version>"
$Work = "<analysis-dir>"
Invoke-WebRequest "https://github.com/MaaXYZ/MaaFramework/releases/download/v$MaaVersion/MAA-win-x86_64-v$MaaVersion.zip" -OutFile "$Work\maa-fw.zip"
Expand-Archive "$Work\maa-fw.zip" "$Work\maa-fw" -Force
```

常见 PDB：

| PDB                       | 模块                    |
| ------------------------- | ----------------------- |
| `MaaFramework.pdb`        | MaaFramework.dll        |
| `MaaUtils.pdb`            | MaaUtils.dll            |
| `MaaToolkit.pdb`          | MaaToolkit.dll          |
| `MaaWin32ControlUnit.pdb` | MaaWin32ControlUnit.dll |
| `MaaAdbControlUnit.pdb`   | MaaAdbControlUnit.dll   |
| `MaaAgentServer.pdb`      | MaaAgentServer.dll      |
| `MaaAgentClient.pdb`      | MaaAgentClient.dll      |

#### MXU

```powershell
$MxuVersion = "<version>"
Invoke-WebRequest "https://github.com/MistEO/MXU/releases/download/v$MxuVersion/MXU-win-x86_64-v$MxuVersion.zip" -OutFile "$Work\mxu.zip"
Expand-Archive "$Work\mxu.zip" "$Work\mxu" -Force
```

`mxu.pdb` 通常位于压缩包根目录。

### 6. PDB 转 Breakpad `.sym`

```powershell
$PdbDir = "$Work\pdb"
$Symbols = "$Work\symbols"
New-Item -ItemType Directory -Force $Symbols | Out-Null
Get-ChildItem $PdbDir -Filter *.pdb | ForEach-Object {
    $name = $_.BaseName
    $header = & dump_syms $_.FullName 2>$null | Select-Object -First 1
    $debugId = ($header -split '\s+')[3]
    $dest = Join-Path $Symbols "$name.pdb\$debugId"
    New-Item -ItemType Directory -Force $dest | Out-Null
    & dump_syms $_.FullName > (Join-Path $dest "$name.sym")
}
```

### 7. 符号化 stackwalk

```powershell
minidump-stackwalk "<path-to-dmp>" "$Work\symbols"
```

输出应包含函数名、源码路径和行号。如果符号化失败，报告原因并保留 module+offset 堆栈。

### 8. 分析重点

#### 异常类型

- `EXCEPTION_ACCESS_VIOLATION` (`0xC0000005`)：空指针、悬垂指针、use-after-free。
- `EXCEPTION_STACK_OVERFLOW` (`0xC00000FD`)：无限递归或栈对象过大。
- `EXCEPTION_ILLEGAL_INSTRUCTION` (`0xC000001D`)：CPU 指令集、二进制损坏或错误模块。
- `STATUS_STACK_BUFFER_OVERRUN` (`0xC0000409`)：Windows fast-fail，不一定是真缓冲区溢出。
    - 参数 `0x7` 常见于 `std::terminate()` / `abort()`，可能是未捕获 C++ 异常。
    - 需要看调用者帧，而不是只归因到 `ucrtbase.dll`。
- `EXCEPTION_BREAKPOINT` (`0x80000003`)：断点、assert、panic 或主动触发崩溃。

#### 模块归属

- `Maa*.dll`：MaaFramework 运行时或控制器。
- `mxu.exe`：MXU/Tauri/Rust 层。
- `onnxruntime_*.dll`、`opencv_*`、OCR/推理库：第三方依赖。
- `DirectML.dll`、显卡驱动相关模块：图形/推理运行时。
- `ntdll.dll`、`KERNELBASE.dll`、`ucrtbase.dll`：通常要看上层调用者。

### 9. 关联源码

如果符号化帧涉及 MaaFramework 或 MXU，并能确定版本，按需 clone 对应 tag 到临时目录：

```powershell
git clone --depth 1 --branch "v<VERSION>" "https://github.com/MaaXYZ/MaaFramework.git" ".cache\upstream-src\MaaFramework"
git clone --depth 1 --branch "v<VERSION>" "https://github.com/MistEO/MXU.git" ".cache\upstream-src\MXU"
```

定位到具体源码行后，在报告中给出远端 GitHub blob 链接。

## 输出格式

````markdown
## DMP 分析结果

- DMP 文件：`<filename>`
- 操作系统：`<OS version>`
- 异常类型：`<EXCEPTION_*>` (`<hex>`)
- 崩溃模块：`<module>+<offset>`
- 符号化状态：已符号化 / 仅 module+offset

## 崩溃堆栈（crashing thread）

```text
Frame 0: ...
Frame 1: ...
```

## 关键模块版本

| Module             | Version |
| ------------------ | ------- |
| MaaFramework.dll   | ...     |
| mxu.exe            | ...     |
| Project executable | ...     |

## 根因判断

- 崩溃归属：MaaFramework / MXU / 项目扩展 / 第三方依赖 / 未知
- 分析：...
- 置信度：高 / 中 / 低

## 建议

- 用户可尝试：...
- 开发者修复方向：...
- 还缺的证据：...
````

## Cleanup

分析完成后可删除临时目录；删除前确认没有用户需要保留的附件或中间产物。
