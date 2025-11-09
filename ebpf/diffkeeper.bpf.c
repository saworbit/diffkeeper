#include "vmlinux.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

char LICENSE[] SEC("license") = "Dual BSD/GPL";

struct syscall_event {
    __u32 pid;
    __u64 bytes;
    char path[256];
};

struct lifecycle_event {
    __u32 pid;
    __u32 state;
    char runtime[16];
    char namespace[64];
    char container[64];
};

struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
} events SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 1 << 20);
} lifecycle_events SEC(".maps");

static __always_inline int emit_syscall_event(struct pt_regs *ctx, struct file *file, size_t count) {
    struct syscall_event ev = {};
    struct path path = {};

    if (!file) {
        return 0;
    }

    ev.pid = bpf_get_current_pid_tgid() >> 32;
    ev.bytes = count;

    bpf_core_read(&path, sizeof(path), &file->f_path);
    if (bpf_d_path(&path, ev.path, sizeof(ev.path)) < 0) {
        struct dentry *dentry = BPF_CORE_READ(file, f_path.dentry);
        const unsigned char *name = BPF_CORE_READ(dentry, d_name.name);
        bpf_probe_read_kernel_str(ev.path, sizeof(ev.path), name);
    }

    return bpf_perf_event_output(ctx, &events, BPF_F_CURRENT_CPU, &ev, sizeof(ev));
}

SEC("kprobe/vfs_write")
int BPF_KPROBE(kprobe_vfs_write, struct file *file, const char *buf, size_t count, loff_t *pos) {
    return emit_syscall_event(ctx, file, count);
}

SEC("kprobe/vfs_writev")
int BPF_KPROBE(kprobe_vfs_writev, struct kiocb *iocb, struct iovec *iov, unsigned long nr_segs) {
    struct file *file = BPF_CORE_READ(iocb, ki_filp);
    size_t total = 0;
    for (unsigned long i = 0; i < nr_segs; i++) {
        size_t len = BPF_CORE_READ(&iov[i], iov_len);
        total += len;
    }
    return emit_syscall_event(ctx, file, total);
}

SEC("kprobe/vfs_pwritev")
int BPF_KPROBE(kprobe_vfs_pwritev, struct file *file, struct iovec *iov, unsigned long nr_segs, loff_t *pos) {
    size_t total = 0;
    for (unsigned long i = 0; i < nr_segs; i++) {
        size_t len = BPF_CORE_READ(&iov[i], iov_len);
        total += len;
    }
    return emit_syscall_event(ctx, file, total);
}

SEC("tracepoint/sched/sched_process_exec")
int handle_sched_exec(struct trace_event_raw_sched_process_exec *ctx) {
    struct lifecycle_event *event;

    event = bpf_ringbuf_reserve(&lifecycle_events, sizeof(*event), 0);
    if (!event) {
        return 0;
    }

    event->pid = bpf_get_current_pid_tgid() >> 32;
    event->state = 1; // create/start

    bpf_get_current_comm(event->runtime, sizeof(event->runtime));
    bpf_probe_read_user_str(event->container, sizeof(event->container), ctx->filename);

    bpf_ringbuf_submit(event, 0);
    return 0;
}
