#ifndef RINGBUFFER_H
#define RINGBUFFER_H

#include "shm_ringbuffer.h"
#include <stddef.h>
#include <sys/types.h> // For ssize_t

static inline ReaderInfo* get_reader_info_array(void* shm_addr) {
    return (ReaderInfo*)((char*)shm_addr + sizeof(RingBufferHeader));
}

static inline char* get_data_buffer(void* shm_addr) {
    return (char*)shm_addr + sizeof(RingBufferHeader) + (sizeof(ReaderInfo) * MAX_READERS);
}

int rb_init(void* shm_addr, size_t buffer_data_size);
int rb_register_reader(void* shm_addr);
int rb_unregister_reader(void* shm_addr, int reader_id);
ssize_t rb_write(void* shm_addr, const void* data, size_t len);
ssize_t rb_read(void* shm_addr, int reader_id, void* buffer, size_t len);
int rb_destroy_semaphores(void* shm_addr);

#endif // RINGBUFFER_H
