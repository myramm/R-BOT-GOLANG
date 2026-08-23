import { describe, it, expect } from 'vitest';
import { generateFingerprint, normalizeErrorMessage } from '../src/logs/fingerprint.js';

describe('Error Fingerprint Generator', () => {
  it('normalizes dynamic IDs, memory addresses, and timestamps', () => {
    const msg1 = 'Failed query on user 628123456789@s.whatsapp.net at 2026-08-23T10:00:00Z (addr: 0x7ffee12)';
    const msg2 = 'Failed query on user 628987654321@s.whatsapp.net at 2026-08-23T10:05:00Z (addr: 0x7ffee99)';

    const norm1 = normalizeErrorMessage(msg1);
    const norm2 = normalizeErrorMessage(msg2);

    expect(norm1).toBe(norm2);
  });

  it('produces identical fingerprints for duplicate errors with different dynamic IDs', () => {
    const fp1 = generateFingerprint(
      'botgo',
      'error',
      'panic in command .sticker for sender 211991247462523@lid in chat 120363431240426186@g.us',
      'at stickerHandler (sticker.go:65)\nat Dispatch (command.go:916)'
    );

    const fp2 = generateFingerprint(
      'botgo',
      'error',
      'panic in command .sticker for sender 50324836462733@lid in chat 120363999999999999@g.us',
      'at stickerHandler (sticker.go:65)\nat Dispatch (command.go:916)'
    );

    expect(fp1).toBe(fp2);
  });

  it('produces different fingerprints for distinct error types', () => {
    const fp1 = generateFingerprint('botgo', 'error', 'database connection timeout', 'at db.go:10');
    const fp2 = generateFingerprint('botgo', 'error', 'nil pointer dereference', 'at main.go:20');

    expect(fp1).not.toBe(fp2);
  });
});
