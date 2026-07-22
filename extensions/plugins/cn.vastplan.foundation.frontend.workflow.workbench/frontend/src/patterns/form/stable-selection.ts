import { useRef } from "react";

export function stabilizeSelection<T>(previous: readonly T[], next: readonly T[]): readonly T[] {
  if (previous.length === next.length && previous.every((value, index) => value === next[index])) return previous;
  return next;
}

export function useStableSelection<T>(selected: readonly T[]): readonly T[] {
  const selectedRef = useRef(selected);
  selectedRef.current = stabilizeSelection(selectedRef.current, selected);
  return selectedRef.current;
}
