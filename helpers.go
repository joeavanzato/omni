package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var filesToCopy = []string{}
var dirsToCopy = []string{}

// parseConfig reads the YAML config file and unmarshalls into a Config struct
func parseConfig(configFile string) (Config, error) {
	var c Config
	f, err := os.ReadFile(configFile)
	if err != nil {
		return c, err
	}
	if err = yaml.Unmarshal(f, &c); err != nil {
		return c, err
	}
	return c, nil
}

// buildBatchScript creates a batch script based on the commands in the config
func buildBatchScript(c Config, nodownload bool) (string, error) {
	batTarget := "omni_batch.bat"
	f, err := os.Create(batTarget)
	if err != nil {
		return "", err
	}
	defer f.Close()
	re := regexp.MustCompile(`file=([^|]+)`)
	dirComponent := regexp.MustCompile(`dir=([^|]+)`)
	cmdAfterFileRegex := regexp.MustCompile(`\|(.+)`)
	for _, v := range c.Commands {
		cmd := v.Command
		if strings.HasPrefix(cmd, "file=") {
			// We are dealing with a file copy inclusion
			// Is it a network path or local file?
			match := re.FindStringSubmatch(cmd)
			file := ""
			if len(match) > 1 {
				file = match[1]
			} else {
				log.Printf("error parsing file copy command, missing pipe?: %s", cmd)
				continue
			}
			match = cmdAfterFileRegex.FindStringSubmatch(cmd)
			if len(match) > 1 {
				cmd = strings.TrimSpace(match[1])
			} else {
				log.Printf("error parsing followup command, missing pipe?: %s", cmd)
				continue
			}
			file = strings.TrimSpace(file)
			files := strings.Split(file, ",")
			tempFilesToCopy := make([]string, 0)
			anyError := false
			for _, j := range files {
				if anyError {
					break
				}
				tmpFile := strings.TrimSpace(j)
				if strings.HasPrefix(tmpFile, "http") {
					// Download the file via HTTP/HTTPS depending
					destFileName := filepath.Base(tmpFile)
					if _, err = os.Stat(destFileName); errors.Is(err, os.ErrNotExist) {
						if nodownload {
							log.Printf("file %s does not exist and skipping download", destFileName)
							anyError = true
							continue
						}
						log.Printf("downloading file %s to %s", file, destFileName)
						_, err = downloadFile(file, destFileName)
						if err != nil {
							log.Printf("Error downloading file %s to %s: %v", file, destFileName, err)
							anyError = true
							continue
						}
						tempFilesToCopy = append(tempFilesToCopy, destFileName)
					} else {
						log.Printf("file %s already exists, skipping download", destFileName)
						tempFilesToCopy = append(tempFilesToCopy, destFileName)
					}
				} else {
					// It's a local file - verify it exists
					_, err = os.Stat(tmpFile)
					if err != nil {
						log.Printf("error checking file exists for id %s: %s, %v", v.ID, file, err)
						anyError = true
						continue
					} else {
						log.Printf("found file %s", tmpFile)
						tempFilesToCopy = append(tempFilesToCopy, tmpFile)
					}
				}
			}

			if !anyError {
				for _, q := range tempFilesToCopy {
					filesToCopy = append(filesToCopy, q)
				}
			}
		} else if strings.HasPrefix(cmd, "dir=") {
			// If dir exists, copy it to C:\Windows\temp on remote
			//
			match := dirComponent.FindStringSubmatch(cmd)
			dir := ""
			if len(match) > 1 {
				dir = match[1]
			} else {
				log.Printf("error parsing dir copy command, missing pipe?: %s", cmd)
				continue
			}
			dir = strings.TrimSpace(dir)
			match = cmdAfterFileRegex.FindStringSubmatch(cmd)
			if len(match) > 1 {
				cmd = strings.TrimSpace(match[1])
			} else {
				log.Printf("error parsing followup command, missing pipe?: %s", cmd)
				continue
			}
			if _, err = os.Stat(dir); errors.Is(err, os.ErrNotExist) {
				log.Printf("specified dir does not exist for id %s: %s, %v", v.ID, dir, err)
				continue
			} else {
				log.Printf("dir %s exists, adding to copy list", dir)
				if !slices.Contains(dirsToCopy, dir) {
					dirsToCopy = append(dirsToCopy, dir)
				}
			}
		}

		fileName := doFileNameReplacements(v.FileName)
		collectionFiles = append(collectionFiles, fileName)
		cmd = doCmdReplacements(cmd, fileName, v.SkipDir)
		_, err := f.WriteString(fmt.Sprintf("start /b /wait cmd.exe /c %s\n", cmd))
		if err != nil {
			return "", err
		}
	}
	f.WriteString(fmt.Sprintf("cmd.exe /c echo 1 > C:\\Windows\\temp\\%s\n", signalFile))
	return batTarget, nil
}

// doFileNameReplacements replaces the placeholders in file names with actual values
func doFileNameReplacements(fileName string) string {
	fileName = strings.ReplaceAll(fileName, "$time$", currentTime)
	return fileName
}

// doCmdReplacements replaces the placeholders in commands with actual values
func doCmdReplacements(cmd string, filename string, skipdir bool) string {
	if skipdir {
		cmd = strings.ReplaceAll(cmd, "$FILENAME$", fmt.Sprintf("%s", filename))
	} else {
		cmd = strings.ReplaceAll(cmd, "$FILENAME$", fmt.Sprintf("C:\\Windows\\temp\\%s", filename))
	}
	return cmd
}

// copyFile copies a file from src to dst and returns the number of bytes copied
func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}

// CopyDirectory recursively copies a source directory to a destination directory
// maintaining the original file structure
func copyDirectory(srcDir, dstDir string) error {
	// Create the destination directory with the same permissions
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("error creating destination directory: %w", err)
	}

	// Get a list of all entries in the source directory
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("error reading source directory: %w", err)
	}

	// Iterate through all entries
	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(dstDir, entry.Name())

		// If the entry is a directory, recursively copy it
		if entry.IsDir() {
			if err = copyDirectory(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Otherwise copy the file
			if err = copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// deduplicateSlice removes duplicates from a slice of strings as well as removing empty elements
func deduplicateSlice(s []string) []string {
	seen := make(map[string]struct{})
	result := []string{}
	for _, item := range s {
		if strings.TrimSpace(item) == "" {
			continue
		}
		if _, ok := seen[item]; !ok {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

// exportSliceToCSV exports a slice of string slices to a CSV file along with a set of headers and destination file
func exportSliceToCSV(headers []string, data [][]string, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	writer := csv.NewWriter(file)
	defer writer.Flush()
	header := headers
	if err = writer.Write(header); err != nil {
		return err
	}
	for _, v := range data {
		if err = writer.Write(v); err != nil {
			return err
		}
	}
	return nil
}

// validateConfig checks to make sure the supplied configuration file has no obvious issues - logical or otherwise
func validateConfig(c Config) error {
	ids := make([]string, 0)
	for _, v := range c.Commands {
		if v.ID == "" {
			return fmt.Errorf("id cannot be empty")
		}
		if !slices.Contains(ids, v.ID) {
			ids = append(ids, v.ID)
		} else {
			return fmt.Errorf("duplicate id: %s", v.ID)
		}
		if !slices.Contains(validMergeFuncs, v.Merge) {
			return fmt.Errorf("id: %s, invalid merge function: %s", v.ID, v.Merge)
		}
		if v.FileName == "" {
			return fmt.Errorf("id: %s, file name cannot be empty", v.ID)
		}
		if !strings.Contains(v.Command, "$FILENAME$") {
			return fmt.Errorf("id: %s, command must contain $FILENAME$ placeholder", v.ID)
		}
		if strings.Contains(v.FileName, " ") {
			return fmt.Errorf("id: %s, file name cannot contain spaces", v.ID)
		}
	}
	return nil
}

// createCSVWriter creates a CSV writer for the specified file
func createCSVWriter(filename string) (*csv.Writer, *os.File, error) {
	f, err := os.Create(filename)
	if err != nil {
		return nil, nil, err
	}
	writer := csv.NewWriter(f)
	return writer, f, nil
}

// createCSVReader creates a CSV reader for the specified file
func createCSVReader(filename string) (*csv.Reader, *os.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	return reader, f, nil
}

// downloadFile downloads a single file from the specified URL and saves it to the specified destination
func downloadFile(url, destFileName string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("bad status: %s", resp.Status)
	}

	out, err := os.Create(destFileName)
	if err != nil {
		return 0, fmt.Errorf("failed to create output file: %w", err)
	}
	defer out.Close()

	bytesWritten, err := io.Copy(out, resp.Body)
	if err != nil {
		return bytesWritten, fmt.Errorf("failed to write to file: %w", err)
	}

	return bytesWritten, nil
}

// executeCommand executes a command locally via temporary batch file and returns any errors
func executeCommand(cmd string) error {
	// Write it to a batch file, invoke, then delete
	batchFile := "tmp.bat"
	f, err := os.Create(batchFile)
	if err != nil {
		return fmt.Errorf("error creating batch file: %w", err)
	}
	_, err = f.WriteString(fmt.Sprintf("cmd.exe /c %s\n", cmd))
	f.Close()
	command := exec.Command("cmd.exe", "/c", batchFile)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error executing command: %s, output: %s", err, string(output))
	}
	return nil
}

func printLogo() {
	s := `
	 ▄▄▄  ▄▄▄▄  ▄▄▄▄  ▄ 
	█   █ █ █ █ █   █ ▄ 
	▀▄▄▄▀ █   █ █   █ █ 
					  █ 
						
						
						`
	fmt.Println(s)
	fmt.Println("	omni - rapid network-wide evidence collection")
	fmt.Println("	github.com/joeavanzato/omni")
	fmt.Println("")
}
