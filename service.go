package main

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

func runService(target, binPath, name string) error {
	createCmd := exec.Command("sc",
		fmt.Sprintf("\\\\%s", target),
		"create", name,
		"binPath=", binPath,
		"DisplayName=", name,
		"start=", "demand",
		"type=", "own",
		"obj=", "LocalSystem")
	createOutput, err := createCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create service: %w, output: %s", err, string(createOutput))
	}
	startCmd := exec.Command("sc",
		fmt.Sprintf("\\\\%s", target),
		"start", name)
	startOutput, err := startCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start service: %w, output: %s", err, string(startOutput))
	}
	return nil
}

func deleteService(target, name string) error {
	stopCmd := exec.Command("sc", strings.Split(target+"stop \""+name+"\"", " ")...)
	_, err := stopCmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to stop service: %s, output: %s", target, err)
	}
	time.Sleep(1 * time.Second)
	deleteCmd := exec.Command("sc", strings.Split(target+"delete \""+name+"\"", " ")...)
	deleteOutput, err := deleteCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete service: %w, output: %s", err, string(deleteOutput))
	}
	return nil
}
