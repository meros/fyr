// Reload the page when the daemon was replaced by a different build (SPEC V0.13).
// The first buildId seen is remembered. A later one that differs reloads once; a missing one never
// reloads (an old daemon without buildId would otherwise reload forever).

export interface BuildWatch {
  first: string | null;
  reloading: boolean;
}

export function initialBuildWatch(): BuildWatch {
  return { first: null, reloading: false };
}

export function observeBuildId(w: BuildWatch, buildId: unknown): { watch: BuildWatch; reload: boolean } {
  if (typeof buildId !== "string" || buildId === "") return { watch: w, reload: false };
  if (w.first === null) return { watch: { ...w, first: buildId }, reload: false };
  if (buildId === w.first || w.reloading) return { watch: w, reload: false };
  return { watch: { ...w, reloading: true }, reload: true };
}
