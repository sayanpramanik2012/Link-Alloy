$ErrorActionPreference = 'Stop'

$expected = @{
    'amd64' = 'e5da8447dc2c320edc0fc52fa01885c103de8c118481f683643cacc3220dafce'
    'arm64' = 'f7ba89005544be9d85231a9e0d5f23b2d15b3311667e2dad0debd344918a3f80'
}

foreach ($architecture in $expected.Keys) {
    $path = Join-Path $PSScriptRoot "..\third-party\wintun\$architecture\wintun.dll"
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing Wintun runtime for $architecture."
    }
    $hash = (Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($hash -ne $expected[$architecture]) {
        throw "Wintun runtime hash mismatch for $architecture."
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $path
    if ($signature.Status -ne 'Valid' -or $signature.SignerCertificate.Subject -notlike '*WireGuard LLC*') {
        throw "Wintun runtime signature is not valid for $architecture."
    }
}

Write-Output 'Official Wintun runtimes passed SHA-256 and Authenticode verification.'
