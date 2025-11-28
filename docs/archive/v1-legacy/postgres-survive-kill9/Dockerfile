FROM ghcr.io/saworbit/diffkeeper:latest AS diffkeeper

# Debian-based Postgres to match the glibc-linked agent binary
FROM postgres:16

# Pull the exact agent binary we ship so the demo matches production
COPY --from=diffkeeper /usr/local/bin/diffkeeper /usr/local/bin/diffkeeper

# Optional metrics for the verification step
EXPOSE 9911

# Wrap the official postgres entrypoint with DiffKeeper
ENTRYPOINT ["diffkeeper", \
  "--state-dir=/var/lib/postgresql/data", \
  "--store=/deltas/diffkeeper.bolt", \
  "--snapshot-interval=15", \
  "--metrics-addr=:9911", \
  "--", \
  "docker-entrypoint.sh"]
CMD ["postgres"]
