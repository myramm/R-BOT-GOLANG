import { config } from '../config.js';
import { RemoteBugReport, RemoteLogEntry } from './types.js';

export class Serv00ApiClient {
  private baseUrl: string;
  private apiKey: string;
  private ownerToken: string;

  constructor(baseUrl?: string, apiKey?: string, ownerToken?: string) {
    this.baseUrl = (baseUrl || config.serv00ApiUrl).replace(/\/$/, '');
    this.apiKey = apiKey || config.serv00ApiKey;
    this.ownerToken = ownerToken || config.serv00OwnerToken;
  }

  private async request<T>(
    endpoint: string,
    options: {
      method?: string;
      body?: any;
      useOwnerToken?: boolean;
      timeoutMs?: number;
    } = {}
  ): Promise<T> {
    const url = `${this.baseUrl}${endpoint.startsWith('/') ? endpoint : '/' + endpoint}`;
    const token = options.useOwnerToken ? this.ownerToken : this.apiKey;
    const timeout = options.timeoutMs || 10000;

    const controller = new AbortController();
    const timeoutId = setTimeout(() => controller.abort(), timeout);

    try {
      const response = await fetch(url, {
        method: options.method || 'GET',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
        },
        body: options.body ? JSON.stringify(options.body) : undefined,
        signal: controller.signal,
      });

      clearTimeout(timeoutId);

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Serv00 API Error [${response.status} ${response.statusText}]: ${errorText}`);
      }

      return (await response.json()) as T;
    } catch (err: any) {
      clearTimeout(timeoutId);
      if (err.name === 'AbortError') {
        throw new Error(`Serv00 API Timeout after ${timeout}ms: ${url}`);
      }
      throw err;
    }
  }

  async checkHealth(): Promise<{ ok: boolean; status: string; stats?: any }> {
    return this.request<{ ok: boolean; status: string; stats?: any }>('/health');
  }

  async getUnprocessedLogs(limit = 20): Promise<RemoteLogEntry[]> {
    const res = await this.request<{ ok: boolean; count: number; logs: RemoteLogEntry[] }>(
      `/logs?status=unprocessed&limit=${limit}`
    );
    return res.logs || [];
  }

  async updateLogStatus(id: string, status: string, bugId?: string): Promise<boolean> {
    const res = await this.request<{ ok: boolean }>(`/logs/${id}/status`, {
      method: 'PATCH',
      body: { status, bugId },
    });
    return res.ok;
  }

  async submitBugReport(bug: any): Promise<RemoteBugReport> {
    const res = await this.request<{ ok: boolean; bug: RemoteBugReport }>('/bugs', {
      method: 'POST',
      body: bug,
    });
    return res.bug;
  }

  async getBugById(id: string): Promise<RemoteBugReport | null> {
    try {
      const res = await this.request<{ ok: boolean; bug: RemoteBugReport }>(`/bugs/${id}`);
      return res.bug || null;
    } catch (e: any) {
      if (e.message?.includes('404')) return null;
      throw e;
    }
  }

  async getBugsByStatus(status: string): Promise<RemoteBugReport[]> {
    const res = await this.request<{ ok: boolean; bugs: RemoteBugReport[] }>(`/bugs?status=${status}`);
    return res.bugs || [];
  }

  async updateBugStatus(id: string, status: string, fixBranch?: string, fixResult?: any): Promise<RemoteBugReport> {
    const res = await this.request<{ ok: boolean; bug: RemoteBugReport }>(`/bugs/${id}/status`, {
      method: 'PATCH',
      body: { status, fixBranch, fixResult },
    });
    return res.bug;
  }

  async approveBugAsOwner(id: string, note?: string): Promise<RemoteBugReport> {
    const res = await this.request<{ ok: boolean; bug: RemoteBugReport }>(`/bugs/${id}/approve`, {
      method: 'POST',
      body: { approved: true, note },
      useOwnerToken: true,
    });
    return res.bug;
  }

  async rejectBugAsOwner(id: string, reason: string): Promise<RemoteBugReport> {
    const res = await this.request<{ ok: boolean; bug: RemoteBugReport }>(`/bugs/${id}/reject`, {
      method: 'POST',
      body: { approved: false, reason },
      useOwnerToken: true,
    });
    return res.bug;
  }
}
