import { Serv00ApiClient } from '../collector/client.js';
import { RemoteBugReport } from '../collector/types.js';
import { GitManager } from './gitManager.js';
import { TestRunner } from './testRunner.js';
import { OutcomeReporter } from '../reporter/outcomeReporter.js';

export class FixRunner {
  private client: Serv00ApiClient;
  private git: GitManager;
  private tester: TestRunner;
  private outcomeReporter: OutcomeReporter;
  private repoPath: string;

  constructor(repoPath: string, client: Serv00ApiClient) {
    this.repoPath = repoPath;
    this.client = client;
    this.git = new GitManager(repoPath);
    this.tester = new TestRunner(repoPath);
    this.outcomeReporter = new OutcomeReporter(client);
  }

  /**
   * Executes the complete fix, build, and test verification workflow for an APPROVED bug.
   */
  async executeApprovedFix(bug: RemoteBugReport): Promise<{ success: boolean; reportText: string }> {
    // 1. HARD GATE: Ensure status is APPROVED
    if (bug.status !== 'APPROVED') {
      throw new Error(
        `[SECURITY GATE VIOLATION] Cannot execute fix for bug ${bug.id}: Status is '${bug.status}'. Owner approval is MANDATORY.`
      );
    }

    console.log(`[sandbox-fix-runner] 🚀 Starting fix execution for APPROVED bug: ${bug.id}`);

    // 2. Transition status: FIXING
    await this.client.updateBugStatus(bug.id, 'FIXING');

    // 3. Checkout dedicated fix branch
    const branch = this.git.checkoutFixBranch(bug.id);
    console.log(`[sandbox-fix-runner] Checked out branch: ${branch}`);

    // 4. Transition status: TESTING
    await this.client.updateBugStatus(bug.id, 'TESTING', branch);

    // 5. Run Verification Tests & Build
    const testResult = await this.tester.runVerification(bug.proposedFix?.recommendedTests);
    const diffSummary = this.git.getDiffSummary();

    let reportText = '';

    if (testResult.success) {
      // Commit fix on branch
      this.git.commitFix(bug.id, bug.proposedFix?.summary || 'Fix code defect');

      reportText = this.outcomeReporter.formatSuccessReport(bug, branch, diffSummary, testResult);
      await this.outcomeReporter.reportOutcome(bug.id, true, branch, diffSummary, testResult);

      console.log(`\n${reportText}\n`);
      return { success: true, reportText };
    } else {
      reportText = this.outcomeReporter.formatFailureReport(bug, branch, diffSummary, testResult);
      await this.outcomeReporter.reportOutcome(bug.id, false, branch, diffSummary, testResult);

      console.log(`\n${reportText}\n`);
      return { success: false, reportText };
    }
  }
}
