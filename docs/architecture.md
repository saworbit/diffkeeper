# Architecture: The Flight Recorder

DiffKeeper is designed to ingest filesystem events at high speed without blocking the application, then process them asynchronously for efficient storage.

## High Level Data Flow

1. **Capture (The Firehose)**
   * **Source:** eBPF probes intercept `vfs_write` and `vfs_create` calls in the kernel.
   * **Ingest:** The Go agent receives these events and immediately appends them to a **Write-Ahead Log (WAL)** using Pebble DB.
   * *Constraint:* This path is latency-sensitive. We do zero processing here.

2. **Processing (The Worker)**
   * A background goroutine wakes up periodically to drain the WAL.
   * **Hashing:** It calculates the SHA256 of the new content.
   * **Deduplication:** It checks if this content already exists in the CAS (Content Addressable Storage).
   * **Diffing:** If the file is a modification of a known previous version, it computes a binary diff (`bsdiff`) to save space.

3. **Storage (Pebble)**
   * We use [Pebble](https://github.com/cockroachdb/pebble) (an LSM tree) as the backing store.
   * **Prefix `l:` (Log):** Raw incoming events (ephemeral).
   * **Prefix `c:` (CAS):** Compressed chunks of file data.
   * **Prefix `m:` (Metadata):** Maps `Path + Timestamp` -> `CAS CID`.

## Design Decisions

### Why Pebble?
We moved from BoltDB (B+Tree) to Pebble (LSM Tree) because our workload is 99% writes. LSM trees handle high-throughput ingestion significantly better than B+Trees, ensuring the "Recorder" doesn't slow down the application.

### Binary Diffs
We use `bsdiff` because CI/CD artifacts change very little between runs. Storing a 100MB binary 50 times is expensive. Storing it once plus 49 small patches is efficient.
