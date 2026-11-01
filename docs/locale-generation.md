# 繁体界面资源生成

仓库只维护简体中文和英文源文件。繁体界面字典及 WELCOME、CONTACT、DESCRIPTION 文档在打包目录中生成，不提交派生文件。

## 生成器

`tools/generate_zh_tw.py` 使用 [OpenCC 生态的纯 Python 实现 opencc-py](https://pypi.org/project/opencc-py/1.4.2/)，通过 `s2twp` 转换台湾字形和惯用词。实现和字典版本由 `pyproject.toml`、`uv.lock` 锁定，无需为不同操作系统或 CPU 架构编译原生库。

生成时保留简体字典的键名和键序，转换字符串值，并同步生成三个文档、改写 `.file` 引用及注册包内 `interface.json` 的 `zh_tw`。链接目标、代码、占位符保持原样；游戏专有术语的少量修正在生成器的 `VALUE_OVERRIDES` 中按完整键维护。

## 构建入口

- 桌面包：`tools/install.py` 在复制资源并写入版本号后生成。正式发布和 CI 测试包使用相同入口，Mirror 酱上传的发布包也包含繁体资源。
- Android 包：`tools/build_android.py` 在注入手机实体按键警告后生成，保证新增提示和键也进入繁体字典。当前 Android 外壳仍使用简体或英文，生成资源不改变外壳的语言策略。
- 本地预览：复制资源到独立目录后，可手动调用同一生成器：

```powershell
Copy-Item -LiteralPath 'assets' -Destination '.cache\zh-tw-preview' -Recurse
uv run --frozen --only-group install python tools/generate_zh_tw.py .cache\zh-tw-preview
```

预览目录应是全新的独立副本。生成器拒绝直接修改源 `assets`，也拒绝资源路径经符号链接或 junction 逃出包目录；不要对开发安装的链接目录运行生成器。

## 修改与验证

新增任务或改动文案时，继续维护 `zh_cn`、`en_us`。CI 执行以下回归，覆盖键序、文档引用、技术文本保留、Android 提示、重复生成和源文件保护：

```powershell
uv run --frozen --only-group install python -m unittest discover -s tools/verify -p 'test_generate_zh_tw.py'
```

本生成器仅处理界面及三份文档。Go Agent 的语言归一化和运行提示语言维持原有行为。
