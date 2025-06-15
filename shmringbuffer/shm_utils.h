#ifndef SHM_UTILS_H
#define SHM_UTILS_H

#include <sys/types.h> // For key_t
#include <stddef.h>    // For size_t

// Creates a new shared memory segment or gets an existing one.
// Returns shm_id on success, -1 on error.
int create_shared_memory(key_t key, size_t size);

// Attaches to an existing shared memory segment.
// Returns a pointer to the attached memory on success, NULL on error.
void* attach_shared_memory(int shm_id, size_t size); // Added size for shmat validation

// Detaches from a shared memory segment.
// Returns 0 on success, -1 on error.
int detach_shared_memory(const void *shm_addr);

// Destroys a shared memory segment.
// Returns 0 on success, -1 on error.
int destroy_shared_memory(int shm_id);

#endif // SHM_UTILS_H
