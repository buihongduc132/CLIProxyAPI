param(
    [Parameter(Mandatory = $true)]
    [string]$File
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

if (-not (Test-Path -LiteralPath $File)) {
    Write-Error "Target file '$File' not found"
    exit 1
}

$resolved = (Resolve-Path -LiteralPath $File).Path
$analyzer = Get-Module -ListAvailable -Name PSScriptAnalyzer | Select-Object -First 1
if (-not $analyzer) {
    Write-Error "PSScriptAnalyzer module missing. Install with: Install-Module -Name PSScriptAnalyzer"
    exit 1
}

Import-Module $analyzer.Name -ErrorAction Stop
$original = Get-Content -LiteralPath $resolved -Raw
$formatted = Invoke-Formatter -ScriptDefinition $original -Settings (Get-ScriptAnalyzerRule)

if ($null -eq $formatted) {
    # Fallback to original if formatter returned nothing
    $formatted = $original
}

Set-Content -LiteralPath $resolved -Value $formatted -NoNewline
Add-Content -LiteralPath $resolved -Value ""
