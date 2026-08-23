import { z } from 'zod';

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

export interface ProposedFix {
  summary: string;
  steps: string[];
  affectedFiles: string[];
  affectedFunctions: string[];
  riskAssessment: string;
  recommendedTests: string[];
}

export interface BugReport {
  id: string;
  fingerprint: string;
  service: string;
  error: string;
  stack?: string;
  severity: BugSeverity;
  rootCause: string;
  affectedFiles: string[];
  affectedFunctions: string[];
  proposedFix: ProposedFix;
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

export const CreateBugReportSchema = z.object({
  fingerprint: z.string().min(1),
  service: z.string().min(1),
  error: z.string().min(1),
  stack: z.string().optional(),
  severity: z.enum(['LOW', 'MEDIUM', 'HIGH', 'CRITICAL']),
  rootCause: z.string().min(1),
  affectedFiles: z.array(z.string()).default([]),
  affectedFunctions: z.array(z.string()).default([]),
  proposedFix: z.object({
    summary: z.string().min(1),
    steps: z.array(z.string()).default([]),
    affectedFiles: z.array(z.string()).default([]),
    affectedFunctions: z.array(z.string()).default([]),
    riskAssessment: z.string().min(1),
    recommendedTests: z.array(z.string()).default([]),
  }),
  status: z
    .enum([
      'NEW',
      'ANALYZING',
      'BUG_FOUND',
      'WAITING_FOR_OWNER_APPROVAL',
      'APPROVED',
      'REJECTED',
      'FIXING',
      'TESTING',
      'FIXED',
      'FAILED',
    ])
    .default('WAITING_FOR_OWNER_APPROVAL'),
});

export const ApproveBugSchema = z.object({
  approved: z.literal(true),
  note: z.string().optional(),
});

export const RejectBugSchema = z.object({
  approved: z.literal(false).optional(),
  reason: z.string().min(1, 'Alasan penolakan (reason) harus disertakan'),
});

export const UpdateBugStatusSchema = z.object({
  status: z.enum([
    'NEW',
    'ANALYZING',
    'BUG_FOUND',
    'WAITING_FOR_OWNER_APPROVAL',
    'APPROVED',
    'REJECTED',
    'FIXING',
    'TESTING',
    'FIXED',
    'FAILED',
  ]),
  fixBranch: z.string().optional(),
  fixResult: z
    .object({
      success: z.boolean(),
      testsPassed: z.array(z.string()).optional(),
      failedTests: z.array(z.string()).optional(),
      diffSummary: z.string().optional(),
      outputLog: z.string().optional(),
    })
    .optional(),
});
