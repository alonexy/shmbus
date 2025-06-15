#ifndef CGO_WRAPPER_H
#define CGO_WRAPPER_H

#include <sys/types.h> // For key_t
#include <stddef.h>    // For size_t

// Shared Memory Management Wrappers
int CGo_ShmGetId(key_t key, size_t total_shm_size);
void* CGo_ShmAttach(int shm_id);
int CGo_ShmRingBufferInit(void* shm_addr, size_t ring_buffer_data_size);
int CGo_ShmRingBufferDetach(const void* shm_addr);
int CGo_ShmRingBufferDestroySemaphores(void* shm_addr);
int CGo_ShmDestroy(int shm_id);

// Ring Buffer Operation Wrappers
int CGo_ShmRingBufferRegisterReader(void* shm_addr);
int CGo_ShmRingBufferUnregisterReader(void* shm_addr, int reader_id);
long long CGo_ShmRingBufferWrite(void* shm_addr, const void* data, size_t len);
long long CGo_ShmRingBufferRead(void* shm_addr, int reader_id, void* buffer, size_t len);

// Helper functions
unsigned int CGo_GetMagicNumber(void);
int CGo_GetMaxReaders(void);
int CGo_VerifyMagicNumber(void* shm_addr);
size_t CGo_GetHeaderSize(void);
size_t CGo_GetReaderInfoArraySize(void);

#endif // CGO_WRAPPER_H
