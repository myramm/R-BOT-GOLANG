import { BugAnalysisResult } from '../analyzer/types.js';
import { Serv00ApiClient } from '../collector/client.js';
import { RemoteBugReport, RemoteLogEntry } from '../collector/types.js';

export class ApprovalReporter {
  private client: Serv00ApiClient;

  constructor(client: Serv00ApiClient) {
    this.client = client;
  }

  /**
   * Formats the official Bug Report Markdown for Owner inspection.
   */
  formatBugReportMarkdown(bug: RemoteBugReport): string {
    const fix = bug.proposedFix || {};
    const steps = (fix.steps || []).map((s: string) => `  - ${s}`).join('\n');
    const tests = (fix.recommendedTests || []).map((t: string) => `  - ${t}`).join('\n');

    return `
==================================================
BUG REPORT
==================================================
ID:                 ${bug.id}
Severity:           ${bug.severity}
Service:            ${bug.service}
Error:              ${bug.error}
Incident Count:     ${bug.incidentCount}
Root Cause:         ${bug.rootCause}
Affected Files:     ${bug.affectedFiles.join(', ') || 'None identified'}
Affected Functions: ${bug.affectedFunctions.join(', ') || 'None identified'}

Proposed Fix:
${steps || '  - Direct code remediation'}

Risk:
  - ${fix.riskAssessment || 'Low risk'}

Tests:
${tests || '  - Run package unit tests'}

Status:             ${bug.status}
==================================================
⚠️ ACTION REQUIRED: Owner approval required.
Approve command: POST /api/bugs/${bug.id}/approve
Reject command:  POST /api/bugs/${bug.id}/reject
==================================================
`.trim();
  }

  /**
   * Submits the Bug Proposal to Serv00 with status WAITING_FOR_OWNER_APPROVAL.
   */
  async submitProposal(log: RemoteLogEntry, analysis: BugAnalysisResult): Promise<RemoteBugReport> {
    const bug = await this.client.submitBugReport({
      fingerprint: log.fingerprint,
      service: log.service,
      error: log.message,
      stack: log.stack,
      severity: analysis.severity,
      rootCause: analysis.rootCause,
      affectedFiles: analysis.affectedFiles,
      affectedFunctions: analysis.affectedFunctions,
      proposedFix: {
        summary: analysis.proposedFix.summary,
        steps: analysis.proposedFix.steps,
        affectedFiles: analysis.proposedFix.affectedFiles,
        affectedFunctions: analysis.proposedFix.affectedFunctions,
        riskAssessment: analysis.proposedFix.riskAssessment,
        recommendedTests: analysis.proposedFix.recommendedTests,
      },
      status: 'WAITING_FOR_OWNER_APPROVAL',
    });

    // Mark the original log as analyzed / waiting
    await this.client.updateLogStatus(log.id, 'processed', bug.id);

    return bug;
  }
}
