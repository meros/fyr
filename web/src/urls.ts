// Decision 0031 (the client names the host): the one place that writes a path to the front. A call to a host is
// `/api/h/<hostId>/<the host protocol's path after /api/>`; a call to the front itself is `/api/front/…`. No other
// module writes "/api/" in a string (test/urls.test.ts holds this), so no call can leave its host out.

import type { HostId } from "./hostid.ts";

/** A host's call: hostPath("F7QK-…", "/threads/t_1/events?after=3"). `path` starts with "/" and is the host protocol's
 *  path without "/api". The type takes an install id only (hostid.ts), so a name or `?? ""` does not compile; an empty
 *  one that got past it anyway throws instead of reaching some other host. */
export function hostPath(host: HostId, path: string): string {
  if (!host) throw new Error("a call to a host names the host");
  return `/api/h/${encodeURIComponent(host)}${path}`;
}

/** The same for a URL that is only shown (an image's src, a link): without a host there is nothing to read, so "" and
 *  not a throw in the middle of a render. */
export function hostUrl(host: HostId | undefined, path: string): string {
  return host ? hostPath(host, path) : "";
}

/** V0.forwarded-ports: where a link to a server on a thread's host points. The front makes the forward and sends the
 *  browser to it (or, on the host's own machine, to the server itself). A page's address, not an API call: it opens in
 *  a new tab and can be copied. `thread` is the host's own thread id. */
export function forwardOpenPath(host: HostId, thread: string, port: number, rest: string): string {
  return `/forward/open?host=${encodeURIComponent(host)}&thread=${encodeURIComponent(thread)}&port=${port}&path=${encodeURIComponent(rest)}`;
}

/** A call to the front itself: frontPath("/state"). */
export function frontPath(path: string): string {
  return `/api/front${path}`;
}

/** The path an agent writes for its own attachment or preview, as its host serves it ("/api/attachments/a_1"): the
 *  host's own path, which the client rewrites to the thread's host (markdown, previews). */
export const AGENT_ATTACHMENT = /^\/api\/attachments\/([A-Za-z0-9_.-]+)$/;
