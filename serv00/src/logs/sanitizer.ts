/**
 * Log Sanitizer
 * Strips secrets, passwords, API keys, tokens, cookies, authorization headers,
 * private keys, credentials, and sensitive tokens from log messages, stack traces, and metadata.
 */

const SENSITIVE_KEY_PATTERNS = [
  /password/i,
  /passphrase/i,
  /secret/i,
  /token/i,
  /api[_-]?key/i,
  /auth(orization)?/i,
  /bearer/i,
  /cookie/i,
  /private[_-]?key/i,
  /access[_-]?token/i,
  /refresh[_-]?token/i,
  /credential/i,
  /session[_-]?id/i,
  /jwt/i,
  /ghp_[a-zA-Z0-9]{30,}/i,
  /sk-[a-zA-Z0-9]{20,}/i,
];

const SENSITIVE_VALUE_REGEXES = [
  // 1. Private Key Headers
  /-----BEGIN [A-Z ]+PRIVATE KEY-----[^-]+-----END [A-Z ]+PRIVATE KEY-----/gis,
  // 2. Basic Auth Credentials in URLs (e.g. https://user:pass@host)
  /(https?:\/\/)([^:\s]+):([^@\s]+)@/gi,
  // 3. Bearer tokens
  /(Bearer\s+)[a-zA-Z0-9._~+/-]+=*/gi,
  // 4. GitHub Personal Access Tokens
  /ghp_[a-zA-Z0-9]{36}/gi,
  // 5. OpenAI / Claude Secret Keys
  /sk-[a-zA-Z0-9_-]{20,}/gi,
  // 6. JWT tokens (3 base64url separated by dots)
  /eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+/g,
  // 7. Generic token / API key assignments (e.g. api_key="...", token: "...")
  /(["']?(?:api[_-]?key|secret|token|password|pass|auth|bearer)["']?\s*[:=]\s*["']?)([^"'\s,;&]+)(["']?)/gi,
];

export function sanitizeText(input: string | undefined): string {
  if (!input) return '';

  let sanitized = input;

  // Mask sensitive values in string
  for (const regex of SENSITIVE_VALUE_REGEXES) {
    if (regex.source.includes('BEGIN')) {
      sanitized = sanitized.replace(regex, '[REDACTED_PRIVATE_KEY]');
    } else if (regex.source.includes('https?:')) {
      sanitized = sanitized.replace(regex, '$1[REDACTED_USER]:[REDACTED_PASS]@');
    } else if (regex.source.includes('Bearer')) {
      sanitized = sanitized.replace(regex, '$1[REDACTED_TOKEN]');
    } else if (regex.source.includes('ghp_')) {
      sanitized = sanitized.replace(regex, '[REDACTED_GITHUB_TOKEN]');
    } else if (regex.source.includes('sk-')) {
      sanitized = sanitized.replace(regex, '[REDACTED_API_KEY]');
    } else if (regex.source.includes('eyJ')) {
      sanitized = sanitized.replace(regex, '[REDACTED_JWT]');
    } else {
      sanitized = sanitized.replace(regex, '$1[REDACTED]$3');
    }
  }

  return sanitized;
}

export function sanitizeMetadata(metadata: Record<string, any> | undefined): Record<string, any> {
  if (!metadata || typeof metadata !== 'object') {
    return {};
  }

  const result: Record<string, any> = {};

  for (const [key, value] of Object.entries(metadata)) {
    const isKeySensitive = SENSITIVE_KEY_PATTERNS.some((pattern) => pattern.test(key));

    if (isKeySensitive) {
      result[key] = '[REDACTED]';
    } else if (typeof value === 'string') {
      result[key] = sanitizeText(value);
    } else if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
      result[key] = sanitizeMetadata(value);
    } else if (Array.isArray(value)) {
      result[key] = value.map((item) => {
        if (typeof item === 'string') return sanitizeText(item);
        if (typeof item === 'object' && item !== null) return sanitizeMetadata(item);
        return item;
      });
    } else {
      result[key] = value;
    }
  }

  return result;
}
