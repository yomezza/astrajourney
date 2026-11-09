#Requires -Version 7.4
# Explicit local preparation only; does not change NATS or any Windows Service.
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$ServiceAccount
)
$ErrorActionPreference = 'Stop'
if (-not $IsWindows) { throw 'Windows ACL script; run on the target Windows host.' }
if (Test-Path -LiteralPath $Path) { throw 'Refusing to overwrite an existing password file.' }
$directory = Split-Path -Parent ([IO.Path]::GetFullPath($Path))
if (-not (Test-Path -LiteralPath $directory -PathType Container)) { throw 'Create a private secret directory first; see the runbook.' }
$secret = Read-Host 'Enter the new NATS password (same value used for its bcrypt hash)' -AsSecureString
$pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secret)
try {
    $plain = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    if ([string]::IsNullOrWhiteSpace($plain) -or [Text.Encoding]::UTF8.GetByteCount($plain) -gt 72) { throw 'Use a nonempty password of at most 72 UTF-8 bytes (bcrypt limit).' }
    $stream = [IO.File]::Open($Path,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
    try {
        $bytes = [Text.Encoding]::UTF8.GetBytes($plain)
        $stream.Write($bytes,0,$bytes.Length)
        $stream.Flush($true)
        [Array]::Clear($bytes,0,$bytes.Length)
    } finally { $stream.Dispose() }
    & icacls.exe $Path '/inheritance:r' '/grant:r' "${ServiceAccount}:(R)" '*S-1-5-18:(F)' '*S-1-5-32-544:(F)' | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Password file ACL setup failed; keep the enclosing directory private.' }
} finally {
    $plain = $null
    [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    $secret.Dispose()
}
Write-Output 'Password file created; password not printed.'
