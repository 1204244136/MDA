# Android 构建与测试包

工作流为 `.github/workflows/android.yml`（`build-android-apk`），参考 MaaEnd 的 meta → agents → apk 三阶段。
产物统一是国际服、Shizuku/root、后台 1280×720、arm64-v8a 的 APK。
正式包在 `v*` tag 时由 `install.yml` 以 `workflow_call`（`assemble: release`）调用本工作流构建，与桌面包一起进入 GitHub Release，再由 `mirrorchyan_release.yml` 上传 Mirror酱；PR 与手动 debug 仍只产出 Actions 测试包，不接 Release 与 Mirror酱。

## 发布与测试链路

| 触发                                   | 产物                              | 去向                                             |
| -------------------------------------- | --------------------------------- | ------------------------------------------------ |
| `v*` tag → `install.yml`               | 正式包 `com.aliothmoon.maafw.mda` | GitHub Release + Mirror酱 `MDA`（android/arm64） |
| PR 改动 Android、资源、Go Agent 或工具 | PR 包 `...membershippr`           | 仅 Actions artifact                              |
| 手动运行（`assemble: debug`）          | 联调包 `...membershiptest`        | 仅 Actions artifact                              |

`android.yml` 也支持手动选 `assemble: release` 出正式包，同样需要正式签名 Secrets；常规发版走 tag 链路，不要在手动构建里另造版本号。
`workflow_dispatch` 的 `tag` 留空时要求 HEAD 正好在某个 `v*` tag 上，不再回退到「最近可达 tag」；`maafw_version` 留空沿用 `Android/ci-versions.json`。tag 链路会显式传入 tag 与桌面包同版本的 MaaFramework。

正式包资产名为 `MDA-android-arm64-v8a-<version>.apk`，`<version>` 是 tag 去掉 `v`。Release 步骤只把 APK 放到资产根目录，SHA256 与 JSON 构建信息留在 Actions artifact。
Mirror酱 由 `mirrorchyan_release.yml` 的两个 job 分担，二者共用桌面 RID `MDA`：桌面 job 走 `os` × `arch` 矩阵，`mirrorchyan_android` 用 `os=android`、`arch=arm64` 匹配 `MDA-android-arm64-v8a-*`。

## 如何拿测试包

代码进入 GitHub 后，在 Actions 中选择 **build-android-apk → Run workflow**（`assemble` 保持 `debug`）。
完成后下载名称以 `MDA-android-arm64-test-` 开头的 artifact，解压后安装其中的 APK。
同一 artifact 中有 SHA256 和 JSON 构建信息，包含代码提交、外壳和框架版本、构建号、Agent 哈希和签名指纹，用于排查装错包、缓存旧资源和签名变化。
测试 artifact 保留 14 天，正式 artifact 保留 90 天；下载的文件可自行长期保存。

志愿者只需安装发给他们的 APK，测试后导出并发送整个日志 ZIP，无需核对哈希或解压日志。
构建信息由维护者保留，分析时与日志一起使用。

PR 改动 Android、资源、Go Agent 或构建工具时自动检查并产出独立 PR 包：

| 来源     | 包名                                      | 签名及用途                                         |
| -------- | ----------------------------------------- | -------------------------------------------------- |
| 手动运行 | `com.aliothmoon.maafw.mda.membershiptest` | 固定测试证书，适合连续真机会员联调                 |
| PR       | `com.aliothmoon.maafw.mda.membershippr`   | runner 临时 debug 证书，适合验证能否构建及单次测试 |

PR 包不会覆盖会员联调版，每次 PR 构建的证书可能不同，不能保证互相覆盖安装。
不要把 PR 包用于已有会员的连续换包验证，优先分发手动构建的包。
两种包都启用客户端六字段映射和实际会员/额度检查；Gradle debug 不等于 Go 的 dev 免会员模式。
手机单独绑定、单独开通，并使用与电脑不同的会员 ID。
两种测试包与正式包包名不同，可与正式包并存安装。

## 版本号规则

| 项目        | 正式包                                        | 测试包                                               |
| ----------- | --------------------------------------------- | ---------------------------------------------------- |
| version     | 取 `v*` tag 去掉 `v`，如 `v1.12.3` → `1.12.3` | `X.Y.Z-android.ci.<run>.<attempt>+<commit>`          |
| versionCode | 主仓库提交数（`git rev-list --count HEAD`）   | `100000 + run_number × 100 + run_attempt`            |
| 包名        | `com.aliothmoon.maafw.mda`                    | `...membershiptest` / `...membershippr`              |
| Artifact    | `MDA-android-arm64-v8a-<version>`             | `MDA-android-arm64-test-<version>` / `-pr-<version>` |
| 保留期      | 90 天（APK 本体常驻 Release）                 | 14 天                                                |

正式 tag 必须形如 `v1.12.3`（major ≥ 1，允许 `-beta.1` 之类的预发布后缀），由 `python tools/android_ci.py release-meta --tag <tag>` 解析；versionCode 随主仓库提交数单调递增，不依赖本地计数器。
测试版本取最近的 `vX.Y.Z` tag，生成 `X.Y.Z-android.ci.<run>.<attempt>+<commit>`，统一写入 APK、PI 资源和 Go Agent；major 必须 ≥ 1，缺少有效 tag 会失败。重跑次数限制为 1–99；不要删除重建工作流后继续沿用同一包名而不规划构建号。
`android-build-metadata` 这个中间 artifact 无论哪种模式都保留 14 天。

## 签名配置

正式签名由 `tools/android_ci.py prepare-signing` 解码，密码只经环境变量传给 Gradle；测试包由 `tools/android_ci.py sign` 用固定测试证书重新签名。两套签名分开管理，不能共用同一个 keystore，也不要拿测试证书签正式包。

正式签名（`v*` tag 发版必需）：

| 类型     | 名称                      | 内容                              |
| -------- | ------------------------- | --------------------------------- |
| Secret   | `ANDROID_KEYSTORE_BASE64` | 正式 keystore 文件的 Base64，单行 |
| Secret   | `ANDROID_STORE_PASSWORD`  | keystore 密码                     |
| Secret   | `ANDROID_KEY_ALIAS`       | 签名 alias                        |
| Secret   | `ANDROID_KEY_PASSWORD`    | 对应私钥密码                      |
| Variable | `ANDROID_CERT_SHA256`     | 证书 SHA256 指纹，可带冒号        |

缺少任一项时 `prepare-signing` 直接失败；`verify --release` 还会核对 APK 实际签名指纹与 `ANDROID_CERT_SHA256` 一致，不符则不上传 artifact。

固定测试签名（手动 debug 出联调包必需）：

| 类型     | 名称                           | 内容                                  |
| -------- | ------------------------------ | ------------------------------------- |
| Secret   | `ANDROID_TEST_KEYSTORE_BASE64` | 固定测试 keystore 文件的 Base64，单行 |
| Secret   | `ANDROID_TEST_STORE_PASSWORD`  | keystore 密码                         |
| Secret   | `ANDROID_TEST_KEY_ALIAS`       | 签名 alias                            |
| Secret   | `ANDROID_TEST_KEY_PASSWORD`    | 对应私钥密码                          |
| Variable | `ANDROID_TEST_CERT_SHA256`     | 证书 SHA256 指纹，可带冒号            |

优先沿用之前手机上会员联调版的签名 keystore，避免覆盖安装失败及签名相关的 App Android ID 改变。
配置前用 `keytool -list -v -keystore <文件> -alias <alias>` 核对证书指纹。
不要将 keystore、密码或 Base64 写进 Git、日志、issue 或发给志愿者。
缺少任一项时，手动构建直接失败；指纹不符也不上传最终 APK。

仅在确实要换测试证书时创建新 keystore，并长期备份。更换签名无法覆盖原应用，卸载或清除数据前须先导出并保存日志 ZIP。
PR 使用 `pull_request` 事件，不使用 `pull_request_target`，不提供固定测试私钥。

## 版本与依赖

`Android/ci-versions.json` 锁定 MaaFwApp commit、MaaFramework、NDK 和 SDK，并记录正式包的包名、ABI 与资产名前缀。
测试/PR 通道使用这里的 `maafw_tag`；正式包与桌面包使用 `install.yml` 解析出的同一 MaaFramework release（未固定时取 latest）。两者不一致时，发版前必须用 `workflow_dispatch` + `assemble=release` 干跑一次，确认该框架组合能构建并通过验收。
当前身份桥接补丁只验证过锁定的外壳 commit，升级外壳时需要同时审查补丁及回归。
CI 不修改旁边的 MaaFwApp 仓库，独立检出到 runner 的 `.cache/android-shell`。

## 发布前检查

1. 仓库 Secrets 已配置 `ANDROID_KEYSTORE_BASE64`、`ANDROID_STORE_PASSWORD`、`ANDROID_KEY_ALIAS`、`ANDROID_KEY_PASSWORD`，Variable `ANDROID_CERT_SHA256` 与实际证书指纹一致。
2. Mirror酱 的 `MDA` 项目已支持 `android` / `arm64` 平台（客服确认复用原 RID，不需要新建项目）。
3. 正式 tag 一旦推送，`install.yml` 会立即触发发布流程，因此无法在推送前用同一个 tag 干跑正式包。发布前只做不依赖 tag 的检查：正式签名 Secrets/Variable、Mirror酱 项目，以及 `Android/release.profile.yaml` 与 `Android/ci-versions.json` 的包名、ABI、产物名一致。
4. 想在合并后提前验证 release 变体，只能在本地按「开发与构建」执行 `assembleRelease`（自行设置 `KEYSTORE_*`，不发布），或接受正式包的 tag 相关校验随发布流程一起完成。
5. 推送 `v*` tag 后，`install.yml` 会调用本工作流。Android 失败会阻断整个 Release，因此正式 tag 应尽量在 release 变体本地构建通过后再推。
6. 正式包需要能覆盖安装上一版：新旧包必须使用同一正式签名；换签名后只能卸载重装。

## 构建校验

Agent 阶段执行 Go vet/test、Android 资源和会员桥接相关的 Python 回归、实际 Java 兼容层测试，
然后交叉编译并检查 ELF 架构、Android linker 和注入版本。
同时下载同一框架版本的 Linux 主机运行时，执行真实 Agent IPC 回归：
先在任务事件中反向读取控制器，再验证会员计时产生实际扣减与结束结算。
这个测试使用空白控制器和本地会员样例，不访问会员服务，不连接手机。
APK 阶段初始化 OCR 模型，部署锁定框架，应用身份桥接，再验证包名、版本、arm64 库、OCR、
会员开关、入包 Agent build ID、签名及 16KB ZIP 对齐，最后生成可追溯的 artifact。
正式包的资源还要求带 GitHub 与 Mirror酱 更新源，测试包则明确不允许携带更新源。

跨仓库 DoroPay Miniflare 测试依赖旁边的 DoroPay 源码与 npm 依赖，当前没有纳入此 CI；
现有 Go 映射测试和共享向量仍会执行。服务端六字段协议及数据库维持原状。
CI 成功说明构建及这些静态/离线检查通过，会员开通、重启与换绑仍需真机验证。

维护者持有故障现场图时，还可以设置 `MDA_MAA_LIB`、`MDA_BATTLE_SCREENSHOT`、
`MDA_SHOP_SCREENSHOT`，从仓库根目录运行
`go -C agent/go-service test . -run '^TestAndroidPipelineReplay$' -count=1`。
它只回放识别和模拟点击：验证手机战斗 HUD 的正反例、商店漏点重试和重试上限。
现场图保存在本机忽略目录，不能纳入提交；志愿者不需要运行这些命令。

只验证测试包时不必在本机重新编译 APK，也不必触发桌面 install 工作流；Android 正式包只在 `v*` tag 时随桌面包一起构建。
