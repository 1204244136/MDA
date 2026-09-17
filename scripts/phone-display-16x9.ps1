<#
.SYNOPSIS
    手机端 16:9 显示适配：临时设置逻辑分辨率（可选屏幕常亮），并在宿主程序退出后自动还原。

.DESCRIPTION
    pipeline 的识别基准是 1280x720（16:9）。多数手机屏幕是 20:9，
    直接截图时框架会按短边等比缩放得到 1600x720，ROI 无法对齐。
    本脚本用 `wm size <竖屏形状的 16:9 覆盖值>` 让游戏按 16:9 渲染
    （横屏旋转后正好是 1920x1080），并且**在宿主程序（MXU 等）退出后自动还原**，
    避免手机停留在异常分辨率上。

    两种动作：
      设置（默认）      设置分辨率（可选屏幕常亮）→ 启动后台守护进程 → 立即退出
      -Restore          还原分辨率与屏幕常亮（可随时手动执行）
      守护进程          由「设置」自动拉起：等待宿主进程退出后执行还原

    推荐用法：把「设置」加为 MXU 的**前置程序**。
    注意 MXU 没有后置钩子，所以还原由本脚本的守护进程负责；若守护进程被强杀，
    重启手机同样可恢复（wm size 覆盖值不跨重启）。

.PARAMETER Adb
    adb.exe 的完整路径（必填）。

.PARAMETER Serial
    设备序列号，即 `adb devices` 输出第一列（必填）。

.PARAMETER Size
    竖屏形状的覆盖尺寸，默认 1080x1920（游戏横屏旋转后为 1920x1080）。
    ⚠ 不要填 1920x1080：横屏形状的覆盖值在竖屏面板上会导致画面错位。

.PARAMETER KeepScreenOn
    同时设置「充电时屏幕常亮」，还原时会恢复成原来的值。

.PARAMETER Restore
    只执行还原，不做设置。

.PARAMETER WatchPid
    内部使用：等待该进程退出后再还原（由「设置」自动传入宿主进程 PID）。

.PARAMETER NoWatch
    不启动守护进程（之后需手动 -Restore，或重启手机）。

.PARAMETER MaxWatchHours
    守护进程最长等待时长，默认 24 小时，超时也会还原。

.PARAMETER StateFile
    记录变更的状态文件，默认放在临时目录。

.EXAMPLE
    # 作为 MXU 前置程序：程序填 pwsh.exe，参数填下面这一行
    #   -NoProfile -ExecutionPolicy Bypass -File "<MDA目录>\scripts\phone-display-16x9.ps1" -Adb "<adb.exe 路径>" -Serial "<设备序列号>" -KeepScreenOn
    # 把 <...> 换成自己的路径与序列号（序列号可用 `adb devices` 查看）

.EXAMPLE
    # 手动还原
    pwsh -File scripts/phone-display-16x9.ps1 -Adb "<adb.exe 路径>" -Serial "<设备序列号>" -Restore
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Adb,
    [Parameter(Mandatory = $true)][string]$Serial,
    [string]$Size = '1080x1920',
    [switch]$KeepScreenOn,
    [switch]$Restore,
    [int]$WatchPid = 0,
    [switch]$NoWatch,
    [int]$MaxWatchHours = 24,
    [string]$StateFile = (Join-Path $env:TEMP 'mda-phone-display.state.json')
)

$ErrorActionPreference = 'Stop'

function Write-Log {
    param([string]$Message)
    Write-Host ("[phone-display] " + $Message)
}

function Invoke-Adb {
    param([string[]]$AdbArgs)
    $output = & $Adb -s $Serial @AdbArgs 2>&1
    $code = $LASTEXITCODE
    return [pscustomobject]@{
        Code   = $code
        Output = (($output | ForEach-Object { "$_" }) -join "`n").Trim()
    }
}

function Assert-Device {
    if (-not (Test-Path -LiteralPath $Adb)) {
        throw "找不到 adb：$Adb"
    }
    $state = Invoke-Adb @('get-state')
    if ($state.Code -ne 0 -or $state.Output -notmatch '^device$') {
        throw "设备 $Serial 不可用（adb get-state 返回：$($state.Output)）。请确认已插好、已授权 USB 调试。"
    }
}

function Get-ScreenState {
    $size = Invoke-Adb @('shell', 'wm', 'size')
    $stayOn = Invoke-Adb @('shell', 'settings', 'get', 'global', 'stay_on_while_plugged_in')
    return [pscustomobject]@{
        Size   = $size.Output
        StayOn = $stayOn.Output
    }
}

# 找到宿主进程 PID（优先匹配 mxu，其次退回直接父进程），用于退出后自动还原
function Resolve-HostPid {
    param([int]$FromPid)
    $start = $FromPid
    if ($start -le 0) { $start = $PID }
    $cur = $start
    for ($i = 0; $i -lt 6; $i++) {
        $proc = Get-CimInstance Win32_Process -Filter "ProcessId=$cur" -ErrorAction SilentlyContinue
        if (-not $proc) { break }
        if ($proc.Name -match '^mxu') { return [int]$proc.ProcessId }
        if ($proc.ParentProcessId -le 0) { break }
        $cur = [int]$proc.ParentProcessId
    }
    $self = Get-CimInstance Win32_Process -Filter "ProcessId=$start" -ErrorAction SilentlyContinue
    if ($self) { return [int]$self.ParentProcessId }
    return 0
}

function Invoke-Restore {
    param([string]$Reason)
    $changed = $false
    if (Test-Path -LiteralPath $StateFile) {
        try {
            $state = Get-Content -LiteralPath $StateFile -Raw | ConvertFrom-Json
        }
        catch {
            $state = $null
        }
        if ($state) {
            if ($state.KeepScreenOn -and $null -ne $state.OldStayOn) {
                $old = "$($state.OldStayOn)"
                if ($old -eq '' -or $old -eq 'null') {
                    $r = Invoke-Adb @('shell', 'settings', 'delete', 'global', 'stay_on_while_plugged_in')
                }
                else {
                    $r = Invoke-Adb @('shell', 'settings', 'put', 'global', 'stay_on_while_plugged_in', $old)
                }
                Write-Log "屏幕常亮已恢复为原值（$old）"
                $changed = $true
            }
        }
        Remove-Item -LiteralPath $StateFile -Force -ErrorAction SilentlyContinue
    }
    $res = Invoke-Adb @('shell', 'wm', 'size', 'reset')
    if ($res.Code -ne 0) {
        Write-Log "还原失败：$($res.Output)"
        return 1
    }
    $after = Invoke-Adb @('shell', 'wm', 'size')
    Write-Log "分辨率已还原（$Reason）。当前状态：$($after.Output -replace "`n", ' / ')"
    if (-not $changed) { Write-Log "（没有记录到需要恢复的屏幕常亮设置）" }
    return 0
}

# 守护模式：等宿主退出后还原
if ($Restore -and $WatchPid -gt 0) {
    $deadline = (Get-Date).ToUniversalTime().AddHours($MaxWatchHours)
    Write-Log "守护进程已启动：等待宿主进程 $WatchPid 退出（最长 $MaxWatchHours 小时）"
    while ($true) {
        if (-not (Get-Process -Id $WatchPid -ErrorAction SilentlyContinue)) {
            Write-Log "宿主进程 $WatchPid 已退出，开始还原"
            break
        }
        if ((Get-Date).ToUniversalTime() -gt $deadline) {
            Write-Log "等待超时（$MaxWatchHours 小时），强制还原"
            break
        }
        Start-Sleep -Seconds 2
    }
    try { $null = Assert-Device } catch { Write-Log "还原时设备不可用：$($_.Exception.Message)"; exit 1 }
    exit (Invoke-Restore -Reason '宿主退出')
}

Assert-Device

if ($Restore) {
    exit (Invoke-Restore -Reason '手动执行')
}

# ---- 设置 ----
$before = Get-ScreenState
Write-Log "当前：$($before.Size -replace "`n", ' / ')"

$res = Invoke-Adb @('shell', 'wm', 'size', $Size)
if ($res.Code -ne 0) {
    throw "设置分辨率失败：$($res.Output)"
}

if ($KeepScreenOn) {
    $null = Invoke-Adb @('shell', 'svc', 'power', 'stayon', 'true')
}

$after = Invoke-Adb @('shell', 'wm', 'size')
if ($after.Output -notmatch [regex]::Escape($Size)) {
    Write-Log "⚠ 未确认到 Override size=$Size，当前：$($after.Output -replace "`n", ' / ')"
}
else {
    Write-Log "已设置 Override size=$Size（游戏横屏后为 16:9，框架识别图 1280x720）"
}

$state = [pscustomobject]@{
    Adb          = $Adb
    Serial       = $Serial
    Size         = $Size
    KeepScreenOn = [bool]$KeepScreenOn
    OldStayOn    = $before.StayOn
    AppliedAtUtc = (Get-Date).ToUniversalTime().ToString('o')
}
$state | ConvertTo-Json | Set-Content -LiteralPath $StateFile -Encoding UTF8

if ($NoWatch) {
    Write-Log "已跳过守护进程；请记得手动还原：-Restore，或重启手机"
    exit 0
}

$hostPid = Resolve-HostPid -FromPid $WatchPid
if ($hostPid -le 0) {
    Write-Log "⚠ 未能识别宿主进程，未启动守护进程；请手动 -Restore 或重启手机"
    exit 0
}

$shell = (Get-Process -Id $PID).Path
$watchArgs = @(
    '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $PSCommandPath,
    '-Adb', $Adb, '-Serial', $Serial, '-Restore', '-WatchPid', "$hostPid",
    '-MaxWatchHours', "$MaxWatchHours", '-StateFile', $StateFile
)
Start-Process -FilePath $shell -ArgumentList $watchArgs -WindowStyle Hidden | Out-Null
Write-Log "已启动守护进程（PID $hostPid 退出后自动还原）。立即还原可用：-Restore"
exit 0
