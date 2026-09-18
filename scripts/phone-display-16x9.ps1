<#
.SYNOPSIS
    手机端 16:9 显示适配：临时设置逻辑分辨率（可选屏幕常亮），任务结束或宿主退出后自动还原。

.DESCRIPTION
    pipeline 的识别基准是 1280x720（16:9）。多数手机屏幕是 20:9，
    直接截图时框架会按短边等比缩放得到 1600x720，ROI 无法对齐。
    本脚本用 `wm size <竖屏形状的 16:9 覆盖值>` 让游戏按 16:9 渲染
    （横屏旋转后正好是 1920x1080），并在**任务结束或宿主程序（MXU 等）退出后自动还原**，
    避免手机停留在异常分辨率上。

    两种动作：
      设置（默认）      设置分辨率（可选屏幕常亮）→ 启动后台守护进程 → 立即退出
      -Restore          还原分辨率与屏幕常亮（可随时手动执行）
      守护进程          由「设置」自动拉起，满足任一条件即还原：
                          1. MXU 状态接口显示所有实例 is_running 均为 false（任务结束/被终止）
                          2. 宿主进程退出
                          3. 等待任务开始超时（默认 300 秒）
                          4. 守护时长超过上限（默认 24 小时）

    推荐用法：把「设置」加为 MXU 的**前置程序**。
    MXU 没有后置钩子，因此还原由本脚本的守护进程负责；若守护进程被强杀，
    重启手机同样可恢复（wm size 覆盖值不跨重启）。

    权限自愈：
    部分厂商 ROM（MIUI / HyperOS 等）会锁住 `wm size` 的写入，执行时会抛
    SecurityException（用户反馈「手机把 wm size 锁了」）。脚本检测到权限被拒后，
    会自动执行 `pm grant com.android.shell android.permission.WRITE_SECURE_SETTINGS`
    并重试一次。该授权不跨重启，重启手机后脚本会再次自动授予。

.PARAMETER Adb
    adb.exe 的路径。可省略：依次尝试 `-Adb` → 环境变量 `MDA_ADB_PATH` → PATH 里的 `adb`
    → 常见安装位置（%LOCALAPPDATA%\Android\Sdk\platform-tools 等）。

.PARAMETER Serial
    设备序列号。可省略：依次尝试 `-Serial` → 环境变量 `MDA_ADB_SERIAL` →
    `adb devices` 中**唯一**已授权设备（多设备时会报错并列出候选）。

.PARAMETER Size
    竖屏形状的覆盖尺寸，默认 1080x1920（游戏横屏旋转后为 1920x1080）。
    ⚠ 不要填 1920x1080：横屏形状的覆盖值在竖屏面板上会导致画面错位。

.PARAMETER KeepScreenOn
    同时设置「充电时屏幕常亮」，还原时会恢复成原来的值。

.PARAMETER Restore
    只执行还原，不做设置。

.PARAMETER WatchPid
    内部使用：守护进程要监视的宿主进程 PID（由「设置」自动传入）。

.PARAMETER Watch
    内部使用：进入守护模式（等待任务结束/宿主退出后还原）。由「设置」自动拉起，
    手工执行 `-Restore` 不会进入守护模式，而是立即还原。

.PARAMETER NoWatch
    不启动守护进程（之后需手动 -Restore，或重启手机）。

.PARAMETER MaxWatchHours
    守护进程最长等待时长，默认 24 小时，超时也会还原。

.PARAMETER MxuApiUrl
    用于判断「任务是否还在运行」的 MXU 状态接口，默认
    http://127.0.0.1:12701/api/maa/state（MXU 默认 Web 端口 12701）。
    任务结束（所有实例 is_running 均为 false）后即还原；接口不可达时自动回退为
    「仅监视宿主进程退出」。

.PARAMETER StopDebounceSec
    判定「任务已结束」所需的持续时间，默认 8 秒，避免任务间隙误还原。

.PARAMETER StartTimeoutSec
    等待任务开始的最长时间，默认 300 秒；超时视为未真正启动，直接还原。

.PARAMETER NoApi
    不查询 MXU 状态接口，只按宿主进程退出还原。

.PARAMETER StateFile
    记录变更的状态文件，默认放在临时目录。

.EXAMPLE
    # 作为 MXU 前置程序（推荐）：程序填 pwsh.exe，参数填下面这一行
    #   -NoProfile -ExecutionPolicy Bypass -File "<MDA目录>\scripts\phone-display-16x9.ps1" -KeepScreenOn
    # adb 与设备会自动探测：adb 取自 PATH（或 MDA_ADB_PATH），设备取 adb devices 中唯一已授权设备。
    # 需要固定指定时再追加 -Adb "<adb.exe 路径>" -Serial "<序列号>"。

.EXAMPLE
    # 手动设置 / 手动还原
    pwsh -File scripts/phone-display-16x9.ps1 -KeepScreenOn
    pwsh -File scripts/phone-display-16x9.ps1 -Restore
#>
[CmdletBinding()]
param(
    [string]$Adb = '',
    [string]$Serial = '',
    [string]$Size = '1080x1920',
    [switch]$KeepScreenOn,
    [switch]$Restore,
    [int]$WatchPid = 0,
    [switch]$Watch,
    [switch]$NoWatch,
    [int]$MaxWatchHours = 24,
    [string]$MxuApiUrl = 'http://127.0.0.1:12701/api/maa/state',
    [int]$StopDebounceSec = 8,
    [int]$StartTimeoutSec = 300,
    [switch]$NoApi,
    [string]$StateFile = (Join-Path $env:TEMP 'mda-phone-display.state.json')
)

$ErrorActionPreference = 'Stop'

function Write-Log {
    param([string]$Message)
    Write-Host ("[phone-display] " + $Message)
}

# 解析 adb 路径：显式参数 → MDA_ADB_PATH → PATH → 注册表里的 PATH（进程环境可能过期）→ 常见安装位置
function Resolve-AdbPath {
    param([string]$Given)
    if ($Given -and (Test-Path -LiteralPath $Given)) { return $Given }

    if ($env:MDA_ADB_PATH -and (Test-Path -LiteralPath $env:MDA_ADB_PATH)) { return $env:MDA_ADB_PATH }

    $cmd = Get-Command adb -ErrorAction SilentlyContinue
    if ($cmd -and $cmd.Source) { return $cmd.Source }

    # 宿主进程环境可能是在改 PATH 之前启动的，再读一遍注册表里的用户级/系统级 PATH
    foreach ($scope in @('User', 'Machine')) {
        try {
            $regPath = [Environment]::GetEnvironmentVariable('Path', $scope)
        }
        catch {
            $regPath = $null
        }
        if (-not $regPath) { continue }
        foreach ($dir in ($regPath -split ';')) {
            if (-not $dir) { continue }
            $candidate = Join-Path $dir.Trim() 'adb.exe'
            if (Test-Path -LiteralPath $candidate) { return $candidate }
        }
    }

    $candidates = @(
        (Join-Path $env:LOCALAPPDATA 'Android\Sdk\platform-tools\adb.exe'),
        (Join-Path $env:ProgramFiles 'Android\platform-tools\adb.exe'),
        (Join-Path ${env:ProgramFiles(x86)} 'Android\platform-tools\adb.exe'),
        (Join-Path $env:USERPROFILE 'platform-tools\adb.exe')
    )
    foreach ($c in $candidates) {
        if ($c -and (Test-Path -LiteralPath $c)) { return $c }
    }

    throw "找不到 adb：请把 platform-tools 加入 PATH、设置环境变量 MDA_ADB_PATH，或用 -Adb 指定 adb.exe 路径。"
}

# 解析设备序列号：显式参数 → MDA_ADB_SERIAL → adb devices 中唯一已授权设备
function Resolve-Serial {
    param([string]$Given, [string]$AdbPath)
    if ($Given) { return $Given }
    if ($env:MDA_ADB_SERIAL) { return $env:MDA_ADB_SERIAL }

    $raw = (& $AdbPath devices 2>&1 | ForEach-Object { "$_" }) -join "`n"
    $devices = @()
    foreach ($line in ($raw -split "`r?`n")) {
        if ($line -match '^(\S+)\s+device\s*$') { $devices += $Matches[1] }
    }

    if ($devices.Count -eq 1) { return $devices[0] }
    if ($devices.Count -eq 0) {
        throw "没有已授权的设备。adb devices 输出：`n$raw`n请确认已插好数据线、手机已允许 USB 调试（或先用 -Serial 指定）。"
    }
    throw ("检测到多台设备：" + ($devices -join ', ') + "。请用 -Serial 指定，或设置环境变量 MDA_ADB_SERIAL。")
}

function Invoke-Adb {
    param([string[]]$AdbArgs)
    $output = & $script:Adb -s $script:Serial @AdbArgs 2>&1
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

# 部分厂商 ROM（常见于 MIUI / HyperOS 等）把 wm size 的写入权限锁住，
# shell 执行 `wm size` 会抛 SecurityException。此时给 shell 授予
# WRITE_SECURE_SETTINGS 即可解锁（授权随 adb shell 生效，重启手机后需重新授予）。
$script:ShellPkg = 'com.android.shell'
$script:ShellPerm = 'android.permission.WRITE_SECURE_SETTINGS'
$script:PermGrantAttempted = $false
$script:PermGrantOk = $false
$script:PermHintShown = $false

function Test-PermissionError {
    param([string]$Output)
    if (-not $Output) { return $false }
    # PowerShell 的 -match 默认不区分大小写，覆盖 SecurityException / Permission denial 等写法
    return ($Output -match 'SecurityException' -or
        $Output -match 'permission denial' -or
        $Output -match 'permission denied' -or
        $Output -match 'not allowed')
}

function Grant-ShellSecureSettings {
    if ($script:PermGrantAttempted) { return $script:PermGrantOk }
    $script:PermGrantAttempted = $true

    $grant = Invoke-Adb @('shell', 'pm', 'grant', $script:ShellPkg, $script:ShellPerm)
    if ($grant.Code -eq 0 -and $grant.Output -notmatch 'Exception|Error|denied') {
        $script:PermGrantOk = $true
        Write-Log "已为 $($script:ShellPkg) 授予 WRITE_SECURE_SETTINGS（设备锁定了 wm size，需此权限才能改分辨率）"
        return $true
    }

    $script:PermGrantOk = $false
    Write-Log "授予 WRITE_SECURE_SETTINGS 失败：$($grant.Output)"
    return $false
}

# 执行 shell 命令；若因权限被拒，先补授 WRITE_SECURE_SETTINGS 再重试一次
# 注意：部分 ROM 的 wm 会把 SecurityException 打到输出里但退出码仍为 0，
# 因此这里不看退出码、只看输出是否命中权限拒绝对待。
function Invoke-ShellWithPerm {
    param([string[]]$ShellArgs, [string]$Label = 'shell')
    $res = Invoke-Adb (@('shell') + $ShellArgs)
    $denied = Test-PermissionError -Output $res.Output
    if (-not $denied) { return $res }

    if (-not $script:PermGrantAttempted) {
        Write-Log "$Label 被权限拒绝（$($res.Output -replace "`n", ' / ')），尝试授予 WRITE_SECURE_SETTINGS 后重试"
    }
    if (-not (Grant-ShellSecureSettings)) {
        # 只提示一次：该机型可能不允许给 shell 授权，需用户手动处理
        if (-not $script:PermHintShown) {
            $script:PermHintShown = $true
            Write-Log "无法自动解锁。可手动执行：adb -s $Serial shell pm grant $($script:ShellPkg) $($script:ShellPerm)；若仍失败，请关闭手机的「分辨率锁定 / 显示大小锁定」类设置。"
        }
        return $res
    }

    $retry = Invoke-Adb (@('shell') + $ShellArgs)
    if ($retry.Code -eq 0 -and -not (Test-PermissionError -Output $retry.Output)) { return $retry }
    Write-Log "授权后仍失败：$($retry.Output -replace "`n", ' / ')"
    return $retry
}

# wm 子命令（size / density 等）走同一套权限自愈
function Invoke-Wm {
    param([string[]]$WmArgs)
    return Invoke-ShellWithPerm -ShellArgs (@('wm') + $WmArgs) -Label 'wm'
}

function Get-ScreenState {
    $size = Invoke-Wm @('size')
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
                    $r = Invoke-ShellWithPerm -ShellArgs @('settings', 'delete', 'global', 'stay_on_while_plugged_in') -Label 'settings delete'
                }
                else {
                    $r = Invoke-ShellWithPerm -ShellArgs @('settings', 'put', 'global', 'stay_on_while_plugged_in', $old) -Label 'settings put'
                }
                Write-Log "屏幕常亮已恢复为原值（$old）"
                $changed = $true
            }
        }
        Remove-Item -LiteralPath $StateFile -Force -ErrorAction SilentlyContinue
    }
    $res = Invoke-Wm @('size', 'reset')
    if ($res.Code -ne 0 -or (Test-PermissionError -Output $res.Output)) {
        Write-Log "还原失败：$($res.Output)"
        return 1
    }
    $after = Invoke-Wm @('size')
    Write-Log "分辨率已还原（$Reason）。当前状态：$($after.Output -replace "`n", ' / ')"
    if (-not $changed) { Write-Log "（没有记录到需要恢复的屏幕常亮设置）" }
    if ($script:PermGrantOk) {
        Write-Log "提示：本次为 $($script:ShellPkg) 授予的 WRITE_SECURE_SETTINGS 不跨重启，重启手机后脚本会自动重新授予。"
    }
    return 0
}

# ---- 解析 adb 与设备（参数可省略）----
$Adb = Resolve-AdbPath -Given $Adb
$Serial = Resolve-Serial -Given $Serial -AdbPath $Adb
Write-Log "adb：$Adb"
Write-Log "设备：$Serial"

# 守护模式：任务结束或宿主退出后还原（由「设置」以 -Watch 拉起，不要手工加）
if ($Watch -and $Restore) {
    $deadline = (Get-Date).ToUniversalTime().AddHours($MaxWatchHours)
    $startDeadline = (Get-Date).AddSeconds($StartTimeoutSec)
    $useApi = -not $NoApi
    $started = $false
    $stoppedSince = $null
    $apiFailStreak = 0
    Write-Log "守护进程已启动：等待任务开始（最长 $StartTimeoutSec 秒），任务结束或宿主退出后自动还原"

    while ($true) {
        if ((Get-Date).ToUniversalTime() -gt $deadline) {
            Write-Log "等待超时（$MaxWatchHours 小时），强制还原"
            break
        }
        if ($WatchPid -gt 0 -and -not (Get-Process -Id $WatchPid -ErrorAction SilentlyContinue)) {
            Write-Log "宿主进程 $WatchPid 已退出，开始还原"
            break
        }

        if ($useApi) {
            $anyRunning = $null
            try {
                $snapshot = Invoke-RestMethod -Uri $MxuApiUrl -TimeoutSec 3
                $apiFailStreak = 0
                $anyRunning = $false
                if ($snapshot.instances) {
                    foreach ($inst in $snapshot.instances.PSObject.Properties) {
                        if ($inst.Value.is_running) { $anyRunning = $true }
                    }
                }
            }
            catch {
                $apiFailStreak++
                if ($apiFailStreak -eq 5) {
                    Write-Log "状态接口不可达：$MxuApiUrl（$($_.Exception.Message)）"
                    if (-not $started) {
                        $useApi = $false
                        Write-Log "回退为「仅监视宿主进程退出」"
                    }
                }
            }

            if ($null -ne $anyRunning) {
                if ($anyRunning) {
                    if (-not $started) { Write-Log "检测到任务已开始运行" }
                    $started = $true
                    $stoppedSince = $null
                }
                elseif ($started) {
                    if (-not $stoppedSince) {
                        $stoppedSince = Get-Date
                    }
                    elseif (((Get-Date) - $stoppedSince).TotalSeconds -ge $StopDebounceSec) {
                        Write-Log "任务已结束（连续 $StopDebounceSec 秒无运行中实例），开始还原"
                        break
                    }
                }
                elseif ((Get-Date) -gt $startDeadline) {
                    Write-Log "等待任务开始超时（$StartTimeoutSec 秒），按未真正启动处理，开始还原"
                    break
                }
            }
        }

        Start-Sleep -Seconds 2
    }

    try { $null = Assert-Device } catch { Write-Log "还原时设备不可用：$($_.Exception.Message)"; exit 1 }
    exit (Invoke-Restore -Reason '任务结束或宿主退出')
}

Assert-Device

if ($Restore) {
    exit (Invoke-Restore -Reason '手动执行')
}

# ---- 设置 ----
$before = Get-ScreenState
Write-Log "当前：$($before.Size -replace "`n", ' / ')"

$res = Invoke-Wm @('size', $Size)
if ($res.Code -ne 0 -or (Test-PermissionError -Output $res.Output)) {
    throw "设置分辨率失败：$($res.Output)`n脚本已自动尝试 `pm grant $($script:ShellPkg) $($script:ShellPerm)`；若仍失败，请手动执行该命令，或关闭手机的「分辨率锁定 / 显示大小锁定」类设置。"
}

if ($KeepScreenOn) {
    $null = Invoke-ShellWithPerm -ShellArgs @('svc', 'power', 'stayon', 'true') -Label 'svc power stayon'
}

$after = Invoke-Wm @('size')
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
    GrantAttempted = $script:PermGrantAttempted
    GrantOk        = $script:PermGrantOk
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
    '-Watch', '-Restore', '-WatchPid', "$hostPid", '-Adb', $Adb, '-Serial', $Serial,
    '-MaxWatchHours', "$MaxWatchHours", '-StateFile', $StateFile,
    '-MxuApiUrl', $MxuApiUrl, '-StopDebounceSec', "$StopDebounceSec", '-StartTimeoutSec', "$StartTimeoutSec"
)
if ($NoApi) { $watchArgs += '-NoApi' }
Start-Process -FilePath $shell -ArgumentList $watchArgs -WindowStyle Hidden | Out-Null
Write-Log "已启动守护进程：任务结束或宿主进程（PID $hostPid）退出后自动还原；立即还原可用 -Restore"
exit 0
