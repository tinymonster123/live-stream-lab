# ai-worker

Native Python worker for Live Stream Lab. It consumes AI Jobs from Redis Streams and produces
Transcripts and Summaries with local MLX models.

The worker runs natively on macOS because Docker Desktop cannot expose Metal/MLX to a container
(ADR-0003, ADR-0004). Redis consumption and MLX models land with the AI pipeline slice; this
package currently exposes only its health surface.
