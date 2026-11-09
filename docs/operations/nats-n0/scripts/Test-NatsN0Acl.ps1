#Requires -Version 7.4
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$SecretDirectory,
    [Parameter(Mandatory)][string]$CAFile,
    [Parameter(Mandatory)][string]$WrongCAFile,
    [int]$Port = 14222,
    [string]$ServerHost = '127.0.0.1'
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
# Publishes test fixtures into real signal subjects: isolated broker only.
$address = [Net.IPAddress]::Parse($ServerHost)
if (-not [Net.IPAddress]::IsLoopback($address) -or $Port -eq 4222 -or $Port -lt 1024) {
    throw 'Only a loopback test broker on a dedicated non-production port is allowed.'
}
Import-Module (Join-Path $PSScriptRoot NatsWire.psm1) -Force -DisableNameChecking
$matrix = Get-Content (Join-Path $PSScriptRoot ../acl-matrix.json) -Raw | ConvertFrom-Json
$script:passed = 0
function Check([string]$Name, [string]$Expected, [scriptblock]$Action) {
    $actual = 'OK'
    try { & $Action } catch {
        $actual = $_.Exception.Message
        Write-Verbose ($_.Exception.GetType().Name + ': ' + $_.ScriptStackTrace)
    }
    if ($actual -ne $Expected) { throw "FAILED: $Name (expected $Expected; result redacted)" }
    $script:passed++
    Write-Output "PASS $Name"
}
function Connection([string]$User) {
    Open-N0Connection -ServerHost $ServerHost -Port $Port -CAFile $CAFile -Username $User -PasswordFile (Join-Path $SecretDirectory "$User.password") -RequireIsolated
}
Check 'anonymous rejected' 'NATS_AUTHORIZATION' {
    $c = $null
    try { $c = Open-N0Connection -ServerHost $ServerHost -Port $Port -CAFile $CAFile -Anonymous -RequireIsolated }
    finally { Close-N0Connection $c }
}
Check 'untrusted CA rejected' 'NATS_TLS_VALIDATION' {
    $c = $null
    try { $c = Open-N0Connection -ServerHost $ServerHost -Port $Port -CAFile $WrongCAFile -Anonymous -RequireIsolated }
    finally { Close-N0Connection $c }
}
Check 'wrong SAN rejected' 'NATS_TLS_VALIDATION' {
    $c = $null
    try { $c = Open-N0Connection -ServerHost $ServerHost -Port $Port -CAFile $CAFile -ServerName wrong.invalid -Anonymous -RequireIsolated }
    finally { Close-N0Connection $c }
}
foreach ($user in 'cosmos', 'planet', 'chat') {
    Check "$user authenticated" 'OK' {
        $c = $null; try { $c = Connection $user } finally { Close-N0Connection $c }
    }
    foreach ($subject in $matrix.$user.publish) {
        Check "$user publishes $subject" 'OK' {
            $c = $null; try { $c = Connection $user; Publish-N0 $c $subject } finally { Close-N0Connection $c }
        }
    }
    foreach ($subject in $matrix.$user.subscribe) {
        Check "$user subscribes $subject" 'OK' {
            $c = $null; try { $c = Connection $user; Subscribe-N0 $c $subject } finally { Close-N0Connection $c }
        }
    }
    $forbidden = @('$JS.API.INFO', '$SYS.REQ.SERVER.PING', 'cosmos.v1.event.economic.account.changed', 'unrelated.subject')
    foreach ($other in 'cosmos', 'planet', 'chat') {
        if ($other -ne $user) { $forbidden += $matrix.$other.publish[0] }
    }
    foreach ($subject in $forbidden) {
        Check "$user cannot publish $subject" 'NATS_PERMISSION' {
            $c = $null; try { $c = Connection $user; Publish-N0 $c $subject } finally { Close-N0Connection $c }
        }
    }
    foreach ($subject in @('>', 'cosmos.v1.signal.>', '$JS.API.>', '$SYS.>', '_INBOX.>', 'unrelated.subject')) {
        Check "$user cannot subscribe $subject" 'NATS_PERMISSION' {
            $c = $null; try { $c = Connection $user; Subscribe-N0 $c $subject } finally { Close-N0Connection $c }
        }
    }
}
# Actual delivery, not just a PONG on publisher/subscriber connections.
Check 'Planet signal reaches Chat' 'OK' {
    $publisher = $subscriber = $null
    try {
        $subscriber = Connection chat
        $publisher = Connection planet
        Subscribe-N0 $subscriber 'cosmos.v1.signal.planet.sector.changed'
        Publish-N0 $publisher 'cosmos.v1.signal.planet.sector.changed'
        Sync-N0Connection $subscriber
        if ($subscriber.Messages.Count -ne 1) { throw 'NATS_DELIVERY_MISSING' }
    } finally { Close-N0Connection $publisher; Close-N0Connection $subscriber }
}
Write-Output "ACL/TLS checks passed: $script:passed"
