param(
    [Parameter(Mandatory = $true)]
    [string]$directory,
    [Parameter(Mandatory = $false)]
    [string]$outputFile = (Join-Path $directory 'Merged.csv')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path -Path $directory -PathType Container)) {
    exit 1
}
$csvFiles = Get-ChildItem -Path $directory -Filter "*.csv" -File
if ($csvFiles.Count -eq 0) {
    Write-Host "No CSV files found in the directory."
    exit
}
$allHeaders = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)

$standardColumns = @("PSComputerName","SourceFileName")
foreach ($col in $standardColumns) {
    [void]$allHeaders.Add($col)
}

Write-Host "Collecting headers from all files..."
foreach ($file in $csvFiles) {
    try {
        $firstRow = Import-Csv -Path $file.FullName
        if ($firstRow) {
            foreach ($prop in $firstRow[0].PSObject.Properties.Name) {
                [void]$allHeaders.Add($prop)
            }
        }
    }
    catch {
        Write-Warning "Error reading headers from $($file.Name): $_"
    }
}
$headerArray = [System.Linq.Enumerable]::ToArray($allHeaders)
$dataColumns = $headerArray | Where-Object { $_ -notin $standardColumns } | Sort-Object
$headerArray = $standardColumns + $dataColumns
$headerLine = '"' + ($headerArray -join '","') + '"'
Set-Content -Path $outputFile -Value $headerLine -Encoding UTF8
$outFileStream = [System.IO.StreamWriter]::new($outputFile, $true, [System.Text.Encoding]::UTF8)
$computerName = $env:COMPUTERNAME
foreach ($file in $csvFiles) {
    Write-Host "Processing $($file.Name)..."
    
    try {
        $csvData = Import-Csv -Path $file.FullName
        foreach ($dataRow in $csvData) {
            $rowData = [ordered]@{}
            $rowData["PSComputerName"] = $computerName
            $rowData["SourceFileName"] = $file.Name
            
            foreach ($header in $dataColumns) {
                if ($dataRow.PSObject.Properties.Name -contains $header) {
                    $rowData[$header] = $dataRow.$header
                }
                else {
                    $rowData[$header] = ""
                }
            }
            
            # Build the CSV line
            $values = @()
            foreach ($header in $headerArray) {
                $value = $rowData[$header]
                
                # Handle values that might contain commas by ensuring they're properly quoted
                if ($value -match ',|\r|\n|"') {
                    $value = $value -replace '"', '""'  # Double up any existing quotes
                    $value = """$value"""  # Wrap in quotes
                } 
                elseif ($value -eq $null -or $value -eq "") {
                    $value = '""'  # Empty values get empty quotes
                }
                
                $values += $value
            }
            
            $outFileStream.WriteLine($values -join ',')
        }
    }
    catch {
        Write-Warning "Error processing file $($file.Name): $_"
    }
}

$outFileStream.Close()
$outFileStream.Dispose()