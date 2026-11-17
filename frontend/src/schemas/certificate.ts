import { z } from 'zod';

const domain = /^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i;

export function isIPAddress(value: string): boolean {
  const v = value.trim();
  return z.ipv4().safeParse(v).success || z.ipv6().safeParse(v).success;
}

export const CertDomainsSchema = z
  .array(z.string())
  .min(1, 'pages.certs.needDomain')
  .refine(
    (list) => list.every((d) => domain.test(d.trim()) || isIPAddress(d)),
    'pages.certs.badDomain',
  );

export const CertificateFormSchema = z
  .object({
    id: z.number(),
    name: z.string().trim(),
    domains: z.array(z.string()),
    challenge: z.enum(['dns-01', 'http-01', 'tls-alpn-01']),
    port: z.number().int().min(0).max(65535),
    dnsProvider: z.enum(['cloudflare', 'arvancloud']),
    dnsToken: z.string(),
    email: z.string().trim(),
    staging: z.boolean(),
    autoRenew: z.boolean(),
    nodeIds: z.array(z.number()),
  })
  .loose();

export type CertificateFormValues = z.infer<typeof CertificateFormSchema>;

// challengeMismatch: a wildcard is proved only by DNS-01, an IP address only by HTTP-01.
export function challengeMismatch(domains: string[], challenge: string): string | true {
  if (challenge !== 'dns-01' && domains.some((d) => d.trim().startsWith('*.'))) {
    return 'pages.certs.wildcardDns';
  }
  if (challenge !== 'http-01' && domains.some(isIPAddress)) return 'pages.certs.ipHttp';
  return true;
}

// expiryColor warns in the last sixth of a certificate's life, at most two weeks ahead.
export function expiryColor(notBefore: number, notAfter: number, now = Date.now()): string {
  const left = notAfter - now;
  if (left < 0) return 'red';
  return left < Math.min(14 * 86_400_000, (notAfter - notBefore) / 6) ? 'orange' : 'green';
}

// daysLeft rounds down, so a certificate that expires today has 0 days left.
export function daysLeft(notAfter: number, now = Date.now()): number {
  return Math.floor((notAfter - now) / 86_400_000);
}
