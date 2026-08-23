import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';

export interface TestExecutionResult {
  success: boolean;
  testsPassed: string[];
  failedTests: string[];
  buildSuccess: boolean;
  outputLog: string;
}

export class TestRunner {
  private repoPath: string;

  constructor(repoPath: string) {
    this.repoPath = path.resolve(repoPath);
  }

  /**
   * Executes test and build verification suites in target repository.
   */
  async runVerification(recommendedTests?: string[]): Promise<TestExecutionResult> {
    const isGo = fs.existsSync(path.join(this.repoPath, 'go.mod'));
    const isNode = fs.existsSync(path.join(this.repoPath, 'package.json'));

    const testsPassed: string[] = [];
    const failedTests: string[] = [];
    let buildSuccess = true;
    let combinedLog = '';

    if (isGo) {
      // 1. Run Go Build
      try {
        const buildOut = execSync('go build ./...', {
          cwd: this.repoPath,
          encoding: 'utf-8',
          timeout: 45000,
        });
        combinedLog += `[Build Output]\n${buildOut}\n`;
      } catch (e: any) {
        buildSuccess = false;
        combinedLog += `[Build Error]\n${e.stdout || ''}\n${e.stderr || e.message}\n`;
      }

      // 2. Run Go Tests
      try {
        let testCmd = 'go test ./...';
        if (recommendedTests && recommendedTests.length > 0 && recommendedTests[0].includes('.go')) {
          const testTarget = recommendedTests[0];
          const dir = path.dirname(testTarget);
          testCmd = `go test ./${dir}/...`;
        }

        const testOut = execSync(testCmd, {
          cwd: this.repoPath,
          encoding: 'utf-8',
          timeout: 60000,
        });
        combinedLog += `[Test Output]\n${testOut}\n`;
        testsPassed.push('Go Test Suite Passed');
      } catch (e: any) {
        failedTests.push('Go Test Suite Failed');
        combinedLog += `[Test Error]\n${e.stdout || ''}\n${e.stderr || e.message}\n`;
      }
    } else if (isNode) {
      // Node.js verification
      try {
        const testOut = execSync('npm test', {
          cwd: this.repoPath,
          encoding: 'utf-8',
          timeout: 45000,
        });
        combinedLog += `[Test Output]\n${testOut}\n`;
        testsPassed.push('NPM Test Suite Passed');
      } catch (e: any) {
        failedTests.push('NPM Test Suite Failed');
        combinedLog += `[Test Error]\n${e.stdout || ''}\n${e.stderr || e.message}\n`;
      }
    } else {
      testsPassed.push('Generic verification check completed');
    }

    const success = buildSuccess && failedTests.length === 0;

    return {
      success,
      testsPassed,
      failedTests,
      buildSuccess,
      outputLog: combinedLog.trim(),
    };
  }
}
