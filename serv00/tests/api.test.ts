import { describe, it, expect, beforeEach, afterAll } from 'vitest';
import request from 'supertest';
import { createServer } from '../src/api/server.js';
import { config } from '../src/config.js';
import { getDatabase, closeDatabase } from '../src/storage/db.js';

describe('Serv00 Bug Tracker API Endpoints', () => {
  const app = createServer();
  const validApiKey = config.apiKey;
  const validOwnerToken = config.ownerToken;

  beforeEach(() => {
    const db = getDatabase();
    db.prepare('DELETE FROM logs').run();
    db.prepare('DELETE FROM bugs').run();
  });

  afterAll(() => {
    closeDatabase();
  });

  describe('GET /api/health', () => {
    it('returns healthy status and stats', async () => {
      const res = await request(app).get('/api/health');
      expect(res.status).toBe(200);
      expect(res.body.ok).toBe(true);
      expect(res.body.status).toBe('healthy');
      expect(res.body.stats).toBeDefined();
    });
  });

  describe('POST /api/logs', () => {
    it('rejects unauthorized requests', async () => {
      const res = await request(app)
        .post('/api/logs')
        .send({ level: 'error', service: 'botgo', message: 'Test error' });

      expect(res.status).toBe(401);
      expect(res.body.ok).toBe(false);
    });

    it('ingests valid error log and sanitizes secrets', async () => {
      const dummyGhp = 'ghp_mocktesttoken000000000000000000000000';
      const res = await request(app)
        .post('/api/logs')
        .set('Authorization', `Bearer ${validApiKey}`)
        .send({
          level: 'error',
          service: 'botgo',
          message: `Error with secret token ${dummyGhp}`,
          stack: 'at stickerHandler (sticker.go:65)\nat Dispatch (command.go:916)',
          metadata: { apiKey: 'secret123', safeInfo: 'test' },
        });

      expect(res.status).toBe(201);
      expect(res.body.ok).toBe(true);
      expect(res.body.log).toBeDefined();
      expect(res.body.log.message).not.toContain(dummyGhp);
      expect(res.body.log.metadata.apiKey).toBe('[REDACTED]');
      expect(res.body.log.status).toBe('unprocessed');
      expect(res.body.log.occurrences).toBe(1);
    });

    it('deduplicates identical error logs and increments occurrence count', async () => {
      const payload = {
        level: 'error',
        service: 'botgo',
        message: 'Video terlalu panjang. Maksimal 8 detik for user 62812345678@s.whatsapp.net',
        stack: 'at stickerHandler (sticker.go:65)',
      };

      const res1 = await request(app)
        .post('/api/logs')
        .set('Authorization', `Bearer ${validApiKey}`)
        .send(payload);
      expect(res1.status).toBe(201);
      expect(res1.body.log.occurrences).toBe(1);

      // Send same error with different user ID
      const res2 = await request(app)
        .post('/api/logs')
        .set('Authorization', `Bearer ${validApiKey}`)
        .send({
          ...payload,
          message: 'Video terlalu panjang. Maksimal 8 detik for user 62899999999@s.whatsapp.net',
        });

      expect(res2.status).toBe(201);
      expect(res2.body.log.occurrences).toBe(2);
      expect(res2.body.log.id).toBe(res1.body.log.id);
    });
  });

  describe('Bug Report & Owner Approval Workflow', () => {
    it('handles bug proposal, owner approval, and rejection workflow', async () => {
      // 1. Ingest Bug Proposal (from Sandbox AGY)
      const bugProposal = {
        fingerprint: 'test_fp_12345',
        service: 'botgo',
        error: 'Video terlalu panjang error tracker alert',
        stack: 'at stickerHandler (sticker.go:65)',
        severity: 'MEDIUM' as const,
        rootCause: 'User validation treated as fatal error instead of user reply',
        affectedFiles: ['cmd/sticker.go'],
        affectedFunctions: ['stickerHandler'],
        proposedFix: {
          summary: 'Use stickerWarn to reply to user and return nil',
          steps: ['Add stickerWarn helper', 'Return nil without calling ReportErrorMessage'],
          affectedFiles: ['cmd/sticker.go'],
          affectedFunctions: ['stickerHandler'],
          riskAssessment: 'Low risk: only affects validation warning response',
          recommendedTests: ['go test ./cmd/sticker_test.go'],
        },
      };

      const createRes = await request(app)
        .post('/api/bugs')
        .set('Authorization', `Bearer ${validApiKey}`)
        .send(bugProposal);

      expect(createRes.status).toBe(201);
      expect(createRes.body.ok).toBe(true);
      const bugId = createRes.body.bug.id;
      expect(createRes.body.bug.status).toBe('WAITING_FOR_OWNER_APPROVAL');

      // 2. Reject attempt without Owner Token (Forbidden)
      const unauthReject = await request(app)
        .post(`/api/bugs/${bugId}/reject`)
        .set('Authorization', `Bearer ${validApiKey}`) // service key, not owner token
        .send({ reason: 'Not now' });
      expect(unauthReject.status).toBe(403);

      // 3. Approve with Owner Token
      const approveRes = await request(app)
        .post(`/api/bugs/${bugId}/approve`)
        .set('Authorization', `Bearer ${validOwnerToken}`)
        .send({ approved: true, note: 'Approved, please fix and run tests.' });

      expect(approveRes.status).toBe(200);
      expect(approveRes.body.ok).toBe(true);
      expect(approveRes.body.bug.status).toBe('APPROVED');
      expect(approveRes.body.bug.ownerDecision.approved).toBe(true);

      // 4. Update status to FIXING and FIXED (from Sandbox AGY)
      const fixingRes = await request(app)
        .patch(`/api/bugs/${bugId}/status`)
        .set('Authorization', `Bearer ${validApiKey}`)
        .send({ status: 'FIXING', fixBranch: 'agy/fix/BUG-01' });
      expect(fixingRes.status).toBe(200);
      expect(fixingRes.body.bug.status).toBe('FIXING');
      expect(fixingRes.body.bug.fixBranch).toBe('agy/fix/BUG-01');

      const fixedRes = await request(app)
        .patch(`/api/bugs/${bugId}/status`)
        .set('Authorization', `Bearer ${validApiKey}`)
        .send({
          status: 'FIXED',
          fixResult: {
            success: true,
            testsPassed: ['TestStickerAddExif'],
            diffSummary: '1 file changed, 8 insertions(+)',
          },
        });
      expect(fixedRes.status).toBe(200);
      expect(fixedRes.body.bug.status).toBe('FIXED');
      expect(fixedRes.body.bug.fixResult.success).toBe(true);
    });

    it('handles owner rejection properly', async () => {
      const bugProposal = {
        fingerprint: 'test_fp_reject_999',
        service: 'botgo',
        error: 'Refactor authentication module',
        severity: 'HIGH' as const,
        rootCause: 'Deprecated method',
        affectedFiles: ['brain/auth/auth.go'],
        affectedFunctions: ['VerifyToken'],
        proposedFix: {
          summary: 'Refactor VerifyToken',
          steps: ['Rewrite auth logic'],
          affectedFiles: ['brain/auth/auth.go'],
          affectedFunctions: ['VerifyToken'],
          riskAssessment: 'High risk to all users',
          recommendedTests: ['go test ./brain/auth/...'],
        },
      };

      const createRes = await request(app)
        .post('/api/bugs')
        .set('Authorization', `Bearer ${validApiKey}`)
        .send(bugProposal);

      const bugId = createRes.body.bug.id;

      const rejectRes = await request(app)
        .post(`/api/bugs/${bugId}/reject`)
        .set('Authorization', `Bearer ${validOwnerToken}`)
        .send({ reason: 'Jangan ubah bagian authentication' });

      expect(rejectRes.status).toBe(200);
      expect(rejectRes.body.bug.status).toBe('REJECTED');
      expect(rejectRes.body.bug.ownerDecision.approved).toBe(false);
      expect(rejectRes.body.bug.ownerDecision.note).toBe('Jangan ubah bagian authentication');
    });
  });
});
