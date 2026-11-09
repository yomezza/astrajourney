#Requires -Version 7.4
# Read-only authenticated PING after TLS rollout; no publish/subscribe/JS calls.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][ValidateSet('cosmos','planet','chat')][string]$Username,
    [Parameter(Mandatory)][string]$PasswordFile,
    [Parameter(Mandatory)][string]$CAFile,
    [string]$ServerHost = '192.168.111.44',
    [int]$Port = 4222
)
$ErrorActionPreference = 'Stop'
Import-Module (Join-Path $PSScriptRoot NatsWire.psm1) -Force -DisableNameChecking
$connection = $null
try {
    $connection = Open-N0Connection -ServerHost $ServerHost -Port $Port -CAFile $CAFile -Username $Username -PasswordFile $PasswordFile
    Write-Output "TLS and authenticated PING accepted for $Username; no game signal published."
} catch {
    # Never render third-party exception messages containing input data.
    throw 'NATS login check failed. Check address, certificate SAN/CA, credentials and broker logs locally.'
} finally { Close-N0Connection $connection }
