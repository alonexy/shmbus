package shmringbuffer

/*
#cgo CFLAGS: -I.
#cgo LDFLAGS: -lpthread

#include <errno.h>   // For errno
#include <string.h>  // For strerror
#include <stdio.h>   // For C.stderr in ShmRingBuffer.Destroy

#include "shm_utils.h"    // For key_t definition
#include "ringbuffer.h"   // Includes shm_ringbuffer.h
#include "cgo_wrapper.h"  // Declarations for CGo_ functions
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	ErrShmGetIdFailed      = errors.New("shmringbuffer: failed to get shared memory ID")
	ErrShmAttachFailed     = errors.New("shmringbuffer: failed to attach to shared memory")
	ErrShmInitFailed       = errors.New("shmringbuffer: failed to initialize ring buffer metadata in shared memory")
	ErrShmDetachFailed     = errors.New("shmringbuffer: failed to detach from shared memory")
	ErrShmDestroyFailed    = errors.New("shmringbuffer: failed to destroy shared memory segment")
	ErrSemDestroyFailed    = errors.New("shmringbuffer: failed to destroy semaphores")
	ErrRegisterReaderFailed= errors.New("shmringbuffer: failed to register reader")
	ErrUnregisterRdFailed  = errors.New("shmringbuffer: failed to unregister reader")
	ErrWriteFailed         = errors.New("shmringbuffer: write operation failed")
	ErrReadFailed          = errors.New("shmringbuffer: read operation failed")
	ErrNotInitialized      = errors.New("shmringbuffer: shared memory not initialized (magic number mismatch or other issue)")
	ErrReaderNotActive     = errors.New("shmringbuffer: reader is not active or buffer not attached")
	ErrDataSizeTooLarge    = errors.New("shmringbuffer: data size too large for buffer capacity")
	ErrInvalidKey          = errors.New("shmringbuffer: key must be positive")
)

// ShmRingBuffer represents the shared memory ring buffer.
type ShmRingBuffer struct {
	key            C.key_t
	shmID          C.int
	shmAddr        unsafe.Pointer
	dataBufferSize uint64
	totalShmSize   uint64
	isCreator      bool
}

// Reader represents a single reader attached to the ShmRingBuffer.
type Reader struct {
	rb       *ShmRingBuffer
	readerID C.int
	active   bool
}

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

// Create initializes a new shared memory segment and the ring buffer within it.
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

// Attach connects to an existing shared memory ring buffer.
func Attach(key uint32, expectedDataBufferSize uint64) (*ShmRingBuffer, error) {
	if key == 0 {
		return nil, ErrInvalidKey
	}
	totalSize, err := calculateTotalShmSize(expectedDataBufferSize)
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

// Detach unmaps the shared memory segment.
func (rb *ShmRingBuffer) Detach() error {
	if rb.shmAddr == nil {
		return errors.New("shmringbuffer: not attached")
	}
	ret := C.CGo_ShmRingBufferDetach(rb.shmAddr)
	if ret == -1 {
		return fmt.Errorf("%w: %s", ErrShmDetachFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}
	rb.shmAddr = nil
	return nil
}

// Destroy removes the shared memory segment and its semaphores.
func (rb *ShmRingBuffer) Destroy() error {
	if rb.shmAddr != nil {
		if C.CGo_ShmRingBufferDestroySemaphores(rb.shmAddr) == -1 {
			C.fprintf(C.stderr, C.CString("shmringbuffer: warning: CGo_ShmRingBufferDestroySemaphores failed: %s\n"), C.strerror(C.int(*C.__errno_location())))
		}
		// System V SHM is marked for destruction by IPC_RMID.
		// Actual removal happens when reference count (attachments) is zero.
		// Detaching here is good practice for this process.
		if err := rb.Detach(); err != nil {
			C.fprintf(C.stderr, C.CString("shmringbuffer: warning: Detach failed during Destroy: %s\n"), C.CString(err.Error()))
		}
	}

	if C.CGo_ShmDestroy(rb.shmID) == -1 {
		return fmt.Errorf("%w: %s", ErrShmDestroyFailed, C.GoString(C.strerror(C.int(*C.__errno_location()))))
	}

	rb.shmAddr = nil
	return nil
}

// Write adds data to the ring buffer.
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
		return 0, fmt.Errorf("%w (C code returned %d)", ErrWriteFailed, bytesWrittenC)
	}
	return int(bytesWrittenC), nil
}

// RegisterReader registers the calling process as a new reader.
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

// Read consumes data from the ring buffer for this reader.
func (r *Reader) Read(buffer []byte) (int, error) {
	if !r.active || r.rb.shmAddr == nil {
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

// Close unregisters the reader.
func (r *Reader) Close() error {
	if !r.active {
		return nil
	}
	if r.rb.shmAddr == nil {
	    r.active = false
	    return errors.New("shmringbuffer: parent buffer not attached or destroyed")
	}

	ret := C.CGo_ShmRingBufferUnregisterReader(r.rb.shmAddr, r.readerID)
	r.active = false
	if ret < 0 {
		return ErrUnregisterRdFailed
	}
	return nil
}
