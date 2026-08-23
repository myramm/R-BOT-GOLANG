import crypto from 'crypto';
import {
  saveOrUpdateBug,
  getBugs,
  getBugById,
  updateBugStatus,
  setBugOwnerDecision,
} from '../storage/db.js';
import { BugReport, BugWorkflowStatus, ProposedFix } from './types.js';

export function createOrUpdateBugReport(data: {
  fingerprint: string;
  service: string;
  error: string;
  stack?: string;
  severity: string;
  rootCause: string;
  affectedFiles: string[];
  affectedFunctions: string[];
  proposedFix: ProposedFix;
  status?: BugWorkflowStatus;
}): BugReport {
  const id = `BUG-${crypto.randomBytes(4).toString('hex').toUpperCase()}`;
  const status: BugWorkflowStatus = data.status || 'WAITING_FOR_OWNER_APPROVAL';

  return saveOrUpdateBug({
    id,
    fingerprint: data.fingerprint,
    service: data.service,
    error: data.error,
    stack: data.stack,
    severity: data.severity,
    rootCause: data.rootCause,
    affectedFiles: data.affectedFiles,
    affectedFunctions: data.affectedFunctions,
    proposedFix: data.proposedFix,
    status,
  });
}

export function listBugs(status?: BugWorkflowStatus, limit = 50, offset = 0): BugReport[] {
  return getBugs(status, limit, offset);
}

export function findBugById(id: string): BugReport | null {
  return getBugById(id);
}

export function approveBug(id: string, note?: string): BugReport | null {
  return setBugOwnerDecision(id, true, note);
}

export function rejectBug(id: string, reason: string): BugReport | null {
  return setBugOwnerDecision(id, false, reason);
}

export function transitionBugStatus(
  id: string,
  status: BugWorkflowStatus,
  fixBranch?: string,
  fixResult?: any
): boolean {
  return updateBugStatus(id, status, fixBranch, fixResult);
}
