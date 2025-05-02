package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type MergeFuncs string

var validMergeFuncs = []MergeFuncs{MergeFuncCSV, MergeFuncNone, MergeFuncPool}

const (
	MergeFuncCSV  MergeFuncs = "csv"
	MergeFuncNone MergeFuncs = "none"
	MergeFuncPool MergeFuncs = "pool"
)

func doMerges(c Config) error {
	// For each command that specifies CSV merge, we find all the relevant files
	var wg sync.WaitGroup
	for _, v := range c.Commands {
		if v.Merge == MergeFuncCSV {
			//sourceFile := doFileNameReplacements(v.FileName)
			sourceFile := strings.Replace(v.FileName, "$time$", "", 1)
			//sourceFile = strings.TrimSuffix(sourceFile, filepath.Ext(sourceFile))
			destinationFile := fmt.Sprintf("aggregated\\%s.csv", v.ID)
			wg.Add(1)
			go doCSVMerge(sourceFile, destinationFile, &wg, v.AddHostname)
		} else if v.Merge == MergeFuncNone {

		} else if v.Merge == MergeFuncPool {
			wg.Add(1)
			go doPoolMerge(strings.Replace(v.FileName, "$time$", "", 1), "aggregated", &wg, v.AddHostname)
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
	for _, v := range files {
		// TODO - Regex to extract ...devices\<hostname>\ instead of assuming we are always at the base
		currentHost := getLastPathElement(v)
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
					headerDone = true
					record_count += 1
				}
				// In case the file has no records or we can't read it for some reason, this one won't count
				if record_count != 0 {
					firstFileDone = true
				}
			} else {
				// Skip the header
				_, err := r.Read()
				if err == io.EOF {
					return
				}
				if err != nil {
					log.Printf("error reading header: %v (%s)", err, v)
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
