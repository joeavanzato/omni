package main

import (
	"os/exec"
	"time"
)

// I decided to use schtasks here instead of anything else such as RPC over Named Pipes or COM/OLE (https://github.com/joeavanzato/goexec/blob/main/task.go)
// due to the simplicity and the assumption that we are always running this type of software as an admin already authenticated on the network

// runTask creates a scheduled task on the target machine and runs it immediately
func runTask(target, command, taskName string) error {
	//remoteCommand := "cmd.exe /c C:\\temp\\test.bat"
	createCmd := exec.Command("schtasks.exe",
		"/Create",
		"/TN", taskName,
		"/TR", command,
		"/SC", "ONCE",
		"/ST", time.Now().Add(2*time.Hour).Format("15:04"),
		"/RU", "SYSTEM",
		"/RL", "HIGHEST",
		"/F",
		"/S", target)

	_, err := createCmd.CombinedOutput()
	if err != nil {
		return err
	}
	runCmd := exec.Command("schtasks.exe", "/Run", "/TN", taskName, "/S", target)
	_, runErr := runCmd.CombinedOutput()
	if runErr != nil {
		return runErr
	}
	return nil
}

func stopTask(target, taskName string) {
	stopCmd := exec.Command("schtasks.exe", "/End", "/TN", taskName, "/S", target)
	_, stopErr := stopCmd.CombinedOutput()
	if stopErr != nil {
		return
	}
	return
}

func deleteTask(target, taskName string) error {
	// Attempt to stop before deleting
	stopTask(target, taskName)
	deleteCmd := exec.Command("schtasks.exe", "/Delete", "/TN", taskName, "/F", "/S", target)
	_, deleteErr := deleteCmd.CombinedOutput()
	if deleteErr != nil {
		return deleteErr
	}
	return nil
}
