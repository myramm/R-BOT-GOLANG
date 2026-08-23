import { RemoteLogEntry } from '../collector/types.js';
import { CodeInspector } from './codeInspector.js';
import { BugAnalysisResult, ProposedFixPlan } from './types.js';

export class BugDetector {
  private inspector: CodeInspector;

  constructor(repoPath: string) {
    this.inspector = new CodeInspector(repoPath);
  }

  /**
   * Analyzes an error log and produces a complete Bug Analysis Proposal.
   */
  async analyze(log: RemoteLogEntry): Promise<BugAnalysisResult> {
    const context = this.inspector.inspectErrorContext(log.stack, log.message);
    const msg = (log.message || '').toLowerCase();
    const stack = (log.stack || '').toLowerCase();

    // 1. Determine Category & True Bug Verification
    let isRealBug = true;
    let category: BugAnalysisResult['category'] = 'CODE_DEFECT';
    let severity: BugAnalysisResult['severity'] = 'MEDIUM';
    let rootCause = '';
    let justification = '';

    if (msg.includes('panic') || stack.includes('panic:') || msg.includes('fatal error') || log.level === 'fatal') {
      isRealBug = true;
      severity = 'CRITICAL';
      category = 'CODE_DEFECT';
      rootCause = `Unhandled runtime panic / crash in service ${log.service}: ${log.message}`;
      justification = 'Runtime panic crashes execution flow and requires immediate code fix and recovery guard.';
    } else if (msg.includes('nil pointer') || msg.includes('null pointer') || msg.includes('undefined is not a function')) {
      isRealBug = true;
      severity = 'HIGH';
      category = 'CODE_DEFECT';
      rootCause = `Nil/Null reference encountered in ${context.targetFile || 'application code'}`;
      justification = 'Null/Nil dereference causes unhandled exception and failed user requests.';
    } else if (msg.includes('timeout') || msg.includes('econnrefused') || msg.includes('network')) {
      isRealBug = true;
      severity = 'MEDIUM';
      category = 'DEPENDENCY_ERROR';
      rootCause = `Network/External service connection failure: ${log.message}`;
      justification = 'External dependency or API endpoint timeout; requires graceful error handling and retry mechanism.';
    } else if (msg.includes('too long') || msg.includes('terlalu panjang') || msg.includes('invalid input') || msg.includes('bad request')) {
      isRealBug = true;
      severity = 'LOW';
      category = 'INPUT_VALIDATION';
      rootCause = `User validation message was improperly routed to system error tracker instead of direct chat warning`;
      justification = 'Input validation should notify user in chat and return nil rather than bubbling up as a system error.';
    } else {
      rootCause = `Application error in ${log.service}: ${log.message}`;
      justification = 'Identified anomaly in application log requiring developer attention.';
    }

    // 2. Identify affected files & functions
    const affectedFiles: string[] = [];
    if (context.targetFile) {
      affectedFiles.push(context.targetFile);
    }
    const affectedFunctions: string[] = [];
    if (context.targetFunction) {
      affectedFunctions.push(context.targetFunction);
    }

    // 3. Formulate Proposed Fix Plan & Risk Assessment
    const proposedFix: ProposedFixPlan = {
      summary: `Fix ${category.toLowerCase().replace('_', ' ')} in ${context.targetFile || log.service}`,
      steps: [
        `Inspect code at ${context.targetFile || 'affected file'}`,
        `Apply defensive checks, proper error handling, or validation warning response`,
        `Run associated unit tests and build verification`,
      ],
      affectedFiles: affectedFiles.length > 0 ? affectedFiles : ['(Determined during inspection)'],
      affectedFunctions: affectedFunctions.length > 0 ? affectedFunctions : ['(Determined during inspection)'],
      riskAssessment: severity === 'CRITICAL' || severity === 'HIGH' ? 'Moderate risk: impacts core execution' : 'Low risk: isolated to command/handler logic',
      recommendedTests: context.availableTests && context.availableTests.length > 0 ? context.availableTests : ['Run full package test suite'],
    };

    return {
      isRealBug,
      severity,
      category,
      rootCause,
      affectedFiles,
      affectedFunctions,
      codeContext: context,
      proposedFix,
      justification,
    };
  }
}
