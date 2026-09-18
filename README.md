<!-- markdownlint-disable MD033 MD041 -->
<p align="center">
  <img alt="LOGO" src="https://s1.imagehub.cc/images/2026/05/12/d1d0730a19f251d8ea800897754f0ab2.png" width="256" height="256" />
</p>

<div align="center">

# MDA

Maa Doro Assistant

**简体中文** | **[English](README_en.md)**

</div>

<p align="center">
  <img alt="Go" style="display:inline-block" src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white">
  <img alt="MaaFramework" style="display:inline-block" src="https://img.shields.io/badge/MaaFramework-%2300BFFF">
  <img alt="platform" style="display:inline-block" src="https://img.shields.io/badge/platform-Windows%20%7C%20macOS%20%7C%20Linux-blueviolet">
  <img alt="license" style="display:inline-block" src="https://img.shields.io/github/license/1204244136/MDA">
  <br>
  <img alt="release" style="display:inline-block" src="https://img.shields.io/github/v/release/1204244136/MDA">
  <img alt="commit" style="display:inline-block" src="https://img.shields.io/github/commit-activity/m/1204244136/MDA">
  <img alt="stars" style="display:inline-block" src="https://img.shields.io/github/stars/1204244136/MDA?style=social">
  <img alt="downloads" style="display:inline-block" src="https://img.shields.io/github/downloads/1204244136/MDA/total?style=social">
  <a href="https://mirrorchyan.com/zh/projects?rid=MDA&os=windows&arch=x64&channel=stable&source=mdagh-badge" target="_blank"><img alt="mirrorc" style="display:inline-block" src="https://img.shields.io/badge/Mirror%E9%85%B1-%239af3f6?logo=countingworkspro&logoColor=4f46e5"></a>
</p>

MDA 是一款基于 [MaaFramework](https://github.com/MaaXYZ/MaaFramework) 开发的游戏自动化辅助工具，由 [DoroHelper](https://github.com/1204244136/DoroHelper) 重写而来。它可以帮你自动完成游戏中的日常任务与活动内容，省时省力、解放双手。

---

## ✨ 功能特性

MDA 内置了多种任务，覆盖日常、活动与实用工具，全部可以在程序内按需开启：

### 前置流程

- 🚪 **进入大厅**：返回游戏大厅，为后续所有任务准备统一的起始界面。

### 日常任务

- 📅 **每日奖励**：一键领取友情点、邮箱、任务、Pass 等各类每日奖励。
- 🏠 **前哨基地**：领取防御奖励，完成派遣公告栏与突发活动。
- 🛒 **商店**：在普通、竞技场、废铁商店中按需购买商品。
- 💎 **付费商店**：进入付费商店，领取免费礼包等奖励。
- 🧪 **模拟室**：自动完成普通 / 超频模拟室战斗。
- ⚔️ **竞技场**：自动挑战新人、特殊、冠军竞技场，领取累积奖励。
- 🗼 **无限之塔**：自动挑战各阵营的无限之塔。
- 🎯 **拦截战**：自动挑战普通 / 异常拦截战并领取奖励。
- 💬 **咨询**：自动咨询妮姬，领取好感度与花絮奖励。

### 周期任务

- 🎪 **大活动**：自动推进大活动（带 SD 小人的活动）的签到、挑战、剧情、任务与小游戏。
- 🎫 **小活动**：自动推进小活动（不带 SD 小人的活动）的挑战、剧情与任务。
- 🔥 **单人突击**：自动完成单人突击的关卡挑战或快速战斗。
- 🤝 **协同作战**：自动完成协同作战并领取奖励。

### 实用工具

- 🎁 **开宝箱**：自动开启物品栏中的各类宝箱。
- 📈 **账号养成**：自动进行角色突破、同步器增强等养成操作。
- 🔨 **洗词条**：对 T10 装备自动洗词条，支持角色与单件两种模式。
- 🗺️ **自动推图**：在地图上自动点击怪物战斗、触发机关，推进主线关卡。
- 🔴 **清除红点**：自动清除各界面上的红点提醒。
- 👥 **好友管理**：删除长期未登录好友、接受全部好友申请。
- 📊 **额度显示**：显示今日已使用和剩余的运行额度，本任务不消耗额度。

> ⚠️ **高级任务**（自动推图、洗词条、自定义爆裂）：当没有可用的专项额度时，按 **5 倍**额度消耗。

---

## 快速开始

### 1. 首次启动

启动后不要急着执行任务，先浏览一下界面，了解有哪些功能和设置项。

### 2. 设置快捷键（推荐）

进入 **右上角「设置」→「快捷键」**，开启全局快捷键。以防程序卡死时无法退出。

### 3. 安装 Interception 模拟点击驱动

「AAA」控制器需先安装 [Interception 驱动](https://github.com/oblitum/interception)，用于解决部分环境下鼠标点击失效的问题。**只有选择该控制器时才需要安装**，其余控制器无需安装。

1. 下载 Interception 官方 Release。
2. 以**管理员身份**打开 CMD / Windows Terminal。
3. 进入 `command line installer` 目录并执行：

    ```bat
    install-interception.exe /install
    ```

4. 重启电脑。

如需卸载驱动，执行：

```bat
install-interception.exe /uninstall
```

> 该方式需要管理员权限，且宿主程序通常需要与游戏相同或更高的权限级别才能生效。

### 4. 手机端 16:9 适配（Android 真机）

MDA 的识别基准是 1280x720（16:9）。多数手机屏幕是 20:9，直接截图时框架会按短边等比缩放得到 1600x720，ROI 对不上，表现为「识别不到界面 / 点不中按钮」。

`scripts/phone-display-16x9.ps1` 会用 `wm size` 把逻辑分辨率临时改成**竖屏形状的 16:9**（1080x1920，游戏横屏旋转后正好是 1920x1080），并在**任务结束或宿主程序退出后自动还原**，不会把手机留在异常分辨率上。

还原由脚本自带的守护进程负责（MXU 只有前置程序、没有后置钩子），按以下顺序判定，满足任一条件即还原：

1. 查询 MXU 本地状态接口（默认 `http://127.0.0.1:12701/api/maa/state`），所有实例 `is_running` 均为 `false` 且持续 8 秒（`-StopDebounceSec` 可调）→ **终止任务或任务跑完都会还原**
2. 宿主进程（MXU）退出
3. 等待任务开始超过 300 秒（`-StartTimeoutSec` 可调）→ 视为未真正启动
4. 状态接口不可达 → 自动回退为「仅监视宿主进程退出」

用法：在 MXU 里把它加为**前置程序**（配置页 → 「+ 添加任务」→「▶️ 前置程序」），两种填法任选一种：

**方式 A：参数恒定（推荐）**

| 字段          | 值                                                                                                   |
| ------------- | ---------------------------------------------------------------------------------------------------- |
| 程序          | `pwsh.exe`                                                                                           |
| 参数          | `-NoProfile -ExecutionPolicy Bypass -File "<MDA 目录>\scripts\phone-display-16x9.ps1" -KeepScreenOn` |
| 等待退出      | 勾选                                                                                                 |
| 已运行时跳过  | **不勾**（默认是勾上的，会让你开着终端时整段被跳过）                                                 |
| 通过 cmd 启动 | 不勾                                                                                                 |

**方式 B：参数完全留空**

| 字段          | 值                                          |
| ------------- | ------------------------------------------- |
| 程序          | `<MDA 目录>\scripts\phone-display-16x9.cmd` |
| 参数          | （留空）                                    |
| 等待退出      | 勾选                                        |
| 已运行时跳过  | **不勾**                                    |
| 通过 cmd 启动 | **勾选**（`.cmd` 必须经 `cmd.exe` 执行）    |

**adb 路径与设备序列号不需要手填**：adb 依次从 `-Adb` → 环境变量 `MDA_ADB_PATH` → 进程 PATH → 注册表 PATH → 常见安装位置（`%LOCALAPPDATA%\Android\Sdk\platform-tools` 等）查找；设备取 `adb devices` 中**唯一**已授权设备（多台时会报错列出候选）。要固定指定时再加 `-Adb "<路径>" -Serial "<序列号>"`，或设置环境变量 `MDA_ADB_PATH` / `MDA_ADB_SERIAL`。

**部分机型需要额外权限（脚本已自动处理）**：MIUI / HyperOS 等厂商 ROM 会锁住 `wm size` 的写入，此时 `wm size` 会抛 `SecurityException`，表现为「设置分辨率失败」。脚本检测到权限被拒后，会自动执行

```powershell
adb shell pm grant com.android.shell android.permission.WRITE_SECURE_SETTINGS
```

并重试一次 `wm size`。也可以手动执行这条命令解锁（该授权**不跨重启**，重启手机后脚本会再次自动授予）。

不放心可以先手动还原一次确认效果：

```powershell
pwsh -File "<MDA 目录>\scripts\phone-display-16x9.ps1" -Restore
```

切记：

- 尺寸**不要填 `1920x1080`**：横屏形状的覆盖值在竖屏面板上会让游戏画面错位
- `wm size` 的覆盖值**不跨重启**；守护进程被强杀时重启手机即可恢复，也可随时手动 `-Restore`
- `WRITE_SECURE_SETTINGS` 授权同样**不跨重启**，重启手机后脚本会自动重新授予，无需手动操作
- 脚本不含任何固定路径与序列号：adb 与设备自动探测，参数与环境变量都可覆盖

> 手机端左右贴边元素（左上角返回键、左侧功能列等）的识别窗口，已经按挖孔安全区导致的偏移加宽过，不需要额外配置。

---

## 语言适配说明

MDA 的界面支持中文、英文等多种语言，但**脚本的功能目前仅适配中文游戏界面**。

如果你使用英文或其他语言的游戏界面，可能会遇到识别错误、功能异常等问题。遇到报错时，请先将游戏切换到**简体中文**界面再尝试。若切换后问题仍然存在，欢迎提交反馈，我们会协助排查。

---

## 相关项目

[BlablalinkTasker](https://github.com/1204244136/BlablalinkTasker) 是一款 Blablalink / NIKKE 社区每日任务自动化工具，支持签到、点赞、浏览和奖励兑换。完成首次登录配置后，可通过 MXU 的「特殊任务」→「自定义程序」调用项目内的 `日常运行.bat`，在运行 MDA 时一并执行。

---

## ⭐ Star 历史

<a href="https://www.star-history.com/#1204244136/MDA&Date">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=1204244136/MDA&type=Date&theme=dark" />
    <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=1204244136/MDA&type=Date" />
    <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=1204244136/MDA&type=Date" />
  </picture>
</a>

---

## 遇到问题？如何反馈

如果脚本运行出错，请按以下步骤收集信息并反馈，这能帮助快速定位问题。

### 第一步：开启调试图像

1. 进入 **「设置」→「调试」**
2. 勾选 **「保存调试图像」**

> ⚠️ 每次启动程序都需要重新开启此选项。

### 第二步：复现问题

调试模式会为每个操作步骤保存截图，因此**不要长时间挂着跑任务**，否则会产生大量图片占用磁盘空间。

推荐做法：

- 开启调试模式后，**只运行有问题的那一个任务**
- 问题复现后立即停止，准备打包日志

### 第三步：导出日志

1. 点击右下角 **「运行日志」** 旁的 **「导出日志」** 图标
2. 在弹出的 `debug` 文件夹中，找到生成的压缩文件
3. 将该压缩文件发送给开发人员

> 💡 每次反馈问题后，建议**删除旧的 `vision` 文件夹**并重启程序，这样能保证每次的调试图像互不混淆，方便排查。
