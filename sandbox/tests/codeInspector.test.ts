import { describe, it, expect } from 'vitest';
import { CodeInspector } from '../src/analyzer/codeInspector.js';

describe('Code Inspector', () => {
  const inspector = new CodeInspector('/project/sandbox/botgo');

  it('resolves Go stack trace file and line numbers', () => {
    const stack = 'goroutine 1 [running]:\nmain.stickerHandler(0x0, 0x0)\n\tcmd/sticker.go:65 +0x12a\nmain.Dispatch(...)';
    const loc = inspector.resolveFileAndLocation(stack);

    expect(loc.filePath).toBe('cmd/sticker.go');
    expect(loc.line).toBe(65);
  });

  it('reads code snippet around target line with line numbering', () => {
    const res = inspector.readCodeSnippet('cmd/sticker.go', 65, 5);
    expect(res).not.toBeNull();
    if (res) {
      expect(res.snippet).toContain('sticker');
      expect(res.lineStart).toBeLessThanOrEqual(65);
      expect(res.lineEnd).toBeGreaterThanOrEqual(65);
    }
  });

  it('finds associated test files for Go code', () => {
    const tests = inspector.findAssociatedTests('cmd/sticker.go');
    expect(tests).toContain('cmd/sticker_test.go');
  });
});
