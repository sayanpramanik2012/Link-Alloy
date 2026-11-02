param(
    [Parameter(Mandatory)]
    [string]$InputPath,
    [string]$CertificateThumbprint = $env:LINKALLOY_SIGNING_THUMBPRINT,
    [uri]$TimestampServer = 'http://timestamp.digicert.com'
)

$ErrorActionPreference = 'Stop'
if ($CertificateThumbprint -notmatch '^[A-Fa-f0-9]{40}$') {
    throw 'A code-signing certificate thumbprint must be configured before signing.'
}
if ($TimestampServer.Scheme -notin @('http', 'https')) { throw 'The timestamp server must use HTTP or HTTPS.' }
$path = (Resolve-Path -LiteralPath $InputPath -ErrorAction Stop).Path
if (-not (Test-Path -LiteralPath $path -PathType Leaf) -or [IO.Path]::GetExtension($path) -notin @('.exe', '.dll')) {
    throw 'Only an existing Windows executable or DLL can be signed with this script.'
}
$certificate = Get-Item -LiteralPath "Cert:\CurrentUser\My\$CertificateThumbprint" -ErrorAction Stop
$usageExtension = $certificate.Extensions | Where-Object { $_.Oid.Value -eq '2.5.29.37' } | Select-Object -First 1
if (-not $certificate.HasPrivateKey -or '1.3.6.1.5.5.7.3.3' -notin @($usageExtension.EnhancedKeyUsages.Value)) {
    throw 'The selected certificate needs a private key and the Code Signing purpose.'
}
if ($certificate.NotAfter -le (Get-Date) -or $certificate.NotBefore -gt (Get-Date)) {
    throw 'The configured code-signing certificate is outside its validity period.'
}
$signTool = Get-Command signtool.exe -ErrorAction SilentlyContinue | Select-Object -First 1 -ExpandProperty Source
if (-not $signTool) {
    $sdkRoot = Join-Path ${env:ProgramFiles(x86)} 'Windows Kits\10\bin'
    $signTool = Get-ChildItem -LiteralPath $sdkRoot -Directory -ErrorAction SilentlyContinue |
        Sort-Object Name -Descending |
        ForEach-Object { Join-Path $_.FullName 'x64\signtool.exe' } |
        Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
        Select-Object -First 1
}
if (-not $signTool) { throw 'Windows SDK SignTool is required for release signing.' }

& $signTool sign /sha1 $CertificateThumbprint /s My /fd SHA256 /tr $TimestampServer.AbsoluteUri /td SHA256 $path
if ($LASTEXITCODE -ne 0) { throw 'Windows code signing failed.' }
& (Join-Path $PSScriptRoot 'verify-windows-signatures.ps1') -InputPath $path -ExpectedThumbprint $CertificateThumbprint
