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

// TODO - Deploy PowerShell script instead of batch script - need to theorize a bit on best approach
// TODO - Alternate execution mechanisms

type Config struct {
	Commands []struct {
		Command  string     `yaml:"command"`
		FileName string     `yaml:"file_name"`
		Merge    MergeFuncs `yaml:"merge"`
		ID       string     `yaml:"id"`
	} `yaml:"commands"`
}

var (
	// Config
	config = Config{}

	// Arguments
	configFile = flag.String("config", "config.yaml", "path to config file")
	execMethod = flag.String("method", "wmi", "execution method (wmi)")
	targets    = flag.String("targets", "all", "comma-separated list of targets OR file-path to line-delimited targets - if not specified, will query for all enabled computer devices")
	workers    = flag.Int("workers", 250, "number of concurrent workers to use")
	timeout    = flag.Int("timeout", 3, "timeout in minutes for each worker to complete")
	aggregate  = flag.Bool("aggregate", false, "skip everything except aggregation - in the case where the script has already been run and you just want to aggregate the results")

	// Internal
	currentTime     = time.Now().Format("15_04_05")
	signalFile      = fmt.Sprintf("%s_omni_done", currentTime)
	collectionFiles = []string{}
)

func main() {
	var err error
	err = parseArgs()
	if err != nil {
		log.Fatalf("Error parsing arguments: %v", err)
	}
	config, err = parseConfig(*configFile)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}
	err = validateConfig(config)
	if err != nil {
		log.Fatalf("Error validating config: %v", err)
	}

	if *aggregate {
		err = os.Mkdir("aggregated", 0755)
		if err != nil && !os.IsExist(err) {
			log.Fatalf("Error creating aggregated directory: %v", err)
		}
		err = doMerges(config)
		if err != nil {
			log.Printf("Error merging files: %v", err)
		}
		return
	}

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
	batScript, err := buildBatchScript(config)
	if err != nil {
		log.Fatalf("Error building batch script: %v", err)
	}
	log.Printf("Batch Script: %s", batScript)
	err = os.Mkdir("devices", 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Error creating aggregated directory: %v", err)
	}
	startWorkers(batScript, computerTargets, *workers, *timeout)

	// At this point, we have data stored in devices\<target>\*
	// There are a few things we want to do with this data
	// First, we iterate through config to merge files with the same name across all computers based on the specified merge type
	err = os.Mkdir("aggregated", 0755)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("Error creating aggregated directory: %v", err)
	}
	err = doMerges(config)
	if err != nil {
		log.Printf("Error merging files: %v", err)
	}

}

func parseArgs() error {
	flag.Parse()
	validExecutionMethods := []string{"wmi"}
	if !slices.Contains(validExecutionMethods, *execMethod) {
		return fmt.Errorf("invalid execution method: %s", *execMethod)
	}
	return nil
}
