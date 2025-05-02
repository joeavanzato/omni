package main

import (
	"os/exec"
	"time"
)

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

func deleteTask(target, taskName string) error {
	deleteCmd := exec.Command("schtasks.exe", "/Delete", "/TN", taskName, "/F", "/S", target)
	_, deleteErr := deleteCmd.CombinedOutput()
	if deleteErr != nil {
		return deleteErr
	}
	return nil
}
