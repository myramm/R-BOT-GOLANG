import { describe, it, expect } from 'vitest';
import { sanitizeText, sanitizeMetadata } from '../src/logs/sanitizer.js';

describe('Log Sanitizer', () => {
  it('redacts Bearer tokens in message text', () => {
    const raw = 'Request failed with Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.xyz.abc';
    const sanitized = sanitizeText(raw);
    expect(sanitized).not.toContain('eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.xyz.abc');
    expect(sanitized).toContain('[REDACTED');
  });

  it('redacts GitHub Tokens and OpenAI API keys', () => {
    const dummyGhp = 'ghp_mocktokenforunittest00000000000000000';
    const dummySk = 'sk-mockapikey0000000000000000000000000000';
    const raw = `Error uploading with token ${dummyGhp} and ${dummySk}`;
    const sanitized = sanitizeText(raw);
    expect(sanitized).not.toContain(dummyGhp);
    expect(sanitized).not.toContain(dummySk);
    expect(sanitized).toContain('[REDACTED_GITHUB_TOKEN]');
    expect(sanitized).toContain('[REDACTED_API_KEY]');
  });

  it('redacts basic auth in URLs', () => {
    const raw = 'Connecting to https://admin:SuperSecret123@db.example.com/data';
    const sanitized = sanitizeText(raw);
    expect(sanitized).not.toContain('SuperSecret123');
    expect(sanitized).toContain('[REDACTED_USER]:[REDACTED_PASS]@');
  });

  it('redacts sensitive keys in metadata object', () => {
    const metadata = {
      service: 'botgo',
      user: 'alice',
      password: 'mypassword123',
      apiKey: 'secret_key_abc',
      nested: {
        authorization: 'Bearer secret_token',
        normalInfo: 'visible',
      },
    };

    const sanitized = sanitizeMetadata(metadata);
    expect(sanitized.password).toBe('[REDACTED]');
    expect(sanitized.apiKey).toBe('[REDACTED]');
    expect(sanitized.user).toBe('alice');
    expect(sanitized.nested.authorization).toBe('[REDACTED]');
    expect(sanitized.nested.normalInfo).toBe('visible');
  });
});
