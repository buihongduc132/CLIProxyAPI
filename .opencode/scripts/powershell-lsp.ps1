param()
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$module = Get-Module -ListAvailable -Name PowerShellEditorServices | Select-Object -First 1
if (-not $module) {
    Write-Error "PowerShellEditorServices module missing. Install with: Install-Module -Name PowerShellEditorServices -Force"
    exit 1
}

Import-Module $module.Name -ErrorAction Stop
$stat = Start-EditorServices -HostName "oqo-pwsh" -HostProfilePath $PROFILE -LogPath "$env:TEMP/oqo-pwsh.log" -LogLevel Normal -BundledModulesPath $module.ModuleBase -Stdio
