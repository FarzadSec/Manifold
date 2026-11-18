// The DT Connect app's sign-in link: its QR scanner, a tap on it and a paste into the app all read it.
export function appSignInLink(subLink: string, username: string): string {
  return `dtconnect://login?sub=${encodeURIComponent(subLink)}&user=${encodeURIComponent(username)}`;
}
