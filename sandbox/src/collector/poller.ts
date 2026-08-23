import { Serv00ApiClient } from './client.js';
import { RemoteLogEntry } from './types.js';

export interface PollerOptions {
  intervalMs?: number;
  maxBackoffMs?: number;
  onLogReceived: (log: RemoteLogEntry) => Promise<void>;
  onError?: (err: Error) => void;
}

export class LogPoller {
  private client: Serv00ApiClient;
  private intervalMs: number;
  private maxBackoffMs: number;
  private currentBackoffMs: number;
  private onLogReceived: (log: RemoteLogEntry) => Promise<void>;
  private onError?: (err: Error) => void;
  private isRunning: boolean = false;
  private timer: NodeJS.Timeout | null = null;
  private processedInBatch: Set<string> = new Set();

  constructor(client: Serv00ApiClient, options: PollerOptions) {
    this.client = client;
    this.intervalMs = options.intervalMs || 15000;
    this.maxBackoffMs = options.maxBackoffMs || 60000;
    this.currentBackoffMs = this.intervalMs;
    this.onLogReceived = options.onLogReceived;
    this.onError = options.onError;
  }

  start(): void {
    if (this.isRunning) return;
    this.isRunning = true;
    console.log(`[sandbox-collector] Started log collector poller (interval: ${this.intervalMs}ms)`);
    this.pollLoop();
  }

  stop(): void {
    this.isRunning = false;
    if (this.timer) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    console.log('[sandbox-collector] Stopped log collector poller');
  }

  private async pollLoop(): Promise<void> {
    if (!this.isRunning) return;

    try {
      // Check if AGY is enabled on Serv00 Web Dashboard
      const agyStatus = await this.client.getAgyStatus();
      if (!agyStatus.enabled) {
        // AGY is toggled OFF by owner on web dashboard
        this.currentBackoffMs = Math.min(this.currentBackoffMs * 1.5, this.maxBackoffMs);
        if (this.isRunning) {
          this.timer = setTimeout(() => this.pollLoop(), this.currentBackoffMs);
        }
        return;
      }

      const logs = await this.client.getUnprocessedLogs(10);

      if (logs.length > 0) {
        // Reset backoff when data is found
        this.currentBackoffMs = this.intervalMs;

        for (const log of logs) {
          if (!this.isRunning) break;
          if (this.processedInBatch.has(log.id)) continue;

          this.processedInBatch.add(log.id);
          // Keep set bounded
          if (this.processedInBatch.size > 500) {
            this.processedInBatch.clear();
          }

          try {
            await this.onLogReceived(log);
          } catch (err: any) {
            console.error(`[sandbox-collector] Error processing log ${log.id}:`, err);
            if (this.onError) this.onError(err);
          }
        }
      } else {
        // Subtle backoff when idle
        this.currentBackoffMs = Math.min(this.currentBackoffMs * 1.2, this.maxBackoffMs);
      }
    } catch (err: any) {
      console.warn(`[sandbox-collector] Polling failed: ${err.message}. Retrying with exponential backoff.`);
      if (this.onError) this.onError(err);
      this.currentBackoffMs = Math.min(this.currentBackoffMs * 2, this.maxBackoffMs);
    }

    if (this.isRunning) {
      // Add +/- 15% random jitter to avoid synchronized polling
      const jitter = (Math.random() * 0.3 - 0.15) * this.currentBackoffMs;
      const delay = Math.max(2000, Math.floor(this.currentBackoffMs + jitter));

      this.timer = setTimeout(() => this.pollLoop(), delay);
    }
  }
}
