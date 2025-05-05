param(
    [Parameter(Mandatory=$true)]
    [int]$DaysAgo,
    
    [Parameter(Mandatory=$true)]
    [string]$OutputFile
)

$computerName = $env:COMPUTERNAME


function Get-TerminalServicesEvents {
    param(
        [datetime]$StartTime
    )
    
    Write-Host "Collecting Terminal Services events since $($StartTime.ToString('yyyy-MM-dd HH:mm:ss'))"
    
    # Define event IDs to capture
    $logonEvents = @(21, 22, 25, 1149)          # Logon/reconnect events
    $logoffEvents = @(23, 24, 1150, 1151)       # Logoff/disconnect events
    $rdpAuthEvents = @(4624, 4625, 4634, 4647)  # Authentication events
    $rdpTSGEvents = @(300, 301, 302, 303)       # RDP Gateway events
    
    $events = @()
    
    # Search Microsoft-Windows-TerminalServices-LocalSessionManager logs
    try {
        $localSessionEvents = Get-WinEvent -FilterHashtable @{
            LogName = 'Microsoft-Windows-TerminalServices-LocalSessionManager/Operational'
            StartTime = $StartTime
            ID = $logonEvents + $logoffEvents
        } -ErrorAction SilentlyContinue
        
        $events += $localSessionEvents
    }
    catch {
        Write-Host "No LocalSessionManager events found or error accessing logs"
    }
    
    # Search Microsoft-Windows-TerminalServices-RemoteConnectionManager logs
    try {
        $remoteConnectionEvents = Get-WinEvent -FilterHashtable @{
            LogName = 'Microsoft-Windows-TerminalServices-RemoteConnectionManager/Operational'
            StartTime = $StartTime
        } -ErrorAction SilentlyContinue
        
        $events += $remoteConnectionEvents
    }
    catch {
        Write-Host "No RemoteConnectionManager events found or error accessing logs"
    }
    
    # Search Security logs for RDP authentication events
    try {
        $securityEvents = Get-WinEvent -FilterHashtable @{
            LogName = 'Security'
            StartTime = $StartTime
            ID = $rdpAuthEvents
        } -ErrorAction SilentlyContinue | Where-Object {
            $_.Message -match "logon type:\s*10" -or $_.Message -match "Logon Type:\s*10"
        }
        
        $events += $securityEvents
    }
    catch {
        Write-Host "No RDP Security events found or error accessing logs"
    }
    
    # Search Microsoft-Windows-TerminalServices-Gateway logs if available
    try {
        $gatewayEvents = Get-WinEvent -FilterHashtable @{
            LogName = 'Microsoft-Windows-TerminalServices-Gateway/Operational'
            StartTime = $StartTime
            ID = $rdpTSGEvents
        } -ErrorAction SilentlyContinue
        
        $events += $gatewayEvents
    }
    catch {
        Write-Host "No Terminal Services Gateway events found or error accessing logs"
    }
    
    return $events
}

function Process-Events {
    param(
        [array]$Events
    )
    
    $rdpConnections = @()
    
    foreach ($event in $Events) {
        $eventData = @{
            PSComputerName = $computerName
            TimeCreated = $event.TimeCreated
            EventID = $event.Id
            LogName = $event.LogName
            MachineName = $event.MachineName
            Username = $null
            SourceIP = $null
            SourceHostname = $null
            SessionID = $null
            EventType = $null
            Status = $null
            AdditionalInfo = $null
        }
        
        # Process event based on log name and event ID
        switch -Wildcard ($event.LogName) {
            "*LocalSessionManager*" {
                # Extract session info from LocalSessionManager events
                switch ($event.Id) {
                    21 { # Session logon
                        $eventData.EventType = "Logon"
                        $eventData.Status = "Success"
                    }
                    22 { # Shell start
                        $eventData.EventType = "ShellStart"
                        $eventData.Status = "Success"
                    }
                    23 { # Session logoff
                        $eventData.EventType = "Logoff"
                        $eventData.Status = "Success"
                    }
                    24 { # Session disconnect
                        $eventData.EventType = "Disconnect"
                        $eventData.Status = "Success"
                    }
                    25 { # Session reconnect
                        $eventData.EventType = "Reconnect"
                        $eventData.Status = "Success"
                    }
                    1149 { # User authentication succeeded
                        $eventData.EventType = "Authentication"
                        $eventData.Status = "Success"
                    }
                    default {
                        $eventData.EventType = "Other"
                    }
                }
                
                # Extract data from XML
                $eventXML = [xml]$event.ToXml()
                $eventData.Username = ($eventXML.Event.UserData.EventXML.User) -replace '^.*\\', ''
                $eventData.SourceIP = $eventXML.Event.UserData.EventXML.Address
                $eventData.SessionID = $eventXML.Event.UserData.EventXML.SessionID
                
                # Additional context information
                if ($eventXML.Event.UserData.EventXML.SessionName) {
                    $eventData.AdditionalInfo = "Session: " + $eventXML.Event.UserData.EventXML.SessionName
                }
            }
            
            "*RemoteConnectionManager*" {
                # Extract data from Remote Connection Manager
                $eventData.EventType = "Connection"
                
                # Extract data from XML
                $eventXML = [xml]$event.ToXml()
                
                if ($event.Id -eq 1149) {
                    $eventData.Username = ($eventXML.Event.UserData.EventXML.Param1) -replace '^.*\\', ''
                    $eventData.SourceIP = $eventXML.Event.UserData.EventXML.Param3
                    $eventData.Status = "Success"
                }
                elseif ($event.Id -eq 1150 -or $event.Id -eq 1151) {
                    $eventData.Username = ($eventXML.Event.UserData.EventXML.Param1) -replace '^.*\\', ''
                    $eventData.Status = "Disconnected"
                }
            }
            
            "*Security*" {
                # Security log events need special parsing
                if ($event.Id -eq 4624 -and $event.Message -match "logon type:\s*10") {
                    $eventData.EventType = "Authentication"
                    $eventData.Status = "Success"
                    
                    # Extract username
                    if ($event.Message -match "Account Name:\s*(.*?)\s*Account Domain:") {
                        $eventData.Username = $Matches[1]
                    }
                    
                    # Extract source IP
                    if ($event.Message -match "Source Network Address:\s*(.*?)\s*") {
                        $eventData.SourceIP = $Matches[1]
                    }
                    
                    # Extract source hostname
                    if ($event.Message -match "Workstation Name:\s*(.*?)\s*") {
                        $eventData.SourceHostname = $Matches[1]
                    }
                    
                    # Extract session ID if available
                    if ($event.Message -match "Logon ID:\s*(.*?)\s*") {
                        $eventData.SessionID = $Matches[1]
                    }
                }
                elseif ($event.Id -eq 4625 -and $event.Message -match "logon type:\s*10") {
                    $eventData.EventType = "Authentication"
                    $eventData.Status = "Failure"
                    
                    # Extract username
                    if ($event.Message -match "Account Name:\s*(.*?)\s*Account Domain:") {
                        $eventData.Username = $Matches[1]
                    }
                    
                    # Extract source IP
                    if ($event.Message -match "Source Network Address:\s*(.*?)\s*") {
                        $eventData.SourceIP = $Matches[1]
                    }
                    
                    # Extract source hostname
                    if ($event.Message -match "Workstation Name:\s*(.*?)\s*") {
                        $eventData.SourceHostname = $Matches[1]
                    }
                    
                    # Extract failure reason if available
                    if ($event.Message -match "Failure Reason:\s*(.*?)\s*Status:") {
                        $eventData.AdditionalInfo = $Matches[1]
                    }
                }
                elseif (($event.Id -eq 4634 -or $event.Id -eq 4647) -and $event.Message -match "Logon Type:\s*10") {
                    $eventData.EventType = "Logoff"
                    $eventData.Status = "Success"
                    
                    # Extract username
                    if ($event.Message -match "Account Name:\s*(.*?)\s*Account Domain:") {
                        $eventData.Username = $Matches[1]
                    }
                    
                    # Extract session ID if available
                    if ($event.Message -match "Logon ID:\s*(.*?)\s*") {
                        $eventData.SessionID = $Matches[1]
                    }
                }
            }
            
            "*Gateway*" {
                # RDP Gateway events
                $eventXML = [xml]$event.ToXml()
                
                switch ($event.Id) {
                    300 { # Connection authorization request
                        $eventData.EventType = "Gateway"
                        $eventData.Status = "Authorization"
                    }
                    301 { # Connection authorization succeeded
                        $eventData.EventType = "Gateway"
                        $eventData.Status = "Success"
                    }
                    302 { # Connection authorization failed
                        $eventData.EventType = "Gateway"
                        $eventData.Status = "Failure"
                    }
                    303 { # Resource authorization completed
                        $eventData.EventType = "Gateway"
                        $eventData.Status = "ResourceAuth"
                    }
                }
                
                # Try to extract username and IP from Gateway events
                if ($eventXML.Event.UserData.EventXML.UserName) {
                    $eventData.Username = $eventXML.Event.UserData.EventXML.UserName
                }
                
                if ($eventXML.Event.UserData.EventXML.ClientIPAddress) {
                    $eventData.SourceIP = $eventXML.Event.UserData.EventXML.ClientIPAddress
                }
                
                # Additional info from Gateway events
                if ($eventXML.Event.UserData.EventXML.ConnectionID) {
                    $eventData.AdditionalInfo = "Connection ID: " + $eventXML.Event.UserData.EventXML.ConnectionID
                }
            }
        }
        
        # Add the processed event to our collection
        if ($eventData.Username -or $eventData.SourceIP -or $eventData.SessionID) {
            $rdpConnections += New-Object PSObject -Property $eventData
        }
    }
    
    return $rdpConnections
}

function Export-ToCSV {
    param(
        [array]$Data,
        [string]$FilePath
    )
    
    # Create directory if it doesn't exist
    $directory = Split-Path -Path $FilePath -Parent
    if (-not (Test-Path -Path $directory)) {
        New-Item -ItemType Directory -Path $directory -Force | Out-Null
    }
    
    # Export to CSV
    try {
        $Data | Select-Object TimeCreated, EventID, LogName, MachineName, Username, SourceIP, SourceHostname, SessionID, EventType, Status, AdditionalInfo |
        Sort-Object TimeCreated |
        Export-Csv -Path $FilePath -NoTypeInformation -Encoding UTF8
    }
    catch {
        Write-Host "Error exporting data to CSV: $_"
        throw $_
    }
}

try {
    $startTime = (Get-Date).AddDays(-$DaysAgo)
    $events = Get-TerminalServicesEvents -StartTime $startTime
    if ($events.Count -eq 0) {
        Write-Host "No Terminal Services events found for the specified time period"
        exit 0
    }
    $rdpConnections = Process-Events -Events $events
    Export-ToCSV -Data $rdpConnections -FilePath $OutputFile
}
catch {
    Write-Host "Script execution failed: $_"
    exit 1
}