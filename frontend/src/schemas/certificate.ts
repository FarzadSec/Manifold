import { z } from 'zod';

const domain = /^(\*\.)?([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$/i;

export const CertDomainsSchema = z
  .array(z.string())
  .min(1, 'pages.certs.needDomain')
  .refine((list) => list.every((d) => domain.test(d.trim())), 'pages.certs.badDomain');

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

// wildcardNeedsDNS is the message for a wildcard asked over HTTP-01 or TLS-ALPN-01.
export function wildcardNeedsDNS(domains: string[], challenge: string): string | true {
  return challenge !== 'dns-01' && domains.some((d) => d.trim().startsWith('*.'))
    ? 'pages.certs.wildcardDns'
    : true;
}

// daysLeft rounds down, so a certificate that expires today has 0 days left.
export function daysLeft(notAfter: number, now = Date.now()): number {
  return Math.floor((notAfter - now) / 86_400_000);
}
