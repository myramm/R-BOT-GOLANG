import dotenv from 'dotenv';
import path from 'path';

dotenv.config();

export interface Config {
  port: number;
  nodeEnv: string;
  dbPath: string;
  apiKey: string;
  ownerToken: string;
  logRetentionDays: number;
  rateLimitPerMinute: number;
}

export const config: Config = {
  port: parseInt(process.env.PORT || '7090', 10),
  nodeEnv: process.env.NODE_ENV || 'development',
  dbPath: process.env.DB_PATH || path.join(process.cwd(), 'data', 'serv00_bug_tracker.db'),
  apiKey: process.env.API_KEY || 'serv00_sandbox_secure_token_change_in_production',
  ownerToken: process.env.OWNER_TOKEN || 'owner_secret_approval_token_change_in_production',
  logRetentionDays: parseInt(process.env.LOG_RETENTION_DAYS || '14', 10),
  rateLimitPerMinute: parseInt(process.env.RATE_LIMIT_PER_MINUTE || '120', 10),
};
