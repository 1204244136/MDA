@echo off
rem 手机端 16:9 显示适配 —— 免参数包装脚本
rem
rem 用途：在 MXU 里作为「前置程序」时，「程序」填本文件、「附加参数」留空，
rem       并**勾选「通过 cmd 启动」**（.cmd 需要经 cmd.exe 执行）。
rem       adb 与设备由 phone-display-16x9.ps1 自动探测；需要手动还原时，
rem       把「附加参数」临时填成 -Restore 即可。
rem
rem 也可在终端直接运行：
rem       scripts\phone-display-16x9.cmd
rem       scripts\phone-display-16x9.cmd -Restore
setlocal
set "PS=pwsh"
where pwsh >nul 2>nul || set "PS=powershell"
"%PS%" -NoProfile -ExecutionPolicy Bypass -File "%~dp0phone-display-16x9.ps1" -KeepScreenOn %*
exit /b %ERRORLEVEL%
