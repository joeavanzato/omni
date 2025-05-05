package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

type MergeFuncs string

var validMergeFuncs = []MergeFuncs{MergeFuncCSV, MergeFuncNone, MergeFuncPool, MergeFuncAsymcCSV}

const (
	MergeFuncCSV      MergeFuncs = "csv"
	MergeFuncNone     MergeFuncs = "none"
	MergeFuncPool     MergeFuncs = "pool"
	MergeFuncAsymcCSV MergeFuncs = "asym_csv"
)

func doMerges(c Config, aggregateExplicit bool) error {
	// For each command that specifies CSV merge, we find all the relevant files
	var wg sync.WaitGroup
	cmds := make([]Command, 0)
	if aggregateExplicit {
		cmds = c.Commands
	} else {
		cmds = commandsExecuting
	}
	for _, v := range cmds {
		if v.Merge == MergeFuncCSV {
			//sourceFile := doNameReplacements(v.FileName)
			sourceFile := strings.Replace(v.FileName, "$time$", "", 1)
			//sourceFile = strings.TrimSuffix(sourceFile, filepath.Ext(sourceFile))
			destinationFile := fmt.Sprintf("aggregated\\%s.csv", v.ID)
			wg.Add(1)
			go doCSVMerge(sourceFile, destinationFile, &wg, v.AddHostname)
		} else if v.Merge == MergeFuncNone {

		} else if v.Merge == MergeFuncPool {
			wg.Add(1)
			go doPoolMerge(strings.Replace(v.FileName, "$time$", "", 1), "aggregated", &wg, v.AddHostname)
		} else if v.Merge == MergeFuncAsymcCSV {
			sourceFile := strings.Replace(v.FileName, "$time$", "", 1)
			destinationFile := fmt.Sprintf("aggregated\\%s.csv", v.ID)
			wg.Add(1)
			go asymmetricCSVMerge(sourceFile, destinationFile, &wg, v.AddHostname)
		}
	}
	wg.Wait()
	return nil
}

func doPoolMerge(sourceFile, destinationDir string, wg *sync.WaitGroup, addhostname bool) {
	defer wg.Done()
	files, err := findFilesByName("devices", sourceFile, true)
	if err != nil {
		log.Printf("error finding files: %v (%s)", err, sourceFile)
		return
	}
	if len(files) == 0 {
		log.Printf("no files found for %s", sourceFile)
		return
	}
	devicePattern := regexp.MustCompile(`devices\\([\w\-\.]+)\\`)
	for _, v := range files {
		fileName := filepath.Base(v)
		if addhostname {
			matches := devicePattern.FindStringSubmatch(v)
			if len(matches) > 1 {
				fileName = fmt.Sprintf("%s_%s", matches[1], fileName)
			} else {
				log.Printf("error parsing device regex for file %s", v)
				continue
			}
		}
		destPath := fmt.Sprintf("%s\\%s", destinationDir, fileName)
		err = moveFile(v, destPath)
		if err != nil {
			log.Printf("error moving file %s to %s: %v", fileName, destPath, err)
			continue
		}

	}
}

// doCSVMerge combines symmetric CSV files into a single file - it expects that the files being merged have the same columns
func doCSVMerge(sourceFile, destinationFile string, wg *sync.WaitGroup, addhostname bool) {
	defer wg.Done()
	// Find all files with sourceFile name in devices directory
	firstFileDone := false
	files, err := findFilesByName("devices", sourceFile, true)
	if err != nil {
		log.Printf("error finding files: %v (%s)", err, sourceFile)
		return
	}
	if len(files) == 0 {
		log.Printf("no files found for %s", sourceFile)
		return
	}
	w, fw, err := createCSVWriter(destinationFile)
	if err != nil {
		log.Printf("error creating CSV writer: %v (%s)", err, destinationFile)
		return
	}
	defer fw.Close()
	defer w.Flush()
	deviceRegex := regexp.MustCompile(`devices\\([^\\]+)`)
	headers := make([]string, 0)
	for _, v := range files {
		currentHost := ""
		matches := deviceRegex.FindStringSubmatch(v)
		if len(matches) > 1 {
			currentHost = matches[1]
		}
		//currentHost := getLastPathElement(v)
		headerDone := false

		func() {
			var r *csv.Reader
			var fr *os.File
			r, fr, err = createCSVReader(v)
			if err != nil {
				log.Printf("error creating CSV reader: %v (%s)", err, v)
				return
			}
			defer fr.Close()
			if !firstFileDone {
				record_count := 0
				for {
					record, err := r.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						log.Printf("error reading header: %v (%s)", err, v)
						break
					}
					if addhostname && !headerDone {
						record = append([]string{"PSComputerName"}, record...)
					} else if addhostname {
						record = append([]string{currentHost}, record...)
					}

					if err := w.Write(record); err != nil {
						log.Printf("error writing record: %v (%s)", err, v)
						break
					}
					if !headerDone {
						headers = record
					}
					headerDone = true
					record_count += 1
				}
				// In case the file has no records or we can't read it for some reason, this one won't count
				if record_count != 0 {
					firstFileDone = true
				}
			} else {
				// Skip the header
				headerRow, err := r.Read()
				if err == io.EOF {
					return
				}
				if err != nil {
					log.Printf("error reading header: %v (%s)", err, v)
					return
				}
				if addhostname {
					headerRow = append([]string{"PSComputerName"}, headerRow...)
				}

				if !slices.Equal(headerRow, headers) {
					log.Printf("error: headers do not match for %s", v)
					return
				}

				for {
					record, err := r.Read()
					if err == io.EOF {
						break
					}
					if err != nil {
						log.Printf("error reading header: %v (%s)", err, v)
						break
					}
					if addhostname {
						record = append([]string{currentHost}, record...)
					}
					if err := w.Write(record); err != nil {
						log.Printf("error writing record: %v (%s)", err, v)
						break
					}
				}
			}
		}()
	}
}

// asymmetricCSVMerge combines CSV files that have varying headers into a single output file - useful for iterating over a complex tool output
// utilities/CSVMerge.ps1 also achieves this across an entire directory
func asymmetricCSVMerge(sourceFile, destinationFile string, wg *sync.WaitGroup, addhostname bool) {
	// We will not read all headers first - instead we will maintain header slice as we parse each file
	// If header exists in slice, we build record, if not, we add it and build
	defer wg.Done()
	// Find all files with sourceFile name in devices directory
	files, err := findFilesByName("devices", sourceFile, true)
	if err != nil {
		log.Printf("error finding files: %v (%s)", err, sourceFile)
		return
	}
	if len(files) == 0 {
		log.Printf("no files found for %s", sourceFile)
		return
	}
	w, fw, err := createCSVWriter(destinationFile)
	if err != nil {
		log.Printf("error creating CSV writer: %v (%s)", err, destinationFile)
		return
	}
	defer fw.Close()
	defer w.Flush()
	deviceRegex := regexp.MustCompile(`devices\\([^\\]+)`)
	// Iterate once to build header structure
	headers := make([]string, 0)
	if addhostname {
		headers = append(headers, "PSComputerName")
	}
	for _, v := range files {
		func() {
			var r *csv.Reader
			var fr *os.File
			r, fr, err = createCSVReader(v)
			if err != nil {
				log.Printf("error creating CSV reader: %v (%s)", err, v)
				return
			}
			defer fr.Close()

			headerRow, err := r.Read()
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("error reading header: %v (%s)", err, v)
				return
			}
			for _, val := range headerRow {
				if !slices.Contains(headers, val) {
					headers = append(headers, val)
				}
			}
		}()
	}
	w.Write(headers)

	// Map current headers to their corresponding index in mainHeaders
	headerMap := make(map[string]int)
	for i, header := range headers {
		headerMap[header] = i
	}
	// Iterate again to build records
	for _, v := range files {
		currentHost := ""
		if addhostname {
			matches := deviceRegex.FindStringSubmatch(v)
			if len(matches) > 1 {
				currentHost = matches[1]
			}
		}
		func() {
			headerDone := false
			headerRow := make([]string, 0)
			var r *csv.Reader
			var fr *os.File
			r, fr, err = createCSVReader(v)
			if err != nil {
				log.Printf("error creating CSV reader: %v (%s)", err, v)
				return
			}
			defer fr.Close()

			for {
				if !headerDone {
					headerRow, err = r.Read()
					if err == io.EOF {
						return
					}
					if err != nil {
						log.Printf("error reading header: %v (%s)", err, v)
						return
					}
					headerDone = true
				}
				record, err := r.Read()
				if err == io.EOF {
					break
				}
				if err != nil {
					log.Printf("error reading record: %v (%s)", err, v)
					break
				}
				record = buildRecordSlice(headers, headerRow, record, v, headerMap)
				if addhostname {
					record = append([]string{currentHost}, record...)
				}
				if err := w.Write(record); err != nil {
					log.Printf("error writing record: %v (%s)", err, v)
					break
				}
			}

		}()
	}

}

// findFilesByName searches for files with the specified name in the given directory and its subdirectories.
// Does not check extension
func findFilesByName(root string, filename string, suffixMatch bool) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, info os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if suffixMatch {
			// Check if the file name ends with the specified filename
			if strings.HasSuffix(info.Name(), filename) {
				matches = append(matches, path)
			}
			return nil
		} else if info.Name() == filename {
			matches = append(matches, path)
			return nil
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return matches, nil
}
