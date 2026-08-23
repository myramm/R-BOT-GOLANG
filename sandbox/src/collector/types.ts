export type LogLevel = 'debug' | 'info' | 'warn' | 'error' | 'fatal';
export type LogProcessingStatus = 'unprocessed' | 'analyzing' | 'processed' | 'ignored';

export interface RemoteLogEntry {
  id: string;
  fingerprint: string;
  level: LogLevel;
  service: string;
  message: string;
  stack?: string;
  metadata: Record<string, any>;
  timestamp: string;
  firstSeen: string;
  lastSeen: string;
  occurrences: number;
  status: LogProcessingStatus;
  bugId?: string;
  createdAt: string;
  updatedAt: string;
}

export type BugSeverity = 'LOW' | 'MEDIUM' | 'HIGH' | 'CRITICAL';

export type BugWorkflowStatus =
  | 'NEW'
  | 'ANALYZING'
  | 'BUG_FOUND'
  | 'WAITING_FOR_OWNER_APPROVAL'
  | 'APPROVED'
  | 'REJECTED'
  | 'FIXING'
  | 'TESTING'
  | 'FIXED'
  | 'FAILED';

export interface RemoteProposedFix {
  summary: string;
  steps: string[];
  affectedFiles: string[];
  affectedFunctions: string[];
  riskAssessment: string;
  recommendedTests: string[];
}

export interface RemoteBugReport {
  id: string;
  fingerprint: string;
  service: string;
  error: string;
  stack?: string;
  severity: BugSeverity;
  rootCause: string;
  affectedFiles: string[];
  affectedFunctions: string[];
  proposedFix: RemoteProposedFix;
  status: BugWorkflowStatus;
  incidentCount: number;
  firstSeen: string;
  lastSeen: string;
  ownerDecision?: {
    approved: boolean;
    decidedAt: string;
    note?: string;
  };
  fixBranch?: string;
  fixResult?: {
    success: boolean;
    completedAt: string;
    testsPassed?: string[];
    failedTests?: string[];
    diffSummary?: string;
    outputLog?: string;
  };
  createdAt: string;
  updatedAt: string;
}
