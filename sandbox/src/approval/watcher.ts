import { Serv00ApiClient } from '../collector/client.js';
import { RemoteBugReport } from '../collector/types.js';

export class ApprovalWatcher {
  private client: Serv00ApiClient;

  constructor(client: Serv00ApiClient) {
    this.client = client;
  }

  /**
   * Fetches all bugs currently approved by Owner and ready for fix execution.
   */
  async getApprovedBugs(): Promise<RemoteBugReport[]> {
    return this.client.getBugsByStatus('APPROVED');
  }

  /**
   * Checks the current status of a specific bug.
   */
  async checkStatus(bugId: string): Promise<RemoteBugReport | null> {
    return this.client.getBugById(bugId);
  }
}
