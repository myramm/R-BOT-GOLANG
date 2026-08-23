import { Router, Request, Response } from 'express';
import crypto from 'crypto';
import { requireApiKey } from '../../auth/middleware.js';
import { RawLogPayloadSchema } from '../../logs/types.js';
import { sanitizeMetadata, sanitizeText } from '../../logs/sanitizer.js';
import { generateFingerprint } from '../../logs/fingerprint.js';
import { getLogById, getLogs, saveOrUpdateLog, updateLogStatus } from '../../storage/db.js';

export const logsRouter = Router();

// Log Ingestion Endpoint: POST /api/logs
logsRouter.post('/logs', requireApiKey, (req: Request, res: Response) => {
  const parseResult = RawLogPayloadSchema.safeParse(req.body);

  if (!parseResult.success) {
    res.status(400).json({
      ok: false,
      error: 'Invalid log payload format',
      details: parseResult.error.format(),
    });
    return;
  }

  const raw = parseResult.data;

  // 1. Sanitize text and metadata
  const sanitizedMessage = sanitizeText(raw.message);
  const sanitizedStack = raw.stack ? sanitizeText(raw.stack) : undefined;
  const sanitizedMetadata = sanitizeMetadata(raw.metadata);

  // 2. Generate deterministic error fingerprint
  const fingerprint = generateFingerprint(raw.service, raw.level, sanitizedMessage, sanitizedStack);
  const logId = `LOG-${crypto.randomBytes(4).toString('hex').toUpperCase()}`;

  // 3. Save or increment duplicate incident in lightweight DB
  const savedEntry = saveOrUpdateLog({
    id: logId,
    fingerprint,
    level: raw.level,
    service: raw.service,
    message: sanitizedMessage,
    stack: sanitizedStack,
    metadata: sanitizedMetadata,
    timestamp: raw.timestamp || new Date().toISOString(),
  });

  res.status(201).json({
    ok: true,
    message: 'Log processed and stored successfully',
    log: savedEntry,
  });
});

// List Logs: GET /api/logs
logsRouter.get('/logs', requireApiKey, (req: Request, res: Response) => {
  const level = req.query.level as any;
  const service = req.query.service as string | undefined;
  const status = req.query.status as any;
  const fingerprint = req.query.fingerprint as string | undefined;
  const limit = req.query.limit ? parseInt(req.query.limit as string, 10) : 50;
  const offset = req.query.offset ? parseInt(req.query.offset as string, 10) : 0;

  const logs = getLogs({
    level,
    service,
    status,
    fingerprint,
    limit,
    offset,
  });

  res.status(200).json({
    ok: true,
    count: logs.length,
    logs,
  });
});

// Get Single Log: GET /api/logs/:id
logsRouter.get('/logs/:id', requireApiKey, (req: Request, res: Response) => {
  const id = String(req.params.id);
  const log = getLogById(id);

  if (!log) {
    res.status(404).json({
      ok: false,
      error: `Log with ID '${id}' not found`,
    });
    return;
  }

  res.status(200).json({
    ok: true,
    log,
  });
});

// Update Log Status: PATCH /api/logs/:id/status
logsRouter.patch('/logs/:id/status', requireApiKey, (req: Request, res: Response) => {
  const id = String(req.params.id);
  const { status, bugId } = req.body;

  if (!status || !['unprocessed', 'analyzing', 'processed', 'ignored'].includes(status)) {
    res.status(400).json({
      ok: false,
      error: "Status must be one of 'unprocessed', 'analyzing', 'processed', 'ignored'",
    });
    return;
  }

  const success = updateLogStatus(id, status, bugId);
  if (!success) {
    res.status(404).json({
      ok: false,
      error: `Log with ID '${id}' not found`,
    });
    return;
  }

  res.status(200).json({
    ok: true,
    message: `Log status updated to '${status}'`,
  });
});
