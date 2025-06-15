package shmringbuffer

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"testing"
	"time"
)

// Helper to generate unique keys for each test to avoid collisions
var testKeyCounter uint32 = uint32(os.Getpid() % 10000) // Start with PID based component
var keyMutex sync.Mutex

func getTestKey() uint32 {
	keyMutex.Lock()
	defer keyMutex.Unlock()
	// Using a combination of time and a counter for uniqueness in tests
	// System V IPC keys need to be system-wide unique if not careful.
	// For testing, we try to make them unique enough.
	// Using a high counter starting from PID is a common trick.
	keyCounter := uint32(time.Now().UnixNano()%100000) + testKeyCounter*100000
	testKeyCounter++
	if keyCounter == 0 { // Ensure key is not 0
		keyCounter = 1
	}
	return keyCounter
}

func TestCreateDestroy(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(1024)

	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	if rb == nil {
		t.Fatal("Create() returned nil ShmRingBuffer")
	}
	t.Logf("Created SHM with key %d, shmID %d", key, rb.shmID)

	// Defer destruction to ensure cleanup
	t.Cleanup(func() {
		t.Logf("Cleaning up SHM key %d, shmID %d", key, rb.shmID)
		// It might already be detached or destroyed by the test logic itself
		if rb.shmAddr != nil || rb.isCreator { // Check if it's meaningful to destroy
			err := rb.Destroy()
			if err != nil {
				// Log error during cleanup, but don't fail the test here
				// as the primary test logic might have already passed/failed.
				// On some systems, destroying an already IPC_RMID'd segment
				// might give an error, or if not attached.
				t.Logf("Error during Destroy in Cleanup: %v", err)
			} else {
				t.Logf("Successfully destroyed SHM key %d in Cleanup", key)
			}
		}
	})

	// Test IsCreator flag
	if !rb.isCreator {
		t.Error("Expected isCreator to be true after Create()")
	}

	// Detach before destroy (good practice, though Destroy might handle it)
	err = rb.Detach()
	if err != nil {
		t.Fatalf("Detach() failed: %v", err)
	}
	if rb.shmAddr != nil {
		t.Error("shmAddr should be nil after Detach()")
	}

	// Destroy is handled by t.Cleanup, but we can test it explicitly too
	// Re-assign rb.isCreator for this explicit destroy if Detach nilled it out
	// For this test, let's rely on Cleanup for the final destroy.
	// If we call destroy here, cleanup might try again.
	// Let's test a scenario where we destroy and then try to attach.

	// Explicit destroy for testing its effect
	err = rb.Destroy() // Will call Detach again if it re-attaches internally, which it shouldn't
	if err != nil {
		t.Fatalf("Destroy() failed: %v", err)
	}
	rb.isCreator = false // Mark as no longer the valid creator for cleanup logic

	// Try to attach to the destroyed segment (should fail or be like new)
	_, err = Attach(key, dataSize)
	if err == nil {
		t.Errorf("Attach() to a destroyed segment should fail, but it succeeded.")
		// If it succeeded, it means a new segment was created by shmget, clean it up.
		// This might happen if the key is reused by the OS quickly.
		// For robust testing, OS-level checks might be needed.
		// For now, we expect an error.
		rbAttachAfterDestroy, attachErr := Attach(key, dataSize)
		if attachErr == nil {
			rbAttachAfterDestroy.Destroy()
		}

	} else if !errors.Is(err, ErrNotInitialized) && !errors.Is(err, ErrShmAttachFailed) && !errors.Is(err, ErrShmGetIdFailed) {
		// Depending on timing and OS, shmget might return a new ID or fail.
		// If it gets an ID but can't init (because it's new), ErrNotInitialized is one path.
		// If shmat fails, ErrShmAttachFailed. If shmget fails, ErrShmGetIdFailed.
		t.Logf("Attach to destroyed segment failed as expected: %v", err)
	} else {
		t.Logf("Attach to destroyed segment failed as expected with specific error: %v", err)
	}
}

func TestAttach(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(2048)

	rbCreator, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create() for creator failed: %v", err)
	}
	t.Cleanup(func() {
		// Ensure creator cleans up if test fails before explicit destroy
		if rbCreator.isCreator { // Check if it still "owns" the right to destroy
			t.Logf("Cleanup: Destroying from creator instance for key %d", key)
			rbCreator.Destroy()
		}
	})

	if !rbCreator.isCreator {
		t.Fatal("Creator's isCreator flag is false")
	}
    if rbCreator.shmAddr == nil {
        t.Fatal("Creator's shmAddr is nil after Create")
    }

	rbAttacher, err := Attach(key, dataSize)
	if err != nil {
		t.Fatalf("Attach() for attacher failed: %v", err)
	}
	if rbAttacher.isCreator {
		t.Error("Attacher's isCreator flag is true")
	}
    if rbAttacher.shmAddr == nil {
        t.Fatal("Attacher's shmAddr is nil after Attach")
    }
    if rbAttacher.dataBufferSize != dataSize {
        t.Errorf("Attacher's dataBufferSize = %d, want %d", rbAttacher.dataBufferSize, dataSize)
    }


	// Test that magic number was verified by Attach (implicitly)
	// We can also try to write from one and read from another (in a different test)

	err = rbAttacher.Detach()
	if err != nil {
		t.Errorf("Attacher Detach() failed: %v", err)
	}

	err = rbCreator.Detach() // Detach creator
	if err != nil {
		t.Errorf("Creator Detach() failed: %v", err)
	}

	// Creator is responsible for Destroy
	err = rbCreator.Destroy()
	if err != nil {
		t.Errorf("Creator Destroy() failed: %v", err)
	}
	rbCreator.isCreator = false // Mark as destroyed for cleanup
}

func TestAttachWrongSize(t *testing.T) {
	key := getTestKey()
	creatorDataSize := uint64(1024)
	attacherDataSize := uint64(2048) // Different size

	rbCreator, err := Create(key, creatorDataSize)
	if err != nil {
		t.Fatalf("Create() for creator failed: %v", err)
	}
	t.Cleanup(func() {
		if rbCreator.isCreator {
			rbCreator.Destroy()
		}
	})

	// Attacher tries to attach with a different (expected) dataBufferSize
	_, err = Attach(key, attacherDataSize)
	if err == nil {
		t.Errorf("Attach() with wrong dataBufferSize should have failed, but succeeded")
		// If it succeeded, try to clean up this unexpected segment
		rbUnexpected, attachErr := Attach(key, attacherDataSize)
		if attachErr == nil {
			rbUnexpected.Destroy()
		}
	} else {
		// We expect an error, specifically ErrNotInitialized due to buffer size mismatch check
		if !errors.Is(err, ErrNotInitialized) {
			t.Errorf("Attach() with wrong dataBufferSize: expected ErrNotInitialized, got %v", err)
		} else {
			t.Logf("Attach() with wrong dataBufferSize failed as expected: %v", err)
		}
	}
}


func TestWriteReadSingleReader(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(128) // Small buffer to encourage wrapping
	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	t.Cleanup(func() { rb.Destroy() })

	reader, err := rb.RegisterReader()
	if err != nil {
		t.Fatalf("RegisterReader() failed: %v", err)
	}
	t.Cleanup(func() { reader.Close() }) // Ensure reader is closed

	testMessages := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		[]byte(string(make([]byte, int(dataSize/2)))), // Half buffer size
		[]byte("this message will wrap around the buffer completely and then some more"),
		[]byte("another short one"),
	}

	for i, msg := range testMessages {
		written, err := rb.Write(msg)
		if err != nil {
			t.Fatalf("Write() message %d failed: %v", i, err)
		}
		if written != len(msg) {
			t.Fatalf("Write() message %d: wrote %d bytes, expected %d", i, written, len(msg))
		}

		readBuf := make([]byte, len(msg) + 10) // Slightly larger buffer for reading
		readN, err := reader.Read(readBuf)
		if err != nil {
			t.Fatalf("Read() message %d failed: %v", i, err)
		}
		if readN != len(msg) {
			t.Fatalf("Read() message %d: read %d bytes, expected %d", i, readN, len(msg))
		}
		if !bytes.Equal(readBuf[:readN], msg) {
			t.Fatalf("Read() message %d: data mismatch.\nExpected: %q\nGot:      %q", i, msg, readBuf[:readN])
		}
		t.Logf("Successfully wrote and read message %d: %s", i, msg)
	}
}

func TestZeroLengthOperations(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(100)
	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Cleanup(func() { rb.Destroy() })

	// Test zero-length write
	n, err := rb.Write([]byte{})
	if err != nil {
		t.Errorf("Write of empty slice failed: %v", err)
	}
	if n != 0 {
		t.Errorf("Write of empty slice: expected 0 bytes written, got %d", n)
	}

	reader, err := rb.RegisterReader()
	if err != nil {
		t.Fatalf("RegisterReader failed: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	// Test zero-length read into non-empty buffer
	buf := make([]byte, 10)
	n, err = reader.Read(buf[:0]) // Reading into a zero-length slice
	if err != nil {
		t.Errorf("Read with zero-length buffer slice failed: %v", err)
	}
	if n != 0 {
		t.Errorf("Read with zero-length buffer slice: expected 0 bytes read, got %d", n)
	}

	// Test zero-length read into nil buffer (should be handled by Go layer or C)
	// Our Go Read checks len(buffer) == 0.
    // n, err = reader.Read(nil)
    // if err != nil {
    //    t.Errorf("Read with nil buffer failed: %v", err)
    // }
    // if n != 0 {
    //    t.Errorf("Read with nil buffer: expected 0 bytes read, got %d", n)
    // }
}

func TestWriteTooLarge(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(100)
	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Cleanup(func() { rb.Destroy() })

	largeData := make([]byte, dataSize+1)
	_, err = rb.Write(largeData)
	if !errors.Is(err, ErrDataSizeTooLarge) {
		t.Errorf("Write of data larger than buffer: expected ErrDataSizeTooLarge, got %v", err)
	}
}

// More tests to be added:
// - TestMultipleReaders
// - TestBufferFullBlockingWrite
// - TestBufferEmptyBlockingRead
// - TestReaderRegistration (max readers, unregister/re-register)

// Basic init for random numbers
func init() {
	rand.Seed(time.Now().UnixNano())
}
