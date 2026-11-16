// The tunnel edition runs on a relay: tunnels and the panel's own settings, no inbounds.
export function isTunnelEdition(): boolean {
  return typeof window !== 'undefined' && window.X_UI_EDITION === 'tunnel';
}
