export function Switch({
  on,
  onChange,
  disabled,
  title,
}: {
  on: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  title?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      disabled={disabled}
      title={title ?? (on ? "On" : "Off")}
      onClick={(e) => {
        e.stopPropagation();
        onChange(!on);
      }}
      className={`relative inline-flex h-[22px] w-[40px] shrink-0 items-center rounded-full outline-none transition-colors duration-200 focus-visible:ring-2 focus-visible:ring-bindery/40 disabled:cursor-not-allowed disabled:opacity-40 ${
        on ? "bg-bindery" : "bg-thread"
      }`}
    >
      <span
        className={`inline-block h-[18px] w-[18px] transform rounded-full bg-folio shadow-sm transition-transform duration-200 ${
          on ? "translate-x-[20px]" : "translate-x-[2px]"
        }`}
      />
    </button>
  );
}
