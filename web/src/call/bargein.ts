// Barge-in: how you take the floor back from the Bot's voice. The moment you
// seem to start speaking over it the voice is muted here (no round trip) and
// the control plane is told to pause; once the detector is sure, to stop. If
// it was nothing, the voice picks up where it paused.
//
// A pause must always end in one of those two: the control plane holds the
// voice until it does, however long that takes.

// The Bot is ducked after duckFrames of likely speech.
const duckFrames = 3;
const duckThreshold = 0.7;

export type BargeMsg = "duck" | "unduck" | "interrupt";

export class BargeIn {
  // The Bot's voice is on, as the control plane says.
  speaking = false;
  // You are speaking for sure: the detector is, or the button is held.
  talking = false;
  ducked = false;
  private cut = false;
  private run = 0;
  private tell: (m: BargeMsg) => void;
  private mute: (on: boolean) => void;

  constructor(tell: (m: BargeMsg) => void, mute: (on: boolean) => void) {
    this.tell = tell;
    this.mute = mute;
  }

  /** One frame of the microphone. detect is false while hold-to-talk replaces the detector. */
  frame(speech: number, detect: boolean) {
    if (!this.speaking) {
      this.run = 0;
      return;
    }
    // You were already talking when the voice began: it does not get to start.
    if (this.talking) {
      this.interrupt();
      return;
    }
    if (this.ducked || !detect) return;
    this.run = speech > duckThreshold ? this.run + 1 : 0;
    if (this.run >= duckFrames) {
      this.ducked = true;
      this.mute(true);
      this.tell("duck");
    }
  }

  /** You are speaking for sure. Nothing else need have happened first: the frames may not have ducked. */
  sure() {
    this.talking = true;
    this.interrupt();
  }

  /** Your speech ended, or was never speech: a pause that was not followed through lifts. */
  ended() {
    this.talking = false;
    this.run = 0;
    if (this.ducked && !this.cut) {
      this.ducked = false;
      this.mute(false);
      this.tell("unduck");
    }
  }

  /** Stops the Bot mid-sentence. */
  interrupt() {
    if (this.cut || (!this.speaking && !this.ducked)) return;
    this.ducked = true;
    this.cut = true;
    this.mute(true);
    this.tell("interrupt");
  }

  /** The Bot started a new sentence. After a cut, that is the answer to what you said. */
  said() {
    if (!this.cut) return;
    this.cut = false;
    this.ducked = false;
    this.run = 0;
    this.mute(false);
  }
}
