#include "shm_utils.h"
#include <sys/ipc.h>
#include <sys/shm.h>
#include <stdio.h> // For perror

// Creates a new shared memory segment or gets an existing one.
int create_shared_memory(key_t key, size_t size) {
    // IPC_CREAT: Create the segment if it does not exist.
    // IPC_EXCL: Fail if segment already exists (optional, useful for ensuring creation).
    // We'll use IPC_CREAT only to either create or get existing.
    // Permissions 0666: Read/write for owner, group, and others.
    int shm_id = shmget(key, size, IPC_CREAT | 0666);
    if (shm_id == -1) {
        perror("shmget");
    }
    return shm_id;
}

// Attaches to an existing shared memory segment.
void* attach_shared_memory(int shm_id, size_t size) {
    // shmaddr = NULL: Let the system choose the attachment address.
    // shmflg = 0: No special flags (e.g., read-only).
    void *shm_addr = shmat(shm_id, NULL, 0);
    if (shm_addr == (void *)-1) {
        perror("shmat");
        return NULL;
    }
    // Optional: Verify the size of the attached segment if possible,
    // though shmctl with IPC_STAT is typically used for this before attach.
    // For simplicity, we assume the creator sized it correctly.
    return shm_addr;
}

// Detaches from a shared memory segment.
int detach_shared_memory(const void *shm_addr) {
    if (shmdt(shm_addr) == -1) {
        perror("shmdt");
        return -1;
    }
    return 0;
}

// Destroys a shared memory segment.
// Note: The segment is only actually destroyed when the last process detaches from it.
// This call marks the segment for destruction.
int destroy_shared_memory(int shm_id) {
    if (shmctl(shm_id, IPC_RMID, NULL) == -1) {
        perror("shmctl IPC_RMID");
        return -1;
    }
    return 0;
}
