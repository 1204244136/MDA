"""MDA 开发工具脚本包。

使 `tools` 下的平铺脚本（cli_support / configure / install 等）既能被
`python tools/xxx.py` 直接运行，也能以 `tools.xxx` 包内模块被 console script
（如 `uv run setup-workspace`）导入：
- 直接运行时：脚本目录（tools/）在 sys.path 中，平铺导入天然可用；
- 包导入时：先执行本文件，把 tools/ 塞回 sys.path，平铺导入同样可用。
"""

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
