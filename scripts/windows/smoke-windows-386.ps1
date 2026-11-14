param(
    [Parameter(Mandatory=$true)][string]$PackageDir,
    [int]$Port = 18080,
    [string]$ExpectedVersion = '',
    [switch]$KeepRunning
)
$ErrorActionPreference = 'Stop'
$PackageDir = [IO.Path]::GetFullPath($PackageDir)
$stateDir = Join-Path $PackageDir 'smoke-state'
New-Item -ItemType Directory -Force $stateDir | Out-Null
$report = @{ success=$false; os=[Environment]::OSVersion.VersionString; host_os_64bit=[Environment]::Is64BitOperatingSystem }
$process = $null
try {
    foreach ($name in @('graywolf.exe','graywolf-modem.exe')) {
        $path = Join-Path $PackageDir $name
        $bytes = [IO.File]::ReadAllBytes($path)
        $peOffset = [BitConverter]::ToInt32($bytes,0x3c)
        $machine = [BitConverter]::ToUInt16($bytes,$peOffset+4)
        if ($machine -ne 0x14c) { throw "$name is not an x86 PE executable" }
        $report[$name+'_machine'] = '0x014c (x86)'
    }
    $exe = Join-Path $PackageDir 'graywolf.exe'
    $modem = Join-Path $PackageDir 'graywolf-modem.exe'
    $report.graywolf_version = (& $exe version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Graywolf version command failed' }
    $report.modem_version = (& $modem --version | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Modem version command failed' }
    if ($report.graywolf_version -ne $report.modem_version) { throw 'Go and Rust version stamps differ' }
    if ($ExpectedVersion -and $report.graywolf_version -ne $ExpectedVersion) { throw 'Packaged version does not match the expected commit' }
    $tests = Join-Path $PackageDir 'configstore.test.exe'
    if (Test-Path $tests) {
        $testProcess = Start-Process -FilePath $tests -ArgumentList '-test.timeout=90s' -WorkingDirectory $PackageDir -NoNewWindow -RedirectStandardOutput (Join-Path $stateDir 'configstore-tests.txt') -RedirectStandardError (Join-Path $stateDir 'configstore-errors.txt') -Wait -PassThru
        if ($testProcess.ExitCode -ne 0) { throw 'Windows x86 configstore migration/persistence tests failed' }
        $report.configstore_tests = 'PASS'
    }
    $arguments = @('-config',('"'+(Join-Path $stateDir 'graywolf.db')+'"'),
        '-history-db',('"'+(Join-Path $stateDir 'history.db')+'"'),
        '-tile-cache-dir',('"'+(Join-Path $stateDir 'tiles')+'"'),
        '-modem',('"'+$modem+'"'),'-http',"127.0.0.1:$Port")
    $stdout = Join-Path $stateDir 'stdout.log'
    $stderr = Join-Path $stateDir 'stderr.log'
    $process = Start-Process -FilePath $exe -ArgumentList $arguments -WorkingDirectory $PackageDir -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru
    $deadline = (Get-Date).AddSeconds(40)
    $version = $null
    while ((Get-Date) -lt $deadline) {
        if ($process.HasExited) { throw "Graywolf exited: $($process.ExitCode)" }
        try { $version = Invoke-RestMethod "http://127.0.0.1:$Port/api/version" -TimeoutSec 2; break } catch { Start-Sleep -Milliseconds 500 }
    }
    if (-not $version) { throw 'HTTP server did not start' }
    $report.http_version = $version
    $deadline = (Get-Date).AddSeconds(20)
    $modemReady = $false
    while ((Get-Date) -lt $deadline) {
        $logs = (Get-Content $stdout,$stderr -Raw -ErrorAction SilentlyContinue) -join "`n"
        if ($logs -match 'modembridge state.*state=RUNNING') { $modemReady=$true; break }
        Start-Sleep -Milliseconds 500
    }
    if (-not $modemReady) { throw 'Rust modem did not reach RUNNING' }
    $report.modem_state = 'RUNNING'
    $page = Invoke-WebRequest "http://127.0.0.1:$Port/" -UseBasicParsing
    if ($page.StatusCode -ne 200) { throw 'Web UI not served' }
    $report.web_ui = 'HTTP 200'
    $report.url = "http://127.0.0.1:$Port/"
    $report.pid = $process.Id
    $report.success = $true
} catch { $report.error=$_.Exception.ToString() }
finally {
    if ($process -and (-not $KeepRunning -or -not $report.success)) {
        # Only terminate this test's process tree, never the normal installation.
        & taskkill.exe /PID $process.Id /T /F 2>$null | Out-Null
    }
    $report | ConvertTo-Json -Depth 5 | Set-Content -Encoding UTF8 (Join-Path $stateDir 'result.json')
}
if (-not $report.success) { exit 1 }
