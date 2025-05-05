param (
    [Parameter(Mandatory=$false)]
    [string]$OutputFile = "ConsoleHostHistory.csv"
)

$computerName = $env:COMPUTERNAME

try {
    $UserProfiles = Get-ChildItem -Path "C:\Users" -Directory    

    $Results = @()
        
    foreach ($Profile in $UserProfiles) {
        $HistoryPath = Join-Path -Path $Profile.FullName -ChildPath "AppData\Roaming\Microsoft\Windows\PowerShell\PSReadLine\ConsoleHost_history.txt"
            
        if (Test-Path -Path $HistoryPath) {
            $FileInfo = Get-ItemProperty -Path $HistoryPath
            $HistoryContent = Get-Content -Path $HistoryPath -ErrorAction SilentlyContinue
            foreach ($HistoryItem in $HistoryContent) {
                if (-not [string]::IsNullOrWhiteSpace($HistoryItem)) {
                    $Results += [PSCustomObject]@{
                        PSComputerName = $computerName
                        UserName = $Profile.Name
                        Command = $HistoryItem
                        CreationTime = $FileInfo.CreationTime
                        LastAccessTime = $FileInfo.LastAccessTime
                        LastWriteTime = $FileInfo.LastWriteTime
                    }
                }
            }
        }
    }
    if ($Results.Count -eq 0) {
        Write-Host "No console host history found."
    }
    $Results | Export-Csv -Path $OutputFile -NoTypeInformation
    
} catch {
    Write-Error "An error occurred: $_"
}