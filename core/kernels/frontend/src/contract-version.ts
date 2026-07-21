/** Limited, fail-closed semver matcher for public Frontend contracts. */
export function contractSatisfies(actual: string, requested: string): boolean {
  const actualMatch = /^(\d+)\.(\d+)\.(\d+)$/.exec(actual);
  const requestedMatch = /^\^(\d+)\.(\d+)\.(\d+)$/.exec(requested);
  if (actualMatch === null || requestedMatch === null) return false;
  const [actualMajor, actualMinor, actualPatch] = actualMatch.slice(1).map(Number);
  const [requestedMajor, requestedMinor, requestedPatch] = requestedMatch.slice(1).map(Number);
  return actualMajor === requestedMajor && (actualMinor > requestedMinor || (actualMinor === requestedMinor && actualPatch >= requestedPatch));
}
