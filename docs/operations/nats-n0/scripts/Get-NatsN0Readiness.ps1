#Requires -Version 7.4
# Read-only: no restart/reload, no publish, no stream or firewall mutation.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$NatsServerPath,
    [Parameter(Mandatory)][string]$ConfigPath,
    [string]$ServerAddress = '192.168.111.44',
    [uri]$MonitorUri = 'http://127.0.0.1:8222'
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'Run this inventory on the Windows host running cosmoNATS.' }
if (-not (Test-Path -LiteralPath $ConfigPath -PathType Leaf)) { throw 'Main config not found.' }
$version = & $NatsServerPath --version
if ($LASTEXITCODE -ne 0) { throw 'Cannot read nats-server version.' }
# Parser only. Capture diagnostics: never emit a config line containing a secret.
$validation = & $NatsServerPath -t -c $ConfigPath 2>&1
$valid = $LASTEXITCODE -eq 0
$authPath = Join-Path (Split-Path -Parent $ConfigPath) auth.config
$authExists = Test-Path -LiteralPath $authPath -PathType Leaf
$placeholder = $false
if ($authExists) { $placeholder = [IO.File]::ReadAllText($authPath).Contains('<BCRYPT_HASH_') }
[pscustomobject]@{ Version = "$version"; ConfigSyntaxValid = $valid; AuthConfigExists = $authExists; AuthContainsPlaceholders = $placeholder }

Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
    Where-Object LocalPort -In 4222,8222 |
    Select-Object LocalAddress,LocalPort,OwningProcess
Test-NetConnection -ComputerName $ServerAddress -Port 4222 -InformationLevel Detailed |
    Select-Object ComputerName,RemotePort,TcpTestSucceeded,SourceAddress

# INFO is readable before TLS; no CONNECT and no credentials are sent.
$tcp = [Net.Sockets.TcpClient]::new()
try {
    if (-not $tcp.ConnectAsync($ServerAddress,4222).Wait(3000)) { throw 'INFO_CONNECT_TIMEOUT' }
    $stream = $tcp.GetStream(); $stream.ReadTimeout = 3000
    $reader = [IO.StreamReader]::new($stream)
    $line = $reader.ReadLine()
    if (-not $line.StartsWith('INFO ')) { throw 'INFO_MISSING' }
    $info = $line.Substring(5) | ConvertFrom-Json
    $info | Select-Object server_name,version,auth_required,tls_required,jetstream
} finally { $tcp.Dispose() }

foreach ($endpoint in 'varz','jsz') {
    try {
        $v = Invoke-RestMethod ([uri]::new($MonitorUri, "/$endpoint")) -TimeoutSec 4
        if ($endpoint -eq 'varz') {
            $v | Select-Object server_name,version,auth_required,tls_required,connections,slow_consumers
        } else {
            $v | Select-Object disabled,config,memory,storage,accounts,streams,consumers,messages
        }
    } catch { Write-Warning "Monitoring /$endpoint unavailable at the chosen URI; no remote reachability claim." }
}

$processes = @(Get-CimInstance Win32_Process -Filter "Name='nats-server.exe'")
$processes | Select-Object ProcessId,ParentProcessId,ExecutablePath
$ids = @($processes.ProcessId) + @($processes.ParentProcessId)
Get-CimInstance Win32_Service |
    Where-Object { $_.ProcessId -ne 0 -and ($_.ProcessId -in $ids -or $_.Name -match 'nats') } |
    Select-Object Name,State,StartMode,StartName,ProcessId
Get-ScheduledTask -ErrorAction SilentlyContinue |
    Where-Object { ($_.Actions.Execute -join ' ') -match 'nats' } |
    Select-Object TaskName,TaskPath,State

Get-NetFirewallProfile | Select-Object Name,Enabled,DefaultInboundAction,DefaultOutboundAction
function Includes-NatsPort($Ports) {
    foreach ($p in $Ports) {
        if ($p -eq 'Any' -or $p -in '4222','8222') { return $true }
        if ($p -match '^(\d+)-(\d+)$' -and ((4222 -ge [int]$Matches[1] -and 4222 -le [int]$Matches[2]) -or (8222 -ge [int]$Matches[1] -and 8222 -le [int]$Matches[2]))) { return $true }
    }
    return $false
}
foreach ($rule in Get-NetFirewallRule -PolicyStore ActiveStore -Enabled True -Direction Inbound) {
    $port = $rule | Get-NetFirewallPortFilter
    if ($port.Protocol -in 'TCP','6','Any' -and (Includes-NatsPort $port.LocalPort)) {
        $address = $rule | Get-NetFirewallAddressFilter
        $application = $rule | Get-NetFirewallApplicationFilter
        [pscustomobject]@{ Rule = $rule.Name; Action = $rule.Action; Profile = $rule.Profile; LocalPort = $port.LocalPort; RemoteAddress = $address.RemoteAddress; Program = $application.Program }
    }
}
# Presence of an Allow rule alone does not prove effective remote reachability.
