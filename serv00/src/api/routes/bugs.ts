import { Router, Request, Response } from 'express';
import { requireApiKey, requireOwnerToken } from '../../auth/middleware.js';
import {
  ApproveBugSchema,
  CreateBugReportSchema,
  RejectBugSchema,
  UpdateBugStatusSchema,
} from '../../bugs/types.js';
import {
  approveBug,
  createOrUpdateBugReport,
  findBugById,
  listBugs,
  rejectBug,
  transitionBugStatus,
} from '../../bugs/manager.js';

export const bugsRouter = Router();

// List Bugs: GET /api/bugs
bugsRouter.get('/bugs', requireApiKey, (req: Request, res: Response) => {
  const status = req.query.status as any;
  const limit = req.query.limit ? parseInt(req.query.limit as string, 10) : 50;
  const offset = req.query.offset ? parseInt(req.query.offset as string, 10) : 0;

  const bugs = listBugs(status, limit, offset);

  res.status(200).json({
    ok: true,
    count: bugs.length,
    bugs,
  });
});

// Get Bug Detail: GET /api/bugs/:id
bugsRouter.get('/bugs/:id', requireApiKey, (req: Request, res: Response) => {
  const id = String(req.params.id);
  const bug = findBugById(id);

  if (!bug) {
    res.status(404).json({
      ok: false,
      error: `Bug report with ID '${id}' not found`,
    });
    return;
  }

  res.status(200).json({
    ok: true,
    bug,
  });
});

// Create / Ingest Bug Report Proposal: POST /api/bugs (Called by Sandbox AGY)
bugsRouter.post('/bugs', requireApiKey, (req: Request, res: Response) => {
  const parseResult = CreateBugReportSchema.safeParse(req.body);

  if (!parseResult.success) {
    res.status(400).json({
      ok: false,
      error: 'Invalid bug report payload format',
      details: parseResult.error.format(),
    });
    return;
  }

  const raw = parseResult.data;
  const bug = createOrUpdateBugReport({
    fingerprint: raw.fingerprint,
    service: raw.service,
    error: raw.error,
    stack: raw.stack,
    severity: raw.severity,
    rootCause: raw.rootCause,
    affectedFiles: raw.affectedFiles,
    affectedFunctions: raw.affectedFunctions,
    proposedFix: raw.proposedFix,
    status: raw.status,
  });

  res.status(201).json({
    ok: true,
    message: 'Bug report proposal stored. Waiting for Owner Approval.',
    bug,
  });
});

// Owner Approval: POST /api/bugs/:id/approve (Requires Owner Token)
bugsRouter.post('/bugs/:id/approve', requireOwnerToken, (req: Request, res: Response) => {
  const id = String(req.params.id);
  const parseResult = ApproveBugSchema.safeParse(req.body);

  if (!parseResult.success) {
    res.status(400).json({
      ok: false,
      error: 'Invalid approval request format. Expected { approved: true, note?: string }',
    });
    return;
  }

  const existing = findBugById(id);
  if (!existing) {
    res.status(404).json({
      ok: false,
      error: `Bug report with ID '${id}' not found`,
    });
    return;
  }

  const updatedBug = approveBug(id, parseResult.data.note);

  res.status(200).json({
    ok: true,
    message: `Bug '${id}' has been APPROVED by owner. AGY may proceed with fix branch and testing.`,
    bug: updatedBug,
  });
});

// Owner Rejection: POST /api/bugs/:id/reject (Requires Owner Token)
bugsRouter.post('/bugs/:id/reject', requireOwnerToken, (req: Request, res: Response) => {
  const id = String(req.params.id);
  const parseResult = RejectBugSchema.safeParse(req.body);

  if (!parseResult.success) {
    res.status(400).json({
      ok: false,
      error: 'Invalid rejection request format. Expected { reason: string }',
      details: parseResult.error.format(),
    });
    return;
  }

  const existing = findBugById(id);
  if (!existing) {
    res.status(404).json({
      ok: false,
      error: `Bug report with ID '${id}' not found`,
    });
    return;
  }

  const updatedBug = rejectBug(id, parseResult.data.reason);

  res.status(200).json({
    ok: true,
    message: `Bug '${id}' has been REJECTED by owner. AGY must not modify any source code.`,
    bug: updatedBug,
  });
});

// Update Status / Result: PATCH /api/bugs/:id/status (Called by Sandbox AGY)
bugsRouter.patch('/bugs/:id/status', requireApiKey, (req: Request, res: Response) => {
  const id = String(req.params.id);
  const parseResult = UpdateBugStatusSchema.safeParse(req.body);

  if (!parseResult.success) {
    res.status(400).json({
      ok: false,
      error: 'Invalid status update payload',
      details: parseResult.error.format(),
    });
    return;
  }

  const { status, fixBranch, fixResult } = parseResult.data;
  const success = transitionBugStatus(id, status, fixBranch, fixResult);

  if (!success) {
    res.status(404).json({
      ok: false,
      error: `Bug report with ID '${id}' not found`,
    });
    return;
  }

  const updatedBug = findBugById(id);

  res.status(200).json({
    ok: true,
    message: `Bug '${id}' status updated to '${status}'`,
    bug: updatedBug,
  });
});
