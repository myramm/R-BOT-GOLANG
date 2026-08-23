import Database from 'better-sqlite3';
import fs from 'fs';
import path from 'path';
import { config } from '../config.js';
import { LogEntry, LogFilterQuery, LogLevel, LogProcessingStatus } from '../logs/types.js';
import { BugReport, BugWorkflowStatus } from '../bugs/types.js';

let dbInstance: Database.Database | null = null;

export function getDatabase(): Database.Database {
  if (dbInstance) {
    return dbInstance;
  }

  const dbDir = path.dirname(config.dbPath);
  if (!fs.existsSync(dbDir)) {
    fs.mkdirSync(dbDir, { recursive: true });
  }

  dbInstance = new Database(config.dbPath);

  // Enable WAL mode for high concurrency and fast reads/writes
  dbInstance.pragma('journal_mode = WAL');
  dbInstance.pragma('synchronous = NORMAL');
  dbInstance.pragma('foreign_keys = ON');

  initSchema(dbInstance);
  return dbInstance;
}

export function closeDatabase(): void {
  if (dbInstance) {
    try {
      dbInstance.close();
    } catch {}
    dbInstance = null;
  }
}

function initSchema(db: Database.Database) {
  db.exec(`
    CREATE TABLE IF NOT EXISTS logs (
      id TEXT PRIMARY KEY,
      fingerprint TEXT NOT NULL,
      level TEXT NOT NULL,
      service TEXT NOT NULL,
      message TEXT NOT NULL,
      stack TEXT,
      metadata TEXT NOT NULL DEFAULT '{}',
      status TEXT NOT NULL DEFAULT 'unprocessed',
      bug_id TEXT,
      occurrences INTEGER NOT NULL DEFAULT 1,
      first_seen TEXT NOT NULL,
      last_seen TEXT NOT NULL,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );

    CREATE INDEX IF NOT EXISTS idx_logs_fingerprint ON logs(fingerprint);
    CREATE INDEX IF NOT EXISTS idx_logs_status ON logs(status);
    CREATE INDEX IF NOT EXISTS idx_logs_created_at ON logs(created_at);
    CREATE INDEX IF NOT EXISTS idx_logs_service ON logs(service);

    CREATE TABLE IF NOT EXISTS bugs (
      id TEXT PRIMARY KEY,
      fingerprint TEXT UNIQUE NOT NULL,
      service TEXT NOT NULL,
      error TEXT NOT NULL,
      stack TEXT,
      severity TEXT NOT NULL,
      root_cause TEXT NOT NULL,
      affected_files TEXT NOT NULL DEFAULT '[]',
      affected_functions TEXT NOT NULL DEFAULT '[]',
      proposed_fix TEXT NOT NULL DEFAULT '{}',
      status TEXT NOT NULL DEFAULT 'WAITING_FOR_OWNER_APPROVAL',
      incident_count INTEGER NOT NULL DEFAULT 1,
      first_seen TEXT NOT NULL,
      last_seen TEXT NOT NULL,
      owner_decision TEXT,
      fix_branch TEXT,
      fix_result TEXT,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );

    CREATE INDEX IF NOT EXISTS idx_bugs_status ON bugs(status);
    CREATE INDEX IF NOT EXISTS idx_bugs_fingerprint ON bugs(fingerprint);
    CREATE INDEX IF NOT EXISTS idx_bugs_created_at ON bugs(created_at);
  `);
}

// ----------------------------------------------------
// Log Database Queries
// ----------------------------------------------------

export function saveOrUpdateLog(data: {
  id: string;
  fingerprint: string;
  level: LogLevel;
  service: string;
  message: string;
  stack?: string;
  metadata: Record<string, any>;
  timestamp: string;
}): LogEntry {
  const db = getDatabase();
  const now = new Date().toISOString();

  // Check if fingerprint already exists
  const existing = db
    .prepare('SELECT * FROM logs WHERE fingerprint = ? ORDER BY created_at DESC LIMIT 1')
    .get(data.fingerprint) as any;

  if (existing) {
    const occurrences = existing.occurrences + 1;
    db.prepare(`
      UPDATE logs 
      SET occurrences = ?, last_seen = ?, updated_at = ?, message = ?, stack = ?, metadata = ?
      WHERE id = ?
    `).run(
      occurrences,
      data.timestamp || now,
      now,
      data.message,
      data.stack || existing.stack,
      JSON.stringify(data.metadata),
      existing.id
    );

    return {
      id: existing.id,
      fingerprint: existing.fingerprint,
      level: existing.level as LogLevel,
      service: existing.service,
      message: data.message,
      stack: data.stack || existing.stack,
      metadata: data.metadata,
      timestamp: data.timestamp || now,
      firstSeen: existing.first_seen,
      lastSeen: data.timestamp || now,
      occurrences,
      status: existing.status as LogProcessingStatus,
      bugId: existing.bug_id || undefined,
      createdAt: existing.created_at,
      updatedAt: now,
    };
  }

  // Insert new log entry
  const firstSeen = data.timestamp || now;
  db.prepare(`
    INSERT INTO logs (
      id, fingerprint, level, service, message, stack, metadata, 
      status, occurrences, first_seen, last_seen, created_at, updated_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, 'unprocessed', 1, ?, ?, ?, ?)
  `).run(
    data.id,
    data.fingerprint,
    data.level,
    data.service,
    data.message,
    data.stack || null,
    JSON.stringify(data.metadata),
    firstSeen,
    firstSeen,
    now,
    now
  );

  return {
    id: data.id,
    fingerprint: data.fingerprint,
    level: data.level,
    service: data.service,
    message: data.message,
    stack: data.stack,
    metadata: data.metadata,
    timestamp: data.timestamp || now,
    firstSeen,
    lastSeen: firstSeen,
    occurrences: 1,
    status: 'unprocessed',
    createdAt: now,
    updatedAt: now,
  };
}

export function getLogs(filter: LogFilterQuery = {}): LogEntry[] {
  const db = getDatabase();
  const conditions: string[] = [];
  const params: any[] = [];

  if (filter.level) {
    conditions.push('level = ?');
    params.push(filter.level);
  }
  if (filter.service) {
    conditions.push('service = ?');
    params.push(filter.service);
  }
  if (filter.status) {
    conditions.push('status = ?');
    params.push(filter.status);
  }
  if (filter.fingerprint) {
    conditions.push('fingerprint = ?');
    params.push(filter.fingerprint);
  }

  const whereClause = conditions.length > 0 ? `WHERE ${conditions.join(' AND ')}` : '';
  const limit = filter.limit || 50;
  const offset = filter.offset || 0;

  params.push(limit, offset);

  const rows = db
    .prepare(`SELECT * FROM logs ${whereClause} ORDER BY created_at DESC LIMIT ? OFFSET ?`)
    .all(...params) as any[];

  return rows.map(mapRowToLogEntry);
}

export function getLogById(id: string): LogEntry | null {
  const db = getDatabase();
  const row = db.prepare('SELECT * FROM logs WHERE id = ?').get(id) as any;
  return row ? mapRowToLogEntry(row) : null;
}

export function updateLogStatus(id: string, status: LogProcessingStatus, bugId?: string): boolean {
  const db = getDatabase();
  const now = new Date().toISOString();
  const result = db
    .prepare('UPDATE logs SET status = ?, bug_id = COALESCE(?, bug_id), updated_at = ? WHERE id = ?')
    .run(status, bugId || null, now, id);
  return result.changes > 0;
}

export function purgeOldLogs(retentionDays: number): number {
  const db = getDatabase();
  const cutoff = new Date(Date.now() - retentionDays * 24 * 60 * 60 * 1000).toISOString();
  const result = db.prepare('DELETE FROM logs WHERE created_at < ?').run(cutoff);
  return result.changes;
}

function mapRowToLogEntry(row: any): LogEntry {
  let metadata = {};
  try {
    metadata = JSON.parse(row.metadata || '{}');
  } catch {}

  return {
    id: row.id,
    fingerprint: row.fingerprint,
    level: row.level as LogLevel,
    service: row.service,
    message: row.message,
    stack: row.stack || undefined,
    metadata,
    timestamp: row.last_seen || row.created_at,
    firstSeen: row.first_seen,
    lastSeen: row.last_seen,
    occurrences: row.occurrences,
    status: row.status as LogProcessingStatus,
    bugId: row.bug_id || undefined,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

// ----------------------------------------------------
// Bug Database Queries
// ----------------------------------------------------

export function saveOrUpdateBug(bug: {
  id: string;
  fingerprint: string;
  service: string;
  error: string;
  stack?: string;
  severity: string;
  rootCause: string;
  affectedFiles: string[];
  affectedFunctions: string[];
  proposedFix: any;
  status: BugWorkflowStatus;
}): BugReport {
  const db = getDatabase();
  const now = new Date().toISOString();

  const existing = db.prepare('SELECT * FROM bugs WHERE fingerprint = ?').get(bug.fingerprint) as any;

  if (existing) {
    const count = existing.incident_count + 1;
    db.prepare(`
      UPDATE bugs 
      SET incident_count = ?, last_seen = ?, updated_at = ?, severity = ?, root_cause = ?,
          affected_files = ?, affected_functions = ?, proposed_fix = ?
      WHERE id = ?
    `).run(
      count,
      now,
      now,
      bug.severity,
      bug.rootCause,
      JSON.stringify(bug.affectedFiles),
      JSON.stringify(bug.affectedFunctions),
      JSON.stringify(bug.proposedFix),
      existing.id
    );

    return mapRowToBugReport({
      ...existing,
      incident_count: count,
      last_seen: now,
      updated_at: now,
      severity: bug.severity,
      root_cause: bug.rootCause,
      affected_files: JSON.stringify(bug.affectedFiles),
      affected_functions: JSON.stringify(bug.affectedFunctions),
      proposed_fix: JSON.stringify(bug.proposedFix),
    });
  }

  db.prepare(`
    INSERT INTO bugs (
      id, fingerprint, service, error, stack, severity, root_cause,
      affected_files, affected_functions, proposed_fix, status,
      incident_count, first_seen, last_seen, created_at, updated_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
  `).run(
    bug.id,
    bug.fingerprint,
    bug.service,
    bug.error,
    bug.stack || null,
    bug.severity,
    bug.rootCause,
    JSON.stringify(bug.affectedFiles),
    JSON.stringify(bug.affectedFunctions),
    JSON.stringify(bug.proposedFix),
    bug.status,
    now,
    now,
    now,
    now
  );

  return {
    id: bug.id,
    fingerprint: bug.fingerprint,
    service: bug.service,
    error: bug.error,
    stack: bug.stack,
    severity: bug.severity as any,
    rootCause: bug.rootCause,
    affectedFiles: bug.affectedFiles,
    affectedFunctions: bug.affectedFunctions,
    proposedFix: bug.proposedFix,
    status: bug.status,
    incidentCount: 1,
    firstSeen: now,
    lastSeen: now,
    createdAt: now,
    updatedAt: now,
  };
}

export function getBugs(status?: BugWorkflowStatus, limit = 50, offset = 0): BugReport[] {
  const db = getDatabase();
  let query = 'SELECT * FROM bugs';
  const params: any[] = [];

  if (status) {
    query += ' WHERE status = ?';
    params.push(status);
  }

  query += ' ORDER BY updated_at DESC LIMIT ? OFFSET ?';
  params.push(limit, offset);

  const rows = db.prepare(query).all(...params) as any[];
  return rows.map(mapRowToBugReport);
}

export function getBugById(id: string): BugReport | null {
  const db = getDatabase();
  const row = db.prepare('SELECT * FROM bugs WHERE id = ?').get(id) as any;
  return row ? mapRowToBugReport(row) : null;
}

export function updateBugStatus(
  id: string,
  status: BugWorkflowStatus,
  fixBranch?: string,
  fixResult?: any
): boolean {
  const db = getDatabase();
  const now = new Date().toISOString();
  const result = db
    .prepare(`
      UPDATE bugs 
      SET status = ?, 
          fix_branch = COALESCE(?, fix_branch), 
          fix_result = COALESCE(?, fix_result), 
          updated_at = ? 
      WHERE id = ?
    `)
    .run(status, fixBranch || null, fixResult ? JSON.stringify(fixResult) : null, now, id);
  return result.changes > 0;
}

export function setBugOwnerDecision(
  id: string,
  approved: boolean,
  note?: string
): BugReport | null {
  const db = getDatabase();
  const now = new Date().toISOString();
  const decision = {
    approved,
    decidedAt: now,
    note,
  };
  const nextStatus: BugWorkflowStatus = approved ? 'APPROVED' : 'REJECTED';

  const result = db
    .prepare(`
      UPDATE bugs 
      SET status = ?, owner_decision = ?, updated_at = ? 
      WHERE id = ?
    `)
    .run(nextStatus, JSON.stringify(decision), now, id);

  if (result.changes === 0) return null;
  return getBugById(id);
}

function mapRowToBugReport(row: any): BugReport {
  let affectedFiles: string[] = [];
  let affectedFunctions: string[] = [];
  let proposedFix: any = {};
  let ownerDecision: any = undefined;
  let fixResult: any = undefined;

  try {
    affectedFiles = JSON.parse(row.affected_files || '[]');
  } catch {}
  try {
    affectedFunctions = JSON.parse(row.affected_functions || '[]');
  } catch {}
  try {
    proposedFix = JSON.parse(row.proposed_fix || '{}');
  } catch {}
  try {
    if (row.owner_decision) ownerDecision = JSON.parse(row.owner_decision);
  } catch {}
  try {
    if (row.fix_result) fixResult = JSON.parse(row.fix_result);
  } catch {}

  return {
    id: row.id,
    fingerprint: row.fingerprint,
    service: row.service,
    error: row.error,
    stack: row.stack || undefined,
    severity: row.severity,
    rootCause: row.root_cause,
    affectedFiles,
    affectedFunctions,
    proposedFix,
    status: row.status,
    incidentCount: row.incident_count,
    firstSeen: row.first_seen,
    lastSeen: row.last_seen,
    ownerDecision,
    fixBranch: row.fix_branch || undefined,
    fixResult,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

export function getStatsSummary() {
  const db = getDatabase();
  const totalLogs = (db.prepare('SELECT COUNT(*) as count FROM logs').get() as any)?.count || 0;
  const unprocessedLogs = (db.prepare("SELECT COUNT(*) as count FROM logs WHERE status = 'unprocessed'").get() as any)?.count || 0;
  const totalBugs = (db.prepare('SELECT COUNT(*) as count FROM bugs').get() as any)?.count || 0;
  const waitingApprovalBugs = (db.prepare("SELECT COUNT(*) as count FROM bugs WHERE status = 'WAITING_FOR_OWNER_APPROVAL'").get() as any)?.count || 0;
  const approvedBugs = (db.prepare("SELECT COUNT(*) as count FROM bugs WHERE status = 'APPROVED'").get() as any)?.count || 0;
  const fixedBugs = (db.prepare("SELECT COUNT(*) as count FROM bugs WHERE status = 'FIXED'").get() as any)?.count || 0;

  return {
    totalLogs,
    unprocessedLogs,
    totalBugs,
    waitingApprovalBugs,
    approvedBugs,
    fixedBugs,
  };
}
