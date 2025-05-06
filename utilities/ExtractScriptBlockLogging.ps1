param(
    [Parameter(Mandatory=$false)]
    [int]$DaysBack = 7,
    
    [Parameter(Mandatory=$false)]
    [string]$OutputFile = ".\ScriptBlockLog_Export.csv"
)


$StartTime = (Get-Date).AddDays(-$DaysBack)
$computerName = $env:COMPUTERNAME


function Format-ScriptForCsv {
    param (
        [Parameter(Mandatory=$true)]
        [string]$ScriptText
    )
    
    # Replace problematic characters for CSV
    #$ScriptText = $ScriptText -replace "`r`n", "{{NEWLINE}}" # Replace newlines with placeholder
    #$ScriptText = $ScriptText -replace "`n", "{{NEWLINE}}" # Catch any lone LF
    #$ScriptText = $ScriptText -replace "`r", "{{NEWLINE}}" # Catch any lone CR
    $ScriptText = $ScriptText -replace ",", "{{COMMA}}" # Replace commas
    $ScriptText = $ScriptText -replace '"', '""' # Double any quotes (CSV escaping)
    
    return $ScriptText
}

function Get-ScriptMetadata {
    param (
        [Parameter(Mandatory=$true)]
        [string]$ScriptText
    )
    
    $Metadata = @{
        HasInvokeExpression = $ScriptText -match "Invoke-Expression|IEX|iex"
        HasDownloadString = $ScriptText -match "DownloadString|DownloadFile"
        HasEncodedCommand = $ScriptText -match "-EncodedCommand|-enc "
        HasObfuscation = $ScriptText -match "\{\d+\}\s*\-f" -or $ScriptText -match "join\s*\(" -or $ScriptText -match "replace\s*\(" -or $ScriptText -match "-replace"
        HasCompression = $ScriptText -match "Decompress|FromBase64String"
        HasRemoting = $ScriptText -match "Invoke-Command|New-PSSession|Enter-PSSession"
        CommandCount = ($ScriptText | Select-String -Pattern "\w+-\w+" -AllMatches).Matches.Count
    }
    
    return $Metadata
}

try {
    $Events = Get-WinEvent -FilterHashtable @{
        LogName = 'Microsoft-Windows-PowerShell/Operational'
        Id = 4104
        StartTime = $StartTime
    } -ErrorAction Stop

    Write-Host "Found $($Events.Count) Script Block Logging events. Reconstructing complete script blocks..."
    $ScriptBlocks = @{}
    
    foreach ($Event in $Events) {
        $EventXML = [xml]$Event.ToXml()
        $MessageData = $EventXML.Event.EventData.Data
        
        $ScriptBlockId = ($MessageData | Where-Object { $_.Name -eq 'ScriptBlockId' }).'#text'
        $ScriptBlockText = ($MessageData | Where-Object { $_.Name -eq 'ScriptBlockText' }).'#text'
        $MessageNumber = [int]($MessageData | Where-Object { $_.Name -eq 'MessageNumber' }).'#text'
        $MessageTotal = [int]($MessageData | Where-Object { $_.Name -eq 'MessageTotal' }).'#text'
        $Path = ($MessageData | Where-Object { $_.Name -eq 'Path' }).'#text'
        
        $Key = "$ScriptBlockId"
        # If this is the first fragment of this script block or it's a single-part script block
        if (-not $ScriptBlocks.ContainsKey($Key)) {
            $ScriptBlocks[$Key] = @{
                TimeCreated = $Event.TimeCreated
                UserId = $Event.UserId
                ScriptBlockId = $ScriptBlockId
                Path = $Path
                ScriptBlockText = @{}
                TotalParts = $MessageTotal
                PartsCollected = 0
                EventRecordId = $Event.RecordId
                Complete = $false
                ComputerName = $Event.MachineName
            }
        }
        
        if (-not $ScriptBlocks[$Key].ScriptBlockText.ContainsKey($MessageNumber)) {
            $ScriptBlocks[$Key].ScriptBlockText[$MessageNumber] = $ScriptBlockText
            $ScriptBlocks[$Key].PartsCollected++
            
            # Check if we have all parts of this script block
            if ($ScriptBlocks[$Key].PartsCollected -eq $ScriptBlocks[$Key].TotalParts) {
                $ScriptBlocks[$Key].Complete = $true
            }
        }
    }
    
    # Build the final results by combining script block fragments in correct order
    $Results = @()
    foreach ($Key in $ScriptBlocks.Keys) {
        $ScriptBlock = $ScriptBlocks[$Key]
        
        $CompleteScript = ""
        if ($ScriptBlock.Complete) {
            # Sort by message number to ensure correct order
            for ($i = 1; $i -le $ScriptBlock.TotalParts; $i++) {
                if ($ScriptBlock.ScriptBlockText.ContainsKey($i)) {
                    $CompleteScript += $ScriptBlock.ScriptBlockText[$i]
                }
            }
        } else {
            # For incomplete script blocks, join what we have and note it's incomplete
            for ($i = 1; $i -le $ScriptBlock.TotalParts; $i++) {
                if ($ScriptBlock.ScriptBlockText.ContainsKey($i)) {
                    $CompleteScript += $ScriptBlock.ScriptBlockText[$i]
                }
            }
            $CompleteScript += "`n[WARNING: Incomplete script block - only $($ScriptBlock.PartsCollected) of $($ScriptBlock.TotalParts) parts found]"
        }
        
        $ScriptMetadata = Get-ScriptMetadata -ScriptText $CompleteScript
        
        $FormattedScript = Format-ScriptForCsv -ScriptText $CompleteScript
        
        $ScriptLength = $CompleteScript.Length
        $TruncatedScript = $FormattedScript
        $Truncated = $false
        
        if ($ScriptLength -gt 32000) {  # Excel cell limit is ~32K characters
            $TruncatedScript = $FormattedScript.Substring(0, 32000) + "... [TRUNCATED - Full length: $ScriptLength characters]"
            $Truncated = $true
        }
        
        $Results += [PSCustomObject]@{
            PSComputerName = $computerName
            TimeCreated = $ScriptBlock.TimeCreated
            UserId = $ScriptBlock.UserId
            ComputerName = $ScriptBlock.ComputerName
            ScriptBlockId = $ScriptBlock.ScriptBlockId
            Path = $ScriptBlock.Path
            CompleteScriptBlock = $TruncatedScript
            ScriptLength = $ScriptLength
            IsTruncated = $Truncated
            TotalParts = $ScriptBlock.TotalParts
            IsComplete = $ScriptBlock.Complete
            EventRecordId = $ScriptBlock.EventRecordId
            HasInvokeExpression = $ScriptMetadata.HasInvokeExpression
            HasDownloadString = $ScriptMetadata.HasDownloadString
            HasEncodedCommand = $ScriptMetadata.HasEncodedCommand
            HasObfuscation = $ScriptMetadata.HasObfuscation
            HasCompression = $ScriptMetadata.HasCompression
            HasRemoting = $ScriptMetadata.HasRemoting
            CommandCount = $ScriptMetadata.CommandCount
            SuspiciousRating = (
                [int]$ScriptMetadata.HasInvokeExpression + 
                [int]$ScriptMetadata.HasDownloadString + 
                [int]$ScriptMetadata.HasEncodedCommand + 
                [int]$ScriptMetadata.HasObfuscation + 
                [int]$ScriptMetadata.HasCompression +
                [int]$ScriptMetadata.HasRemoting
            )
        }
    }
    
    $Results = $Results | Sort-Object -Property TimeCreated -Descending

    if ($Results.Count -ne 0) {
        $Results | Select-Object * | Export-Csv -Path $OutputFile -NoTypeInformation
    }
}
catch {
    if ($_.Exception.Message -like "*No events were found*") {
        Write-Warning "No Script Block Logging events were found for the specified time period."
    }
}