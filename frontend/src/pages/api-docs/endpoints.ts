export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD';
export type ParamLocation =
  | 'path'
  | 'query'
  | 'header'
  | 'body'
  | 'body (form)'
  | 'body (json)'
  | 'body (multipart)';
export type ParamType =
  | 'string'
  | 'integer'
  | 'integer[]'
  | 'string[]'
  | 'number'
  | 'boolean'
  | 'object'
  | 'object[]'
  | 'array'
  | 'file';

export interface EndpointParam {
  name: string;
  in: ParamLocation;
  type: ParamType;
  desc?: string;
  optional?: boolean;
  defaultValue?: string | number | boolean;
  minLength?: number;
  pattern?: string;
  enum?: readonly (string | number | boolean)[];
}

export interface Endpoint {
  method: HttpMethod;
  path: string;
  summary: string;
  description?: string;
  deprecated?: boolean;
  params?: EndpointParam[];
  body?: string;
  response?: string;
  errorResponse?: string;
  errorStatus?: number;
  requestSchema?: Record<string, unknown>;
  bodyRequiredOneOf?: string[];
  responseSchema?: string;
  responseSchemaArray?: boolean;
  responseSchemaArrayNullable?: boolean;
  responseObjectSchema?: Record<string, unknown>;
  responses?: Record<string, Record<string, unknown>>;
  security?: readonly Record<string, readonly string[]>[];
}

export interface SubscriptionHeader {
  name: string;
  desc: string;
}

export interface Section {
  id: string;
  title: string;
  description?: string;
  subHeader?: SubscriptionHeader[];
  endpoints: Endpoint[];
}

// /inbounds/update replaces the whole row, so it takes the same payload as /add.
const inboundBody =
  '{\n  "enable": true,\n  "remark": "VLESS-443",\n  "listen": "",\n  "port": 443,\n  "protocol": "vless",\n  "expiryTime": 0,\n  "total": 0,\n  "settings": {\n    "clients": [{ "id": "...", "email": "user1" }],\n    "decryption": "none"\n  },\n  "streamSettings": {\n    "network": "tcp",\n    "security": "reality",\n    "realitySettings": { "show": false, "dest": "..." }\n  },\n  "sniffing": {\n    "enabled": true,\n    "destOverride": ["http", "tls"]\n  }\n}';

const outboundSubscriptionBodyParams: EndpointParam[] = [
  {
    name: 'remark',
    in: 'body (form)',
    type: 'string',
    desc: 'Optional display label.',
    optional: true,
  },
  {
    name: 'url',
    in: 'body (form)',
    type: 'string',
    desc: 'Subscription URL (required). Must be a public http(s) address; private/internal targets are blocked unless allowPrivate is true.',
  },
  {
    name: 'tagPrefix',
    in: 'body (form)',
    type: 'string',
    desc: 'Prefix for generated outbound tags. Defaults to the lowest free "sub<N>-" prefix.',
    optional: true,
  },
  {
    name: 'userAgent',
    in: 'body (form)',
    type: 'string',
    desc: 'Custom User-Agent sent when fetching this subscription. Defaults to "manifold-outbound-sub/1.0".',
    optional: true,
    defaultValue: 'manifold-outbound-sub/1.0',
  },
  {
    name: 'updateInterval',
    in: 'body (form)',
    type: 'integer',
    desc: 'Seconds between auto-refreshes. Default 600.',
    optional: true,
    defaultValue: 600,
  },
  {
    name: 'enabled',
    in: 'body (form)',
    type: 'boolean',
    desc: 'Whether the subscription is active. Default true.',
    optional: true,
    defaultValue: true,
  },
  {
    name: 'allowPrivate',
    in: 'body (form)',
    type: 'boolean',
    desc: 'Allow the URL to point at a private/internal/loopback address. Default false.',
    optional: true,
    defaultValue: false,
  },
  {
    name: 'allowInsecure',
    in: 'body (form)',
    type: 'boolean',
    desc: "Skip TLS certificate verification when fetching the subscription's URL. Default false.",
    optional: true,
    defaultValue: false,
  },
  {
    name: 'prepend',
    in: 'body (form)',
    type: 'boolean',
    desc: "Place this subscription's outbounds before the manual template outbounds. Default false.",
    optional: true,
    defaultValue: false,
  },
];

const subBalancerBodyParams: EndpointParam[] = [
  {
    name: 'remark',
    in: 'body (form)',
    type: 'string',
    desc: 'Display label, used as the config remarks (required).',
  },
  {
    name: 'strategy',
    in: 'body (form)',
    type: 'string',
    desc: 'Balancer strategy: "leastLoad", "leastPing", "roundRobin" or "random". Default "random".',
    optional: true,
    defaultValue: 'random',
  },
  {
    name: 'inboundIds',
    in: 'body (form)',
    type: 'integer[]',
    desc: 'Repeated form keys selecting the member inbounds (required, at least one).',
  },
  {
    name: 'memberWeights',
    in: 'body (form)',
    type: 'object',
    desc: 'leastLoad only: JSON object mapping inbound id to a static weight > 0, e.g. {"3":0.2}. Lower weight = picked more often; absent ids weigh 1. Rejected for other strategies; entries for unselected inbounds are dropped.',
    optional: true,
  },
  {
    name: 'sortOrder',
    in: 'body (form)',
    type: 'integer',
    desc: '1-based position in the subscription list. Default 1.',
    optional: true,
    defaultValue: 1,
  },
  {
    name: 'enabled',
    in: 'body (form)',
    type: 'boolean',
    desc: 'Whether the balancer is emitted. Default true on create; unchanged when omitted on update.',
    optional: true,
  },
];

const subscriptionHeadResponses = {
  '200': { description: 'Subscription is available. Headers match GET; no response body.' },
  '404': { description: 'No enabled client matches the subscription ID.' },
  '500': { description: 'Subscription generation failed.' },
};

const hwidStatusErrorResponses = {
  '404': { description: 'No enabled client matches the subscription ID. Empty body.' },
  '500': { description: 'Database lookup failed. Empty body.' },
};

export const sections: readonly Section[] = [
  {
    id: 'authentication',
    title: 'Authentication',
    description:
      'Two authentication modes are supported. UI sessions use a cookie set by the login endpoint. Programmatic clients (bots, scripts, remote panels) authenticate with a Bearer token taken from Settings → Security → API Token. Both work for every endpoint under /panel/api/*.',
    endpoints: [
      {
        method: 'POST',
        path: '/login',
        summary:
          'Authenticate with username + password and receive a session cookie. Required before any cookie-based API call.',
        params: [
          { name: 'username', in: 'body', type: 'string', desc: 'Panel admin username.' },
          { name: 'password', in: 'body', type: 'string', desc: 'Panel admin password.' },
          {
            name: 'twoFactorCode',
            in: 'body',
            type: 'string',
            desc: 'OTP code when 2FA is enabled. Omit otherwise.',
            optional: true,
          },
        ],
        body: '{\n  "username": "admin",\n  "password": "admin",\n  "twoFactorCode": "123456"\n}',
        response: '{\n  "success": true,\n  "msg": "Logged in successfully"\n}',
        errorResponse: '{\n  "success": false,\n  "msg": "Wrong username or password"\n}',
      },
      {
        method: 'POST',
        path: '/logout',
        summary: 'Clear the session cookie. Requires the CSRF header for browser sessions.',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'GET',
        path: '/csrf-token',
        summary:
          'Mint a CSRF token for the current session. The SPA replays it in the X-CSRF-Token header on unsafe requests. Bearer-token callers can skip this — the middleware short-circuits CSRF for authenticated API requests.',
        response: '{\n  "success": true,\n  "obj": "csrf-token-string"\n}',
      },
      {
        method: 'POST',
        path: '/getTwoFactorEnable',
        summary:
          'Returns whether 2FA is enabled on the panel — used by the login page to decide whether to show the OTP field.',
        response: '{\n  "success": true,\n  "obj": false\n}',
      },
    ],
  },

  {
    id: 'inbounds',
    title: 'Inbounds',
    description:
      'Manage inbound configurations and their clients. All endpoints live under /panel/api/inbounds and require a logged-in session or Bearer token. Link-generating endpoints honour forwarded headers only when the request comes from a configured trusted proxy.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/inbounds/list',
        summary:
          'List every inbound owned by the authenticated user, including each inbound’s clientStats traffic counters. settings, streamSettings, and sniffing are returned as nested JSON objects (no escaped strings); legacy callers that send them back as JSON-encoded strings are still accepted on write.',
        responseSchema: 'Inbound',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/list/slim',
        summary:
          'Same shape as /list but with settings.clients[] stripped down to {email, enable, comment} and ClientStats not enriched with UUID/SubId. Use this for list pages; fetch /get/:id when you need the full per-client payload (uuid, password, flow, ...).',
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "id": 1,\n      "remark": "VLESS-443",\n      "settings": {\n        "clients": [\n          { "email": "alice", "enable": true }\n        ],\n        "decryption": "none"\n      },\n      "clientStats": []\n    }\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/options',
        summary:
          'Lightweight picker projection of the authenticated user’s inbounds. Returns id, remark, tag, protocol, port, a server-computed tlsFlowCapable flag (true for VLESS on TCP with tls or reality, or on XHTTP with VLESS encryption / vlessenc enabled), and ssMethod (the Shadowsocks cipher, empty for non-Shadowsocks inbounds — used by the client UI to generate a valid Shadowsocks 2022 PSK). Use this for dropdowns and attach pickers — it skips settings, streamSettings, and clientStats so the payload stays small even on panels with thousands of clients.',
        responseSchema: 'InboundOption',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/amneziawgKernelReadiness',
        responseSchema: 'Readiness',
        summary:
          'Whether this host can run an AmneziaWG inbound on the kernel module. Returns ready, host blocker codes (root/tools/module/nft), and the command that fixes them. Saving an inbound also checks its features and rejects IPv6, port forwarding, routeThroughXray, or a pinned listen address that the kernel backend cannot honor.',
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/allLinks',
        responseObjectSchema: { type: 'array', nullable: true, items: { type: 'string' } },
        summary:
          'Return every protocol URL (vless://, vmess://, trojan://, ss://, hysteria://, mtproto) across all inbounds and all of their clients. Links are rendered through the subscription engine, so the configured remark template (name-only display part) is applied per client — the same output the client info/QR pages use. Protocols without a URL form (socks, http, mixed, wireguard, dokodemo, tunnel) contribute nothing. Used by the panel’s "Export all inbound links" action.',
        response:
          '{\n  "success": true,\n  "obj": [\n    "vless://uuid@host:443?security=reality&...#Germany-alice",\n    "vmess://eyJ2IjoyLC..."\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/get/:id',
        summary: 'Fetch a single inbound by numeric ID.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/add',
        summary:
          'Create a new inbound. Send the full inbound payload (protocol, port, settings, streamSettings, sniffing, remark, expiryTime, total, enable). settings, streamSettings, and sniffing may be sent as nested JSON objects (preferred) or as JSON-encoded strings (legacy). VLESS empty fallback arrays are normalized away; nonempty fallbacks cannot be combined with active server decryption.',
        body: inboundBody,
        errorResponse: '{\n  "success": false,\n  "msg": "Port 443 is already in use"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/del/:id',
        summary: 'Delete an inbound by ID. Also removes its associated client stats rows.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/bulkDel',
        summary:
          'Delete many inbounds in one call. Processes the list sequentially; failures are reported per id and the rest still proceed. Restarts xray at most once.',
        body: '{\n  "ids": [1, 2, 3]\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "deleted": 2,\n    "skipped": [\n      { "id": 3, "reason": "..." }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/update/:id',
        summary:
          'Replace an inbound’s configuration. Body shape mirrors /add, but the inbound keeps its stored client list and enable flag: settings.clients and enable in the body are ignored. Manage clients through the /panel/api/clients endpoints and toggle the inbound with /setEnable. Remove effective fallback rules before enabling VLESS server decryption. A linked inbound carries the edit to its copies, each keeping its own node, tag, traffic, users, switch and share address.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
        body: inboundBody,
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/deploy/:id',
        summary:
          'Put linked copies of an inbound on more nodes (0 is this panel): the same settings and port, its users attached. From then on an edit to any copy reaches all, and a user put on or taken off one is put on or taken off all. A node already holding a copy is skipped.',
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' },
          {
            name: 'nodeIds',
            in: 'body',
            type: 'integer[]',
            desc: 'Nodes to deploy to; 0 is this panel.',
          },
        ],
        body: '{\n  "nodeIds": [2, 3]\n}',
        responseSchema: 'InboundDeployResult',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/unlink/:id',
        summary:
          'Take one copy out of its linked group so it can be edited on its own. The other copies stay linked.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/setEnable/:id',
        summary:
          'Toggle only the enable flag without serialising the whole settings JSON. Recommended for UI switches on large inbounds.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
        body: '{\n  "enable": false\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/:id/subSortIndex',
        summary:
          'Set only the subscription sort order. Reads the stored inbound, so a reorder cannot carry a stale client list over a concurrent edit.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
        body: '{\n  "subSortIndex": 2\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/:id/resetTraffic',
        summary:
          'Zero out upload + download counters for a single inbound. Does not touch per-client counters.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/:id/delAllClients',
        summary:
          'Remove every client attached to a single inbound while keeping the inbound itself. Collects emails from settings.clients[] and feeds them into the optimized bulk-delete path (runtime user removal + traffic-row cleanup + SyncInbound). Destructive and cannot be undone.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Inbound ID.' }],
        response: '{\n  "success": true,\n  "obj": {\n    "deleted": 12\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/resetAllTraffics',
        summary:
          'Reset upload + download counters on every inbound. Destructive — accounting history is lost.',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/import',
        summary:
          'Bulk-import an inbound from a JSON blob (e.g. one exported via the UI). The body uses form encoding with a single "data" field. VLESS fallback validation is identical to /add.',
        params: [
          {
            name: 'data',
            in: 'body (form)',
            type: 'string',
            desc: 'JSON-encoded inbound payload.',
          },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/engines',
        responseObjectSchema: {
          type: 'array',
          items: { $ref: '#/components/schemas/EngineStatus' },
        },
        summary:
          'For each protocol that is served outside Xray (OpenVPN, WireGuard), whether the program it needs is installed on the panel server, and the system package that brings it. An inbound of a protocol whose engine is missing is saved but does not start until the package is installed.',
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/wireguard/importable',
        responseSchema: 'WireGuardImportSources',
        summary:
          'List the WireGuard interfaces in /etc/wireguard on the panel server that are not inbounds yet, such as the ones WGDashboard or wg-quick run. Each entry says whether it is up and, when it cannot be imported, why. Also returns the WGDashboard database peer names and usage would be read from, and whether WGDashboard is still running.',
        params: [
          {
            name: 'dashboardDb',
            in: 'query',
            type: 'string',
            optional: true,
            desc: 'Path of the WGDashboard database on the server. Leave it out to look in the usual places.',
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/wireguard/import',
        responseSchema: 'WireGuardImportResult',
        summary:
          'Turn an existing WireGuard interface of the panel server into an inbound. The interface keeps its name, port, keys, addresses and hooks, and every peer becomes a client with its key and address unchanged, so the configs already handed out keep working. With a WGDashboard database, peers also keep their names, their usage and their private keys, and restricted peers are imported disabled. The old config is kept beside the new one as <name>.conf.before-manifold; if the inbound cannot be created the interface is put back as it was.',
        body: '{\n  "interface": "wg0",\n  "dashboardDb": ""\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/pushClientTraffics',
        summary:
          "Receive a master panel's aggregated per-client usage, keyed by the master's GUID. Stored in a side table used only for the UI display overlay and local quota enforcement — never folded into the local counters that masters poll, so delta accounting stays intact. Called panel-to-panel by the node traffic sync job.",
        params: [
          {
            name: 'masterGuid',
            in: 'body (json)',
            type: 'string',
            desc: 'Stable GUID of the pushing master panel.',
          },
          {
            name: 'traffics',
            in: 'body (json)',
            type: 'object[]',
            desc: 'Client traffic rows; only email/up/down are read.',
          },
        ],
        body: '{\n  "masterGuid": "9f6c2d-…",\n  "traffics": [\n    { "email": "alice", "up": 1048576, "down": 2097152 }\n  ]\n}',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/inbounds/:id/fallbacks',
        summary:
          "List the fallback rules attached to a master VLESS/Trojan TCP-TLS inbound. Each rule links one child inbound (the dest) to optional SNI/ALPN/path/dest/xver match criteria. When dest is empty the child inbound's listen+port is used.",
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Master inbound ID.' }],
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "id": 1,\n      "masterId": 10,\n      "childId": 11,\n      "name": "",\n      "alpn": "",\n      "path": "/vlws",\n      "dest": "",\n      "xver": 2,\n      "sortOrder": 0\n    }\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/inbounds/:id/fallbacks',
        summary:
          'Replace the entire fallback list for a master inbound. Body is JSON. Nonempty lists require TCP with TLS/REALITY and no active VLESS server decryption. An empty list can remove rules even from an incompatible inbound. Triggers an Xray restart.',
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Master inbound ID.' },
          {
            name: 'fallbacks',
            in: 'body (json)',
            type: 'object[]',
            desc: 'Array of {childId, name, alpn, path, dest, xver, sortOrder} entries. Leave dest empty to auto-resolve from the child inbound\'s listen+port; set it (e.g. "8443", "127.0.0.1:8443", "/dev/shm/x.sock") to override.',
          },
        ],
        body: '{\n  "fallbacks": [\n    { "childId": 11, "path": "/vlws", "xver": 2 },\n    { "childId": 12, "alpn": "h2", "dest": "8443" }\n  ]\n}',
        response: '{\n  "success": true,\n  "msg": "Inbound updated"\n}',
      },
    ],
  },

  {
    id: 'server',
    title: 'Server',
    description:
      'System status, log retrieval, certificate generators, Xray binary management, and backup/restore. All under /panel/api/server.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/openapi.json',
        summary:
          'Serve this API description as an OpenAPI 3 document — the same file that powers the API Docs page. Requires a session or Bearer token like the rest of /panel/api. Useful for generating clients or importing into API tooling.',
      },
      {
        method: 'GET',
        path: '/panel/api/metrics',
        summary:
          "Prometheus metrics in the text exposition format: this server, Xray, users by state, today's traffic, inbound traffic, nodes, webhooks and backups. Scrape it with an API token that has stats:read (authorization: Bearer). users=1 adds a series per user, which a large panel should leave out.",
        params: [
          {
            name: 'users',
            in: 'query',
            type: 'string',
            desc: '1 adds manifold_user_traffic_bytes_total and manifold_user_quota_bytes per user.',
            optional: true,
          },
        ],
        response:
          '# HELP manifold_users Users by state; expired and limited overlap.\n# TYPE manifold_users gauge\nmanifold_users{state="active"} 96\n…',
      },
      {
        method: 'GET',
        path: '/panel/api/server/summary',
        summary:
          "The dashboard's business tiles: users by state and online, today's traffic, nodes online, webhook deliveries waiting or given up in 24 hours, failing backup targets and the last good backup.",
        responseSchema: 'PanelSummary',
      },
      {
        method: 'GET',
        path: '/panel/api/server/status',
        summary:
          'Real-time machine snapshot: CPU, memory, swap, disk, network IO, load averages, open connections, Xray state. Cached and refreshed every 2 seconds in the background.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "cpu": 12.5,\n    "mem": { "current": 2147483648, "total": 8589934592 },\n    "swap": { "current": 0, "total": 4294967296 },\n    "disk": { "current": 53687091200, "total": 268435456000 },\n    "netIO": { "up": 1073741824, "down": 2147483648 },\n    "xray": { "state": "running", "version": "v25.10.31" },\n    "tcpCount": 42,\n    "load": { "load1": 0.5, "load5": 0.3, "load15": 0.2 }\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/fail2banStatus',
        summary:
          'Reports whether per-client IP limits can be enforced on this host. The panel uses it to gate the "IP Limit" field, since enforcement depends on Fail2ban being installed.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "enabled": true,\n    "installed": true,\n    "usable": true,\n    "windows": false\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/cpuHistory/:bucket',
        summary:
          'Legacy: aggregated CPU history. Use /history/cpu/:bucket instead — same data with a uniform {t, v} shape.',
        params: [
          {
            name: 'bucket',
            in: 'path',
            type: 'number',
            desc: 'Bucket size in seconds. Allowed: 2, 30, 60, 120, 180, 300.',
          },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/server/history/:metric/:bucket',
        summary:
          'Aggregated time-series for one metric. Returns an array of {t, v} samples covering the last ~6 hours.',
        params: [
          {
            name: 'metric',
            in: 'path',
            type: 'string',
            desc: 'cpu | mem | netUp | netDown | online | load1 | load5 | load15.',
          },
          {
            name: 'bucket',
            in: 'path',
            type: 'number',
            desc: 'Bucket size in seconds. Allowed: 2, 30, 60, 120, 180, 300.',
          },
        ],
        response:
          '{\n  "success": true,\n  "obj": [\n    { "t": 1700000000, "v": 12.5 },\n    { "t": 1700000002, "v": 13.1 }\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/xrayMetricsState',
        summary:
          'Xray runtime metrics state — whether the xray config has a `metrics` block, which expvar keys are flowing, and the current snapshot values for each. Returns an empty state when metrics are not configured.',
      },
      {
        method: 'GET',
        path: '/panel/api/server/xrayMetricsHistory/:metric/:bucket',
        summary:
          'Time-series history for one Xray runtime metric over the last ~6 hours. Same {t, v} shape as /history/:metric/:bucket.',
        params: [
          {
            name: 'metric',
            in: 'path',
            type: 'string',
            desc: 'xrAlloc | xrSys | xrHeapObjects | xrNumGC | xrPauseNs.',
          },
          {
            name: 'bucket',
            in: 'path',
            type: 'number',
            desc: 'Bucket size in seconds. Allowed: 2, 30, 60, 120, 180, 300.',
          },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/server/xrayObservatory',
        summary:
          'Latest snapshot from the Xray observatory — per-outbound latency, health status, and last-probe time. Only populated when the Xray config has an observatory configured.',
      },
      {
        method: 'GET',
        path: '/panel/api/server/xrayObservatoryHistory/:tag/:bucket',
        summary:
          'Time-series of observatory probe results for one outbound tag. Same {t, v} shape as the other history endpoints.',
        params: [
          {
            name: 'tag',
            in: 'path',
            type: 'string',
            desc: 'Outbound tag from the observatory config.',
          },
          {
            name: 'bucket',
            in: 'path',
            type: 'number',
            desc: 'Bucket size in seconds. Allowed: 2, 30, 60, 120, 180, 300.',
          },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/server/getXrayVersion',
        summary: 'List Xray binary versions available for install on this host.',
        response: '{\n  "success": true,\n  "obj": ["v25.10.31", "v25.9.15", "v25.8.1"]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getPanelUpdateInfo',
        summary:
          'Report the running version. The panel never updates itself, so the latest version always equals the current one and updateAvailable is false.',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getUpdateStatus',
        summary:
          'Report the outcome of the most recently launched panel self-update (see POST updatePanel). Compare the returned runId against the one updatePanel returned to tell this run apart from a stale result.',
        responseSchema: 'PanelUpdateStatus',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getConfigJson',
        summary: 'Return the assembled Xray config that\u2019s currently running on this host.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "log": { "loglevel": "warning" },\n    "inbounds": [...],\n    "outbounds": [...],\n    "routing": { "rules": [...] }\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getDb',
        summary:
          'Stream a full database backup as an attachment: the SQLite .db file on SQLite panels, or a pg_dump custom-format archive (.dump) on PostgreSQL panels. Use as a manual backup.',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getMigration',
        summary:
          'Stream a cross-engine migration file as an attachment: a .dump (SQL text) on SQLite, or a .db SQLite database built from the live data on PostgreSQL.',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getNewUUID',
        summary: 'Generate a fresh UUID v4. Convenience helper for client IDs.',
        responseSchema: 'NewUUIDResponse',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getWebCertFiles',
        summary:
          'Return this panel\'s own web TLS certificate and key file paths. The central panel calls it on a node (via the node API token) so "Set Cert from Panel" fills a node-assigned inbound with paths that exist on the node.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "webCertFile": "/root/cert/example.com/fullchain.pem",\n    "webKeyFile": "/root/cert/example.com/privkey.pem"\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/descendants',
        summary:
          'Read-only summaries (guid, parentGuid, name, address, status, versions) of the nodes this panel manages. A parent panel calls it on a node (via the node API token) to surface transitive sub-nodes in a chained topology. Counts are computed by the parent, not returned here.',
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "guid": "c3d4-...",\n      "parentGuid": "a1b2-...",\n      "name": "Node3",\n      "address": "10.0.0.3",\n      "status": "online"\n    }\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getNewX25519Cert',
        summary: 'Generate a new X25519 keypair for Reality.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "privateKey": "uN9qLfV3zH8w...",\n    "publicKey": "5v8xPqR2sM7k..."\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getNewmldsa65',
        summary: 'Generate a new ML-DSA-65 keypair. Returns {seed, verify}.',
        responseSchema: 'MLDSA65Response',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getNewmlkem768',
        summary: 'Generate a new ML-KEM-768 keypair. Returns {seed, client}.',
        responseSchema: 'MLKEM768Response',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getNewVlessEnc',
        summary:
          'Generate VLESS encryption auth options. Returns an auths array each with id, label, encryption, and decryption fields.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "auths": [\n      { "id": 0, "label": "Auth #0", "encryption": "aes-256-gcm", "decryption": "" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/stopXrayService',
        summary: 'Stop the Xray binary. All proxies go offline immediately.',
        errorResponse: '{\n  "success": false,\n  "msg": "Xray is not running"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/restartXrayService',
        summary:
          'Reload Xray with the current config. Typically required after structural inbound or routing changes.',
        errorResponse: '{\n  "success": false,\n  "msg": "Xray config is invalid: ..."\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/installXray/:version',
        summary:
          'Download and install the specified Xray version. Pass "latest" for the newest release.',
        params: [
          {
            name: 'version',
            in: 'path',
            type: 'string',
            desc: 'Xray tag (e.g. v25.10.31) or "latest".',
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/server/updatePanel',
        summary:
          'Always refused: the panel never downloads or runs an updater. Update with the installer. Bearer-token callers need admin or node-admin scope.',
        params: [
          {
            name: 'dev',
            in: 'body (form)',
            type: 'boolean',
            desc: "Override this run's channel. Omit to use the panel's configured channel.",
            optional: true,
          },
        ],
        response: '{\n  "success": true,\n  "obj": {\n    "runId": "1735689600123456789"\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/setUpdateChannel',
        summary:
          'Toggle the panel update channel between stable and the rolling per-commit dev release. Only effective on dev builds.',
        params: [
          {
            name: 'dev',
            in: 'body (form)',
            type: 'boolean',
            desc: 'true = dev channel, false = stable.',
          },
        ],
        body: 'dev=true',
      },
      {
        method: 'POST',
        path: '/panel/api/server/updateGeofile',
        summary:
          'Refresh the default GeoIP / GeoSite data files. Use the /:fileName variant to update one file.',
      },
      {
        method: 'POST',
        path: '/panel/api/server/updateGeofile/:fileName',
        summary: 'Refresh a single Geo file by filename (e.g. geoip.dat, geosite.dat).',
        params: [
          {
            name: 'fileName',
            in: 'path',
            type: 'string',
            desc: 'Filename of the data file to refresh.',
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/server/logs/:count',
        summary: 'Return the last N lines of the panel\u2019s own log.',
        params: [
          { name: 'count', in: 'path', type: 'number', desc: 'Number of trailing log lines.' },
          {
            name: 'level',
            in: 'body (form)',
            type: 'string',
            desc: 'Minimum log level filter.',
            optional: true,
          },
          {
            name: 'syslog',
            in: 'body (form)',
            type: 'boolean',
            desc: 'Read system logs instead of the panel log.',
            optional: true,
          },
        ],
        body: 'level=info&syslog=false',
        responseObjectSchema: { type: 'array', nullable: true, items: { type: 'string' } },
        response:
          '{\n  "success": true,\n  "obj": [\n    "2025/01/01 12:00:00 [INFO] Server started",\n    "2025/01/01 12:00:01 [INFO] Xray is running"\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/xraylogs/:count',
        summary: 'Return the last N lines of the Xray process log.',
        params: [
          { name: 'count', in: 'path', type: 'number', desc: 'Number of trailing log lines.' },
          {
            name: 'filter',
            in: 'body (form)',
            type: 'string',
            desc: 'Keyword filter — only lines containing this string.',
            optional: true,
          },
          {
            name: 'showDirect',
            in: 'body (form)',
            type: 'string',
            desc: '"true" to include direct (freedom) traffic lines.',
            optional: true,
          },
          {
            name: 'showBlocked',
            in: 'body (form)',
            type: 'string',
            desc: '"true" to include blocked (blackhole) traffic lines.',
            optional: true,
          },
          {
            name: 'showProxy',
            in: 'body (form)',
            type: 'string',
            desc: '"true" to include proxy traffic lines.',
            optional: true,
          },
        ],
        body: 'filter=error&showDirect=false&showBlocked=true&showProxy=true',
        responseSchema: 'LogEntry',
        responseSchemaArray: true,
        responseSchemaArrayNullable: true,
      },
      {
        method: 'POST',
        path: '/panel/api/server/amneziawglogs/:count',
        summary:
          'Return live AmneziaWG peer activity (handshake, endpoint, transfer) plus the panel’s own AmneziaWG event lines.',
        params: [
          {
            name: 'count',
            in: 'path',
            type: 'number',
            desc: 'Maximum peer rows and event lines to return.',
          },
          {
            name: 'filter',
            in: 'body (form)',
            type: 'string',
            desc: 'Keyword filter — only rows/lines containing this string.',
            optional: true,
          },
        ],
        body: 'filter=awg1',
        responseSchema: 'AmneziaWGLogs',
      },
      {
        method: 'POST',
        path: '/panel/api/server/importDB',
        summary:
          'Restore the panel DB from an uploaded backup (multipart form, field name "db"). SQLite panels accept a SQLite database (.db) or a SQLite migration dump (.dump); PostgreSQL panels accept a pg_dump archive (.dump), a SQLite database (.db), or a SQLite migration dump. The panel restarts after restore. Destructive.',
        params: [
          {
            name: 'db',
            in: 'body (multipart)',
            type: 'file',
            desc: 'Database backup or migration file to upload.',
          },
          {
            name: 'keepHostSettings',
            in: 'body (multipart)',
            type: 'boolean',
            desc: "Keep this machine's addresses, certificates and node identity. Default true.",
            optional: true,
            defaultValue: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/server/getNewEchCert',
        summary:
          'Generate a new ECH (Encrypted Client Hello) keypair and config list for the given SNI.',
        params: [
          {
            name: 'sni',
            in: 'body (form)',
            type: 'string',
            desc: 'Server Name Indication to generate the ECH config for.',
          },
        ],
        body: 'sni=example.com',
        response:
          '{\n  "success": true,\n  "obj": {\n    "echKeySet": "...",\n    "echServerKeys": [...],\n    "echConfigList": "..."\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/getCertHash',
        summary:
          'Compute the hex SHA-256 of a certificate (DER) for pinning (pinnedPeerCertSha256). Provide either a server file path or inline PEM/DER content.',
        bodyRequiredOneOf: ['certFile', 'certContent'],
        params: [
          {
            name: 'certFile',
            in: 'body (form)',
            type: 'string',
            desc: 'Path to a certificate file on the server. Takes precedence over certContent.',
            optional: true,
            pattern: '.*\\S.*',
          },
          {
            name: 'certContent',
            in: 'body (form)',
            type: 'string',
            desc: 'Inline PEM (or DER) certificate content, used when certFile is empty.',
            optional: true,
            pattern: '.*\\S.*',
          },
        ],
        body: 'certFile=/root/cert.crt',
        response: '{\n  "success": true,\n  "obj": [\n    "e8e2d3..."\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/server/getFullBackup',
        summary:
          "Download a full backup for moving this panel to another server: a .tar.gz with the database, every certificate and key the settings and inbounds point at, and a manifest naming this server's version and public address.",
        response: '(binary .tar.gz)',
      },
      {
        method: 'POST',
        path: '/panel/api/server/inspectBackup',
        summary:
          "Check a full backup against this server and change nothing: the addresses users' configs carry and what the move does to each, the tunnels and what their other ends need, the files it brings, and every port, program or version that would stop it.",
        params: [
          {
            name: 'backup',
            in: 'body (multipart)',
            type: 'file',
            desc: 'A .tar.gz from getFullBackup.',
          },
        ],
        responseSchema: 'MigrationReport',
      },
      {
        method: 'POST',
        path: '/panel/api/server/restoreBackup',
        summary:
          "Restore a full backup on this server: write its certificates, replace the database, make inbounds and tunnels bound to the old addresses use this server's, resend relayed tunnels with this server's address, then restart the panel.",
        params: [
          {
            name: 'backup',
            in: 'body (multipart)',
            type: 'file',
            desc: 'A .tar.gz from getFullBackup.',
          },
          {
            name: 'keepPanelAccess',
            in: 'body (multipart)',
            type: 'boolean',
            desc: "Keep this server's panel port, path and certificate (default true), so the panel stays where you reach it now.",
            optional: true,
          },
          {
            name: 'rewriteAddress',
            in: 'body (multipart)',
            type: 'boolean',
            desc: "Replace the old server's IP with this one's in hosts, inbound share addresses, external proxies and subscription URLs. Users then need their configs again; leave it off to keep the old IP by moving it.",
            optional: true,
          },
        ],
        responseSchema: 'MigrationReport',
      },
      {
        method: 'POST',
        path: '/panel/api/server/openvpnKeys',
        summary:
          'Issue a new key set for an OpenVPN inbound: a CA, a server certificate signed by it and a control channel key. Nothing is saved; the inbound keeps its keys until it is saved with these.',
        params: [
          {
            name: 'keyType',
            in: 'body (form)',
            type: 'string',
            desc: 'ecdsa-p256 (default), ecdsa-p384, rsa-2048 or rsa-4096. RSA is for routers whose OpenVPN knows no ECDSA.',
            optional: true,
          },
          {
            name: 'days',
            in: 'body (form)',
            type: 'number',
            desc: 'How long both certificates last, 1 to 36500 days (default 3650).',
            optional: true,
          },
          {
            name: 'caName',
            in: 'body (form)',
            type: 'string',
            desc: 'Common name of the CA (default "Manifold OpenVPN CA").',
            optional: true,
          },
          {
            name: 'serverName',
            in: 'body (form)',
            type: 'string',
            desc: 'Common name of the server certificate, which every profile verifies. Letters, digits, ".", "_" and "-".',
            optional: true,
          },
        ],
        body: 'keyType=rsa-2048&days=3650&serverName=vpn.example.com',
        responseSchema: 'OpenVPNKeySet',
      },
      {
        method: 'POST',
        path: '/panel/api/server/openvpnCertInfo',
        summary:
          'Describe a PEM certificate of an OpenVPN inbound: its names, key type, validity and SHA-256 fingerprint.',
        params: [
          {
            name: 'cert',
            in: 'body (form)',
            type: 'string',
            desc: 'The certificate in PEM.',
            pattern: '.*\\S.*',
          },
        ],
        body: 'cert=-----BEGIN CERTIFICATE-----...',
        responseSchema: 'OpenVPNCertInfo',
      },
      {
        method: 'POST',
        path: '/panel/api/server/getRemoteCertHash',
        summary:
          'Run `xray tls ping` against a remote server and return its live leaf-certificate SHA-256 hash(es) for pinning (pinnedPeerCertSha256).',
        params: [
          {
            name: 'server',
            in: 'body (form)',
            type: 'string',
            desc: 'Remote server as domain or domain:port (default port 443), e.g. cloudflare-dns.com.',
          },
          {
            name: 'allowPrivate',
            in: 'body (form)',
            type: 'boolean',
            optional: true,
            desc: 'Ping a private/internal/loopback server (LAN, Docker service name). Default false (SSRF guard blocks it and the error response sets obj.privateTarget=true).',
          },
        ],
        body: 'server=cloudflare-dns.com',
        response: '{\n  "success": true,\n  "obj": [\n    "e8e2d3..."\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/server/scanRealityTarget',
        summary:
          'Run a live TLS 1.3 probe against a candidate REALITY target and return a feasibility verdict (TLS 1.3 + h2 + X25519 + trusted certificate) plus the certificate SAN DNS names. A target on a private/loopback address is reported with privateTarget=true and probed only when allowPrivate is set.',
        params: [
          {
            name: 'target',
            in: 'body (form)',
            type: 'string',
            desc: 'Candidate target as host or host:port (default port 443), e.g. www.cloudflare.com:443.',
          },
          {
            name: 'sni',
            in: 'body (form)',
            type: 'string',
            optional: true,
            desc: 'SNI the handshake sends and the certificate is verified against (the inbound serverNames). Defaults to the target host, which a fronting proxy answers with its default certificate.',
          },
          {
            name: 'xver',
            in: 'body (form)',
            type: 'number',
            optional: true,
            desc: 'PROXY protocol version the target expects (matches the inbound xver). 0 = none.',
          },
          {
            name: 'allowPrivate',
            in: 'body (form)',
            type: 'boolean',
            optional: true,
            desc: 'Probe a private/internal/loopback target (LAN, Docker service name). Default false (SSRF guard blocks it and the response sets privateTarget=true).',
          },
        ],
        body: 'target=www.cloudflare.com:443',
        responseSchema: 'RealityScanResult',
      },
      {
        method: 'POST',
        path: '/panel/api/server/scanRealityTargets',
        summary:
          'Probe/discover REALITY targets and return each verdict ranked by feasibility then latency. Each comma-separated token may be a domain (validated with SNI), a bare IP, or a CIDR range (discovered without SNI by reading the certificate domain). When empty, the realityScanCandidates setting is probed (the built-in seed list if that setting is empty).',
        params: [
          {
            name: 'targets',
            in: 'body (form)',
            type: 'string',
            optional: true,
            desc: 'Optional comma-separated tokens: domain[:port], IP[:port], or CIDR (e.g. 104.16.0.0/24). When omitted, the realityScanCandidates setting is probed (the built-in seed list if that setting is empty).',
          },
        ],
        body: 'targets=104.16.0.0/24,www.apple.com:443',
        responseSchema: 'RealityScanResult',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/server/clientIps',
        summary:
          'Fetch the fully aggregated inbound_client_ips database table. Used by nodes to sync recently active IPs across the cluster.',
        responseSchema: 'InboundClientIps',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/server/clientIps',
        summary:
          'Submit a list of recently active IP timestamps. The panel merges them with the existing database to maintain a unified global IP-limit view.',
        requestSchema: {
          type: 'array',
          items: {
            type: 'object',
            properties: {
              clientEmail: { type: 'string' },
              ips: {
                type: 'array',
                nullable: true,
                items: {
                  type: 'object',
                  properties: { ip: { type: 'string' }, timestamp: { type: 'integer' } },
                  required: ['ip', 'timestamp'],
                },
              },
            },
            required: ['clientEmail', 'ips'],
          },
        },
      },
    ],
  },

  {
    id: 'clients',
    title: 'Clients',
    description:
      'Manage clients as first-class entities that can be attached to one or more inbounds. A single client row drives the settings.clients entry in every inbound it belongs to. Endpoints live under /panel/api/clients.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/clients/list',
        summary:
          'List every client with its attached inbound IDs and traffic record. The reverse field, if set, is returned as a nested JSON object (legacy JSON-encoded-string form is still accepted on write).',
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "id": 1,\n      "email": "alice@example.com",\n      "subId": "abcd1234",\n      "uuid": "...",\n      "totalGB": 53687091200,\n      "expiryTime": 1735689600000,\n      "enable": true,\n      "reverse": null,\n      "inboundIds": [3, 5],\n      "traffic": { "up": 1024, "down": 4096, "enable": true }\n    }\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/list/paged',
        summary:
          'Filter, sort, and paginate clients on the server. Each item is a slim row (no uuid/password/auth/flow/security/reverse/tgId) so the clients page can ship 25-ish rows in a few KB instead of the full table. The response also includes a summary computed across the full DB row set so dashboard counters stay stable as the user paginates or filters: the *Count fields are exact, while the email arrays beside them stop at 200 entries so the payload does not grow with the panel. Page size capped at 200; fetch /get/:email to obtain the full per-client payload for an edit/info modal.',
        params: [
          {
            name: 'page',
            in: 'query',
            type: 'number',
            desc: '1-indexed page number. Defaults to 1.',
            optional: true,
            defaultValue: 1,
          },
          {
            name: 'pageSize',
            in: 'query',
            type: 'number',
            desc: 'Rows per page. Defaults to 25, capped at 200.',
            optional: true,
            defaultValue: 25,
          },
          {
            name: 'search',
            in: 'query',
            type: 'string',
            desc: 'Case-insensitive substring match on email, subId, comment, UUID, password, auth or Telegram ID.',
            optional: true,
          },
          {
            name: 'filter',
            in: 'query',
            type: 'string',
            desc: 'CSV status buckets: online, active, deactive, depleted or expiring. Values are ORed.',
            optional: true,
          },
          {
            name: 'protocol',
            in: 'query',
            type: 'string',
            desc: 'CSV inbound protocols: vmess, vless, trojan, shadowsocks, wireguard, hysteria, http, mixed, tunnel, tun, mtproto or amneziawg. Values are ORed.',
            optional: true,
          },
          {
            name: 'inbound',
            in: 'query',
            type: 'string',
            desc: 'CSV positive inbound IDs. Values are ORed; invalid or non-positive IDs are ignored.',
            optional: true,
          },
          {
            name: 'sort',
            in: 'query',
            type: 'string',
            desc: 'Sort key. An omitted or unknown value falls back to client ID ascending.',
            optional: true,
            enum: [
              'enable',
              'email',
              'inboundIds',
              'traffic',
              'remaining',
              'expiryTime',
              'createdAt',
              'updatedAt',
              'lastOnline',
            ],
          },
          {
            name: 'order',
            in: 'query',
            type: 'string',
            desc: 'Sort direction. Only descend selects descending order; otherwise ascending.',
            optional: true,
            enum: ['ascend', 'descend'],
          },
          {
            name: 'expiryFrom',
            in: 'query',
            type: 'number',
            desc: 'Inclusive minimum expiry time in Unix milliseconds. Zero or negative means unset.',
            optional: true,
          },
          {
            name: 'expiryTo',
            in: 'query',
            type: 'number',
            desc: 'Inclusive maximum expiry time in Unix milliseconds. Zero or negative means unbounded.',
            optional: true,
          },
          {
            name: 'usageFrom',
            in: 'query',
            type: 'number',
            desc: 'Inclusive minimum combined upload and download usage in bytes. Zero means unset.',
            optional: true,
          },
          {
            name: 'usageTo',
            in: 'query',
            type: 'number',
            desc: 'Inclusive maximum combined upload and download usage in bytes. Zero means unbounded.',
            optional: true,
          },
          {
            name: 'autoRenew',
            in: 'query',
            type: 'string',
            desc: 'on selects clients with an interval or calendar-day reset; off selects clients without either.',
            optional: true,
            enum: ['on', 'off'],
          },
          {
            name: 'hasTgId',
            in: 'query',
            type: 'string',
            desc: 'yes selects clients with a non-zero Telegram ID; no selects clients without one.',
            optional: true,
            enum: ['yes', 'no'],
          },
          {
            name: 'hasComment',
            in: 'query',
            type: 'string',
            desc: 'yes selects clients with a non-blank comment; no selects clients without one.',
            optional: true,
            enum: ['yes', 'no'],
          },
          {
            name: 'group',
            in: 'query',
            type: 'string',
            desc: 'CSV group names, matched case-insensitively after trimming. Values are ORed.',
            optional: true,
          },
        ],
        responseSchema: 'ClientPageResponse',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/get/:email',
        summary:
          'Fetch one client by email, including the inbound IDs and external config IDs it is attached to.',
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email (unique identifier).' },
        ],
        response:
          '{\n  "success": true,\n  "obj": {\n    "client": { "id": 1, "email": "alice@example.com", ... },\n    "inboundIds": [3, 5],\n    "externalLinks": [\n      { "id": 11, "kind": "link", "value": "vless://...", "remark": "DE", "enable": true, "expiryTime": 0 },\n      { "id": 12, "kind": "subscription", "value": "https://provider.example/sub/abc", "remark": "Provider", "enable": false, "expiryTime": 1767225600000, "namePrefix": "[zjh] ", "lastFetchAt": 1767220000000, "lastFetchError": "" }\n    ]\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/get',
        summary:
          'Fetch one client by email, including the inbound IDs and external config IDs it is attached to.',
        description:
          'Query-parameter alias for the path endpoint. Use email=. or email=.. to manage legacy reserved names without URL dot-segment normalization.',
        params: [
          { name: 'email', in: 'query', type: 'string', desc: 'Client email (unique identifier).' },
        ],
        response:
          '{\n  "success": true,\n  "obj": {\n    "client": { "id": 1, "email": "alice@example.com", ... },\n    "inboundIds": [3, 5],\n    "externalLinks": [\n      { "id": 11, "kind": "link", "value": "vless://...", "remark": "DE", "enable": true, "expiryTime": 0 },\n      { "id": 12, "kind": "subscription", "value": "https://provider.example/sub/abc", "remark": "Provider", "enable": false, "expiryTime": 1767225600000, "namePrefix": "[zjh] ", "lastFetchAt": 1767220000000, "lastFetchError": "" }\n    ]\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/get/subId/:subId',
        summary: 'Fetch clients by subscription ID.',
        description:
          'Matches the exact subscription ID and orders results by client ID. Returns an array of the same payloads as GET /clients/get/:email, since multiple clients can share a subscription ID. Includes disabled, expired and unattached clients. An unknown ID returns HTTP 200 with success:false, obj:null and a record-not-found message, matching the email lookup. Requires an admin token or a panel login session.',
        params: [
          {
            name: 'subId',
            in: 'path',
            type: 'string',
            desc: 'Exact subscription ID (URL-encoded).',
          },
        ],
        responseObjectSchema: {
          type: 'array',
          nullable: true,
          items: {
            type: 'object',
            properties: {
              client: { $ref: '#/components/schemas/ClientRecord' },
              inboundIds: { type: 'array', items: { type: 'integer' } },
              externalLinks: { type: 'array', items: { type: 'object' } },
              usedTraffic: { type: 'integer', format: 'int64' },
              tunnelAllowedIPs: { type: 'object', additionalProperties: { type: 'string' } },
            },
          },
        },
      },
      {
        method: 'GET',
        path: '/panel/api/clients/get/tgId/:tgId',
        summary:
          'Fetch clients by Telegram user ID. Returns an array since multiple clients can share the same Telegram ID.',
        params: [
          { name: 'tgId', in: 'path', type: 'integer', desc: 'Telegram user ID (numeric).' },
        ],
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "client": { "id": 1, "email": "alice@example.com", ... },\n      "inboundIds": [3, 5],\n      "externalLinks": [],\n      "usedTraffic": 1048576\n    }\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/add',
        summary:
          'Create a new client and attach it to one or more inbounds in a single call. Body is JSON. Per-protocol secrets are generated server-side when omitted, so callers can send only the universal fields.',
        description:
          'Client email must not be exactly `.` or `..`. Existing reserved-name clients can be read, renamed to a valid name, or deleted using the query-parameter /get, /update and /del aliases. Fields the server fills in when they are omitted — a valid value sent by the caller is never overwritten. Re-adding an email that already exists, with its stored `subId`, reuses the stored `id`, `password`, `auth` and `secret` instead of minting new ones, so the identity stays in sync across its inbounds.\n\n- **VLESS / VMess** — `id`, a fresh UUID\n- **Trojan** — `password`\n- **Shadowsocks** — `password`. On a `2022-blake3-*` inbound a supplied password that does not base64-decode to the key length of the cipher (16 or 32 bytes) is replaced by a generated key and the call still succeeds, so read the client back if you did not let the server pick. Legacy ciphers keep any non-empty password\n- **Hysteria** — `auth`\n- **mtproto** — `secret`, a FakeTLS secret derived from the fronting domain of the inbound, or from `www.cloudflare.com` when it has none\n- **WireGuard** — `privateKey` and `publicKey` when both are blank, or `publicKey` alone when only a `privateKey` was sent, plus `allowedIPs`: one free `/32` taken from the /24 the existing peers of that inbound already sit in, or from `10.0.0.0/24` when it has none\n\nAccepted on the same body but never generated: `preSharedKey` and `keepAlive` (WireGuard), `adTag` (mtproto).\n\nWireGuard is the only one of these that can fail. Allocation widens the search to the containing /16 before giving up with `inbound <id>: wireguard: no free address available in <scope>`, and an `allowedIPs` supplied by the caller is validated instead of allocated: `inbound <id>: wireguard: allowedIPs entry <entry> overlaps <address> used by another client` when its range overlaps an address or prefix a different client of that same inbound holds, or `... used by a client on <inbound>` when the holder sits on another WireGuard or AmneziaWG inbound. Ranges are compared, not strings, so `10.0.0.9/24` collides with `10.0.0.5/32`; a `/0` entry collides with every address of its family another client holds, and only a `0.0.0.0/0` or `::/0` default route that is already stored (legacy peers) keeps nobody out. Allocation likewise skips every address inside a prefix another client holds. The same validation runs on POST /panel/api/clients/{email}/attach, where a client that already carries an address brings it along.\n\nAn `inboundIds` entry that names no existing inbound rejects the whole call before anything is written. Past that, the inbounds are applied concurrently and independently: one that fails no longer stops the others, so a `success:false` response can still have created the client on the rest. Every error names the inbound it came from (`inbound 7: <message>`), and several failures are reported together, one per line. `limitHwid` is applied only when every inbound succeeded, so re-run the call after fixing the failure.',
        params: [
          {
            name: 'client',
            in: 'body (json)',
            type: 'object',
            desc: 'Client fields: email, subId, id (uuid), password, auth, flow, totalGB, expiryTime, limitIp (simultaneous IPs on Xray inbounds), userLimit (simultaneous OpenVPN and L2TP connections), limitHwid, tgId (numeric Telegram user ID, 0 = none), comment, enable. Protocol-specific: secret and adTag (mtproto), privateKey, publicKey, preSharedKey, allowedIPs and keepAlive (WireGuard).',
          },
          {
            name: 'inboundIds',
            in: 'body (json)',
            type: 'integer[]',
            desc: 'Inbound IDs to attach the client to. At least one required.',
          },
        ],
        body: '{\n  "client": {\n    "email": "alice@example.com",\n    "totalGB": 53687091200,\n    "expiryTime": 1735689600000,\n    "tgId": 0,\n    "limitIp": 0,\n    "userLimit": 0,\n    "limitHwid": 0,\n    "enable": true\n  },\n  "inboundIds": [3, 5]\n}',
        response: '{\n  "success": true,\n  "msg": "Client added"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/renewalPreview',
        summary: 'Preview client auto-renewal dates without saving or resetting anything.',
        description:
          'Uses the same calendar and catch-up calculation as auto-renew in the panel timezone. resetWeekday is 1 (Monday) to 7 (Sunday), 0 disables weekly mode; it cannot be combined with positive reset or resetDay. Existing resetDay takes precedence over reset. With expiryTime=0, calendar modes suggest a first cutoff but do not activate renewal. Negative expiryTime waits for first-use activation. resetMax and resetCount simulate the existing per-period allowance limit; the preview is informational and does not reserve an allowance or guarantee node availability.',
        params: [
          {
            name: 'expiryTime',
            in: 'body (json)',
            type: 'integer',
            desc: 'Current cutoff in Unix milliseconds; 0 unlimited, negative first-use duration.',
          },
          {
            name: 'reset',
            in: 'body (json)',
            type: 'integer',
            desc: 'Fixed interval in days; 0 disabled.',
          },
          {
            name: 'resetDay',
            in: 'body (json)',
            type: 'integer',
            desc: 'Monthly calendar day 1-31; 0 disabled.',
          },
          {
            name: 'resetWeekday',
            in: 'body (json)',
            type: 'integer',
            desc: 'Weekly calendar day 1-7 (Monday-Sunday); 0 disabled.',
          },
          {
            name: 'resetMax',
            in: 'body (json)',
            type: 'integer',
            desc: 'Maximum renewals; 0 unlimited.',
          },
          {
            name: 'resetCount',
            in: 'body (json)',
            type: 'integer',
            desc: 'Renewals already consumed; defaults to 0.',
          },
        ],
        responseSchema: 'ClientRenewalPreview',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/update/:email',
        summary:
          'Update an existing client by email. Changes propagate to every attached inbound. Body is the JSON client payload: omitted fields keep their current values, and an explicit zero, empty or null value clears a field, except as noted below.',
        description:
          'An omitted `flow` leaves every attached inbound on its own flow. Send `"reverse": null` to remove the reverse tag. Credentials (`id`, `password`, `auth`, `secret`, `subId`) and WireGuard/AmneziaWG `allowedIPs` are never cleared: an empty value keeps the stored one. The inbounds are applied concurrently and independently: one that fails no longer stops the others. Every inbound error names the inbound it came from (`inbound 7: <message>`), and several failures are reported together, one per line. So a `success:false` response can still have applied the edit to the remaining inbounds. The client record is written after the inbounds, so a failure there is reported without an `inbound <id>:` prefix and leaves the inbound edits in place.',
        params: [
          {
            name: 'email',
            in: 'path',
            type: 'string',
            desc: 'Current client email (unique identifier).',
          },
        ],
        body: '{\n  "email": "alice@example.com",\n  "totalGB": 107374182400,\n  "expiryTime": 1767225600000,\n  "limitHwid": 2,\n  "tgId": 123456789,\n  "enable": true\n}',
        response: '{\n  "success": true,\n  "msg": "Client updated"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/update',
        summary:
          'Update an existing client by email. Changes propagate to every attached inbound. Body is the JSON client payload: omitted fields keep their current values, and an explicit zero, empty or null value clears a field, except as noted below.',
        description:
          'Query-parameter alias for the path endpoint. Use email=. or email=.. to manage legacy reserved names without URL dot-segment normalization. An omitted `flow` leaves every attached inbound on its own flow. Send `"reverse": null` to remove the reverse tag. Credentials (`id`, `password`, `auth`, `secret`, `subId`) and WireGuard/AmneziaWG `allowedIPs` are never cleared: an empty value keeps the stored one. The inbounds are applied concurrently and independently: one that fails no longer stops the others. Every inbound error names the inbound it came from (`inbound 7: <message>`), and several failures are reported together, one per line. So a `success:false` response can still have applied the edit to the remaining inbounds. The client record is written after the inbounds, so a failure there is reported without an `inbound <id>:` prefix and leaves the inbound edits in place.',
        params: [
          {
            name: 'email',
            in: 'query',
            type: 'string',
            desc: 'Current client email (unique identifier).',
          },
        ],
        body: '{\n  "email": "alice@example.com",\n  "totalGB": 107374182400,\n  "expiryTime": 1767225600000,\n  "limitHwid": 2,\n  "tgId": 123456789,\n  "enable": true\n}',
        response: '{\n  "success": true,\n  "msg": "Client updated"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/del/:email',
        summary:
          'Delete a client by email. Removes it from every attached inbound and drops its traffic record unless keepTraffic=1 is passed.',
        description:
          'The inbounds are applied concurrently and independently: one that fails no longer stops the others. Every inbound error names the inbound it came from (`inbound 7: <message>`), and several failures are reported together, one per line. So a `success:false` response can still have removed the client from the remaining inbounds; the client record is kept in that case, so re-running the call retries exactly the leftovers. The record and traffic rows are dropped after the inbounds, so a failure there is reported without an `inbound <id>:` prefix and leaves the client already removed from every inbound.',
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email (unique identifier).' },
          {
            name: 'keepTraffic',
            in: 'query',
            type: 'integer',
            desc: 'Pass 1 to retain the xray_client_traffic row after deletion.',
          },
        ],
        response: '{\n  "success": true,\n  "msg": "Client deleted"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/del',
        summary:
          'Delete a client by email. Removes it from every attached inbound and drops its traffic record unless keepTraffic=1 is passed.',
        description:
          'Query-parameter alias for the path endpoint. Use email=. or email=.. to manage legacy reserved names without URL dot-segment normalization. The inbounds are applied concurrently and independently: one that fails no longer stops the others. Every inbound error names the inbound it came from (`inbound 7: <message>`), and several failures are reported together, one per line. So a `success:false` response can still have removed the client from the remaining inbounds; the client record is kept in that case, so re-running the call retries exactly the leftovers. The record and traffic rows are dropped after the inbounds, so a failure there is reported without an `inbound <id>:` prefix and leaves the client already removed from every inbound.',
        params: [
          { name: 'email', in: 'query', type: 'string', desc: 'Client email (unique identifier).' },
          {
            name: 'keepTraffic',
            in: 'query',
            type: 'integer',
            desc: 'Pass 1 to retain the xray_client_traffic row after deletion.',
          },
        ],
        response: '{\n  "success": true,\n  "msg": "Client deleted"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/:email/attach',
        summary: 'Attach an existing client to one or more additional inbounds. Body is JSON.',
        description:
          'A WireGuard client brings its stored `allowedIPs` into the new inbound instead of being given a fresh address, so the call fails with `inbound <id>: wireguard: allowedIPs entry <entry> overlaps <address> used by another client` when its range overlaps an address or prefix a different client of the target inbound holds. Free the address on that inbound first — see POST /panel/api/clients/add for the full rule. Inbounds are applied independently, so the remaining ones are still attached and a `success:false` response can be partial.',
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email (unique identifier).' },
          {
            name: 'inboundIds',
            in: 'body (json)',
            type: 'integer[]',
            desc: 'Inbound IDs to attach.',
          },
        ],
        body: '{\n  "inboundIds": [7, 9]\n}',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/:email/detach',
        summary: 'Detach a client from one or more inbounds without deleting the client.',
        description:
          'The inbounds are applied concurrently and independently: one that fails no longer stops the others. Every inbound error names the inbound it came from (`inbound 7: <message>`), and several failures are reported together, one per line. So a `success:false` response can still have detached the remaining inbounds. Detach writes nothing beyond the inbounds, so every error carries the prefix.',
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email (unique identifier).' },
          {
            name: 'inboundIds',
            in: 'body (json)',
            type: 'integer[]',
            desc: 'Inbound IDs to detach.',
          },
        ],
        body: '{\n  "inboundIds": [5]\n}',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/:email/externalLinks',
        summary:
          "Replace a client's external links and external subscriptions. Sends the full set; the server replaces all rows. Disabled rows stay saved for editing but are not emitted in generated subscriptions. The owning client's disabled or expired state also stops these rows from being emitted on future subscription fetches; credentials already imported by an app remain valid until the external provider revokes them.",
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email (unique identifier).' },
          {
            name: 'externalLinks',
            in: 'body',
            type: 'object[]',
            desc: "Full replacement list; the server replaces all rows. Each row supports { kind, value, remark, enable, expiryTime, namePrefix }. kind=link: value must be a supported share link such as vless://, vmess://, trojan://, ss://, hysteria2://, or wireguard://, and remark overrides the exported node name. kind=subscription: value must be an http(s) subscription URL, and namePrefix is prepended to fetched node names. Omit enable to default true; enable=false or an expired expiryTime keeps the row saved but excludes it from generated subscriptions. expiryTime is a unix millisecond timestamp where 0 means no link-specific expiry; the owning client's enabled state and expiry still apply. A negative value is rejected. Rows are matched by kind+value across saves, so id is ignored on write. lastFetchAt and lastFetchError are read-only status fields returned by GET.",
          },
        ],
        body: '{\n  "externalLinks": [\n    { "kind": "link", "value": "vless://uuid@host:443?...#srv", "remark": "DE", "enable": true, "expiryTime": 0 },\n    { "kind": "subscription", "value": "https://provider.example/sub/abc", "remark": "Provider", "enable": false, "expiryTime": 1767225600000, "namePrefix": "[zjh] " }\n  ]\n}',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/resetAllTraffics',
        summary:
          'Reset the up/down counters for every client globally. Quotas and expiry are not affected. Triggers an Xray restart if any counter actually moved.',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/delDepleted',
        summary:
          'Delete every client whose traffic quota is exhausted (used >= total, when reset is disabled) or whose expiry has passed. Returns the deleted count and triggers an Xray restart when any client was on a running inbound.',
        response: '{\n  "success": true,\n  "obj": {\n    "deleted": 0\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/delExpired',
        summary:
          'Delete every client whose fixed expiry passed more than `days` ago, switched off or not. A length counted from the first connection that never started, and auto-renewing clients, are left alone. With dryRun the clients are only counted.',
        body: '{\n  "days": 30,\n  "dryRun": true\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "count": 4\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/exportCsv',
        summary:
          'A CSV list of users, one row each with its subscription link, for the named emails or, with all, every user the account may see (a reseller only its own). Every export is written to the audit log.',
        body: '{\n  "emails": ["alice", "bob"],\n  "all": false\n}',
        response: '{\n  "success": true,\n  "obj": "name,enabled,...\\nalice,true,..."\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/configPack',
        summary:
          'A zip, base64 encoded, with a folder per user: its subscription link, its share links and the files dedicated apps open (an OpenVPN .ovpn, a WireGuard .conf). Same selection as exportCsv; audited.',
        body: '{\n  "emails": ["alice"]\n}',
        response: '{\n  "success": true,\n  "obj": "UEsDBBQ..."\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/delOrphans',
        summary:
          'Delete every client that is not attached to any inbound, along with its traffic record, IP log, HWID devices, and external links. Useful for clearing clients left unattached after their inbounds were removed. Returns the deleted count. Cannot be undone.',
        response: '{\n  "success": true,\n  "obj": {\n    "deleted": 0\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/export',
        summary:
          'Return every client as a {client, inboundIds, traffic} array — the shape /import accepts — so the payload round-trips straight back through /import. traffic carries the usage counters (up, down, resetCount, lastOnline, lastSubFetch) and is omitted for a client with no traffic row; the quota itself stays in client.totalGB. Clients with no inbound attachment are included with an empty inboundIds list. The UI shows this in a CodeMirror viewer (copy / download); programmatic callers get the array in obj.',
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "client": {\n        "email": "alice@example.com",\n        "id": "...",\n        "totalGB": 53687091200,\n        "expiryTime": 0,\n        "limitHwid": 2,\n        "enable": true,\n        "subId": "..."\n      },\n      "inboundIds": [7, 9],\n      "traffic": {\n        "up": 1048576,\n        "down": 2097152,\n        "resetCount": 0,\n        "lastOnline": 1735680000000\n      }\n    }\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/import',
        summary:
          'Import clients from a JSON body { "data": "<json>" }, where data is a string-encoded array produced by /export ([{client, inboundIds, traffic}]). Items with inboundIds are created and attached to those inbounds; items with an empty inboundIds list are restored as unattached client records. An optional traffic object restores the usage counters, only for clients this import creates. Existing emails are never overwritten — they are returned in skipped, and their live counters are left untouched. Triggers a single Xray restart at the end if any target inbound was running; a failure while restoring counters still reports success=false after the clients were created.',
        body: '{\n  "data": "[{\\"client\\":{\\"email\\":\\"alice@example.com\\",\\"enable\\":true},\\"inboundIds\\":[7]}]"\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "created": 2,\n    "skipped": [\n      { "email": "alice@example.com", "reason": "email already in use: alice@example.com" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkAdjust',
        summary:
          'Shift expiry and/or traffic quota for many clients in one call. addDays/addBytes may be negative. Clients with unlimited expiry (expiryTime=0) or unlimited traffic (totalGB=0) are skipped for the corresponding field — bulk extend never converts unlimited to limited. A client that was auto-disabled solely because it was depleted (expired or over quota) is automatically re-enabled — locally and on its node — when the adjustment lifts it out of depletion; a manually-disabled or still-depleted client is left disabled. The optional flow directive sets the XTLS flow on every client: "none" clears it, "xtls-rprx-vision"/"xtls-rprx-vision-udp443" set it where the inbound supports it (omit or "" to leave it unchanged). The optional limitHwid sets maximum registered devices (0 = unlimited); the optional speedLimit, limitIp and userLimit set that limit to the same value on every client. The optional adTag sets MTProto Telegram sponsor channel ("none" clears). Returns the adjusted count and per-email skip reasons.',
        body: '{\n  "emails": ["alice", "bob"],\n  "addDays": 30,\n  "addBytes": 53687091200,\n  "flow": "xtls-rprx-vision",\n  "limitHwid": 2,\n  "adTag": "0123456789abcdef0123456789abcdef"\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "adjusted": 2,\n    "skipped": [\n      { "email": "carol", "reason": "unlimited expiry" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkEnable',
        summary:
          'Enable many clients in one call. Emails are grouped by inbound and applied with a single read-modify-write per inbound; the running Xray (local or remote node) is updated to add each user. Note that enabling a client whose quota is exhausted or whose expiry has passed only flips the flag — the traffic loop will disable it again on the next tick. Returns the changed count and per-email skip reasons.',
        body: '{\n  "emails": ["alice", "bob"]\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "changed": 2,\n    "skipped": [\n      { "email": "carol", "reason": "client not found" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkDisable',
        summary:
          'Disable many clients in one call. Emails are grouped by inbound and applied with a single read-modify-write per inbound; the running Xray (local or remote node) is updated to remove each user. Returns the changed count and per-email skip reasons.',
        body: '{\n  "emails": ["alice", "bob"]\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "changed": 2,\n    "skipped": [\n      { "email": "carol", "reason": "client not found" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkDel',
        summary:
          'Delete many clients in one call. The server processes the list sequentially so each delete sees the committed state of the previous one — avoids the race the per-email fan-out had on the panel side. Pass keepTraffic=true to retain the xray_client_traffic rows after deletion.',
        body: '{\n  "emails": ["alice", "bob"],\n  "keepTraffic": false\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "deleted": 2,\n    "skipped": [\n      { "email": "carol", "reason": "client not found" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkCreate',
        summary:
          'Create many clients in one call. Body is a JSON array of {client, inboundIds} payloads — the same shape /add accepts. Items are processed sequentially; per-email skip reasons are returned for items that fail (e.g., duplicate email). Triggers a single Xray restart at the end if any inbound was running.',
        body: '[\n  {\n    "client": {\n      "email": "alice@example.com",\n      "totalGB": 53687091200,\n      "expiryTime": 0,\n      "limitHwid": 2,\n      "enable": true\n    },\n    "inboundIds": [7]\n  },\n  {\n    "client": {\n      "email": "bob@example.com",\n      "totalGB": 53687091200,\n      "expiryTime": 0,\n      "limitHwid": 0,\n      "enable": true\n    },\n    "inboundIds": [7, 9]\n  }\n]',
        response:
          '{\n  "success": true,\n  "obj": {\n    "created": 2,\n    "skipped": [\n      { "email": "alice@example.com", "reason": "email already in use" }\n    ]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/groups/bulkAdd',
        summary:
          "Add many clients to a group in one call. Updates clients.group_name and patches the matching client entry inside every owning inbound's settings JSON in a single transaction. If the group name does not yet exist (in client_groups or as a derived label), it is auto-created as a persistent group. To clear the group label, use /groups/bulkRemove instead.",
        body: '{\n  "emails": ["alice", "bob"],\n  "group": "customer-a"\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "affected": 2\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/groups/bulkRemove',
        summary:
          "Clear the group label on many clients in one call. Inverse of /groups/bulkAdd. Clients themselves are kept — only the group label is cleared from clients.group_name and from each owning inbound's settings JSON. Groups become empty if all their members are removed.",
        body: '{\n  "emails": ["alice", "bob"]\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "affected": 2\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkAttach',
        summary:
          'Attach many existing clients to many inbounds in one call. Each client keeps its identity (email/UUID/password/subId) and a shared traffic row; all clients are added to a target inbound in a single AddInboundClient call. Clients already present on a target are reported under skipped. Returns per-email attached/skipped/errors lists and triggers a single Xray restart if any target inbound was running.',
        params: [
          {
            name: 'emails',
            in: 'body (json)',
            type: 'string[]',
            desc: 'Emails of existing clients to attach.',
          },
          {
            name: 'inboundIds',
            in: 'body (json)',
            type: 'integer[]',
            desc: 'Target inbound IDs to attach every client to.',
          },
        ],
        body: '{\n  "emails": ["alice", "bob"],\n  "inboundIds": [7, 9]\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "attached": ["alice", "bob"],\n    "skipped": ["bob"],\n    "errors": []\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkDetach',
        summary:
          "Mirror of bulkAttach: detach many existing clients from many inbounds in one call. For each email, intersects the client's current inbounds with the requested set and detaches from those only; (email, inbound) pairs where the client is not currently attached are silently no-ops. Emails not attached to any of the requested inbounds are reported under skipped. Client records are kept even if they become orphaned — use bulkDel for full removal. Returns per-email detached/skipped/errors lists and triggers a single Xray restart if any target inbound was running.",
        params: [
          {
            name: 'emails',
            in: 'body (json)',
            type: 'string[]',
            desc: 'Emails of existing clients to detach.',
          },
          {
            name: 'inboundIds',
            in: 'body (json)',
            type: 'integer[]',
            desc: 'Inbound IDs to detach the clients from.',
          },
        ],
        body: '{\n  "emails": ["alice", "bob"],\n  "inboundIds": [7, 9]\n}',
        response:
          '{\n  "success": true,\n  "obj": {\n    "detached": ["alice", "bob"],\n    "skipped": [],\n    "errors": []\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/bulkResetTraffic',
        summary:
          'Zero up/down counters for many clients in one call. Loops the single-reset path so each client its quota had disabled is re-enabled across its attached inbounds and pushed to Xray/remote nodes; a client disabled by hand stays disabled. Returns the count of successfully reset clients.',
        body: '{\n  "emails": ["alice", "bob"]\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "affected": 2\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/groups',
        summary:
          'List all client groups with their member counts. Merges persisted groups (rows in client_groups, including empty placeholders) with the distinct group_name values currently set on clients. Sorted alphabetically (case-insensitive).',
        response:
          '{\n  "success": true,\n  "obj": [\n    { "name": "customer-a", "clientCount": 5 },\n    { "name": "internal", "clientCount": 0 }\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/groups/:name/emails',
        summary:
          'Return just the email list of clients that currently belong to the given group. Useful for fanning a single bulk action over an entire group without round-tripping the full client list.',
        params: [{ name: 'name', in: 'path', type: 'string', desc: 'Group name (URL-encoded).' }],
        response: '{\n  "success": true,\n  "obj": ["alice", "bob", "carol"]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/groups/create',
        summary:
          'Create a new empty (placeholder) group. The group becomes selectable in client forms and the filter drawer even before any client is added to it. Errors if a group with the same name already exists.',
        body: '{\n  "name": "customer-a"\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "name": "customer-a"\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/groups/rename',
        summary:
          "Rename a group. The new name is applied to the client_groups row AND propagated to every matching client (both clients.group_name and the client entry inside every owning inbound's settings JSON) in a single transaction. Returns the number of clients whose label was updated.",
        body: '{\n  "oldName": "customer-a",\n  "newName": "tier-1"\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "affected": 5\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/groups/delete',
        summary:
          'Remove a group. Deletes the client_groups row and clears the group label from every matching client (both clients.group_name and the inbound settings JSON). The clients themselves are NOT deleted — use /bulkDel after filtering by group for that. Returns the count of clients whose label was cleared.',
        body: '{\n  "name": "customer-a"\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "affected": 5\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/groups/resetTraffic',
        summary:
          "Reset only the group-level traffic counter shown on the groups page. Snapshots the current up/down sum of the group's members as a baseline so the group total reads zero, while leaving each client's own counters (and their quotas) untouched. No Xray restart is triggered. Creates the client_groups row if the group exists only as a derived label.",
        body: '{\n  "name": "customer-a"\n}',
        response: '{\n  "success": true,\n  "obj": {\n    "name": "customer-a"\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/resetTraffic/:email',
        summary:
          'Zero out a single client’s up/down counters. Re-enables a client its quota had disabled across every attached inbound and pushes the change to Xray (or the remote node) so depleted users can connect again immediately; a client disabled by hand stays disabled.',
        params: [{ name: 'email', in: 'path', type: 'string', desc: 'Client email.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/clients/updateTraffic/:email',
        summary:
          'Manually adjust a client’s upload + download counters. Useful for migrations from external accounting systems.',
        params: [{ name: 'email', in: 'path', type: 'string', desc: 'Client email.' }],
        body: '{\n  "upload": 1073741824,\n  "download": 5368709120\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/ips/:email',
        summary:
          'List source IPs that have connected with the given client’s credentials. Returns an array of "ip (timestamp)" strings.',
        params: [{ name: 'email', in: 'path', type: 'string', desc: 'Client email.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/clients/clearIps/:email',
        summary: 'Reset the recorded IP list for a client.',
        params: [{ name: 'email', in: 'path', type: 'string', desc: 'Client email.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/clients/hwids/:email',
        summary:
          'List registered HWID devices for a client with a short fingerprint. Full hashes are not exposed.',
        params: [{ name: 'email', in: 'path', type: 'string', desc: 'Client email.' }],
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "id": 1,\n      "firstSeen": 1735000000000,\n      "lastSeen": 1735100000000,\n      "userAgent": "Happ/1.0",\n      "deviceOs": "android",\n      "osVersion": "15",\n      "deviceModel": "Pixel 9",\n      "fingerprint": "6ad17c93e821"\n    }\n  ]\n}',
      },
      {
        method: 'DELETE',
        path: '/panel/api/clients/hwids/:email',
        summary:
          'Clear all registered HWID devices for a client so new devices can register again.',
        params: [{ name: 'email', in: 'path', type: 'string', desc: 'Client email.' }],
      },
      {
        method: 'DELETE',
        path: '/panel/api/clients/hwids/:email/:id',
        summary:
          'Remove a single registered HWID device by its id, freeing one slot under the HWID limit.',
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email.' },
          { name: 'id', in: 'path', type: 'number', desc: 'Device id, from the list endpoint.' },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/clients/onlines',
        summary:
          'List the emails of currently connected clients (last seen within the heartbeat window), deduped across every node.',
        response: '{\n  "success": true,\n  "obj": ["user1", "user2"]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/onlinesByGuid',
        summary:
          'Online client emails grouped by the panelGuid of the node that physically hosts each client. The local panel uses its own GUID; each node (at any depth in a chain) uses its GUID. Lets the inbounds page attribute online status to the real node instead of the intermediate one it syncs through.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "a1b2-...": ["user1"],\n    "c3d4-...": ["user1", "user2"]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/clientIpsByGuid',
        summary:
          'Per-client source IPs grouped by the panelGuid of the node that observed them. Lets the central panel attribute and enforce per-client IP limits using the real visitor IPs each node sees, instead of the address of the intermediate panel it syncs through.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "a1b2-...": {\n      "user1": [\n        { "ip": "1.2.3.4", "timestamp": 1700000000 }\n      ]\n    }\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/activeInbounds',
        summary:
          "Inbound tags that carried traffic within the heartbeat window, grouped by the hosting node's panelGuid. Pairs with onlinesByGuid so the inbounds page only marks a multi-inbound client online on the inbounds it actually used. Nodes that do not report per-inbound activity are absent.",
        response:
          '{\n  "success": true,\n  "obj": {\n    "a1b2-...": ["in-443-tcp", "in-8443-tcp"]\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/lastOnline',
        summary: 'Map of client email → last-seen unix timestamp.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "user1": 1700000000,\n    "user2": 1699999000\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/traffic/:email',
        summary: 'Traffic counters for a client identified by email.',
        params: [
          {
            name: 'email',
            in: 'path',
            type: 'string',
            desc: 'Client email (unique across the panel).',
          },
        ],
        responseSchema: 'ClientTraffic',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/usage/:email',
        summary:
          "A client's traffic over a window, hour by hour (day by day past a month), and how much went through each server. Server 0 is this panel.",
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email.' },
          {
            name: 'window',
            in: 'query',
            type: 'string',
            desc: '1h, 24h (default), 7d, 30d or month.',
          },
          {
            name: 'month',
            in: 'query',
            type: 'string',
            desc: 'With window=month: YYYY-MM, in the calendar below.',
          },
          {
            name: 'calendar',
            in: 'query',
            type: 'string',
            desc: 'gregorian (default) or persian, in the panel time zone.',
          },
        ],
        responseSchema: 'ClientUsageView',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/subLinks/:subId',
        summary:
          'Return every protocol URL (vless://, vmess://, trojan://, ss://, hysteria://, hy2://) for clients matching the subscription ID. Same result set as the configured subPath endpoint, but as a JSON array — no base64. When an inbound has streamSettings.externalProxy set, one URL is emitted per external proxy. Empty array when the subId has no enabled clients.',
        params: [
          {
            name: 'subId',
            in: 'path',
            type: 'string',
            desc: "Subscription ID, taken from the client's subId field.",
          },
        ],
        response:
          '{\n  "success": true,\n  "obj": [\n    "vless://uuid@host:443?security=reality&...#user1",\n    "vmess://eyJ2IjoyLC..."\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/subEntries/:subId',
        summary:
          "The same links as subLinks, in the same order, each with the inbound it comes from (inboundId, protocol) and that inbound's app location (locationId, location, countryCode, city, sortOrder). locationId is 0 for an inbound in no location; inboundId is 0 for a client external link. Lets a bot hand out one file per location and name it after the location.",
        params: [
          {
            name: 'subId',
            in: 'path',
            type: 'string',
            desc: "Subscription ID, taken from the client's subId field.",
          },
        ],
        responseSchema: 'SubEntry',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/clients/happLink/:id',
        summary:
          'Generate a fresh Happ crypt5 link locally from the current client subscription URL when Happ link generation is enabled. The panel applies a resource limit of 8192 UTF-8 bytes to the source URL; this is not a Happ client maximum. Longer sources return success: false with msg: happ_source_too_long and obj: null. The source URL is not sent to a generation provider, and the result is not stored or reused.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Stable client record ID.' }],
        responseSchema: 'HappLinkResult',
      },
      {
        method: 'GET',
        path: '/panel/api/clients/links/:email',
        summary:
          'Return every URL for one client across all attached inbounds, one per advertised endpoint: the managed hosts of the inbound, else its streamSettings.externalProxy entries, else its own address. Supported protocols: vmess, vless, trojan, shadowsocks, hysteria, mtproto. Protocols without a URL form (socks, http, mixed, wireguard, dokodemo, tunnel) contribute nothing.',
        params: [
          { name: 'email', in: 'path', type: 'string', desc: 'Client email (unique identifier).' },
        ],
        response:
          '{\n  "success": true,\n  "obj": [\n    "vless://uuid@host:443?...#user1"\n  ]\n}',
      },
    ],
  },

  {
    id: 'nodes',
    title: 'Nodes',
    description:
      'Manage remote Manifold panels acting as nodes for a central panel. All endpoints under /panel/api/nodes.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/nodes/list',
        summary:
          'List every configured node with its connection details, health, and last heartbeat patch.',
        responseSchema: 'NodeView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/mtls/ca',
        summary:
          "This panel's node-auth CA certificate (public, PEM) to paste into a node's mTLS trust setting. Lazily mints the CA and the master client cert on first call. Pair with setting tlsVerifyMode=mtls on the node.",
        response:
          '{\n  "success": true,\n  "obj": {\n    "caCert": "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n"\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/mtls/trustCA',
        summary:
          "Set the CA certificate this panel trusts for incoming node-API client certificates (this panel acting as a node). Paste the managing panel's CA (from nodes/mtls/ca). An empty caCert disables it. A non-empty value must be a PEM certificate. Applied on the next panel restart.",
        body: '{\n  "caCert": "-----BEGIN CERTIFICATE-----\\n...\\n-----END CERTIFICATE-----\\n"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/mtls/reloadClient',
        summary:
          'Validate the stored master mTLS client credential and invalidate cached transports. Each transport closes its old idle pool and rebuilds with the rotated certificate before its next request.',
      },
      {
        method: 'GET',
        path: '/panel/api/nodes/get/:id',
        summary: 'Fetch a single node by ID.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Node ID.' }],
        responseSchema: 'NodeView',
      },
      {
        method: 'GET',
        path: '/panel/api/nodes/webCert/:id',
        summary:
          'Fetch a node\'s own web TLS certificate/key file paths (proxied to the node). Used by the inbound form\'s "Set Cert from Panel" so a node-assigned inbound gets paths that exist on the node, not the central panel.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Node ID.' }],
        response:
          '{\n  "success": true,\n  "obj": {\n    "webCertFile": "/root/cert/example.com/fullchain.pem",\n    "webKeyFile": "/root/cert/example.com/privkey.pem"\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/add',
        summary:
          'Register a new remote node. Provide its URL, write-only apiToken, and optional remark / allowPrivateAddress flag. Responses expose hasApiToken only.',
        body: '{\n  "name": "de-fra-1",\n  "remark": "",\n  "scheme": "https",\n  "address": "node1.example.com",\n  "port": 2053,\n  "basePath": "/",\n  "apiToken": "abcdef...",\n  "clearApiToken": false,\n  "enable": true,\n  "allowPrivateAddress": false\n}',
        responseSchema: 'NodeView',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/update/:id',
        summary:
          'Replace a node\u2019s connection details. apiToken is write-only: omit it or send an empty string to keep the stored token; set clearApiToken=true to clear it.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Node ID.' }],
        body: '{\n  "name": "de-fra-1",\n  "remark": "",\n  "scheme": "https",\n  "address": "node1.example.com",\n  "port": 2053,\n  "basePath": "/",\n  "apiToken": "",\n  "clearApiToken": false,\n  "enable": true,\n  "allowPrivateAddress": false\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/del/:id',
        summary: 'Delete a node. Inbounds bound to it are not auto-migrated.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Node ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/setEnable/:id',
        summary: 'Pause or resume traffic sync with this node.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Node ID.' }],
        body: '{\n  "enable": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/test',
        summary:
          'Probe a node without saving it. Uses the body as connection details and returns the same heartbeat snapshot a registered node would have.',
        body: '{\n  "scheme": "https",\n  "address": "node1.example.com",\n  "port": 2053,\n  "basePath": "/",\n  "apiToken": "abcdef..."\n}',
        responseSchema: 'ProbeResultUI',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/certFingerprint',
        summary:
          "Connect to the node over HTTPS without verifying its certificate and return the leaf certificate's SHA-256 (base64). Used by the Add/Edit Node dialog to fetch and pin a self-signed certificate. Uses the same body as /test.",
        body: '{\n  "scheme": "https",\n  "address": "node1.example.com",\n  "port": 2053,\n  "basePath": "/"\n}',
        response: '{\n  "success": true,\n  "obj": "k3b1...base64-sha256...="\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/inbounds',
        summary:
          'Use unsaved node connection details to list the remote inbounds available for selective import.',
        body: '{\n  "name": "de-fra-1",\n  "scheme": "https",\n  "address": "node1.example.com",\n  "port": 2053,\n  "basePath": "/",\n  "apiToken": "abcdef..."\n}',
        response:
          '{\n  "success": true,\n  "obj": [\n    { "tag": "inbound-443", "remark": "VLESS", "protocol": "vless", "port": 443 }\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/probe/:id',
        summary: 'Probe an existing node, updating its cached health state.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Node ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/updatePanel',
        summary:
          'Ask each given node to update its panel. Nodes refuse, because a panel never updates itself; each node is reported with that error. Update nodes with the installer.',
        body: '{\n  "ids": [1, 2, 3],\n  "dev": false\n}',
        response:
          '{\n  "success": true,\n  "obj": [\n    { "id": 1, "name": "de-1", "ok": true },\n    { "id": 2, "name": "fr-1", "ok": false, "error": "node is offline" }\n  ]\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/nodes/history/:id/:metric/:bucket',
        summary:
          'Aggregated metric history for a node — same shape as /server/history, scoped to one node.',
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Node ID.' },
          { name: 'metric', in: 'path', type: 'string', desc: 'cpu | mem.' },
          {
            name: 'bucket',
            in: 'path',
            type: 'number',
            desc: 'Bucket size in seconds. Allowed: 2, 30, 60, 120, 180, 300.',
          },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/nodes/stats/:id',
        summary:
          "A node's health over a window from its five-minute samples (kept 90 days): availability stretches (up, down, or unwatched while the panel recorded nothing), uptime counted over watched time only, load, and each user's traffic on it.",
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Node ID.' },
          {
            name: 'window',
            in: 'query',
            type: 'string',
            desc: '1h, 24h (default), 7d, 30d or month.',
          },
          { name: 'month', in: 'query', type: 'string', desc: 'With window=month: YYYY-MM.' },
          {
            name: 'calendar',
            in: 'query',
            type: 'string',
            desc: 'gregorian (default) or persian.',
          },
        ],
        responseSchema: 'NodeStatsView',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/logs/:id',
        summary:
          "The last lines of a node panel's own log, read through the node API (the node-sync scope may read it). Poll it for a live view.",
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Node ID.' },
          { name: 'count', in: 'body', type: 'number', desc: 'Lines, 10 to 2000.' },
          {
            name: 'level',
            in: 'body',
            type: 'string',
            desc: 'debug, info, notice, warning or error: this level and above.',
          },
        ],
        body: '{\n  "count": 200,\n  "level": "info"\n}',
        response: '{\n  "success": true,\n  "obj": ["2026/11/17 10:00:00 INFO - node started"]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/ssh/hostKey',
        summary:
          "Read a server's SSH host key fingerprint. The handshake stops at the key, so no credential is sent; the admin compares it and passes it to the install.",
        params: [
          { name: 'host', in: 'body', type: 'string', desc: 'SSH host.' },
          {
            name: 'port',
            in: 'body',
            type: 'number',
            desc: 'SSH port, 22 when 0.',
            optional: true,
          },
        ],
        body: '{\n  "host": "203.0.113.10",\n  "port": 22\n}',
        response: '{\n  "success": true,\n  "obj": "SHA256:Zm9vYmFy"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/nodes/ssh/install',
        summary:
          'Install Manifold on a server over SSH and add it as a node, or update an existing node (update: true). A first install gets a self-signed certificate, pinned from its own output over the pinned SSH channel, and a node-admin token. SSH credentials are used for this job only and never stored. Returns the job id.',
        body: '{\n  "name": "de-fra-1",\n  "address": "203.0.113.10",\n  "panelPort": 2053,\n  "sshPort": 22,\n  "sshUser": "root",\n  "password": "…",\n  "hostKey": "SHA256:Zm9vYmFy"\n}',
        response: '{\n  "success": true,\n  "obj": "9f2c1a5b7d3e4f60"\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/nodes/ssh/job/:id',
        summary:
          "An install job's state (running, done or failed), its log so far and the node it added.",
        params: [{ name: 'id', in: 'path', type: 'string', desc: 'Job id from the install call.' }],
        responseSchema: 'NodeInstallJob',
      },
      {
        method: 'POST',
        path: '/panel/api/seats/sync',
        summary:
          "A master's plan for this node's user-limit seats (node-sync tokens may call it): the seats each account holds on the master's other servers, counted against its limit here for 30 s, and the seats to cut because an account got in on two servers at once. Returns the seats this server holds after it, oldest first.",
        body: '{\n  "elsewhere": { "alice": 1 },\n  "cut": ["l2tp/9#2/s4101"]\n}',
        response:
          '{\n  "success": true,\n  "obj": [\n    { "id": "openvpn/1#1/7", "account": "bob", "protocol": "openvpn", "inbound": 1, "address": "10.8.0.6", "since": "2026-11-19T18:00:00Z", "limit": 2 }\n  ]\n}',
      },
    ],
  },

  {
    id: 'addons',
    title: 'Addons',
    description:
      "An addon is a program next to the panel, such as a sales bot, installed from its manifest (addon.json: id, name, version, permissions, events, webhook, panelUrl). Installing gives it an API token that acts as the installing account, narrowed to the permissions it asks for and never above that account, and a signed webhook for the events it names; both are shown once and go into the addon's own settings. Installing the same id again updates the permissions, events and webhook in place. Disabling turns the token and webhook off together; uninstalling removes them. All endpoints under /panel/api/addons.",
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/addons/list',
        summary: 'Installed addons with their manifests.',
        responseSchema: 'AddonView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/addons/install',
        summary: 'Install or update an addon; the body is its manifest.',
        body: '{\n  "id": "mirza-bot",\n  "name": "Mirza Bot",\n  "version": "1.0.0",\n  "permissions": ["users:read", "users:write"],\n  "events": ["user.limited", "user.expired"],\n  "webhook": "https://bot.example.com/manifold"\n}',
        responseSchema: 'AddonInstalled',
      },
      {
        method: 'POST',
        path: '/panel/api/addons/enable/:id',
        summary: "Turn an addon's token and webhook back on.",
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Addon ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/addons/disable/:id',
        summary: "Turn an addon's token and webhook off; it can neither act nor hear events.",
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Addon ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/addons/uninstall/:id',
        summary: 'Remove an addon with its token and webhook.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Addon ID.' }],
      },
    ],
  },

  {
    id: 'import',
    title: 'Import from another panel',
    description:
      "Moves users in from Marzban, PasarGuard, Marzneshin, 3x-ui/x-ui and s-ui (their SQLite database), Hiddify (its JSON backup), or Marzban, PasarGuard and Remnawave over their API. A read returns a preview held for 30 minutes; apply creates the users on the inbounds picked for each protocol, keeping each user's UUID or password, quota, usage, expiry and reset cycle. With keepLinks the old panel's subscription links keep working on this panel's subscription server: kept ids (3x-ui, s-ui, Hiddify, Remnawave), Marzneshin's /sub/<user>/<key>, and Marzban/PasarGuard tokens checked against the old panel's secret. Users whose name is already taken are skipped. All endpoints under /panel/api/import.",
    endpoints: [
      {
        method: 'POST',
        path: '/panel/api/import/file',
        summary:
          'Read an uploaded database or backup (multipart field file, up to 512 MiB); the kind is recognised from its tables.',
        responseSchema: 'ImportPreview',
      },
      {
        method: 'POST',
        path: '/panel/api/import/remote',
        summary:
          "Read a panel over its API: marzban or pasarguard (admin username and password, or a token; secret is the old panel's jwt secret_key for old links) or remnawave (an API token).",
        body: '{\n  "kind": "marzban",\n  "url": "https://old.example.com:8000",\n  "username": "admin",\n  "password": "…",\n  "secret": ""\n}',
        responseSchema: 'ImportPreview',
      },
      {
        method: 'POST',
        path: '/panel/api/import/apply',
        summary:
          'Create the users of a preview. inbounds maps a protocol (vless, vmess, trojan, shadowsocks, hysteria2) to inbound ids; fallback takes users with none of them.',
        body: '{\n  "id": "f3a1c0d9e8b7a6c5",\n  "inbounds": { "vless": [1], "trojan": [2] },\n  "fallback": [],\n  "keepLinks": true,\n  "ownerId": 0\n}',
        responseSchema: 'ImportApplyResult',
      },
    ],
  },

  {
    id: 'certs',
    title: 'Certificates',
    description:
      "The certificate store: certificates issued by the panel over ACME (Let's Encrypt; HTTP-01, TLS-ALPN-01, or DNS-01 through Cloudflare or ArvanCloud for wildcards) or uploaded, kept as fullchain.pem and privkey.pem under /etc/manifold/certs/<name>/ for inbounds and the panel to read. Renewed 30 days before expiry; a renewal restarts Xray when an inbound reads the files, the panel picks up its own certificate without a restart, and each named node gets a copy. The DNS token, account key and private key are never returned. All endpoints under /panel/api/certs.",
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/certs/list',
        summary: 'Every certificate with its domains, expiry, last run and file paths.',
        responseSchema: 'CertificateView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/certs/save',
        summary:
          'Create (id 0) or change an ACME certificate. dnsToken left out keeps the stored one. Saving does not issue: call issue.',
        body: '{\n  "id": 0,\n  "name": "example.com",\n  "domains": ["example.com", "*.example.com"],\n  "challenge": "dns-01",\n  "dnsProvider": "cloudflare",\n  "dnsToken": "…",\n  "autoRenew": true,\n  "nodeIds": [2]\n}',
        responseSchema: 'CertificateView',
      },
      {
        method: 'POST',
        path: '/panel/api/certs/upload',
        summary:
          'Store a certificate made elsewhere (PEM chain and key, checked to be a pair); the name defaults to its first domain.',
        responseSchema: 'CertificateView',
      },
      {
        method: 'POST',
        path: '/panel/api/certs/issue/:id',
        summary: 'Start an ACME order now; the list shows issuing until it ends, then the outcome.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Certificate ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/certs/del/:id',
        summary:
          'Delete a certificate and its files here. Copies on nodes stay, since their inbounds may read them.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Certificate ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/certs/put',
        summary:
          'Store a copy a master sends to this node (node-sync tokens may call it). The node lists it as from the master and restarts Xray when an inbound reads it.',
        params: [
          { name: 'name', in: 'body', type: 'string', desc: 'Certificate name.' },
          { name: 'cert', in: 'body', type: 'string', desc: 'PEM chain.' },
          { name: 'key', in: 'body', type: 'string', desc: 'PEM private key.' },
        ],
      },
    ],
  },

  {
    id: 'webhooks',
    title: 'Webhooks',
    description:
      'Events sent out as they happen: to your own URL (JSON, signed), an ntfy topic, or a Bale chat. An HTTP delivery carries X-Manifold-Event, X-Manifold-Delivery (the event id, for de-duplication), X-Manifold-Timestamp and X-Manifold-Signature = sha256=HMAC-SHA256(secret, "<timestamp>.<body>"). A delivery that does not get a 2xx is retried after 30s, 2m, 10m, 30m, 1h, 2h, 4h and 8h, then given up; the log keeps a week. A reseller sees and makes only its own webhooks, hears only about its own users, and may only reach public addresses. All endpoints under /panel/api/webhooks.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/webhooks/list',
        summary: 'Every webhook you may see, without its secret.',
        responseSchema: 'WebhookView',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/webhooks/events',
        summary:
          'The event types a webhook may subscribe to. An empty list on a webhook means all.',
      },
      {
        method: 'POST',
        path: '/panel/api/webhooks/save',
        summary:
          'Create (id 0) or replace a webhook. A new HTTP webhook without a secret gets one, shown in this answer only. For Bale the secret is the bot token.',
        body: '{\n  "id": 0,\n  "name": "mirza",\n  "enable": true,\n  "kind": "http",\n  "url": "https://bot.example.com/manifold",\n  "events": ["user.limited", "user.expired"]\n}',
        responseSchema: 'WebhookView',
      },
      {
        method: 'POST',
        path: '/panel/api/webhooks/del/:id',
        summary: 'Delete a webhook and its delivery log.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Webhook ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/webhooks/test/:id',
        summary: 'Queue a webhook.test event for this webhook alone.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Webhook ID.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/webhooks/deliveries/:id',
        summary:
          "A webhook's last 200 deliveries, newest first, with the payload sent and the answer.",
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Webhook ID.' }],
        responseSchema: 'WebhookDelivery',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/webhooks/replay/:id',
        summary: 'Send a logged delivery again, from its first attempt.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Delivery ID.' }],
      },
    ],
  },

  {
    id: 'backups',
    title: 'Scheduled backups',
    description:
      'Targets that receive a full backup on a schedule: a directory of this server, an S3-compatible bucket (AWS, Cloudflare R2, ArvanCloud, MinIO; path-style, Signature V4) or another server over SSH. With a passphrase the backup is sealed with AES-256-GCM under a PBKDF2 key; restore asks for it. Only the newest backups the target keeps are left; other files there are never touched. Secrets (passphrase, secret key, password, private key) are write-only. All endpoints under /panel/api/backups.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/backups/targets',
        summary: 'Every backup target with its last run, and which secrets are set.',
        responseSchema: 'BackupTargetView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/backups/targets/save',
        summary:
          'Create (id 0) or replace a target. A secret left out keeps the stored one; an empty string clears it.',
        body: '{\n  "id": 0,\n  "name": "offsite",\n  "enable": true,\n  "kind": "s3",\n  "everyHours": 24,\n  "keep": 7,\n  "endpoint": "https://s3.example.com",\n  "region": "us-east-1",\n  "bucket": "manifold",\n  "prefix": "fra-1",\n  "accessKey": "AKIA…",\n  "secretKey": "…",\n  "passphrase": "…"\n}',
        responseSchema: 'BackupTargetView',
      },
      {
        method: 'POST',
        path: '/panel/api/backups/targets/del/:id',
        summary: 'Delete a target. Backups already stored there stay.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Target ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/backups/targets/run/:id',
        summary: 'Back up to the target now, then prune; answers with the outcome recorded on it.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Target ID.' }],
        responseSchema: 'BackupTargetView',
      },
      {
        method: 'GET',
        path: '/panel/api/backups/targets/files/:id',
        summary: 'The files a target holds, newest first.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Target ID.' }],
        responseSchema: 'File',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/backups/sshHostKey',
        summary:
          "Read an SSH target's host key fingerprint without logging in, to accept before saving.",
        params: [
          { name: 'host', in: 'body', type: 'string', desc: 'SSH host.' },
          {
            name: 'port',
            in: 'body',
            type: 'number',
            desc: 'SSH port, 22 when 0.',
            optional: true,
          },
        ],
        body: '{\n  "host": "backup.example.com",\n  "port": 22\n}',
        response: '{\n  "success": true,\n  "obj": "SHA256:Zm9vYmFy"\n}',
      },
    ],
  },

  {
    id: 'ingress',
    title: 'Shared ports',
    description:
      'One TCP port, such as 443, shared by many services: each connection goes to the target its TLS server name (SNI) or HTTP Host names, untouched, so REALITY and TLS inbounds, the panel and the subscription server can all answer on one port. Routes are tried in order; a route may also require an ALPN. A target may receive a PROXY protocol header so it sees the real client address. Runs on this server only. All endpoints under /panel/api/ingress.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/ingress/list',
        summary: 'Every shared port with its routes, and why it is not serving when it is not.',
        responseSchema: 'IngressStatus',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/ingress/save',
        summary:
          'Create (id 0) or replace a shared port and bind it at once. Refused when a local TCP inbound, the panel, the subscription server or another shared port holds the port.',
        body: '{\n  "id": 0,\n  "remark": "443",\n  "enable": true,\n  "listen": "",\n  "port": 443,\n  "routes": [\n    { "hosts": ["panel.example.com"], "target": "127.0.0.1:2053", "proxyProtocol": 0 },\n    { "hosts": ["www.speedtest.net"], "target": "127.0.0.1:8443", "proxyProtocol": 1 }\n  ],\n  "defaultTarget": "",\n  "defaultProxyProtocol": 0\n}',
        responseSchema: 'IngressListener',
      },
      {
        method: 'POST',
        path: '/panel/api/ingress/del/:id',
        summary: 'Delete a shared port and free it.',
        params: [{ name: 'id', in: 'path', type: 'number', desc: 'Shared port ID.' }],
      },
    ],
  },

  {
    id: 'tunnels',
    title: 'Tunnels',
    description:
      'Tunnels between this server and another: kernel GRE, GRETAP, IP6GRE, IP6GRETAP, IPIP and SIT links, and BackPack reverse and layer-3 tunnels. The client end runs on the panel that serves the accounts, the server end on the relay users connect to, which forwards its ports to the client end. A relay is another Manifold, usually the tunnel edition, reached with an API token of the tunnel scope. All endpoints under /panel/api/tunnels.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/tunnels/list',
        summary: 'List every tunnel end of this server with its live status.',
        responseSchema: 'TunnelView',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/status',
        summary:
          'The live status of every tunnel end: up, degraded, down, connecting, disabled or error, with latency, loss and bytes.',
        responseSchema: 'TunnelStatus',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/suggest',
        summary:
          "A tunnel subnet no tunnel of this server uses, a random port, a new secret and this server's public address, for a new tunnel.",
        responseSchema: 'TunnelSuggestion',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/add',
        summary:
          'Create a tunnel end and start it. With a relayId the other end is created on that relay too.',
        body: '{\n  "name": "uk-relay",\n  "kind": "gre",\n  "role": "client",\n  "enable": true,\n  "relayId": 0,\n  "settings": {\n    "remoteAddr": "198.51.100.7",\n    "localIp": "10.80.0.2/30",\n    "peerIp": "10.80.0.1",\n    "forwards": [{ "listen": "443", "proto": "tcp" }]\n  }\n}',
        responseSchema: 'TunnelView',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/update/:id',
        summary:
          'Change a tunnel end. Its pair id stays, so the other end keeps matching it; a relay is updated too.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Tunnel ID.' }],
        body: '{\n  "name": "uk-relay",\n  "kind": "gre",\n  "role": "client",\n  "enable": true,\n  "relayId": 0,\n  "settings": {\n    "remoteAddr": "198.51.100.7",\n    "localIp": "10.80.0.2/30",\n    "peerIp": "10.80.0.1",\n    "forwards": [{ "listen": "443", "proto": "tcp" }]\n  }\n}',
        responseSchema: 'TunnelView',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/del/:id',
        summary: 'Delete a tunnel end, and its other end on the relay when it has one.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Tunnel ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/pairing/:id',
        summary:
          "The pairing code that makes another panel this tunnel's other end, with the sides swapped.",
        params: [
          { name: 'id', in: 'path', type: 'integer', desc: 'Tunnel ID.' },
          {
            name: 'address',
            in: 'body (form)',
            type: 'string',
            desc: "How the other server reaches this one. Empty uses the tunnel's public address, or the detected one.",
            optional: true,
          },
        ],
        responseSchema: 'TunnelPairing',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/import',
        summary:
          'Create or replace the tunnel end a pairing code describes. An end with the same pair id is replaced, which is how the owning panel updates its relay.',
        params: [
          {
            name: 'code',
            in: 'body (form)',
            type: 'string',
            desc: 'A code starting with manifold-tunnel://.',
            pattern: '^manifold-tunnel://.+',
          },
        ],
        responseSchema: 'TunnelView',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/withdraw',
        summary: 'Delete the tunnel end with this pair id, for the panel that owns the tunnel.',
        params: [
          { name: 'pairId', in: 'body (form)', type: 'string', desc: 'Pair ID of the tunnel.' },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/resync',
        summary:
          'Send every relayed tunnel to its relay again, as after this panel moved to another server. Returns the tunnels that failed.',
        response: '{\n  "success": true,\n  "obj": []\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/relays',
        summary: 'List the relays this panel manages.',
        responseSchema: 'TunnelRelay',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/relays/add',
        summary: 'Add a relay: its panel URL and an API token made on it with the tunnel scope.',
        params: [
          { name: 'name', in: 'body (form)', type: 'string', desc: 'A name for the relay.' },
          {
            name: 'url',
            in: 'body (form)',
            type: 'string',
            desc: "The relay panel's URL, with its base path.",
          },
          {
            name: 'token',
            in: 'body (form)',
            type: 'string',
            desc: 'API token of the tunnel scope.',
          },
        ],
        responseSchema: 'TunnelRelay',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/relays/update/:id',
        summary: 'Change a relay; an empty token keeps the stored one.',
        params: [
          { name: 'id', in: 'path', type: 'integer', desc: 'Relay ID.' },
          { name: 'name', in: 'body (form)', type: 'string', desc: 'A name for the relay.' },
          {
            name: 'url',
            in: 'body (form)',
            type: 'string',
            desc: "The relay panel's URL, with its base path.",
          },
          {
            name: 'token',
            in: 'body (form)',
            type: 'string',
            desc: 'API token of the tunnel scope.',
            optional: true,
          },
        ],
        responseSchema: 'TunnelRelay',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/relays/del/:id',
        summary: 'Forget a relay. Its tunnels stay on both servers and are no longer kept in step.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Relay ID.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/relays/:id/tunnels',
        summary: 'The tunnel ends on a relay, with their status, as that relay reports them.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Relay ID.' }],
        responseSchema: 'TunnelView',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/groups',
        summary:
          "This relay's tunnel groups: several relay ends to different origins sharing forwarded ports. failover sends everything to the first member that answers its probe, balance gives each new connection to the next member that answers; when none answers, all members are used.",
        responseSchema: 'TunnelGroupView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/groups/save',
        summary:
          "Create (id 0) or replace a group of 2 to 16 tunnels with a tunnel address (kernel links or BackPack layer 3). Without relayId the members are this relay's ends and the ports are checked against tunnels, other groups, inbounds and the panel. With relayId the members are this panel's ends of that managed relay's tunnels, and the group is placed on the relay, which checks its ports and runs it.",
        body: '{\n  "id": 0,\n  "name": "origins",\n  "mode": "failover",\n  "relayId": 0,\n  "members": [1, 2],\n  "forwards": [{ "listen": "443", "proto": "tcp" }],\n  "enable": true\n}',
        responseSchema: 'TunnelGroupView',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/groups/del/:id',
        summary:
          'Delete a group; its ports stop being forwarded, here or on the relay it was placed on.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Group ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/groups/import',
        summary:
          "Store the group a managing panel places on this relay, its tunnels named by pair id and mapped to this relay's ends, replacing the group of the same pair id. Reachable with the tunnel token.",
        params: [
          {
            name: 'group',
            in: 'body (form)',
            type: 'string',
            desc: 'The group as JSON: pairId, name, mode, members and backups (pair ids), sticky, forwards, enable.',
          },
        ],
        responseSchema: 'TunnelGroupView',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/groups/withdraw',
        summary:
          'Delete the group a managing panel placed here under this pair id. Reachable with the tunnel token.',
        params: [
          { name: 'pairId', in: 'body (form)', type: 'string', desc: 'Pair ID of the group.' },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/bench/state',
        summary:
          "The tunnel test of this server: the one under way or its link waiting for the other server, the last one finished, every tunnel a test can try, and the test's profiles with the most data and about the time each takes.",
        responseSchema: 'TunnelBenchState',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/bench/link',
        summary:
          "Make a tunnel test link for the other server to import. It carries this panel's address and certificate pin, this server's addresses and side, the first of the 80 ports the test may use on both servers, and a credential that reaches only the test. One link or test at a time.",
        body: '{\n  "role": "server",\n  "url": "https://relay.example.com:2053/path/",\n  "addr4": "198.51.100.7",\n  "addr6": "",\n  "minutes": 30\n}',
        responseSchema: 'TunnelBenchLink',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/bench/start',
        summary:
          'Run a tunnel test from the link the other server made. Every chosen tunnel is screened over TCP and UDP, the best are measured in full and under load, and the best with an interface are tried at smaller MTUs; the test then names the best tunnel overall, for TCP inbounds and for UDP inbounds. It runs in the background; follow it with /bench/state.',
        body: '{\n  "code": "manifold-tunnel-test://...",\n  "profile": "standard",\n  "keys": []\n}',
        responseSchema: 'TunnelBenchReport',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/bench/cancel',
        summary:
          'Stop the test under way, on both servers, or withdraw the link waiting here. Everything the test started is taken down.',
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/bench/history',
        summary: 'The tunnel tests this server took part in, newest first, without their results.',
        responseSchema: 'TunnelBench',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/tunnels/bench/report/:id',
        summary: 'One finished test of the history, with every result and the picks.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'History ID.' }],
        responseSchema: 'TunnelBenchReport',
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/bench/del/:id',
        summary: 'Delete a test from the history.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'History ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/tunnels/bench/peer/:op',
        summary:
          "Called by the server that runs a test, with the link's credential as a bearer token and nothing else: hello, end (start or stop one candidate's end, which this server builds itself), measure (start or poll, on the relay), report and close. Errors come back undecorated; 410 means this server no longer holds the test.",
        params: [
          {
            name: 'op',
            in: 'path',
            type: 'string',
            desc: 'hello, end, measure, report or close.',
            pattern: '^(hello|end|measure|report|close)$',
          },
        ],
      },
    ],
  },

  {
    id: 'access',
    title: 'Access',
    description:
      'Admin accounts, custom roles, plans and the audit log. Accounts sit on a tier (main admin, operator, reseller); a reseller works only on the users it owns, inside its allowance. All endpoints under /panel/api/access.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/access/me',
        summary:
          'The signed-in account or token: its tier, permissions and, for a reseller, its allowance and how much of it is used.',
        responseSchema: 'MeView',
      },
      {
        method: 'GET',
        path: '/panel/api/access/me/twoFactor',
        summary: "A new secret and otpauth address for the account's authenticator app.",
        responseSchema: 'TwoFactorSetupView',
      },
      {
        method: 'POST',
        path: '/panel/api/access/me/twoFactor',
        summary:
          "Turn the account's own two-factor on with the new secret and a code from it, or off with a current code.",
        body: '{\n  "enable": true,\n  "secret": "JBSWY3DPEHPK3PXP",\n  "code": "123456"\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/access/admins',
        summary: 'List every admin account, the owner first, with how many users each owns.',
        responseSchema: 'AdminView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/access/admins/add',
        summary: 'Create an admin account. A reseller also takes its allowance.',
        body: '{\n  "username": "seller1",\n  "password": "a-long-password",\n  "tier": "reseller",\n  "volumeBytes": 1099511627776,\n  "resetCycle": "monthly",\n  "resetDay": 1,\n  "maxUsers": 100,\n  "mayEditUsers": true\n}',
        responseSchema: 'AdminView',
      },
      {
        method: 'POST',
        path: '/panel/api/access/admins/update/:id',
        summary:
          "Change an account. An empty password keeps the current one; a new tier, role or password ends the account's sessions.",
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Account ID.' }],
        body: '{\n  "username": "seller1",\n  "password": "",\n  "tier": "reseller",\n  "volumeBytes": 2199023255552\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/access/admins/del/:id',
        summary:
          'Delete an account. Its users pass to the panel and keep working; its API tokens are deleted.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Account ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/access/admins/endSessions/:id',
        summary: 'Sign an account out everywhere.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Account ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/access/admins/handover/:id',
        summary:
          "Make another main admin the panel's owner. Only the owner may, from its own login, not with a token.",
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Account ID of the new owner.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/access/roles',
        summary: 'List the custom roles. The three built-in roles are not stored.',
        responseSchema: 'Role',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/access/roles/add',
        summary: 'Create a custom role: a tier, the permissions it keeps and its limits.',
        body: '{\n  "name": "Support",\n  "tier": "operator",\n  "permissions": ["users:read"]\n}',
        responseSchema: 'Role',
      },
      {
        method: 'POST',
        path: '/panel/api/access/roles/update/:id',
        summary:
          "Change a custom role; every account on it signs in again with the role's new reach.",
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Role ID.' }],
        body: '{\n  "name": "Support",\n  "tier": "operator",\n  "permissions": ["users:read", "users:write"]\n}',
        responseSchema: 'Role',
      },
      {
        method: 'POST',
        path: '/panel/api/access/roles/del/:id',
        summary: 'Delete a custom role no account holds.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Role ID.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/access/plans',
        summary:
          'List the plans. A reseller that sells from plans only sees just the plans it may sell.',
        responseSchema: 'Plan',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/access/owners',
        summary: "Every account users can belong to, for the users page's owner column and filter.",
        responseSchema: 'OwnerView',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/access/plans/add',
        summary: 'Create a plan new users can be created from.',
        body: '{\n  "name": "30 days 50 GB",\n  "totalBytes": 53687091200,\n  "days": 30,\n  "startOnFirstUse": true,\n  "limitIp": 2,\n  "userLimit": 2,\n  "allInbounds": true\n}',
        responseSchema: 'Plan',
      },
      {
        method: 'POST',
        path: '/panel/api/access/plans/update/:id',
        summary: 'Change a plan. Users created from it keep the limits they were given.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Plan ID.' }],
        body: '{\n  "name": "30 days 50 GB",\n  "totalBytes": 53687091200,\n  "days": 30\n}',
        responseSchema: 'Plan',
      },
      {
        method: 'POST',
        path: '/panel/api/access/plans/del/:id',
        summary: 'Delete a plan. Users created from it keep every limit they were given.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Plan ID.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/access/audit',
        summary: 'One page of the audit log, newest first.',
        params: [
          {
            name: 'admin',
            in: 'query',
            type: 'string',
            desc: 'Only this username.',
            optional: true,
          },
          {
            name: 'entity',
            in: 'query',
            type: 'string',
            desc: 'Only this entity, such as users.',
            optional: true,
          },
          {
            name: 'action',
            in: 'query',
            type: 'string',
            desc: 'Only this action, such as delete.',
            optional: true,
          },
          {
            name: 'from',
            in: 'query',
            type: 'integer',
            desc: 'Unix ms, inclusive.',
            optional: true,
          },
          { name: 'to', in: 'query', type: 'integer', desc: 'Unix ms, inclusive.', optional: true },
          {
            name: 'page',
            in: 'query',
            type: 'integer',
            desc: 'Page number, from 1.',
            optional: true,
          },
          {
            name: 'pageSize',
            in: 'query',
            type: 'integer',
            desc: 'Up to 200; 50 by default.',
            optional: true,
          },
        ],
        responseSchema: 'AuditPage',
      },
      {
        method: 'POST',
        path: '/panel/api/access/audit/clear',
        summary: 'Delete every audit entry. The clearing becomes the first entry of the new log.',
      },
      {
        method: 'GET',
        path: '/panel/api/access/audit/settings',
        summary: 'Whether admin changes are logged, and how many days entries are kept.',
        responseSchema: 'AuditSettings',
      },
      {
        method: 'POST',
        path: '/panel/api/access/audit/settings',
        summary: 'Switch change logging and set the retention; 0 days keeps everything.',
        body: '{\n  "enabled": true,\n  "retentionDays": 90\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/clients/changeOwner',
        summary:
          "Hand users to another account (0 is the panel). A reseller's user limit is checked once for the whole selection.",
        body: '{\n  "emails": ["alice", "bob"],\n  "ownerId": 3\n}',
      },
    ],
  },

  {
    id: 'hosts',
    title: 'Hosts',
    description:
      'Per-inbound override endpoints. Each enabled host renders one extra subscription link/proxy with its own address/port/TLS, superseding the legacy externalProxy array. All endpoints under /panel/api/hosts.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/hosts/list',
        summary:
          'List every host across all inbounds, grouped by inbound then ordered by sort order.',
        responseSchema: 'HostGroup',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/hosts/raw',
        summary:
          'List every host row with its numeric id, for id-based pickers such as the per-client host rule override.',
        responseSchema: 'Host',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/hosts/get/:groupId',
        summary: 'Fetch a single host group by Group ID.',
        params: [{ name: 'groupId', in: 'path', type: 'string', desc: 'Host Group ID.' }],
        responseSchema: 'HostGroup',
      },
      {
        method: 'GET',
        path: '/panel/api/hosts/byInbound/:inboundId',
        summary: "Fetch one inbound's hosts, grouped by host group.",
        params: [{ name: 'inboundId', in: 'path', type: 'number', desc: 'Inbound ID.' }],
        responseSchema: 'HostGroup',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/hosts/tags',
        summary: 'Distinct, sorted set of tags used across all hosts.',
        response: '{\n  "success": true,\n  "obj": ["CDN", "EU", "FAST"]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/add',
        summary: 'Create a host group on inbounds.',
        body: '{\n  "inboundIds": [1],\n  "remark": "cdn-front",\n  "hosts": ["cdn.example.com"],\n  "port": 8443,\n  "security": "same",\n  "tags": ["CDN"]\n}',
        responseSchema: 'Host',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/update/:groupId',
        summary: 'Replace a host group’s content.',
        params: [{ name: 'groupId', in: 'path', type: 'string', desc: 'Host Group ID.' }],
        body: '{\n  "inboundIds": [1],\n  "remark": "cdn-front",\n  "hosts": ["cdn.example.com"],\n  "port": 8443,\n  "security": "same",\n  "tags": ["CDN"]\n}',
        responseSchema: 'Host',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/del/:groupId',
        summary: 'Delete a host group.',
        params: [{ name: 'groupId', in: 'path', type: 'string', desc: 'Host Group ID.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/setEnable/:groupId',
        summary: 'Enable or disable a host group.',
        params: [{ name: 'groupId', in: 'path', type: 'string', desc: 'Host Group ID.' }],
        body: '{\n  "enable": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/reorder',
        summary:
          'Set host group sort order by the position of each groupId in the array. IDs must be unique and refer to existing groups; omitted groups follow in their previous order.',
        body: '{\n  "ids": ["abc-123", "def-456"]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/bulk/add',
        summary: 'Add a host group to inbounds (same as /add).',
        body: '{\n  "inboundIds": [1, 2],\n  "hosts": ["cdn.example.com", "cdn2.example.com:443"],\n  "remark": "Cloudflare CDN",\n  "port": 0,\n  "security": "same",\n  "isDisabled": false\n}',
        responseSchema: 'Host',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/bulk/setEnable',
        summary: 'Enable or disable many host groups in one call.',
        body: '{\n  "ids": ["abc-123", "def-456"],\n  "enable": false\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/hosts/bulk/del',
        summary: 'Delete many host groups in one call.',
        body: '{\n  "ids": ["abc-123", "def-456"]\n}',
      },
    ],
  },

  {
    id: 'backup',
    title: 'Backup',
    description: 'Operations that interact with the configured Telegram bot.',
    endpoints: [
      {
        method: 'POST',
        path: '/panel/api/backuptotgbot',
        summary:
          'Send a fresh DB backup to every Telegram chat configured as an admin recipient. No body, no params.',
      },
    ],
  },

  {
    id: 'settings',
    title: 'Settings',
    description:
      'Panel configuration and user credentials. All endpoints live under /panel/api/setting and require a logged-in session or Bearer token.',
    endpoints: [
      {
        method: 'POST',
        path: '/panel/api/setting/all',
        summary:
          'Return every panel setting: web server, Telegram bot, subscription, security, LDAP. The full JSON blob that the Settings page edits.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "webPort": 2053,\n    "webCertFile": "",\n    "webKeyFile": "",\n    "webBasePath": "/",\n    "subPort": 10882,\n    "subPath": "/sub/",\n    "subClashAutoDetect": false,\n    "subClashUserAgentRegex": "",\n    "subJsonEnable": false,\n    "subJsonAutoDetect": false,\n    "subJsonAlwaysArray": false,\n    "subJsonUserAgentRegex": "",\n    "subJsonPath": "/json/",\n    "subJsonURI": "https://sub.example.com/json/",\n    "subClashEnable": true,\n    "subClashPath": "/clash/",\n    "subClashURI": "https://sub.example.com/clash/",\n    "tgBotEnable": false,\n    "tgBotToken": "",\n    ...\n  }\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/defaultSettings',
        summary:
          'Return the computed default settings based on the request host. Useful to preview what a fresh install would use.',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/factoryDefaults',
        summary:
          'Return the shipped (factory) default value per browser-safe setting key, so clients can tell a stored value apart from the default it would fall back to. Per-install material (secret, panelGuid, mTLS keys) and credential fields are never included.',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/update',
        summary:
          'Persist every setting at once. The body mirrors the shape returned by /all. Invalid values (bad ports, missing cert pairs, etc.) are rejected before write.',
        body: '{\n  "webPort": 2053,\n  "webBasePath": "/",\n  "subPort": 10882,\n  "subPath": "/sub/",\n  "subClashAutoDetect": false,\n  "subClashUserAgentRegex": "",\n  "subJsonEnable": false,\n  "subJsonAutoDetect": false,\n  "subJsonAlwaysArray": false,\n  "subJsonUserAgentRegex": "",\n  "subJsonPath": "/json/",\n  "subJsonURI": "https://sub.example.com/json/",\n  "subClashEnable": true,\n  "subClashPath": "/clash/",\n  "subClashURI": "https://sub.example.com/clash/",\n  "tgBotEnable": false,\n  ...\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/validateRegex',
        summary:
          'Validate any regular expression with the backend Go RE2 compiler without saving it.',
        body: '{\n  "regex": "(?m)^general-purpose$"\n}',
        response: '{\n  "success": true,\n  "msg": ""\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/updateUser',
        summary:
          'Change the panel admin username and password. Requires the current credentials for verification. The session is refreshed with the new values on success.',
        params: [
          { name: 'oldUsername', in: 'body', type: 'string', desc: 'Current admin username.' },
          { name: 'oldPassword', in: 'body', type: 'string', desc: 'Current admin password.' },
          { name: 'newUsername', in: 'body', type: 'string', desc: 'Desired new username.' },
          { name: 'newPassword', in: 'body', type: 'string', desc: 'Desired new password.' },
        ],
        body: '{\n  "oldUsername": "admin",\n  "oldPassword": "admin",\n  "newUsername": "newadmin",\n  "newPassword": "newpass"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/restartPanel',
        summary:
          'Restart the entire Manifold process after a 3-second grace period. The connection drops immediately; the panel comes back online ~5-10 seconds later.',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/testSmtp',
        summary:
          'Test SMTP connection with stage-by-stage reporting (connect, auth, send). Returns structured result with stage and message.',
        response:
          '{\n  "success": true,\n  "stage": "send",\n  "msg": "Test email sent successfully"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/testTgBot',
        summary: 'Test Telegram bot connection by sending a test message to the configured chat.',
        response: '{\n  "success": true,\n  "msg": "Test message sent to Telegram"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/tgBotCapabilities',
        summary:
          'Report what the configured bot can do. Inline mode is a BotFather switch, so the panel warns when it is off.',
        responseSchema: 'TgBotCapabilities',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/testDiscord',
        summary: 'Test Discord bot connection by sending a test embed to the configured channel.',
        response: '{\n  "success": true,\n  "msg": "Test notification sent successfully"\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/setting/subThemes',
        summary:
          'The subscription page themes to pick from: the built-in ones, then the ones installed from a catalogue.',
        responseSchema: 'Theme',
      },
      {
        method: 'GET',
        path: '/panel/api/setting/subThemes/catalog',
        summary:
          'Read the theme catalogue (subThemeCatalogURL, empty for the Manifold repository) through the egress proxy; each entry says which version is installed.',
        responseSchema: 'CatalogTheme',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/subThemes/install',
        summary:
          'Download a catalogue theme into the themes folder. An older copy is replaced only once every file has arrived and the page parses.',
        params: [
          { name: 'name', in: 'body', type: 'string', desc: 'Theme name in the catalogue.' },
        ],
        body: '{\n  "name": "neon"\n}',
        responseSchema: 'Theme',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/subThemes/remove',
        summary:
          'Delete an installed theme. When it was the one in use, the page goes back to the panel page.',
        params: [{ name: 'name', in: 'body', type: 'string', desc: 'Installed theme name.' }],
        body: '{\n  "name": "neon"\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/setting/getDefaultJsonConfig',
        summary:
          'Return the built-in default Xray JSON config template that ships with this panel version.',
      },
    ],
  },

  {
    id: 'api-tokens',
    title: 'API Tokens',
    description:
      'Manage scoped Bearer tokens for programmatic auth. admin grants the full panel API; monitor is read-only for status and metrics; node-sync manages node configuration without panel updates; node-admin adds panel self-update to node-sync. Tokens may expire and are stored as SHA-256 hashes. The plaintext is returned only once at creation.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/setting/apiTokens',
        summary:
          'List every API token, enabled or not. The token value is never returned — only metadata.',
        response:
          '{\n  "success": true,\n  "obj": [\n    {\n      "id": 1,\n      "name": "central-panel-a",\n      "enabled": true,\n      "createdAt": 1736000000,\n      "scope": "node-admin",\n      "expiresAt": 0\n    }\n  ]\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/apiTokens/create',
        summary:
          'Mint a scoped API token. The server-generated plaintext is returned only once and stored as a hash.',
        params: [
          {
            name: 'name',
            in: 'body',
            type: 'string',
            desc: 'Human-readable label, e.g. "central-panel-a".',
          },
          {
            name: 'scope',
            in: 'body',
            type: 'string',
            desc: 'admin (default), monitor, node-sync, or node-admin. node-admin inherits node-sync and may also start a panel update.',
            enum: ['admin', 'monitor', 'node-sync', 'node-admin', 'tunnel'],
            optional: true,
          },
          {
            name: 'expiresAt',
            in: 'body',
            type: 'number',
            desc: 'Future Unix milliseconds, or 0 for no expiry.',
            optional: true,
          },
          {
            name: 'maxRequests',
            in: 'body',
            type: 'number',
            desc: 'Admin scope: requests the token may make over its life, 0 for no cap. Past it the token is refused with 403.',
            optional: true,
          },
          {
            name: 'deleteOnLimit',
            in: 'body',
            type: 'boolean',
            desc: 'Admin scope: delete the token once it reaches maxRequests.',
            optional: true,
          },
        ],
        body: '{\n  "name": "central-panel-a",\n  "scope": "node-admin",\n  "expiresAt": 1798761600000\n}',
        responseSchema: 'ApiTokenView',
        errorResponse:
          '{\n  "success": false,\n  "msg": "a token with that name already exists"\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/apiTokens/delete/:id',
        summary:
          'Permanently delete a token. Any caller using it stops authenticating immediately.',
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Token row ID.' },
          {
            name: 'expectedScope',
            in: 'body',
            type: 'string',
            desc: 'Stored scope expected by the operator.',
          },
        ],
        body: '{\n  "expectedScope": "node-sync"\n}',
        response: '{\n  "success": true\n}',
      },
      {
        method: 'POST',
        path: '/panel/api/setting/apiTokens/setEnabled/:id',
        summary:
          'Toggle a token enabled/disabled without deleting it. Disabled tokens are rejected by checkAPIAuth on the next request.',
        params: [
          { name: 'id', in: 'path', type: 'number', desc: 'Token row ID.' },
          { name: 'enabled', in: 'body', type: 'boolean', desc: 'New enabled state.' },
          {
            name: 'expectedScope',
            in: 'body',
            type: 'string',
            desc: 'Stored scope expected by the operator.',
          },
        ],
        body: '{\n  "enabled": false,\n  "expectedScope": "node-sync"\n}',
        response: '{\n  "success": true\n}',
      },
    ],
  },

  {
    id: 'xray-settings',
    title: 'Xray Settings',
    description:
      'Xray configuration template, outbound management, Warp/Nord/PIA integration, and config testing. All endpoints under /panel/api/xray.',
    endpoints: [
      {
        method: 'POST',
        path: '/panel/api/xray/',
        summary:
          'Return the Xray config template (JSON string), available inbound tags, client reverse tags, and the configured outbound test URL in one response.',
        response:
          '{\n  "success": true,\n  "obj": {\n    "xraySetting": "{...raw xray config...}",\n    "inboundTags": "[\\"in-443-tcp\\"]",\n    "clientReverseTags": "[]",\n    "outboundTestUrl": "https://www.google.com/generate_204"\n  }\n}',
      },
      {
        method: 'GET',
        path: '/panel/api/xray/getDefaultJsonConfig',
        summary:
          'Return the built-in default Xray config shipped with the panel (identical to /panel/api/setting/getDefaultJsonConfig).',
      },
      {
        method: 'GET',
        path: '/panel/api/xray/getOutboundsTraffic',
        summary:
          'Return traffic statistics for every outbound. Each outbound shows up/down/total counters.',
      },
      {
        method: 'GET',
        path: '/panel/api/xray/getXrayResult',
        summary:
          'Return the most recent Xray process stdout/stderr output. Useful to check for startup errors or runtime warnings.',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/update',
        summary:
          'Save the Xray JSON config template and optionally the outbound test URL. Both are sent as form fields.',
        params: [
          {
            name: 'xraySetting',
            in: 'body (form)',
            type: 'string',
            desc: 'Full Xray JSON config template.',
          },
          {
            name: 'outboundTestUrl',
            in: 'body (form)',
            type: 'string',
            desc: 'URL used for outbound reachability tests. Defaults to https://www.google.com/generate_204.',
            optional: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/warp/:action',
        summary: 'Manage Cloudflare Warp integration. The action parameter selects the operation.',
        params: [
          {
            name: 'action',
            in: 'path',
            type: 'string',
            desc: 'data — return Warp stats. del — delete Warp data. config — return current config. reg — register (sends keys). regMasque — register a separate device enrolled for MASQUE and return its warp key pair, addresses and endpoint. changeIp — rotate the endpoint. license — set a Warp+ key. interval — set automatic rotation in hours.',
          },
          {
            name: 'privateKey',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=reg.',
            optional: true,
          },
          {
            name: 'publicKey',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=reg.',
            optional: true,
          },
          {
            name: 'license',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=license.',
            optional: true,
          },
          {
            name: 'interval',
            in: 'body (form)',
            type: 'integer',
            desc: 'Non-negative hours between automatic rotations. Required when action=interval; 0 disables rotation.',
            optional: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/nord/:action',
        summary: 'Manage NordVPN integration. The action parameter selects the operation.',
        params: [
          {
            name: 'action',
            in: 'path',
            type: 'string',
            desc: 'countries — list available countries. servers — list servers in a country (sends countryId). reg — get NordVPN credentials (sends token). setKey — store NordVPN API key (sends key). data — return current NordVPN connection data. del — delete NordVPN data.',
          },
          {
            name: 'countryId',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=servers.',
            optional: true,
          },
          {
            name: 'token',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=reg.',
            optional: true,
          },
          {
            name: 'key',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=setKey.',
            optional: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/pia/:action',
        summary: 'Manage PIA WireGuard integration. The action parameter selects the operation.',
        params: [
          {
            name: 'action',
            in: 'path',
            type: 'string',
            desc: 'countries — list available countries from the signed PIA server list. servers — list regions and WireGuard servers in a country (sends countryCode). reg — sign in with a PIA username and password (sends username, password). data — return the signed-in account hint. del — delete stored PIA credentials. addKey — register a WireGuard key with the selected server (sends hostname) and return fields to build the outbound.',
          },
          {
            name: 'username',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=reg.',
            optional: true,
          },
          {
            name: 'password',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=reg.',
            optional: true,
          },
          {
            name: 'countryCode',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=servers.',
            optional: true,
          },
          {
            name: 'hostname',
            in: 'body (form)',
            type: 'string',
            desc: 'Required when action=addKey.',
            optional: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/resetOutboundsTraffic',
        summary: 'Reset traffic counters for a specific outbound by tag.',
        params: [
          {
            name: 'tag',
            in: 'body (form)',
            type: 'string',
            desc: 'Outbound tag to reset (e.g. "proxy", "direct").',
          },
        ],
        body: 'tag=proxy',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/testOutbound',
        summary:
          'Test an outbound configuration. Sends the outbound JSON (required), optionally all outbounds (to resolve sockopt.dialerProxy dependencies), and a mode flag.',
        params: [
          {
            name: 'outbound',
            in: 'body (form)',
            type: 'string',
            desc: 'JSON-encoded single outbound to test (required).',
          },
          {
            name: 'allOutbounds',
            in: 'body (form)',
            type: 'string',
            desc: 'JSON array of all outbounds — used to resolve dialerProxy chains.',
            optional: true,
          },
          {
            name: 'mode',
            in: 'body (form)',
            type: 'string',
            desc: '"tcp" for a fast dial-only probe (parallel-safe), "real" for a real-delay probe whose delay is the full request time including tunnel establishment. Default/empty uses a full HTTP probe reporting the warm per-request round-trip. Both HTTP variants run through a temp xray instance.',
            optional: true,
          },
        ],
        body: 'outbound={"protocol":"freedom","settings":{}}&mode=tcp',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/testOutbounds',
        summary:
          'Test a batch of outbounds (max 50) through one shared temp xray instance. Returns an array of results in input order, each with the outbound tag, delay, HTTP status and a connect/TLS/TTFB timing breakdown.',
        params: [
          {
            name: 'outbounds',
            in: 'body (form)',
            type: 'string',
            desc: 'JSON array of outbound configs to test (required).',
          },
          {
            name: 'allOutbounds',
            in: 'body (form)',
            type: 'string',
            desc: 'JSON array of all outbounds — used to resolve dialerProxy chains.',
            optional: true,
          },
          {
            name: 'mode',
            in: 'body (form)',
            type: 'string',
            desc: '"tcp" for fast dial-only probes (UDP-transport outbounds are still probed over HTTP), "real" for real-delay probes whose delay is the full request time including tunnel establishment. Default/empty routes an HTTP request through each outbound and reports the warm per-request round-trip.',
            optional: true,
          },
        ],
        body: 'outbounds=[{"tag":"direct","protocol":"freedom","settings":{}}]&mode=http',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/balancerStatus',
        summary:
          'Live state of routing balancers in the running core (RoutingService.GetBalancerInfo): current override and the targets the strategy prefers. Returns a map keyed by balancer tag.',
        params: [
          {
            name: 'tags',
            in: 'body (form)',
            type: 'string',
            desc: 'Comma-separated balancer tags to query (e.g. "b1,b2").',
          },
        ],
        body: 'tags=b1,b2',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/balancerOverride',
        summary:
          'Force a balancer in the running core to always pick one outbound (RoutingService.OverrideBalancerTarget). Applied live without a restart and kept across config saves; cleared when Xray restarts or the target outbound is removed.',
        params: [
          { name: 'tag', in: 'body (form)', type: 'string', desc: 'Balancer tag (required).' },
          {
            name: 'target',
            in: 'body (form)',
            type: 'string',
            desc: 'Outbound tag to force; it must exist in the running config. Empty clears the override and returns control to the strategy.',
            optional: true,
          },
        ],
        body: 'tag=b1&target=proxy',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/routeTest',
        summary:
          'Ask the running core which outbound its router would pick for a synthetic connection (RoutingService.TestRoute). No traffic is sent.',
        bodyRequiredOneOf: ['domain', 'ip'],
        params: [
          {
            name: 'domain',
            in: 'body (form)',
            type: 'string',
            desc: 'Target domain. Either domain or ip is required.',
            optional: true,
            minLength: 1,
          },
          {
            name: 'ip',
            in: 'body (form)',
            type: 'string',
            desc: 'Target IP. Either domain or ip is required.',
            optional: true,
            minLength: 1,
          },
          {
            name: 'port',
            in: 'body (form)',
            type: 'number',
            desc: 'Target port (optional).',
            optional: true,
          },
          {
            name: 'network',
            in: 'body (form)',
            type: 'string',
            desc: '"tcp" (default) or "udp".',
            optional: true,
          },
          {
            name: 'inboundTag',
            in: 'body (form)',
            type: 'string',
            desc: 'Simulate arrival on this inbound (optional).',
            optional: true,
          },
          {
            name: 'protocol',
            in: 'body (form)',
            type: 'string',
            desc: 'Sniffed protocol such as http, tls, bittorrent (optional).',
            optional: true,
          },
          {
            name: 'email',
            in: 'body (form)',
            type: 'string',
            desc: 'User attribution for user-based rules (optional).',
            optional: true,
          },
        ],
        body: 'domain=example.com&port=443&network=tcp',
      },
      {
        method: 'GET',
        path: '/panel/api/xray/geodata/files',
        summary:
          'List the geo databases (.dat files) in the Xray asset folder, with the layout detected from their contents, size, modification time and category count. A database that fails to parse is still listed, with the reason in "error".',
      },
      {
        method: 'GET',
        path: '/panel/api/xray/geodata/categories',
        summary:
          'One page of a database\'s categories, each with its entry count and the attributes its domains carry (e.g. "ads", "cn").',
        params: [
          {
            name: 'file',
            in: 'query',
            type: 'string',
            desc: 'Database file name inside the asset folder, e.g. geosite.dat (required).',
          },
          {
            name: 'q',
            in: 'query',
            type: 'string',
            optional: true,
            desc: 'Case-insensitive substring filter on the category code.',
          },
          {
            name: 'offset',
            in: 'query',
            type: 'integer',
            optional: true,
            desc: 'Rows to skip. Defaults to 0.',
          },
          {
            name: 'limit',
            in: 'query',
            type: 'integer',
            optional: true,
            desc: 'Rows to return, capped at 500. Omit it to return every category — the index is small and the panel filters it client-side.',
          },
        ],
      },
      {
        method: 'GET',
        path: '/panel/api/xray/geodata/entries',
        summary:
          'One page of the rules inside a category — domain rules typed as domain/full/keyword/regexp for geosite databases, CIDRs for geoip ones.',
        params: [
          {
            name: 'file',
            in: 'query',
            type: 'string',
            desc: 'Database file name inside the asset folder (required).',
          },
          {
            name: 'code',
            in: 'query',
            type: 'string',
            desc: 'Category code, case-insensitive, e.g. google (required).',
          },
          {
            name: 'q',
            in: 'query',
            type: 'string',
            optional: true,
            desc: 'Case-insensitive substring filter on the rule value.',
          },
          {
            name: 'offset',
            in: 'query',
            type: 'integer',
            optional: true,
            desc: 'Rows to skip. Defaults to 0.',
          },
          {
            name: 'limit',
            in: 'query',
            type: 'integer',
            optional: true,
            desc: 'Rows to return, capped at 500. Defaults to the cap.',
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/geodata/validate',
        summary:
          'Check routing tokens against the databases on disk and return only the ones that do not resolve. Plain domains and CIDRs are ignored. Each issue carries a reason: syntax, fileMissing or categoryMissing.',
        params: [
          {
            name: 'tokens',
            in: 'body (form)',
            type: 'string',
            desc: 'Comma-separated routing tokens, e.g. "geosite:google,geosite:blabla". Max 500 per request.',
          },
          {
            name: 'kind',
            in: 'body (form)',
            type: 'string',
            desc: '"ip" to parse the tokens as IP rules (geoip:, ext-ip:, leading !). Anything else parses them as domain rules (geosite:, ext-site:).',
            optional: true,
          },
        ],
        body: 'kind=domain&tokens=geosite:google,geosite:blabla',
      },
      {
        method: 'GET',
        path: '/panel/api/xray/outbound-subs',
        summary:
          'List all outbound subscriptions (remote URLs that supply additional outbounds), newest first.',
      },
      {
        method: 'POST',
        path: '/panel/api/xray/outbound-subs',
        summary:
          'Create an outbound subscription. The URL is fetched, parsed into outbounds with stable tags, and merged additively into the running Xray config.',
        params: outboundSubscriptionBodyParams,
      },
      {
        method: 'POST',
        path: '/panel/api/xray/outbound-subs/:id',
        summary:
          'Update an existing outbound subscription by id. Accepts the same form fields as create.',
        params: [
          { name: 'id', in: 'path', type: 'integer', desc: 'Subscription id.' },
          ...outboundSubscriptionBodyParams,
        ],
      },
      {
        method: 'DELETE',
        path: '/panel/api/xray/outbound-subs/:id',
        summary: 'Delete an outbound subscription by id.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Subscription id.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/outbound-subs/:id/del',
        summary:
          'Delete an outbound subscription by id (POST alias of DELETE for clients that cannot send DELETE).',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Subscription id.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/outbound-subs/:id/refresh',
        summary:
          'Force an immediate re-fetch of the subscription and return the parsed outbounds. Signals Xray to reload.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Subscription id.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/outbound-subs/:id/move',
        summary:
          'Reorder a subscription one step up or down in priority (controls its position in the merged outbounds).',
        params: [
          { name: 'id', in: 'path', type: 'integer', desc: 'Subscription id.' },
          {
            name: 'dir',
            in: 'body (form)',
            type: 'string',
            desc: '"up" to raise priority, anything else to lower it.',
            optional: true,
          },
        ],
      },
      {
        method: 'POST',
        path: '/panel/api/xray/outbound-subs/parse',
        summary:
          'Preview a subscription URL: fetch and parse it into outbounds without persisting anything.',
        params: [
          {
            name: 'url',
            in: 'body (form)',
            type: 'string',
            desc: 'Subscription URL to preview (required).',
          },
          {
            name: 'userAgent',
            in: 'body (form)',
            type: 'string',
            desc: 'Custom User-Agent sent while fetching the preview.',
            optional: true,
          },
          {
            name: 'allowPrivate',
            in: 'body (form)',
            type: 'boolean',
            desc: 'Allow a private/internal/loopback URL. Default false.',
            optional: true,
          },
          {
            name: 'allowInsecure',
            in: 'body (form)',
            type: 'boolean',
            desc: 'Skip TLS certificate verification. Default false.',
            optional: true,
          },
        ],
      },
    ],
  },

  {
    id: 'sub-balancers',
    title: 'Subscription Balancers',
    description:
      'Client-side balancers for the JSON subscription: each enabled balancer is emitted as one extra config document whose members are the proxy outbounds of the selected inbounds (routing.balancers + burstObservatory). Managed in Settings → Sub Balancers.',
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/sub-balancers',
        summary: 'List all subscription balancers in sort order (sort_order asc, id asc).',
        responseSchema: 'SubBalancer',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/sub-balancers',
        summary:
          'Create a subscription balancer. It appears in the JSON subscription of every client that sits on at least one selected inbound.',
        params: subBalancerBodyParams,
        responseSchema: 'SubBalancer',
      },
      {
        method: 'POST',
        path: '/panel/api/sub-balancers/:id',
        summary:
          'Update a balancer by id. Accepts the same form fields as create (full-row update); omitting memberWeights clears stored weights, while omitting enabled keeps its current value.',
        params: [
          { name: 'id', in: 'path', type: 'integer', desc: 'Balancer id.' },
          ...subBalancerBodyParams,
        ],
        responseSchema: 'SubBalancer',
      },
      {
        method: 'DELETE',
        path: '/panel/api/sub-balancers/:id',
        summary: 'Delete a balancer by id.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Balancer id.' }],
        responseSchema: 'SubBalancer',
      },
      {
        method: 'POST',
        path: '/panel/api/sub-balancers/:id/del',
        summary:
          'Delete a balancer by id (POST alias of DELETE for clients that cannot send DELETE).',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Balancer id.' }],
        responseSchema: 'SubBalancer',
      },
    ],
  },

  {
    id: 'app',
    title: 'App',
    description:
      "The locations the app offers, the notices it shows and the support reports it sends. A location (Germany, say) gathers the inbounds that serve it, on any protocol and any server; an inbound serves one location at most, and one in none is still used by the app's quick connect. The app itself signs in on the subscription server (see Subscription Server → app/v1); app access is switched on with the appEnable setting.",
    endpoints: [
      {
        method: 'GET',
        path: '/panel/api/app/locations',
        summary: 'List the locations, each with the ids of the inbounds that serve it.',
        responseSchema: 'AppLocation',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/app/locations',
        summary:
          'Create a location (id 0 or absent) or replace one, its inbounds included. Refused when the name is empty, the country is not a two-letter ISO 3166 code, the timezone is unknown, an inbound does not exist or already serves another location.',
        body: '{\n  "id": 0,\n  "name": "Germany",\n  "countryCode": "DE",\n  "city": "Frankfurt",\n  "timezone": "Europe/Berlin",\n  "inboundIds": [3, 7, 12]\n}',
        responseSchema: 'AppLocation',
      },
      {
        method: 'POST',
        path: '/panel/api/app/locations/del/:id',
        summary: 'Delete a location. Its inbounds stay, serving no location.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Location id.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/app/notices',
        summary: 'List the notices published to the app, newest first.',
        responseSchema: 'AppNotice',
        responseSchemaArray: true,
      },
      {
        method: 'POST',
        path: '/panel/api/app/notices',
        summary:
          "Publish a notice to every signed-in app, which receives it on its next profile refresh (every 10 minutes, or when the user refreshes). Level info shows in the app's notices list only; warning and critical also show as an amber or a red banner. Refused when the text is empty, over 1000 characters or the level is unknown.",
        body: '{\n  "body": "قطعی سرور هلند تا ساعت ۱۸",\n  "level": "critical"\n}',
        responseSchema: 'AppNotice',
      },
      {
        method: 'POST',
        path: '/panel/api/app/notices/del/:id',
        summary: 'Delete a notice. Apps drop it on their next profile refresh.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Notice id.' }],
      },
      {
        method: 'GET',
        path: '/panel/api/app/reports',
        summary:
          "List the support reports apps sent, newest first, without their logs: each with the account's username and Telegram id, the device, the app version, the user's message and whether it was opened. The panel keeps the newest 500.",
        responseSchema: 'AppReport',
        responseSchemaArray: true,
      },
      {
        method: 'GET',
        path: '/panel/api/app/reports/unseen',
        summary: 'Count the reports no one has opened yet.',
      },
      {
        method: 'GET',
        path: '/panel/api/app/reports/:id',
        summary: 'Return one report with its log.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Report id.' }],
        responseSchema: 'AppReport',
      },
      {
        method: 'POST',
        path: '/panel/api/app/reports/seen/:id',
        summary: 'Mark a report as opened.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Report id.' }],
      },
      {
        method: 'POST',
        path: '/panel/api/app/reports/del/:id',
        summary: 'Delete a report.',
        params: [{ name: 'id', in: 'path', type: 'integer', desc: 'Report id.' }],
      },
    ],
  },

  {
    id: 'subscription',
    title: 'Subscription Server',
    description:
      'A separate HTTP/HTTPS server that serves proxy subscription links (standard, JSON, and Clash) to clients. The server listens on its own port (default 2096) and is configured in Settings → Subscription. Fresh panels generate random path prefixes for each format; all paths remain configurable. Every subscription endpoint sets response headers for client apps to read traffic/expiry info.',
    subHeader: [
      {
        name: 'Subscription-Userinfo',
        desc: 'Traffic and expiry: <code>upload=N; download=N; total=N; expire=TS</code>',
      },
      { name: 'Profile-Title', desc: 'Base64-encoded subscription display name' },
      { name: 'Profile-Web-Page-Url', desc: 'Link to the subscription info page' },
      { name: 'Support-Url', desc: 'Support contact URL configured in settings' },
      {
        name: 'Profile-Update-Interval',
        desc: 'Suggested polling interval in minutes (e.g. <code>10</code>)',
      },
      { name: 'Announce', desc: 'Base64-encoded announcement string' },
      {
        name: 'Routing-Enable',
        desc: '<code>true</code> or <code>false</code> — whether routing rules are included',
      },
      {
        name: 'Routing',
        desc: 'Global routing rules for client apps that support them (e.g. Happ)',
      },
    ],
    endpoints: [
      {
        method: 'GET',
        path: '/{subPath}:subid',
        summary:
          'Return base64-encoded subscription links for all enabled clients matching the subscription ID. When the request has an Accept: text/html header or ?html=1, renders a styled info page instead. With ?format=info, returns the page view-model as JSON (traffic, expiry, online status; no links) for live polling. The path prefix is configured by subPath.',
        params: [
          { name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' },
          {
            name: 'format',
            in: 'query',
            type: 'string',
            optional: true,
            desc: 'Set to "info" to get the subscription status view-model as JSON instead of the links.',
          },
        ],
      },
      {
        method: 'HEAD',
        path: '/{subPath}:subid',
        summary:
          'Return the same status and subscription metadata headers as GET without a response body.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        responses: subscriptionHeadResponses,
      },
      {
        method: 'GET',
        path: '/{subPath}:subid/hwid-status',
        summary:
          'Return aggregate HWID device-slot usage for the subscription: whether an HWID limit is active, the limit, how many devices are registered and how many slots remain. Read-only — it never registers a device, so asking does not consume a slot. Counters only: no HWID value, email or device metadata. The path prefix is configured by subPath.',
        description:
          'Responds with the bare HwidSlotStatus object, not the <code>{success,msg,obj}</code> panel envelope, like the other subscription-server routes. With no HWID limit configured, <code>active</code> is false and every counter is 0.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        responses: {
          '200': {
            description: 'Device-slot counters for the subscription.',
            content: {
              'application/json': { schema: { $ref: '#/components/schemas/HwidSlotStatus' } },
            },
          },
          ...hwidStatusErrorResponses,
        },
      },
      {
        method: 'HEAD',
        path: '/{subPath}:subid/hwid-status',
        summary:
          'Return the HWID device-slot status code and headers as GET without a response body.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        responses: {
          '200': { description: 'Headers match GET; no response body.' },
          ...hwidStatusErrorResponses,
        },
      },
      {
        method: 'POST',
        path: '/{subPath}:subid/app/v1/login',
        summary:
          'Sign a device in to the app with its subscription link and the username of one of the link\'s accounts. The device takes a slot of the same HWID limit the link\'s other apps share. Answers <code>{"token":"..."}</code>; the same app/v1 routes answer under every subscription path.',
        description:
          'JSON body: <code>username</code>, <code>deviceId</code> (6-128 characters, stable per device), <code>deviceName</code>, <code>os</code>, <code>osVersion</code>, <code>appVersion</code>. Errors are <code>{"error":code}</code>: 403 <code>app_disabled</code>, 401 <code>bad_login</code>, 400 <code>bad_device</code>, 403 <code>device_limit</code>, 429 <code>too_many_attempts</code> (5 failures per address and link in 5 minutes block for 15).',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
      },
      {
        method: 'GET',
        path: '/{subPath}:subid/app/v1/profile',
        summary:
          'Return the signed-in account, its locations, every entry of its subscription, with the Xray outbound of each entry Xray can run, and the 50 newest notices. OpenVPN profiles carry the login even when the server asks other apps to prompt for it. Needs <code>Authorization: Bearer token</code> and <code>X-HWID: deviceId</code>.',
        description:
          "A session ends when the device is removed from the client's devices, the link is rotated, the account is renamed or app access is switched off; the call then answers 401 <code>bad_session</code> or 403 <code>app_disabled</code>, and 403 <code>device_limit</code> when the device lost its slot.",
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        responses: {
          '200': {
            description: 'The account and its configs.',
            content: {
              'application/json': { schema: { $ref: '#/components/schemas/AppProfile' } },
            },
          },
        },
      },
      {
        method: 'POST',
        path: '/{subPath}:subid/app/v1/logout',
        summary: 'End the session of the bearer token.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
      },
      {
        method: 'POST',
        path: '/{subPath}:subid/app/v1/disconnected',
        summary:
          "The device left a connection, so its user-limit seat is freed at once. WireGuard: the account's seat at that location, on whichever server holds it. OpenVPN: the session with that tunnel address. L2TP and Xray protocols need nothing and are answered the same. Needs the bearer token and X-HWID.",
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        body: '{\n  "protocol": "wireguard",\n  "locationId": 3,\n  "address": "10.8.0.6"\n}',
        response: '{\n  "ok": true\n}',
      },
      {
        method: 'POST',
        path: '/{subPath}:subid/app/v1/report',
        summary:
          "File a support report: the user's message and the app's log, under the session's account and its Telegram id. Answers <code>{\"id\":N}</code>, the number the user can quote. Needs <code>Authorization: Bearer token</code> and <code>X-HWID: deviceId</code>.",
        description:
          'JSON body, sent gzipped with <code>Content-Encoding: gzip</code> (plain JSON is taken too): <code>message</code> (cut to 4000 characters), <code>log</code> (8 MB at most), <code>deviceName</code>, <code>os</code>, <code>osVersion</code>, <code>appVersion</code>. Errors are <code>{"error":code}</code>: 401 <code>bad_session</code>, 403 <code>app_disabled</code>, 400 <code>bad_request</code>, 413 <code>too_large</code> (over 3 MB on the wire or 9 MB unpacked), 429 <code>too_many_reports</code> (5 per device an hour).',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
      },
      {
        method: 'GET',
        path: '/{jsonPath}:subid',
        summary:
          'Return subscription as a JSON array of proxy configs (one per enabled client). Only when JSON subscription is enabled in settings. The path prefix is configured by subJsonPath.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
      },
      {
        method: 'HEAD',
        path: '/{jsonPath}:subid',
        summary:
          'Return the JSON subscription status and metadata headers without a body. Registered only when JSON subscriptions are enabled.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        responses: subscriptionHeadResponses,
      },
      {
        method: 'GET',
        path: '/{clashPath}:subid',
        summary:
          'Return subscription as a Clash/Mihomo-compatible YAML config, including configured global Clash routing rules. Only when Clash subscription is enabled in settings. The path prefix is configured by subClashPath.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
      },
      {
        method: 'HEAD',
        path: '/{clashPath}:subid',
        summary:
          'Return the Clash subscription status and metadata headers without a body. Registered only when Clash subscriptions are enabled.',
        params: [{ name: 'subid', in: 'path', type: 'string', desc: 'Client subscription ID.' }],
        responses: subscriptionHeadResponses,
      },
    ],
  },

  {
    id: 'websocket',
    title: 'WebSocket',
    description:
      'Real-time status updates via WebSocket. Connect once at <code>ws://<panel>/ws</code> to receive a stream of JSON messages without polling. Requires an authenticated session cookie (Bearer token auth is not supported). Each message has a <code>type</code> field that identifies the payload shape.',
    endpoints: [
      {
        method: 'GET',
        path: '/ws',
        summary:
          'Upgrade an HTTP connection to a WebSocket. Requires an authenticated session cookie (Bearer token auth is not supported here). Returns 101 Switching Protocols on success. The server then pushes JSON messages described below.',
        responses: {
          '101': { description: 'Switching Protocols. WebSocket messages use WebSocketEnvelope.' },
          '401': { description: 'No authenticated panel session cookie.' },
        },
        security: [{ cookieAuth: [] }],
      },
    ],
  },
];
