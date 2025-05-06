
param(
    [Parameter(Mandatory=$false)]
    [string]$OutputFile = ".\NamedPipeExport.csv"
)



Add-Type -TypeDefinition @"
using System;
using System.Runtime.InteropServices;

public class Kernel32
{
    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern IntPtr CreateFile(
        string lpFileName,
        uint dwDesiredAccess,
        uint dwShareMode,
        IntPtr lpSecurityAttributes,
        uint dwCreationDisposition,
        uint dwFlagsAndAttributes,
        IntPtr hTemplateFile);

    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool GetNamedPipeServerProcessId(
        IntPtr hNamedPipe,
        ref uint ServerProcessId);

    [DllImport("kernel32.dll", SetLastError = true)]
    public static extern bool CloseHandle(
        IntPtr hObject);

    public const int INVALID_HANDLE_VALUE = -1;
}
"@

# Get all network connections first and store them for reference
$netConnections = Get-NetTCPConnection | Where-Object { $_.RemoteAddress -ne '::' -and $_.RemoteAddress -ne '0.0.0.0' -and $_.RemoteAddress -ne '127.0.0.1' -and $_.RemoteAddress -ne '::1' }

# Create a hashtable for quick lookup of connections by PID
$connectionsByPid = @{}
foreach ($conn in $netConnections) {
    if (-not $connectionsByPid.ContainsKey($conn.OwningProcess)) {
        $connectionsByPid[$conn.OwningProcess] = @()
    }
    # Add connection info with format "RemoteAddress:RemotePort (State)"
    $connectionsByPid[$conn.OwningProcess] += "$($conn.RemoteAddress):$($conn.RemotePort) ($($conn.State))"
}

$pipes = [System.IO.Directory]::EnumerateFiles('\\.\pipe\');
$output = @()
$pipeOwner = 0
$computerName = $env:COMPUTERNAME
ForEach($pipe in $pipes){
    $hPipe = [Kernel32]::CreateFile($pipe, [System.IO.FileAccess]::Read, [System.IO.FileShare]::None, [System.IntPtr]::Zero, [System.IO.FileMode]::Open, [System.UInt32]::0x80,[System.IntPtr]::Zero)
    if ($hPipe -eq -1)
    {
        $output += New-Object PSObject -Property @{
                PSComputerName = $computerName
                ProcessID = "-"
                ProcessName = "-"
                CommandLine = "-"
                User = "-"
                Pipe = $pipe
                RemoteConnections = "-"
            }
        continue
    }
    $pipeOwnerFound = [Kernel32]::GetNamedPipeServerProcessId([System.IntPtr]$hPipe, [ref]$pipeOwner)
    if ($pipeOwnerFound)
    {
        $process = Get-WmiObject -Query "SELECT * FROM Win32_Process WHERE ProcessID = $pipeOwner"
        $owner = $process.GetOwner();
        if ($owner){$User = $owner.User}else{$User = "-"}
        
        if ($connectionsByPid.ContainsKey($pipeOwner)) {
            $remoteConnections = $connectionsByPid[$pipeOwner] -join ", "
        } else {
            $remoteConnections = "-"
        }
        

        $output += New-Object PSObject -Property @{
            PSComputerName = $computerName
            ProcessID = $pipeOwner
            ProcessName = $process.Name
            CommandLine = $process.CommandLine
            User = $User
            Pipe = $pipe
            RemoteConnections = $remoteConnections
        }
    }
    $closeHandle = [Kernel32]::CloseHandle($hPipe)
}

if ($output.Count -ne 0) {
    $output | Select-Object PSComputerName,ProcessID,ProcessName,CommandLine,User,Pipe,RemoteConnections | Export-Csv -Path $OutputFile -NoTypeInformation
}