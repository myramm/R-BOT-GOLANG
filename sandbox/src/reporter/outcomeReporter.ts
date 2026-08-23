import { Serv00ApiClient } from '../collector/client.js';
import { RemoteBugReport } from '../collector/types.js';
import { TestExecutionResult } from '../agy/testRunner.js';

export class OutcomeReporter {
  private client: Serv00ApiClient;

  constructor(client: Serv00ApiClient) {
    this.client = client;
  }

  /**
   * Formats the FIX SUCCESSFUL report.
   */
  formatSuccessReport(bug: RemoteBugReport, branch: string, diffSummary: string, testResult: TestExecutionResult): string {
    return `
==================================================
✅ FIX SUCCESSFUL
==================================================
Bug:            ${bug.id} (${bug.error})
Root Cause:     ${bug.rootCause}
Files Changed:  ${bug.affectedFiles.join(', ') || 'Inspected files'}
Git Branch:     ${branch}

Diff Summary:
${diffSummary}

Tests Passed:
${testResult.testsPassed.map((t) => `  - ${t}`).join('\n') || '  - Build and tests verified'}

Build Status:   ${testResult.buildSuccess ? 'SUCCESS' : 'FAILED'}

==================================================
⚠️ NOTICE: AGY does not automatically merge or deploy to production.
Owner holds final decision to review branch '${branch}' and merge.
==================================================
`.trim();
  }

  /**
   * Formats the FIX FAILED report.
   */
  formatFailureReport(bug: RemoteBugReport, branch: string, diffSummary: string, testResult: TestExecutionResult): string {
    return `
==================================================
❌ FIX FAILED
==================================================
Bug:                    ${bug.id} (${bug.error})
Git Branch:             ${branch}
Failed Tests:
${testResult.failedTests.map((t) => `  - ${t}`).join('\n') || '  - Test suite failure'}

Error Log:
${testResult.outputLog.slice(0, 1500)}

Recommended Next Step:
  - Keep branch '${branch}' for developer inspection
  - Revise fix logic or manual code investigation
==================================================
`.trim();
  }

  /**
   * Reports final outcome to Serv00 API.
   */
  async reportOutcome(
    bugId: string,
    success: boolean,
    branch: string,
    diffSummary: string,
    testResult: TestExecutionResult
  ): Promise<RemoteBugReport> {
    const status = success ? 'FIXED' : 'FAILED';

    return this.client.updateBugStatus(bugId, status, branch, {
      success,
      completedAt: new Date().toISOString(),
      testsPassed: testResult.testsPassed,
      failedTests: testResult.failedTests,
      diffSummary,
      outputLog: testResult.outputLog,
    });
  }
}
