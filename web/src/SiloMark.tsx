// The path follows the build's base: "/" in the console, relative in the demo
// build, which the website serves from a folder (vite.demo.config.ts).
export function SiloMark({ className }: { className?: string }) {
  return <img src={`${import.meta.env.BASE_URL}silo-mark.svg`} alt="" className={className} />;
}
