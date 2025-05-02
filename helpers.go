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
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var filesToCopy = []string{}

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
func buildBatchScript(c Config) (string, error) {
	batTarget := "omni_batch.bat"
	f, err := os.Create(batTarget)
	if err != nil {
		return "", err
	}
	defer f.Close()
	re := regexp.MustCompile(`file=([^|]+)`)
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
			if strings.HasPrefix(file, "http") {
				// Download the file via HTTP/HTTPS depending
				destFileName := filepath.Base(file)
				if _, err = os.Stat(destFileName); errors.Is(err, os.ErrNotExist) {
					log.Printf("downloading file %s to %s", file, destFileName)
					_, err = downloadFile(file, destFileName)
					if err != nil {
						log.Printf("Error downloading file %s to %s: %v", file, destFileName, err)
						continue
					}
					filesToCopy = append(filesToCopy, destFileName)
				} else {
					log.Printf("file %s already exists, skipping download", destFileName)
					filesToCopy = append(filesToCopy, destFileName)
				}
			} else {
				// It's a local file - verify it exists
				_, err = os.Stat(file)
				if err != nil {
					log.Printf("error checking file exists for id %s: %s, %v", v.ID, file, err)
					continue
				} else {
					log.Printf("found file %s", file)
					filesToCopy = append(filesToCopy, file)
				}
			}
		}

		fileName := doFileNameReplacements(v.FileName)
		collectionFiles = append(collectionFiles, fileName)
		cmd = doCmdReplacements(cmd, fileName)
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
func doCmdReplacements(cmd string, filename string) string {
	cmd = strings.ReplaceAll(cmd, "$FILENAME$", fmt.Sprintf("C:\\Windows\\temp\\%s", filename))
	return cmd
}

// copyFile copies a file from src to dst and returns the number of bytes copied
func copyFile(src, dst string) (int64, error) {
	source, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destination.Close()
	nBytes, err := io.Copy(destination, source)
	return nBytes, err
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

// validateConfig checks to make sure the supplied configuration file has no obvious issues
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
	}
	return nil
}

func createCSVWriter(filename string) (*csv.Writer, *os.File, error) {
	f, err := os.Create(filename)
	if err != nil {
		return nil, nil, err
	}
	writer := csv.NewWriter(f)
	return writer, f, nil
}

func createCSVReader(filename string) (*csv.Reader, *os.File, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, nil, err
	}
	reader := csv.NewReader(f)
	reader.LazyQuotes = true
	return reader, f, nil
}

func downloadFile(url, destFileName string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Go-http-client/1.1")

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
