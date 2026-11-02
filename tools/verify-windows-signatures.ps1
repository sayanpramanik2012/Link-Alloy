param(
    [Parameter(Mandatory)]
    [string[]]$InputPath,
    [string]$ExpectedThumbprint = $env:LINKALLOY_SIGNING_THUMBPRINT
)

$ErrorActionPreference = 'Stop'
if (-not [string]::IsNullOrWhiteSpace($ExpectedThumbprint) -and $ExpectedThumbprint -notmatch '^[A-Fa-f0-9]{40}$') {
    throw 'The expected signing certificate thumbprint is invalid.'
}

foreach ($item in $InputPath) {
    $path = (Resolve-Path -LiteralPath $item -ErrorAction Stop).Path
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) { throw 'A signing verification input is not a file.' }
    $signature = Get-AuthenticodeSignature -LiteralPath $path
    if ($signature.Status -ne 'Valid' -or $null -eq $signature.SignerCertificate) {
        throw "Release signature is not trusted and valid: $([IO.Path]::GetFileName($path)) ($($signature.Status))."
    }
    if ($ExpectedThumbprint -and $signature.SignerCertificate.Thumbprint -ne $ExpectedThumbprint) {
        throw "Release signer does not match the configured certificate: $([IO.Path]::GetFileName($path))."
    }
    if ($null -eq $signature.TimeStamperCertificate) {
        throw "Release signature has no trusted timestamp: $([IO.Path]::GetFileName($path))."
    }
    Write-Output "Verified release signature: $([IO.Path]::GetFileName($path))."
}
