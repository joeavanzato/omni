package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type ComputerReport struct {
	PSComputerName    string
	FilesCopied       bool
	ExecutionSuccess  bool
	SignalFileSuccess bool
	ResultsCollected  bool
}

type SafeCounter struct {
	value int64
	mutex sync.Mutex
}

func NewSafeCounter() *SafeCounter {
	return &SafeCounter{value: 0}
}
func (c *SafeCounter) GetAndIncrement() int64 {
	c.mutex.Lock()
	current := atomic.LoadInt64(&c.value)
	atomic.AddInt64(&c.value, 1)
	c.mutex.Unlock()
	return current + 1
}
func (c *SafeCounter) Get() int64 {
	return atomic.LoadInt64(&c.value)
}

func startWorkers(batchFile string, targets []string, workers int, timeout int, execMethod string) {
	batchBytes, err := os.ReadFile(batchFile)
	if err != nil {
		log.Fatalf("Error reading batch file: %v", err)
	}

	var wg sync.WaitGroup
	workerChan := make(chan string, workers)
	reportChan := make(chan ComputerReport, workers)
	counter := NewSafeCounter()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go workerLoop(batchBytes, workerChan, &wg, reportChan, timeout, counter, len(targets), execMethod)
	}

	var rg sync.WaitGroup
	var computerReportData = make(map[string]ComputerReport)
	rg.Add(1)
	go func() {
		defer rg.Done()
		count := 0
		for {
			report, ok := <-reportChan
			if !ok {
				break
			}
			count += 1
			log.Printf("Target Finished: %s [%d/%d]", report.PSComputerName, count, len(targets))
			computerReportData[report.PSComputerName] = report
		}
	}()
	// Guaranteed to not store duplicates
	for _, target := range targets {
		workerChan <- target
	}
	close(workerChan)
	wg.Wait()
	close(reportChan)
	rg.Wait()
	reportHeaders := []string{"PSComputerName", "FilesCopied", "ExecutionSuccess", "SignalFileSuccess", "ResultsCollected"}
	reportData := make([][]string, 0)
	for _, v := range computerReportData {
		row := []string{
			v.PSComputerName,
			fmt.Sprintf("%t", v.FilesCopied),
			fmt.Sprintf("%t", v.ExecutionSuccess),
			fmt.Sprintf("%t", v.SignalFileSuccess),
			fmt.Sprintf("%t", v.ResultsCollected),
		}
		reportData = append(reportData, row)
	}
	err = exportSliceToCSV(reportHeaders, reportData, "computer_report.csv")
	if err != nil {
		log.Printf("failed to export ComputerReport CSV: %v", err)
	}

}

func workerLoop(batchBytes []byte, workerChan chan string, wg *sync.WaitGroup, reportChan chan ComputerReport, timeout int, counter *SafeCounter, totalTargets int, execMethod string) {
	defer wg.Done()
	for {
		target, ok := <-workerChan
		if !ok {
			break
		}
		if target == "" {
			continue
		}
		computerReport := ComputerReport{
			PSComputerName:    target,
			FilesCopied:       false,
			ExecutionSuccess:  false,
			SignalFileSuccess: false,
			ResultsCollected:  false,
		}

		filesCopiedToTarget := make([]string, 0)
		dirsCopiedToTarget := make([]string, 0) // actually needed?
		log.Printf("Handling Target: %s [%d/%d]", target, counter.GetAndIncrement(), totalTargets)

		// Make C:\Windows\Temp if it does not already exist for some reason
		err := os.Mkdir(fmt.Sprintf("\\\\%s\\C$\\Windows\\temp", target), os.ModeDir)
		if err != nil && !os.IsExist(err) {
			log.Printf("Error creating temp directory %s: %v", target, err)
			reportChan <- computerReport
			continue
		}

		// We won't establish explicit SMB connection because we are on the domain running with appropriate authentication
		// Process can negotiate on our behalf transparently assuming we have permissions and the share is available
		// Deploy auxiliary files (scripts, binaries, etc) specified in config.yaml
		badError := false
		for _, v := range filesToCopy {
			targetPath := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s", target, filepath.Base(v))
			// Copy v to targetPath
			err := copyFile(v, targetPath)
			if err != nil {
				log.Printf("Error copying file %s to %s: %v", v, targetPath, err)
				badError = true
				break
			}
			filesCopiedToTarget = append(filesCopiedToTarget, targetPath)
		}
		if badError {
			log.Printf("Error copying files to %s, skipping target", target)
			reportChan <- computerReport
			continue
		}
		// Deploy required directories
		for _, v := range dirsToCopy {
			targetPath := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s", target, v)
			err := copyDirectory(v, targetPath)
			if err != nil {
				log.Printf("Error copying directory %s to %s: %v", v, targetPath, err)
				continue
			}
			dirsCopiedToTarget = append(dirsCopiedToTarget, targetPath)
		}

		// Deploy Batch
		batchFile := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s_omni.bat", target, currentTime)
		err = os.WriteFile(batchFile, batchBytes, 0644)
		if err != nil {
			log.Printf("Error writing batch file to %s: %v", target, err)
			reportChan <- computerReport
			continue
		}
		filesCopiedToTarget = append(filesCopiedToTarget, batchFile)
		computerReport.FilesCopied = true
		// Execute Batch
		taskName := fmt.Sprintf("omni_launcher_%s", currentTime)
		if execMethod == "wmi" {
			err = executeRemoteWMI(target, fmt.Sprintf("cmd.exe /c %s", batchFile), "C:\\Windows\\temp", "", "", "")
			if err != nil {
				log.Printf("Error executing batch file on %s: %v", target, err)
				reportChan <- computerReport
				continue
			}
		} else if execMethod == "schtasks" {
			err = runTask(target, fmt.Sprintf("cmd.exe /c %s", batchFile), taskName)
			if err != nil {
				log.Printf("Error executing batch file on %s: %v", target, err)
				reportChan <- computerReport
				continue
			}
		} else if execMethod == "sc" {
			err = runService(target, fmt.Sprintf("C:\\Windows\\System32\\cmd.exe /c %s", batchFile), taskName)
			if err != nil {
				log.Printf("Error executing batch file on %s: %v", target, err)
				reportChan <- computerReport
				continue
			}
		}
		computerReport.ExecutionSuccess = true

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Minute)
		tempSignalFile := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s", target, signalFile)
		filesCopiedToTarget = append(filesCopiedToTarget, tempSignalFile)
		// Wait for signal file
		sentReport := false
		done := false
		for {
			select {
			case <-ctx.Done():
				done = true
				// Delete Copied Directories
				for _, v := range dirsToCopy {
					tmp := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s", target, v)
					err = os.RemoveAll(tmp)
					if err != nil && !os.IsNotExist(err) {
						log.Printf("Error deleting directory %s: %v", tmp, err)
						continue
					}
				}

				// Delete Copied Files
				for _, v := range filesCopiedToTarget {
					err = os.Remove(v)
					if err != nil && !os.IsNotExist(err) {
						log.Printf("Error deleting file %s: %v", v, err)
						continue
					}
				}

				if execMethod == "schtasks" {
					err = deleteTask(target, taskName)
					if err != nil {
						log.Printf("Error deleting task %s on %s: %v", taskName, target, err)
					}
				}
				if execMethod == "sc" {
					err = deleteService(target, taskName)
					if err != nil {
						log.Printf("Error deleting service %s on %s: %v", taskName, target, err)
					}
				}
				break
			default:
				time.Sleep(5 * time.Second)
				_, err = os.Stat(tempSignalFile)
				if err == nil {
					time.Sleep(1 * time.Second)
					computerReport.SignalFileSuccess = true

					// Collect and Delete Output Files
					collectionFolder := fmt.Sprintf("devices\\%s", target)
					err = os.MkdirAll(collectionFolder, os.ModePerm)
					if err != nil {
						log.Printf("Error creating collection folder %s: %v", collectionFolder, err)
						reportChan <- computerReport
						sentReport = true
						cancel()
						continue
					}

					for _, v := range collectionFiles {
						collectionFile := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s", target, v)
						destinationFile := fmt.Sprintf("%s\\%s", collectionFolder, v)
						err = copyFile(collectionFile, destinationFile)
						if err != nil && !os.IsNotExist(err) {
							log.Printf("Error copying file %s to %s: %v", collectionFile, destinationFile, err)
							continue
						} else if err != nil && os.IsNotExist(err) {
							continue
						}
						err = os.Remove(collectionFile)
						if err != nil {
							log.Printf("Error deleting file %s: %v", collectionFile, err)
							continue
						}
					}

					for _, v := range collectionDirs {
						collectionDir := fmt.Sprintf("\\\\%s\\C$\\Windows\\temp\\%s", target, v)
						destinationDir := fmt.Sprintf("%s\\%s", collectionFolder, v)
						err = copyDirectory(collectionDir, destinationDir)
						if err != nil && !os.IsNotExist(err) {
							log.Printf("Error copying directory %s to %s: %v", collectionDir, destinationDir, err)
							continue
						} else if err != nil && os.IsNotExist(err) {
							continue
						}
						err = os.RemoveAll(collectionDir)
						if err != nil {
							log.Printf("Error deleting directory %s: %v", collectionDir, err)
							continue
						}
					}

					computerReport.ResultsCollected = true
					reportChan <- computerReport
					sentReport = true
					cancel()
				}
			}
			if done {
				break
			}
		}
		if !sentReport {
			reportChan <- computerReport
		}
		cancel()
	}
}
