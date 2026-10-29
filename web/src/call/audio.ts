// Pure helpers for a call's audio, kept out of the session so node can test
// them.

/** A turn's samples (-1..1 at `rate`) as a 16-bit mono WAV file. */
export function encodeWAV(samples: Float32Array, rate: number): ArrayBuffer {
  const buf = new ArrayBuffer(44 + samples.length * 2);
  const v = new DataView(buf);
  const text = (at: number, s: string) => {
    for (let i = 0; i < s.length; i++) v.setUint8(at + i, s.charCodeAt(i));
  };
  text(0, "RIFF");
  v.setUint32(4, 36 + samples.length * 2, true);
  text(8, "WAVEfmt ");
  v.setUint32(16, 16, true);
  v.setUint16(20, 1, true);
  v.setUint16(22, 1, true);
  v.setUint32(24, rate, true);
  v.setUint32(28, rate * 2, true);
  v.setUint16(32, 2, true);
  v.setUint16(34, 16, true);
  text(36, "data");
  v.setUint32(40, samples.length * 2, true);
  for (let i = 0; i < samples.length; i++) {
    const s = Math.max(-1, Math.min(1, samples[i]));
    v.setInt16(44 + i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return buf;
}

/** Joins frames into one run of samples. */
export function joinFrames(frames: Float32Array[]): Float32Array {
  let n = 0;
  for (const f of frames) n += f.length;
  const out = new Float32Array(n);
  let at = 0;
  for (const f of frames) {
    out.set(f, at);
    at += f.length;
  }
  return out;
}

/** How loud a frame is, 0..1: its RMS on a curve that makes speech fill the meter. */
export function loudness(frame: Float32Array): number {
  if (frame.length === 0) return 0;
  let sum = 0;
  for (let i = 0; i < frame.length; i++) sum += frame[i] * frame[i];
  const rms = Math.sqrt(sum / frame.length);
  return Math.min(1, Math.sqrt(rms * 6));
}

/** The browser's playout delay in ms from an inbound-rtp report, or 0 when it cannot tell. */
export function playoutMs(report: { jitterBufferDelay?: number; jitterBufferEmittedCount?: number }): number {
  const delay = report.jitterBufferDelay ?? 0;
  const count = report.jitterBufferEmittedCount ?? 0;
  if (!(count > 0) || !(delay >= 0)) return 0;
  // Plus what the output device holds on to.
  return Math.round((delay / count) * 1000) + 40;
}

/** A call's length as m:ss. */
export function callClock(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

/** A frame (-1..1) as 16-bit little-endian PCM, what a live turn streams. */
export function pcm16(frame: Float32Array): ArrayBuffer {
  const buf = new ArrayBuffer(frame.length * 2);
  const v = new DataView(buf);
  for (let i = 0; i < frame.length; i++) {
    const s = Math.max(-1, Math.min(1, frame[i]));
    v.setInt16(i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return buf;
}
