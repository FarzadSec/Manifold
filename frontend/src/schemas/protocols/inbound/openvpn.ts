import { z } from 'zod';

export const OpenvpnClientSchema = z.object({
  password: z.string().default(''),
  email: z.string().min(1),
  limitIp: z.number().int().min(0).default(0),
  totalGB: z.number().int().min(0).default(0),
  expiryTime: z.number().int().default(0),
  enable: z.boolean().default(true),
  tgId: z
    .union([z.number(), z.string()])
    .transform((v) => Number(v) || 0)
    .default(0),
  subId: z.string().default(''),
  comment: z.string().default(''),
  reset: z.number().int().min(0).default(0),
  created_at: z.number().int().optional(),
  updated_at: z.number().int().optional(),
});
export type OpenvpnClient = z.infer<typeof OpenvpnClientSchema>;

const IPV4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/;

// The panel refuses the same values on save (internal/openvpn/settings.go).
export function isOpenvpnSubnet(value: string): boolean {
  const [address, bits, ...rest] = value.trim().split('/');
  if (rest.length > 0 || !IPV4.test(address ?? '') || !/^\d{1,2}$/.test(bits ?? '')) return false;
  const prefix = Number(bits);
  return prefix >= 16 && prefix <= 29;
}

export function isOpenvpnDns(value: string): boolean {
  return IPV4.test(value.trim());
}

// The same lists and ranges the panel checks on save (internal/openvpn/settings.go).
export const OPENVPN_DATA_CIPHERS = ['AES-256-GCM', 'AES-128-GCM', 'CHACHA20-POLY1305'] as const;
export const OPENVPN_TLS_MODES = ['tls-crypt', 'tls-auth', 'none'] as const;
export const OPENVPN_KEY_TYPES = ['ecdsa-p256', 'ecdsa-p384', 'rsa-2048', 'rsa-4096'] as const;
export const OPENVPN_SERVER_NAME = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/;

const optionalRange = (low: number, high: number, message: string) =>
  z
    .number()
    .int()
    .default(0)
    .refine((v) => v === 0 || (v >= low && v <= high), { message });

export const OpenvpnServerSchema = z.object({
  proto: z.enum(['udp', 'tcp']).default('udp'),
  subnet: z.string().default('10.8.0.0/24').refine(isOpenvpnSubnet, {
    message: 'pages.inbounds.openvpn.subnetInvalid',
  }),
  dns: z
    .array(z.string().refine(isOpenvpnDns, { message: 'pages.inbounds.openvpn.dnsInvalid' }))
    .default(['1.1.1.1', '8.8.8.8']),
  promptCredentials: z.boolean().default(false),
  mtu: optionalRange(576, 9000, 'pages.inbounds.openvpn.mtuInvalid'),
  mssfix: optionalRange(512, 9000, 'pages.inbounds.openvpn.mssfixInvalid'),
  dataCiphers: z.array(z.enum(OPENVPN_DATA_CIPHERS)).default([]),
  splitTunnel: z.boolean().default(false),
  tlsMode: z.enum(OPENVPN_TLS_MODES).default('tls-crypt'),
  tlsVersionMin: z.enum(['', '1.2', '1.3']).default(''),
  // -1 switches renegotiation off; 0 keeps OpenVPN's hourly default.
  renegSec: z
    .number()
    .int()
    .default(0)
    .refine((v) => v === 0 || v === -1 || (v >= 60 && v <= 604800), {
      message: 'pages.inbounds.openvpn.renegInvalid',
    }),
  maxClients: z.number().int().min(0).default(0),
  searchDomains: z
    .array(
      z
        .string()
        .regex(/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*\.?$/i, {
          message: 'pages.inbounds.openvpn.searchDomainInvalid',
        }),
    )
    .max(8)
    .default([]),
  serverName: z.string().optional(),
  clientDirectives: z.string().default(''),
  caCert: z.string().default(''),
  caKey: z.string().optional(),
  serverCert: z.string().optional(),
  serverKey: z.string().optional(),
  tlsCryptKey: z.string().default(''),
});
export type OpenvpnServer = z.infer<typeof OpenvpnServerSchema>;

export const OpenvpnInboundSettingsSchema = z.object({
  server: OpenvpnServerSchema.optional(),
  clients: z.array(OpenvpnClientSchema).default([]),
});
export type OpenvpnInboundSettings = z.infer<typeof OpenvpnInboundSettingsSchema>;
