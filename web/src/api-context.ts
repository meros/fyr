// #41: the one way a component reaches the host. The app provides its Api once at the root; a test can
// provide a fake one. api.ts stays the only module that talks HTTP.

import { createContext } from "preact";
import { useContext } from "preact/hooks";
import type { Api } from "./api.ts";

export const ApiContext = createContext<Api | null>(null);

export function useApi(): Api {
  const api = useContext(ApiContext);
  if (!api) throw new Error("useApi: no ApiContext.Provider above this component");
  return api;
}
