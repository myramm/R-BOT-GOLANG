import express, { Express, Request, Response, NextFunction } from 'express';
import cors from 'cors';
import helmet from 'helmet';
import rateLimit from 'express-rate-limit';
import { config } from '../config.js';
import { healthRouter } from './routes/health.js';
import { logsRouter } from './routes/logs.js';
import { bugsRouter } from './routes/bugs.js';
import { purgeOldLogs } from '../storage/db.js';

export function createServer(): Express {
  const app = express();

  // Security Headers
  app.use(helmet());

  // CORS - allow sandbox & dashboard access
  app.use(cors({ origin: true, credentials: true }));

  // Request Body Size Limit (Lightweight on Serv00)
  app.use(express.json({ limit: '1mb' }));
  app.use(express.urlencoded({ extended: true, limit: '1mb' }));

  // Rate Limiting
  const limiter = rateLimit({
    windowMs: 60 * 1000, // 1 minute
    max: config.rateLimitPerMinute,
    standardHeaders: true,
    legacyHeaders: false,
    message: {
      ok: false,
      error: 'Too many requests from this IP, please try again later.',
    },
  });
  app.use('/api/', limiter);

  // Mount API Routers
  app.use('/api', healthRouter);
  app.use('/api', logsRouter);
  app.use('/api', bugsRouter);

  // 404 Handler
  app.use((_req: Request, res: Response) => {
    res.status(404).json({
      ok: false,
      error: 'Endpoint not found',
    });
  });

  // Global Error Handler
  app.use((err: any, _req: Request, res: Response, _next: NextFunction) => {
    const status = err.status || err.statusCode || 500;
    res.status(status).json({
      ok: false,
      error: err.message || 'Internal Server Error',
    });
  });

  // Schedule periodic log retention cleanup (every 6 hours)
  setInterval(() => {
    try {
      const purged = purgeOldLogs(config.logRetentionDays);
      if (purged > 0) {
        console.log(`[serv00-storage] Purged ${purged} logs older than ${config.logRetentionDays} days`);
      }
    } catch (e) {
      console.error('[serv00-storage] Failed to purge old logs:', e);
    }
  }, 6 * 60 * 60 * 1000);

  return app;
}
