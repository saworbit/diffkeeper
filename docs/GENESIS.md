# GENESIS

Project "Time Machine" (v2.0) pivots DiffKeeper from BoltDB-backed database persistence to a Pebble-powered CI/CD flight recorder.

Key notes from the strategy document:
- Target audience: junior/mid-level Go developers.
- Goal: convert DiffKeeper from a "Database Persistence" tool (BoltDB) to a "CI/CD Flight Recorder" (Pebble).
- Key constraint: high ingestion speed; we must not block the container's execution.

This snapshot marks the archive of the BoltDB era (tagged `v1.0.0-legacy`) and the groundwork for the Pebble journaling and CAS pipeline.
