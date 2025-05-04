package main

import (
	"fmt"
	"github.com/go-ldap/ldap/v3"
	"github.com/go-ldap/ldap/v3/gssapi"
	"log"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

func LDAPBind() (*ldap.Conn, *gssapi.SSPIClient, string, error) {
	// TODO - We could set this up to support username/password bind and explicitly specifying a Domain Controller to bind to
	// Seems somewhat unnecessary right this minute since the idea is you would run this on a domain-joined machine as a user with correct permissions
	sspiClient, err := gssapi.NewSSPIClient()
	if err != nil {
		return nil, nil, "", err
	}

	ldapDomain, err := getCurrentDomain()
	if err != nil {
		return nil, nil, "", err
	}

	logonServer, err := getLogonServer()
	if err != nil {
		return nil, nil, "", err
	}

	log.Printf("Using LDAP Domain: %s", ldapDomain)
	l, err := ldap.DialURL(fmt.Sprintf("ldap://%s:389", ldapDomain))
	if err != nil {
		return nil, nil, "", err
	}

	ldapBindServer := fmt.Sprintf("ldap/%s.%s", logonServer, ldapDomain)
	log.Printf("Using Domain Controller: %s", logonServer)

	err = l.GSSAPIBind(sspiClient, ldapBindServer, "")
	if err != nil {
		return nil, nil, "", err
	}
	return l, sspiClient, ldapDomain, nil
}

func getLogonServer() (string, error) {
	logonServer := os.Getenv("LOGONSERVER")
	if logonServer == "" {
		return "", fmt.Errorf("$LOGONSERVER environment variable is not set")
	}
	parts := strings.Split(logonServer, "\\\\")
	domainName := parts[1]
	return domainName, nil
}

func getCurrentDomain() (string, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	GetComputerNameExW := kernel32.NewProc("GetComputerNameExW")

	// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/ne-sysinfoapi-computer_name_format
	const ComputerNameDnsDomain = 2

	// https://learn.microsoft.com/en-us/windows/win32/api/sysinfoapi/nf-sysinfoapi-getcomputernameexw
	// First call to get the required buffer size
	var bufferSize uint32
	ret, _, err := GetComputerNameExW.Call(
		uintptr(ComputerNameDnsDomain),
		uintptr(0),
		uintptr(unsafe.Pointer(&bufferSize)),
	)
	buffer := make([]uint16, bufferSize)

	// Second call to get the actual domain name
	ret, _, err = GetComputerNameExW.Call(
		uintptr(ComputerNameDnsDomain),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&bufferSize)),
	)
	if ret == 0 {
		return "", fmt.Errorf("failed to get domain name: %s", err.Error())
	}

	domainName := syscall.UTF16ToString(buffer)
	return domainName, nil
}

func getAllEnabledComputerDevices() ([]string, error) {
	ldapConnection, sspiClient, domain, err := LDAPBind()
	if err != nil {
		log.Fatalf("Error binding to LDAP: %v", err)
	}
	defer ldapConnection.Close()
	defer sspiClient.Close()
	domainComponents := strings.Split(domain, ".")
	dnString := ""
	for i, v := range domainComponents {
		if i == len(domainComponents)-1 {
			dnString += fmt.Sprintf("dc=%s", v)
		} else {
			dnString += fmt.Sprintf("dc=%s,", v)
		}
	}

	// TODO - Filter out Linux machines
	searchRequest := ldap.NewSearchRequest(
		dnString, // The base dn to search
		ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false,
		"(&(objectClass=computer)(!userAccountControl:1.2.840.113556.1.4.803:=2))",
		[]string{"dNSHostName"}, // A list attributes to retrieve
		nil,
	)

	sr, err := ldapConnection.Search(searchRequest)
	if err != nil {
		log.Fatal(err)
	}

	enabledComputers := make([]string, 0)
	for _, entry := range sr.Entries {
		enabledComputers = append(enabledComputers, entry.GetAttributeValue("dNSHostName"))
	}

	return enabledComputers, nil
}
