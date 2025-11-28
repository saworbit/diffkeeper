# Genesis & Pivot: From "Stateful Containers" to "Time Travel Debugging"

## The Original Vision: "Git for Runtime State"
DiffKeeper began with a seductive premise: **What if containers could be ephemeral, but their state could be immortal?**

We built a sophisticated engine to achieve this:
1.  **eBPF Interception:** We hooked into the Linux kernel to watch `vfs_write` calls in real-time.
2.  **Binary Diffs:** We used `bsdiff` to capture only the changed bytes, not full files.
3.  **Content-Addressable Storage (CAS):** We deduplicated everything to save space.

The goal was to let a Postgres database survive a `kill -9` instantly, without external volume mounts. We wanted to decouple "compute" from "storage" at the process level.

## The "Failure": Why it didn't work for Databases
As we benchmarked and pushed the system, we hit a hard reality. We were trying to re-implement a filesystem in userspace, and that is a dangerous place for ACID-compliant applications.

1.  **The Blast Radius:** Intercepting every write from a high-throughput database (like Redis or Postgres) introduced unacceptable latency.
2.  **The Consistency Trap:** Databases spend decades optimizing how they flush data to disk. By intercepting writes and processing them in a Go agent, we risked corrupting the Write-Ahead Log (WAL) if our agent crashed or the ring buffer overflowed.
3.  **The Wrong Tool:** We used BoltDB (a read-optimized B+Tree) for a write-heavy workload. It choked under pressure.

We built a Ferrari engine (eBPF + Binary Diffs) and put it in a tractor (Database Persistence).

## The Pivot: The "Time Machine"
In reviewing our "failure," we realized our architecture had accidental superpowers.
* **Determinism:** We had a perfect, timestamped log of every filesystem change.
* **Efficiency:** Storing 100 versions of a binary is cheap because of our diffing engine.

We realized the problem isn't "saving state for production"—it's **"seeing state for debugging."**

In CI/CD pipelines, flaky tests are a nightmare. When a container crashes in a CI runner, the state is lost. Developers are left guessing.

**DiffKeeper is now the "Black Box Flight Recorder" for Kubernetes.**
We don't try to keep your database alive. We record its death so you can replay it, rewind it, and fix it.

## The Future
We are moving from:
* **Goal:** Persistence -> **Observability**
* **Storage:** BoltDB -> **Pebble (LSM Tree)**
* **Use Case:** Production Databases -> **CI/CD & Forensics**

We are building the **Time Machine for Kubernetes**.
