import { Power } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { btnClass } from "./Btn";

export function NeedMachine({
  copy,
  starting,
  onStart,
}: {
  copy: string;
  starting: boolean;
  onStart: () => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center rounded-[10px] bg-cloth px-4 wide:px-8">
      <p className="mb-7 max-w-[22rem] text-center text-[17px] leading-7 text-stone">{copy}</p>
      {starting ? (
        <WakeMark />
      ) : (
        <button type="button" className={btnClass("primary", "silo-need-go")} onClick={onStart}>
          Start Bot
          <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-[4px] bg-bindery-deep/45">
            <Power size={15} />
          </span>
        </button>
      )}
    </div>
  );
}

function WakeMark() {
  const [sec, setSec] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setSec((n) => n + 1), 1000);
    return () => clearInterval(t);
  }, []);
  const line = sec < 3 ? "Waking the box" : sec < 8 ? "Waiting for the worker" : "Still connecting";
  return (
    <div className="flex flex-col items-center" aria-busy="true" aria-live="polite">
      <div className="silo-wake" />
      <p className="mt-4 text-[16px] font-medium text-iron">{line}</p>
      <p className="mt-1 font-mono text-[13px] text-stone">{sec}s</p>
    </div>
  );
}
