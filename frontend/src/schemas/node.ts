import { z } from 'zod';

export const NodeRecordSchema = z
  .object({
    id: z.number(),
    name: z.string().optional(),
    remark: z.string().optional(),
    scheme: z.string().optional(),
    address: z.string().optional(),
    port: z.number().optional(),
    basePath: z.string().optional(),
    apiToken: z.string().optional(),
    hasApiToken: z.boolean().optional(),
    enable: z.boolean().optional(),
    status: z.string().optional(),
    latencyMs: z.number().optional(),
    cpuPct: z.number().optional(),
    memPct: z.number().optional(),
    xrayVersion: z.string().optional(),
    panelVersion: z.string().optional(),
    uptimeSecs: z.number().optional(),
    unavailableMetrics: z.array(z.string()).optional(),
    inboundCount: z.number().optional(),
    clientCount: z.number().optional(),
    onlineCount: z.number().optional(),
    activeCount: z.number().optional(),
    disabledCount: z.number().optional(),
    depletedCount: z.number().optional(),
    lastHeartbeat: z.number().optional(),
    lastError: z.string().optional(),
    // Xray state captured from the remote node's own /panel/api/server/status.
    // Lets the nodes list show a distinct indicator when the panel API is reachable
    // (status=online) but the Xray core on that node has failed.
    xrayState: z.string().optional(),
    xrayError: z.string().optional(),
    allowPrivateAddress: z.boolean().optional(),
    tlsVerifyMode: z.enum(['verify', 'skip', 'pin', 'mtls']).optional(),
    pinnedCertSha256: z.string().optional(),
    inboundSyncMode: z.enum(['all', 'selected']).optional(),
    // Backend serializes a nil []string as null for nodes saved before #5178.
    inboundTags: z.array(z.string()).nullish(),
    outboundTag: z.string().optional(),
    proxyUrl: z.string().optional(),
    trafficMultiplier: z.number().optional(),
    usageLimit: z.number().optional(),
    usagePeriod: z.string().optional(),
    usageResetDay: z.number().optional(),
    usageResetMonth: z.number().optional(),
    usageResetCalendar: z.string().optional(),
    usageDisabled: z.boolean().optional(),
    usageUsed: z.number().optional(),
    // Multi-hop node tree (#4983): a node's stable GUID, its parent's GUID, and
    // whether it's a read-only transitive sub-node surfaced from a downstream node.
    guid: z.string().optional(),
    parentGuid: z.string().optional(),
    transitive: z.boolean().optional(),
  })
  .loose();

export const NodeListSchema = z.array(NodeRecordSchema);

export const ProbeResultSchema = z
  .object({
    status: z.string(),
    unavailableMetrics: z.array(z.string()).optional(),
    latencyMs: z.number().optional(),
    xrayVersion: z.string().optional(),
    error: z.string().optional(),
    // Present on successful probe; used to surface "connected to panel, but xray failed on node".
    xrayState: z.string().optional(),
    xrayError: z.string().optional(),
  })
  .loose();

export const NodeFormSchema = z
  .object({
    id: z.number().optional(),
    name: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
    remark: z.string().optional(),
    scheme: z.enum(['http', 'https']),
    address: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
    port: z.number().int().min(1).max(65535),
    basePath: z.string(),
    // mTLS nodes authenticate via the client certificate, so the token is optional
    // there; every other verify mode still requires one (matches remote.do()).
    apiToken: z.string().trim(),
    hasStoredToken: z.boolean().optional().default(false),
    enable: z.boolean(),
    allowPrivateAddress: z.boolean(),
    tlsVerifyMode: z.enum(['verify', 'skip', 'pin', 'mtls']),
    pinnedCertSha256: z.string().optional().default(''),
    inboundSyncMode: z.enum(['all', 'selected']).optional().default('all'),
    // Unmounted when sync mode is "all" (absent from antd onFinish values) and
    // serialized as null by the backend for a nil slice — tolerate both.
    inboundTags: z
      .array(z.string())
      .nullish()
      .transform((tags) => tags ?? []),
    outboundTag: z.string().optional(),
    proxyUrl: z
      .string()
      .trim()
      .regex(/^$|^(https?|socks5h?):\/\/[^\s/]+:\d+\/?$/i, 'pages.nodes.proxyUrlInvalid')
      .optional()
      .default(''),
    trafficMultiplier: z.number().min(0.01).max(100),
    usagePeriod: z.enum(['', 'weekly', 'monthly', 'yearly']),
    usageLimitGB: z.number().min(0),
    usageResetDay: z.number().int().min(1).max(31),
    usageResetMonth: z.number().int().min(1).max(12),
    usageResetCalendar: z.enum(['gregorian', 'persian']),
  })
  .superRefine((val, ctx) => {
    if (val.tlsVerifyMode !== 'mtls' && val.apiToken.length === 0 && !val.hasStoredToken) {
      ctx.addIssue({
        code: 'custom',
        path: ['apiToken'],
        message: 'pages.nodes.toasts.fillRequired',
      });
    }
  });

export type NodeRecord = z.infer<typeof NodeRecordSchema>;
export type ProbeResult = z.infer<typeof ProbeResultSchema>;
export type NodeFormValues = z.infer<typeof NodeFormSchema>;

export const NodeInstallFormSchema = z.object({
  name: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
  address: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
  panelPort: z.number().int().min(1).max(65535),
  sshHost: z.string().trim(),
  sshPort: z.number().int().min(1).max(65535),
  sshUser: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
  auth: z.enum(['password', 'key']),
  password: z.string(),
  privateKey: z.string(),
});

export type NodeInstallFormValues = z.infer<typeof NodeInstallFormSchema>;

const ingressTarget = z
  .string()
  .trim()
  .regex(/^(\[[0-9a-fA-F:.]+\]|[A-Za-z0-9.-]+):\d{1,5}$/, {
    message: 'pages.ingress.targetInvalid',
  });

export const IngressRouteFormSchema = z.object({
  remark: z.string(),
  hosts: z.array(z.string()).min(1, 'pages.ingress.hostsRequired'),
  alpn: z.array(z.string()),
  target: ingressTarget,
  proxyProtocol: z.number().int().min(0).max(2),
});

export const IngressFormSchema = z.object({
  id: z.number(),
  remark: z.string(),
  enable: z.boolean(),
  listen: z.string().trim(),
  port: z.number().int().min(1).max(65535),
  routes: z.array(IngressRouteFormSchema),
  defaultTarget: z.union([z.literal(''), ingressTarget]),
  defaultProxyProtocol: z.number().int().min(0).max(2),
});

export type IngressFormValues = z.infer<typeof IngressFormSchema>;

export const BackupTargetFormSchema = z
  .object({
    id: z.number(),
    name: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
    enable: z.boolean(),
    kind: z.enum(['local', 's3', 'ssh']),
    everyHours: z.number().int().min(0).max(720),
    keep: z.number().int().min(0).max(1000),
    dir: z.string().trim(),
    endpoint: z.string().trim(),
    region: z.string().trim(),
    bucket: z.string().trim(),
    prefix: z.string().trim(),
    accessKey: z.string().trim(),
    host: z.string().trim(),
    port: z.number().int().min(0).max(65535),
    user: z.string().trim(),
    hostKey: z.string().trim(),
    passphrase: z.string(),
    secretKey: z.string(),
    password: z.string(),
    privateKey: z.string(),
  })
  .loose();

export type BackupTargetFormValues = z.infer<typeof BackupTargetFormSchema>;
