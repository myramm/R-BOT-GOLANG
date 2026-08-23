import { z } from 'zod';

export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';

export type LogProcessingStatus = 'unprocessed' | 'analyzing' | 'processed' | 'ignored';

export const RawLogPayloadSchema = z.object({
  level: z.enum(['debug', 'info', 'warn', 'error', 'fatal']),
  service: z.string().min(1).max(100),
  message: z.string().min(1).max(10000),
  stack: z.string().max(20000).optional(),
  timestamp: z.string().optional(),
  metadata: z.record(z.any()).optional().default({}),
});

export type RawLogPayload = z.infer<typeof RawLogPayloadSchema>;

export interface LogEntry {
  id: string;
  fingerprint: string;
  level: LogLevel;
  service: string;
  message: string;
  stack?: string;
  metadata: Record<string, any>;
  timestamp: string;
  firstSeen: string;
  lastSeen: string;
  occurrences: number;
  status: LogProcessingStatus;
  bugId?: string;
  createdAt: string;
  updatedAt: string;
}

export interface LogFilterQuery {
  level?: LogLevel;
  service?: string;
  status?: LogProcessingStatus;
  fingerprint?: string;
  limit?: number;
  offset?: number;
}
