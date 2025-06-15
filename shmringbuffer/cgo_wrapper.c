#include "shm_utils.h"
#include "ringbuffer.h"
// Note: shm_ringbuffer.h is included via ringbuffer.h

#include <stddef.h>    // For size_t
#include <sys/types.h> // For key_t
#include <stdio.h>     // For NULL

// Shared Memory Management Wrappers
int CGo_ShmGetId(key_t key, size_t total_shm_size) {
    return create_shared_memory(key, total_shm_size);
}

void* CGo_ShmAttach(int shm_id) {
    return attach_shared_memory(shm_id, 0);
}

int CGo_ShmRingBufferInit(void* shm_addr, size_t ring_buffer_data_size) {
    if (!shm_addr) return -1;
    return rb_init(shm_addr, ring_buffer_data_size);
}

int CGo_ShmRingBufferDetach(const void* shm_addr) {
    return detach_shared_memory(shm_addr);
}

int CGo_ShmRingBufferDestroySemaphores(void* shm_addr) {
    if (!shm_addr) return -1;
    return rb_destroy_semaphores(shm_addr);
}

int CGo_ShmDestroy(int shm_id) {
    return destroy_shared_memory(shm_id);
}

// Ring Buffer Operation Wrappers
int CGo_ShmRingBufferRegisterReader(void* shm_addr) {
    if (!shm_addr) return -1;
    return rb_register_reader(shm_addr);
}

int CGo_ShmRingBufferUnregisterReader(void* shm_addr, int reader_id) {
    if (!shm_addr) return -1;
    return rb_unregister_reader(shm_addr, reader_id);
}

long long CGo_ShmRingBufferWrite(void* shm_addr, const void* data, size_t len) {
    if (!shm_addr) return -1LL;
    ssize_t bytes_written = rb_write(shm_addr, data, len);
    return (long long)bytes_written;
}

long long CGo_ShmRingBufferRead(void* shm_addr, int reader_id, void* buffer, size_t len) {
    if (!shm_addr) return -1LL;
    ssize_t bytes_read = rb_read(shm_addr, reader_id, buffer, len);
    return (long long)bytes_read;
}

// Helper functions
unsigned int CGo_GetMagicNumber(void) {
    return MAGIC_NUMBER;
}

int CGo_GetMaxReaders(void) {
    return MAX_READERS;
}

int CGo_VerifyMagicNumber(void* shm_addr) {
    if (!shm_addr) {
        return -1;
    }
    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    if (header->magic == MAGIC_NUMBER) {
        return 0;
    }
    return 1;
}

size_t CGo_GetHeaderSize(void) {
    return sizeof(RingBufferHeader);
}

size_t CGo_GetReaderInfoArraySize(void) {
    return sizeof(ReaderInfo) * MAX_READERS;
}
