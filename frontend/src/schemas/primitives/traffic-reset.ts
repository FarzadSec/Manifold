/**
 * The traffic reset cycles an inbound or a client may be put on. Shared so the
 * inbound form, the client form and the bulk-add form cannot drift apart.
 */
export const TRAFFIC_RESETS = ['never', 'hourly', 'daily', 'weekly', 'monthly'] as const;

export type TrafficResetCycle = (typeof TRAFFIC_RESETS)[number];

/** A client's own cycle may also be yearly or every N days, on a weekday or in the Persian calendar. */
export const CLIENT_TRAFFIC_RESETS = [...TRAFFIC_RESETS, 'yearly', 'days'] as const;

export type ClientTrafficResetCycle = (typeof CLIENT_TRAFFIC_RESETS)[number];
