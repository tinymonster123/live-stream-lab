/**
 * The fake camera used by the Playwright media tests.
 *
 * Each frame carries its frame index as high-contrast blocks, so a test can
 * read the publisher's local preview and the subscriber's remote video at the
 * same time and subtract the indices. The difference is the pipeline latency
 * in frames, including encode, the SFU, jitter buffering, and decode.
 *
 * The generator and the reader share these constants so changing the geometry
 * cannot silently make the test sample the wrong pixels.
 */
import { writeFileSync } from "node:fs";

const width = 320;
const height = 240;
const fps = 30;
const bits = 8;
const blockSize = 28;
const blockGap = 6;
const firstBlockX = 27;

export const frameClock = {
  width,
  height,
  fps,
  bits,
  frameCount: 1 << bits, // eight bits; loops every ~8.5 seconds
  blockSize,
  blockGap,
  firstBlockX,
  /** Center X of each bit's block, MSB first. */
  bitCenters: Array.from(
    { length: bits },
    (_, bit) => firstBlockX + bit * (blockSize + blockGap) + blockSize / 2,
  ),
} as const;

export function writeFrameClock(output: string): void {
  const lumaSize = width * height;
  const chromaSize = (width / 2) * (height / 2);
  const blockY = height / 2 - blockSize / 2;
  const chunks: Buffer[] = [
    Buffer.from(`YUV4MPEG2 W${width} H${height} F${fps}:1 Ip A1:1 C420jpeg\n`),
  ];

  for (let index = 0; index < frameClock.frameCount; index += 1) {
    const frame = Buffer.alloc(lumaSize + 2 * chromaSize);
    frame.fill(128, 0, lumaSize); // neutral gray background
    frame.fill(128, lumaSize); // neutral chroma

    for (let bit = 0; bit < bits; bit += 1) {
      const set = (index >> (bits - 1 - bit)) & 1;
      const value = set ? 235 : 16;
      const xStart = firstBlockX + bit * (blockSize + blockGap);
      for (let y = blockY; y < blockY + blockSize; y += 1) {
        const row = y * width + xStart;
        frame.fill(value, row, row + blockSize);
      }
    }

    chunks.push(Buffer.from("FRAME\n"), frame);
  }

  const bytes = Buffer.concat(chunks);
  writeFileSync(output, bytes);
  console.log(
    `wrote ${output} (${(bytes.length / 1024 / 1024).toFixed(1)} MiB, ${frameClock.frameCount} frames)`,
  );
}
