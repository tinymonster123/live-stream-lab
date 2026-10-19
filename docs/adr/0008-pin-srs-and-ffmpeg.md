# Pin SRS 6.0-r2 and a Homebrew FFmpeg for the first slices

Status: accepted

Use SRS `v6.0-r2` (stable) and a Homebrew FFmpeg binary for M1. Build FFmpeg `n8.1.3` from source only when custom flags such as SRT or additional codecs are needed. The local `../srs` develop checkout and `../FFmpeg` `n9.1-dev` are for reading and experiments, not for the running system.

## Consequences

- The running SRS version is reproducible and stable.
- FFmpeg flags and codecs are limited to what Homebrew ships until a source build is justified.
