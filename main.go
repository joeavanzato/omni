package main

// Command-line software designed to execute commands on remote-devices for light-weight threat-hunting and collection purposes
// Reads a configuration file containing 1 or more commands, dynamically updates their outputs and executes via PowerShell
// Commands are designed to send their output to a file
// Each host gets 1 or more goroutines and establishes a single SMB connection to the device and waits for the final signal file to be completed
// Then we collect all the outputs - which are ideally CSV or JSON - and merge them together when all hosts are complete
// In this way, we produce a series of CSVs for analysts to peruse for low-maturity environments

// We will actually dynamically build a small batch-file in memory and copy this to each host and execute
// This batch file will contain the commands to be executed

func main() {

}
