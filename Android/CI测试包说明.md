# Android CI 测试包

工作流为 `.github/workflows/android.yml`，参考 MaaEnd 的 meta → agents → apk 三阶段。
当前只产出国际服、Shizuku/root、后台 1280×720 的 arm64 会员测试 APK。
没有 tag 触发、Release 上传、MirrorChyan 分发或自动更新；不产出正式 release 包。

## 如何拿包

代码进入 GitHub 后，在 Actions 中选择 **build-android-test-apk → Run workflow**。
完成后下载名称以 `MDA-android-arm64-test-` 开头的 artifact，解压后安装其中的 APK。
同一 artifact 中有 SHA256 和 JSON 构建信息，包含代码提交、外壳和框架版本、构建号、
Agent 哈希和签名指纹，用于排查装错包、缓存旧资源和签名变化。
artifact 保留 14 天；下载的文件可自行长期保存。

志愿者只需安装发给他们的 APK，测试后导出并发送整个日志 ZIP，无需核对哈希或解压日志。
构建信息由维护者保留，分析时与日志一起使用。

PR 改动 Android、资源、Go Agent 或构建工具时自动检查并产出独立 PR 包：

| 来源 | 包名 | 签名及用途 |
| --- | --- | --- |
| 手动运行 | `com.aliothmoon.maafw.mda.membershiptest` | 固定测试证书，适合连续真机会员联调 |
| PR | `com.aliothmoon.maafw.mda.membershippr` | runner 临时 debug 证书，适合验证能否构建及单次测试 |

PR 包不会覆盖会员联调版，每次 PR 构建的证书可能不同，不能保证互相覆盖安装。
不要把 PR 包用于已有会员的连续换包验证，优先分发手动构建的包。
两种包都启用客户端六字段映射和实际会员/额度检查；Gradle debug 不等于 Go 的 dev 免会员模式。
手机单独绑定、单独开通，并使用与电脑不同的会员 ID。

## 首次配置固定测试签名

维护者在仓库 Settings → Secrets and variables → Actions 配置一次：

| 类型 | 名称 | 内容 |
| --- | --- | --- |
| Secret | `ANDROID_TEST_KEYSTORE_BASE64` | 固定测试 keystore 文件的 Base64，单行 |
| Secret | `ANDROID_TEST_STORE_PASSWORD` | keystore 密码 |
| Secret | `ANDROID_TEST_KEY_ALIAS` | 签名 alias |
| Secret | `ANDROID_TEST_KEY_PASSWORD` | 对应私钥密码 |
| Variable | `ANDROID_TEST_CERT_SHA256` | 证书 SHA256 指纹，可带冒号 |

优先沿用之前手机上会员联调版的签名 keystore，避免覆盖安装失败及签名相关的 App Android ID 改变。
配置前用 `keytool -list -v -keystore <文件> -alias <alias>` 核对证书指纹。
不要将 keystore、密码或 Base64 写进 Git、日志、issue 或发给志愿者。
缺少任一项时，手动构建直接失败；指纹不符也不上传最终 APK。

仅在确实要换测试证书时创建新 keystore，并长期备份。更换签名无法覆盖原应用，
卸载或清除数据前须先导出并保存日志 ZIP。正式发行签名与测试签名单独管理。
PR 使用 `pull_request` 事件，不使用 `pull_request_target`，不提供固定测试私钥。

## 版本与依赖

`Android/ci-versions.json` 锁定 MaaFwApp commit、MaaFramework、NDK 和 SDK。
当前身份桥接补丁只验证过锁定的外壳 commit，升级外壳时需要同时审查补丁及回归。
CI 不修改旁边的 MaaFwApp 仓库，独立检出到 runner 的 `.cache/android-shell`。

版本取最近的 `vX.Y.Z` tag，生成 `X.Y.Z-android.ci.<run>.<attempt>+<commit>`，
统一写入 APK、PI 资源和 Go Agent。major 必须 ≥ 1；缺少有效 tag 会失败。
`versionCode = 100000 + run_number × 100 + run_attempt`，高于现有本地测试包的 434，
同一工作流重跑和下一次运行均递增，不依赖临时 runner 上的本地计数器。
重跑次数限制为 1–99；不要删除重建工作流后继续沿用同一包名而不规划构建号。

Agent 阶段执行 Go vet/test、Android 资源和会员桥接相关的 Python 回归、实际 Java 兼容层测试，
然后交叉编译并检查 ELF 架构、Android linker 和注入版本。
同时下载同一框架版本的 Linux 主机运行时，执行真实 Agent IPC 回归：
先在任务事件中反向读取控制器，再验证会员计时产生实际扣减与结束结算。
这个测试使用空白控制器和本地会员样例，不访问会员服务，不连接手机。
APK 阶段初始化 OCR 模型，部署锁定框架，应用身份桥接，再验证包名、版本、arm64 库、OCR、
会员开关、入包 Agent build ID、签名及 16KB ZIP 对齐，最后生成可追溯的 artifact。

跨仓库 DoroPay Miniflare 测试依赖旁边的 DoroPay 源码与 npm 依赖，当前没有纳入此 CI；
现有 Go 映射测试和共享向量仍会执行。服务端六字段协议及数据库维持原状。
CI 成功说明构建及这些静态/离线检查通过，会员开通、重启与换绑仍需真机验证。

维护者持有故障现场图时，还可以设置 `MDA_MAA_LIB`、`MDA_BATTLE_SCREENSHOT`、
`MDA_SHOP_SCREENSHOT`，从仓库根目录运行
`go -C agent/go-service test . -run '^TestAndroidPipelineReplay$' -count=1`。
它只回放识别和模拟点击：验证手机战斗 HUD 的正反例、商店漏点重试和重试上限。
现场图保存在本机忽略目录，不能纳入提交；志愿者不需要运行这些命令。

不需要为了测试 CI 在本机重新编译 APK，也不需要触发桌面 install/release 工作流。
