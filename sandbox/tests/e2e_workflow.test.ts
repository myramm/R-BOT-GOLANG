import { describe, it, expect, beforeAll, afterAll } from 'vitest';
import http from 'http';
import { execSync } from 'child_process';
import { createServer } from '../../serv00/src/api/server.js';
import { Serv00ApiClient } from '../src/collector/client.js';
import { BugDetector } from '../src/analyzer/detector.js';
import { ApprovalReporter } from '../src/approval/reporter.js';
import { FixRunner } from '../src/agy/fixRunner.js';
import { getDatabase, closeDatabase } from '../../serv00/src/storage/db.js';

describe('End-to-End Bug Detection & Owner Approval Workflow', () => {
  let server: http.Server;
  const testPort = 7199;
  const baseUrl = `http://localhost:${testPort}/api`;
  const apiKey = 'serv00_sandbox_secure_token_change_in_production';
  const ownerToken = 'owner_secret_approval_token_change_in_production';

  let client: Serv00ApiClient;
  let detector: BugDetector;
  let reporter: ApprovalReporter;
  let fixRunner: FixRunner;

  beforeAll(async () => {
    // Reset database
    const db = getDatabase();
    db.prepare('DELETE FROM logs').run();
    db.prepare('DELETE FROM bugs').run();

    const app = createServer();
    server = app.listen(testPort);

    client = new Serv00ApiClient(baseUrl, apiKey, ownerToken);
    detector = new BugDetector('/project/sandbox/botgo');
    reporter = new ApprovalReporter(client);
    fixRunner = new FixRunner('/project/sandbox/botgo', client);
  });

  afterAll(() => {
    try {
      execSync('git checkout main', { cwd: '/project/sandbox/botgo' });
    } catch {}
    closeDatabase();
    server.close();
  });

  it('completes the full flow: Log Ingestion -> AGY Analysis -> Bug Proposal -> Owner Approval -> Test Verification', async () => {
    // 1. Ingest Error Log into Serv00 API
    const logRes = await fetch(`${baseUrl}/logs`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify({
        level: 'error',
        service: 'botgo',
        message: 'Video terlalu panjang. Maksimal 8 detik.',
        stack: 'cmd/sticker.go:65',
      }),
    });
    const logJson = await logRes.json();
    expect(logJson.ok).toBe(true);
    const logEntry = logJson.log;

    // 2. Collector fetches unprocessed logs
    const unprocessed = await client.getUnprocessedLogs();
    expect(unprocessed.length).toBeGreaterThan(0);
    const targetLog = unprocessed.find((l) => l.id === logEntry.id);
    expect(targetLog).toBeDefined();

    // 3. Sandbox Detector analyzes error & inspects repository
    const analysis = await detector.analyze(targetLog!);
    expect(analysis.isRealBug).toBe(true);
    expect(analysis.affectedFiles).toContain('cmd/sticker.go');

    // 4. Sandbox submits Bug Proposal to Serv00
    const proposal = await reporter.submitProposal(targetLog!, analysis);
    expect(proposal.status).toBe('WAITING_FOR_OWNER_APPROVAL');

    const formattedReport = reporter.formatBugReportMarkdown(proposal);
    expect(formattedReport).toContain('BUG REPORT');
    expect(formattedReport).toContain('WAITING_FOR_OWNER_APPROVAL');

    // 5. SECURITY GATE CHECK: Attempting fix before approval MUST fail
    await expect(fixRunner.executeApprovedFix(proposal)).rejects.toThrow(
      /SECURITY GATE VIOLATION/
    );

    // 6. Owner Approves the Bug Fix
    const approvedBug = await client.approveBugAsOwner(proposal.id, 'Approved, proceed with test verification');
    expect(approvedBug.status).toBe('APPROVED');
    expect(approvedBug.ownerDecision?.approved).toBe(true);

    // 7. Fix Runner executes fix, branch checkout, tests & build verification
    const outcome = await fixRunner.executeApprovedFix(approvedBug);
    expect(outcome.success).toBe(true);
    expect(outcome.reportText).toContain('FIX SUCCESSFUL');
    expect(outcome.reportText).toContain('agy/fix/');

    // 8. Verify final status on Serv00
    const finalBug = await client.getBugById(proposal.id);
    expect(finalBug?.status).toBe('FIXED');
    expect(finalBug?.fixResult?.success).toBe(true);
  }, 45000);

  it('handles Owner Rejection properly and blocks any code modification', async () => {
    // 1. Ingest Another Error
    const logRes = await fetch(`${baseUrl}/logs`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${apiKey}`,
      },
      body: JSON.stringify({
        level: 'warn',
        service: 'botgo',
        message: 'Deprecated configuration key used: old_key',
        stack: 'brain/config/config.go:12',
      }),
    });
    const logJson = await logRes.json();
    const targetLog = logJson.log;

    // 2. Analyze & Submit Proposal
    const analysis = await detector.analyze(targetLog);
    const proposal = await reporter.submitProposal(targetLog, analysis);
    expect(proposal.status).toBe('WAITING_FOR_OWNER_APPROVAL');

    // 3. Owner Rejects
    const rejectedBug = await client.rejectBugAsOwner(proposal.id, 'Do not refactor config keys right now');
    expect(rejectedBug.status).toBe('REJECTED');
    expect(rejectedBug.ownerDecision?.approved).toBe(false);

    // 4. Fix Runner MUST refuse execution
    await expect(fixRunner.executeApprovedFix(rejectedBug)).rejects.toThrow(
      /SECURITY GATE VIOLATION/
    );

    // Verify status remains REJECTED
    const checkBug = await client.getBugById(proposal.id);
    expect(checkBug?.status).toBe('REJECTED');
  }, 15000);
});
