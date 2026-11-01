param(
    [Parameter(Mandatory)]
    [ValidatePattern('^[^/]+/[^/]+$')]
    [string]$Repository
)

$ErrorActionPreference = 'Stop'

# Retain the two newest published release records; leave all Git tags untouched.
$rows = @(
    gh api --paginate "repos/$Repository/releases?per_page=100" --jq '.[] | select(.draft == false) | [.id, .published_at] | @tsv'
)
if ($LASTEXITCODE -ne 0) { throw "Could not list releases for $Repository." }

$published = foreach ($row in $rows) {
    if ([string]::IsNullOrWhiteSpace($row)) { continue }
    $fields = $row -split "`t", 2
    [pscustomobject]@{
        Id = $fields[0]
        PublishedAt = [DateTimeOffset]::Parse($fields[1]).UtcDateTime
    }
}

$older = @($published | Sort-Object PublishedAt -Descending | Select-Object -Skip 2)
foreach ($release in $older) {
    gh api --method DELETE "repos/$Repository/releases/$($release.Id)"
    if ($LASTEXITCODE -ne 0) { throw "Could not remove an older release from $Repository." }
}
