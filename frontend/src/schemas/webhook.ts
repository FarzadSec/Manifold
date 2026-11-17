import { z } from 'zod';

export const WebhookFormSchema = z
  .object({
    id: z.number(),
    name: z.string().trim().min(1, 'pages.nodes.toasts.fillRequired'),
    enable: z.boolean(),
    kind: z.enum(['http', 'ntfy', 'bale']),
    url: z.string().trim(),
    chatId: z.string().trim(),
    events: z.array(z.string()),
    secret: z.string(),
  })
  .loose();

export type WebhookFormValues = z.infer<typeof WebhookFormSchema>;

export const WebhookURLSchema = z
  .string()
  .trim()
  .regex(/^https?:\/\/[^/\s]+/, 'pages.webhooks.urlInvalid');

export type EventGroup = 'user' | 'node' | 'backup' | 'system';

// eventGroup files users.bulk_changed with user.*; unknown prefixes are system events.
export function eventGroup(event: string): EventGroup {
  const head = event.split('.')[0].replace(/s$/, '');
  return head === 'user' || head === 'node' || head === 'backup' ? head : 'system';
}
