// V0.tailscale-operator-hint: Tailscale refuses `serve` from a user that is not its operator.
// The note on the host setup card then names the one command that fixes it.
export const TAILSCALE_OPERATOR_FIX = "sudo tailscale set --operator=$USER";

export function setupErrorHint(error: string): string | null {
  return /tailscale serve/i.test(error) && /401|must be root, or be an operator/i.test(error) ? TAILSCALE_OPERATOR_FIX : null;
}
