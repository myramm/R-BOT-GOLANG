import { config } from './config.js';
import { Serv00ApiClient } from './collector/client.js';
import { LogPoller } from './collector/poller.js';
import { BugDetector } from './analyzer/detector.js';
import { ApprovalReporter } from './approval/reporter.js';
import { ApprovalWatcher } from './approval/watcher.js';
import { FixRunner } from './agy/fixRunner.js';
import { RemoteLogEntry } from './collector/types.js';

async function main() {
  console.log(`====================================================`);
  console.log(`🤖 SANDBOX AGY BUG COLLECTOR & ANALYZER DAEMON`);
  console.log(`====================================================`);
  console.log(`Serv00 API URL:       ${config.serv00ApiUrl}`);
  console.log(`Target Repo Path:     ${config.targetRepoPath}`);
  console.log(`Polling Interval:     ${config.pollIntervalMs}ms`);
  console.log(`Require Approval:     ${config.requireOwnerApproval}`);
  console.log(`====================================================`);

  const client = new Serv00ApiClient();
  const detector = new BugDetector(config.targetRepoPath);
  const reporter = new ApprovalReporter(client);
  const watcher = new ApprovalWatcher(client);
  const fixRunner = new FixRunner(config.targetRepoPath, client);

  // 1. Verify Serv00 connection
  try {
    const health = await client.checkHealth();
    console.log(`[sandbox-daemon] Connected to Serv00 API. Status: ${health.status}`);
  } catch (err: any) {
    console.warn(`[sandbox-daemon] ⚠️ Serv00 API check warning: ${err.message}. Poller will retry.`);
  }

  // 2. Handler when new unprocessed error log arrives
  async function handleNewLog(log: RemoteLogEntry) {
    console.log(`\n[sandbox-daemon] 📥 New error log received: [${log.service}] ${log.message.slice(0, 100)}...`);

    // Analyze error and inspect codebase
    const analysis = await detector.analyze(log);

    if (analysis.isRealBug) {
      console.log(`[sandbox-daemon] 🔍 Bug detected (${analysis.severity}): ${analysis.rootCause}`);

      // Submit proposal to Serv00 with status WAITING_FOR_OWNER_APPROVAL
      const bugProposal = await reporter.submitProposal(log, analysis);
      const markdown = reporter.formatBugReportMarkdown(bugProposal);

      console.log(`\n${markdown}\n`);
    } else {
      console.log(`[sandbox-daemon] ℹ️ Log analyzed: not classified as code defect. Marking processed.`);
      await client.updateLogStatus(log.id, 'processed');
    }
  }

  // 3. Start Log Poller
  const poller = new LogPoller(client, {
    intervalMs: config.pollIntervalMs,
    maxBackoffMs: config.maxPollBackoffMs,
    onLogReceived: handleNewLog,
    onError: (err) => console.error('[sandbox-daemon] Collector error:', err.message),
  });

  poller.start();

  // 4. Background check for approved bugs (every 30s)
  const approvalCheckInterval = setInterval(async () => {
    try {
      const approvedBugs = await watcher.getApprovedBugs();
      for (const bug of approvedBugs) {
        console.log(`\n[sandbox-daemon] 🟢 Found APPROVED bug by owner: ${bug.id}`);
        await fixRunner.executeApprovedFix(bug);
      }
    } catch (err: any) {
      console.error('[sandbox-daemon] Error checking approved bugs:', err.message);
    }
  }, 30000);

  // Graceful shutdown
  const shutdown = () => {
    console.log('\n[sandbox-daemon] Shutting down gracefully...');
    poller.stop();
    clearInterval(approvalCheckInterval);
    process.exit(0);
  };

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

main().catch((err) => {
  console.error('[sandbox-daemon] Fatal error:', err);
  process.exit(1);
});
