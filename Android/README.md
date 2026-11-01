# Android 手机版

MDA 手机版面向国际服 NIKKE，与桌面版共用同一套任务、选项和预设。正式包随 `v*` tag 构建，与桌面安装包一起进入 GitHub Release，并与桌面共用 Mirror酱 的 `MDA` 项目（按 `os=android`、`arch=arm64` 区分平台）分发；安装后可在应用内检查更新。

| 项目   | 正式版                     |
| ------ | -------------------------- |
| 包名   | `com.aliothmoon.maafw.mda` |
| 应用名 | MDA                        |
| ABI    | 仅 `arm64-v8a`             |
| 权限   | Shizuku 或 root            |
| 屏幕   | 后台虚拟屏 1280×720        |
| 系统   | Android 10+                |

正式包只出 arm64-v8a，不提供 32 位或模拟器 ABI。桌面资源保持原样，Android 资源在构建时另行生成。

## 获取与安装

Release 资产名为 `MDA-android-arm64-v8a-<version>.apk`，`<version>` 是 tag 去掉 `v`：`v1.12.3` 对应 `MDA-android-arm64-v8a-1.12.3.apk`。同一 tag 的桌面包与该 APK 一起发布，构建时使用与桌面版相同的 MaaFramework 版本，Android 与桌面统一由 `install.yml` 调用 `android.yml`（`assemble: release`）产出。

Mirror酱 上的手机版复用桌面 `MDA` 项目（`os=android`、`arch=arm64`），不需要单独 RID。应用内更新由 `Android/release.profile.yaml` 的 `update.mirrorchyanRid: MDA` 指定；PI 资源中的 `mirrorchyan_rid` 同样是 `MDA`。

## 包一览

| 构建来源      | 应用包名                                  | 签名            | 用途                     |
| ------------- | ----------------------------------------- | --------------- | ------------------------ |
| 正式 tag      | `com.aliothmoon.maafw.mda`                | 正式签名        | 正式使用，可应用内更新   |
| CI 手动 debug | `com.aliothmoon.maafw.mda.membershiptest` | 固定测试证书    | 连续会员联调             |
| CI PR         | `com.aliothmoon.maafw.mda.membershippr`   | runner 临时证书 | 独立包名，单次验证       |
| 开发 profile  | `com.aliothmoon.maafw.mda.test`           | 本地 debug      | dev 模式，不验证正式会员 |

四个包名互不相同，可与正式包并存安装。测试通道仍只产出 Actions 测试包，不接 Release 与 Mirror酱。

CI 的两种会员测试包均启用实际会员和额度检查；正式包同样启用正式会员与额度检查。手机需要单独绑定、单独开通，并使用与电脑不同的会员 ID。客户端保持原 DoroPay 六字段协议，详见 [会员身份接入说明](会员身份接入说明.md)。

签名分两套：正式包使用 Secrets `ANDROID_KEYSTORE_BASE64`、`ANDROID_STORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD` 与 Variable `ANDROID_CERT_SHA256`；测试包继续使用 `ANDROID_TEST_*` 五个名字。正式签名与测试签名必须分开管理，混用会让覆盖安装和指纹校验同时失效。

## 任务范围

复用桌面任务、选项和预设，在生成资源中额外提供「启动国际服」和「Agent 自检」。
开放全部任务不代表所有任务已完成真机验证。以下功能依赖实体键盘，手机版请勿使用：

| 功能                        | 按键  | 使用限制                                   |
| --------------------------- | ----- | ------------------------------------------ |
| 自定义爆裂                  | A/S/D | 请勿运行                                   |
| COIN RUSH SHOWDOWN 小游戏   | 空格  | 关闭「大活动小游戏」后再运行该主题其他功能 |
| PERSONA ON FRONTLINE 小游戏 | W/A/D | 关闭「大活动小游戏」后再运行该主题其他功能 |

「当前活动」指向这些主题时同样适用。原动作不改为触控，生成资源中的任务、选项、主题与相关预设附有中英文提示。
测试配方不携带更新源，正式配方保留 GitHub 与 Mirror酱 更新源。

## 真机测试

1. 安装 APK 并授权 Shizuku/root；后台虚拟屏需要 Android 10+。
2. 先运行「Agent 自检」，确认连接和会员/额度信息，再运行「启动国际服」。
3. 完成游戏登录并启用横屏，运行「进入大厅」，之后逐项测试其他任务。
4. 异常时停止任务，从设置导出日志 ZIP 并私下反馈，无需解压、抄哈希或清除游戏数据。

卸载、清数据或重新解压资源前，先导出并另存日志 ZIP。
正式包只能用正式签名的更新覆盖安装，测试包之间换证书同样无法覆盖；持续联调请使用固定签名的手动 CI 包。

## 开发与构建

`tools/build_android.py` 负责复制和标注 Android 专用资源、构建 Agent、部署框架和调用外壳 Gradle。
本地构建同样通过 `uv run --frozen --only-group install python tools/build_android.py <构建参数>` 运行，以加载繁体资源生成依赖。

正式包：

```bash
uv run --frozen --only-group install python tools/build_android.py \
  --release --shell <MaaFwApp 目录> --ndk <NDK 目录> \
  --version 1.12.3 --version-code <构建号> --maafw-tag v5.14.3
```

`--release` 走 `assembleRelease`，正式签名由外壳 Gradle 读取 `KEYSTORE_PATH`、`KEYSTORE_PASSWORD`、`KEY_ALIAS`、`KEY_PASSWORD`（CI 由 `tools/android_ci.py prepare-signing` 解码 Secrets 后注入；本地构建需自行设置），必须带 `--version`（正式 tag 去掉 `v`），配方为 `Android/release.profile.yaml`。
不带参数或带 `--membership` 仍出 debug 测试包，`--pr-build` 只能与 `--membership` 一起使用。

`tools/android_ci.py` 覆盖 CI 各阶段：`meta` / `release-meta`、`agent`、`prepare-signing`、`sign`、`verify`；正式包用 `release-meta --tag <tag>` 取版本，用 `verify --release` 验收签名与更新源。
`tools/android_shell.py` 只对锁定版本的独立外壳 checkout 应用会员桥接补丁。`Android/ci-versions.json` 锁定 MaaFwApp 的 `shell_revision`、MaaFramework、NDK、SDK，以及正式包的包名、ABI 与资产名前缀；会员桥接补丁只验证过该 commit，升级外壳时需同时审查补丁并回归。
生成资源、APK、工具链、签名文件、真机日志均不得提交。
