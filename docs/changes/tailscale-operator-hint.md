# V0.tailscale-operator-hint: the host setup card names the fix when Tailscale refuses `serve` with a 401

A host whose user is not a Tailscale operator answers `tailscale serve` with `401 Unauthorized: must be root, or be an operator`. The card printed that text only.

1. The note on the host setup card adds `sudo tailscale set --operator=$USER` and says to press Check now after it. The card is one component for desktop and phone, so both show it.
2. The match is on the error text (`web/src/setup-hint.ts`); other errors are unchanged.
