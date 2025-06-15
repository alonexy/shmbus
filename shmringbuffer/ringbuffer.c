#include "ringbuffer.h"
#include <string.h> // For memcpy, memset
#include <stdio.h>  // For perror, fprintf
#include <unistd.h> // For getpid()
#include <errno.h>  // For errno

int rb_init(void* shm_addr, size_t buffer_data_size) {
    if (!shm_addr) return -1;

    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    header->buffer_size = buffer_data_size;
    header->write_offset = 0;
    header->reader_count = 0;
    header->magic = MAGIC_NUMBER;

    if (sem_init(&header->sem_mutex, 1, 1) == -1) {
        perror("sem_init (sem_mutex)");
        return -1;
    }
    if (sem_init(&header->sem_filled_slots, 1, 0) == -1) {
        perror("sem_init (sem_filled_slots)");
        sem_destroy(&header->sem_mutex);
        return -1;
    }
    // Initialize empty_slots to the actual number of byte slots available
    if (sem_init(&header->sem_empty_slots, 1, buffer_data_size) == -1) {
        perror("sem_init (sem_empty_slots)");
        sem_destroy(&header->sem_mutex);
        sem_destroy(&header->sem_filled_slots);
        return -1;
    }

    ReaderInfo* readers = get_reader_info_array(shm_addr);
    for (int i = 0; i < MAX_READERS; ++i) {
        readers[i].pid = -1;
        readers[i].read_offset = 0;
        readers[i].active = 0;
    }
    return 0;
}

int rb_destroy_semaphores(void* shm_addr) {
    if (!shm_addr) return -1;
    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    int ret = 0;
    if (sem_destroy(&header->sem_mutex) == -1) {
        perror("sem_destroy (sem_mutex)");
        ret = -1;
    }
    if (sem_destroy(&header->sem_filled_slots) == -1) {
        perror("sem_destroy (sem_filled_slots)");
        ret = -1;
    }
    if (sem_destroy(&header->sem_empty_slots) == -1) {
        perror("sem_destroy (sem_empty_slots)");
        ret = -1;
    }
    return ret;
}

int rb_register_reader(void* shm_addr) {
    if (!shm_addr) return -1;
    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    if (header->magic != MAGIC_NUMBER) {
        fprintf(stderr, "rb_register_reader: Magic number mismatch!\n");
        return -1;
    }

    sem_wait(&header->sem_mutex);
    ReaderInfo* readers = get_reader_info_array(shm_addr);
    int reader_id = -1;
    for (int i = 0; i < MAX_READERS; ++i) {
        if (!readers[i].active) {
            readers[i].active = 1;
            readers[i].pid = getpid();
            readers[i].read_offset = header->write_offset; // Start reading from where writer is currently
            header->reader_count++;
            reader_id = i;
            break;
        }
    }
    sem_post(&header->sem_mutex);

    if (reader_id == -1) {
        fprintf(stderr, "rb_register_reader: No free reader slots.\n");
    }
    return reader_id;
}

int rb_unregister_reader(void* shm_addr, int reader_id) {
    if (!shm_addr) return -1;
    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    if (header->magic != MAGIC_NUMBER) {
        fprintf(stderr, "rb_unregister_reader: Magic number mismatch!\n");
        return -1;
    }

    sem_wait(&header->sem_mutex);
    ReaderInfo* readers = get_reader_info_array(shm_addr);
    if (reader_id < 0 || reader_id >= MAX_READERS || !readers[reader_id].active) {
        sem_post(&header->sem_mutex);
        fprintf(stderr, "rb_unregister_reader: Invalid reader ID %d or reader not active.\n", reader_id);
        return -1;
    }
    readers[reader_id].active = 0;
    header->reader_count--;
    // Note: If this reader was the "slowest" and holding back sem_empty_slots,
    // the writer might still be blocked until other readers make progress or this
    // reader's unconsumed data is effectively "lapped". The current semaphore logic
    // (sem_empty_slots initialized to buffer_size) doesn't directly tie empty slots
    // to individual reader progress, but rather to overall buffer space.
    // A more complex scheme would be needed to adjust sem_empty_slots based on
    // the minimum readable offset of all active readers. For now, unregistering
    // simply frees the slot.
    sem_post(&header->sem_mutex);
    return 0;
}

ssize_t rb_write(void* shm_addr, const void* data, size_t len) {
    if (!shm_addr || !data || len == 0) return -1;
    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    if (header->magic != MAGIC_NUMBER) {
        fprintf(stderr, "rb_write: Magic number mismatch!\n");
        return -1;
    }
    // It's an error to try to write more than the buffer can hold in one go.
    if (len > header->buffer_size) {
        fprintf(stderr, "rb_write: Write length %zu exceeds buffer capacity %zu.\n", len, header->buffer_size);
        return -2; // Or a specific error code for this condition
    }

    char* data_buffer = get_data_buffer(shm_addr);

    // Acquire 'len' empty slots. Each sem_wait decrements the count of empty slots.
    // This will block if 'len' slots are not available.
    for (size_t i = 0; i < len; ++i) {
        if (sem_wait(&header->sem_empty_slots) == -1) {
            // If interrupted, release any slots already acquired and return error
            if (errno == EINTR) {
                for (size_t j = 0; j < i; ++j) sem_post(&header->sem_empty_slots); // Rollback
                fprintf(stderr, "rb_write: sem_wait(sem_empty_slots) interrupted.\n");
                return -1;
            }
            perror("rb_write: sem_wait(sem_empty_slots) failed");
            // Rollback any acquired slots before erroring out
            for(size_t j = 0; j < i; ++j) sem_post(&header->sem_empty_slots);
            return -1; // Other semaphore error
        }
    }

    // Acquired empty slots, now lock mutex for actual write operation
    sem_wait(&header->sem_mutex);

    size_t part1_len = 0;
    size_t part2_len = 0;
    if (header->write_offset + len <= header->buffer_size) {
        // Fits in one contiguous block
        part1_len = len;
    } else {
        // Wraps around
        part1_len = header->buffer_size - header->write_offset;
        part2_len = len - part1_len;
    }

    memcpy(data_buffer + header->write_offset, data, part1_len);
    if (part2_len > 0) {
        memcpy(data_buffer, (const char*)data + part1_len, part2_len); // Corrected line
    }
    header->write_offset = (header->write_offset + len) % header->buffer_size;

    sem_post(&header->sem_mutex);

    // Signal that 'len' new filled slots are available for readers
    for (size_t i = 0; i < len; ++i) {
        sem_post(&header->sem_filled_slots);
    }

    return len;
}

ssize_t rb_read(void* shm_addr, int reader_id, void* buffer, size_t len) {
    if (!shm_addr || !buffer || len == 0) return 0; // Reading 0 bytes is a success (no-op)
    RingBufferHeader* header = (RingBufferHeader*)shm_addr;
    if (header->magic != MAGIC_NUMBER) {
        fprintf(stderr, "rb_read: Magic number mismatch!\n");
        return -1;
    }

    ReaderInfo* readers_base = get_reader_info_array(shm_addr); // For direct access after checks
    char* data_buffer = get_data_buffer(shm_addr);
    size_t buffer_capacity = header->buffer_size;

    size_t actual_bytes_read_count = 0;

    // We need to acquire the mutex first to safely check reader state and offsets.
    sem_wait(&header->sem_mutex);
    ReaderInfo* current_reader_meta = &readers_base[reader_id];
    if (reader_id < 0 || reader_id >= MAX_READERS || !current_reader_meta->active) {
         sem_post(&header->sem_mutex);
         fprintf(stderr, "rb_read: Invalid reader ID %d or reader not active.\n", reader_id);
         return -1;
    }
    size_t reader_current_offset = current_reader_meta->read_offset;
    size_t writer_current_offset = header->write_offset;
    sem_post(&header->sem_mutex); // Release mutex after getting state

    // Determine how many bytes are actually readable for this reader
    size_t readable_for_this_reader;
    if (writer_current_offset >= reader_current_offset) {
        readable_for_this_reader = writer_current_offset - reader_current_offset;
    } else { // Writer has wrapped around relative to this reader
        readable_for_this_reader = (buffer_capacity - reader_current_offset) + writer_current_offset;
    }

    // If no data is currently available for this reader, wait for at least one byte.
    // This also handles the initial state where readable_for_this_reader might be 0.
    if (readable_for_this_reader == 0) {
        if (sem_wait(&header->sem_filled_slots) == -1) {
            if (errno == EINTR) {
                 fprintf(stderr, "rb_read: sem_wait(sem_filled_slots) interrupted while waiting for initial data.\n");
                 return -1; // Interrupted
            }
            perror("rb_read: sem_wait(sem_filled_slots) for initial data");
            return -1; // Other semaphore error
        }
        // Successfully acquired one slot. Now, re-evaluate readable bytes.
        sem_wait(&header->sem_mutex);
        current_reader_meta = &readers_base[reader_id]; // Re-fetch, state might have changed
         if (reader_id < 0 || reader_id >= MAX_READERS || !current_reader_meta->active) {
             sem_post(&header->sem_mutex);
             sem_post(&header->sem_filled_slots); // Release the slot we took
             fprintf(stderr, "rb_read: Reader became inactive after initial wait.\n");
             return -1;
        }
        reader_current_offset = current_reader_meta->read_offset;
        writer_current_offset = header->write_offset;
        sem_post(&header->sem_mutex);

        if (writer_current_offset >= reader_current_offset) {
            readable_for_this_reader = writer_current_offset - reader_current_offset;
        } else {
            readable_for_this_reader = (buffer_capacity - reader_current_offset) + writer_current_offset;
        }

        // If, after acquiring a slot, there's somehow still no data (should be rare, means another reader got it or complex race)
        // or if the only slot was consumed by another reader before we re-checked.
        if (readable_for_this_reader == 0) {
            sem_post(&header->sem_filled_slots); // Release the slot we couldn't use.
            return 0; // No data to read after all.
        }
        actual_bytes_read_count = 1; // We've accounted for one byte via sem_filled_slots
    }

    // Determine how many bytes we will actually attempt to copy in this call
    size_t bytes_to_copy_this_round = (len < readable_for_this_reader) ? len : readable_for_this_reader;

    // If we already accounted for one byte from sem_filled_slots, and that's all we needed.
    if (actual_bytes_read_count == 1 && bytes_to_copy_this_round == 0) {
        // This case should ideally not be hit if readable_for_this_reader was >0.
        // But as a safeguard, if we intended to read 0 more bytes, release the slot and return.
        sem_post(&header->sem_filled_slots);
        return 0;
    }


    // Acquire any additional filled slots needed beyond the first one (if taken).
    size_t additional_slots_to_acquire = (actual_bytes_read_count == 1) ?
                                          ( (bytes_to_copy_this_round > 0) ? bytes_to_copy_this_round - 1 : 0)
                                          : bytes_to_copy_this_round;

    for (size_t i = 0; i < additional_slots_to_acquire; ++i) {
        if (sem_wait(&header->sem_filled_slots) == -1) {
            if (errno == EINTR) {
                 // Release already acquired slots (including the first one if actual_bytes_read_count is 1)
                 for(size_t j=0; j < i; ++j) sem_post(&header->sem_filled_slots);
                 if(actual_bytes_read_count == 1) sem_post(&header->sem_filled_slots);
                 fprintf(stderr, "rb_read: sem_wait(sem_filled_slots) interrupted while acquiring additional data.\n");
                 return -1; // Interrupted
            }
            perror("rb_read: sem_wait(sem_filled_slots) for additional data");
            // Release already acquired slots
            for(size_t j=0; j < i; ++j) sem_post(&header->sem_filled_slots);
            if(actual_bytes_read_count == 1) sem_post(&header->sem_filled_slots);
            return -1; // Other semaphore error
        }
    }
    actual_bytes_read_count += additional_slots_to_acquire; // Total slots we've "claimed"

    // Now, lock mutex to perform the actual read from the buffer and update reader's offset
    sem_wait(&header->sem_mutex);
    current_reader_meta = &readers_base[reader_id]; // Re-check reader status under mutex
     if (reader_id < 0 || reader_id >= MAX_READERS || !current_reader_meta->active) {
         sem_post(&header->sem_mutex);
         // Release all claimed slots as reader is no longer valid
         for(size_t i=0; i < actual_bytes_read_count; ++i) sem_post(&header->sem_filled_slots);
         fprintf(stderr, "rb_read: Reader became inactive before final read section.\n");
         return -1;
    }

    // Re-evaluate reader_current_offset as it might have been updated by another thread if not using per-reader semaphores
    // (though with a global mutex for offset update, this shouldn't be an issue here)
    reader_current_offset = current_reader_meta->read_offset;
    // writer_current_offset does not need to be re-read here as we only care about reader's view for copying

    // final_bytes_to_copy should be min(requested_len, actual_slots_claimed, what's *still* readable from current_reader_offset to writer_offset)
    // This re-check of readable for this reader under mutex is critical
    size_t final_readable_under_mutex;
    writer_current_offset = header->write_offset; // Get the latest writer_offset
    if (writer_current_offset >= reader_current_offset) {
        final_readable_under_mutex = writer_current_offset - reader_current_offset;
    } else {
        final_readable_under_mutex = (buffer_capacity - reader_current_offset) + writer_current_offset;
    }

    size_t final_bytes_to_copy = (bytes_to_copy_this_round < final_readable_under_mutex) ? bytes_to_copy_this_round : final_readable_under_mutex;
    final_bytes_to_copy = (final_bytes_to_copy < actual_bytes_read_count) ? final_bytes_to_copy : actual_bytes_read_count;


    if (final_bytes_to_copy == 0) {
        sem_post(&header->sem_mutex);
        // Release all claimed slots if we are not actually copying anything
        for(size_t i=0; i < actual_bytes_read_count; ++i) sem_post(&header->sem_filled_slots);
        return 0;
    }

    size_t part1_len = 0;
    size_t part2_len = 0;
    if (reader_current_offset + final_bytes_to_copy <= buffer_capacity) {
        part1_len = final_bytes_to_copy;
    } else {
        part1_len = buffer_capacity - reader_current_offset;
        part2_len = final_bytes_to_copy - part1_len;
    }

    memcpy(buffer, data_buffer + reader_current_offset, part1_len);
    if (part2_len > 0) {
        memcpy((char*)buffer + part1_len, data_buffer, part2_len);
    }
    current_reader_meta->read_offset = (reader_current_offset + final_bytes_to_copy) % buffer_capacity;
    sem_post(&header->sem_mutex);

    // Signal that 'final_bytes_to_copy' slots are now empty
    for (size_t i = 0; i < final_bytes_to_copy; ++i) {
        sem_post(&header->sem_empty_slots);
    }

    // If we claimed more slots than we actually used (due to race condition or len limit), return the unused ones.
    if (actual_bytes_read_count > final_bytes_to_copy) {
        for (size_t i = 0; i < (actual_bytes_read_count - final_bytes_to_copy); ++i) {
            sem_post(&header->sem_filled_slots); // Return to filled_slots as they weren't consumed
        }
    }

    return final_bytes_to_copy;
}
