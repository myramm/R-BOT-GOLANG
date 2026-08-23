import { describe, it, expect } from 'vitest';
import { BugDetector } from '../src/analyzer/detector.js';
import { RemoteLogEntry } from '../src/collector/types.js';

describe('Bug Detector & Analyzer', () => {
  const detector = new BugDetector('/project/sandbox/botgo');

  it('identifies unhandled runtime panics as CRITICAL defects', async () => {
    const log: RemoteLogEntry = {
      id: 'LOG-01',
      fingerprint: 'fp_panic_1',
      level: 'fatal',
      service: 'botgo',
      message: 'panic: runtime error: index out of range [5] with length 2',
      stack: 'goroutine 1 [running]:\ncmd/sticker.go:65 +0x12a',
      metadata: {},
      timestamp: new Date().toISOString(),
      firstSeen: new Date().toISOString(),
      lastSeen: new Date().toISOString(),
      occurrences: 1,
      status: 'unprocessed',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    const analysis = await detector.analyze(log);

    expect(analysis.isRealBug).toBe(true);
    expect(analysis.severity).toBe('CRITICAL');
    expect(analysis.category).toBe('CODE_DEFECT');
    expect(analysis.affectedFiles).toContain('cmd/sticker.go');
    expect(analysis.proposedFix.steps.length).toBeGreaterThan(0);
    expect(analysis.proposedFix.recommendedTests.length).toBeGreaterThan(0);
  });

  it('identifies input validation misclassifications as LOW severity', async () => {
    const log: RemoteLogEntry = {
      id: 'LOG-02',
      fingerprint: 'fp_validation_1',
      level: 'error',
      service: 'botgo',
      message: 'Video terlalu panjang. Maksimal 8 detik.',
      stack: 'cmd/sticker.go:65',
      metadata: {},
      timestamp: new Date().toISOString(),
      firstSeen: new Date().toISOString(),
      lastSeen: new Date().toISOString(),
      occurrences: 3,
      status: 'unprocessed',
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
    };

    const analysis = await detector.analyze(log);

    expect(analysis.isRealBug).toBe(true);
    expect(analysis.severity).toBe('LOW');
    expect(analysis.category).toBe('INPUT_VALIDATION');
  });
});
