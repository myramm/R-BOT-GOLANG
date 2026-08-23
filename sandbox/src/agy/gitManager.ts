import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';

export class GitManager {
  private repoPath: string;

  constructor(repoPath: string) {
    this.repoPath = path.resolve(repoPath);
  }

  private exec(cmd: string): string {
    return execSync(cmd, { cwd: this.repoPath, encoding: 'utf-8', timeout: 15000 }).trim();
  }

  /**
   * Checks if target is a valid git repository.
   */
  isGitRepo(): boolean {
    return fs.existsSync(path.join(this.repoPath, '.git'));
  }

  /**
   * Creates and checks out a dedicated branch: agy/fix/<bug-id>.
   */
  checkoutFixBranch(bugId: string): string {
    const branchName = `agy/fix/${bugId.toLowerCase()}`;

    if (!this.isGitRepo()) {
      return branchName;
    }

    try {
      // Check if branch already exists
      const existing = this.exec(`git branch --list ${branchName}`);
      if (existing) {
        this.exec(`git checkout ${branchName}`);
      } else {
        this.exec(`git checkout -b ${branchName}`);
      }
      return branchName;
    } catch (e: any) {
      console.warn(`[sandbox-git] Branch checkout fallback: ${e.message}`);
      return branchName;
    }
  }

  /**
   * Returns current git diff summary.
   */
  getDiffSummary(): string {
    if (!this.isGitRepo()) return 'No git repository';
    try {
      return this.exec('git diff --stat') || 'No modifications detected';
    } catch {
      return 'Diff unavailable';
    }
  }

  /**
   * Stages modified files and commits fix to branch.
   */
  commitFix(bugId: string, summary: string): boolean {
    if (!this.isGitRepo()) return true;
    try {
      this.exec('git add .');
      const status = this.exec('git status --porcelain');
      if (!status) return true; // Nothing to commit

      const msg = `fix(agy): resolve ${bugId} - ${summary.replace(/"/g, "'")}`;
      this.exec(`git commit -m "${msg}"`);
      return true;
    } catch (e: any) {
      console.error(`[sandbox-git] Commit failed: ${e.message}`);
      return false;
    }
  }
}
