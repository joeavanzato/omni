package main

import (
	"fmt"
	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

var wmiReturnValues = map[int]string{
	0:  "Success",
	2:  "Access denied",
	3:  "Insufficient privilege",
	8:  "Unknown failure",
	9:  "Path not found",
	21: "Invalid parameter",
}

func executeRemoteWMI(remoteHost, command, dir, username, password, domain string) error {
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		return fmt.Errorf("failed to initialize COM: %v", err)
	}
	defer ole.CoUninitialize()

	// Create WbemScripting.SWbemLocator
	unknown, err := oleutil.CreateObject("WbemScripting.SWbemLocator")
	if err != nil {
		return fmt.Errorf("failed to create SWbemLocator: %v", err)
	}
	defer unknown.Release()

	wmiLocator, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("failed to get IDispatch for locator: %v", err)
	}
	defer wmiLocator.Release()

	// Connect to remote WMI
	var serviceRaw *ole.VARIANT
	if username == "" {
		serviceRaw, err = oleutil.CallMethod(wmiLocator, "ConnectServer", remoteHost, "root\\cimv2")
	} else {
		if domain == "" {
			username = fmt.Sprintf(".\\%s", username)
		} else {
			username = fmt.Sprintf("%s\\%s", domain, username)
		}
		serviceRaw, err = oleutil.CallMethod(
			wmiLocator,
			"ConnectServer",
			remoteHost,    // strServer
			"root\\cimv2", // strNamespace
			username,      // strUser
			password,      // strPassword
			"MS_409",      // strLocale
			"",            // strAuthority
			0,             // iSecurityFlags
			nil,           // objwbemNamedValueSet
		)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to remote host %s: %v", remoteHost, err)
	}
	service := serviceRaw.ToIDispatch()
	defer service.Release()

	// Get Win32_Process class
	processClassRaw, err := oleutil.CallMethod(service, "Get", "Win32_Process")
	if err != nil {
		return fmt.Errorf("failed to get Win32_Process: %v", err)
	}
	processClass := processClassRaw.ToIDispatch()
	defer processClass.Release()

	// Get Win32_ProcessStartup instance (optional, we can omit this too)
	startupClassRaw, err := oleutil.CallMethod(service, "Get", "Win32_ProcessStartup")
	if err != nil {
		return fmt.Errorf("failed to get Win32_ProcessStartup: %v", err)
	}
	startupClass := startupClassRaw.ToIDispatch()
	defer startupClass.Release()

	startupInstanceRaw, err := oleutil.CallMethod(startupClass, "SpawnInstance_")
	if err != nil {
		return fmt.Errorf("failed to spawn startup instance: %v", err)
	}
	startupInstance := startupInstanceRaw.ToIDispatch()
	defer startupInstance.Release()

	// Set ShowWindow = 0 (hidden)
	_, err = oleutil.PutProperty(startupInstance, "ShowWindow", 0)
	if err != nil {
		return fmt.Errorf("failed to set ShowWindow: %v", err)
	}

	// Create an uninitialized variant to receive the ProcessId (by reference)
	var pid ole.VARIANT
	ole.VariantInit(&pid)

	result, err := oleutil.CallMethod(
		processClass,
		"Create",
		command,         // CommandLine
		dir,             // CurrentDirectory (cannot be blank/nil)
		startupInstance, // ProcessStartupInformation
		&pid,            // Out: ProcessId
	)
	if err != nil {
		return fmt.Errorf("failed to execute Create: %v", err)
	}

	returnCode := result.Val
	if returnCode != 0 {
		if message, ok := wmiReturnValues[int(returnCode)]; ok {
			return fmt.Errorf("WMI process creation failed (code %d): %s", returnCode, message)
		}
		return fmt.Errorf("WMI process creation failed, return code: %d", returnCode)
	}

	return nil
}
