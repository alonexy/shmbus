# Go Shared Memory Ring Buffer (shmringbuffer)

This Go package provides a high-performance, inter-process communication (IPC) mechanism using a shared memory ring buffer. It supports a single writer process and multiple reader processes. The underlying implementation is in C, utilizing POSIX semaphores for synchronization and System V shared memory segments, exposed to Go via CGo.

## Features

-   Single writer, multiple readers.
-   Blocking writes when the buffer is full.
-   Blocking reads when the buffer is empty (per reader).
-   Efficient data transfer via shared memory.
-   Process-safe synchronization using POSIX semaphores.

## Directory Structure

The core logic resides in the `shmringbuffer/` directory:

```
shmringbuffer/
├── shmringbuffer.go         // Go package API
├── cgo_wrapper.c/h          // CGo wrapper functions
├── ringbuffer.c/h           // C ring buffer logic and synchronization
├── shm_utils.c/h            // C shared memory utilities
└── shm_ringbuffer.h         // C data structures for shared memory layout
```

## Prerequisites

-   A C compiler (like GCC) for CGo.
-   Go programming environment.
-   Linux or macOS environment (System V IPC and POSIX semaphores are used).
-   `pthread` library (usually available by default on target systems).

## Building

The Go package uses CGo to compile the C source files. You can build it using standard Go tools:

```bash
# To build the package (and dependencies)
go build ./shmringbuffer

# To run tests
go test ./shmringbuffer -v
```

A `Makefile` is also provided for convenience (details in the Makefile section).

## Basic Usage

### Writer Process

```go
package main

import (
	"fmt"
	"log"
	"time"

	"your_module_path/shmringbuffer" // Replace with your actual module path
)

const (
	shmKey         = 12345 // Choose a unique IPC key
	bufferDataSize = 1024 * 1024 // 1MB data buffer
)

func main() {
	// Create the shared memory ring buffer
	rb, err := shmringbuffer.Create(shmKey, bufferDataSize)
	if err != nil {
		log.Fatalf("Failed to create ring buffer: %v", err)
	}
	defer func() {
		log.Println("Writer: Destroying ring buffer...")
		if err := rb.Destroy(); err != nil {
			log.Printf("Writer: Failed to destroy ring buffer: %v", err)
		} else {
			log.Println("Writer: Ring buffer destroyed.")
		}
	}()

	log.Println("Writer: Ring buffer created successfully.")

	for i := 0; i < 100; i++ {
		message := []byte(fmt.Sprintf("Message %d from writer", i))
		written, err := rb.Write(message)
		if err != nil {
			log.Printf("Writer: Failed to write message: %v", err)
			// Handle error, maybe retry or exit
			break
		}
		log.Printf("Writer: Wrote %d bytes: %s", written, message)
		time.Sleep(50 * time.Millisecond)
	}

	log.Println("Writer: Finished writing messages.")
	// Keep alive for a bit for readers, or use other synchronization
	time.Sleep(10 * time.Second)
}
```

### Reader Process

```go
package main

import (
	"fmt"
	"log"
	"time"
	"os"

	"your_module_path/shmringbuffer" // Replace with your actual module path
)

const (
	shmKey         = 12345 // Must match the writer's key
	bufferDataSize = 1024 * 1024 // Must match the writer's buffer data size
)

func main() {
	// Attach to the existing shared memory ring buffer
	rb, err := shmringbuffer.Attach(shmKey, bufferDataSize)
	if err != nil {
		log.Fatalf("Reader: Failed to attach to ring buffer: %v", err)
	}
	defer rb.Detach() // Detach when done

	log.Printf("Reader (PID: %d): Attached to ring buffer successfully.", os.Getpid())

	reader, err := rb.RegisterReader()
	if err != nil {
		log.Fatalf("Reader: Failed to register: %v", err)
	}
	defer reader.Close()

	log.Println("Reader: Registered successfully.")

	readBuffer := make([]byte, 512) // Buffer to read data into

	for {
		n, err := reader.Read(readBuffer)
		if err != nil {
			log.Printf("Reader: Error reading: %v", err)
			// Consider specific error handling, e.g., if buffer is destroyed
			break
		}
		if n > 0 {
			log.Printf("Reader: Read %d bytes: %s", n, readBuffer[:n])
		}
		// Add a small delay or specific logic if no data is read (though Read should block)
		// time.Sleep(10 * time.Millisecond) // Only if non-blocking read was implemented
	}
	log.Println("Reader: Exiting.")
}

```

**Note on `your_module_path`**: Replace `your_module_path/shmringbuffer` with the actual Go module path where you have this package (e.g., `github.com/yourusername/yourproject/shmringbuffer`). If it's a local module, it might be `myproject/shmringbuffer`.

## System V IPC Cleanup

System V shared memory segments and semaphores persist in the system until explicitly removed. If a program crashes before cleaning up, these resources can be orphaned.

-   **List IPC resources**:
    ```bash
    ipcs -m # List shared memory segments
    ipcs -s # List semaphores
    ```

-   **Remove IPC resources**:
    ```bash
    ipcrm -m <shm_id>   # Remove shared memory segment by ID
    ipcrm -s <sem_id>   # Remove semaphore set by ID
    # To remove by key (less common for unnamed semaphores used here with SHM)
    # ipcrm -M <shm_key>
    ```
    The `shmKey` used in the Go examples corresponds to the System V key. The `shm_id` and `sem_id` are internal identifiers assigned by the kernel. The `Destroy()` method in the Go package handles removal of the SHM segment (which also removes associated unnamed semaphores if they were created within that segment and `rb_destroy_semaphores` is effective before `shmctl IPC_RMID`). For System V semaphores created with `semget`, they would need separate removal. Our current implementation places semaphores *within* the shared memory segment created by `shmget` and uses `sem_init` for unnamed semaphores, so `rb_destroy_semaphores` followed by `shmctl` with `IPC_RMID` on the segment ID should clean them up. If named semaphores were used (`sem_open`), they'd be cleaned with `sem_unlink`.

## Error Handling

The Go package functions return errors for various failure conditions. Check these errors and handle them appropriately in your application. Common errors include issues with shared memory creation/attachment, buffer full/empty (though these are blocking), and reader registration failures.

## Concurrency

The C library uses POSIX semaphores to ensure that writes and reads are synchronized.
-   The writer will block if the buffer is full (i.e., it would overwrite data that the slowest reader has not yet consumed).
-   Readers will block if the buffer is empty (i.e., no new data is available for that specific reader).

Each reader maintains its own read pointer, allowing readers to consume data at different rates.

## Limitations / Future Considerations

-   **Error Propagation**: Detailed error codes from C functions could be more explicitly propagated to Go errors.
-   **Named Semaphores**: For systems where unnamed semaphores in SHM are problematic or for different IPC paradigms, named semaphores could be an alternative.
-   **Windows Support**: This implementation relies on POSIX-specific features (System V SHM, POSIX unnamed semaphores) and is not directly compatible with Windows. A Windows version would require using Windows-specific IPC mechanisms (e.g., File Mapping, Events/Mutexes).
-   **Buffer Size Discovery**: Attaching processes currently need to know the `dataBufferSize`. This could be enhanced by storing it more reliably in a part of the header accessible even before full ring buffer init, or using a fixed metadata segment. (The current implementation *does* store it and `Attach` verifies it).
-   **Dynamic Resizing**: The buffer size is fixed at creation. Dynamic resizing of shared memory segments is complex and not supported.
