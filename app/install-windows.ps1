# Installs the F1 Party build on this PC for the current user: the app in %LOCALAPPDATA%\Programs\F1 Party and
# a Start menu shortcut, so it can be found in Windows search and pinned to the taskbar. Run it again after
# a new build to update. Remove with -Uninstall.
param([switch]$Uninstall)
$ErrorActionPreference = 'Stop'
$dir = Join-Path $env:LOCALAPPDATA 'Programs\F1 Party'
$exe = Join-Path $dir 'F1Party.exe'
$lnk = Join-Path ([Environment]::GetFolderPath('Programs')) 'F1 Party.lnk'
$proto = 'HKCU:\Software\Classes\f1party'   # invite links: f1party://join/CODE

Get-Process F1Party -ErrorAction SilentlyContinue | Stop-Process -Force
if ($Uninstall) {
    Remove-Item $lnk -Force -ErrorAction SilentlyContinue
    Remove-Item $proto -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item $dir -Recurse -Force -ErrorAction SilentlyContinue
    Write-Output 'F1 Party removed.'
    return
}
$build = Join-Path $PSScriptRoot 'build\bin\F1Party.exe'
if (-not (Test-Path $build)) { throw "No build found at $build (run: wails build)" }
New-Item -ItemType Directory -Force $dir | Out-Null
Start-Sleep -Milliseconds 500
Copy-Item $build $exe -Force

$shell = New-Object -ComObject WScript.Shell
$s = $shell.CreateShortcut($lnk)
$s.TargetPath = $exe
$s.WorkingDirectory = $dir
$s.IconLocation = "$exe,0"
$s.Description = 'Watch MultiViewer in sync with friends'
$s.Save()
# f1party:// links (from Discord invites) open this app, which then joins that party.
New-Item -Path "$proto\shell\open\command" -Force | Out-Null
Set-ItemProperty -Path $proto -Name '(default)' -Value 'URL:F1 Party invite link'
Set-ItemProperty -Path $proto -Name 'URL Protocol' -Value ''
New-Item -Path "$proto\DefaultIcon" -Force | Out-Null
Set-ItemProperty -Path "$proto\DefaultIcon" -Name '(default)' -Value "`"$exe`",0"
Set-ItemProperty -Path "$proto\shell\open\command" -Name '(default)' -Value "`"$exe`" `"%1`""
Write-Output "Installed: $exe"
Write-Output "Start menu: $lnk"
