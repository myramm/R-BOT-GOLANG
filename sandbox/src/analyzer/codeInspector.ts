import fs from 'fs';
import path from 'path';
import { execSync } from 'child_process';
import { CodeInspectionContext } from './types.js';

export class CodeInspector {
  private repoPath: string;

  constructor(repoPath: string) {
    this.repoPath = path.resolve(repoPath);
  }

  /**
   * Resolves target file and line number from stack trace or message text.
   */
  resolveFileAndLocation(stack?: string, message?: string): { filePath?: string; line?: number; funcName?: string } {
    if (stack) {
      // Go stack trace format: cmd/sticker.go:65 +0x12a or brain/web/server.go:880
      const goMatch = stack.match(/([a-zA-Z0-9_\-/]+\.go):(\d+)/);
      if (goMatch) {
        return {
          filePath: goMatch[1],
          line: parseInt(goMatch[2], 10),
        };
      }

      // JS/TS stack trace format: at stickerHandler (src/cmd/sticker.ts:65:10)
      const jsMatch = stack.match(/at (?:([a-zA-Z0-9_$.]+)\s+\()?([a-zA-Z0-9_\-/.]+\.[jt]sx?):(\d+)/);
      if (jsMatch) {
        return {
          funcName: jsMatch[1] || undefined,
          filePath: jsMatch[2],
          line: parseInt(jsMatch[3], 10),
        };
      }
    }

    if (message) {
      const match = message.match(/in (?:file |command )?([a-zA-Z0-9_\-/]+\.(?:go|ts|js)):?(\d+)?/i);
      if (match) {
        return {
          filePath: match[1],
          line: match[2] ? parseInt(match[2], 10) : undefined,
        };
      }
    }

    return {};
  }

  /**
   * Reads snippet of code around the affected line.
   */
  readCodeSnippet(relPath: string, targetLine?: number, contextRadius = 15): { snippet: string; lineStart: number; lineEnd: number } | null {
    try {
      const fullPath = path.isAbsolute(relPath) ? relPath : path.join(this.repoPath, relPath);

      if (!fs.existsSync(fullPath)) {
        return null;
      }

      const content = fs.readFileSync(fullPath, 'utf-8');
      const lines = content.split('\n');

      if (!targetLine || targetLine <= 0) {
        return {
          snippet: lines.slice(0, Math.min(lines.length, 40)).join('\n'),
          lineStart: 1,
          lineEnd: Math.min(lines.length, 40),
        };
      }

      const lineStart = Math.max(1, targetLine - contextRadius);
      const lineEnd = Math.min(lines.length, targetLine + contextRadius);

      const snippetLines: string[] = [];
      for (let i = lineStart; i <= lineEnd; i++) {
        const prefix = i === targetLine ? ' > ' : '   ';
        snippetLines.push(`${prefix}${i.toString().padStart(4, ' ')} | ${lines[i - 1]}`);
      }

      return {
        snippet: snippetLines.join('\n'),
        lineStart,
        lineEnd,
      };
    } catch {
      return null;
    }
  }

  /**
   * Finds associated test files for a source file.
   */
  findAssociatedTests(relPath: string): string[] {
    const tests: string[] = [];
    if (!relPath) return tests;

    const parsed = path.parse(relPath);

    if (parsed.ext === '.go') {
      const testFile = path.join(parsed.dir, `${parsed.name}_test.go`);
      if (fs.existsSync(path.join(this.repoPath, testFile))) {
        tests.push(testFile);
      }
    } else if (parsed.ext === '.ts' || parsed.ext === '.js') {
      const testFile1 = path.join(parsed.dir, `${parsed.name}.test${parsed.ext}`);
      const testFile2 = path.join(parsed.dir, '__tests__', `${parsed.name}.test${parsed.ext}`);
      if (fs.existsSync(path.join(this.repoPath, testFile1))) tests.push(testFile1);
      if (fs.existsSync(path.join(this.repoPath, testFile2))) tests.push(testFile2);
    }

    return tests;
  }

  /**
   * Inspects recent git diff or log.
   */
  getGitDiffSummary(): string {
    try {
      if (!fs.existsSync(path.join(this.repoPath, '.git'))) {
        return 'No git repository detected';
      }

      const status = execSync('git status --short', { cwd: this.repoPath, encoding: 'utf-8', timeout: 5000 });
      const recentLog = execSync('git log -n 3 --oneline', { cwd: this.repoPath, encoding: 'utf-8', timeout: 5000 });

      return `Recent Commits:\n${recentLog.trim()}\n\nWorking Directory Status:\n${status.trim() || 'Clean'}`;
    } catch (e: any) {
      return `Git inspection error: ${e.message}`;
    }
  }

  inspectErrorContext(stack?: string, message?: string): CodeInspectionContext {
    const location = this.resolveFileAndLocation(stack, message);
    const context: CodeInspectionContext = {
      targetFile: location.filePath,
      targetFunction: location.funcName,
    };

    if (location.filePath) {
      const snippetResult = this.readCodeSnippet(location.filePath, location.line);
      if (snippetResult) {
        context.codeSnippet = snippetResult.snippet;
        context.lineStart = snippetResult.lineStart;
        context.lineEnd = snippetResult.lineEnd;
      }

      context.availableTests = this.findAssociatedTests(location.filePath);
    }

    context.gitDiffSummary = this.getGitDiffSummary();
    return context;
  }
}
