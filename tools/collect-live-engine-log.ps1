$ErrorActionPreference = 'Stop'
# Read-only collection from the elevated engine. Output is local diagnostic
# data only; no journal, credentials, routes, settings, or packet payloads.
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$reportDir = Join-Path $repoRoot 'bin\live-speedcheck'
New-Item -ItemType Directory -Force -Path $reportDir | Out-Null
$source = Join-Path $env:ProgramData 'Link Alloy\logs\engine.jsonl'
try {
    Get-Content -LiteralPath $source -Tail 5000 | Set-Content -LiteralPath (Join-Path $reportDir 'engine-events.jsonl') -Encoding utf8
} catch {
    $_.Exception.Message | Set-Content -LiteralPath (Join-Path $reportDir 'collection-error.txt') -Encoding utf8
    exit 1
}
