import { Router, Request, Response } from 'express';
import { getStatsSummary } from '../../storage/db.js';

export const healthRouter = Router();

healthRouter.get('/health', (_req: Request, res: Response) => {
  const stats = getStatsSummary();

  res.status(200).json({
    ok: true,
    status: 'healthy',
    timestamp: new Date().toISOString(),
    uptimeSeconds: Math.floor(process.uptime()),
    service: 'serv00-bug-tracker-api',
    stats,
  });
});
