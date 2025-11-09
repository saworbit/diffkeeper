# Patent & Prior-Art Notes

DiffKeeper v2.0 introduces two novel elements that may intersect with patent disclosures:

1. **Adaptive eBPF Profiling** – lightweight EMA-based prediction of "hot paths" that dynamically reprograms eBPF filters and CAS priorities. Prior art exists for adaptive sampling in tracing systems (e.g., Facebook's Scribe, Uber's Jaeger), but applying the technique specifically to filesystem diff capture appears unclaimed.
2. **Lifecycle-Driven Auto-Injection** – using kernel tracepoints on CRI runtimes to inject an agent without wrapping container commands. Comparable ideas exist in Kubernetes mutating webhooks and Falco's runtime security hooks, yet the combination with a diffing sidecar is unique.

We recommend recording design history (this repository), publishing blog posts, or filing a defensive publication if patentability is a concern. Until then, contributors should:

- Reference existing OSS work (Falco, Cilium Tetragon) when expanding lifecycle tracing.
- Avoid incorporating third-party eBPF snippets unless their licenses are compatible with Apache-2.0.
- Discuss any planned patent filings on the project RFC list to keep the community aligned.
