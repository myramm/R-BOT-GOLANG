import dotenv from 'dotenv';
import path from 'path';

dotenv.config();

export interface SandboxConfig {
  nodeEnv: string;
  serv00ApiUrl: string;
  serv00ApiKey: string;
  serv00OwnerToken: string;
  targetRepoPath: string;
  pollIntervalMs: number;
  maxPollBackoffMs: number;
  requireOwnerApproval: boolean;
}

export const config: SandboxConfig = {
  nodeEnv: process.env.NODE_ENV || 'development',
  serv00ApiUrl: (process.env.SERV00_API_URL || 'http://s11.serv00.com:7090/api').replace(/\/$/, ''),
  serv00ApiKey: process.env.SERV00_API_KEY || 'serv00_sandbox_secure_token_change_in_production',
  serv00OwnerToken: process.env.SERV00_OWNER_TOKEN || 'owner_secret_approval_token_change_in_production',
  targetRepoPath: process.env.TARGET_REPO_PATH || path.resolve(process.cwd(), '../botgo'),
  pollIntervalMs: parseInt(process.env.POLL_INTERVAL_MS || '15000', 10),
  maxPollBackoffMs: parseInt(process.env.MAX_POLL_BACKOFF_MS || '60000', 10),
  requireOwnerApproval: process.env.REQUIRE_OWNER_APPROVAL !== 'false',
};
