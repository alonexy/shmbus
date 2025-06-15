// Package shmringbuffer provides a high-performance, inter-process communication (IPC)
// mechanism using a shared memory ring buffer. It supports a single writer process
// and multiple reader processes. The underlying implementation is in C, utilizing
// POSIX semaphores for synchronization and System V shared memory segments,
// exposed to Go via CGo.
package shmringbuffer

/*
#cgo CFLAGS: -I.
#cgo LDFLAGS: -L. -lshmringbuffer -lpthread

// The LDFLAGS above instruct CGo to link against libshmringbuffer.so.
// -L. adds the current directory (where shmringbuffer.go resides, and where
// libshmringbuffer.so is expected to be placed by the Makefile) to the library search path.
// -lshmringbuffer links against the library named 'shmringbuffer' (libshmringbuffer.so).
// -lpthread is still needed for the POSIX semaphore functions used by the C library.

// Standard C headers needed for CGo to interact with C types or error handling.
#include <errno.h>   // For errno, used by C.GoString(C.strerror(C.int(*C.__errno_location())))
#include <string.h>  // For strerror
#include <stdio.h>   // For C.stderr, if used directly in Go for debug prints from C context

// cgo_wrapper.h contains all CGo_* function declarations.
// This header must be self-contained or include other necessary headers
// (like sys/types.h for key_t, stddef.h for size_t) for the function
// signatures it declares, so CGo can understand the C API surface.
// Our cgo_wrapper.h is set up this way.
#include "cgo_wrapper.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	// ErrShmGetIdFailed indicates a failure in obtaining a shared memory segment ID (shmget).
	ErrShmGetIdFailed      = errors.New("shmringbuffer: failed to get shared memory ID")
	// ErrShmAttachFailed indicates a failure in attaching (mapping) the shared memory segment (shmat).
	ErrShmAttachFailed     = errors.New("shmringbuffer: failed to attach to shared memory")
	// ErrShmInitFailed indicates a failure to initialize the ring buffer metadata within the shared memory.
	ErrShmInitFailed       = errors.New("shmringbuffer: failed to initialize ring buffer metadata in shared memory")
	// ErrShmDetachFailed indicates a failure in detaching (unmapping) the shared memory segment (shmdt).
	ErrShmDetachFailed     = errors.New("shmringbuffer: failed to detach from shared memory")
	// ErrShmDestroyFailed indicates a failure in destroying the shared memory segment (shmctl IPC_RMID).
	ErrShmDestroyFailed    = errors.New("shmringbuffer: failed to destroy shared memory segment")
	// ErrSemDestroyFailed indicates a failure to destroy the POSIX semaphores associated with the ring buffer.
	ErrSemDestroyFailed    = errors.New("shmringbuffer: failed to destroy semaphores")
	// ErrRegisterReaderFailed indicates a failure when trying to register a new reader (e.g., no slots available).
	ErrRegisterReaderFailed= errors.New("shmringbuffer: failed to register reader")
	// ErrUnregisterRdFailed indicates a failure when trying to unregister an existing reader.
	ErrUnregisterRdFailed  = errors.New("shmringbuffer: failed to unregister reader")
	// ErrWriteFailed indicates a failure during a write operation to the ring buffer.
	ErrWriteFailed         = errors.New("shmringbuffer: write operation failed")
	// ErrReadFailed indicates a failure during a read operation from the ring buffer.
	ErrReadFailed          = errors.New("shmringbuffer: read operation failed")
	// ErrNotInitialized indicates that the shared memory segment does not appear to be a valid or initialized ring buffer (e.g., magic number mismatch, size mismatch).
	ErrNotInitialized      = errors.New("shmringbuffer: shared memory not initialized (magic number mismatch or other issue)")
	// ErrReaderNotActive indicates that an operation was attempted on a reader that is not active or on a ring buffer that is not attached.
	ErrReaderNotActive     = errors.New("shmringbuffer: reader is not active or buffer not attached")
	// ErrDataSizeTooLarge indicates that a write operation was attempted with a data payload larger than the ring buffer's configured data capacity.
	ErrDataSizeTooLarge    = errors.New("shmringbuffer: data size too large for buffer capacity")
	// ErrInvalidKey indicates that the provided System V IPC key was invalid (e.g., zero).
	ErrInvalidKey          = errors.New("shmringbuffer: key must be positive")
)

// ShmRingBuffer represents a shared memory ring buffer.
// It encapsulates the shared memory segment ID, mapped address, and buffer configuration.
// Instances should be created using Create() or Attach().
type ShmRingBuffer struct {
	key            C.key_t        // System V IPC key for the shared memory segment.
	shmID          C.int          // Shared memory segment ID.
	shmAddr        unsafe.Pointer // Pointer to the mapped shared memory address.
	dataBufferSize uint64         // Size of the usable data portion of the ring buffer.
	totalShmSize   uint64         // Total size of the allocated shared memory segment.
	isCreator      bool           // True if this instance created the shared memory segment.
}

// Reader represents a single reader attached to a ShmRingBuffer.
// Each reader maintains its own read offset within the ring buffer.
// Instances are obtained by calling RegisterReader() on a ShmRingBuffer.
type Reader struct {
	rb       *ShmRingBuffer // Pointer to the parent ring buffer.
	readerID C.int          // Internal ID assigned to this reader.
	active   bool           // True if this reader is currently registered and active.
}

// calculateTotalShmSize computes the total shared memory size required, including
// the header, reader information array, and the actual data buffer.
func calculateTotalShmSize(dataBufferSize uint64) (uint64, error) {
	if dataBufferSize == 0 {
		return 0, errors.New("dataBufferSize cannot be zero")
	}
	headerSize := uint64(C.CGo_GetHeaderSize())
	readerInfoArraySize := uint64(C.CGo_GetReaderInfoArraySize())
	if headerSize == 0 || readerInfoArraySize == 0 {
	    return 0, errors.New("CGO helper functions for size returned zero")
	}
	return headerSize + readerInfoArraySize + dataBufferSize, nil
}

// Create initializes a new shared memory segment and sets up the ring buffer structure within it.
// It should be called by only one process (the creator/writer).
//
// Parameters:
//   key: A positive System V IPC key, unique for this shared memory segment.
//   dataBufferSize: The desired size, in bytes, for the actual data storage area of the ring buffer. Must be greater than zero.
//
// Returns:
//   A pointer to an initialized ShmRingBuffer instance on success.
//   An error if shared memory allocation or ring buffer initialization fails. Common errors include
//   ErrInvalidKey, ErrShmGetIdFailed, ErrShmAttachFailed, or ErrShmInitFailed.
func Create(key uint32, dataBufferSize uint64) (*ShmRingBuffer, error) {
	if key == 0 {
		return nil, ErrInvalidKey
	}
	totalSize, err := calculateTotalShmSize(dataBufferSize)
	if err != nil {
		return nil, fmt.Errorf("shmringbuffer: %w", err)
	}
	cKey := C.key_t(key)

	shmID := C.CGo_ShmGetId(cKey, C.size_t(totalSize))
	if shmID == -1 {
		return nil, fmt.Errorf("%w: %s", ErrShmGetIdFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}

	shmAddr := C.CGo_ShmAttach(shmID)
	if shmAddr == nil {
		C.CGo_ShmDestroy(shmID)
		return nil, fmt.Errorf("%w: %s", ErrShmAttachFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}

	ret := C.CGo_ShmRingBufferInit(shmAddr, C.size_t(dataBufferSize))
	if ret == -1 {
		C.CGo_ShmRingBufferDetach(shmAddr)
		C.CGo_ShmRingBufferDestroySemaphores(shmAddr)
		C.CGo_ShmDestroy(shmID)
		return nil, fmt.Errorf("%w (rb_init failed)", ErrShmInitFailed)
	}

	return &ShmRingBuffer{
		key:            cKey,
		shmID:          shmID,
		shmAddr:        shmAddr,
		dataBufferSize: dataBufferSize,
		totalShmSize:   totalSize,
		isCreator:      true,
	}, nil
}

// Attach connects to an existing shared memory ring buffer created by another process.
//
// Parameters:
//   key: The System V IPC key used by the creator.
//   expectedDataBufferSize: The data buffer size that the attaching process expects.
//                           This is verified against the actual size stored in the shared memory header.
//
// Returns:
//   A pointer to a ShmRingBuffer instance mapped to the existing segment on success.
//   An error if attachment fails, the segment is not initialized, or if the
//   expectedDataBufferSize does not match the actual size. Common errors include
//   ErrInvalidKey, ErrShmGetIdFailed, ErrShmAttachFailed, or ErrNotInitialized.
func Attach(key uint32, expectedDataBufferSize uint64) (*ShmRingBuffer, error) {
	if key == 0 {
		return nil, ErrInvalidKey
	}
	totalSize, err := calculateTotalShmSize(expectedDataBufferSize) // totalSize is used for shmget call consistency
	if err != nil {
		return nil, fmt.Errorf("shmringbuffer: %w", err)
	}
	cKey := C.key_t(key)

	shmID := C.CGo_ShmGetId(cKey, C.size_t(totalSize))
	if shmID == -1 {
		return nil, fmt.Errorf("%w: %s", ErrShmGetIdFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}

	shmAddr := C.CGo_ShmAttach(shmID)
	if shmAddr == nil {
		return nil, fmt.Errorf("%w: %s", ErrShmAttachFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}

	if C.CGo_VerifyMagicNumber(shmAddr) != 0 {
		C.CGo_ShmRingBufferDetach(shmAddr)
		return nil, ErrNotInitialized
	}

    headerPtr := (*C.RingBufferHeader)(shmAddr)
    if uint64(headerPtr.buffer_size) != expectedDataBufferSize {
        C.CGo_ShmRingBufferDetach(shmAddr)
        return nil, fmt.Errorf("%w: expected dataBufferSize %d, found %d in shared memory",
            ErrNotInitialized, expectedDataBufferSize, uint64(headerPtr.buffer_size))
    }

	return &ShmRingBuffer{
		key:            cKey,
		shmID:          shmID,
		shmAddr:        shmAddr,
		dataBufferSize: expectedDataBufferSize,
		totalShmSize:   totalSize,
		isCreator:      false,
	}, nil
}

// Detach unmaps the shared memory segment from the current process's address space.
// It does not destroy the shared memory segment itself.
// After detaching, the ShmRingBuffer instance should not be used further unless re-attached.
//
// Returns:
//   An error (ErrShmDetachFailed) if detachment fails.
//   Nil on success.
func (rb *ShmRingBuffer) Detach() error {
	if rb.shmAddr == nil {
		return errors.New("shmringbuffer: not attached")
	}
	ret := C.CGo_ShmRingBufferDetach(rb.shmAddr)
	if ret == -1 {
		return fmt.Errorf("%w: %s", ErrShmDetachFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}
	rb.shmAddr = nil // Mark as detached
	return nil
}

// Destroy removes the shared memory segment and its associated semaphores from the system.
// This method should typically be called only by the process that created the segment
// or by a designated cleanup utility.
// It first attempts to destroy the semaphores, then detaches the memory segment (if attached),
// and finally requests the system to remove the shared memory segment ID.
//
// Returns:
//   An error (ErrShmDestroyFailed) if removing the segment ID fails.
//   Warnings may be printed to C.stderr if semaphore destruction or detachment fails,
//   but the method will still attempt to destroy the segment ID.
//   Nil on successful marking for destruction.
func (rb *ShmRingBuffer) Destroy() error {
	if rb.shmAddr != nil {
		if C.CGo_ShmRingBufferDestroySemaphores(rb.shmAddr) == -1 {
			// Warning only, attempt to continue cleanup
			C.fprintf(C.stderr, C.CString("shmringbuffer: warning: CGo_ShmRingBufferDestroySemaphores failed: %s\n"), C.strerror(C.int(*C.__errno_location())))
		}
		// Detach before destroying by ID. This process won't use it anymore.
		if err := rb.Detach(); err != nil {
			C.fprintf(C.stderr, C.CString("shmringbuffer: warning: Detach failed during Destroy: %s\n"), C.CString(err.Error()))
		}
	}
	// If shmAddr is nil, but we have an shmID (e.g., from a previous attach or if creator detached early),
	// still attempt to destroy by ID.

	if C.CGo_ShmDestroy(rb.shmID) == -1 {
		return fmt.Errorf("%w: %s", ErrShmDestroyFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}

	rb.shmAddr = nil // Ensure it's marked as unusable
	rb.isCreator = false // No longer the creator after destruction
	return nil
}

// Write adds data to the ring buffer.
// If the buffer does not have enough contiguous space for the data (considering the slowest reader),
// this call will block until space becomes available.
//
// Parameters:
//   data: A byte slice containing the data to be written.
//
// Returns:
//   The number of bytes written (should match len(data) on success).
//   An error if the ShmRingBuffer is not attached, if the data length exceeds the buffer's
//   total data capacity (ErrDataSizeTooLarge), or if the underlying C write operation fails (ErrWriteFailed).
//   Returns (0, nil) if an empty data slice is provided.
func (rb *ShmRingBuffer) Write(data []byte) (int, error) {
	if rb.shmAddr == nil {
		return 0, errors.New("shmringbuffer: not attached or already destroyed")
	}
	if len(data) == 0 {
		return 0, nil
	}
	if uint64(len(data)) > rb.dataBufferSize {
	    return 0, ErrDataSizeTooLarge
	}

	var dataPtr unsafe.Pointer
	if len(data) > 0 {
		dataPtr = unsafe.Pointer(&data[0])
	}

	bytesWrittenC := C.CGo_ShmRingBufferWrite(rb.shmAddr, dataPtr, C.size_t(len(data)))

	if bytesWrittenC < 0 {
		// The C layer's rb_write currently returns -1 for general errors or -2 for length > capacity.
		// The ErrDataSizeTooLarge is checked above. So, any negative here is likely ErrWriteFailed.
		return 0, fmt.Errorf("%w (C code returned %d)", ErrWriteFailed, bytesWrittenC)
	}
	return int(bytesWrittenC), nil
}

// RegisterReader registers the calling process/goroutine as a new reader for the ring buffer.
// Each reader gets a unique ID and maintains its own read offset.
//
// Returns:
//   A pointer to a Reader instance on success.
//   An error (ErrRegisterReaderFailed) if no reader slots are available or if the
//   ShmRingBuffer is not attached.
func (rb *ShmRingBuffer) RegisterReader() (*Reader, error) {
	if rb.shmAddr == nil {
		return nil, errors.New("shmringbuffer: not attached or already destroyed")
	}
	readerIDC := C.CGo_ShmRingBufferRegisterReader(rb.shmAddr)
	if readerIDC < 0 {
		return nil, ErrRegisterReaderFailed
	}
	return &Reader{rb: rb, readerID: readerIDC, active: true}, nil
}

// Read consumes data from the ring buffer for this specific reader.
// If no data is available for this reader, the call will block until data is written.
// Data is copied into the provided `buffer` slice.
//
// Parameters:
//   buffer: A byte slice into which data will be read. The number of bytes read will
//           be at most len(buffer).
//
// Returns:
//   The number of bytes read into `buffer`.
//   An error if the reader is not active, the ShmRingBuffer is not attached,
//   or if the underlying C read operation fails (ErrReadFailed).
//   Returns (0, nil) if a zero-length buffer is provided.
func (r *Reader) Read(buffer []byte) (int, error) {
	if !r.active || r.rb.shmAddr == nil { // Check parent buffer's status via r.rb
		return 0, ErrReaderNotActive
	}
	if len(buffer) == 0 {
		return 0, nil
	}

	var bufferPtr unsafe.Pointer
	if len(buffer) > 0 {
	    bufferPtr = unsafe.Pointer(&buffer[0])
	}

	bytesReadC := C.CGo_ShmRingBufferRead(r.rb.shmAddr, r.readerID, bufferPtr, C.size_t(len(buffer)))

	if bytesReadC < 0 {
		return 0, fmt.Errorf("%w (C code returned %d)", ErrReadFailed, bytesReadC)
	}
	return int(bytesReadC), nil
}

// Close unregisters the reader from the ring buffer.
// This marks the reader slot as available for new registrations.
// It's important to close readers when they are no longer needed to free up resources.
//
// Returns:
//   An error (ErrUnregisterRdFailed) if unregistration fails at the C level.
//   Nil on success or if the reader was already inactive.
func (r *Reader) Close() error {
	if !r.active {
		return nil
	}
	// Check parent buffer status before attempting to use r.rb.shmAddr
	if r.rb == nil || r.rb.shmAddr == nil {
	    r.active = false
	    // Don't return error if parent is gone, just mark inactive.
	    // The C call would fail anyway.
	    return nil
	}

	ret := C.CGo_ShmRingBufferUnregisterReader(r.rb.shmAddr, r.readerID)
	r.active = false // Mark as inactive regardless of C call result for Go state
	if ret < 0 {
		return ErrUnregisterRdFailed
	}
	return nil
}
