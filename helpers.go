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
	"runtime"
	"slices"
	"strconv"
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

// buildBatchScript creates a batch script based on the commands in the config and file/dir availability if dependencies are detected
func buildBatchScript(c Config, nodownload bool, tagArgs []string, daysback int) (string, error) {
	batTarget := "omni_batch.bat"
	f, err := os.Create(batTarget)
	if err != nil {
		return "", err
	}
	defer f.Close()
	cmdCount := 0
	for _, v := range c.Commands {
		tagMatch := false
		if tagArgs[0] != "*" {
			for _, t := range v.Tags {
				if slices.Contains(tagArgs, strings.ToLower(t)) {
					tagMatch = true
					break
				}
			}
		} else {
			tagMatch = true
		}
		if !tagMatch {
			continue
		}

		cmd := v.Command
		tempFilesToCopy := make([]string, 0)
		tempDirsToCopy := make([]string, 0)
		anyError := false
		for _, j := range v.Dependencies {
			tmpFile := strings.TrimSpace(j)
			if strings.HasPrefix(tmpFile, "http") {
				// Download the file via HTTP/HTTPS depending
				destFileName := filepath.Base(tmpFile)
				if _, err = os.Stat(destFileName); errors.Is(err, os.ErrNotExist) {
					if nodownload {
						log.Printf("file %s does not exist and skipping download", destFileName)
						anyError = true
						break
					}
					log.Printf("downloading file %s to %s", tmpFile, destFileName)
					_, err = downloadFile(tmpFile, destFileName)
					if err != nil {
						log.Printf("Error downloading file %s to %s: %v", tmpFile, destFileName, err)
						anyError = true
						break
					}
					tempFilesToCopy = append(tempFilesToCopy, destFileName)
				} else {
					log.Printf("file %s already exists, skipping download", destFileName)
					tempFilesToCopy = append(tempFilesToCopy, destFileName)
				}
			} else {
				// Local Reference, verify it exists and determine if it's a file or directory
				st, err := os.Stat(tmpFile)
				if err != nil {
					log.Printf("error checking path exists for id %s: %s, %v", v.ID, tmpFile, err)
					anyError = true
					break
				} else {
					log.Printf("found path %s", tmpFile)
					if st.IsDir() {
						tempDirsToCopy = append(tempDirsToCopy, tmpFile)
					} else {
						tempFilesToCopy = append(tempFilesToCopy, tmpFile)
					}
				}
			}
		}
		if !anyError {
			for _, q := range tempFilesToCopy {
				filesToCopy = append(filesToCopy, q)
			}
			for _, q := range tempDirsToCopy {
				dirsToCopy = append(dirsToCopy, q)
			}
		} else {
			log.Printf("Skipping command %s due to dependency errors", v.ID)
			continue
		}

		fileName := doNameReplacements(v.FileName)
		dirName := doNameReplacements(v.DirName)
		if v.FileName != "" {
			collectionFiles = append(collectionFiles, fileName)
		}
		if v.DirName != "" {
			collectionDirs = append(collectionDirs, dirName)
		}
		cmd = doCmdReplacements(cmd, fileName, v.SkipDir, dirName, daysback)
		commandsExecuting = append(commandsExecuting, v)
		_, err := f.WriteString(fmt.Sprintf("start /b /wait cmd.exe /c %s\n", cmd))
		if err != nil {
			return "", err
		}
		cmdCount += 1
	}
	if cmdCount == 0 {
		return "", fmt.Errorf("no commands to execute")
	}
	f.WriteString(fmt.Sprintf("cmd.exe /c echo 1 > C:\\Windows\\temp\\%s\n", signalFile))
	return batTarget, nil
}

// doNameReplacements replaces the placeholders in file names with actual values
func doNameReplacements(name string) string {
	name = strings.ReplaceAll(name, "$time$", currentTime)
	return name
}

// doCmdReplacements replaces the placeholders in commands with actual values
func doCmdReplacements(cmd string, filename string, skipdir bool, dirname string, daysback int) string {
	if skipdir {
		cmd = strings.ReplaceAll(cmd, "$FILENAME$", fmt.Sprintf("%s", filename))
	} else {
		cmd = strings.ReplaceAll(cmd, "$FILENAME$", fmt.Sprintf("C:\\Windows\\temp\\%s", filename))
	}
	cmd = strings.ReplaceAll(cmd, "$DIRNAME$", dirname)
	cmd = strings.ReplaceAll(cmd, "$DAYSBACK$", strconv.Itoa(daysback))
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
		return err
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
		if v.FileName == "" && v.DirName == "" {
			return fmt.Errorf("id: %s, must specify one of file_name/dir_name", v.ID)
		}
		if !strings.Contains(v.Command, "$FILENAME$") && !strings.Contains(v.Command, "$DIRNAME$") {
			return fmt.Errorf("id: %s, command must contain $FILENAME$ or $DIRNAME$ placeholder", v.ID)
		}
		if strings.Contains(v.FileName, " ") {
			return fmt.Errorf("id: %s, file name cannot contain spaces", v.ID)
		}
		if strings.Contains(v.DirName, " ") {
			return fmt.Errorf("id: %s, dir name cannot contain spaces", v.ID)
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
`
	fmt.Println(s)
	fmt.Println("	omni - rapid network-wide evidence collection")
	fmt.Println("	github.com/joeavanzato/omni")
	fmt.Println("")
}

// getLastPathElement returns the last element of a path
func getLastPathElement(path string) string {
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, "/", "\\")
	}
	dir := filepath.Dir(path)
	return filepath.Base(dir)
}

// moveFile moves a file from source to destination using copy/remove
func moveFile(source, destination string) error {
	err := copyFile(source, destination)
	if err != nil {
		return err
	}
	err = os.Remove(source)
	if err != nil {
		return err
	}
	return nil
}

func buildRecordSlice(mainHeaders []string, currentHeaders []string, row []string, filename string, headerMap map[string]int) []string {
	record := make([]string, len(mainHeaders))
	for i := range record {
		record[i] = ""
	}
	for i, header := range currentHeaders {
		if index, ok := headerMap[header]; ok {
			record[index] = row[i]
		} else {
			log.Printf("header %s not found in main headers for file %v", header, filename)
		}
	}
	return record
}
