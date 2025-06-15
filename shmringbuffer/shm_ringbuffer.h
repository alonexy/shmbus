#ifndef SHM_RINGBUFFER_H
#define SHM_RINGBUFFER_H

#include <stddef.h>    // For size_t
#include <sys/types.h> // For pid_t
#include <semaphore.h> // For sem_t

#define MAX_READERS 10
#define MAGIC_NUMBER 0xDEADBEEF

typedef struct {
    pid_t pid;
    size_t read_offset;
    int active;
} ReaderInfo;

typedef struct {
    size_t buffer_size;
    size_t write_offset;
    int reader_count;
    unsigned int magic;

    sem_t sem_mutex;
    sem_t sem_filled_slots;
    sem_t sem_empty_slots;

} RingBufferHeader;

#endif // SHM_RINGBUFFER_H
