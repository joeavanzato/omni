param (
    [Parameter(Mandatory=$false)]
    [int]$DaysBack = 7,
    
    [Parameter(Mandatory=$false)]
    [string]$OutputFile = "LogonEvents.csv",

    [Parameter(Mandatory=$false)]
    [int[]]$LogonTypes = @(2, 3, 4, 5, 7, 8, 9, 10, 11)
)

$StartDate = (Get-Date).AddDays(-$DaysBack)
$computerName = $env:COMPUTERNAME

try {
    $Events = Get-WinEvent -FilterHashtable @{
        LogName = 'Security'
        Id = 4624
        StartTime = $StartDate
    } -ErrorAction Stop
    
    
    $LogonEvents = $Events | ForEach-Object {
        $EventXML = [xml]$_.ToXml()
        $EventData = $EventXML.Event.EventData.Data

        $LogonType = ($EventData | Where-Object { $_.Name -eq 'LogonType' }).'#text'
        $LogonTypeDesc = switch ($LogonType) {
            2 { "Interactive" }
            3 { "Network" }
            4 { "Batch" }
            5 { "Service" }
            7 { "Unlock" }
            8 { "NetworkCleartext" }
            9 { "NewCredentials" }
            10 { "RemoteInteractive" }
            11 { "CachedInteractive" }
            default { "Other ($LogonType)" }
        }

        if ($LogonTypes -and $LogonType -notin $LogonTypes) {
            return
        }
        
        [PSCustomObject]@{
            PSComputerName = $computerName
            TimeCreated = $_.TimeCreated
            UserName = ($EventData | Where-Object { $_.Name -eq 'TargetUserName' }).'#text'
            Domain = ($EventData | Where-Object { $_.Name -eq 'TargetDomainName' }).'#text'
            LogonTypeDescription = $LogonTypeDesc
            LogonType = $LogonType
            WorkstationName = ($EventData | Where-Object { $_.Name -eq 'WorkstationName' }).'#text'
            SourceIP = ($EventData | Where-Object { $_.Name -eq 'IpAddress' }).'#text'
            ProcessName = ($EventData | Where-Object { $_.Name -eq 'ProcessName' }).'#text'
            LogonID = ($EventData | Where-Object { $_.Name -eq 'TargetLogonId' }).'#text'
            LogonProcessName = ($EventData | Where-Object { $_.Name -eq 'LogonProcessName' }).'#text'
            AuthenticationPackage = ($EventData | Where-Object { $_.Name -eq 'AuthenticationPackageName' }).'#text'
            Status = "Success"
        }
    }
    if ($LogonEvents.Count -eq 0) {
        Write-Warning "No logon events found for the specified criteria."
        return
    }
    $LogonEvents | Export-Csv -Path $OutputFile -NoTypeInformation
    
} catch {
    Write-Error "An error occurred: $_"
    if ($_.Exception.Message -like "*No events were found*") {
        Write-Warning "No logon events found in the specified time period. Check if you have sufficient permissions to access the Security log."
    }
}