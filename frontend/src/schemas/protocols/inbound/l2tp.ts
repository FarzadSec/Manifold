import { z } from 'zod';

import { isOpenvpnDns, isOpenvpnSubnet } from './openvpn';

export const L2tpClientSchema = z.object({
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
export type L2tpClient = z.infer<typeof L2tpClientSchema>;

// The panel refuses the same keys on save (internal/l2tp/settings.go). An
// empty key is not an error: the panel then keeps the stored key or makes one.
export function isL2tpPsk(value: string): boolean {
  return value === '' || /^[A-Za-z0-9!@#$%^&*()_+=.,:;~-]{8,64}$/.test(value);
}

export const L2tpServerSchema = z.object({
  // Off also lets in sessions that are not encrypted.
  ipsec: z.boolean().default(true),
  psk: z.string().default('').refine(isL2tpPsk, { message: 'pages.inbounds.l2tp.pskInvalid' }),
  // The tunnel subnet and DNS rules are the same as an OpenVPN inbound's.
  subnet: z
    .string()
    .default('10.9.0.0/24')
    .refine(isOpenvpnSubnet, { message: 'pages.inbounds.openvpn.subnetInvalid' }),
  dns: z
    .array(z.string().refine(isOpenvpnDns, { message: 'pages.inbounds.openvpn.dnsInvalid' }))
    .max(2, { message: 'pages.inbounds.l2tp.dnsTooMany' })
    .default(['1.1.1.1', '8.8.8.8']),
  mtu: z.number().int().min(576).max(1460).default(1280),
});
export type L2tpServer = z.infer<typeof L2tpServerSchema>;

export const L2tpInboundSettingsSchema = z.object({
  server: L2tpServerSchema.optional(),
  clients: z.array(L2tpClientSchema).default([]),
});
export type L2tpInboundSettings = z.infer<typeof L2tpInboundSettingsSchema>;
