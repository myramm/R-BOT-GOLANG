import { Request, Response, NextFunction } from 'express';
import { config } from '../config.js';
import { timingSafeEqual } from './tokens.js';

export function extractAuthToken(req: Request): string | null {
  const header = req.headers['authorization'];
  if (header && typeof header === 'string') {
    const parts = header.split(' ');
    if (parts.length === 2 && parts[0].toLowerCase() === 'bearer') {
      return parts[1];
    }
    return header;
  }

  const apiKeyHeader = req.headers['x-api-key'];
  if (apiKeyHeader && typeof apiKeyHeader === 'string') {
    return apiKeyHeader;
  }

  return null;
}

/**
 * Middleware: Requires valid Service / Sandbox API Key.
 */
export function requireApiKey(req: Request, res: Response, next: NextFunction): void {
  const token = extractAuthToken(req);

  if (!token || (!timingSafeEqual(token, config.apiKey) && !timingSafeEqual(token, config.ownerToken))) {
    res.status(401).json({
      ok: false,
      error: 'Unauthorized: Invalid or missing API key',
    });
    return;
  }

  next();
}

/**
 * Middleware: Requires Owner Approval Token.
 */
export function requireOwnerToken(req: Request, res: Response, next: NextFunction): void {
  const token = extractAuthToken(req);

  if (!token || !timingSafeEqual(token, config.ownerToken)) {
    res.status(403).json({
      ok: false,
      error: 'Forbidden: Valid Owner Token required for approval actions',
    });
    return;
  }

  next();
}
