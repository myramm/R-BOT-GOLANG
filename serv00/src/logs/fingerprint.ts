import crypto from 'crypto';

/**
 * Normalizes error messages and stack traces to produce stable fingerprints
 * by removing ephemeral values (UUIDs, timestamps, memory addresses, hex IDs, numbers).
 */
export function normalizeErrorMessage(message: string): string {
  if (!message) return '';

  return message
    .toLowerCase()
    // Normalize memory addresses (e.g. 0x7ffd234a)
    .replace(/0x[0-9a-fA-F]+/g, '0xHEX')
    // Normalize UUIDs (e.g. 123e4567-e89b-12d3-a456-426614174000)
    .replace(/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi, 'UUID')
    // Normalize timestamps (e.g. 2026-08-23T10:18:00Z or 2026-08-23 10:18:00)
    .replace(/\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?/g, 'TIMESTAMP')
    // Normalize IP addresses
    .replace(/\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b/g, 'IP_ADDR')
    // Normalize phone numbers & JIDs (e.g. 628512345678@s.whatsapp.net)
    .replace(/\d{8,16}@s\.whatsapp\.net/g, 'USER_JID')
    .replace(/\d{10,25}@g\.us/g, 'GROUP_JID')
    .replace(/\d{10,25}@lid/g, 'LID_JID')
    // Normalize pure numeric values (e.g. durations, IDs)
    .replace(/\b\d+\b/g, 'NUM')
    // Collapse extra whitespace
    .replace(/\s+/g, ' ')
    .trim();
}

/**
 * Extracts normalized top stack frame or location signature.
 */
export function extractStackSignature(stack?: string): string {
  if (!stack) return '';

  const lines = stack.split('\n');
  const relevantLines: string[] = [];

  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed.startsWith('at ') || trimmed.includes('.go:') || trimmed.includes('.ts:') || trimmed.includes('.js:')) {
      // Normalize absolute paths and line/column numbers
      const normalized = trimmed
        .replace(/(\/|\w:)?([a-zA-Z0-9_-]+\/)+/g, '') // Keep just filename
        .replace(/:(\d+)(:\d+)?/g, ':#') // Replace line & column with #
        .replace(/0x[0-9a-fA-F]+/g, '0xHEX');
      relevantLines.push(normalized);
      if (relevantLines.length >= 3) break; // Use top 3 relevant frames
    }
  }

  return relevantLines.join(' | ');
}

/**
 * Generates SHA-256 fingerprint from service, level, normalized message, and stack signature.
 */
export function generateFingerprint(service: string, level: string, message: string, stack?: string): string {
  const normService = (service || 'default').toLowerCase().trim();
  const normLevel = (level || 'error').toLowerCase().trim();
  const normMsg = normalizeErrorMessage(message);
  const normStack = extractStackSignature(stack);

  const payload = `${normService}:${normLevel}:${normMsg}:${normStack}`;
  return crypto.createHash('sha256').update(payload).digest('hex').substring(0, 32);
}
