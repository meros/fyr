// Decision 0031 slice 2b (V0.client-per-host): a host's install id as its own type. Every call to a host names one (urls.ts, api.ts), and a
// string is not one: an id comes only from what the front lists (`hosts[].id`, decodeHosts) or from the host a record
// came in, so `t.hostId ?? ""` or a name does not typecheck where a host is called. Where no host is known the screen
// says so or offers nothing; it never picks one.

declare const HOST_ID: unique symbol;

/** An install id ("F7QK-2M9X-AB34-CD56"), never "". */
export type HostId = string & { readonly [HOST_ID]: true };

/** The id in a decoded value (a front's host list, a stream frame, a stored record): a non-empty string, else none. */
export function asHostId(v: unknown): HostId | undefined {
  return typeof v === "string" && v ? (v as HostId) : undefined;
}
