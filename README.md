

<p align="center">
  <img width="400" src="images/omni.png">
</p>

### What is it?

- An open-source, modular, extensible utility for orchestrating evidence collection from on-prem Windows computers via remote deployment and execution of commands, scripts and tools to enhance the efficiency of Incident Responders

**omni** helps incident responders **rapidly aggregate information** from domain-joined devices across an enterprise network.

The main focus is on **collecting light-weight datasets as quickly as possible** to help responders identify anomalies and quickly hunt known-bad indicators across a network - but technically it is possible to execute and collect any type of evidence using any type of host-based tooling.

If you find yourself in a situation where you need to pivot on indicators of compromise (IPs, Filenames, Processes, User Activity, Service/Task Names, etc) in a cyber-immature network (lack of EDR, SIEM, Logging, etc), omni can help you get answers.

It is easy to collect new data by modifying config.yaml on the fly to run scripts, commands or tools on remote devices.

It works by dynamically building a batch file that is deployed to targets along with any specified files and directories - this batch file controls execution and is remotely executed via schtasks by default - output files are then retrieved via SMB and deployed artifacts are cleaned-up from each target.

omni can receive a list of targets at the command-line, via a line-delimited file or can dynamically query Active Directory for all enabled computer accounts to use as response targets.

Warning - it is easy to accidentally collect **a lot** of data - be mindful of this when building your configuration file - omni will collect everything specified in the config.yaml file for each target - if you have 1000 devices and each device produces 5 megabytes of data following execution, you will be collecting 5 gigabytes of data, etc.

<p align="center">
  <img src="images/2.png">
</p>

### When would I need this?

Consider the following questions - if you answer 'yes' to any of these, omni can help you.

* Do you have a need to execute and collect the results of one or more commands/scripts/tools on multiple devices concurrently?
* Do you need to collect data from a large amount of devices that are not connected to the internet?
* Have you ever run into issues trying to rapidly pivot on indicators of compromise across a large number of devices?
* Does the current environment lack a centralized logging solution or EDR that can help you quickly query devices?
* Do you need to execute a series of triage scripts on 1 or more networked devices?

### Example Usage
```
omni.exe -tags builtin
- Launch omni with all targets from .\config.yaml having tag 'builtin' with default timeout 
(15) and worker (250) settings, using Scheduled Tasks for execution and querying AD for enabled computers to use as targets

omni.exe -workers 500 -timeout 30 -tags quick,process
- Add more workers, increase the timeout duration per-target and only use configurations with the specified tags

omni.exe -targets hostname1,hostname2,hostname3
omni.exe -targets targets.txt
- Use the specified computer targets from command-line or file

omni.exe -method wmi
- Deploy omni using WMI instead of Scheduled Tasks for remote execution

omni.exe -config configs\test.yaml
- Execute a specific named configuration file
```

### Configuration File
The configuration file controls omni's behavior - it is a YAML file that specifies commands to run, files/directories to copy, tools to prepare/download, etc.

config.yaml can specify individual commands to execute - each of which are loaded into a batch file and prefixed with cmd.exe /c - for example:
```yaml
command: powershell.exe -Command "Get-WmiObject -Class Win32_StartupCommand -Locale MS_409 -ErrorAction SilentlyContinue | Select PSComputerName,Caption,Command,Description,Location,Name,User,UserSID | Export-Csv -Path '$FILENAME$' -NoTypeInformation"
file_name: $time$_wmi_startups.csv
merge: csv
id: wmi_startups
tags: [quick, persistence]
```

We can also specify files - locally or remotely, that are to be copied to the target device for further use - such as copying a complex PowerShell script to execute:

```yaml
command: powershell.exe -Command "Add-MpPreference -ExclusionPath "C:\Windows\Temp\trawler.ps1" -Force" & powershell.exe C:\Windows\temp\trawler.ps1 -csvfilename '$FILENAME$' -OutputLocation 'C:\Windows\temp' -ScanOptions ActiveSetup,AMSIProviders,AppCertDLLs,AppInitDLLs,ApplicationShims,AppPaths,AssociationHijack,AutoDialDLL,BIDDll,BITS,BootVerificationProgram,COMHijacks,CommandAutoRunProcessors,Connections,ContextMenu,ChromiumExtensions,DebuggerHijacks,DNSServerLevelPluginDLL,DisableLowIL,DirectoryServicesRestoreMode,DiskCleanupHandlers,ErrorHandlerCMD,ExplorerHelperUtilities,FolderOpen,GPOExtensions,GPOScripts,HTMLHelpDLL,IFEO,InstalledSoftware,InternetSettingsLUIDll,KnownManagedDebuggers,LNK,LSA,MicrosoftTelemetryCommands,ModifiedWindowsAccessibilityFeature,MSDTCDll,Narrator,NaturalLanguageDevelopmentDLLs,NetSHDLLs,NotepadPPPlugins,OfficeAI,OfficeGlobalDotName,Officetest,OfficeTrustedLocations,OfficeTrustedDocuments,OutlookStartup,PATHHijacks,PeerDistExtensionDll,PolicyManager,PowerShellProfiles,PrintMonitorDLLs,PrintProcessorDLLs,RATS,RDPShadowConsent,RDPStartupPrograms,RemoteUACSetting,ScheduledTasks,ScreenSaverEXE,ServiceControlManagerSD,SEMgrWallet,ServiceHijacks,Services,SethcHijack,SilentProcessExitMonitoring,Startups,SuspiciousFileLocation,TerminalProfiles,TerminalServicesDLL,TerminalServicesInitialProgram,TimeProviderDLLs,TrustProviderDLL,UninstallStrings,UserInitMPRScripts,Users,UtilmanHijack,WellKnownCOM,WERRuntimeExceptionHandlers,WindowsLoadKey,WindowsUnsignedFiles,WindowsUpdateTestDlls,WinlogonHelperDLLs,WMIConsumers,Wow64LayerAbuse,WSL & powershell.exe -Command "Remove-MpPreference -ExclusionPath "C:\Windows\Temp\trawler.ps1" -Force"
file_name: $time$_trawler.csv
merge: csv
id: trawler
tags: [persistence]
dependencies: [https://raw.githubusercontent.com/joeavanzato/Trawler/refs/heads/main/trawler.ps1]
```

This configuration tells omni attempt to download the specified file on our analysis machine using the base name (trawler.ps1) for copying to remote targets.

We also could have it specify a local file to copy if we don't want to invoke http requests to download something on our main device / can't download anything.

```yaml
command: powershell.exe -Command "Add-MpPreference -ExclusionPath "C:\Windows\Temp\trawler.ps1" -Force" & powershell.exe C:\Windows\temp\trawler.ps1 -csvfilename '$FILENAME$' -OutputLocation 'C:\Windows\temp' -ScanOptions ActiveSetup,AMSIProviders,AppCertDLLs,AppInitDLLs,ApplicationShims,AppPaths,AssociationHijack,AutoDialDLL,BIDDll,BITS,BootVerificationProgram,COMHijacks,CommandAutoRunProcessors,Connections,ContextMenu,ChromiumExtensions,DebuggerHijacks,DNSServerLevelPluginDLL,DisableLowIL,DirectoryServicesRestoreMode,DiskCleanupHandlers,ErrorHandlerCMD,ExplorerHelperUtilities,FolderOpen,GPOExtensions,GPOScripts,HTMLHelpDLL,IFEO,InstalledSoftware,InternetSettingsLUIDll,KnownManagedDebuggers,LNK,LSA,MicrosoftTelemetryCommands,ModifiedWindowsAccessibilityFeature,MSDTCDll,Narrator,NaturalLanguageDevelopmentDLLs,NetSHDLLs,NotepadPPPlugins,OfficeAI,OfficeGlobalDotName,Officetest,OfficeTrustedLocations,OfficeTrustedDocuments,OutlookStartup,PATHHijacks,PeerDistExtensionDll,PolicyManager,PowerShellProfiles,PrintMonitorDLLs,PrintProcessorDLLs,RATS,RDPShadowConsent,RDPStartupPrograms,RemoteUACSetting,ScheduledTasks,ScreenSaverEXE,ServiceControlManagerSD,SEMgrWallet,ServiceHijacks,Services,SethcHijack,SilentProcessExitMonitoring,Startups,SuspiciousFileLocation,TerminalProfiles,TerminalServicesDLL,TerminalServicesInitialProgram,TimeProviderDLLs,TrustProviderDLL,UninstallStrings,UserInitMPRScripts,Users,UtilmanHijack,WellKnownCOM,WERRuntimeExceptionHandlers,WindowsLoadKey,WindowsUnsignedFiles,WindowsUpdateTestDlls,WinlogonHelperDLLs,WMIConsumers,Wow64LayerAbuse,WSL & powershell.exe -Command "Remove-MpPreference -ExclusionPath "C:\Windows\Temp\trawler.ps1" -Force"
file_name: $time$_trawler.csv
merge: csv
id: trawler
tags: [persistence]
dependencies: [trawler.ps1]
```
Just remember - all files are copied into C:\Windows\temp\$BASENAME$ for the subsequent execution command.  Files are only copied once - so even if you need the same dependency or executable for multiple commands, omni knows it only needs to execute a single copy operation for each source file no matter how many times it is specified in different configurations.

config.yaml comes preloaded to run many EZ Tools - to make the most of this, use 'omni.exe -prepare' to execute preparation statements, including Get-ZimmermanTools and any other configured commands designed to stage the response directory.
use ',' as a delimiter, like below.

Some of these tools require ancillary files (DLLs, etc) be copied to the host, like below.
```yaml
command: C:\Windows\Temp\PECmd.exe -d C:\Windows\Prefetch --csvf $FILENAME$ & powershell.exe -Command "$data = Import-CSV -Path $FILENAME$; $data | Select-Object -Property @{name='PSComputerName'; expression={ $env:COMPUTERNAME }},* | Export-CSV -Path $FILENAME$ -NoTypeInformation"
file_name: $time$_PECmd.csv
merge: csv
id: PECmd
dependencies: [net6\PECmd.exe,net6\PECmd.dll]
```

Dependencies can also specify a directory tree to copy to the target instead of specific files.

```yaml
command: C:\Windows\Temp\net6\RECmd\RECmd.exe -d C:\Users --csvf $FILENAME$ --bn C:\Windows\Temp\net6\RECmd\BatchExamples\DFIRBatch.reb && powershell.exe -Command "$data = Import-CSV -Path $FILENAME$; $data | Select-Object -Property @{name='PSComputerName'; expression={ $env:COMPUTERNAME }},* | Export-CSV -Path $FILENAME$ -NoTypeInformation"
file_name: $time$_RECmd_DFIRBatch_Users.csv
merge: csv
id: RECmd_DFIRBatch_Users
dependencies: [net6\RECmd]
```

For evidence retrieval, there are a few things to consider:
* Some tools may produce multiple files and we may not have control over the output names
  * Sometimes we can get away with aggregating to a single named file via PowerShell addon, but not always
* Sometimes we may want to collect an entire output directory
* Sometimes we may want to collect a single file

One example of this is the output of WxTCmd.exe - designed to process ActivitiesCache.db files from Windows10+ devices - to effectively run this on a remote device, we would need to wrap it in some PowerShell like below:
```powershell
$files = Get-ChildItem -Path "C:\Users\*\AppData\Local\ConnectedDevicesPlatform\*\ActivitiesCache.db" -Recurse;
foreach ($f in $files){
cmd.exe /c WxTCmd.exe -f "$($f.FullName)" --csv C:\Windows\Temp\wxtcmd
}
```

Now we know that the tool will execute on each discovered cache file and output dynamically into C:\Windows\temp\wxtcmd - we can instruct omni to run this remotely after copying WxTCmd to the target and then copy back an entire directory of files instead of a single file like below:

```yaml
command: powershell.exe -Command "$files = Get-ChildItem -Path 'C:\Users\*\AppData\Local\ConnectedDevicesPlatform\*\ActivitiesCache.db' -Recurse;foreach ($f in $files){cmd.exe /c C:\Windows\Temp\WxTCmd.exe -f "$($f.FullName)" --csv C:\Windows\Temp\$DIRNAME$}"
dir_name: $time$_WxTCmd
merge: none
id: WxTCmd
tags: [execution, quick, eztools, wxtcmd]
dependencies: [net6\WxTCmd.dll,net6\WxTCmd.exe,net6\WxTCmd.runtimeconfig.json]
```

The other problem with this type of tooling is that it produces a variable number of CSV files by default, such as for different users or aspects of the artifact in question.  omni includes a helper script for merging disparate CSV files (CSVMerge.ps1) that we can deploy to targets to help improve our analysis and aggregation capabilities - the below configuration will run the tooling then merge all of the output CSVs into a single CSV as well as adding the local hostname to the output - this makes it easier for us to run the artifact on many targets and return all results to a single output file.

To take advantage of this, we can simply modify the command like below:

```yaml
command: powershell.exe -Command "$files = Get-ChildItem -Path 'C:\Users\*\AppData\Local\ConnectedDevicesPlatform\*\ActivitiesCache.db' -Recurse;foreach ($f in $files){cmd.exe /c C:\Windows\Temp\WxTCmd.exe -f "$($f.FullName)" --csv C:\Windows\Temp\$DIRNAME$}" && powershell.exe C:\Windows\temp\CSVMerge.ps1 -addhostname -directory C:\Windows\temp\$DIRNAME$ -outputFile $FILENAME$
dir_name: $time$_WxTCmd
file_name: $time$_WxTCmd_Merged.csv
merge: none
id: WxTCmd
tags: [execution, quick, eztools, wxtcmd]
dependencies: [net6\WxTCmd.dll,net6\WxTCmd.exe,net6\WxTCmd.runtimeconfig.json]
```

### Preparation
It is also possible to run 'preparation' commands as specified in the configuration file when using the '-prepare' switch - these are designed to execute before anything else on the localhost and are intended to download, organize or otherwise prepare local tools/scripts for use later on against remote hosts as needed.

One example of this is to execute Get-ZimmermanTools to ensure they exist before we use them on remote hosts.
```yaml
preparations:
  - command: powershell.exe -Command "iex ((New-Object System.Net.WebClient).DownloadString('https://raw.githubusercontent.com/EricZimmerman/Get-ZimmermanTools/refs/heads/master/Get-ZimmermanTools.ps1'))"
    note: Download and execute Get-ZimmermanTools into current working directory
```
Each command is written to a temporary batch file and prefixed with cmd.exe /c before executing the bat.  Commands specified in either preparation or commands are executed in the order they are defined.

### Collection
After commands are finished executing on each host, omni will collect results back to a directory like 'devices\$DEVICENAME' for each host - this will look like below:

<p align="center">
  <img src="images/1.png">
</p>

File names correspond to the name specified in the config.yaml file for each executed command.  

Additionally, if a merge function is specified such as 'csv', omni will attempt to merge all files across all devices when collection is complete to produce a unified file for each command output.

For this to be useful, you should ensure that each command output includes a 'hostname' or similar - omni can also force-add a hostname column if the command configuration includes addhostname:true, such as below:
yaml
```
command: powershell.exe -Command "Get-NetNeighbor -ErrorAction SilentlyContinue | Select-Object * | Export-Csv -NoTypeInformation -Path '$FILENAME$'"
file_name: $time$_arp_cache.csv
merge: csv
id: arp_cache
add_hostname: true
```
This will insert a column named 'PSComputerName' that will reflect the name of the base directory containing the current file being merged.

### Detailed Usage
```
  -aggregate
        skip everything except aggregation - in the case where the script has already been run and you just want to aggregate the results
  -config string
        path to config file (default "config.yaml")
  -method string
        execution method (wmi, schtasks) (default "schtasks")
  -nodownload
        skip downloading missing files contained inside 'commands' section of the config file
  -prepare
        executes commands on localhost listed in the 'prepare' section of the config file
  -tags string
        comma-separated list of tags to filter the config file by - if not specified, all commands will be executed (default "*")
  -targets string
        comma-separated list of targets OR file-path to line-delimited targets - if not specified, will query for all enabled computer devices (default "all")
  -timeout int
        timeout in minutes for each worker to complete (default 15)
  -workers int
        number of concurrent workers to use (default 250)
```

### Running Common Tools

omni is designed to be flexible - as such, it is more than feasible to run any type of host-based tool at scale - for example, KAPE - we could setup a command processor that drops KAPE on our targets, executes and then collects the resulting ZIPs like below:
```yaml
command: C:\windows\temp\kape\kape.exe --tsource C --tdest C:\Windows\temp\kape\machine\ --tflush --target !SANS_Triage --zip kape && powershell.exe -Command "$kapezip = Get-ChildItem -Path C:\Windows\temp\kape\machine\*.zip; Rename-Item -Path $kapezip.FullName -NewName '$FILENAME$'"
file_name: $time$_kape.zip
merge: pool
id: kape
add_hostname: True
dependencies: [KAPE]
```

This will result in running KAPE with the specified arguments, copying the resulting ZIP back to our device folder and then renaming it with the detected hostname and moving all output ZIPs into our 'aggregated' directory once all collections are completed.

### Merge Types
* csv
  * Merges all CSV files having the same suffix into a single CSV file - this is most appropriate if your command/tool outputs to a CSV
  * Expects the CSVs to have the same headers - will skip files that do not match the first one inspected
  * add_hostname will insert a column at the beginning of the CSV with the detected device name based on the parent directory
* none
  * Do not do any type of merging on collected files - they will remain in their per-device directories
* pool
  * Collect files that match the suffix and move them into the 'aggregated' directory
  * add_hostname will add a prefix to the filename with the detected device name based on the parent directory
  * This will remove the original file from it's original location inside 'devices'
* asym_csv
  * Used for merging CSV files with different headers - slightly slower than a normal CSV merge, but interchangeable

### Other Topics

Tools have dependencies - for example, you can download 3 different versions of most EZTools like PECmd - targeting .NET 4, 6 and 9 respectively - in most situations, investigators first collect raw evidence then have a standardized processing setup where the data can be parsed.

If we really wanted to, we could copy all 3 versions of an EZTool to the target, write a PowerShell script that determines which version of .NET exists then execute the appropriate version.  We could also just write a command that ZIPs up all Prefetch files on the target and brings them back to our machine for analysis.  Or deploy KAPE as discussed above to do this for us.

### File Sizes

Keep in mind - every configuration executed means more data to collect from a target host - if we target 1000 devices and each devices produces 5 megabytes, we have 5 gigabytes to pickup - not a massive number but this can easily grow exponentially if we execute a configuration that produces a large amount of data.  

This is something to consider when you are building a configuration file that meets your needs and requirements - remove those that are unnecessary for your usecase to reduce overall data volume.