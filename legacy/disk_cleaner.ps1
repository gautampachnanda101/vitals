# Enterprise Universal Disk Cleanup Tool for Windows

Write-Host "[+] Starting Windows Disk Cleanup..." -ForegroundColor Green

# 1. Target Safe System Folders
$TempFolders = @(
    "$env:TEMP",
    "$env:SystemRoot\Temp",
    "$env:SystemRoot\Prefetch",
    "$env:LOCALAPPDATA\Temp"
)

foreach ($folder in $TempFolders) {
    if (Test-Path $folder) {
        Write-Host "Cleaning $folder..." -ForegroundColor Cyan
        Get-ChildItem -Path $folder -Recurse -Force -ErrorAction SilentlyContinue | 
            Remove-Item -Force -Recurse -ErrorAction SilentlyContinue
    }
}

# 2. Empty Recycle Bin
Write-Host "Emptying Recycle Bin..." -ForegroundColor Cyan
Clear-RecycleBin -Force -ErrorAction SilentlyContinue

# 3. Trigger Native Windows Disk Cleanup (Cleanmgr) silently
Write-Host "Triggering Windows Component Cleanup..." -ForegroundColor Cyan
dism.exe /Online /Cleanup-Image /StartComponentCleanup /ResetBase /Quiet

# 4. Developer / Runtime Caches
if (Test-Path "$env:USERPROFILE\.nuget\packages") {
    Write-Host "Cleaning NuGet Cache..." -ForegroundColor Cyan
    dotnet nuget locals all --clear | Out-Null
}

Write-Host "[+] Windows Cleanup complete!" -ForegroundColor Green