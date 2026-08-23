import { BugSeverity } from '../collector/types.js';

export interface CodeInspectionContext {
  targetFile?: string;
  targetFunction?: string;
  codeSnippet?: string;
  lineStart?: number;
  lineEnd?: number;
  availableTests?: string[];
  gitDiffSummary?: string;
}

export interface ProposedFixPlan {
  summary: string;
  steps: string[];
  affectedFiles: string[];
  affectedFunctions: string[];
  riskAssessment: string;
  recommendedTests: string[];
}

export interface BugAnalysisResult {
  isRealBug: boolean;
  severity: BugSeverity;
  category: 'CODE_DEFECT' | 'INPUT_VALIDATION' | 'DEPENDENCY_ERROR' | 'ENVIRONMENT' | 'UNKNOWN';
  rootCause: string;
  affectedFiles: string[];
  affectedFunctions: string[];
  codeContext?: CodeInspectionContext;
  proposedFix: ProposedFixPlan;
  justification: string;
}
