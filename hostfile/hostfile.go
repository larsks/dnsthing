// Package hostfile provides utilities for managing /etc/hosts-format files.
//
// The package supports reading and writing hosts files, adding and removing
// host entries, and looking up host-to-IP mappings. Both IPv4 and IPv6
// addresses are supported.
//
// Example usage:
//
//	hf := hostfile.NewHostfile("/etc/hosts")
//	if err := hf.Read(); err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := hf.AddHost("myserver.local", "192.168.1.100"); err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := hf.Write(); err != nil {
//	    log.Fatal(err)
//	}
package hostfile

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Hostfile represents a hosts file with hostname to IP address mappings.
type Hostfile struct {
	path  string            // File path to the hosts file
	hosts map[string]string // Maps hostname -> IP address
}

// NewHostfile creates a new Hostfile instance for the given file path.
// The file is not read automatically; call Read() to load the file contents.
func NewHostfile(path string) *Hostfile {
	return &Hostfile{
		path:  path,
		hosts: make(map[string]string),
	}
}

// AddHost adds or updates a host entry with the given name and IP address.
// If a host with the given name already exists, its address is updated.
// Returns an error if the hostname or address is empty, or if the address
// is not a valid IPv4 or IPv6 address.
func (hf *Hostfile) AddHost(name, address string) error {
	if name == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	if address == "" {
		return fmt.Errorf("address cannot be empty")
	}

	// Validate IP address using net.ParseIP (supports both IPv4 and IPv6)
	if net.ParseIP(address) == nil {
		return fmt.Errorf("invalid IP address: %s", address)
	}

	hf.hosts[name] = address
	return nil
}

// RemoveHost removes the host entry with the given name.
// Returns an error if the host does not exist.
func (hf *Hostfile) RemoveHost(name string) error {
	if _, exists := hf.hosts[name]; !exists {
		return fmt.Errorf("host not found: %s", name)
	}

	delete(hf.hosts, name)
	return nil
}

// LookupHost looks up the IP address for the given hostname.
// Returns the IP address and nil error if found, or an empty string
// and an error if the host is not found.
func (hf *Hostfile) LookupHost(name string) (string, error) {
	address, exists := hf.hosts[name]
	if !exists {
		return "", fmt.Errorf("host not found: %s", name)
	}
	return address, nil
}

// Read reads the hosts file from disk and loads the entries into memory.
// This replaces any existing in-memory mappings. Returns an error if the
// file cannot be opened or read. Malformed lines are skipped silently.
func (hf *Hostfile) Read() error {
	// Open the file
	file, err := os.Open(hf.path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close() //nolint:errcheck

	// Clear existing hosts map
	hf.hosts = make(map[string]string)

	// Parse file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Remove comments (anything after #)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
		}

		// Split into fields
		fields := strings.Fields(line)
		if len(fields) < 2 {
			// Skip malformed lines (need at least IP and one hostname)
			continue
		}

		ip := fields[0]
		// Validate IP address
		if net.ParseIP(ip) == nil {
			// Skip invalid IP addresses
			continue
		}

		// Store each hostname -> IP mapping
		for _, hostname := range fields[1:] {
			hf.hosts[hostname] = ip
		}
	}

	// Check for scanner errors
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	return nil
}

// Write writes the hosts file to disk, replacing any existing content.
// Uses an atomic write pattern (write to temp file, then rename) to prevent
// corruption. Entries are written in sorted order for deterministic output.
// Preserves the permissions of the original file if it exists.
func (hf *Hostfile) Write() error {
	// Get permissions from existing file, or use default
	var perm os.FileMode = 0644 // Default permissions
	if info, err := os.Stat(hf.path); err == nil {
		perm = info.Mode().Perm()
	}

	// Create temp file in same directory for atomic write
	dir := filepath.Dir(hf.path)
	tmpFile, err := os.CreateTemp(dir, ".hostfile-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure cleanup on error
	//nolint:errcheck
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write entries to temp file
	writer := bufio.NewWriter(tmpFile)

	// Sort keys for deterministic output
	hostnames := make([]string, 0, len(hf.hosts))
	for hostname := range hf.hosts {
		hostnames = append(hostnames, hostname)
	}
	sort.Strings(hostnames)

	for _, hostname := range hostnames {
		ip := hf.hosts[hostname]
		_, err := fmt.Fprintf(writer, "%s\t%s\n", ip, hostname)
		if err != nil {
			return fmt.Errorf("failed to write entry: %w", err)
		}
	}

	// Flush buffer
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush buffer: %w", err)
	}

	// Close temp file
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	tmpFile = nil // Prevent deferred cleanup

	// Set permissions to match original file
	if err := os.Chmod(tmpPath, perm); err != nil {
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Atomic rename
	log.Printf("updating %s", hf.path)
	if err := os.Rename(tmpPath, hf.path); err != nil {
		// Clean up on rename failure
		os.Remove(tmpPath) //nolint:errcheck
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
