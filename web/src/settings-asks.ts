// V0.settings-ia: what the confirmation dialog says before the presses in Settings that cannot be taken back and reach
// beyond this host's own small things (settings-rows.tsx ConfirmButton says which press asks where). Pure: the words;
// confirm-ui.tsx shows them. Delete project and Remove provider are in confirm.ts.
import type { Ask } from "./confirm.ts";

/** Hosts: Remove a paired host. It ends trust for good, on another machine too, so it is never an inline press. */
export function removeHostAsk(name: string): Ask {
  return {
    title: `Remove ${name}?`,
    text: `Its pairing ends for ever: the link to ${name} closes, and what it signed counts for nothing. To follow it again you pair it anew with an invite.`,
    confirm: "Remove host",
    danger: true,
  };
}

/** Sign-in: turning Require sign-in on while requests without a session still arrive would refuse them: it asks first.
 *  null when nothing would be refused (the switch then acts at once; it can be turned off again). */
export function requireSignInAsk(m: { refused: number }): Ask | null {
  if (m.refused <= 0) return null;
  return {
    title: "Require sign-in?",
    text: `${m.refused === 1 ? "1 request" : `${m.refused} requests`} without a session got in while Fyr only watched. From now on a device that is not signed in is refused until you allow it. This device is signed in and stays so.`,
    confirm: "Require sign-in",
  };
}
