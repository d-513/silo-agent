import { useEffect, useRef, useState, type DependencyList } from "react";
import { fail } from "./errors";

// useSearch runs a debounced search for query and keeps only the newest answer,
// so a slow earlier request never overwrites a later one. hits is null while
// there is no query; a failed search leaves hits empty and sets error. deps are
// what `run` closes over besides the query.
export function useSearch<T>(query: string, run: (q: string) => Promise<T[]>, delay: number, deps: DependencyList) {
  const [hits, setHits] = useState<T[] | null>(null);
  const [searching, setSearching] = useState(false);
  const [error, setError] = useState("");
  const seq = useRef(0);

  useEffect(() => {
    const q = query.trim();
    const n = ++seq.current;
    setError("");
    if (!q) {
      setHits(null);
      setSearching(false);
      return;
    }
    setSearching(true);
    const t = setTimeout(() => {
      run(q)
        .then((r) => {
          if (n === seq.current) setHits(r);
        })
        .catch((e) => {
          if (n === seq.current) {
            setHits([]);
            setError(fail(e));
          }
        })
        .finally(() => {
          if (n === seq.current) setSearching(false);
        });
    }, delay);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, ...deps]);

  return { hits, setHits, searching, error };
}
