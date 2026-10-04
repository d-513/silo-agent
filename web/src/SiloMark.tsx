export function SiloGlyph({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 32 32" fill="none" aria-hidden="true">
      <path
        d="M6 12a10 10 0 0 1 20 0v14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2ZM11.8 11.2h8.4a0.8 0.8 0 0 1 0 1.6h-8.4a0.8 0.8 0 0 1 0-1.6Z"
        fill="currentColor"
        fillRule="evenodd"
      />
    </svg>
  );
}

export function SiloMark() {
  return (
    <div className="flex shrink-0 items-center gap-2 px-3 text-ink max-wide:h-12 wide:flex-col wide:gap-1 wide:px-2 wide:pt-4">
      <SiloGlyph className="max-wide:h-5 max-wide:w-5 wide:h-[26px] wide:w-[26px]" />
      <span className="font-semibold max-wide:text-[13px] wide:text-[11px] wide:leading-4">Silo</span>
    </div>
  );
}
