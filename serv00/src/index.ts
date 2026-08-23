import { createServer } from './api/server.js';
import { config } from './config.js';
import { getDatabase } from './storage/db.js';

// Ensure Database is initialized
getDatabase();

const app = createServer();

const server = app.listen(config.port, () => {
  console.log(`====================================================`);
  console.log(`🚀 SERV00 BUG TRACKER API SERVICE RUNNING`);
  console.log(`====================================================`);
  console.log(`Port: ${config.port}`);
  console.log(`Environment: ${config.nodeEnv}`);
  console.log(`Database: ${config.dbPath}`);
  console.log(`Log Retention: ${config.logRetentionDays} days`);
  console.log(`Health Check: http://localhost:${config.port}/api/health`);
  console.log(`====================================================`);
});

process.on('SIGTERM', () => {
  console.log('Received SIGTERM, shutting down gracefully...');
  server.close(() => {
    process.exit(0);
  });
});

process.on('SIGINT', () => {
  console.log('Received SIGINT, shutting down gracefully...');
  server.close(() => {
    process.exit(0);
  });
});
