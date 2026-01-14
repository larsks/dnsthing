package hostfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewHostfile(t *testing.T) {
	hf := NewHostfile("/etc/hosts")

	if hf.path != "/etc/hosts" {
		t.Errorf("NewHostfile() path = %s, want /etc/hosts", hf.path)
	}

	if hf.hosts == nil {
		t.Error("NewHostfile() hosts map is nil")
	}

	if len(hf.hosts) != 0 {
		t.Errorf("NewHostfile() hosts map not empty, got %d entries", len(hf.hosts))
	}
}

func TestAddHost(t *testing.T) {
	tests := []struct {
		name        string
		hostname    string
		address     string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid IPv4 address",
			hostname: "localhost",
			address:  "127.0.0.1",
			wantErr:  false,
		},
		{
			name:     "valid IPv6 address",
			hostname: "ip6-localhost",
			address:  "::1",
			wantErr:  false,
		},
		{
			name:     "valid IPv6 full address",
			hostname: "myserver",
			address:  "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			wantErr:  false,
		},
		{
			name:     "update existing host",
			hostname: "example.com",
			address:  "192.168.1.100",
			wantErr:  false,
		},
		{
			name:        "empty hostname",
			hostname:    "",
			address:     "192.168.1.1",
			wantErr:     true,
			errContains: "hostname cannot be empty",
		},
		{
			name:        "empty address",
			hostname:    "example.com",
			address:     "",
			wantErr:     true,
			errContains: "address cannot be empty",
		},
		{
			name:        "invalid IPv4 address",
			hostname:    "badhost",
			address:     "999.999.999.999",
			wantErr:     true,
			errContains: "invalid IP address",
		},
		{
			name:        "malformed IP address",
			hostname:    "badhost",
			address:     "not-an-ip",
			wantErr:     true,
			errContains: "invalid IP address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf := NewHostfile("/tmp/test")
			err := hf.AddHost(tt.hostname, tt.address)

			if tt.wantErr {
				if err == nil {
					t.Errorf("AddHost() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("AddHost() error = %v, want error containing %s", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("AddHost() unexpected error = %v", err)
					return
				}
				// Verify the host was added
				if hf.hosts[tt.hostname] != tt.address {
					t.Errorf("AddHost() host not added correctly, got %s, want %s", hf.hosts[tt.hostname], tt.address)
				}
			}
		})
	}
}

func TestAddHostUpdate(t *testing.T) {
	hf := NewHostfile("/tmp/test")

	// Add initial host
	if err := hf.AddHost("example.com", "192.168.1.1"); err != nil {
		t.Fatalf("AddHost() initial add failed: %v", err)
	}

	// Update the same host
	if err := hf.AddHost("example.com", "192.168.1.2"); err != nil {
		t.Fatalf("AddHost() update failed: %v", err)
	}

	// Verify the address was updated
	if hf.hosts["example.com"] != "192.168.1.2" {
		t.Errorf("AddHost() update failed, got %s, want 192.168.1.2", hf.hosts["example.com"])
	}
}

func TestRemoveHost(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*Hostfile)
		removeHost  string
		wantErr     bool
		errContains string
	}{
		{
			name: "remove existing host",
			setup: func(hf *Hostfile) {
				hf.AddHost("example.com", "192.168.1.1")
			},
			removeHost: "example.com",
			wantErr:    false,
		},
		{
			name:        "remove non-existent host",
			setup:       func(hf *Hostfile) {},
			removeHost:  "nonexistent.com",
			wantErr:     true,
			errContains: "host not found",
		},
		{
			name:        "remove from empty hostfile",
			setup:       func(hf *Hostfile) {},
			removeHost:  "example.com",
			wantErr:     true,
			errContains: "host not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf := NewHostfile("/tmp/test")
			tt.setup(hf)

			err := hf.RemoveHost(tt.removeHost)

			if tt.wantErr {
				if err == nil {
					t.Errorf("RemoveHost() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("RemoveHost() error = %v, want error containing %s", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("RemoveHost() unexpected error = %v", err)
					return
				}
				// Verify the host was removed
				if _, exists := hf.hosts[tt.removeHost]; exists {
					t.Errorf("RemoveHost() host still exists after removal")
				}
			}
		})
	}
}

func TestLookupHost(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*Hostfile)
		lookupHost  string
		wantAddress string
		wantErr     bool
		errContains string
	}{
		{
			name: "lookup existing host",
			setup: func(hf *Hostfile) {
				hf.AddHost("example.com", "192.168.1.1")
			},
			lookupHost:  "example.com",
			wantAddress: "192.168.1.1",
			wantErr:     false,
		},
		{
			name: "lookup IPv6 host",
			setup: func(hf *Hostfile) {
				hf.AddHost("ip6host", "::1")
			},
			lookupHost:  "ip6host",
			wantAddress: "::1",
			wantErr:     false,
		},
		{
			name:        "lookup non-existent host",
			setup:       func(hf *Hostfile) {},
			lookupHost:  "nonexistent.com",
			wantErr:     true,
			errContains: "host not found",
		},
		{
			name:        "lookup from empty hostfile",
			setup:       func(hf *Hostfile) {},
			lookupHost:  "example.com",
			wantErr:     true,
			errContains: "host not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf := NewHostfile("/tmp/test")
			tt.setup(hf)

			address, err := hf.LookupHost(tt.lookupHost)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LookupHost() expected error, got nil")
					return
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("LookupHost() error = %v, want error containing %s", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("LookupHost() unexpected error = %v", err)
					return
				}
				if address != tt.wantAddress {
					t.Errorf("LookupHost() = %s, want %s", address, tt.wantAddress)
				}
			}
		})
	}
}

func TestRead(t *testing.T) {
	tests := []struct {
		name         string
		fileContent  string
		wantHosts    map[string]string
		wantErr      bool
		errContains  string
		skipFileTest bool // for cases where we test file errors
	}{
		{
			name: "valid hosts file",
			fileContent: `127.0.0.1 localhost
192.168.1.1 example.com
`,
			wantHosts: map[string]string{
				"localhost":   "127.0.0.1",
				"example.com": "192.168.1.1",
			},
			wantErr: false,
		},
		{
			name: "hosts file with comments",
			fileContent: `# This is a comment
127.0.0.1 localhost # inline comment
# Another comment
192.168.1.1 example.com
`,
			wantHosts: map[string]string{
				"localhost":   "127.0.0.1",
				"example.com": "192.168.1.1",
			},
			wantErr: false,
		},
		{
			name: "hosts file with blank lines",
			fileContent: `
127.0.0.1 localhost

192.168.1.1 example.com

`,
			wantHosts: map[string]string{
				"localhost":   "127.0.0.1",
				"example.com": "192.168.1.1",
			},
			wantErr: false,
		},
		{
			name: "multiple hostnames per line",
			fileContent: `127.0.0.1 localhost localhost.localdomain
192.168.1.100 app.local api.local web.local
`,
			wantHosts: map[string]string{
				"localhost":             "127.0.0.1",
				"localhost.localdomain": "127.0.0.1",
				"app.local":             "192.168.1.100",
				"api.local":             "192.168.1.100",
				"web.local":             "192.168.1.100",
			},
			wantErr: false,
		},
		{
			name: "IPv6 addresses",
			fileContent: `::1 ip6-localhost ip6-loopback
2001:db8::1 myserver
`,
			wantHosts: map[string]string{
				"ip6-localhost": "::1",
				"ip6-loopback":  "::1",
				"myserver":      "2001:db8::1",
			},
			wantErr: false,
		},
		{
			name: "mixed IPv4 and IPv6",
			fileContent: `127.0.0.1 localhost
::1 ip6-localhost
192.168.1.1 example.com
2001:db8::2 example6.com
`,
			wantHosts: map[string]string{
				"localhost":     "127.0.0.1",
				"ip6-localhost": "::1",
				"example.com":   "192.168.1.1",
				"example6.com":  "2001:db8::2",
			},
			wantErr: false,
		},
		{
			name: "malformed lines skipped",
			fileContent: `127.0.0.1 localhost
invalid-line
192.168.1.1
just-hostname
999.999.999.999 badip
192.168.1.2 valid.host
`,
			wantHosts: map[string]string{
				"localhost":  "127.0.0.1",
				"valid.host": "192.168.1.2",
			},
			wantErr: false,
		},
		{
			name:        "empty file",
			fileContent: "",
			wantHosts:   map[string]string{},
			wantErr:     false,
		},
		{
			name: "only comments",
			fileContent: `# Just comments
# Nothing else
`,
			wantHosts: map[string]string{},
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "hosts")

			// Write test file
			if err := os.WriteFile(tmpFile, []byte(tt.fileContent), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			hf := NewHostfile(tmpFile)
			err := hf.Read()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Read() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Read() error = %v, want error containing %s", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("Read() unexpected error = %v", err)
					return
				}

				// Verify hosts were loaded correctly
				if len(hf.hosts) != len(tt.wantHosts) {
					t.Errorf("Read() loaded %d hosts, want %d", len(hf.hosts), len(tt.wantHosts))
				}

				for hostname, wantIP := range tt.wantHosts {
					gotIP, exists := hf.hosts[hostname]
					if !exists {
						t.Errorf("Read() missing host %s", hostname)
					} else if gotIP != wantIP {
						t.Errorf("Read() host %s = %s, want %s", hostname, gotIP, wantIP)
					}
				}
			}
		})
	}
}

func TestReadNonExistentFile(t *testing.T) {
	hf := NewHostfile("/nonexistent/path/to/hosts")
	err := hf.Read()

	if err == nil {
		t.Error("Read() expected error for non-existent file, got nil")
	}

	if !strings.Contains(err.Error(), "failed to open file") {
		t.Errorf("Read() error = %v, want error containing 'failed to open file'", err)
	}
}

func TestWrite(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*Hostfile)
		wantLines []string
	}{
		{
			name: "single host",
			setup: func(hf *Hostfile) {
				hf.AddHost("localhost", "127.0.0.1")
			},
			wantLines: []string{"127.0.0.1\tlocalhost"},
		},
		{
			name: "multiple hosts",
			setup: func(hf *Hostfile) {
				hf.AddHost("localhost", "127.0.0.1")
				hf.AddHost("example.com", "192.168.1.1")
				hf.AddHost("test.local", "10.0.0.1")
			},
			wantLines: []string{
				"192.168.1.1\texample.com",
				"127.0.0.1\tlocalhost",
				"10.0.0.1\ttest.local",
			},
		},
		{
			name: "IPv6 addresses",
			setup: func(hf *Hostfile) {
				hf.AddHost("ip6-localhost", "::1")
				hf.AddHost("myserver", "2001:db8::1")
			},
			wantLines: []string{
				"::1\tip6-localhost",
				"2001:db8::1\tmyserver",
			},
		},
		{
			name:      "empty hostfile",
			setup:     func(hf *Hostfile) {},
			wantLines: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "hosts")

			hf := NewHostfile(tmpFile)
			tt.setup(hf)

			err := hf.Write()
			if err != nil {
				t.Errorf("Write() unexpected error = %v", err)
				return
			}

			// Read the file back
			content, err := os.ReadFile(tmpFile)
			if err != nil {
				t.Fatalf("Failed to read written file: %v", err)
			}

			lines := strings.Split(strings.TrimSpace(string(content)), "\n")

			// Handle empty file case
			if len(tt.wantLines) == 0 {
				if len(content) != 0 {
					t.Errorf("Write() wrote content to empty hostfile, got %s", string(content))
				}
				return
			}

			if len(lines) != len(tt.wantLines) {
				t.Errorf("Write() wrote %d lines, want %d. Content:\n%s", len(lines), len(tt.wantLines), string(content))
				return
			}

			for i, wantLine := range tt.wantLines {
				if lines[i] != wantLine {
					t.Errorf("Write() line %d = %s, want %s", i, lines[i], wantLine)
				}
			}
		})
	}
}

func TestWriteOverwritesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hosts")

	// Write initial content
	initialContent := `# Old content
127.0.0.1 oldhost
192.168.1.100 anotherold
`
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	// Create new hostfile and write new content
	hf := NewHostfile(tmpFile)
	hf.AddHost("newhost", "10.0.0.1")

	if err := hf.Write(); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read the file back
	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	got := string(content)
	if strings.Contains(got, "oldhost") {
		t.Errorf("Write() did not overwrite old content, still contains 'oldhost'")
	}
	if !strings.Contains(got, "newhost") {
		t.Errorf("Write() missing new content 'newhost'")
	}
}

func TestReadWriteRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hosts")

	// Write initial content
	initialContent := `127.0.0.1 localhost
192.168.1.1 example.com
::1 ip6-localhost
`
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	// Read the file
	hf := NewHostfile(tmpFile)
	if err := hf.Read(); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Verify loaded correctly
	expectedHosts := map[string]string{
		"localhost":     "127.0.0.1",
		"example.com":   "192.168.1.1",
		"ip6-localhost": "::1",
	}

	if len(hf.hosts) != len(expectedHosts) {
		t.Errorf("Read() loaded %d hosts, want %d", len(hf.hosts), len(expectedHosts))
	}

	for hostname, wantIP := range expectedHosts {
		if gotIP := hf.hosts[hostname]; gotIP != wantIP {
			t.Errorf("Read() host %s = %s, want %s", hostname, gotIP, wantIP)
		}
	}

	// Write back
	if err := hf.Write(); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read again
	hf2 := NewHostfile(tmpFile)
	if err := hf2.Read(); err != nil {
		t.Fatalf("Second Read() error = %v", err)
	}

	// Verify still identical
	if len(hf2.hosts) != len(expectedHosts) {
		t.Errorf("Second Read() loaded %d hosts, want %d", len(hf2.hosts), len(expectedHosts))
	}

	for hostname, wantIP := range expectedHosts {
		if gotIP := hf2.hosts[hostname]; gotIP != wantIP {
			t.Errorf("Second Read() host %s = %s, want %s", hostname, gotIP, wantIP)
		}
	}
}

func TestIntegrationAddWriteRead(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hosts")

	// Create new hostfile
	hf := NewHostfile(tmpFile)

	// Add some hosts
	if err := hf.AddHost("localhost", "127.0.0.1"); err != nil {
		t.Fatalf("AddHost() error = %v", err)
	}
	if err := hf.AddHost("example.com", "192.168.1.1"); err != nil {
		t.Fatalf("AddHost() error = %v", err)
	}
	if err := hf.AddHost("ip6host", "::1"); err != nil {
		t.Fatalf("AddHost() error = %v", err)
	}

	// Write to file
	if err := hf.Write(); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read back with a new instance
	hf2 := NewHostfile(tmpFile)
	if err := hf2.Read(); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Verify all hosts persisted
	tests := []struct {
		hostname string
		wantIP   string
	}{
		{"localhost", "127.0.0.1"},
		{"example.com", "192.168.1.1"},
		{"ip6host", "::1"},
	}

	for _, tt := range tests {
		gotIP, err := hf2.LookupHost(tt.hostname)
		if err != nil {
			t.Errorf("LookupHost(%s) error = %v", tt.hostname, err)
		}
		if gotIP != tt.wantIP {
			t.Errorf("LookupHost(%s) = %s, want %s", tt.hostname, gotIP, tt.wantIP)
		}
	}
}

func TestIntegrationModifyWriteRead(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "hosts")

	// Write initial content
	initialContent := `127.0.0.1 localhost
192.168.1.1 old.example.com
10.0.0.1 remove-me.local
`
	if err := os.WriteFile(tmpFile, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to create initial file: %v", err)
	}

	// Read the file
	hf := NewHostfile(tmpFile)
	if err := hf.Read(); err != nil {
		t.Fatalf("Read() error = %v", err)
	}

	// Modify: update one, remove one, add one
	if err := hf.AddHost("old.example.com", "192.168.1.99"); err != nil { // Update
		t.Fatalf("AddHost() update error = %v", err)
	}
	if err := hf.RemoveHost("remove-me.local"); err != nil { // Remove
		t.Fatalf("RemoveHost() error = %v", err)
	}
	if err := hf.AddHost("new.example.com", "172.16.0.1"); err != nil { // Add
		t.Fatalf("AddHost() new error = %v", err)
	}

	// Write changes
	if err := hf.Write(); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Read back
	hf2 := NewHostfile(tmpFile)
	if err := hf2.Read(); err != nil {
		t.Fatalf("Second Read() error = %v", err)
	}

	// Verify modifications
	if ip, _ := hf2.LookupHost("localhost"); ip != "127.0.0.1" {
		t.Errorf("localhost address changed, got %s, want 127.0.0.1", ip)
	}

	if ip, _ := hf2.LookupHost("old.example.com"); ip != "192.168.1.99" {
		t.Errorf("old.example.com not updated, got %s, want 192.168.1.99", ip)
	}

	if _, err := hf2.LookupHost("remove-me.local"); err == nil {
		t.Error("remove-me.local still exists after removal")
	}

	if ip, err := hf2.LookupHost("new.example.com"); err != nil {
		t.Errorf("new.example.com not found: %v", err)
	} else if ip != "172.16.0.1" {
		t.Errorf("new.example.com = %s, want 172.16.0.1", ip)
	}
}
