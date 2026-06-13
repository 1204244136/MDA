# 项目代理说明

## 平台与环境

- **操作系统**：Windows（本项目仅面向 Windows）
- **Shell**：PowerShell 7
- **路径分隔符**：在终端命令中引用 Windows 本地路径时优先使用反斜杠 `\`（例如 `C:\Users\...`）；Markdown 链接、URL、前端 import、配置约定等语境按各自规范使用 `/` 或 `\`

## 终端环境

- 本项目在 Windows 的 **PowerShell 7** 中运行终端命令。
- 执行命令时，使用兼容 PowerShell 7 的语法，并为包含空格的路径加引号。
- 除非任务明确要求且工具可用，否则避免使用 Bash 专用语法，例如 heredoc（`cat <<'EOF'`）、依赖 GNU 工具的 Unix 管道，以及 `grep`、`find`、`sed`、`awk` 等命令。
- 避免使用 Linux/macOS 专用命令（例如 `ls`、`cat`、`chmod`、`mkdir -p`、`rm -rf`）。
- 优先使用 PowerShell 原生命令、PowerShell 7 现代特性，或仓库提供的 npm/scripts 命令，减少因 shell 语法差异导致的反复试错。
- 除非任务明确要求，否则不要切换到 `cmd`、Git Bash、WSL Bash 或其他 shell。

## 常用 PowerShell 对照

| Linux/macOS | PowerShell                                     |
| ----------- | ---------------------------------------------- |
| `ls`        | `Get-ChildItem` 或 `dir`                       |
| `cat`       | `Get-Content` 或 `type`                        |
| `mkdir -p`  | `New-Item -ItemType Directory -Force`          |
| `rm -rf`    | `Remove-Item -Recurse -Force`                  |
| `cp -r`     | `Copy-Item -Recurse`                           |
| `mv`        | `Move-Item`                                    |
| `chmod`     | `Set-ItemProperty` 或 `icacls`                 |
| `grep`      | `Select-String`                                |
| `find`      | `Get-ChildItem -Recurse`                       |
| `sed`       | `-replace` 运算符或 `ForEach-Object`           |
| `awk`       | `ConvertFrom-Csv`、`Select-Object` 或 `-split` |

## 跨代理记忆

- 本项目需要维护一份跨代理共享的长期记忆，用来让 Claude Code、Codex 或其他代理在接手时理解同一套项目偏好、工作约束和用户要求，而不是只依赖当前会话上下文。
- 共享跨代理记忆位于 `C:\Users\12042\OneDrive\AgentMemory\MDA.md`；新代理接手或同步长期项目偏好时先查看这里。
- Claude Code 项目记忆位于 `C:\Users\12042\.claude\projects\c--Users-12042-Documents-GitHub-MDA\memory\`；需要跨代理会话持久保存的偏好要与 OneDrive 共享记忆保持同步。
- 与用户对话、生成文件内容、生成 commit 信息时优先使用中文。
- commit 后不要自动 push；由用户决定何时推送。
- commit 信息应使用中文并遵守 Conventional Commits 风格。
- 编辑 i18 本地化文件时，保持与参考文件一致的排序。
- Go 测试从仓库根目录运行；Go module 位于 `agent\go-service`。
- 审查 pipeline 名称时，要检查每个节点的实际职责，不只根据后缀判断。
