package write

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/larsks/dnsthing/hostfile"
)

// helper function to create a temporary hostfile for testing
func createTempHostfile(t *testing.T) (*hostfile.Hostfile, string) {
	t.Helper()
	tmpDir := t.TempDir()
	hostsPath := filepath.Join(tmpDir, "hosts")
	hf := hostfile.NewHostfile(hostsPath)
	return hf, hostsPath
}

func TestWriteManagerNoThrottling(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := NewManager(ctx, hf, "", 0) // No throttling

	// Add a host and request write
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}

	// Write should happen immediately
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("RequestWrite failed: %v", err)
	}

	// Verify the host was written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	ip, err := hf.LookupHost("test1")
	if err != nil {
		t.Errorf("host not found after write: %v", err)
	} else if ip != "192.168.1.1" {
		t.Errorf("got IP %s, want 192.168.1.1", ip)
	}
}

func TestWriteManagerWithThrottling(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := NewManager(ctx, hf, "", 200*time.Millisecond) // 200ms throttle

	// First write should be immediate
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}

	start := time.Now()
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("first RequestWrite failed: %v", err)
	}
	firstWriteDuration := time.Since(start)

	// First write should be very fast (< 50ms)
	if firstWriteDuration > 50*time.Millisecond {
		t.Errorf("first write took %v, expected < 50ms", firstWriteDuration)
	}

	// Second write should be delayed
	if err := hf.AddHost("test2", "192.168.1.2"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}

	secondWriteStart := time.Now()
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("second RequestWrite failed: %v", err)
	}
	immediateReturnDuration := time.Since(secondWriteStart)

	// The second write should have been scheduled (returned quickly)
	if immediateReturnDuration > 50*time.Millisecond {
		t.Errorf("RequestWrite should return immediately, took %v", immediateReturnDuration)
	}

	// Wait for the scheduled write to complete
	time.Sleep(250 * time.Millisecond)

	// Verify both hosts were written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	if _, err := hf.LookupHost("test1"); err != nil {
		t.Errorf("test1 not found: %v", err)
	}
	if _, err := hf.LookupHost("test2"); err != nil {
		t.Errorf("test2 not found after scheduled write: %v", err)
	}
}

func TestWriteManagerBatching(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := NewManager(ctx, hf, "", 300*time.Millisecond) // 300ms throttle

	// First write - immediate
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Add multiple hosts rapidly (within throttle interval)
	time.Sleep(50 * time.Millisecond)
	if err := hf.AddHost("test2", "192.168.1.2"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("second write request failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if err := hf.AddHost("test3", "192.168.1.3"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("third write request failed: %v", err)
	}

	// Wait for batched write
	time.Sleep(350 * time.Millisecond)

	// Verify all hosts were written in a single batched write
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	for i := 1; i <= 3; i++ {
		hostname := "test" + string(rune('0'+i))
		if _, err := hf.LookupHost(hostname); err != nil {
			t.Errorf("%s not found: %v", hostname, err)
		}
	}
}

func TestWriteManagerFlush(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := NewManager(ctx, hf, "", 500*time.Millisecond)

	// Write something immediately
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Schedule a pending write
	time.Sleep(50 * time.Millisecond)
	if err := hf.AddHost("test2", "192.168.1.2"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("second write request failed: %v", err)
	}

	// Flush should write immediately
	if err := wm.Flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	// Verify both hosts were written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	if _, err := hf.LookupHost("test1"); err != nil {
		t.Errorf("test1 not found: %v", err)
	}
	if _, err := hf.LookupHost("test2"); err != nil {
		t.Errorf("test2 not found after flush: %v", err)
	}
}

func TestWriteManagerFlushNoPending(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := NewManager(ctx, hf, "", 0)

	// Flush with no pending writes should not error
	if err := wm.Flush(); err != nil {
		t.Errorf("flush with no pending writes failed: %v", err)
	}
}

func TestWriteManagerWithUpdateCommand(t *testing.T) {
	hf, hostsPath := createTempHostfile(t)
	ctx := context.Background()

	// Create a marker file to verify command execution
	markerPath := filepath.Join(filepath.Dir(hostsPath), "marker")
	updateCmd := "touch " + markerPath

	wm := NewManager(ctx, hf, updateCmd, 0)

	// Add and write
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Wait for command to execute (it runs in background)
	time.Sleep(100 * time.Millisecond)

	// Verify command was executed
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		t.Errorf("update command was not executed (marker file not found)")
	}
}

func TestWriteManagerNoUpdateCommand(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := NewManager(ctx, hf, "", 0) // No update command

	// Add and write
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.RequestWrite(); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Should succeed without error
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	if _, err := hf.LookupHost("test1"); err != nil {
		t.Errorf("test1 not found: %v", err)
	}
}
