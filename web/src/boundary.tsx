// SPEC 0.5 V0.24: a render error stays in the part of the screen where it
// happened. The part shows a small card with Try again and Reload, and the rest
// of Fyr keeps working. Before this, one bad field blanked the whole app.
import { Component, type ComponentChildren } from "preact";

type Props = { name: string; children?: ComponentChildren; resetKey?: unknown };
type State = { error: Error | null; key?: unknown };

export class Boundary extends Component<Props, State> {
  state: State = { error: null };
  static getDerivedStateFromError(error: unknown): Partial<State> {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }
  componentDidCatch(error: unknown) {
    console.error(`[fyr] ${this.props.name} failed to render`, error);
  }
  componentDidUpdate(prev: Props) {
    if (prev.resetKey !== this.props.resetKey && this.state.error) this.setState({ error: null });
  }
  render() {
    const e = this.state.error;
    if (!e) return this.props.children;
    return (
      <div class="crash" role="alert">
        <b>{this.props.name} could not be shown.</b>
        <span class="crash-msg">{e.message}</span>
        <span class="crash-btns">
          <button onClick={() => this.setState({ error: null })}>Try again</button>
          <button onClick={() => location.reload()}>Reload</button>
        </span>
      </div>
    );
  }
}

/** Mock mode only (?mock=…&crash=1): a component that throws, for the layout test. */
// Read once at load: the app rewrites the URL when it opens a thread.
const CRASH = typeof location !== "undefined" && /[?&]mock=/.test(location.search) && /[?&]crash=1\b/.test(location.search);
export const crashProbe = (): boolean => CRASH;
export function CrashProbe(): null {
  throw new Error("crash probe");
}
