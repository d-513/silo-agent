import { lazy, type ComponentType, type LazyExoticComponent } from "react";

// lazyNamed is React.lazy for a module's named export, keeping that export's own
// props:
//   const Memories = lazyNamed(() => import("./Memories"), "MemoriesPane");
export function lazyNamed<M, K extends keyof M>(load: () => Promise<M>, name: K) {
  return lazy(() => load().then((m) => ({ default: m[name] as ComponentType<any> }))) as unknown as LazyExoticComponent<
    M[K] extends ComponentType<any> ? M[K] : never
  >;
}
