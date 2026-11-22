// What the mock files share: the page's ?query switches, the rail's fixed clock and the long fixture names.

/** One ?query switch of the page, read when asked; null without one (or without a page: a test). */
export function mockParam(name: string): string | null {
  return typeof location !== "undefined" ? new URLSearchParams(location.search).get(name) : null;
}

export const LONG_PROJECT = "acme-platform-infrastructure-terraform-modules-and-tooling"; // 59 chars

// ?mock=rail: the exact content of the first (light) frame of the rail direction (decision 0006).
export function railNow(real: number): number {
  const d = new Date(real);
  d.setHours(14, 32, 0, 0);
  return d.getTime();
}

/** ?hosts=2: a second host, "lab" (read once at load, like the other switches). */
export const MOCK_HOSTS = mockParam("hosts") === "2";
