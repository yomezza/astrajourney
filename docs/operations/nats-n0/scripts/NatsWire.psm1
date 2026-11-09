# Standalone N0 protocol probe, not a game client. PowerShell 7.4+ / .NET 8+.
Set-StrictMode -Version Latest

function Send-N0Bytes($Connection, [byte[]]$Bytes) {
    $Connection.Stream.Write($Bytes, 0, $Bytes.Length)
    $Connection.Stream.Flush()
}
function Send-N0Line($Connection, [string]$Line) {
    Send-N0Bytes $Connection ([Text.Encoding]::UTF8.GetBytes($Line + "`r`n"))
}
function Read-N0Line($Connection) {
    $bytes = [Collections.Generic.List[byte]]::new()
    while ($bytes.Count -lt 16384) {
        $b = $Connection.Stream.ReadByte()
        if ($b -lt 0) { throw 'NATS_EOF' }
        if ($b -eq 10) { return [Text.Encoding]::UTF8.GetString($bytes.ToArray()).TrimEnd("`r") }
        $bytes.Add([byte]$b)
    }
    throw 'NATS_FRAME_TOO_LARGE'
}
function Sync-N0Connection($Connection) {
    Send-N0Line $Connection 'PING'
    for ($i = 0; $i -lt 100; $i++) {
        $line = Read-N0Line $Connection
        if ($line -eq 'PONG') { return }
        if ($line -eq 'PING') { Send-N0Line $Connection 'PONG'; continue }
        if ($line.StartsWith('-ERR')) {
            if ($line -match 'Permissions Violation') { throw 'NATS_PERMISSION' }
            if ($line -match 'Authorization|Authentication') { throw 'NATS_AUTHORIZATION' }
            throw 'NATS_PROTOCOL_ERROR'
        }
        if ($line.StartsWith('MSG ')) {
            $parts = $line.Split(' ')
            $size = [int]$parts[-1]
            if ($size -lt 0 -or $size -gt 2048) { throw 'NATS_PAYLOAD_TOO_LARGE' }
            $body = [byte[]]::new($size + 2)
            $offset = 0
            while ($offset -lt $body.Length) {
                $read = $Connection.Stream.Read($body, $offset, $body.Length - $offset)
                if ($read -le 0) { throw 'NATS_EOF' }
                $offset += $read
            }
            $Connection.Messages.Add([pscustomobject]@{
                Subject = $parts[1]; Body = [Text.Encoding]::UTF8.GetString($body, 0, $size)
            })
        }
    }
    throw 'NATS_SYNC_LIMIT'
}
function Close-N0Connection($Connection) {
    if ($null -ne $Connection) { $Connection.Stream.Dispose(); $Connection.Client.Dispose() }
}
function Open-N0Connection {
    param(
        [string]$ServerHost, [int]$Port, [string]$CAFile,
        [string]$Username = '', [string]$PasswordFile = '',
        [string]$ServerName = '', [switch]$Anonymous, [switch]$RequireIsolated
    )
    $client = [Net.Sockets.TcpClient]::new()
    try {
        $task = $client.ConnectAsync($ServerHost, $Port)
        if (-not $task.Wait(3000)) { throw 'NATS_CONNECT_TIMEOUT' }
        [void]$task.GetAwaiter().GetResult()
        $connection = [pscustomobject]@{
            Client = $client; Stream = $client.GetStream()
            Messages = [Collections.Generic.List[object]]::new()
        }
        $connection.Stream.ReadTimeout = 3000
        $info = Read-N0Line $connection
        if (-not $info.StartsWith('INFO ')) { throw 'NATS_INFO_REQUIRED' }
        $details = $info.Substring(5) | ConvertFrom-Json
        if ($RequireIsolated -and $details.server_name -ne 'N0-isolated-only') { throw 'NATS_NOT_ISOLATED' }
        if (-not $details.tls_required) { throw 'NATS_TLS_REQUIRED' }
        $ssl = [Net.Security.SslStream]::new($connection.Stream, $false)
        $chain = [Security.Cryptography.X509Certificates.X509ChainPolicy]::new()
        $chain.TrustMode = [Security.Cryptography.X509Certificates.X509ChainTrustMode]::CustomRootTrust
        $pem = [IO.File]::ReadAllText($CAFile) -replace '-----BEGIN CERTIFICATE-----|-----END CERTIFICATE-----|\s',''
        $ca = [Security.Cryptography.X509Certificates.X509Certificate2]::new([Convert]::FromBase64String($pem))
        [void]$chain.CustomTrustStore.Add($ca)
        # Private test CA has no CRL. Trust-chain, expiry and SAN checks remain on.
        $chain.RevocationMode = [Security.Cryptography.X509Certificates.X509RevocationMode]::NoCheck
        $options = [Net.Security.SslClientAuthenticationOptions]::new()
        $options.TargetHost = $(if ($ServerName) { $ServerName } else { $ServerHost })
        $options.CertificateChainPolicy = $chain
        try {
            $handshake = $ssl.AuthenticateAsClientAsync($options)
            if (-not $handshake.Wait(5000)) { throw 'NATS_TLS_TIMEOUT' }
            [void]$handshake.GetAwaiter().GetResult()
        } catch { $ssl.Dispose(); throw 'NATS_TLS_VALIDATION' }
        finally { $ca.Dispose() }
        $connection.Stream = $ssl
        $connection.Stream.ReadTimeout = 3000
        $payload = @{ verbose = $false; pedantic = $true; name = 'n0-isolated-probe'; tls_required = $true }
        if (-not $Anonymous) {
            $payload.user = $Username
            $payload.pass = [IO.File]::ReadAllText($PasswordFile).TrimEnd("`r", "`n")
        }
        Send-N0Line $connection ('CONNECT ' + ($payload | ConvertTo-Json -Compress))
        $payload.Clear()
        Sync-N0Connection $connection
        return $connection
    } catch {
        $client.Dispose()
        throw
    }
}
function Publish-N0($Connection, [string]$Subject, [string]$Body = 'n0-isolated-fixture') {
    $bytes = [Text.Encoding]::UTF8.GetBytes($Body)
    Send-N0Line $Connection ('PUB ' + $Subject + ' ' + $bytes.Length)
    Send-N0Bytes $Connection $bytes
    Send-N0Line $Connection ''
    Sync-N0Connection $Connection
}
function Subscribe-N0($Connection, [string]$Subject) {
    Send-N0Line $Connection ('SUB ' + $Subject + ' 1')
    Sync-N0Connection $Connection
}
Export-ModuleMember -Function Open-N0Connection, Close-N0Connection, Publish-N0, Subscribe-N0, Sync-N0Connection
