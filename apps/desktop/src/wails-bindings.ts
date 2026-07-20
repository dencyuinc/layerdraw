// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

import {
  WailsBindingError,
  wailsEngineBindingDescriptors,
  wailsRuntimeBindingDescriptors,
  type WailsExchange,
  type WailsGeneratedBindings,
} from "@layerdraw/engine-client/wails";

export interface DesktopInvokeResult {
  readonly outcome: string;
  readonly value?: WailsExchange;
}

export type DesktopWailsInvoke = (method: string, exchange: WailsExchange) => Promise<DesktopInvokeResult>;

/** Maps the one generated Go Invoke façade to the exact closed Wails table. */
export function createDesktopGeneratedBindings(invoke: DesktopWailsInvoke): WailsGeneratedBindings {
  const bindings: Record<string, (exchange: WailsExchange) => Promise<unknown>> = {};
  for (const descriptor of [...wailsEngineBindingDescriptors, ...wailsRuntimeBindingDescriptors]) {
    bindings[descriptor.generatedMethod] = async (exchange: WailsExchange) => {
      const result = await invoke(descriptor.generatedMethod, exchange);
      if (result.outcome !== "success" || result.value === undefined) {
        throw new WailsBindingError("BINDING_VERSION_MISMATCH", "upgrade_desktop");
      }
      return result.value;
    };
  }
  return Object.freeze(bindings) as unknown as WailsGeneratedBindings;
}
