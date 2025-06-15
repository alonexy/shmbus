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

// (Existing imports and helper functions in shmringbuffer_test.go remain)

func TestMultipleReaders(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(1024) // Sufficiently large buffer
	numMessages := 100
	numReaders := 5

	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	t.Cleanup(func() { rb.Destroy() })

	var messages [][]byte
	for i := 0; i < numMessages; i++ {
		messages = append(messages, []byte(fmt.Sprintf("Message-%d-%s", i, string(make([]byte, rand.Intn(50)+10))))) // Random length
	}

	var wg sync.WaitGroup
	errChan := make(chan error, numReaders)

	for rN := 0; rN < numReaders; rN++ {
		wg.Add(1)
		go func(readerNum int) {
			defer wg.Done()
			t.Logf("Reader %d: Starting", readerNum)
			reader, err := rb.RegisterReader()
			if err != nil {
				errChan <- fmt.Errorf("reader %d RegisterReader() failed: %w", readerNum, err)
				return
			}
			defer reader.Close()
			t.Logf("Reader %d: Registered", readerNum)

			readBuf := make([]byte, 256) // Max message size expected around 60-70 bytes
			for msgIdx, expectedMsg := range messages {
				// Add a timeout to prevent test hanging indefinitely if read blocks unexpectedly
				timeout := time.After(5 * time.Second) // Generous timeout
				var readN int
				var readErr error

				select {
				case <- func() chan struct{} { // Anonymous func to do the read
					doneRead := make(chan struct{})
					go func() {
						readN, readErr = reader.Read(readBuf)
						close(doneRead)
					}()
					return doneRead
				}():
					// Read completed
				case <-timeout:
					errChan <- fmt.Errorf("reader %d Read() message %d timed out", readerNum, msgIdx)
					return
				}

				if readErr != nil {
					errChan <- fmt.Errorf("reader %d Read() message %d failed: %w", readerNum, msgIdx, readErr)
					return
				}
				if readN != len(expectedMsg) {
					errChan <- fmt.Errorf("reader %d Read() message %d: read %d bytes, expected %d. Got: %q, Expected: %q",
						readerNum, msgIdx, readN, len(expectedMsg), readBuf[:readN], expectedMsg)
					return
				}
				if !bytes.Equal(readBuf[:readN], expectedMsg) {
					errChan <- fmt.Errorf("reader %d Read() message %d: data mismatch.\nExpected: %q\nGot:      %q",
						readerNum, msgIdx, expectedMsg, readBuf[:readN])
					return
				}
				t.Logf("Reader %d: Successfully read message %d (%s...)", readerNum, msgIdx, string(expectedMsg[:min(10, len(expectedMsg))]))
			}
			t.Logf("Reader %d: Finished reading all messages", readerNum)
		}(rN)
	}

	// Writer goroutine (or main test goroutine can be the writer)
	t.Log("Writer: Starting to write messages")
	for i, msg := range messages {
		written, err := rb.Write(msg)
		if err != nil {
			t.Fatalf("Writer: Write() message %d failed: %v", i, err)
		}
		if written != len(msg) {
			t.Fatalf("Writer: Write() message %d: wrote %d bytes, expected %d", i, written, len(msg))
		}
		t.Logf("Writer: Wrote message %d (%s...)", i, string(msg[:min(10, len(msg))]))
		// time.Sleep(5 * time.Millisecond) // Small delay to allow readers to catch up slightly if desired
	}
	t.Log("Writer: Finished writing all messages")

	wg.Wait()
	close(errChan)

	for err := range errChan {
		if err != nil {
			t.Error(err) // Report all errors from reader goroutines
		}
	}
	if t.Failed() {
		t.Fatal("One or more readers failed.")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


func TestBufferFullAndEmptyBlocking(t *testing.T) {
	key := getTestKey()
	// Small buffer, e.g., capacity for 2 messages of 10 bytes + headers.
	// Let's say each message is "0123456789" (10 bytes).
	// DataSize should be small. Header + ReaderInfoArray + Data.
	// Header ~32-40 bytes (depending on semaphore sizes), ReaderInfoArray for 10 readers ~ 10 * (8+8+4) = 200 bytes.
	// This is too large. The C.CGo_GetHeaderSize() and C.CGo_GetReaderInfoArraySize() are key.
	// Let's assume simple message size of 10 bytes.
	// To make it simple, let's make dataSize = 25 bytes, enough for two 10-byte messages, with some room.
	dataSize := uint64(25)
	msg1 := []byte("message_01") // 10 bytes
	msg2 := []byte("message_02") // 10 bytes
	msg3 := []byte("message_03") // 10 bytes (this one should initially block writer)
	msg4 := []byte("message_04") // 10 bytes (for reader blocking test)


	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create() failed: %v", err)
	}
	t.Cleanup(func() { rb.Destroy() })

	reader, err := rb.RegisterReader()
	if err != nil {
		t.Fatalf("RegisterReader() failed: %v", err)
	}
	t.Cleanup(func() { reader.Close() })

	// --- Phase 1: Test Writer Blocking ---
	t.Log("Phase 1: Testing Writer Blocking")
	// Write msg1
	if _, err := rb.Write(msg1); err != nil {
		t.Fatalf("Write msg1 failed: %v", err)
	}
	t.Logf("Wrote msg1 (%d bytes)", len(msg1))
	// Write msg2, buffer should be nearly full (20/25 bytes used)
	if _, err := rb.Write(msg2); err != nil {
		t.Fatalf("Write msg2 failed: %v", err)
	}
	t.Logf("Wrote msg2 (%d bytes), buffer nearly full", len(msg2))

	writerBlocked := make(chan struct{})
	writerUnblockedAndDone := make(chan error)

	go func() {
		t.Log("Writer goroutine: Attempting to write msg3 (should block)...")
		close(writerBlocked) // Signal that writer is about to attempt the blocking write
		if _, err := rb.Write(msg3); err != nil {
			writerUnblockedAndDone <- fmt.Errorf("write msg3 failed after unblock: %w", err)
			return
		}
		t.Log("Writer goroutine: Write msg3 successful (unblocked).")
		writerUnblockedAndDone <- nil
	}()

	<-writerBlocked // Wait for writer goroutine to be ready to block
	t.Log("Main: Writer goroutine is now attempting to write msg3.")

	// Give a moment for the writer to actually block on the semaphore
	time.Sleep(100 * time.Millisecond)
	t.Log("Main: Assuming writer is blocked. Reader will now read msg1.")

	readBuf1 := make([]byte, len(msg1))
	if n, err := reader.Read(readBuf1); err != nil || n != len(msg1) || !bytes.Equal(readBuf1, msg1) {
		t.Fatalf("Reader: Failed to read msg1. Read %d, err %v, data %q", n, err, readBuf1)
	}
	t.Log("Main: Reader read msg1. Writer should unblock soon.")

	select {
	case err := <-writerUnblockedAndDone:
		if err != nil {
			t.Fatalf("Phase 1 failed: %v", err)
		}
		t.Log("Phase 1: Writer successfully unblocked and wrote msg3.")
	case <-time.After(2 * time.Second): // Timeout for writer to unblock
		t.Fatal("Phase 1 failed: Writer did not unblock after reader read.")
	}

	// --- Phase 2: Test Reader Blocking ---
	t.Log("Phase 2: Testing Reader Blocking")
	// Reader has read msg1. msg2 and msg3 are in buffer.
	// Read msg2
	readBuf2 := make([]byte, len(msg2))
	if n, err := reader.Read(readBuf2); err != nil || n != len(msg2) || !bytes.Equal(readBuf2, msg2) {
		t.Fatalf("Reader: Failed to read msg2. Read %d, err %v, data %q", n, err, readBuf2)
	}
	t.Log("Reader read msg2.")
	// Read msg3
	readBuf3 := make([]byte, len(msg3))
	if n, err := reader.Read(readBuf3); err != nil || n != len(msg3) || !bytes.Equal(readBuf3, msg3) {
		t.Fatalf("Reader: Failed to read msg3. Read %d, err %v, data %q", n, err, readBuf3)
	}
	t.Log("Reader read msg3. Buffer should now be empty.")

	readerBlockedChan := make(chan struct{})
	readerUnblockedAndDoneChan := make(chan error)

	go func() {
		t.Log("Reader goroutine: Attempting to read msg4 (should block)...")
		close(readerBlockedChan)
		readBuf4 := make([]byte, len(msg4))
		n, err := reader.Read(readBuf4)
		if err != nil {
			readerUnblockedAndDoneChan <- fmt.Errorf("reader goroutine Read msg4 failed: %w", err)
			return
		}
		if n != len(msg4) || !bytes.Equal(readBuf4, msg4) {
			readerUnblockedAndDoneChan <- fmt.Errorf("reader goroutine Read msg4 data mismatch: read %d, data %q", n, readBuf4)
			return
		}
		t.Log("Reader goroutine: Read msg4 successful (unblocked).")
		readerUnblockedAndDoneChan <- nil
	}()

	<-readerBlockedChan
	t.Log("Main: Reader goroutine is now attempting to read (should be blocked).")
	time.Sleep(100 * time.Millisecond) // Give reader time to block

	t.Log("Main: Writer writing msg4...")
	if _, err := rb.Write(msg4); err != nil {
		t.Fatalf("Main: Write msg4 failed: %v", err)
	}
	t.Log("Main: Writer wrote msg4. Reader should unblock.")

	select {
	case err := <-readerUnblockedAndDoneChan:
		if err != nil {
			t.Fatalf("Phase 2 failed: %v", err)
		}
		t.Log("Phase 2: Reader successfully unblocked and read msg4.")
	case <-time.After(2 * time.Second):
		t.Fatal("Phase 2 failed: Reader did not unblock after writer wrote.")
	}
}


func TestReaderRegistrationMax(t *testing.T) {
	key := getTestKey()
	dataSize := uint64(100)
	rb, err := Create(key, dataSize)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	t.Cleanup(func() { rb.Destroy() })

	maxCReaders := int(C.CGo_GetMaxReaders())
	if maxCReaders <= 0 {
		t.Fatalf("C.CGo_GetMaxReaders() returned invalid value: %d", maxCReaders)
	}
	t.Logf("Max readers from C: %d", maxCReaders)

	var readers []*Reader
	for i := 0; i < maxCReaders; i++ {
		r, err := rb.RegisterReader()
		if err != nil {
			t.Fatalf("RegisterReader() #%d failed: %v", i+1, err)
		}
		readers = append(readers, r)
	}

	// Try to register one more, should fail
	_, err = rb.RegisterReader()
	if !errors.Is(err, ErrRegisterReaderFailed) {
		t.Errorf("RegisterReader() beyond max: expected ErrRegisterReaderFailed, got %v", err)
	}

	// Unregister one reader
	if len(readers) > 0 {
		err = readers[0].Close()
		if err != nil {
			t.Fatalf("readers[0].Close() failed: %v", err)
		}
		readers = readers[1:] // Remove from slice for bookkeeping
	}

	// Should be able to register one more now
	rNew, err := rb.RegisterReader()
	if err != nil {
		t.Errorf("RegisterReader() after unregistering one failed: %v", err)
	} else {
		t.Logf("Successfully registered a new reader after one was closed.")
		readers = append(readers, rNew) // Add for cleanup
	}

	// Cleanup all registered readers
	for _, r := range readers {
		r.Close()
	}
}
