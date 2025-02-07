#include "common.h"
#define MINORBITS 20
#define MINOR(dev) ((unsigned int)((dev) & ((1U << MINORBITS) - 1)))
#define MAJOR(dev) ((unsigned int)((dev) >> MINORBITS))

char __license[] SEC("license") = "Dual MIT/GPL";

struct event {
    // u64 pid;
    // u64 bi_sector;
    u64 block_start;
    u64 block_end;
    // u32 major;
    // u32 minor;
};

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 24);
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __type(key, u32);
    __type(value, u32);
    __uint(max_entries, 2);
} target_disk_map SEC(".maps");

// Force emitting struct event into the ELF.
const struct event *unused __attribute__((unused));


SEC("tracepoint/block/block_rq_complete")
int block_rq_complete(struct trace_event_raw_block_rq_completion *ctx) {
    u64 id = bpf_get_current_pid_tgid();
    u32 tgid = id >> 32;
    u32 major_key = 0; // index 0 for major number
    u32 minor_key = 1; // index 1 for minor number
    unsigned int *target_major = bpf_map_lookup_elem(&target_disk_map, &major_key);
    unsigned int *target_minor = bpf_map_lookup_elem(&target_disk_map, &minor_key);
    unsigned int major = MAJOR(ctx->dev);
    unsigned int minor = MINOR(ctx->dev);
    char local_rwbs[sizeof(ctx->rwbs)];
    bpf_probe_read_str(&local_rwbs, sizeof(local_rwbs), ctx->rwbs);

    if (!target_major || !target_minor) {
        return 0; // If values are not present in the map
    }
    if (major == *target_major && minor == *target_minor) {
        // Manually check if 'W' is in the rwbs string
        int found_w = 0;
        for (int i = 0; i < sizeof(local_rwbs); i++) {
            if (local_rwbs[i] == 'W') {
                found_w = 1;
                break;
            }
        }
        if (!found_w) {
            return 0; // Exit if there is no write operation
        }
        struct event *task_info = bpf_ringbuf_reserve(&events, sizeof(struct event), 0);
        if (!task_info) {
            return 0;
        }
        
        task_info->block_start = ctx->sector;
        task_info->block_end = ctx->sector + ctx->nr_sector;
        // task_info->pid = tgid;
        // task_info->bi_sector = ctx->sector;
        // task_info->major = major;
        // task_info->minor = minor;

        bpf_ringbuf_submit(task_info, 0);
    }

    return 0;
}