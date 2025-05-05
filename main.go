package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"
)

// TODO - ZIP files/dirs to copy for quicker transfer and unpack on target machines with PowerShell preamble
// TODO - Convert long YAML to multiline for readability
// TODO - Allow specification of specific command to run by ID
// TODO - Force Stop PID if it is still running after context cancel/timeout
// TODO - Add example configs for common hunt scenarios
// TODO - Add -term support to ExtractScriptBlockLogging/ExtractConsoleHostHistory
// TODO - Add -user support to ExtractRDPActivity/ExtractLogons
// TODO - Allow customization of temporary execution directory instead of C:\Windows\Temp
// TODO - Add support for checking if certain files already exist before executing preparation command
// TODO - Asymmetric CSV Merge to support disparate column names
// TODO - Include dependency cleanup in the batch file directly - this can be useful if the program is terminated abruptly mid-execution

type Config struct {
	Preparations []struct {
		Command string `yaml:"command"`
		Note    string `yaml:"note"`
	} `yaml:"preparations"`
	Commands []Command `yaml:"commands"`
}

type Command struct {
	Command      string     `yaml:"command"`      // The command to execute
	FileName     string     `yaml:"file_name"`    // Used to replace $FILENAME$ in cmdline for retrieval/execution
	DirName      string     `yaml:"dir_name"`     // Used to replace $DIRNAME$ in cmdline for retrieval/execution
	Merge        MergeFuncs `yaml:"merge"`        // Specifies how, if at all, output files should be merged
	ID           string     `yaml:"id"`           // Unique ID for the command
	SkipDir      bool       `yaml:"skip_dir"`     // If true, $FILENAME$ will not have C:\Windows\Temp added
	AddHostname  bool       `yaml:"add_hostname"` // When merging, should we add a hostname column (PSComputerName) - for outputs where it may not be possible to include in the file directly
	Tags         []string   `yaml:"tags"`         // Filter Tags
	Dependencies []string   `yaml:"dependencies"` // File/Directory dependencies
}

var (
	// Config
	config = Config{}

	// Arguments
	configFile = flag.String("config", "config.yaml", "path to config file")
	execMethod = flag.String("method", "schtasks", "execution method (wmi, schtasks, sc)")
	targets    = flag.String("targets", "all", "comma-separated list of targets OR file-path to line-delimited targets - if not specified, will query for all enabled computer devices")
	workers    = flag.Int("workers", 250, "number of concurrent workers to use")
	timeout    = flag.Int("timeout", 15, "timeout in minutes for each worker to complete")
	aggregate  = flag.Bool("aggregate", false, "skip everything except aggregation - in the case where the script has already been run and you just want to aggregate the results")
	nodownload = flag.Bool("nodownload", false, "skip downloading missing files contained inside 'commands' section of the config file")
	prep       = flag.Bool("prepare", false, "executes commands on localhost listed in the 'prepare' section of the config file")
	tags       = flag.String("tags", "*", "comma-separated list of tags to filter the config file by - if not specified, all commands will be executed")
	daysBack   = flag.Int("daysback", 7, "number of days to go back for commands that contain $DAYSBACK$ string")

	// Internal
	currentTime       = time.Now().Format("15_04_05")
	signalFile        = fmt.Sprintf("%s_omni_done", currentTime) // When this exists, signals batch execution is completed
	collectionFiles   = []string{}                               // Files to collect from targets
	collectionDirs    = []string{}                               // Directories insice C:\Windows\Temp to collect from targets
	commandsExecuting = []Command{}                              // Commands that are included in the current run
)

func main() {
	printLogo()
	var err error
	err = parseArgs()
	if err != nil {
		log.Fatalf("Error parsing arguments: %v", err)
	}
	log.Printf("Using config File: %s", *configFile)
	log.Printf("Parsing config...\n")
	config, err = parseConfig(*configFile)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
	log.Printf("Validating config...\n")
	err = validateConfig(config)
	if err != nil {
		log.Fatalf("Error validating config: %v", err)
	}

	if *prep {
		log.Printf("Executing preparation commands on localhost")
		// Execute the preparation commands on localhost
		for _, v := range config.Preparations {
			log.Printf("Command: %s", v.Command)
			log.Printf("Note: %s", v.Note)
			err = executeCommand(v.Command)
			if err != nil {
				log.Printf("Error executing preparation command: %v", err)
			}
		}
		return
	}

	if *aggregate {
		log.Printf("Skipping execution - aggregating existing results")
		err = os.Mkdir("aggregated", 0755)
		if err != nil && !os.IsExist(err) {
			log.Fatalf("Error creating aggregated directory: %v", err)
		}
		err = doMerges(config, true)
		if err != nil {
			log.Printf("Error merging files: %v", err)
		}
		return
	}

	log.Printf("Gathering Target List...\n")
	computerTargets := make([]string, 0)
	if *targets != "all" {
		// Check if it's a valid file-path first - if not, it must be a list of targets
		if _, err = os.Stat(*targets); err == nil {
			// Read file lines to slice
			file, err := os.ReadFile(*targets)
			if err != nil {
				log.Fatalf("Error reading targets file: %v", err)
			}
			computerTargets = strings.Split(string(file), "\r\n")
		} else {
			// Not a valid file-path - must be a list of targets with potential comma-separations - break it out
			computerTargets = strings.Split(*targets, ",")
		}
	} else {
		// Query for all enabled computer devices
		computerTargets, err = getAllEnabledComputerDevices()
		if err != nil {
			log.Fatalf("Error querying for all enabled computer devices: %v", err)
		}
	}
	computerTargets = deduplicateSlice(computerTargets)
	log.Printf("Total Target Devices: %d", len(computerTargets))
	log.Printf("Execution Method: %s", *execMethod)
	log.Printf("Timeout: %d minutes", *timeout)
	log.Printf("Workers: %d", *workers)
	log.Printf("Building Batch Script...\n")

	tmpTags := strings.Split(strings.ToLower(*tags), ",")
	batScript, err := buildBatchScript(config, *nodownload, tmpTags, *daysBack)
	if err != nil {
		log.Fatalf("Error building batch script: %v", err)
	}
	log.Printf("Batch Script: %s", batScript)
	err = os.Mkdir("devices", 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Error creating aggregated directory: %v", err)
	}
	log.Printf("Starting...\n")
	start := time.Now()
	startWorkers(batScript, computerTargets, *workers, *timeout, *execMethod)

	// At this point, we have data stored in devices\<target>\*
	// There are a few things we want to do with this data
	// First, we iterate through config to merge files with the same name across all computers based on the specified merge type
	err = os.Mkdir("aggregated", 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Error creating aggregated directory: %v", err)
	}
	log.Printf("Aggregating Results...\n")
	err = doMerges(config, false)
	if err != nil {
		log.Printf("Error merging files: %v", err)
	}
	log.Printf("Done!\n")
	log.Printf("Total Time: %s\n", time.Since(start))

}

func parseArgs() error {
	flag.Parse()
	validExecutionMethods := []string{"wmi", "schtasks", "sc"}
	if !slices.Contains(validExecutionMethods, *execMethod) {
		return fmt.Errorf("invalid execution method: %s", *execMethod)
	}
	if *execMethod == "sc" {
		return fmt.Errorf("exec method 'sc' is not fully supported yet")
	}
	return nil
}
